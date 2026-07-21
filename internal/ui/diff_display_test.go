package ui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/TenaciousMaker/revui/internal/diff"
	"github.com/TenaciousMaker/revui/internal/gitrepo"
)

func TestVisibleDiffCanIgnoreWhitespaceOnlyReplacement(t *testing.T) {
	lines := []diff.Line{
		{Kind: diff.Meta, Text: "@@ -1 +1 @@", Hunk: 0},
		{Kind: diff.Deletion, Text: "value = oldValue", OldNumber: 1, Hunk: 0},
		{Kind: diff.Addition, Text: "value  =  oldValue", NewNumber: 1, Hunk: 0},
	}
	if got := buildVisibleDiffLines(lines, true); len(got) != 0 {
		t.Fatalf("whitespace-only hunk remained visible: %#v", got)
	}
	if got := buildVisibleDiffLines(lines, false); len(got) != len(lines) || got[2].OriginalIndex != 2 {
		t.Fatalf("unfiltered lines changed: %#v", got)
	}
}

func TestCollapsedGapExpandsFromKeyboardWithoutReloadingGit(t *testing.T) {
	repo := hunkExpansionTestRepository(t)
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	source := []byte("one\ntwo\nthree\nfour\nfive\nsix\nseven\n")
	m.repositories = stubSemanticRepository{oldSource: source, newSource: source}
	m.width, m.height, m.focus = 120, 24, focusDiff

	lines := m.currentLines()
	gap := collapsedLineIndex(lines)
	if gap < 0 || lines[gap].Collapsed != 4 || !strings.Contains(lines[gap].Text, "4 unchanged lines") {
		t.Fatalf("collapsed gap missing: %#v", lines)
	}
	// Hunk navigation lands on the next header. Expanding from there should
	// reveal the gap directly above it as well as selecting the gap itself.
	m.line = gap + 1
	updated, command := m.Update(tea.KeyPressMsg{Text: "x", Code: 'x'})
	m = updated.(Model)
	if command == nil {
		t.Fatal("x did not schedule the source read")
	}
	updated, _ = m.Update(command())
	m = updated.(Model)
	lines = m.currentLines()
	if collapsedLineIndex(lines) >= 0 {
		t.Fatalf("gap remained collapsed: %#v", lines)
	}
	for number, text := range []string{"three", "four", "five", "six"} {
		line := lines[gap+number]
		if line.Kind != diff.Context || line.Text != text || line.OldNumber != number+3 || line.NewNumber != number+3 {
			t.Fatalf("expanded line %d = %#v", number, line)
		}
	}
}

func TestCollapsedGapSourceReadHonorsCancellation(t *testing.T) {
	repo := hunkExpansionTestRepository(t)
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	stub := &cancellableHunkRepository{}
	m.repositories = stub
	m.focus = focusDiff
	m.line = collapsedLineIndex(m.currentLines())
	command := m.expandSelectedHunkGap()
	if command == nil {
		t.Fatal("gap expansion did not start")
	}
	m.cancelHunkExpansion()
	message := command()
	result, ok := message.(hunkExpansionMsg)
	if !ok || result.err == nil || !stub.cancelled {
		t.Fatalf("cancelled expansion result=%#v cancelled=%v", message, stub.cancelled)
	}
}

func TestCollapsedGapExpandsInsideChangesSinceReview(t *testing.T) {
	repo := hunkExpansionTestRepository(t)
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	source := []byte("one\ntwo\nthree\nfour\nfive\nsix\nseven\n")
	m.reviewWork.comparisonID = 4
	m.reviewWork.comparisonRepo = repo
	m.reviewWork.comparisonFile = 0
	m.reviewWork.comparisonPath = "app.go"
	m.reviewWork.comparison = &repo.Files[0]
	m.reviewWork.comparisonBefore = gitrepo.SourceSnapshot{Content: source, Exists: true, Available: true}
	m.reviewWork.comparisonCurrent = gitrepo.SourceSnapshot{Content: source, Exists: true, Available: true}
	m.focus = focusDiff
	m.line = collapsedLineIndex(m.currentLines())
	if command := m.expandSelectedHunkGap(); command != nil {
		t.Fatal("saved review sources should expand without another repository read")
	}
	if collapsedLineIndex(m.currentLines()) >= 0 {
		t.Fatal("changes-since-review gap remained collapsed")
	}
}

func TestWhitespaceFilteredDisplayDoesNotMislabelHiddenChangesAsContext(t *testing.T) {
	state := newHunkExpansionState()
	base := hunkExpansionTestRepository(t).Files[0].Lines
	got := state.linesFor(hunkDisplayKey{ignoreWhitespace: true}, base)
	if len(got) != len(base) || collapsedLineIndex(got) >= 0 {
		t.Fatalf("whitespace-filtered hunk topology was rewritten: %#v", got)
	}
}

type cancellableHunkRepository struct{ cancelled bool }

func (s *cancellableHunkRepository) Refresh(context.Context, string, string) (*gitrepo.Repository, error) {
	return nil, nil
}
func (s *cancellableHunkRepository) Search(context.Context, *gitrepo.Repository, string, int) ([]gitrepo.SearchMatch, error) {
	return nil, nil
}
func (s *cancellableHunkRepository) ReadSource(context.Context, *gitrepo.Repository, string) ([]byte, bool, error) {
	return nil, false, nil
}
func (s *cancellableHunkRepository) ReadPair(ctx context.Context, _ *gitrepo.Repository, _ diff.File) ([]byte, []byte, error) {
	if err := ctx.Err(); err != nil {
		s.cancelled = true
		return nil, nil, err
	}
	return nil, nil, nil
}

func hunkExpansionTestRepository(t *testing.T) *gitrepo.Repository {
	t.Helper()
	return &gitrepo.Repository{
		Root: t.TempDir(), Branch: "feature", Base: "main",
		Files: []diff.File{{Path: "app.go", Status: "M", Lines: []diff.Line{
			{Kind: diff.Meta, Text: "@@ -1,2 +1,2 @@", Hunk: 0},
			{Kind: diff.Context, Text: "one", OldNumber: 1, NewNumber: 1, Hunk: 0},
			{Kind: diff.Context, Text: "two", OldNumber: 2, NewNumber: 2, Hunk: 0},
			{Kind: diff.Meta, Text: "@@ -7 +7 @@", Hunk: 1},
			{Kind: diff.Context, Text: "seven", OldNumber: 7, NewNumber: 7, Hunk: 1},
		}}},
	}
}

func collapsedLineIndex(lines []diff.Line) int {
	for index, line := range lines {
		if line.Collapsed > 0 {
			return index
		}
	}
	return -1
}

func TestVisibleDiffKeepsReorderedLinesAcrossHunks(t *testing.T) {
	lines := []diff.Line{
		{Kind: diff.Meta, Text: "@@ -1 +1 @@", Hunk: 0},
		{Kind: diff.Deletion, Text: "import shared", OldNumber: 1, Hunk: 0},
		{Kind: diff.Meta, Text: "@@ -10 +10 @@", Hunk: 1},
		{Kind: diff.Addition, Text: "import shared", NewNumber: 10, Hunk: 1},
	}
	if got := buildVisibleDiffLines(lines, false); len(got) != len(lines) {
		t.Fatalf("reordered lines were hidden: %#v", got)
	}
}
