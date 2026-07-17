package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/sahilm/fuzzy"

	"github.com/TenaciousMaker/revui/internal/config"
	"github.com/TenaciousMaker/revui/internal/diff"
	"github.com/TenaciousMaker/revui/internal/gitrepo"
	"github.com/TenaciousMaker/revui/internal/review"
	"github.com/TenaciousMaker/revui/internal/watcher"
)

type focusArea uint8
type viewMode uint8
type mode uint8

const (
	focusFiles focusArea = iota
	focusDiff
)

const (
	unified viewMode = iota
	split
)

const (
	normal mode = iota
	searching
	searchingRepository
	showHelp
)

type refreshMsg struct {
	repo      *gitrepo.Repository
	err       error
	automatic bool
	id        uint64
}

type repositoryWatchMsg struct {
	event  watcher.Event
	closed bool
}

type repositoryWatcher interface {
	Events() <-chan watcher.Event
	Close() error
}

type renderCache struct {
	version uint64
	view    tea.View
	valid   bool
}

type Model struct {
	filePaneState
	contentPaneState
	searchState
	selectionState
	viewportState

	repo                *gitrepo.Repository
	repositories        repositoryOperations
	watchers            watcherFactory
	clipboard           clipboardWriter
	refreshCancel       context.CancelFunc
	refreshID           uint64
	searchCancel        context.CancelFunc
	searchID            uint64
	session             review.Session
	theme               theme
	highlight           *highlighter
	mode                mode
	status              string
	preferencesPath     string
	preferences         preferenceStore
	reviews             reviewStore
	watcher             repositoryWatcher
	watchRefreshRunning bool
	watchRefreshPending bool
	renderVersion       uint64
	renderCache         *renderCache
}

func New(repo *gitrepo.Repository) (Model, error) {
	return newModel(repo, filePreferenceStore{}, fileReviewStore{})
}

func newModel(repo *gitrepo.Repository, preferences preferenceStore, reviews reviewStore) (Model, error) {
	session, err := reviews.Load(repo.ReviewPath, repo.Branch, repo.Base)
	if err != nil {
		return Model{}, err
	}
	preferencesPath := repo.PreferencesPath
	legacyPreferencesPath := ""
	var preferencesPathErr error
	if preferencesPath == "" {
		preferencesPath, preferencesPathErr = preferences.UserPath()
		if repo.ReviewPath != "" {
			legacyPreferencesPath = filepath.Join(filepath.Dir(repo.ReviewPath), "preferences.json")
		}
	}
	loadedPreferences, preferencesErr := preferences.LoadWithFallback(preferencesPath, legacyPreferencesPath)
	m := Model{
		repo: repo, clipboard: terminalClipboard{}, session: session, theme: newTheme(), highlight: &highlighter{},
		preferences: preferences, reviews: reviews,
		repositories: gitRepositoryOperations{}, watchers: filesystemWatcherFactory{},
		viewportState:    viewportState{focus: focusFiles, now: time.Now},
		contentPaneState: contentPaneState{view: unified, selectFrom: -1},
		filePaneState:    filePaneState{collapsed: map[string]bool{}},
		renderCache:      &renderCache{},
		preferencesPath:  preferencesPath,
	}
	m.applyPreferences(loadedPreferences)
	m.rebuildTreeNodes()
	if len(repo.Files) == 0 {
		m.status = "No changes yet. revui will refresh when you press R."
	} else {
		m.status = "Ready to review. Press ? for keys."
	}
	if preferencesPathErr != nil {
		m.status = "View preferences unavailable: " + preferencesPathErr.Error()
	} else if preferencesErr != nil {
		m.status = "View preferences ignored: " + preferencesErr.Error()
	}
	return m, nil
}

func (m *Model) EnableWatching() {
	watch, err := m.watchers.New(m.repo.Root, m.repo.AllPaths)
	if err != nil {
		m.status = "Realtime refresh unavailable: " + err.Error() + ". Press R to refresh."
		return
	}
	m.watcher = watch
	if len(m.repo.Files) == 0 {
		m.status = "Watching for changes."
	}
}

// Close releases background resources. It is safe to call more than once.
func (m *Model) Close() { m.stopWatcher() }

func (m Model) Init() tea.Cmd { return m.watchCmd() }

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message.(type) {
	case tea.MouseWheelMsg, mouseWheelFrameMsg:
		// Wheel events manage render invalidation at their coalesced frame rate.
	default:
		m.invalidateRender()
	}
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if m.mode == searchingRepository {
			m.ensureRepositorySearchVisible()
		}
		m.ensureVisible()
		return m, nil
	case tea.MouseWheelMsg:
		return m.queueMouseWheel(msg)
	case mouseWheelFrameMsg:
		return m.flushMouseWheel(), nil
	case tea.MouseClickMsg:
		return m.handleMouseClick(msg), nil
	case tea.MouseMotionMsg:
		return m.handleMouseMotion(msg), nil
	case tea.MouseReleaseMsg:
		return m.handleMouseRelease(msg), nil
	case repositoryWatchMsg:
		if msg.closed {
			m.watcher = nil
			return m, nil
		}
		next := m.watchCmd()
		if msg.event.Err != nil {
			m.status = "Realtime watcher error: " + msg.event.Err.Error() + ". Press R to refresh."
			return m, next
		}
		if m.watchRefreshRunning {
			m.watchRefreshPending = true
			return m, next
		}
		m.watchRefreshRunning = true
		return m, tea.Batch(next, m.refreshCmd(true))
	case refreshMsg:
		if msg.id != m.refreshID {
			return m, nil
		}
		m.refreshCancel = nil
		if msg.err != nil {
			m.status = "Refresh failed: " + msg.err.Error()
		} else {
			m.applyRefresh(msg.repo, msg.automatic)
		}
		if msg.automatic {
			m.watchRefreshRunning = false
			if m.watchRefreshPending {
				m.watchRefreshPending = false
				m.watchRefreshRunning = true
				return m, m.refreshCmd(true)
			}
		}
		return m, nil
	case repositorySearchMsg:
		if m.mode != searchingRepository || !m.repoSearching || msg.query != m.input || msg.id != m.searchID {
			return m, nil
		}
		m.repoSearching = false
		m.repoSearchReady = true
		m.repoHits = msg.matches
		m.repoSearchAt = 0
		m.repoSearchTop = 0
		if msg.err != nil {
			m.status = "Repository search failed: " + msg.err.Error()
			m.repoHits = nil
		} else if len(msg.matches) == 0 {
			m.status = fmt.Sprintf("No repository matches for %q.", msg.query)
		} else {
			m.status = fmt.Sprintf("%d repository matches. Enter opens the selected source line.", len(msg.matches))
		}
		return m, nil
	case clipboardResultMsg:
		if msg.err != nil {
			m.status = "Copy failed: " + msg.err.Error()
		} else {
			m.status = clipboardStatus(msg.lineCount)
		}
		return m, nil
	case tea.PasteMsg:
		return m.handlePaste(msg.Content)
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handlePaste(content string) (tea.Model, tea.Cmd) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.NewReplacer("\r", " ", "\n", " ").Replace(content)
	switch m.mode {
	case searchingRepository:
		m.insertInput(content)
		m.resetRepositoryResults()
		m.ensureRepositorySearchVisible()
	case searching:
		m.insertInput(content)
		m.updateSearch()
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.mode == searching {
		return m.handleSearch(msg)
	}
	if m.mode == searchingRepository {
		return m.handleRepositorySearch(msg)
	}
	if m.mode == showHelp {
		if msg.String() == "esc" || msg.String() == "?" || msg.String() == "q" {
			m.mode = normal
		}
		return m, nil
	}
	if m.sourcePath != "" && m.focus == focusDiff {
		return m.handleSourceKey(msg)
	}

	key := msg.String()
	if key != "f" && key != "y" {
		m.clearMouseSelection()
	}
	switch key {
	case "ctrl+c", "q":
		m.stopWatcher()
		return m, tea.Quit
	case "?":
		m.mode = showHelp
	case "tab":
		if m.width < 90 {
			m.focus = 1 - m.focus
		} else if m.focus == focusFiles {
			m.focus = focusDiff
		} else {
			m.focus = focusFiles
		}
	case "left", "h":
		if m.focus == focusFiles && m.fileLayout == treeFiles {
			m.collapseTreeNodeOrSelectParent()
		} else {
			m.focus = focusFiles
		}
	case "right", "l", "enter":
		if m.focus == focusFiles && len(m.repo.Files) > 0 {
			if m.fileLayout == treeFiles {
				handled := false
				if key == "enter" {
					handled = m.activateTreeNode()
				} else {
					handled = m.advanceIntoTreeNode()
				}
				if handled {
					break
				}
			}
			m.focus = focusDiff
			m.resetLineCursor()
		}
	case "up", "k":
		m.move(-1)
	case "down", "j":
		m.move(1)
	case "pgup", "ctrl+u":
		m.move(-m.pageSize())
	case "pgdown", "ctrl+d":
		m.move(m.pageSize())
	case "home", "g":
		if m.focus == focusFiles {
			if m.fileLayout == treeFiles {
				m.treeCursor = 0
				m.moveTreeCursor(0)
			} else {
				m.file = 0
			}
		} else if m.view == split {
			m.splitCursor = 0
			m.syncLineFromSplitCursor()
		} else {
			m.line = 0
		}
		m.ensureVisible()
	case "end", "G", "shift+g":
		if m.focus == focusFiles {
			if m.fileLayout == treeFiles {
				m.treeCursor = max(0, len(m.currentTreeNodes())-1)
				m.moveTreeCursor(0)
			} else {
				m.file = max(0, len(m.repo.Files)-1)
			}
		} else if m.view == split {
			m.splitCursor = max(0, len(m.currentSplitRows())-1)
			m.syncLineFromSplitCursor()
		} else {
			m.line = max(0, len(m.currentLines())-1)
		}
		m.ensureVisible()
	case "/":
		m.mode, m.searchAt, m.searchTop = searching, 0, 0
		m.setInput("")
		m.updateSearch()
	case "f":
		return m, m.beginRepositorySearch(m.selectedText)
	case "o":
		m.openCurrentFileSource()
	case "y":
		return m, m.copySelectionCmd()
	case "t":
		m.toggleFileLayout()
	case "A", "shift+a":
		m.cycleFileScope()
	case " ", "space":
		m.toggleReviewedFile()
	case "w":
		m.toggleFilePaneWidth()
	case "v":
		if m.focus == focusDiff && len(m.currentLines()) > 0 {
			if m.selectFrom >= 0 {
				m.selectFrom = -1
				m.status = "Range selection cleared."
			} else {
				m.selectFrom = m.line
				m.status = "Range started. Move, then press y to copy."
			}
		}
	case "s":
		if m.view == unified {
			m.view = split
			m.syncSplitCursorToLine()
			m.ensureVisible()
			m.status = "Split diff view."
		} else {
			m.view = unified
			m.ensureVisible()
			m.status = "Unified diff view."
		}
		m.persistPreferences()
	case "]":
		m.jumpHunk(1)
	case "[":
		m.jumpHunk(-1)
	case "R", "shift+r", "ctrl+r":
		m.status = "Refreshing diff…"
		return m, m.refreshCmd(false)
	}
	return m, nil
}

func (m Model) handleSearch(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if handled, changed := m.editInput(msg); handled {
		if changed {
			m.updateSearch()
		}
		return m, nil
	}
	switch msg.String() {
	case "esc":
		m.mode = normal
	case "enter":
		if len(m.searchHits) > 0 {
			m.file = m.searchHits[m.searchAt]
			m.syncTreeCursorToFile()
			m.resetLineCursor()
			m.focus, m.mode = focusDiff, normal
			m.ensureVisible()
		}
	case "up", "ctrl+p":
		m.searchAt = max(0, m.searchAt-1)
		m.ensureFileSearchVisible()
	case "down", "ctrl+n":
		m.searchAt = min(max(0, len(m.searchHits)-1), m.searchAt+1)
		m.ensureFileSearchVisible()
	}
	return m, nil
}

func (m *Model) move(delta int) {
	if m.focus == focusFiles {
		if m.fileLayout == treeFiles {
			m.moveTreeCursor(delta)
		} else {
			m.file = clamp(m.file+delta, 0, max(0, len(m.repo.Files)-1))
			m.resetLineCursor()
		}
	} else if m.view == split {
		m.splitCursor = clamp(m.splitCursor+delta, 0, max(0, len(m.currentSplitRows())-1))
		m.syncLineFromSplitCursor()
	} else {
		m.line = clamp(m.line+delta, 0, max(0, len(m.currentLines())-1))
	}
	m.ensureVisible()
}

func (m *Model) ensureVisible() {
	page := m.pageSize()
	if m.focus == focusFiles {
		m.ensureFileVisible()
		return
	}
	if m.sourcePath != "" {
		if m.sourceLine < m.sourceScroll {
			m.sourceScroll = m.sourceLine
		}
		if m.sourceLine >= m.sourceScroll+page {
			m.sourceScroll = m.sourceLine - page + 1
		}
		return
	}
	if m.focus != focusFiles {
		cursor, scroll := m.line, &m.lineScroll
		if m.view == split {
			cursor, scroll = m.splitCursor, &m.splitScroll
		}
		if cursor < *scroll {
			*scroll = cursor
		}
		if cursor >= *scroll+page {
			*scroll = cursor - page + 1
		}
	}
}

func (m *Model) ensureFileVisible() {
	page := m.pageSize()
	cursor := m.file
	if m.fileLayout == treeFiles {
		cursor = m.treeCursor
	}
	if cursor < m.fileScroll {
		m.fileScroll = cursor
	}
	if cursor >= m.fileScroll+page {
		m.fileScroll = cursor - page + 1
	}
}

func (m Model) pageSize() int { return max(4, m.height-8) }

func (m Model) currentLines() []diff.Line {
	if m.file < 0 || m.file >= len(m.repo.Files) {
		return nil
	}
	return m.repo.Files[m.file].Lines
}

func (m Model) currentSplitRows() []splitRow { return splitRows(m.currentLines()) }

func (m Model) currentTreeNodes() []fileTreeNode {
	if m.treeNodesReady && m.treeNodesScope == m.fileScope && m.treeNodesFileCount == len(m.repo.Files) && m.treeNodesPathCount == len(m.repo.AllPaths) {
		return m.treeNodes
	}
	return buildFileTreeView(m.repo.Files, m.repo.AllPaths, m.fileScope, m.collapsed)
}

func (m *Model) rebuildTreeNodes() {
	entries := scopedFileTreeEntries(m.repo.Files, m.repo.AllPaths, m.fileScope)
	m.treeNodes = buildFileTreeEntries(entries, m.collapsed)
	m.treeNodesReady = true
	m.treeNodesScope = m.fileScope
	m.treeNodesFileCount = len(m.repo.Files)
	m.treeNodesPathCount = len(m.repo.AllPaths)
	m.treeFileCount = len(entries)
	m.treeRequiredWidth = m.requiredTreeWidth(m.treeNodes)
}

func (m *Model) toggleFileLayout() {
	if m.fileLayout == flatFiles {
		m.fileLayout = treeFiles
		m.syncTreeCursorToFile()
		m.status = "File tree view. Enter toggles folders."
	} else {
		m.fileLayout = flatFiles
		m.status = "Flat file view."
	}
	m.fileScroll = 0
	m.ensureVisible()
	m.persistPreferences()
}

func (m *Model) cycleFileScope() {
	selectedPath := m.currentPath()
	m.fileScope = (m.fileScope + 1) % 3
	m.fileLayout = treeFiles
	m.rebuildTreeNodes()
	switch m.fileScope {
	case contextFiles:
		m.status = "Context tree: changed files and their unchanged siblings."
	case allRepositoryFiles:
		m.status = "All repository files shown. Unchanged directory chains are compacted."
	default:
		if m.sourcePath != "" {
			if _, changed := m.changedFileIndex(m.sourcePath); !changed {
				m.clearSourceView()
			}
		}
		m.status = "Changed files only."
	}
	m.fileScroll = 0
	if selectedPath != "" {
		m.syncTreeCursorToPath(selectedPath)
	} else {
		m.syncTreeCursorToFile()
	}
	m.ensureVisible()
	m.persistPreferences()
}

func (m *Model) toggleReviewedFile() {
	fileIndex := -1
	if m.fileLayout == treeFiles && m.focus == focusFiles {
		nodes := m.currentTreeNodes()
		if m.treeCursor >= 0 && m.treeCursor < len(nodes) && !nodes[m.treeCursor].directory {
			fileIndex = nodes[m.treeCursor].fileIndex
		}
	} else if m.sourcePath != "" {
		if index, ok := m.changedFileIndex(m.sourcePath); ok {
			fileIndex = index
		}
	} else if m.file >= 0 && m.file < len(m.repo.Files) {
		fileIndex = m.file
	}
	if fileIndex < 0 || fileIndex >= len(m.repo.Files) {
		m.status = "Only changed files can be marked reviewed."
		return
	}
	file := m.repo.Files[fileIndex]
	reviewed := m.session.ToggleReviewed(file.Path, fileReviewFingerprint(file))
	m.persist()
	if reviewed {
		m.status = "Marked " + file.Path + " as reviewed."
	} else {
		m.status = "Marked " + file.Path + " as unreviewed."
	}
}

func (m *Model) toggleFilePaneWidth() {
	if m.width < 90 {
		m.status = "The file pane already fills this terminal width."
		return
	}
	m.wideFiles = !m.wideFiles
	if m.wideFiles {
		m.status = "File pane expanded to fit visible paths."
	} else {
		m.status = "File pane restored to compact width."
	}
	m.persistPreferences()
}

func (m *Model) syncTreeCursorToFile() {
	path := m.currentPath()
	if path == "" && m.file >= 0 && m.file < len(m.repo.Files) {
		path = m.repo.Files[m.file].Path
	}
	m.syncTreeCursorToPath(path)
}

func (m *Model) syncTreeCursorToPath(path string) {
	collapsedChanged := false
	if path != "" {
		parts := strings.Split(path, "/")
		for i := 1; i < len(parts); i++ {
			parent := strings.Join(parts[:i], "/")
			if m.collapsed[parent] {
				delete(m.collapsed, parent)
				collapsedChanged = true
			}
		}
	}
	if collapsedChanged {
		m.rebuildTreeNodes()
	}
	for i, node := range m.currentTreeNodes() {
		if !node.directory && node.path == path {
			m.treeCursor = i
			return
		}
	}
	m.treeCursor = 0
}

func (m *Model) moveTreeCursor(delta int) {
	nodes := m.currentTreeNodes()
	if len(nodes) == 0 {
		return
	}
	m.treeCursor = clamp(m.treeCursor+delta, 0, len(nodes)-1)
	if node := nodes[m.treeCursor]; !node.directory {
		m.selectTreeFile(node)
	}
}

func (m *Model) selectTreeFile(node fileTreeNode) {
	if node.fileIndex >= 0 {
		m.clearSourceView()
		if node.fileIndex != m.file {
			m.file = node.fileIndex
			m.resetLineCursor()
		}
		return
	}
	m.openTreeSource(node.path)
}

func (m *Model) activateTreeNode() bool {
	nodes := m.currentTreeNodes()
	if m.treeCursor < 0 || m.treeCursor >= len(nodes) {
		return false
	}
	node := nodes[m.treeCursor]
	if !node.directory {
		m.selectTreeFile(node)
		return false
	}
	m.collapsed[node.path] = !m.collapsed[node.path]
	m.rebuildTreeNodes()
	nodes = m.currentTreeNodes()
	if len(nodes) > 0 {
		m.treeCursor = clamp(m.treeCursor, 0, len(nodes)-1)
	}
	m.ensureVisible()
	return true
}

func (m *Model) advanceIntoTreeNode() bool {
	nodes := m.currentTreeNodes()
	if m.treeCursor < 0 || m.treeCursor >= len(nodes) {
		return false
	}
	node := nodes[m.treeCursor]
	if !node.directory {
		m.selectTreeFile(node)
		return false
	}
	if m.collapsed[node.path] {
		m.collapsed[node.path] = false
		m.rebuildTreeNodes()
		m.ensureVisible()
		return true
	}
	if m.treeCursor+1 < len(nodes) && nodes[m.treeCursor+1].depth > node.depth {
		m.moveTreeCursor(1)
	}
	return true
}

func (m *Model) collapseTreeNodeOrSelectParent() {
	nodes := m.currentTreeNodes()
	if m.treeCursor < 0 || m.treeCursor >= len(nodes) {
		return
	}
	node := nodes[m.treeCursor]
	if node.directory && !m.collapsed[node.path] {
		m.collapsed[node.path] = true
		m.rebuildTreeNodes()
		m.ensureVisible()
		return
	}
	parentIndex, parentDepth := -1, -1
	for i, candidate := range nodes {
		if candidate.directory && candidate.depth < node.depth && strings.HasPrefix(node.path, candidate.path+"/") && candidate.depth > parentDepth {
			parentIndex, parentDepth = i, candidate.depth
		}
	}
	if parentIndex >= 0 {
		m.treeCursor = parentIndex
		m.ensureVisible()
	}
}

func (m *Model) resetLineCursor() {
	m.line, m.lineScroll = 0, 0
	m.splitCursor, m.splitScroll = 0, 0
	m.selectFrom = -1
}

func (m *Model) syncLineFromSplitCursor() {
	rows := m.currentSplitRows()
	if len(rows) == 0 {
		m.line = 0
		return
	}
	m.splitCursor = clamp(m.splitCursor, 0, len(rows)-1)
	m.line = rows[m.splitCursor].cursorIndex()
}

func (m *Model) syncSplitCursorToLine() {
	rows := m.currentSplitRows()
	for i, row := range rows {
		if row.containsLine(m.line) {
			m.splitCursor = i
			return
		}
	}
	m.splitCursor = 0
	m.syncLineFromSplitCursor()
}

func (m Model) currentPath() string {
	if m.sourcePath != "" {
		return m.sourcePath
	}
	if m.file < 0 || m.file >= len(m.repo.Files) {
		return ""
	}
	return m.repo.Files[m.file].Path
}

func (m *Model) updateSearch() {
	m.searchAt = 0
	m.searchTop = 0
	if m.input == "" {
		m.searchHits = make([]int, len(m.repo.Files))
		for i := range m.repo.Files {
			m.searchHits[i] = i
		}
		return
	}
	paths := make([]string, len(m.repo.Files))
	for i := range m.repo.Files {
		paths[i] = m.repo.Files[i].Path
	}
	matches := fuzzy.Find(m.input, paths)
	m.searchHits = m.searchHits[:0]
	for _, match := range matches {
		m.searchHits = append(m.searchHits, match.Index)
	}
}

func (m *Model) ensureFileSearchVisible() {
	const page = 10
	m.searchAt = clamp(m.searchAt, 0, max(0, len(m.searchHits)-1))
	if m.searchAt < m.searchTop {
		m.searchTop = m.searchAt
	}
	if m.searchAt >= m.searchTop+page {
		m.searchTop = m.searchAt - page + 1
	}
	m.searchTop = clamp(m.searchTop, 0, max(0, len(m.searchHits)-page))
}

func (m *Model) jumpHunk(direction int) {
	if m.file >= len(m.repo.Files) {
		return
	}
	hunks := m.repo.Files[m.file].Hunks
	if direction > 0 {
		for _, hunk := range hunks {
			if hunk.Start > m.line {
				m.line = hunk.Start
				m.syncSplitCursorToLine()
				m.ensureVisible()
				return
			}
		}
	} else {
		for i := len(hunks) - 1; i >= 0; i-- {
			if hunks[i].Start < m.line {
				m.line = hunks[i].Start
				m.syncSplitCursorToLine()
				m.ensureVisible()
				return
			}
		}
	}
}

func (m *Model) persist() {
	if err := m.reviews.Save(m.repo.ReviewPath, m.session); err != nil {
		m.status = "Save failed: " + err.Error()
	}
}

func (m *Model) applyPreferences(preferences config.Preferences) {
	if preferences.FileLayout == "tree" {
		m.fileLayout = treeFiles
	}
	switch preferences.FileScope {
	case "context":
		m.fileScope = contextFiles
	case "all":
		m.fileScope = allRepositoryFiles
	}
	m.wideFiles = preferences.WideFiles
	if preferences.DiffView == "split" {
		m.view = split
	}
}

func (m *Model) persistPreferences() {
	preferences := config.Preferences{
		FileLayout: "flat",
		FileScope:  "changed",
		WideFiles:  m.wideFiles,
		DiffView:   "unified",
	}
	if m.fileLayout == treeFiles {
		preferences.FileLayout = "tree"
	}
	switch m.fileScope {
	case contextFiles:
		preferences.FileScope = "context"
	case allRepositoryFiles:
		preferences.FileScope = "all"
	}
	if m.view == split {
		preferences.DiffView = "split"
	}
	if err := m.preferences.Save(m.preferencesPath, preferences); err != nil {
		m.status = "Save view preferences failed: " + err.Error()
	}
}

func (m Model) watchCmd() tea.Cmd {
	if m.watcher == nil {
		return nil
	}
	events := m.watcher.Events()
	return func() tea.Msg {
		event, ok := <-events
		return repositoryWatchMsg{event: event, closed: !ok}
	}
}

func (m *Model) stopWatcher() {
	if m.refreshCancel != nil {
		m.refreshCancel()
		m.refreshCancel = nil
	}
	if m.searchCancel != nil {
		m.searchCancel()
		m.searchCancel = nil
	}
	if m.watcher != nil {
		_ = m.watcher.Close()
		m.watcher = nil
	}
}

func (m *Model) applyRefresh(repo *gitrepo.Repository, automatic bool) {
	oldPath := m.currentPath()
	oldLine := m.line
	oldSourceLine := m.sourceLine
	hadSource := m.sourcePath != ""
	m.repo = repo
	m.rebuildTreeNodes()

	if hadSource {
		if err := m.loadSource(oldPath, oldSourceLine); err != nil {
			m.clearSourceView()
			hadSource = false
		}
	}
	if index, ok := m.changedFileIndex(oldPath); ok {
		m.file = index
	} else {
		m.file = clamp(m.file, 0, max(0, len(m.repo.Files)-1))
		if m.fileScope != changedFiles && !hadSource && pathInRepository(oldPath, m.repo.AllPaths) {
			if err := m.loadSource(oldPath, oldSourceLine); err == nil {
				hadSource = true
			}
		}
	}
	if !hadSource {
		m.line = clamp(oldLine, 0, max(0, len(m.currentLines())-1))
		m.lineScroll = min(m.lineScroll, m.line)
		m.syncSplitCursorToLine()
	}
	m.selectFrom = -1
	m.syncTreeCursorToFile()
	m.ensureVisible()
	if automatic {
		m.status = "Updated automatically after repository changes."
	} else {
		m.status = "Diff refreshed."
	}
}

func pathInRepository(path string, paths []string) bool {
	for _, candidate := range paths {
		if candidate == path {
			return true
		}
	}
	return false
}

func (m *Model) refreshCmd(automatic bool) tea.Cmd {
	if m.refreshCancel != nil {
		m.refreshCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.refreshCancel = cancel
	m.refreshID++
	id := m.refreshID
	root, base := m.repo.Root, m.repo.Base
	repositories := m.repositories
	return func() tea.Msg {
		repo, err := repositories.Refresh(ctx, root, base)
		return refreshMsg{repo: repo, err: err, automatic: automatic, id: id}
	}
}

func clamp(value, low, high int) int { return min(max(value, low), high) }

func shortPath(path string, width int) string {
	if width <= 1 {
		return ""
	}
	if len([]rune(path)) <= width {
		return path
	}
	base := filepath.Base(path)
	if len([]rune(base))+2 >= width {
		r := []rune(base)
		return "…" + string(r[max(0, len(r)-width+1):])
	}
	remain := width - len([]rune(base)) - 2
	dir := []rune(filepath.Dir(path))
	return string(dir[:min(len(dir), remain)]) + "…/" + base
}
