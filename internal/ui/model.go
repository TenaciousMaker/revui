package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/sahilm/fuzzy"

	"github.com/mattwalker/revui/internal/agent"
	"github.com/mattwalker/revui/internal/diff"
	"github.com/mattwalker/revui/internal/gitrepo"
	"github.com/mattwalker/revui/internal/review"
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
	commenting
	showHelp
	previewAgent
	runningAgent
	showAgentResult
)

type agentResultMsg struct {
	output string
	err    error
}

type refreshMsg struct {
	repo *gitrepo.Repository
	err  error
}

type Model struct {
	repo        *gitrepo.Repository
	session     review.Session
	theme       theme
	highlight   *highlighter
	width       int
	height      int
	focus       focusArea
	view        viewMode
	mode        mode
	file        int
	fileScroll  int
	line        int
	lineScroll  int
	selectFrom  int
	input       string
	inputTitle  string
	inputAnchor review.Anchor
	editingID   string
	searchHits  []int
	searchAt    int
	prompt      string
	agentOutput string
	status      string
}

func New(repo *gitrepo.Repository) (Model, error) {
	session, err := review.Load(repo.ReviewPath, repo.Branch, repo.Base)
	if err != nil {
		return Model{}, err
	}
	m := Model{
		repo: repo, session: session, theme: newTheme(), highlight: &highlighter{},
		focus: focusFiles, view: unified, selectFrom: -1,
	}
	if len(repo.Files) == 0 {
		m.status = "No changes yet. revui will refresh when you press R."
	} else {
		m.status = "Ready to review. Press ? for keys."
	}
	return m, nil
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.ensureVisible()
		return m, nil
	case agentResultMsg:
		m.agentOutput = msg.output
		if msg.err != nil {
			m.agentOutput = fmt.Sprintf("Agent failed: %v\n\n%s", msg.err, msg.output)
		}
		m.mode = showAgentResult
		return m, m.refreshCmd()
	case refreshMsg:
		if msg.err != nil {
			m.status = "Refresh failed: " + msg.err.Error()
			return m, nil
		}
		oldPath := m.currentPath()
		m.repo = msg.repo
		m.file = m.fileIndex(oldPath)
		m.line, m.lineScroll = 0, 0
		if m.mode == runningAgent {
			m.mode = showAgentResult
		}
		m.status = "Diff refreshed. Review agent changes before resolving comments."
		return m, nil
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.mode == searching {
		return m.handleSearch(msg)
	}
	if m.mode == commenting {
		return m.handleCommentEditor(msg)
	}
	if m.mode == showHelp {
		if msg.String() == "esc" || msg.String() == "?" || msg.String() == "q" {
			m.mode = normal
		}
		return m, nil
	}
	if m.mode == previewAgent {
		switch msg.String() {
		case "esc":
			m.mode = normal
		case "enter":
			m.mode = runningAgent
			m.status = "Agent is addressing unresolved comments…"
			return m, m.runAgentCmd()
		}
		return m, nil
	}
	if m.mode == runningAgent {
		return m, nil
	}
	if m.mode == showAgentResult {
		if msg.String() == "esc" || msg.String() == "enter" || msg.String() == "q" {
			m.mode = normal
		}
		return m, nil
	}

	key := msg.String()
	switch key {
	case "ctrl+c", "q":
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
		m.focus = focusFiles
	case "right", "l", "enter":
		if m.focus == focusFiles && len(m.repo.Files) > 0 {
			m.focus = focusDiff
			m.line, m.lineScroll, m.selectFrom = 0, 0, -1
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
			m.file = 0
		} else {
			m.line = 0
		}
		m.ensureVisible()
	case "end", "G", "shift+g":
		if m.focus == focusFiles {
			m.file = max(0, len(m.repo.Files)-1)
		} else {
			m.line = max(0, len(m.currentLines())-1)
		}
		m.ensureVisible()
	case "/":
		m.mode, m.input, m.searchAt = searching, "", 0
		m.updateSearch()
	case "v":
		if m.focus == focusDiff && len(m.currentLines()) > 0 {
			if m.selectFrom >= 0 {
				m.selectFrom = -1
				m.status = "Range selection cleared."
			} else {
				m.selectFrom = m.line
				m.status = "Range started. Move, then press c to comment."
			}
		}
	case "c":
		if m.focus == focusDiff && len(m.currentLines()) > 0 {
			m.beginComment(m.anchorForSelection(), "Add review comment", "", "")
		}
	case "shift+c", "C":
		m.beginComment(review.Anchor{WholeRepo: true}, "Comment on the whole review", "", "")
	case "e":
		if comment := m.commentAtCursor(); comment != nil {
			m.beginComment(comment.Anchor, "Edit review comment", comment.Body, comment.ID)
		}
	case "r":
		if comment := m.commentAtCursor(); comment != nil {
			comment.Resolved = !comment.Resolved
			m.session.Upsert(*comment)
			m.persist()
			if comment.Resolved {
				m.status = "Comment resolved."
			} else {
				m.status = "Comment reopened."
			}
		}
	case "d":
		if comment := m.commentAtCursor(); comment != nil {
			m.session.Delete(comment.ID)
			m.persist()
			m.status = "Comment deleted."
		}
	case "a":
		unresolved := m.session.Unresolved()
		if len(unresolved) == 0 {
			m.status = "Add an unresolved comment before running the agent."
		} else {
			m.prompt = agent.Prompt(m.repo.Branch, m.repo.Base, unresolved)
			m.mode = previewAgent
		}
	case "s":
		if m.view == unified {
			m.view = split
			m.status = "Split diff view."
		} else {
			m.view = unified
			m.status = "Unified diff view."
		}
	case "]":
		m.jumpHunk(1)
	case "[":
		m.jumpHunk(-1)
	case "n":
		m.jumpComment(1)
	case "p":
		m.jumpComment(-1)
	case "R", "shift+r", "ctrl+r":
		m.status = "Refreshing diff…"
		return m, m.refreshCmd()
	}
	return m, nil
}

func (m Model) handleSearch(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = normal
	case "enter":
		if len(m.searchHits) > 0 {
			m.file = m.searchHits[m.searchAt]
			m.line, m.lineScroll, m.selectFrom = 0, 0, -1
			m.focus, m.mode = focusDiff, normal
			m.ensureVisible()
		}
	case "up", "ctrl+p":
		m.searchAt = max(0, m.searchAt-1)
	case "down", "ctrl+n":
		m.searchAt = min(max(0, len(m.searchHits)-1), m.searchAt+1)
	case "backspace":
		m.input = trimLastRune(m.input)
		m.updateSearch()
	default:
		if text := msg.Key().Text; text != "" {
			m.input += text
			m.updateSearch()
		}
	}
	return m, nil
}

func (m Model) handleCommentEditor(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode, m.input, m.editingID = normal, "", ""
		m.status = "Comment cancelled."
	case "ctrl+s":
		body := strings.TrimSpace(m.input)
		if body == "" {
			m.status = "A review comment cannot be empty."
			return m, nil
		}
		if m.editingID != "" {
			for i := range m.session.Comments {
				if m.session.Comments[i].ID == m.editingID {
					m.session.Comments[i].Body = body
					m.session.Upsert(m.session.Comments[i])
					break
				}
			}
		} else {
			m.session.Upsert(review.NewComment(body, m.inputAnchor))
		}
		m.persist()
		m.mode, m.input, m.editingID, m.selectFrom = normal, "", "", -1
		m.status = "Review comment saved."
	case "enter":
		m.input += "\n"
	case "backspace":
		m.input = trimLastRune(m.input)
	case "ctrl+w":
		m.input = strings.TrimRight(m.input, " \n\t")
		if at := strings.LastIndexAny(m.input, " \n\t"); at >= 0 {
			m.input = m.input[:at+1]
		} else {
			m.input = ""
		}
	default:
		if text := msg.Key().Text; text != "" {
			m.input += text
		}
	}
	return m, nil
}

func (m *Model) beginComment(anchor review.Anchor, title, body, id string) {
	m.mode, m.inputTitle, m.inputAnchor, m.input, m.editingID = commenting, title, anchor, body, id
}

func (m *Model) move(delta int) {
	if m.focus == focusFiles {
		m.file = clamp(m.file+delta, 0, max(0, len(m.repo.Files)-1))
		m.line, m.lineScroll, m.selectFrom = 0, 0, -1
	} else {
		m.line = clamp(m.line+delta, 0, max(0, len(m.currentLines())-1))
	}
	m.ensureVisible()
}

func (m *Model) ensureVisible() {
	page := m.pageSize()
	if m.focus == focusFiles {
		if m.file < m.fileScroll {
			m.fileScroll = m.file
		}
		if m.file >= m.fileScroll+page {
			m.fileScroll = m.file - page + 1
		}
	} else {
		if m.line < m.lineScroll {
			m.lineScroll = m.line
		}
		if m.line >= m.lineScroll+page {
			m.lineScroll = m.line - page + 1
		}
	}
}

func (m Model) pageSize() int { return max(4, m.height-8) }

func (m Model) currentLines() []diff.Line {
	if m.file < 0 || m.file >= len(m.repo.Files) {
		return nil
	}
	return m.repo.Files[m.file].Lines
}

func (m Model) currentPath() string {
	if m.file < 0 || m.file >= len(m.repo.Files) {
		return ""
	}
	return m.repo.Files[m.file].Path
}

func (m Model) fileIndex(path string) int {
	for i := range m.repo.Files {
		if m.repo.Files[i].Path == path {
			return i
		}
	}
	return 0
}

func (m *Model) updateSearch() {
	m.searchAt = 0
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

func (m *Model) jumpHunk(direction int) {
	if m.file >= len(m.repo.Files) {
		return
	}
	hunks := m.repo.Files[m.file].Hunks
	if direction > 0 {
		for _, hunk := range hunks {
			if hunk.Start > m.line {
				m.line = hunk.Start
				m.ensureVisible()
				return
			}
		}
	} else {
		for i := len(hunks) - 1; i >= 0; i-- {
			if hunks[i].Start < m.line {
				m.line = hunks[i].Start
				m.ensureVisible()
				return
			}
		}
	}
}

func (m *Model) jumpComment(direction int) {
	type location struct{ file, line int }
	var locations []location
	for _, comment := range m.session.Comments {
		if comment.Anchor.WholeRepo {
			continue
		}
		fi := m.fileIndex(comment.Anchor.Path)
		if fi >= len(m.repo.Files) || m.repo.Files[fi].Path != comment.Anchor.Path {
			continue
		}
		for li, line := range m.repo.Files[fi].Lines {
			if anchorContains(comment.Anchor, line) {
				locations = append(locations, location{fi, li})
				break
			}
		}
	}
	sort.Slice(locations, func(i, j int) bool {
		if locations[i].file != locations[j].file {
			return locations[i].file < locations[j].file
		}
		return locations[i].line < locations[j].line
	})
	if direction > 0 {
		for _, loc := range locations {
			if loc.file > m.file || (loc.file == m.file && loc.line > m.line) {
				m.file, m.line = loc.file, loc.line
				m.focus = focusDiff
				m.ensureVisible()
				return
			}
		}
		if len(locations) > 0 {
			m.file, m.line = locations[0].file, locations[0].line
			m.ensureVisible()
		}
	} else {
		for i := len(locations) - 1; i >= 0; i-- {
			loc := locations[i]
			if loc.file < m.file || (loc.file == m.file && loc.line < m.line) {
				m.file, m.line = loc.file, loc.line
				m.focus = focusDiff
				m.ensureVisible()
				return
			}
		}
		if len(locations) > 0 {
			loc := locations[len(locations)-1]
			m.file, m.line = loc.file, loc.line
			m.ensureVisible()
		}
	}
}

func (m Model) anchorForSelection() review.Anchor {
	start, end := m.line, m.line
	if m.selectFrom >= 0 {
		start, end = min(m.selectFrom, m.line), max(m.selectFrom, m.line)
	}
	return m.anchorForRange(start, end)
}

func (m Model) anchorForRange(start, end int) review.Anchor {
	lines := m.currentLines()
	anchor := review.Anchor{Path: m.currentPath()}
	var context []string
	for i := start; i <= end && i < len(lines); i++ {
		line := lines[i]
		if line.OldNumber > 0 {
			if anchor.OldStart == 0 {
				anchor.OldStart = line.OldNumber
			}
			anchor.OldEnd = line.OldNumber
		}
		if line.NewNumber > 0 {
			if anchor.NewStart == 0 {
				anchor.NewStart = line.NewNumber
			}
			anchor.NewEnd = line.NewNumber
		}
		if line.Kind != diff.Meta && len(context) < 3 {
			context = append(context, strings.TrimSpace(line.Text))
		}
	}
	if anchor.OldStart == 0 && anchor.NewStart == 0 {
		for i := end + 1; i < len(lines); i++ {
			if lines[i].Kind != diff.Meta {
				return m.anchorForRange(i, i)
			}
		}
		for i := start - 1; i >= 0; i-- {
			if lines[i].Kind != diff.Meta {
				return m.anchorForRange(i, i)
			}
		}
	}
	anchor.Context = strings.Join(context, " | ")
	return anchor
}

func (m Model) commentAtCursor() *review.Comment {
	if m.focus != focusDiff || m.file >= len(m.repo.Files) || m.line >= len(m.currentLines()) {
		return nil
	}
	line := m.currentLines()[m.line]
	for i := range m.session.Comments {
		if m.session.Comments[i].Anchor.Path == m.currentPath() && anchorContains(m.session.Comments[i].Anchor, line) {
			c := m.session.Comments[i]
			return &c
		}
	}
	return nil
}

func anchorContains(anchor review.Anchor, line diff.Line) bool {
	if line.NewNumber > 0 && anchor.NewStart > 0 && line.NewNumber >= anchor.NewStart && line.NewNumber <= max(anchor.NewStart, anchor.NewEnd) {
		return true
	}
	return line.OldNumber > 0 && anchor.OldStart > 0 && line.OldNumber >= anchor.OldStart && line.OldNumber <= max(anchor.OldStart, anchor.OldEnd)
}

func (m *Model) persist() {
	if err := review.Save(m.repo.ReviewPath, m.session); err != nil {
		m.status = "Save failed: " + err.Error()
	}
}

func (m Model) runAgentCmd() tea.Cmd {
	root, command, prompt := m.repo.Root, agent.Command(), m.prompt
	return func() tea.Msg {
		output, err := agent.Run(context.Background(), root, command, prompt)
		return agentResultMsg{output: output, err: err}
	}
}

func (m Model) refreshCmd() tea.Cmd {
	root, base := m.repo.Root, m.repo.Base
	return func() tea.Msg { repo, err := gitrepo.Open(root, base); return refreshMsg{repo: repo, err: err} }
}

func trimLastRune(value string) string {
	if value == "" {
		return value
	}
	_, size := utf8.DecodeLastRuneInString(value)
	return value[:len(value)-size]
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
