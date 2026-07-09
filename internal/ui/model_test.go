package ui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/mattwalker/revui/internal/diff"
	"github.com/mattwalker/revui/internal/gitrepo"
)

func TestResponsiveRenderAndFuzzySearch(t *testing.T) {
	repo := &gitrepo.Repository{
		Root: t.TempDir(), Branch: "feature/review", Base: "main",
		ReviewPath: filepath.Join(t.TempDir(), "review.json"),
		Files: []diff.File{
			{Path: "internal/parser/parser.go", Status: "M", Additions: 1, Lines: []diff.Line{{Kind: diff.Meta, Text: "@@ -1 +1 @@"}, {Kind: diff.Addition, Text: "func parse() {}", NewNumber: 1}}},
			{Path: "README.md", Status: "M", Deletions: 1, Lines: []diff.Line{{Kind: diff.Deletion, Text: "old", OldNumber: 1}}},
		},
	}
	m, err := New(repo)
	if err != nil {
		t.Fatal(err)
	}
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	m = updated.(Model)
	wide := m.View().Content
	for _, expected := range []string{"REVUI", "CHANGED FILES", "internal/parser/parser.go", "DIFF"} {
		if !strings.Contains(wide, expected) {
			t.Fatalf("wide render missing %q", expected)
		}
	}
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 72, Height: 24})
	m = updated.(Model)
	if got := m.View().Content; !strings.Contains(got, "CHANGED FILES") {
		t.Fatal("narrow file pane did not render")
	}
	m.input = "prsgo"
	m.updateSearch()
	if len(m.searchHits) != 1 || m.searchHits[0] != 0 {
		t.Fatalf("unexpected fuzzy search hits: %#v", m.searchHits)
	}
}

func TestSplitRowsPairsReplacementBlocks(t *testing.T) {
	lines := []diff.Line{
		{Kind: diff.Meta, Text: "@@"},
		{Kind: diff.Deletion, Text: "old one"},
		{Kind: diff.Deletion, Text: "old two"},
		{Kind: diff.Addition, Text: "new one"},
		{Kind: diff.Context, Text: "same"},
	}
	rows := splitRows(lines)
	if len(rows) != 4 || rows[1].old == nil || rows[1].new == nil || rows[2].old == nil || rows[2].new != nil {
		t.Fatalf("unexpected split rows: %#v", rows)
	}
}

func TestHunkHeaderCommentAnchorsToNearestChangedLine(t *testing.T) {
	repo := &gitrepo.Repository{
		Root:       t.TempDir(),
		Branch:     "feature",
		Base:       "main",
		ReviewPath: filepath.Join(t.TempDir(), "review.json"),
		Files: []diff.File{{
			Path: "app.go",
			Lines: []diff.Line{
				{Kind: diff.Meta, Text: "@@ -1 +1 @@"},
				{Kind: diff.Deletion, Text: "old", OldNumber: 1},
				{Kind: diff.Addition, Text: "new", NewNumber: 1},
			},
		}},
	}
	m, err := New(repo)
	if err != nil {
		t.Fatal(err)
	}
	m.line = 0
	anchor := m.anchorForSelection()
	if anchor.OldStart != 1 || anchor.Context != "old" {
		t.Fatalf("hunk comment was not anchored to the nearest line: %#v", anchor)
	}
}

func TestSplitRenderExpandsTabsWithoutWrappingRows(t *testing.T) {
	repo := &gitrepo.Repository{
		Root:       t.TempDir(),
		Branch:     "feature",
		Base:       "main",
		ReviewPath: filepath.Join(t.TempDir(), "review.json"),
		Files: []diff.File{{
			Path: "go.mod",
			Lines: []diff.Line{
				{Kind: diff.Meta, Text: "@@ -0,0 +1 @@"},
				{Kind: diff.Addition, Text: "\tgithub.com/charmbracelet/ultraviolet v0.0.0-20260416155717-489999b90468 // indirect", NewNumber: 1},
			},
		}},
	}
	m, err := New(repo)
	if err != nil {
		t.Fatal(err)
	}
	output := m.renderSplit(78, 10)
	if got := len(strings.Split(output, "\n")); got != 2 {
		t.Fatalf("split row wrapped into %d lines:\n%s", got, output)
	}
}
