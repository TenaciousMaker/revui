package ui

import (
	"strings"
	"testing"

	"github.com/TenaciousMaker/revui/internal/diff"
)

func TestIntralineChangesFindsOnlyChangedTokens(t *testing.T) {
	oldText := "from package import get_pool"
	newText := "from package import get_pool, get_service_factory"
	oldSpans, newSpans := intralineChanges(oldText, newText)
	if len(oldSpans) != 0 {
		t.Fatalf("unchanged old tokens were emphasized: %#v", oldSpans)
	}
	if len(newSpans) != 1 || string([]rune(newText)[newSpans[0].start:newSpans[0].end]) != ", get_service_factory" {
		t.Fatalf("new spans = %#v", newSpans)
	}
}

func TestIntralineChangesSupportsMultipleRegions(t *testing.T) {
	oldText := "call(alpha, middle, omega)"
	newText := "call(beta, middle, final)"
	oldSpans, newSpans := intralineChanges(oldText, newText)
	if len(oldSpans) != 2 || len(newSpans) != 2 {
		t.Fatalf("old=%#v new=%#v", oldSpans, newSpans)
	}
}

func TestIntralineSpansIgnoreReformattingAcrossReplacementBlock(t *testing.T) {
	lines := []diff.Line{
		{Kind: diff.Deletion, Hunk: 1, Text: "const effectiveLimit = isFullpage", OldNumber: 292},
		{Kind: diff.Deletion, Hunk: 1, Text: "  ? fullpageLimit", OldNumber: 293},
		{Kind: diff.Deletion, Hunk: 1, Text: "  : widgetSettings.widgetLimit;", OldNumber: 294},
		{Kind: diff.Addition, Hunk: 1, Text: "const effectiveLimit = isFullpage ? fullpageLimit : settings.config.limit;", NewNumber: 281},
	}

	if got := selectedSpanText(lines[0].Text, intralineSpansForLine(lines, 0)); got != "" {
		t.Fatalf("unchanged first deleted line was emphasized: %q", got)
	}
	if got := selectedSpanText(lines[1].Text, intralineSpansForLine(lines, 1)); got != "" {
		t.Fatalf("reformatted ternary branch was emphasized: %q", got)
	}
	if got := selectedSpanText(lines[2].Text, intralineSpansForLine(lines, 2)); got != "widgetSettings.widgetLimit" {
		t.Fatalf("old meaningful change = %q", got)
	}
	if got := selectedSpanText(lines[3].Text, intralineSpansForLine(lines, 3)); got != "settings.config.limit" {
		t.Fatalf("new meaningful change = %q", got)
	}
}

func selectedSpanText(text string, spans []textSpan) string {
	runes := []rune(text)
	var selected []string
	for _, span := range spans {
		selected = append(selected, string(runes[span.start:span.end]))
	}
	return strings.Join(selected, "")
}
