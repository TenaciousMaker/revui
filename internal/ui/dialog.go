package ui

func (m Model) searchModalSize() (int, int) {
	return min(72, max(1, m.width-4)), min(18, max(1, m.height-2))
}

func (m Model) helpModalSize() (int, int) {
	return min(82, max(1, m.width-4)), min(28, max(1, m.height-2))
}

func (m Model) fileSearchPageSize() int {
	_, height := m.searchModalSize()
	// Title, query, separator, spacer, and controls use six rows.
	return clamp(modalContentHeight(height)-6, 1, 10)
}

func (m Model) helpPageSize() int {
	_, height := m.helpModalSize()
	contentHeight := modalContentHeight(height)
	if contentHeight > 1 {
		return contentHeight - 1
	}
	return contentHeight
}

func (m Model) helpLines() []string {
	width, _ := m.helpModalSize()
	return wrapANSI(m.renderHelpContent(), modalContentWidth(width))
}

func (m Model) helpScrollLimit() int {
	return max(0, len(m.helpLines())-m.helpPageSize())
}

func (m *Model) scrollHelp(delta int) {
	m.helpScroll = clamp(m.helpScroll+delta, 0, m.helpScrollLimit())
}

func (m *Model) clampHelpScroll() {
	m.helpScroll = clamp(m.helpScroll, 0, m.helpScrollLimit())
}
