package ui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/TenaciousMaker/revui/internal/gitrepo"
)

type repositorySearchMsg struct {
	query   string
	matches []gitrepo.SearchMatch
	err     error
	id      uint64
}

func (m *Model) beginRepositorySearch(seed string) tea.Cmd {
	if m.searchCancel != nil {
		m.searchCancel()
		m.searchCancel = nil
	}
	seed = searchSeed(seed)
	m.mode = searchingRepository
	m.focus = focusDiff
	m.setInput(seed)
	m.repoHits = nil
	m.repoSearchAt = 0
	m.repoSearchTop = 0
	m.repoSearching = false
	m.repoSearchReady = false
	m.mouseSelecting = false
	m.mouseSelectMoved = false
	m.selectedText = ""
	if seed == "" {
		m.status = "Type text, then press Enter to search the repository."
		return nil
	}
	m.repoSearching = true
	m.status = fmt.Sprintf("Searching the repository for %q…", seed)
	return m.repositorySearchCmd(seed)
}

func searchSeed(selection string) string {
	for _, line := range strings.Split(selection, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			runes := []rune(line)
			if len(runes) > 200 {
				line = string(runes[:200])
			}
			return line
		}
	}
	return ""
}

func (m *Model) repositorySearchCmd(query string) tea.Cmd {
	if m.searchCancel != nil {
		m.searchCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.searchCancel = cancel
	m.searchID++
	id := m.searchID
	repo := m.repo
	repositories := m.repositories
	return func() tea.Msg {
		matches, err := repositories.Search(ctx, repo, query, 2)
		return repositorySearchMsg{query: query, matches: matches, err: err, id: id}
	}
}

func (m Model) handleRepositorySearch(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if handled, changed := m.editInput(msg); handled {
		if changed {
			m.resetRepositoryResults()
		}
		m.ensureRepositorySearchVisible()
		return m, nil
	}
	switch msg.String() {
	case "esc":
		if m.searchCancel != nil {
			m.searchCancel()
			m.searchCancel = nil
		}
		m.searchID++
		m.mode = normal
		m.repoSearching = false
	case "enter":
		if m.repoSearching {
			return m, nil
		}
		if m.repoSearchReady && len(m.repoHits) > 0 {
			m.openSearchMatch(m.repoHits[m.repoSearchAt])
			return m, nil
		}
		query := strings.TrimSpace(m.input)
		if query == "" {
			m.status = "Enter text to search across the repository."
			return m, nil
		}
		m.repoSearching = true
		m.repoSearchReady = false
		m.status = fmt.Sprintf("Searching the repository for %q…", query)
		return m, m.repositorySearchCmd(query)
	case "up", "ctrl+p":
		m.repoSearchAt = max(0, m.repoSearchAt-1)
	case "down", "ctrl+n":
		m.repoSearchAt = min(max(0, len(m.repoHits)-1), m.repoSearchAt+1)
	case "pgup":
		m.repoSearchAt = max(0, m.repoSearchAt-5)
	case "pgdown":
		m.repoSearchAt = min(max(0, len(m.repoHits)-1), m.repoSearchAt+5)
	}
	m.ensureRepositorySearchVisible()
	return m, nil
}

func (m *Model) resetRepositoryResults() {
	if m.searchCancel != nil {
		m.searchCancel()
		m.searchCancel = nil
	}
	m.searchID++
	m.repoHits = nil
	m.repoSearchAt = 0
	m.repoSearchTop = 0
	m.repoSearching = false
	m.repoSearchReady = false
}

func (m *Model) openSearchMatch(match gitrepo.SearchMatch) {
	if err := m.loadSource(match.Path, match.Line-1); err != nil {
		m.status = "Open search result failed: " + err.Error()
		return
	}
	m.focus = focusDiff
	m.mode = normal
	if index, ok := m.changedFileIndex(match.Path); ok {
		m.file = index
		m.resetLineCursor()
		m.syncTreeCursorToFile()
	}
	m.status = fmt.Sprintf("Source %s:%d. Press d for its diff or Esc to return.", match.Path, match.Line)
	m.ensureVisible()
}

func (m *Model) openTreeSource(path string) {
	if path == m.sourcePath {
		return
	}
	if err := m.loadSource(path, 0); err != nil {
		m.status = "Open repository file failed: " + err.Error()
		return
	}
	m.status = fmt.Sprintf("Unchanged source %s. Press d to check for a nearby diff.", path)
}

func (m *Model) openCurrentFileSource() {
	if m.file < 0 || m.file >= len(m.repo.Files) {
		m.status = "Select a changed file before opening its full source."
		return
	}
	file := m.repo.Files[m.file]
	line := 0
	if m.line >= 0 && m.line < len(file.Lines) {
		line = file.Lines[m.line].NewNumber
		if line == 0 {
			line = file.Lines[m.line].OldNumber
		}
	}
	if err := m.loadSource(file.Path, max(0, line-1)); err != nil {
		m.status = "Open full file failed: " + err.Error()
		return
	}
	m.focus = focusDiff
	if m.sourceFromBase {
		m.status = "Full base source for deleted file " + file.Path + ". Press o to return to the diff."
	} else {
		m.status = "Full source for " + file.Path + ". Press o to return to the diff."
	}
	m.ensureVisible()
}

func (m *Model) loadSource(path string, line int) error {
	content, fromBase, err := m.repositories.ReadSource(context.Background(), m.repo, path)
	if err != nil {
		return err
	}
	content = []byte(strings.ReplaceAll(string(content), "\r\n", "\n"))
	m.sourcePath = path
	m.sourceLines = strings.Split(string(content), "\n")
	m.sourceLine = clamp(line, 0, max(0, len(m.sourceLines)-1))
	m.sourceScroll = max(0, m.sourceLine-m.pageSize()/3)
	m.sourceFromBase = fromBase
	return nil
}

func (m Model) handleSourceKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() != "f" && msg.String() != "y" {
		m.clearMouseSelection()
	}
	switch msg.String() {
	case "ctrl+c", "q":
		m.stopWatcher()
		return m, tea.Quit
	case "esc":
		m.closeSourceView("Returned to the branch diff.")
	case "f":
		return m, m.beginRepositorySearch(m.selectedText)
	case "y":
		return m, m.copySelectionCmd()
	case "o":
		m.closeSourceView("Returned to the branch diff.")
	case "A", "shift+a":
		m.cycleFileScope()
	case " ", "space":
		m.toggleReviewedFile()
	case "d", "enter":
		m.jumpSourceToDiff()
	case "up", "k":
		m.sourceLine = clamp(m.sourceLine-1, 0, max(0, len(m.sourceLines)-1))
		m.ensureVisible()
	case "down", "j":
		m.sourceLine = clamp(m.sourceLine+1, 0, max(0, len(m.sourceLines)-1))
		m.ensureVisible()
	case "pgup", "ctrl+u":
		m.sourceLine = clamp(m.sourceLine-m.pageSize(), 0, max(0, len(m.sourceLines)-1))
		m.ensureVisible()
	case "pgdown", "ctrl+d":
		m.sourceLine = clamp(m.sourceLine+m.pageSize(), 0, max(0, len(m.sourceLines)-1))
		m.ensureVisible()
	case "home", "g":
		m.sourceLine = 0
		m.ensureVisible()
	case "end", "G", "shift+g":
		m.sourceLine = max(0, len(m.sourceLines)-1)
		m.ensureVisible()
	}
	return m, nil
}

func (m *Model) closeSourceView(status string) {
	m.clearSourceView()
	m.status = status
	m.ensureVisible()
}

func (m *Model) clearSourceView() {
	m.sourcePath = ""
	m.sourceLines = nil
	m.sourceLine = 0
	m.sourceScroll = 0
	m.sourceFromBase = false
}

func (m *Model) jumpSourceToDiff() {
	fileIndex, ok := m.changedFileIndex(m.sourcePath)
	if !ok {
		m.status = "This source file is unchanged, so it has no branch diff."
		return
	}
	target := m.sourceLine + 1
	closest, distance := 0, int(^uint(0)>>1)
	for i, line := range m.repo.Files[fileIndex].Lines {
		number := line.NewNumber
		if number == 0 {
			number = line.OldNumber
		}
		if number == 0 {
			continue
		}
		delta := number - target
		if delta < 0 {
			delta = -delta
		}
		if delta < distance {
			closest, distance = i, delta
		}
	}
	m.file = fileIndex
	m.closeSourceView(fmt.Sprintf("Diff location nearest source line %d.", target))
	m.line = closest
	m.focus = focusDiff
	m.syncTreeCursorToFile()
	m.syncSplitCursorToLine()
	m.ensureVisible()
}

func (m Model) changedFileIndex(path string) (int, bool) {
	for index, file := range m.repo.Files {
		if file.Path == path {
			return index, true
		}
	}
	return 0, false
}

func (m Model) renderRepositorySearch(width, height int) string {
	input := m.theme.text.Render("› " + m.inputWithCursor())
	lines := []string{
		m.theme.focus.Render("FIND IN REPOSITORY"),
		m.theme.muted.Render("Tracked and untracked files · literal text"),
		"",
		input,
		m.theme.border.Render(strings.Repeat("─", max(1, width-4))),
	}

	switch {
	case m.repoSearching:
		lines = append(lines, "", m.theme.focus.Render("◉ Searching…"))
	case !m.repoSearchReady:
		lines = append(lines, "", m.theme.muted.Render("Type a method, symbol, or phrase and press Enter."))
	case len(m.repoHits) == 0:
		lines = append(lines, "", m.theme.muted.Render("No matching repository text."))
	default:
		lines = append(lines, "", m.theme.muted.Render(fmt.Sprintf("%d matches · selected %d", len(m.repoHits), m.repoSearchAt+1)))
		for _, i := range m.repositorySearchWindow(height) {
			chunk := m.renderSearchChunk(m.repoHits[i], i == m.repoSearchAt, max(12, width-4))
			lines = append(lines, "", chunk)
		}
	}
	lines = append(lines, "", m.theme.muted.Render("←→ edit   ctrl+u clear   ↑↓ select   enter open source   esc close"))
	return m.theme.canvas.Width(width).Height(height).Padding(1, 1).Render(strings.Join(lines, "\n"))
}

func (m Model) repositorySearchWindow(height int) []int {
	if len(m.repoHits) == 0 {
		return nil
	}
	start := clamp(m.repoSearchTop, 0, len(m.repoHits)-1)
	budget := max(1, height-11)
	used := 0
	var indices []int
	for index := start; index < len(m.repoHits); index++ {
		cost := len(m.repoHits[index].Context) + 2 // blank separator, header, and context rows
		if len(indices) > 0 && used+cost > budget {
			break
		}
		indices = append(indices, index)
		used += cost
	}
	return indices
}

func (m *Model) ensureRepositorySearchVisible() {
	if len(m.repoHits) == 0 {
		m.repoSearchTop = 0
		return
	}
	m.repoSearchAt = clamp(m.repoSearchAt, 0, len(m.repoHits)-1)
	m.repoSearchTop = clamp(m.repoSearchTop, 0, m.repoSearchAt)
	if m.repoSearchAt < m.repoSearchTop {
		m.repoSearchTop = m.repoSearchAt
	}
	height := max(3, m.height-4)
	for m.repoSearchTop < m.repoSearchAt {
		visible := m.repositorySearchWindow(height)
		if len(visible) > 0 && visible[len(visible)-1] >= m.repoSearchAt {
			return
		}
		m.repoSearchTop++
	}
}

func (m Model) renderSearchChunk(match gitrepo.SearchMatch, selected bool, width int) string {
	headerStyle := m.theme.muted
	if selected {
		headerStyle = m.theme.focus
	}
	rows := []string{headerStyle.Render(fmt.Sprintf("%s  %s:%d", selectionGlyph(selected), match.Path, match.Line))}
	for _, context := range match.Context {
		marker := " "
		background := "#161B22"
		style := m.theme.panel
		if selected {
			background = "#1F2630"
			style = m.theme.raised
		}
		if context.Match {
			marker = "›"
			background = "#172B4D"
			style = m.theme.hunk
		}
		prefix := fmt.Sprintf("%s %4d │ ", marker, context.Number)
		codeWidth := max(1, width-lipgloss.Width(prefix))
		code := xansi.Truncate(expandTabs(context.Text), codeWidth, "…")
		code = m.highlightLine(match.Path, code, background)
		rows = append(rows, style.Width(width).MaxWidth(width).Render(prefix+code))
	}
	return strings.Join(rows, "\n")
}

func selectionGlyph(selected bool) string {
	if selected {
		return "●"
	}
	return "○"
}

func (m Model) renderSource(width, height int) string {
	if len(m.sourceLines) == 0 {
		return m.theme.muted.Render("Source file is empty.")
	}
	lineDigits := len(fmt.Sprintf("%d", len(m.sourceLines)))
	var rows []string
	start := clamp(m.sourceScroll, 0, max(0, len(m.sourceLines)-1))
	for index := start; index < len(m.sourceLines) && len(rows) < height; index++ {
		selected := index == m.sourceLine
		marker := " "
		background := "#0D1117"
		style := m.theme.canvas
		if selected {
			marker = "›"
			background = "#172B4D"
			style = m.theme.hunk
		}
		prefix := fmt.Sprintf("%s %*d │ ", marker, lineDigits, index+1)
		codeWidth := max(1, width-lipgloss.Width(prefix))
		code := xansi.Truncate(expandTabs(m.sourceLines[index]), codeWidth, "…")
		code = m.highlightLine(m.sourcePath, code, background)
		rows = append(rows, style.Width(width).MaxWidth(width).Render(prefix+code))
	}
	return strings.Join(rows, "\n")
}
