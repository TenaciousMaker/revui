package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/TenaciousMaker/revui/internal/diff"
)

func (m Model) View() tea.View {
	if m.renderCache != nil && m.renderCache.valid && m.renderCache.version == m.renderVersion {
		return m.renderCache.view
	}
	view := m.renderView()
	if m.renderCache != nil {
		m.renderCache.version = m.renderVersion
		m.renderCache.view = view
		m.renderCache.valid = true
	}
	return view
}

func (m Model) renderView() tea.View {
	if m.width == 0 || m.height == 0 {
		view := tea.NewView("Starting revui…")
		view.AltScreen = true
		view.MouseMode = tea.MouseModeCellMotion
		return view
	}
	header := m.renderHeader()
	bodyHeight := max(3, m.height-4)
	var body string
	if m.mode == searchingRepository && m.width < 90 {
		body = m.renderRepositorySearch(m.width, bodyHeight)
	} else if m.width < 90 {
		if m.focus == focusFiles {
			body = m.renderFiles(m.width, bodyHeight)
		} else {
			body = m.renderDiff(m.width, bodyHeight)
		}
	} else {
		leftWidth := m.filePaneWidth()
		rightWidth := max(20, m.width-leftWidth)
		right := m.renderDiff(rightWidth, bodyHeight)
		if m.mode == searchingRepository {
			right = m.renderRepositorySearch(rightWidth, bodyHeight)
		}
		body = lipgloss.JoinHorizontal(lipgloss.Top, m.renderFiles(leftWidth, bodyHeight), right)
	}
	content := lipgloss.JoinVertical(lipgloss.Left, header, body, m.renderFooter())
	content = m.theme.canvas.Width(m.width).Height(m.height).Render(content)
	content = m.renderMouseSelection(content)
	switch m.mode {
	case searching:
		content = m.overlay(content, m.renderSearch(), min(72, m.width-4), min(18, m.height-4))
	case showHelp:
		content = m.overlay(content, m.renderHelp(), min(82, m.width-4), min(28, m.height-4))
	}
	view := tea.NewView(content)
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	view.WindowTitle = "revui — pre-PR review"
	return view
}

func (m *Model) invalidateRender() {
	m.renderVersion++
}

func (m Model) renderHeader() string {
	adds, dels := m.repo.Totals()
	logo := m.theme.focus.Render(" REVUI ")
	branch := m.theme.text.Bold(true).Render(m.repo.Branch) + m.theme.muted.Render("  →  "+m.repo.Base)
	live := ""
	if m.watcher != nil {
		live = m.theme.addedText.Render("● live") + "  "
	}
	stats := live + m.theme.addedText.Render(fmt.Sprintf("+%d", adds)) + "  " + m.theme.deletedText.Render(fmt.Sprintf("-%d", dels))
	gap := max(1, m.width-lipgloss.Width(logo)-lipgloss.Width(branch)-lipgloss.Width(stats)-4)
	line := logo + "  " + branch + strings.Repeat(" ", gap) + stats
	return m.theme.panel.Width(m.width).Height(2).Padding(0, 1).Render(line)
}

func (m Model) renderFiles(width, height int) string {
	focused := m.focus == focusFiles
	layout := "FLAT"
	fileCount := len(m.repo.Files)
	if m.fileLayout == treeFiles {
		layout = "TREE  " + m.fileScope.label()
		fileCount = m.treeFileCount
		if !m.treeNodesReady || m.treeNodesScope != m.fileScope || m.treeNodesFileCount != len(m.repo.Files) || m.treeNodesPathCount != len(m.repo.AllPaths) {
			fileCount = len(scopedFileTreeEntries(m.repo.Files, m.repo.AllPaths, m.fileScope))
		}
	}
	if m.wideFiles && m.width >= 90 {
		layout += "  WIDE"
	}
	titleText := xansi.Truncate(fmt.Sprintf("FILES  %d  %s  %s", fileCount, layout, m.reviewedProgressText()), max(1, width-4), "…")
	title := m.theme.muted.Render(titleText)
	if focused {
		title = m.theme.focus.Render(titleText)
	}
	lines := []string{title, ""}
	if fileCount == 0 {
		lines = append(lines, m.theme.muted.Width(max(1, width-4)).Render("Nothing changed against "+m.repo.Base+".\n\nMake a change, then press R to refresh."))
	}
	available := max(1, height-3)
	if m.fileLayout == treeFiles {
		nodes := m.currentTreeNodes()
		start := clamp(m.fileScroll, 0, max(0, len(nodes)-1))
		end := min(len(nodes), start+available)
		for i := start; i < end; i++ {
			lines = append(lines, m.renderTreeNode(nodes[i], i == m.treeCursor, focused, width))
		}
	} else {
		start := clamp(m.fileScroll, 0, max(0, len(m.repo.Files)-1))
		end := min(len(m.repo.Files), start+available)
		for i := start; i < end; i++ {
			lines = append(lines, m.renderFlatFile(i, i == m.file, focused, width))
		}
	}
	style := m.theme.panel.Width(width).Height(height).Padding(1, 1).BorderRight(true).BorderStyle(lipgloss.NormalBorder())
	if m.theme.color {
		style = style.BorderForeground(lipgloss.Color("#30363D"))
	}
	if focused && m.theme.color {
		style = style.BorderForeground(lipgloss.Color("#58A6FF"))
	}
	return style.Render(strings.Join(lines, "\n"))
}

func (m Model) filePaneWidth() int {
	compact := clamp(m.width*30/100, 26, 42)
	if !m.wideFiles || m.width < 90 {
		return compact
	}
	maximum := m.width - 48
	if maximum <= compact {
		return compact
	}
	return clamp(m.filePaneRequiredWidth(), compact, maximum)
}

func (m Model) filePaneRequiredWidth() int {
	required := lipgloss.Width("FILES  000  TREE  CONTEXT  000/000 REVIEWED  WIDE") + 4
	if m.fileLayout == treeFiles {
		if m.treeNodesReady && m.treeRequiredWidth > 0 {
			return max(required, m.treeRequiredWidth)
		}
		return max(required, m.requiredTreeWidth(m.currentTreeNodes()))
	}
	for _, file := range m.repo.Files {
		width := len([]rune(file.Path)) + lipgloss.Width(m.fileCountText(file)) + 9
		required = max(required, width)
	}
	return required
}

func (m Model) requiredTreeWidth(nodes []fileTreeNode) int {
	required := 0
	for _, node := range nodes {
		indent := node.depth * 2
		if node.directory {
			required = max(required, 1+indent+2+len([]rune(node.name))+4)
			continue
		}
		countWidth := 0
		if node.fileIndex >= 0 {
			countWidth = lipgloss.Width(m.fileCountText(m.repo.Files[node.fileIndex]))
		}
		required = max(required, 1+indent+4+len([]rune(node.name))+countWidth+5)
	}
	return required
}

func (m Model) renderFlatFile(fileIndex int, selected, focused bool, width int) string {
	file := m.repo.Files[fileIndex]
	marker := m.statusMarker(file.Status)
	reviewed := m.fileReviewed(fileIndex)
	reviewMark := " "
	if reviewed {
		reviewMark = m.theme.addedText.Render("✓")
	}
	countText := m.fileCountText(file)
	nameWidth := max(6, width-lipgloss.Width(countText)-9)
	row := " " + reviewMark + " " + marker + " " + shortPath(file.Path, nameWidth)
	gap := max(1, width-4-lipgloss.Width(row)-lipgloss.Width(countText))
	row += strings.Repeat(" ", gap) + countText
	if reviewed && !selected {
		row = m.theme.muted.Render(xansi.Strip(row))
	}
	return m.styleFileRow(row, selected, focused, width)
}

func (m Model) renderTreeNode(node fileTreeNode, selected, focused bool, width int) string {
	indent := strings.Repeat("  ", node.depth)
	if node.directory {
		glyph := "▾"
		if m.collapsed[node.path] {
			glyph = "▸"
		}
		row := " " + indent + m.theme.muted.Render(glyph) + " " + node.name
		return m.styleFileRow(xansi.Truncate(row, max(1, width-2), "…"), selected, focused, width)
	}
	marker := m.theme.muted.Render("·")
	reviewMark := " "
	reviewed := false
	countText := ""
	if node.fileIndex >= 0 {
		file := m.repo.Files[node.fileIndex]
		marker = m.statusMarker(file.Status)
		countText = m.fileCountText(file)
		reviewed = m.fileReviewed(node.fileIndex)
		if reviewed {
			reviewMark = m.theme.addedText.Render("✓")
		}
	}
	nameWidth := max(4, width-lipgloss.Width(indent)-lipgloss.Width(countText)-9)
	name := shortPath(node.name, nameWidth)
	if node.fileIndex < 0 {
		name = m.theme.muted.Render(name)
	}
	row := " " + indent + reviewMark + " " + marker + " " + name
	gap := max(1, width-4-lipgloss.Width(row)-lipgloss.Width(countText))
	row += strings.Repeat(" ", gap) + countText
	if reviewed && !selected {
		row = m.theme.muted.Render(xansi.Strip(row))
	}
	return m.styleFileRow(row, selected, focused, width)
}

func (m Model) fileReviewed(fileIndex int) bool {
	if fileIndex < 0 || fileIndex >= len(m.repo.Files) {
		return false
	}
	file := m.repo.Files[fileIndex]
	return m.session.IsReviewed(file.Path, fileReviewFingerprint(file))
}

func (m Model) reviewedProgressText() string {
	reviewed := 0
	for index := range m.repo.Files {
		if m.fileReviewed(index) {
			reviewed++
		}
	}
	return fmt.Sprintf("%d/%d REVIEWED", reviewed, len(m.repo.Files))
}

func (m Model) styleFileRow(row string, selected, focused bool, width int) string {
	row = xansi.Truncate(row, max(1, width-2), "")
	if !selected {
		return row
	}
	if row == "" {
		row = " "
	}
	style := m.theme.panel.Bold(true).Width(max(1, width-2)).MaxWidth(max(1, width-2))
	if focused {
		style = m.theme.raised.Bold(true).Width(max(1, width-2)).MaxWidth(max(1, width-2))
		if m.theme.color {
			style = style.Foreground(lipgloss.Color("#FFFFFF"))
		}
	}
	return style.Render("›" + row[1:])
}

func (m Model) fileCountText(file diff.File) string {
	return fmt.Sprintf(" %s%d %s%d", m.theme.addedText.Render("+"), file.Additions, m.theme.deletedText.Render("-"), file.Deletions)
}

func (m Model) renderDiff(width, height int) string {
	focused := m.focus == focusDiff
	mode := "UNIFIED"
	if m.view == split {
		mode = "SPLIT"
	}
	pathText := m.currentPath()
	if m.sourcePath != "" {
		mode = "SOURCE"
		if m.sourceFromBase {
			mode = "SOURCE BASE"
		}
		pathText = m.sourcePath
	}
	titleText := "DIFF  " + mode
	if m.sourcePath != "" {
		titleText = mode
	}
	title := m.theme.muted.Render(titleText)
	if focused {
		title = m.theme.focus.Render(titleText)
	}
	path := m.theme.text.Bold(true).Render(shortPath(pathText, max(10, width-lipgloss.Width(title)-6)))
	line := title
	if pathText != "" {
		line += "  " + path
	}
	contentHeight := max(1, height-3)
	var code string
	if m.sourcePath != "" {
		code = m.renderSource(width-2, contentHeight)
	} else if len(m.repo.Files) == 0 {
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
	}
	return strings.Join(out, "\n")
}

func (m Model) renderUnifiedLine(index int, line diff.Line, width int) string {
	selected := index == m.line && m.focus == focusDiff
	ranged := m.selectFrom >= 0 && index >= min(m.selectFrom, m.line) && index <= max(m.selectFrom, m.line)
	selectionMark := " "
	if ranged {
		selectionMark = m.theme.focus.Render("│")
	}
	oldNo, newNo := number(line.OldNumber), number(line.NewNumber)
	prefix := fmt.Sprintf("%s %4s %4s ", selectionMark, oldNo, newNo) + m.renderDiffMarker(line.Kind) + " "
	contentWidth := max(1, width-lipgloss.Width(prefix))
	source := truncatePlain(expandTabs(line.Text), contentWidth)
	if line.Kind != diff.Meta {
		source = m.highlightLine(m.currentPath(), source, syntaxBackground(line.Kind))
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
		style = style.Bold(true).BorderLeft(true).BorderStyle(lipgloss.ThickBorder()).PaddingLeft(0)
		if m.theme.color {
			style = style.BorderForeground(lipgloss.Color("#58A6FF"))
		}
	}
	return style.Render(row)
}

type splitRow struct {
	old, new           *diff.Line
	oldIndex, newIndex int
	meta               *diff.Line
	metaIndex          int
}

func (r splitRow) containsLine(line int) bool {
	return r.metaIndex == line || r.oldIndex == line || r.newIndex == line
}

func (r splitRow) cursorIndex() int {
	if r.meta != nil {
		return r.metaIndex
	}
	if r.oldIndex >= 0 {
		return r.oldIndex
	}
	if r.newIndex >= 0 {
		return r.newIndex
	}
	return 0
}

func splitRows(lines []diff.Line) []splitRow {
	var rows []splitRow
	for i := 0; i < len(lines); {
		if lines[i].Kind == diff.Meta {
			line := lines[i]
			rows = append(rows, splitRow{meta: &line, metaIndex: i, oldIndex: -1, newIndex: -1})
			i++
			continue
		}
		if lines[i].Kind == diff.Context {
			line := lines[i]
			rows = append(rows, splitRow{old: &line, new: &line, oldIndex: i, newIndex: i, metaIndex: -1})
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
			row := splitRow{oldIndex: -1, newIndex: -1, metaIndex: -1}
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
	rows := m.currentSplitRows()
	half := max(12, (width-1)/2)
	var out []string
	start := clamp(m.splitScroll, 0, max(0, len(rows)-1))
	for i := start; i < len(rows) && len(out) < height; i++ {
		row := rows[i]
		selected := i == m.splitCursor && m.focus == focusDiff
		if row.meta != nil {
			marker := " "
			if selected {
				marker = m.theme.focus.Render("›")
			}
			content := xansi.Truncate(marker+row.meta.Text, width, "")
			out = append(out, m.theme.hunk.Width(width).MaxWidth(width).Render(content))
			continue
		}
		left := m.renderSplitCell(row.old, half, selected, true)
		right := m.renderSplitCell(row.new, width-half-1, selected, false)
		divider := m.theme.border.Render("│")
		if selected {
			divider = m.theme.focus.Render("│")
		}
		out = append(out, left+divider+right)
	}
	return strings.Join(out, "\n")
}

func (m Model) renderSplitCell(line *diff.Line, width int, selected, left bool) string {
	marker := " "
	if selected && left {
		marker = m.theme.focus.Render("›")
	}
	if line == nil {
		return m.theme.panel.Width(width).MaxWidth(width).Render(marker)
	}
	n := line.OldNumber
	if line.Kind == diff.Addition {
		n = line.NewNumber
	}
	prefix := marker + fmt.Sprintf("%3s ", number(n)) + m.renderDiffMarker(line.Kind) + " "
	contentWidth := max(1, width-lipgloss.Width(prefix))
	source := xansi.Truncate(expandTabs(line.Text), contentWidth, "…")
	source = m.highlightLine(m.currentPath(), source, syntaxBackground(line.Kind))
	content := xansi.Truncate(prefix+source, width, "")
	style := m.theme.canvas.Width(width).MaxWidth(width)
	switch line.Kind {
	case diff.Addition:
		style = m.theme.added.Width(width).MaxWidth(width)
	case diff.Deletion:
		style = m.theme.deleted.Width(width).MaxWidth(width)
	}
	if selected {
		style = style.Bold(true)
	}
	return style.Render(content)
}

func syntaxBackground(kind diff.LineKind) string {
	switch kind {
	case diff.Addition:
		return addedLineBackground
	case diff.Deletion:
		return deletedLineBackground
	default:
		return "#0D1117"
	}
}

func (m Model) renderDiffMarker(kind diff.LineKind) string {
	switch kind {
	case diff.Addition:
		if !m.theme.color {
			return m.theme.addedText.Render("+")
		}
		return m.theme.addedText.Background(lipgloss.Color(addedLineBackground)).Bold(true).Render("+")
	case diff.Deletion:
		if !m.theme.color {
			return m.theme.deletedText.Render("-")
		}
		return m.theme.deletedText.Background(lipgloss.Color(deletedLineBackground)).Bold(true).Render("-")
	default:
		return kind.Marker()
	}
}

func (m Model) renderFooter() string {
	keys := "j/k move   [/] hunk   s split   o source   v range   y copy   f find   ? help"
	if m.mode == searchingRepository {
		keys = "type query   ↑↓ results   enter open   esc close"
	} else if m.sourcePath != "" {
		keys = "j/k move   o/d diff   y copy   space reviewed   f search   A scope   esc back"
	} else if m.width < 90 {
		keys = "tab panes   A scope   o full   / files   f text   v range   y copy   ? help"
	} else if m.focus == focusFiles {
		keys = "j/k move   enter open   t tree   A scope   space reviewed   w widen   / jump   ? help"
	} else if m.width < 135 {
		keys = "t tree   A scope   o full   / files   f text   v range   y copy   ? help"
	}
	status := truncatePlain(m.status, max(10, m.width-len(keys)-5))
	gap := max(1, m.width-lipgloss.Width(status)-lipgloss.Width(keys)-2)
	return m.theme.panel.Width(m.width).Render(" " + m.theme.text.Render(status) + strings.Repeat(" ", gap) + m.theme.muted.Render(keys) + " ")
}

func (m Model) renderSearch() string {
	lines := []string{m.theme.focus.Render("JUMP TO FILE"), "", m.theme.text.Render("› " + m.inputWithCursor()), m.theme.border.Render(strings.Repeat("─", max(1, min(66, m.width-10))))}
	for i := clamp(m.searchTop, 0, max(0, len(m.searchHits)-1)); i < len(m.searchHits); i++ {
		if i >= m.searchTop+10 {
			break
		}
		fileIndex := m.searchHits[i]
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
	lines = append(lines, "", m.theme.muted.Render("←→ edit   ctrl+u clear left   ↑↓ select   enter jump   esc close"))
	return strings.Join(lines, "\n")
}

func (m Model) renderHelp() string {
	return m.theme.focus.Render("REVUI KEYMAP") + "\n\n" +
		m.keyRow("j / k · ↑ / ↓", "move") + m.keyRow("mouse click / wheel", "position row / scroll pane") + m.keyRow("mouse drag then y", "copy selected text") + m.keyRow("tab · h / l", "switch pane or collapse tree") + m.keyRow("t", "toggle flat / tree files") + m.keyRow("A", "cycle changed / context / all files") + m.keyRow("space", "toggle selected changed file reviewed") + m.keyRow("o", "toggle full-file source / diff") + m.keyRow("w", "fit / restore file pane width") + m.keyRow("enter", "open file or toggle folder") + m.keyRow("/", "fuzzy jump to changed file") + m.keyRow("f", "search text across repository") + m.keyRow("v then move", "select a line range") + m.keyRow("y", "copy current line or selected range") + m.keyRow("[ / ]", "previous / next hunk") + m.keyRow("s", "toggle unified / split") + m.keyRow("R", "refresh Git diff") + m.keyRow("q", "quit") + "\n" + m.theme.muted.Render("Reviewed-file progress is saved locally under .git/revui.")
}

func (m Model) overlay(background, foreground string, width, height int) string {
	box, x, y := m.modalLayout(foreground, width, height)
	boxWidth, boxHeight := lipgloss.Width(box), lipgloss.Height(box)
	shadowStyle := lipgloss.NewStyle().Width(boxWidth).Height(boxHeight)
	if m.theme.color {
		shadowStyle = shadowStyle.Background(lipgloss.Color("#010409"))
	}
	shadow := shadowStyle.Render("")
	canvas := lipgloss.NewCanvas(m.width, m.height)
	compositor := lipgloss.NewCompositor(
		lipgloss.NewLayer(background).Z(0),
		lipgloss.NewLayer(shadow).X(min(max(0, m.width-boxWidth), x+1)).Y(min(max(0, m.height-boxHeight), y+1)).Z(1),
		lipgloss.NewLayer(box).X(x).Y(y).Z(2),
	)
	canvas.Compose(compositor)
	return canvas.Render()
}

func (m Model) modalLayout(foreground string, maxWidth, maxHeight int) (box string, x, y int) {
	const horizontalFrame = 6 // two border cells plus two cells of padding on each side
	const verticalFrame = 4   // two border cells plus one row of padding above and below
	contentWidth := max(1, maxWidth-horizontalFrame)
	contentHeight := max(1, maxHeight-verticalFrame)
	boxStyle := m.theme.panel.
		BorderStyle(lipgloss.RoundedBorder()).
		Padding(1, 2).
		Width(contentWidth).
		MaxHeight(contentHeight)
	if m.theme.color {
		boxStyle = boxStyle.BorderForeground(lipgloss.Color("#58A6FF"))
	}
	box = boxStyle.Render(foreground)
	boxWidth, boxHeight := lipgloss.Width(box), lipgloss.Height(box)
	x = max(0, (m.width-boxWidth)/2)
	y = max(0, (m.height-boxHeight)/2)
	return box, x, y
}

func (m Model) statusMarker(status string) string {
	switch status {
	case "A":
		return m.theme.addedText.Render("A")
	case "D":
		return m.theme.deletedText.Render("D")
	case "R":
		if !m.theme.color {
			return "R"
		}
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#D2A8FF")).Render("R")
	default:
		if !m.theme.color {
			return "M"
		}
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
func (m Model) keyRow(key, description string) string {
	return fmt.Sprintf("%-24s %s\n", m.theme.focus.Render(key), description)
}
