package ui

import (
	"math"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	xansi "github.com/charmbracelet/x/ansi"
)

const mouseWheelStep = 1

const mouseWheelFrame = time.Second / 60

type wheelPane uint8

const (
	wheelPaneNone wheelPane = iota
	wheelPaneFiles
	wheelPaneCode
	wheelPaneFuzzySearch
	wheelPaneRepositorySearch
)

type mouseWheelFrameMsg struct{}

func (m Model) queueMouseWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	direction := 0
	switch msg.Button {
	case tea.MouseWheelUp:
		direction = -1
	case tea.MouseWheelDown:
		direction = 1
	default:
		return m, nil
	}
	target := m.wheelPaneAt(msg.X, msg.Y)
	if target == wheelPaneNone {
		return m, nil
	}
	m.updateWheelVelocity(direction, target)
	m.clearMouseSelection()
	if !m.wheelScheduled {
		m.wheelScheduled = true
		m.wheelDirection = direction
		m.wheelTarget = target
		m.wheelBurst = 0
		m.invalidateRender()
		m.applyMouseWheel(target, direction*m.wheelVelocityStep())
		return m, tea.Tick(mouseWheelFrame, func(time.Time) tea.Msg { return mouseWheelFrameMsg{} })
	}
	if direction != m.wheelDirection || target != m.wheelTarget {
		m.wheelDirection = direction
		m.wheelTarget = target
		m.wheelBurst = 0
		m.invalidateRender()
		m.applyMouseWheel(target, direction*m.wheelVelocityStep())
		return m, nil
	}
	m.wheelBurst++
	return m, nil
}

func (m Model) flushMouseWheel() Model {
	m.wheelScheduled = false
	if m.wheelBurst > 0 {
		m.invalidateRender()
		m.applyMouseWheel(m.wheelTarget, m.wheelDirection*m.wheelVelocityStep())
	}
	m.wheelBurst = 0
	m.wheelDirection = 0
	m.wheelTarget = wheelPaneNone
	return m
}

func (m *Model) updateWheelVelocity(direction int, target wheelPane) {
	now := time.Now()
	if m.now != nil {
		now = m.now()
	}
	gap := now.Sub(m.wheelLastAt)
	if m.wheelLastAt.IsZero() || gap < 0 || gap > 220*time.Millisecond || direction != m.wheelLastDirection || target != m.wheelLastTarget {
		m.wheelVelocity = 1
	} else {
		decay := math.Exp(-float64(gap) / float64(90*time.Millisecond))
		m.wheelVelocity = min(10, m.wheelVelocity*decay+1)
	}
	m.wheelLastAt = now
	m.wheelLastDirection = direction
	m.wheelLastTarget = target
}

func (m Model) wheelVelocityStep() int {
	step := int(math.Round(math.Pow(max(1, m.wheelVelocity), 1.3)))
	return clamp(step, mouseWheelStep, m.pageSize())
}

func (m Model) wheelPaneAt(x, y int) wheelPane {
	switch m.mode {
	case searching:
		return wheelPaneFuzzySearch
	case searchingRepository:
		return wheelPaneRepositorySearch
	case normal:
		// Continue below for the main file and code panes.
	default:
		return wheelPaneNone
	}
	if y < 2 || y >= m.height-1 {
		return wheelPaneNone
	}
	if m.width < 90 {
		if m.focus == focusFiles {
			return wheelPaneFiles
		}
		return wheelPaneCode
	}
	if x < m.filePaneWidth() {
		return wheelPaneFiles
	}
	return wheelPaneCode
}

func (m *Model) applyMouseWheel(target wheelPane, direction int) {
	switch target {
	case wheelPaneFuzzySearch:
		m.searchTop = clamp(m.searchTop+direction, 0, max(0, len(m.searchHits)-10))
	case wheelPaneRepositorySearch:
		m.repoSearchTop = clamp(m.repoSearchTop+direction, 0, max(0, len(m.repoHits)-1))
	case wheelPaneFiles:
		m.scrollFiles(direction)
	case wheelPaneCode:
		m.scrollCode(direction)
	}
}

type mousePoint struct{ x, y int }

func (m Model) handleMouseClick(msg tea.MouseClickMsg) Model {
	if msg.Button != tea.MouseLeft {
		m.clearMouseSelection()
		return m
	}
	switch m.mode {
	case searching:
		m.clickFileSearchResult(msg.X, msg.Y)
		return m
	case searchingRepository:
		if m.isFilePanePoint(msg.X, msg.Y) {
			m.positionFileRow(msg.Y)
		} else {
			m.clickRepositorySearchResult(msg.X, msg.Y)
		}
		return m
	case normal:
		if m.isFilePanePoint(msg.X, msg.Y) {
			m.clearMouseSelection()
			m.positionFileRow(msg.Y)
			return m
		}
	default:
		m.clearMouseSelection()
		return m
	}
	if !m.isCodePanePoint(msg.X, msg.Y) {
		m.clearMouseSelection()
		return m
	}
	point := m.clampMousePoint(msg.X, msg.Y)
	m.mouseSelecting = true
	m.mouseSelectMoved = false
	m.mouseSelectStart = point
	m.mouseSelectEnd = point
	m.mouseSelectLeft, m.mouseSelectRight = m.selectionPaneBounds(point.x)
	m.selectedText = ""
	return m
}

func (m Model) handleMouseMotion(msg tea.MouseMotionMsg) Model {
	if !m.mouseSelecting {
		return m
	}
	point := m.clampMousePoint(msg.X, msg.Y)
	m.mouseSelectMoved = m.mouseSelectMoved || point != m.mouseSelectStart
	m.mouseSelectEnd = point
	return m
}

func (m Model) handleMouseRelease(msg tea.MouseReleaseMsg) Model {
	if !m.mouseSelecting {
		return m
	}
	point := m.clampMousePoint(msg.X, msg.Y)
	m.mouseSelectMoved = m.mouseSelectMoved || point != m.mouseSelectStart
	m.mouseSelectEnd = point
	m.mouseSelecting = false
	if !m.mouseSelectMoved {
		m.selectedText = ""
		m.positionCodeRow(msg.Y)
		return m
	}
	m.mouseSelectMoved = false
	content := m.View().Content
	m.mouseSelectMoved = true
	m.selectedText = m.textFromMouseSelection(content)
	m.invalidateRender()
	return m
}

func (m Model) isCodePanePoint(x, y int) bool {
	if m.mode != normal || y < 5 || y >= m.height-1 {
		return false
	}
	if m.width < 90 {
		return m.focus == focusDiff
	}
	return x >= m.filePaneWidth()
}

func (m Model) isFilePanePoint(x, y int) bool {
	if y < 5 || y >= m.height-1 {
		return false
	}
	if m.width < 90 {
		return m.mode == normal && m.focus == focusFiles
	}
	return x < m.filePaneWidth()
}

func (m *Model) positionFileRow(screenY int) {
	index := m.fileScroll + screenY - 5
	m.focus = focusFiles
	if m.fileLayout == treeFiles {
		nodes := m.currentTreeNodes()
		if index < 0 || index >= len(nodes) {
			return
		}
		m.treeCursor = index
		if !nodes[index].directory {
			m.selectTreeFile(nodes[index])
		}
		return
	}
	if index < 0 || index >= len(m.repo.Files) {
		return
	}
	if m.file != index {
		m.file = index
		m.resetLineCursor()
	}
}

func (m *Model) positionCodeRow(screenY int) {
	visualRow := screenY - 5
	if visualRow < 0 {
		return
	}
	m.focus = focusDiff
	if m.sourcePath != "" {
		index := m.sourceScroll + visualRow
		if index >= 0 && index < len(m.sourceLines) {
			m.sourceLine = index
		}
		return
	}
	if m.view == split {
		index := m.splitScroll + visualRow
		if index >= 0 && index < len(m.currentSplitRows()) {
			m.splitCursor = index
			m.syncLineFromSplitCursor()
		}
		return
	}
	if index, ok := m.unifiedLineAtVisualRow(visualRow); ok {
		m.line = index
	}
}

func (m Model) unifiedLineAtVisualRow(target int) (int, bool) {
	lines := m.currentLines()
	index := clamp(m.lineScroll, 0, max(0, len(lines)-1)) + target
	if index >= 0 && index < len(lines) {
		return index, true
	}
	return 0, false
}

func (m *Model) clickFileSearchResult(x, y int) {
	width, height := min(72, m.width-4), min(18, m.height-4)
	box, left, top := m.modalLayout(m.renderSearch(), width, height)
	if x < left || x >= left+lipgloss.Width(box) || y < top+6 {
		return
	}
	index := m.searchTop + y - (top + 6)
	if index >= m.searchTop && index < min(m.searchTop+10, len(m.searchHits)) {
		m.searchAt = index
	}
}

func (m *Model) clickRepositorySearchResult(x, y int) {
	if !m.repoSearchReady || len(m.repoHits) == 0 {
		return
	}
	left := 0
	if m.width >= 90 {
		left = m.filePaneWidth()
	}
	if x < left || y < 3 {
		return
	}
	contentRow := y - 3
	used := 0
	for _, index := range m.repositorySearchWindow(max(3, m.height-4)) {
		height := len(m.repoHits[index].Context) + 1
		headerRow := 8 + used
		if contentRow >= headerRow && contentRow < headerRow+height {
			m.repoSearchAt = index
			return
		}
		used += height + 1
	}
}

func (m Model) clampMousePoint(x, y int) mousePoint {
	left, right := m.codePaneBounds()
	bottom := max(5, m.height-2)
	return mousePoint{x: clamp(x, left, max(left, right-1)), y: clamp(y, 5, bottom)}
}

func (m Model) codePaneBounds() (int, int) {
	left := 0
	if m.width >= 90 {
		left = m.filePaneWidth()
	}
	return left, max(left, m.width)
}

func (m Model) selectionPaneBounds(originX int) (int, int) {
	left, right := m.codePaneBounds()
	if m.sourcePath != "" || m.view != split || right-left < 4 {
		return left, right
	}
	// renderDiff reserves one cell of horizontal padding on each side before
	// renderSplit divides its content around a one-cell separator.
	contentWidth := right - left - 2
	half := max(12, (contentWidth-1)/2)
	divider := clamp(left+1+half, left, max(left, right-1))
	if originX <= divider {
		return left, divider
	}
	return min(right, divider+1), right
}

func (m *Model) clearMouseSelection() {
	m.mouseSelecting = false
	m.mouseSelectMoved = false
	m.mouseSelectLeft = 0
	m.mouseSelectRight = 0
	m.selectedText = ""
}

func (m Model) renderMouseSelection(content string) string {
	if !m.mouseSelectMoved || m.mode != normal {
		return content
	}
	lines := strings.Split(content, "\n")
	start, end := orderedMousePoints(m.mouseSelectStart, m.mouseSelectEnd)
	style := lipgloss.NewStyle().Reverse(true)
	if m.theme.color {
		style = lipgloss.NewStyle().Background(lipgloss.Color("#264F78")).Foreground(lipgloss.Color("#FFFFFF"))
	}
	for y := start.y; y <= end.y && y < len(lines); y++ {
		left, right := selectionColumns(start, end, y, lipgloss.Width(lines[y]), m.mouseSelectLeft, m.mouseSelectRight)
		if right <= left {
			continue
		}
		selected := xansi.Strip(xansi.Cut(lines[y], left, right))
		lines[y] = xansi.Cut(lines[y], 0, left) + style.Render(selected) + xansi.Cut(lines[y], right, lipgloss.Width(lines[y]))
	}
	return strings.Join(lines, "\n")
}

func (m Model) textFromMouseSelection(content string) string {
	lines := strings.Split(xansi.Strip(content), "\n")
	start, end := orderedMousePoints(m.mouseSelectStart, m.mouseSelectEnd)
	var selected []string
	for y := start.y; y <= end.y && y < len(lines); y++ {
		left, right := selectionColumns(start, end, y, lipgloss.Width(lines[y]), m.mouseSelectLeft, m.mouseSelectRight)
		if right <= left {
			continue
		}
		selected = append(selected, strings.TrimRight(xansi.Cut(lines[y], left, right), " "))
	}
	return strings.TrimSpace(strings.Join(selected, "\n"))
}

func orderedMousePoints(a, b mousePoint) (mousePoint, mousePoint) {
	if a.y > b.y || (a.y == b.y && a.x > b.x) {
		return b, a
	}
	return a, b
}

func selectionColumns(start, end mousePoint, y, width, paneLeft, paneRight int) (int, int) {
	left := clamp(paneLeft, 0, width)
	right := clamp(paneRight, left, width)
	if y == start.y {
		left = clamp(start.x, left, right)
	}
	if y == end.y {
		right = clamp(end.x+1, left, right)
	}
	return left, right
}

func (m *Model) scrollFiles(delta int) {
	count := len(m.repo.Files)
	if m.fileLayout == treeFiles {
		count = len(m.currentTreeNodes())
	}
	m.fileScroll = clamp(m.fileScroll+delta, 0, max(0, count-m.pageSize()))
}

func (m *Model) scrollCode(delta int) {
	if m.sourcePath != "" {
		m.sourceScroll = clamp(m.sourceScroll+delta, 0, max(0, len(m.sourceLines)-m.pageSize()))
		return
	}
	if m.view == split {
		m.splitScroll = clamp(m.splitScroll+delta, 0, max(0, len(m.currentSplitRows())-m.pageSize()))
		return
	}
	m.lineScroll = clamp(m.lineScroll+delta, 0, max(0, len(m.currentLines())-m.pageSize()))
}
