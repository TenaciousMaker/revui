package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/TenaciousMaker/revui/internal/diff"
)

const maxWordWrapMetrics = 8192

type unifiedWrapHeightKey struct {
	line  diff.Line
	width int
}

type splitWrapHeightKey struct {
	old, new, meta          diff.Line
	oldSet, newSet, metaSet bool
	width                   int
}

type wordWrapMetrics struct {
	unified map[unifiedWrapHeightKey]int
	split   map[splitWrapHeightKey]int
}

func newWordWrapMetrics() *wordWrapMetrics {
	return &wordWrapMetrics{
		unified: make(map[unifiedWrapHeightKey]int),
		split:   make(map[splitWrapHeightKey]int),
	}
}

func wrapANSI(value string, width int) []string {
	return strings.Split(lipgloss.Wrap(value, max(1, width), ""), "\n")
}

func wrappedTextHeight(value string, width int) int {
	return len(wrapANSI(value, width))
}

func unifiedLineContentWidth(line diff.Line, width int) int {
	gutter := fmt.Sprintf(" %4s %4s %s ", number(line.OldNumber), number(line.NewNumber), line.Kind.Marker())
	return max(1, width-1-len(gutter))
}

func (m Model) unifiedLineVisualHeight(line diff.Line, width int) int {
	key := unifiedWrapHeightKey{line: line, width: width}
	if m.wrapMetrics != nil {
		if height, ok := m.wrapMetrics.unified[key]; ok {
			return height
		}
	}
	height := 0
	if line.Collapsed > 0 {
		height = wrappedTextHeight("  "+line.Text, width)
	} else {
		height = wrappedTextHeight(expandTabs(line.Text), unifiedLineContentWidth(line, width))
	}
	if m.wrapMetrics != nil {
		if len(m.wrapMetrics.unified) >= maxWordWrapMetrics {
			clear(m.wrapMetrics.unified)
		}
		m.wrapMetrics.unified[key] = height
	}
	return height
}

func splitCellContentWidth(line *diff.Line, width int, left bool) int {
	if line == nil {
		return max(1, width-1)
	}
	n := line.OldNumber
	if !left {
		n = line.NewNumber
	}
	if n == 0 {
		if left {
			n = line.NewNumber
		} else {
			n = line.OldNumber
		}
	}
	gutter := fmt.Sprintf("%3s %s ", number(n), line.Kind.Marker())
	return max(1, width-1-len(gutter))
}

func (m Model) splitRowVisualHeight(row splitRow, width int) int {
	key := splitWrapHeightKey{width: width}
	if row.old != nil {
		key.old, key.oldSet = *row.old, true
	}
	if row.new != nil {
		key.new, key.newSet = *row.new, true
	}
	if row.meta != nil {
		key.meta, key.metaSet = *row.meta, true
	}
	if m.wrapMetrics != nil {
		if height, ok := m.wrapMetrics.split[key]; ok {
			return height
		}
	}
	height := 0
	if row.meta != nil {
		content := " " + row.meta.Text
		if row.meta.Collapsed > 0 {
			content = "  " + row.meta.Text
		}
		height = wrappedTextHeight(content, width)
	} else {
		half := max(12, (width-1)/2)
		leftWidth, rightWidth := half, width-half-1
		leftHeight, rightHeight := 1, 1
		if row.old != nil {
			leftHeight = wrappedTextHeight(expandTabs(row.old.Text), splitCellContentWidth(row.old, leftWidth, true))
		}
		if row.new != nil {
			rightHeight = wrappedTextHeight(expandTabs(row.new.Text), splitCellContentWidth(row.new, rightWidth, false))
		}
		height = max(leftHeight, rightHeight)
	}
	if m.wrapMetrics != nil {
		if len(m.wrapMetrics.split) >= maxWordWrapMetrics {
			clear(m.wrapMetrics.split)
		}
		m.wrapMetrics.split[key] = height
	}
	return height
}

func (m Model) diffViewportWidth() int {
	width := m.width
	if m.width >= 90 {
		width = max(20, m.width-m.filePaneWidth())
	}
	return max(1, width-2)
}

func (m Model) unifiedRowsRemaining(limit int) int {
	lines := m.currentLines()
	if len(lines) == 0 {
		return 0
	}
	width := m.diffViewportWidth()
	start := clamp(m.lineScroll, 0, len(lines)-1)
	remaining := max(0, m.unifiedLineVisualHeight(lines[start], width)-m.lineWrapOffset)
	for index := start + 1; index < len(lines) && remaining <= limit; index++ {
		remaining += m.unifiedLineVisualHeight(lines[index], width)
	}
	return remaining
}

func (m Model) splitRowsRemaining(limit int) int {
	rows := m.currentSplitRows()
	if len(rows) == 0 {
		return 0
	}
	width := m.diffViewportWidth()
	start := clamp(m.splitScroll, 0, len(rows)-1)
	remaining := max(0, m.splitRowVisualHeight(rows[start], width)-m.splitWrapOffset)
	for index := start + 1; index < len(rows) && remaining <= limit; index++ {
		remaining += m.splitRowVisualHeight(rows[index], width)
	}
	return remaining
}

func (m *Model) advanceUnifiedVisualRow(direction int) bool {
	lines := m.currentLines()
	if len(lines) == 0 {
		m.lineScroll, m.lineWrapOffset = 0, 0
		return false
	}
	m.lineScroll = clamp(m.lineScroll, 0, len(lines)-1)
	height := m.unifiedLineVisualHeight(lines[m.lineScroll], m.diffViewportWidth())
	m.lineWrapOffset = clamp(m.lineWrapOffset, 0, max(0, height-1))
	if direction > 0 {
		if m.unifiedRowsRemaining(m.pageSize()) <= m.pageSize() {
			return false
		}
		if m.lineWrapOffset+1 < height {
			m.lineWrapOffset++
		} else {
			m.lineScroll++
			m.lineWrapOffset = 0
		}
		return true
	}
	if m.lineWrapOffset > 0 {
		m.lineWrapOffset--
		return true
	}
	if m.lineScroll == 0 {
		return false
	}
	m.lineScroll--
	m.lineWrapOffset = m.unifiedLineVisualHeight(lines[m.lineScroll], m.diffViewportWidth()) - 1
	return true
}

func (m *Model) advanceSplitVisualRow(direction int) bool {
	rows := m.currentSplitRows()
	if len(rows) == 0 {
		m.splitScroll, m.splitWrapOffset = 0, 0
		return false
	}
	m.splitScroll = clamp(m.splitScroll, 0, len(rows)-1)
	height := m.splitRowVisualHeight(rows[m.splitScroll], m.diffViewportWidth())
	m.splitWrapOffset = clamp(m.splitWrapOffset, 0, max(0, height-1))
	if direction > 0 {
		if m.splitRowsRemaining(m.pageSize()) <= m.pageSize() {
			return false
		}
		if m.splitWrapOffset+1 < height {
			m.splitWrapOffset++
		} else {
			m.splitScroll++
			m.splitWrapOffset = 0
		}
		return true
	}
	if m.splitWrapOffset > 0 {
		m.splitWrapOffset--
		return true
	}
	if m.splitScroll == 0 {
		return false
	}
	m.splitScroll--
	m.splitWrapOffset = m.splitRowVisualHeight(rows[m.splitScroll], m.diffViewportWidth()) - 1
	return true
}

func (m *Model) scrollUnifiedVisual(delta int) {
	direction := 1
	if delta < 0 {
		direction = -1
	}
	for range abs(delta) {
		if !m.advanceUnifiedVisualRow(direction) {
			break
		}
	}
}

func (m *Model) scrollSplitVisual(delta int) {
	direction := 1
	if delta < 0 {
		direction = -1
	}
	for range abs(delta) {
		if !m.advanceSplitVisualRow(direction) {
			break
		}
	}
}

func (m Model) unifiedLineAtWrappedVisualRow(target int) (int, bool) {
	lines := m.currentLines()
	if target < 0 || len(lines) == 0 {
		return 0, false
	}
	if m.width <= 0 {
		index := m.lineScroll + target
		return index, index >= 0 && index < len(lines)
	}
	width := m.diffViewportWidth()
	start := clamp(m.lineScroll, 0, len(lines)-1)
	position := -clamp(m.lineWrapOffset, 0, m.unifiedLineVisualHeight(lines[start], width)-1)
	for index := start; index < len(lines); index++ {
		height := m.unifiedLineVisualHeight(lines[index], width)
		if target >= position && target < position+height {
			return index, true
		}
		position += height
		if position > target {
			break
		}
	}
	return 0, false
}

func (m Model) splitRowAtWrappedVisualRow(target int) (int, bool) {
	rows := m.currentSplitRows()
	if target < 0 || len(rows) == 0 {
		return 0, false
	}
	if m.width <= 0 {
		index := m.splitScroll + target
		return index, index >= 0 && index < len(rows)
	}
	width := m.diffViewportWidth()
	start := clamp(m.splitScroll, 0, len(rows)-1)
	position := -clamp(m.splitWrapOffset, 0, m.splitRowVisualHeight(rows[start], width)-1)
	for index := start; index < len(rows); index++ {
		height := m.splitRowVisualHeight(rows[index], width)
		if target >= position && target < position+height {
			return index, true
		}
		position += height
		if position > target {
			break
		}
	}
	return 0, false
}

func (m *Model) ensureUnifiedCursorVisible() {
	lines := m.currentLines()
	if len(lines) == 0 {
		m.lineScroll, m.lineWrapOffset = 0, 0
		return
	}
	m.line = clamp(m.line, 0, len(lines)-1)
	m.lineScroll = clamp(m.lineScroll, 0, len(lines)-1)
	if m.line < m.lineScroll || (m.line == m.lineScroll && m.lineWrapOffset > 0) {
		m.lineScroll, m.lineWrapOffset = m.line, 0
		return
	}
	width := m.diffViewportWidth()
	distance := -m.lineWrapOffset
	for index := m.lineScroll; index < m.line; index++ {
		distance += m.unifiedLineVisualHeight(lines[index], width)
	}
	if distance >= m.pageSize() {
		m.lineScroll, m.lineWrapOffset = m.line, 0
		m.scrollUnifiedVisual(-(m.pageSize() - 1))
	}
}

func (m *Model) ensureSplitCursorVisible() {
	rows := m.currentSplitRows()
	if len(rows) == 0 {
		m.splitScroll, m.splitWrapOffset = 0, 0
		return
	}
	m.splitCursor = clamp(m.splitCursor, 0, len(rows)-1)
	m.splitScroll = clamp(m.splitScroll, 0, len(rows)-1)
	if m.splitCursor < m.splitScroll || (m.splitCursor == m.splitScroll && m.splitWrapOffset > 0) {
		m.splitScroll, m.splitWrapOffset = m.splitCursor, 0
		return
	}
	width := m.diffViewportWidth()
	distance := -m.splitWrapOffset
	for index := m.splitScroll; index < m.splitCursor; index++ {
		distance += m.splitRowVisualHeight(rows[index], width)
	}
	if distance >= m.pageSize() {
		m.splitScroll, m.splitWrapOffset = m.splitCursor, 0
		m.scrollSplitVisual(-(m.pageSize() - 1))
	}
}

func (m *Model) clampWrappedScroll() {
	lines := m.currentLines()
	if len(lines) == 0 {
		m.lineScroll, m.lineWrapOffset = 0, 0
	} else {
		m.lineScroll = clamp(m.lineScroll, 0, len(lines)-1)
		height := m.unifiedLineVisualHeight(lines[m.lineScroll], m.diffViewportWidth())
		m.lineWrapOffset = clamp(m.lineWrapOffset, 0, max(0, height-1))
		for m.unifiedRowsRemaining(m.pageSize()) < m.pageSize() && m.advanceUnifiedVisualRow(-1) {
		}
	}

	rows := m.currentSplitRows()
	if len(rows) == 0 {
		m.splitScroll, m.splitWrapOffset = 0, 0
	} else {
		m.splitScroll = clamp(m.splitScroll, 0, len(rows)-1)
		height := m.splitRowVisualHeight(rows[m.splitScroll], m.diffViewportWidth())
		m.splitWrapOffset = clamp(m.splitWrapOffset, 0, max(0, height-1))
		for m.splitRowsRemaining(m.pageSize()) < m.pageSize() && m.advanceSplitVisualRow(-1) {
		}
	}
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
