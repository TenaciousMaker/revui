package ui

import (
	"testing"

	"github.com/TenaciousMaker/revui/internal/diff"
)

func TestVisibleDiffCanIgnoreWhitespaceOnlyReplacement(t *testing.T) {
	lines := []diff.Line{
		{Kind: diff.Meta, Text: "@@ -1 +1 @@", Hunk: 0},
		{Kind: diff.Deletion, Text: "value = oldValue", OldNumber: 1, Hunk: 0},
		{Kind: diff.Addition, Text: "value  =  oldValue", NewNumber: 1, Hunk: 0},
	}
	if got := buildVisibleDiffLines(lines, true, false); len(got) != 0 {
		t.Fatalf("whitespace-only hunk remained visible: %#v", got)
	}
	if got := buildVisibleDiffLines(lines, false, false); len(got) != len(lines) || got[2].OriginalIndex != 2 {
		t.Fatalf("unfiltered lines changed: %#v", got)
	}
}

func TestVisibleDiffCanIgnoreMovedLinesAcrossHunks(t *testing.T) {
	lines := []diff.Line{
		{Kind: diff.Meta, Text: "@@ -1 +1 @@", Hunk: 0},
		{Kind: diff.Deletion, Text: "import shared", OldNumber: 1, Hunk: 0},
		{Kind: diff.Meta, Text: "@@ -10 +10 @@", Hunk: 1},
		{Kind: diff.Addition, Text: "import shared", NewNumber: 10, Hunk: 1},
	}
	if got := buildVisibleDiffLines(lines, false, true); len(got) != 0 {
		t.Fatalf("moved-only hunks remained visible: %#v", got)
	}
}

func TestMovedMatchingCanNormalizeWhitespaceWithoutHidingRealChanges(t *testing.T) {
	lines := []diff.Line{
		{Kind: diff.Meta, Text: "@@", Hunk: 0},
		{Kind: diff.Deletion, Text: "  import shared", Hunk: 0},
		{Kind: diff.Addition, Text: "const realChange = true", Hunk: 0},
		{Kind: diff.Meta, Text: "@@", Hunk: 1},
		{Kind: diff.Addition, Text: "import   shared", Hunk: 1},
	}
	got := buildVisibleDiffLines(lines, true, true)
	if len(got) != 2 || got[0].Kind != diff.Meta || got[1].Text != "const realChange = true" || got[1].OriginalIndex != 2 {
		t.Fatalf("combined filters = %#v", got)
	}
}
