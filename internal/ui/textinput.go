package ui

import (
	"unicode"

	tea "charm.land/bubbletea/v2"
)

func (m *Model) setInput(text string) {
	m.input = text
	m.inputCursor = len([]rune(text))
}

func (m *Model) insertInput(text string) {
	runes := []rune(m.input)
	m.inputCursor = clamp(m.inputCursor, 0, len(runes))
	inserted := []rune(text)
	result := make([]rune, 0, len(runes)+len(inserted))
	result = append(result, runes[:m.inputCursor]...)
	result = append(result, inserted...)
	result = append(result, runes[m.inputCursor:]...)
	m.input = string(result)
	m.inputCursor += len(inserted)
}

func (m *Model) editInput(msg tea.KeyPressMsg) (handled, changed bool) {
	runes := []rune(m.input)
	m.inputCursor = clamp(m.inputCursor, 0, len(runes))
	switch msg.String() {
	case "left", "ctrl+b":
		m.inputCursor = max(0, m.inputCursor-1)
	case "right", "ctrl+f":
		m.inputCursor = min(len(runes), m.inputCursor+1)
	case "home", "ctrl+a":
		m.inputCursor = 0
	case "end", "ctrl+e":
		m.inputCursor = len(runes)
	case "alt+b":
		m.inputCursor = previousWordBoundary(runes, m.inputCursor)
	case "alt+f":
		m.inputCursor = nextWordBoundary(runes, m.inputCursor)
	case "backspace", "ctrl+h":
		if m.inputCursor > 0 {
			runes = append(runes[:m.inputCursor-1], runes[m.inputCursor:]...)
			m.inputCursor--
			m.input = string(runes)
			changed = true
		}
	case "delete", "ctrl+d":
		if m.inputCursor < len(runes) {
			runes = append(runes[:m.inputCursor], runes[m.inputCursor+1:]...)
			m.input = string(runes)
			changed = true
		}
	case "ctrl+u":
		if m.inputCursor > 0 {
			runes = runes[m.inputCursor:]
			m.inputCursor = 0
			m.input = string(runes)
			changed = true
		}
	case "ctrl+k":
		if m.inputCursor < len(runes) {
			m.input = string(runes[:m.inputCursor])
			changed = true
		}
	case "ctrl+w":
		start := previousWordBoundary(runes, m.inputCursor)
		if start < m.inputCursor {
			runes = append(runes[:start], runes[m.inputCursor:]...)
			m.inputCursor = start
			m.input = string(runes)
			changed = true
		}
	default:
		key := msg.Key()
		if key.Text == "" || key.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModMeta|tea.ModHyper|tea.ModSuper) != 0 {
			return false, false
		}
		m.insertInput(key.Text)
		return true, true
	}
	return true, changed
}

func (m Model) inputWithCursor() string {
	runes := []rune(m.input)
	cursor := clamp(m.inputCursor, 0, len(runes))
	if cursor == len(runes) {
		return m.input + m.theme.cursor.Render(" ")
	}
	return string(runes[:cursor]) + m.theme.cursor.Render(string(runes[cursor])) + string(runes[cursor+1:])
}

func previousWordBoundary(runes []rune, cursor int) int {
	cursor = clamp(cursor, 0, len(runes))
	for cursor > 0 && unicode.IsSpace(runes[cursor-1]) {
		cursor--
	}
	for cursor > 0 && !unicode.IsSpace(runes[cursor-1]) {
		cursor--
	}
	return cursor
}

func nextWordBoundary(runes []rune, cursor int) int {
	cursor = clamp(cursor, 0, len(runes))
	for cursor < len(runes) && !unicode.IsSpace(runes[cursor]) {
		cursor++
	}
	for cursor < len(runes) && unicode.IsSpace(runes[cursor]) {
		cursor++
	}
	return cursor
}
