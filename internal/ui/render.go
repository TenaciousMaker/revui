package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mattwalker/revui/internal/diff"
	"github.com/mattwalker/revui/internal/review"
)

func (m Model) View() tea.View {
	if m.width == 0 || m.height == 0 {
		view := tea.NewView("Starting revui…")
		view.AltScreen = true
		return view
	}
	header := m.renderHeader()
	bodyHeight := max(3, m.height-4)
	var body string
	if m.width < 90 {
		if m.focus == focusFiles {
			body = m.renderFiles(m.width, bodyHeight)
		} else {
			body = m.renderDiff(m.width, bodyHeight)
		}
	} else {
		leftWidth := clamp(m.width*30/100, 26, 42)
		rightWidth := max(20, m.width-leftWidth)
		body = lipgloss.JoinHorizontal(lipgloss.Top, m.renderFiles(leftWidth, bodyHeight), m.renderDiff(rightWidth, bodyHeight))
	}
	content := lipgloss.JoinVertical(lipgloss.Left, header, body, m.renderFooter())
	content = m.theme.canvas.Width(m.width).Height(m.height).Render(content)
	switch m.mode {
	case searching:
		content = m.overlay(content, m.renderSearch(), min(72, m.width-4), min(18, m.height-4))
	case commenting:
		content = m.overlay(content, m.renderCommentEditor(), min(78, m.width-4), min(18, m.height-4))
	case showHelp:
		content = m.overlay(content, m.renderHelp(), min(82, m.width-4), min(28, m.height-4))
	case previewAgent:
		content = m.overlay(content, m.renderPrompt(), min(90, m.width-4), min(30, m.height-4))
	case runningAgent:
		content = m.overlay(content, m.theme.comment.Render("◉  Agent working\n\n")+m.theme.text.Render("revui will refresh the diff when it finishes."), min(58, m.width-4), 8)
	case showAgentResult:
		content = m.overlay(content, m.renderAgentResult(), min(90, m.width-4), min(30, m.height-4))
	}
	view := tea.NewView(content)
	view.AltScreen = true
	view.WindowTitle = "revui — pre-PR review"
	return view
}

func (m Model) renderHeader() string {
	adds, dels := m.repo.Totals()
	unresolved := len(m.session.Unresolved())
	logo := m.theme.focus.Render(" REVUI ")
	branch := m.theme.text.Bold(true).Render(m.repo.Branch) + m.theme.muted.Render("  →  "+m.repo.Base)
	stats := m.theme.addedText.Render(fmt.Sprintf("+%d", adds)) + "  " + m.theme.deletedText.Render(fmt.Sprintf("-%d", dels)) + "  " + m.theme.comment.Render(fmt.Sprintf("● %d open", unresolved))
	gap := max(1, m.width-lipgloss.Width(logo)-lipgloss.Width(branch)-lipgloss.Width(stats)-4)
	line := logo + "  " + branch + strings.Repeat(" ", gap) + stats
	return m.theme.panel.Width(m.width).Height(2).Padding(0, 1).Render(line)
}

func (m Model) renderFiles(width, height int) string {
	focused := m.focus == focusFiles
	title := m.theme.muted.Render(fmt.Sprintf("CHANGED FILES  %d", len(m.repo.Files)))
	if focused {
		title = m.theme.focus.Render(fmt.Sprintf("CHANGED FILES  %d", len(m.repo.Files)))
	}
	lines := []string{title, ""}
	if len(m.repo.Files) == 0 {
		lines = append(lines, m.theme.muted.Width(max(1, width-4)).Render("Nothing changed against "+m.repo.Base+".\n\nMake a change, then press R to refresh."))
	}
	available := max(1, height-3)
	start := clamp(m.fileScroll, 0, max(0, len(m.repo.Files)-1))
	end := min(len(m.repo.Files), start+available)
	for i := start; i < end; i++ {
		file := m.repo.Files[i]
		marker := statusMarker(file.Status)
		counts := m.fileCommentCount(file.Path)
		badge := ""
		if counts > 0 {
			badge = m.theme.comment.Render(fmt.Sprintf(" ●%d", counts))
		}
		countText := fmt.Sprintf(" %s%d %s%d", m.theme.addedText.Render("+"), file.Additions, m.theme.deletedText.Render("-"), file.Deletions)
		nameWidth := max(6, width-lipgloss.Width(countText)-lipgloss.Width(badge)-7)
		row := " " + marker + " " + shortPath(file.Path, nameWidth) + badge
		gap := max(1, width-4-lipgloss.Width(row)-lipgloss.Width(countText))
		row += strings.Repeat(" ", gap) + countText
		if i == m.file {
			if focused {
				row = m.theme.raised.Foreground(lipgloss.Color("#FFFFFF")).Bold(true).Width(max(1, width-2)).Render("›" + row[1:])
			} else {
				row = m.theme.panel.Bold(true).Width(max(1, width-2)).Render("›" + row[1:])
			}
		}
		lines = append(lines, row)
	}
	style := m.theme.panel.Width(width).Height(height).Padding(1, 1).BorderRight(true).BorderStyle(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("#30363D"))
	if focused {
		style = style.BorderForeground(lipgloss.Color("#58A6FF"))
	}
	return style.Render(strings.Join(lines, "\n"))
}

func (m Model) renderDiff(width, height int) string {
	focused := m.focus == focusDiff
	mode := "UNIFIED"
	if m.view == split {
		mode = "SPLIT"
	}
	title := m.theme.muted.Render("DIFF  " + mode)
	if focused {
		title = m.theme.focus.Render("DIFF  " + mode)
	}
	path := m.theme.text.Bold(true).Render(shortPath(m.currentPath(), max(10, width-lipgloss.Width(title)-6)))
	line := title
	if m.currentPath() != "" {
		line += "  " + path
	}
	contentHeight := max(1, height-3)
	var code string
	if len(m.repo.Files) == 0 {
		code = m.theme.muted.Render("No diff to review.")
	} else if m.repo.Files[m.file].Binary {
		code = m.theme.muted.Render("Binary file changed. Content preview is unavailable.")
	} else if m.view == split {
		code = m.renderSplit(width-2, contentHeight)
	} else {
		code = m.renderUnified(width-2, contentHeight)
	}
	style := m.theme.canvas.Width(width).Height(height).Padding(1, 1)
	return style.Render(line + "\n\n" + code)
}

func (m Model) renderUnified(width, height int) string {
	lines := m.currentLines()
	if len(lines) == 0 {
		return m.theme.muted.Render("No textual changes in this file.")
	}
	var out []string
	used := 0
	for i := clamp(m.lineScroll, 0, len(lines)-1); i < len(lines) && used < height; i++ {
		row := m.renderUnifiedLine(i, lines[i], width)
		out = append(out, row)
		used++
		for _, comment := range m.commentsForLine(lines[i]) {
			if used >= height {
				break
			}
			card := m.renderCommentCard(comment, width)
			out = append(out, card)
			used += max(1, strings.Count(card, "\n")+1)
		}
	}
	return strings.Join(out, "\n")
}

func (m Model) renderUnifiedLine(index int, line diff.Line, width int) string {
	selected := index == m.line && m.focus == focusDiff
	ranged := m.selectFrom >= 0 && index >= min(m.selectFrom, m.line) && index <= max(m.selectFrom, m.line)
	commentMark := " "
	if len(m.commentsForLine(line)) > 0 {
		commentMark = m.theme.comment.Render("●")
	} else if ranged {
		commentMark = m.theme.comment.Render("│")
	}
	oldNo, newNo := number(line.OldNumber), number(line.NewNumber)
	prefix := fmt.Sprintf("%s %4s %4s %s ", commentMark, oldNo, newNo, line.Kind.Marker())
	contentWidth := max(1, width-lipgloss.Width(prefix))
	source := truncatePlain(expandTabs(line.Text), contentWidth)
	if line.Kind != diff.Meta {
		source = m.highlight.line(m.currentPath(), source)
	}
	row := prefix + source
	style := m.theme.canvas.Width(width)
	switch line.Kind {
	case diff.Addition:
		style = m.theme.added.Width(width)
	case diff.Deletion:
		style = m.theme.deleted.Width(width)
	case diff.Meta:
		style = m.theme.hunk.Width(width)
	}
	if selected {
		style = style.Bold(true).BorderLeft(true).BorderStyle(lipgloss.ThickBorder()).BorderForeground(lipgloss.Color("#58A6FF")).PaddingLeft(0)
	}
	return style.Render(row)
}

type splitRow struct {
	old, new           *diff.Line
	oldIndex, newIndex int
	meta               *diff.Line
	metaIndex          int
}

func splitRows(lines []diff.Line) []splitRow {
	var rows []splitRow
	for i := 0; i < len(lines); {
		if lines[i].Kind == diff.Meta {
			line := lines[i]
			rows = append(rows, splitRow{meta: &line, metaIndex: i})
			i++
			continue
		}
		if lines[i].Kind == diff.Context {
			line := lines[i]
			rows = append(rows, splitRow{old: &line, new: &line, oldIndex: i, newIndex: i})
			i++
			continue
		}
		start := i
		var dels, adds []int
		for i < len(lines) && lines[i].Kind == diff.Deletion {
			dels = append(dels, i)
			i++
		}
		for i < len(lines) && lines[i].Kind == diff.Addition {
			adds = append(adds, i)
			i++
		}
		if len(dels) == 0 && len(adds) == 0 {
			i = start + 1
			continue
		}
		for j := 0; j < max(len(dels), len(adds)); j++ {
			row := splitRow{oldIndex: -1, newIndex: -1}
			if j < len(dels) {
				line := lines[dels[j]]
				row.old = &line
				row.oldIndex = dels[j]
			}
			if j < len(adds) {
				line := lines[adds[j]]
				row.new = &line
				row.newIndex = adds[j]
			}
			rows = append(rows, row)
		}
	}
	return rows
}

func (m Model) renderSplit(width, height int) string {
	rows := splitRows(m.currentLines())
	half := max(12, (width-1)/2)
	var out []string
	start := 0
	for i, row := range rows {
		if row.metaIndex >= m.line || row.oldIndex >= m.line || row.newIndex >= m.line {
			start = max(0, i-1)
			break
		}
	}
	for i := start; i < len(rows) && len(out) < height; i++ {
		row := rows[i]
		if row.meta != nil {
			out = append(out, m.theme.hunk.Width(width).Render(truncatePlain(row.meta.Text, width)))
			continue
		}
		left := m.renderSplitCell(row.old, row.oldIndex, half)
		right := m.renderSplitCell(row.new, row.newIndex, width-half-1)
		out = append(out, left+m.theme.border.Render("│")+right)
	}
	return strings.Join(out, "\n")
}

func (m Model) renderSplitCell(line *diff.Line, index, width int) string {
	if line == nil {
		return m.theme.panel.Width(width).Render("")
	}
	n := line.OldNumber
	if line.Kind == diff.Addition {
		n = line.NewNumber
	}
	prefix := fmt.Sprintf("%4s %s ", number(n), line.Kind.Marker())
	source := m.highlight.line(m.currentPath(), truncatePlain(expandTabs(line.Text), max(1, width-lipgloss.Width(prefix))))
	style := m.theme.canvas.Width(width)
	if line.Kind == diff.Addition {
		style = m.theme.added.Width(width)
	} else if line.Kind == diff.Deletion {
		style = m.theme.deleted.Width(width)
	}
	if index == m.line && m.focus == focusDiff {
		style = style.Bold(true).Underline(true)
	}
	return style.Render(prefix + source)
}

func (m Model) renderCommentCard(comment review.Comment, width int) string {
	state := "OPEN"
	icon := "●"
	style := m.theme.comment
	if comment.Resolved {
		state = "RESOLVED"
		icon = "✓"
		style = m.theme.muted
	}
	header := style.Render(fmt.Sprintf("   %s %s", icon, state))
	body := m.theme.text.Width(max(10, width-8)).Render(comment.Body)
	return m.theme.raised.BorderLeft(true).BorderStyle(lipgloss.ThickBorder()).BorderForeground(lipgloss.Color("#D2A8FF")).Padding(0, 1).Width(width).Render(header + "\n   " + body)
}

func (m Model) renderFooter() string {
	keys := "/ find   s split   v range   c comment   a address   ? help"
	if m.width < 90 {
		keys = "tab panes   / find   c comment   ? help"
	}
	status := truncatePlain(m.status, max(10, m.width-len(keys)-5))
	gap := max(1, m.width-lipgloss.Width(status)-lipgloss.Width(keys)-2)
	return m.theme.panel.Width(m.width).Render(" " + m.theme.text.Render(status) + strings.Repeat(" ", gap) + m.theme.muted.Render(keys) + " ")
}

func (m Model) renderSearch() string {
	lines := []string{m.theme.focus.Render("JUMP TO FILE"), "", m.theme.text.Render("› " + m.input + "█"), m.theme.border.Render(strings.Repeat("─", max(1, min(66, m.width-10))))}
	for i, fileIndex := range m.searchHits {
		if i >= 10 {
			break
		}
		path := m.repo.Files[fileIndex].Path
		row := "  " + path
		if i == m.searchAt {
			row = m.theme.raised.Bold(true).Width(max(1, min(66, m.width-10))).Render("› " + path)
		} else {
			row = m.theme.text.Render(row)
		}
		lines = append(lines, row)
	}
	if len(m.searchHits) == 0 {
		lines = append(lines, m.theme.muted.Render("  No matching changed files"))
	}
	lines = append(lines, "", m.theme.muted.Render("↑↓ select   enter jump   esc close"))
	return strings.Join(lines, "\n")
}

func (m Model) renderCommentEditor() string {
	location := "whole review"
	if !m.inputAnchor.WholeRepo {
		location = m.inputAnchor.Path
		line := m.inputAnchor.NewStart
		if line == 0 {
			line = m.inputAnchor.OldStart
		}
		location += fmt.Sprintf(":%d", line)
	}
	body := m.input
	if body == "" {
		body = m.theme.muted.Render("Describe the change you want the agent to make…") + "█"
	} else {
		body += "█"
	}
	return m.theme.focus.Render(strings.ToUpper(m.inputTitle)) + "\n" + m.theme.muted.Render(location) + "\n\n" + m.theme.raised.Width(max(20, min(70, m.width-10))).Height(8).Padding(1, 2).Render(body) + "\n\n" + m.theme.muted.Render("ctrl+s save   enter newline   esc cancel")
}

func (m Model) renderHelp() string {
	return m.theme.focus.Render("REVUI KEYMAP") + "\n\n" +
		keyRow("j / k · ↑ / ↓", "move") + keyRow("tab · h / l", "switch pane") + keyRow("/", "fuzzy jump to file") + keyRow("[ / ]", "previous / next hunk") + keyRow("s", "toggle unified / split") + keyRow("v then move", "select a line range") + keyRow("c · shift+c", "line/range · whole-review comment") + keyRow("e / r / d", "edit / resolve / delete comment") + keyRow("n / p", "next / previous comment") + keyRow("a", "preview prompt and run agent") + keyRow("R", "refresh Git diff") + keyRow("q", "quit") + "\n" + m.theme.muted.Render("Comments are saved under .git/revui and never committed.")
}

func (m Model) renderPrompt() string {
	header := m.theme.comment.Render("ADDRESS REVIEW COMMENTS") + "  " + m.theme.muted.Render(agentCommandLabel())
	return header + "\n\n" + m.theme.muted.Render("Preview the exact instructions before the agent edits files:") + "\n\n" + m.theme.raised.Width(max(20, min(82, m.width-10))).Height(max(8, min(20, m.height-10))).Padding(1, 2).Render(truncateLines(m.prompt, max(6, min(16, m.height-12)))) + "\n\n" + m.theme.focus.Render("enter run agent") + m.theme.muted.Render("   esc cancel")
}

func (m Model) renderAgentResult() string {
	output := m.agentOutput
	if output == "" {
		output = "Agent finished without a text response."
	}
	return m.theme.comment.Render("AGENT FINISHED — REVIEW THE REFRESHED DIFF") + "\n\n" + m.theme.raised.Width(max(20, min(82, m.width-10))).Height(max(8, min(20, m.height-10))).Padding(1, 2).Render(truncateLines(output, max(6, min(16, m.height-12)))) + "\n\n" + m.theme.muted.Render("enter or esc close   comments remain unresolved until you press r")
}

func (m Model) overlay(background, foreground string, width, height int) string {
	box := m.theme.panel.BorderStyle(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#58A6FF")).Padding(1, 2).Width(width).Height(height).Render(foreground)
	canvas := lipgloss.NewCanvas(m.width, m.height)
	canvas.Compose(lipgloss.NewLayer(background))
	canvas.Compose(lipgloss.NewLayer(box).X(max(0, (m.width-width)/2)).Y(max(0, (m.height-height)/2)))
	return canvas.Render()
}

func (m Model) commentsForLine(line diff.Line) []review.Comment {
	var comments []review.Comment
	for _, c := range m.session.Comments {
		if c.Anchor.Path == m.currentPath() && anchorContains(c.Anchor, line) {
			comments = append(comments, c)
		}
	}
	return comments
}
func (m Model) fileCommentCount(path string) int {
	n := 0
	for _, c := range m.session.Comments {
		if c.Anchor.Path == path && !c.Resolved {
			n++
		}
	}
	return n
}
func statusMarker(status string) string {
	switch status {
	case "A":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#56D364")).Render("A")
	case "D":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#FF7B72")).Render("D")
	case "R":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#D2A8FF")).Render("R")
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#79C0FF")).Render("M")
	}
}
func number(n int) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("%d", n)
}
func truncatePlain(value string, width int) string {
	r := []rune(value)
	if len(r) <= width {
		return value
	}
	if width <= 1 {
		return "…"
	}
	return string(r[:width-1]) + "…"
}
func expandTabs(value string) string { return strings.ReplaceAll(value, "\t", "    ") }
func truncateLines(value string, count int) string {
	lines := strings.Split(value, "\n")
	if len(lines) <= count {
		return value
	}
	return strings.Join(lines[:count], "\n") + "\n…"
}
func keyRow(key, description string) string {
	return fmt.Sprintf("%-24s %s\n", lipgloss.NewStyle().Foreground(lipgloss.Color("#58A6FF")).Render(key), description)
}
func agentCommandLabel() string { return "configured agent command" }
