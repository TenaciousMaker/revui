package ui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/TenaciousMaker/revui/internal/diff"
	"github.com/TenaciousMaker/revui/internal/gitrepo"
)

func TestNoColorThemeHasNoANSIColorSequences(t *testing.T) {
	theme := newThemeWithColor(false)
	rendered := strings.Join([]string{
		theme.canvas.Render("canvas"), theme.focus.Render("focus"),
		theme.added.Render("added"), theme.deleted.Render("deleted"), theme.hunk.Render("hunk"),
	}, " ")
	for _, sequence := range []string{"38;", "48;"} {
		if strings.Contains(rendered, sequence) {
			t.Fatalf("NO_COLOR rendering contains %q: %q", sequence, rendered)
		}
	}
}

func TestNoColorAppliesToCompleteView(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	root := t.TempDir()
	m, err := newTestModel(t, &gitrepo.Repository{
		Root: root, Branch: "feature", Base: "main",
		ReviewPath: filepath.Join(root, "review.json"), PreferencesPath: filepath.Join(root, "preferences.json"),
		Files: []diff.File{{Path: "main.go", Additions: 1, Lines: []diff.Line{{Kind: diff.Addition, Text: "return true", NewNumber: 1}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	content := updated.(Model).View().Content
	if strings.Contains(content, "38;") || strings.Contains(content, "48;") {
		t.Fatalf("NO_COLOR view contains RGB color sequences: %q", content)
	}
	if !strings.Contains(content, "+") || !strings.Contains(content, "DIFF") {
		t.Fatalf("NO_COLOR removed semantic labels: %q", content)
	}
}
