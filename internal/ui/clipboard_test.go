package ui

import (
	"path/filepath"
	"testing"

	"github.com/TenaciousMaker/revui/internal/diff"
	"github.com/TenaciousMaker/revui/internal/gitrepo"
)

func TestClipboardTextUsesMouseRangeAndCurrentLine(t *testing.T) {
	repo := &gitrepo.Repository{
		Root: t.TempDir(), Branch: "feature", Base: "main", ReviewPath: filepath.Join(t.TempDir(), "review.json"),
		Files: []diff.File{{Path: "service.go", Lines: []diff.Line{
			{Kind: diff.Deletion, Text: "\toldValue", OldNumber: 1},
			{Kind: diff.Addition, Text: "\tnewValue", NewNumber: 1},
			{Kind: diff.Context, Text: "\treturn value", OldNumber: 2, NewNumber: 2},
		}}},
	}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.line = 1
	if text, lines := m.clipboardText(); text != "File: service.go\nLocation: branch L1\n\n\tnewValue" || lines != 1 {
		t.Fatalf("current line text=%q lines=%d", text, lines)
	}
	m.selectFrom, m.line = 0, 2
	if text, lines := m.clipboardText(); text != "File: service.go\nLocation: branch L1-L2\nLocation: main L1\n\n\toldValue\n\tnewValue\n\treturn value" || lines != 3 {
		t.Fatalf("range text=%q lines=%d", text, lines)
	}
	m.selectedText = "  mouse\nselection  "
	m.mouseSelectStart = mousePoint{x: 20, y: 5}
	m.mouseSelectEnd = mousePoint{x: 30, y: 7}
	if text, lines := m.clipboardText(); text != "File: service.go\nLocation: branch L1-L2\nLocation: main L1\n\n  mouse\nselection  " || lines != 2 {
		t.Fatalf("mouse text=%q lines=%d", text, lines)
	}
}

func TestClipboardTextCopiesCurrentSourceLineWithLocation(t *testing.T) {
	m := Model{
		repo:             &gitrepo.Repository{Base: "origin/main"},
		contentPaneState: contentPaneState{sourcePath: "service.go", sourceLines: []string{"one", "two"}, sourceLine: 1},
	}
	if text, lines := m.clipboardText(); text != "File: service.go\nLocation: branch L2\n\ntwo" || lines != 1 {
		t.Fatalf("source text=%q lines=%d", text, lines)
	}
	m.sourceFromBase = true
	if text, _ := m.clipboardText(); text != "File: service.go\nLocation: origin/main L2\n\ntwo" {
		t.Fatalf("base source text=%q", text)
	}
}
