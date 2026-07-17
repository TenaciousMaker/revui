package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestWordNavigationAndEditing(t *testing.T) {
	m := Model{theme: newTheme(), searchState: searchState{input: "alpha  beta", inputCursor: len([]rune("alpha  beta"))}}
	if handled, changed := m.editInput(tea.KeyPressMsg{Code: 'b', Mod: tea.ModAlt}); !handled || changed || m.inputCursor != 7 {
		t.Fatalf("alt-left handled=%v changed=%v cursor=%d", handled, changed, m.inputCursor)
	}
	if handled, changed := m.editInput(tea.KeyPressMsg{Code: 'f', Mod: tea.ModAlt}); !handled || changed || m.inputCursor != 11 {
		t.Fatalf("alt-right handled=%v changed=%v cursor=%d", handled, changed, m.inputCursor)
	}
	m.inputCursor = 7
	if handled, changed := m.editInput(tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl}); !handled || !changed || m.input != "beta" || m.inputCursor != 0 {
		t.Fatalf("ctrl-w handled=%v changed=%v input=%q cursor=%d", handled, changed, m.input, m.inputCursor)
	}
}

func TestInputCursorDoesNotConsumeACharacterCell(t *testing.T) {
	m := Model{theme: newTheme(), searchState: searchState{input: "workspace", inputCursor: 4}}
	if got := m.inputWithCursor(); got == "workspace" || len([]rune(stripANSI(got))) != len([]rune(m.input)) {
		t.Fatalf("cursor rendering changed visible width: %q", got)
	}
}

func stripANSI(value string) string {
	// Lip Gloss emits CSI SGR sequences around the reversed cursor cell.
	inEscape := false
	result := make([]rune, 0, len(value))
	for _, r := range value {
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if r == 'm' {
				inEscape = false
			}
			continue
		}
		result = append(result, r)
	}
	return string(result)
}
