package ui

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/styles"
	uv "github.com/charmbracelet/ultraviolet"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/TenaciousMaker/revui/internal/config"
	"github.com/TenaciousMaker/revui/internal/diff"
	"github.com/TenaciousMaker/revui/internal/gitrepo"
	"github.com/TenaciousMaker/revui/internal/review"
	"github.com/TenaciousMaker/revui/internal/watcher"
)

type stubRepositoryWatcher struct {
	events chan watcher.Event
	closed bool
}

func lastVisibleColumn(text, needle string) int {
	index := strings.LastIndex(text, needle)
	if index < 0 {
		return -1
	}
	return lipgloss.Width(text[:index])
}

func (w *stubRepositoryWatcher) Events() <-chan watcher.Event { return w.events }
func (w *stubRepositoryWatcher) Close() error {
	w.closed = true
	return nil
}

func TestResponsiveRenderAndFuzzySearch(t *testing.T) {
	repo := &gitrepo.Repository{
		Root: t.TempDir(), Branch: "feature/review", Base: "main",
		ReviewPath: filepath.Join(t.TempDir(), "review.json"),
		Files: []diff.File{
			{Path: "internal/parser/parser.go", Status: "M", Additions: 1, Lines: []diff.Line{{Kind: diff.Meta, Text: "@@ -1 +1 @@"}, {Kind: diff.Addition, Text: "func parse() {}", NewNumber: 1}}},
			{Path: "README.md", Status: "M", Deletions: 1, Lines: []diff.Line{{Kind: diff.Deletion, Text: "old", OldNumber: 1}}},
		},
	}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	m = updated.(Model)
	wide := m.View().Content
	for _, expected := range []string{"REVUI", "FILES", "internal/parser/parser.go", "DIFF"} {
		if !strings.Contains(wide, expected) {
			t.Fatalf("wide render missing %q", expected)
		}
	}
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 72, Height: 24})
	m = updated.(Model)
	if got := m.View().Content; !strings.Contains(got, "FILES") {
		t.Fatal("narrow file pane did not render")
	}
	m.input = "prsgo"
	m.updateSearch()
	if len(m.searchHits) != 1 || m.searchHits[0] != 0 {
		t.Fatalf("unexpected fuzzy search hits: %#v", m.searchHits)
	}
}

func TestHeaderPlacesPurpleLiveIndicatorAfterSolidCounts(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	repo := &gitrepo.Repository{
		Root: t.TempDir(), Branch: "feature", Base: "main", ReviewPath: filepath.Join(t.TempDir(), "review.json"),
		Files: []diff.File{{Path: "service.go", Additions: 128, Deletions: 34}},
	}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.width = 100
	m.watcher = &stubRepositoryWatcher{events: make(chan watcher.Event)}
	header := m.renderHeader()
	plain := xansi.Strip(header)
	countsAt, liveAt := strings.Index(plain, "+128  -34"), strings.Index(plain, "● live")
	if countsAt < 0 || liveAt < 0 || liveAt < countsAt {
		t.Fatalf("header stats are not ordered as counts then live: %q", plain)
	}
	buffer := uv.NewScreenBuffer(100, lipgloss.Height(header))
	uv.NewStyledString(header).Draw(buffer, buffer.Bounds())
	red, green, blue, _ := buffer.CellAt(liveAt, 0).Style.Fg.RGBA()
	if got := fmt.Sprintf("#%02X%02X%02X", red>>8, green>>8, blue>>8); got != "#D2A8FF" {
		t.Fatalf("live indicator colour = %s, want #D2A8FF", got)
	}
	repo.Files[0].Deletions = 0
	if zero := xansi.Strip(m.renderHeader()); strings.Contains(zero, "-0") {
		t.Fatalf("header rendered a zero deletion count: %q", zero)
	}
}

func TestRealtimeWatcherCoalescesRefreshesAndPreservesCursor(t *testing.T) {
	repo := &gitrepo.Repository{
		Root: t.TempDir(), Branch: "feature", Base: "main", ReviewPath: filepath.Join(t.TempDir(), "review.json"),
		Files: []diff.File{{Path: "service.go", Lines: []diff.Line{
			{Kind: diff.Context, Text: "one", OldNumber: 1, NewNumber: 1},
			{Kind: diff.Addition, Text: "two", NewNumber: 2},
		}}},
		AllPaths: []string{"service.go"},
	}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height, m.focus, m.line = 120, 24, focusDiff, 1
	stub := &stubRepositoryWatcher{events: make(chan watcher.Event, 1)}
	m.watcher = stub
	if m.Init() == nil {
		t.Fatal("watch-enabled model returned no initial watch command")
	}

	updated, cmd := m.Update(repositoryWatchMsg{})
	m = updated.(Model)
	if !m.watchRefreshRunning || cmd == nil {
		t.Fatalf("first watch event running=%v cmd=%v", m.watchRefreshRunning, cmd)
	}
	updated, _ = m.Update(repositoryWatchMsg{})
	m = updated.(Model)
	if !m.watchRefreshPending {
		t.Fatal("second watch event was not coalesced while refresh was running")
	}

	refreshed := *repo
	refreshed.Files = []diff.File{{Path: "service.go", Lines: []diff.Line{
		{Kind: diff.Context, Text: "one", OldNumber: 1, NewNumber: 1},
		{Kind: diff.Addition, Text: "two updated", NewNumber: 2},
		{Kind: diff.Addition, Text: "three", NewNumber: 3},
	}}}
	updated, cmd = m.Update(refreshMsg{repo: &refreshed, automatic: true, id: m.refreshID})
	m = updated.(Model)
	if m.line != 1 || !m.watchRefreshRunning || m.watchRefreshPending || cmd == nil {
		t.Fatalf("coalesced refresh line=%d running=%v pending=%v cmd=%v", m.line, m.watchRefreshRunning, m.watchRefreshPending, cmd)
	}
	updated, cmd = m.Update(refreshMsg{repo: &refreshed, automatic: true, id: m.refreshID})
	m = updated.(Model)
	if m.watchRefreshRunning || cmd != nil || !strings.Contains(m.status, "Updated automatically") {
		t.Fatalf("final refresh running=%v status=%q cmd=%v", m.watchRefreshRunning, m.status, cmd)
	}

	updated, cmd = m.Update(tea.KeyPressMsg{Text: "q", Code: 'q'})
	_ = updated
	if cmd == nil || !stub.closed {
		t.Fatalf("quit did not close watcher: closed=%v cmd=%v", stub.closed, cmd)
	}
}

func TestDialogOverlayPreservesReviewAndHugsContent(t *testing.T) {
	repo := &gitrepo.Repository{
		Root: t.TempDir(), Branch: "feature", Base: "main", ReviewPath: filepath.Join(t.TempDir(), "review.json"),
		Files: []diff.File{{Path: "service.go", Lines: []diff.Line{{Kind: diff.Addition, Text: "func changed() {}", NewNumber: 1}}}},
	}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height = 120, 30
	box, x, y := m.modalLayout("DIALOG CONTENT", 70, 22)
	if got := lipgloss.Height(box); got >= 22 {
		t.Fatalf("short dialog was forced to maximum height: %d", got)
	}
	if x != (m.width-lipgloss.Width(box))/2 || y != (m.height-lipgloss.Height(box))/2 {
		t.Fatalf("dialog is not centered from rendered dimensions: x=%d y=%d", x, y)
	}
	overlaid := xansi.Strip(m.overlay(m.View().Content, "DIALOG CONTENT", 70, 22))
	for _, expected := range []string{"FILES", "service.go", "DIFF", "DIALOG CONTENT"} {
		if !strings.Contains(overlaid, expected) {
			t.Fatalf("dialog overlay lost %q from the composed view:\n%s", expected, overlaid)
		}
	}
}

func TestRepositorySearchRendersChunksAndOpensExactSourceLine(t *testing.T) {
	root := t.TempDir()
	path := "src/service.go"
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "package service\n\nfunc SharedMethod() {}\n\nfunc Call() { SharedMethod() }\n"
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(path)), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	repo := &gitrepo.Repository{
		Root: root, Branch: "feature", Base: "main", ReviewPath: filepath.Join(root, "review.json"),
		Files: []diff.File{{
			Path: path,
			Lines: []diff.Line{
				{Kind: diff.Meta, Text: "@@ -3,3 +3,3 @@", Hunk: 0},
				{Kind: diff.Context, Text: "func SharedMethod() {}", OldNumber: 3, NewNumber: 3, Hunk: 0},
				{Kind: diff.Addition, Text: "func Call() { SharedMethod() }", NewNumber: 5, Hunk: 0},
			},
		}},
	}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height = 140, 34
	m.mode, m.input, m.repoSearchReady = searchingRepository, "SharedMethod", true
	m.repoHits = []gitrepo.SearchMatch{{
		Path: path, Line: 5, Text: "func Call() { SharedMethod() }",
		Context: []gitrepo.SearchLine{
			{Number: 4, Text: ""},
			{Number: 5, Text: "func Call() { SharedMethod() }", Match: true},
		},
	}}

	plain := xansi.Strip(m.renderRepositorySearch(100, 28))
	for _, expected := range []string{"FIND IN REPOSITORY", "src/service.go:5", "4 │", "5 │", "SharedMethod"} {
		if !strings.Contains(plain, expected) {
			t.Fatalf("repository search is missing %q:\n%s", expected, plain)
		}
	}

	updated, _ := m.handleRepositorySearch(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)
	if m.mode != normal || m.sourcePath != path || m.sourceLine != 4 {
		t.Fatalf("search jump opened mode=%v path=%q line=%d", m.mode, m.sourcePath, m.sourceLine)
	}
	source := xansi.Strip(m.renderDiff(100, 12))
	if !strings.Contains(source, "SOURCE") || !strings.Contains(source, "› 5 │") {
		t.Fatalf("source preview did not select the exact match:\n%s", source)
	}

	m.jumpSourceToDiff()
	if m.sourcePath != "" || m.file != 0 || m.line != 2 {
		t.Fatalf("diff jump selected source=%q file=%d line=%d", m.sourcePath, m.file, m.line)
	}
}

func TestRepositorySearchAcceptsBracketedPaste(t *testing.T) {
	repo := &gitrepo.Repository{
		Root: t.TempDir(), Branch: "feature", Base: "main", ReviewPath: filepath.Join(t.TempDir(), "review.json"),
	}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.mode = searchingRepository
	m.input = "find "
	m.inputCursor = len([]rune(m.input))
	m.repoSearchReady = true
	m.repoHits = []gitrepo.SearchMatch{{Path: "service.go", Line: 1}}

	updated, _ := m.Update(tea.PasteMsg{Content: "sharedMethod"})
	m = updated.(Model)
	if m.input != "find sharedMethod" {
		t.Fatalf("repository search input after paste = %q, want %q", m.input, "find sharedMethod")
	}
	if m.repoSearchReady || len(m.repoHits) != 0 {
		t.Fatalf("paste did not invalidate prior results: ready=%v hits=%d", m.repoSearchReady, len(m.repoHits))
	}
}

func TestSearchFieldsSupportCursorMovementAndCtrlU(t *testing.T) {
	repo := &gitrepo.Repository{
		Root: t.TempDir(), Branch: "feature", Base: "main", ReviewPath: filepath.Join(t.TempDir(), "review.json"),
		Files: []diff.File{{Path: "abcdef.go"}, {Path: "Xef.go"}},
	}
	for _, mode := range []mode{searchingRepository, searching} {
		m, err := newTestModel(t, repo)
		if err != nil {
			t.Fatal(err)
		}
		m.mode = mode
		m.input = "abcdef"
		m.inputCursor = len([]rune(m.input))
		for _, key := range []tea.KeyPressMsg{
			{Code: tea.KeyLeft},
			{Code: tea.KeyLeft},
			{Code: 'u', Mod: tea.ModCtrl},
			{Text: "X", Code: 'X', Mod: tea.ModShift},
		} {
			updated, _ := m.Update(key)
			m = updated.(Model)
		}
		if m.input != "Xef" {
			t.Fatalf("mode %v input after left, left, ctrl+u, X = %q, want Xef", mode, m.input)
		}
	}
}

func TestSearchInputRendersAndPastesAtCursor(t *testing.T) {
	m := Model{theme: newTheme()}
	m.setInput("a界c")
	m.inputCursor = 2
	rendered := m.inputWithCursor()
	if got := xansi.Strip(rendered); got != "a界c" {
		t.Fatalf("cursor changed input text to %q", got)
	}
	if got, want := lipgloss.Width(rendered), lipgloss.Width("a界c"); got != want {
		t.Fatalf("cursor changed input width to %d, want %d", got, want)
	}
	if rendered == "a界c" {
		t.Fatal("cursor character was not visibly styled")
	}
	m.insertInput("β")
	if m.input != "a界βc" || m.inputCursor != 3 {
		t.Fatalf("paste at cursor produced input=%q cursor=%d", m.input, m.inputCursor)
	}
	if handled, changed := m.editInput(tea.KeyPressMsg{Code: tea.KeyRight}); !handled || changed || m.inputCursor != 4 {
		t.Fatalf("right arrow handled=%v changed=%v cursor=%d", handled, changed, m.inputCursor)
	}
	if handled, changed := m.editInput(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}); !handled || !changed || m.input != "" || m.inputCursor != 0 {
		t.Fatalf("ctrl+u handled=%v changed=%v input=%q cursor=%d", handled, changed, m.input, m.inputCursor)
	}
}

func TestFullFileToggleOpensAtDiffLineAndReturns(t *testing.T) {
	root := t.TempDir()
	var source strings.Builder
	for line := 1; line <= 12; line++ {
		fmt.Fprintf(&source, "line %d\n", line)
	}
	if err := os.WriteFile(filepath.Join(root, "service.go"), []byte(source.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	repo := &gitrepo.Repository{
		Root: root, Branch: "feature", Base: "main", ReviewPath: filepath.Join(root, "review.json"),
		Files: []diff.File{{Path: "service.go", Status: "M", Lines: []diff.Line{
			{Kind: diff.Meta, Text: "@@ -7,2 +7,2 @@"},
			{Kind: diff.Addition, Text: "line 8", NewNumber: 8},
		}}},
	}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height, m.focus, m.line = 120, 24, focusDiff, 1
	updated, _ := m.Update(tea.KeyPressMsg{Text: "o", Code: 'o'})
	m = updated.(Model)
	if m.sourcePath != "service.go" || m.sourceLine != 7 || m.sourceFromBase {
		t.Fatalf("full source path=%q line=%d fromBase=%v", m.sourcePath, m.sourceLine, m.sourceFromBase)
	}
	plain := xansi.Strip(m.renderDiff(80, 18))
	for _, expected := range []string{"SOURCE", "service.go", "line 3", "›  8 │ line 8"} {
		if !strings.Contains(plain, expected) {
			t.Fatalf("full source is missing %q:\n%s", expected, plain)
		}
	}

	updated, _ = m.Update(tea.KeyPressMsg{Text: "o", Code: 'o'})
	m = updated.(Model)
	if m.sourcePath != "" || !strings.Contains(xansi.Strip(m.renderDiff(80, 18)), "DIFF  UNIFIED") {
		t.Fatalf("second o did not return to diff: source=%q", m.sourcePath)
	}
}

func TestRepositorySearchKeepsFittingResultsInViewport(t *testing.T) {
	repo := &gitrepo.Repository{
		Root: t.TempDir(), Branch: "feature", Base: "main", ReviewPath: filepath.Join(t.TempDir(), "review.json"),
	}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height = 120, 24
	m.mode, m.input, m.repoSearchReady = searchingRepository, "SharedMethod", true
	for index := 0; index < 3; index++ {
		m.repoHits = append(m.repoHits, gitrepo.SearchMatch{
			Path: fmt.Sprintf("src/result%d.go", index), Line: index + 1,
			Context: []gitrepo.SearchLine{{Number: index + 1, Text: "SharedMethod()", Match: true}},
		})
	}

	updated, _ := m.handleRepositorySearch(tea.KeyPressMsg{Code: tea.KeyDown})
	m = updated.(Model)
	const paneHeight = 20
	rendered := m.renderRepositorySearch(80, paneHeight)
	plain := xansi.Strip(rendered)
	for _, expected := range []string{"src/result0.go", "●  src/result1.go", "src/result2.go", "enter open source"} {
		if !strings.Contains(plain, expected) {
			t.Fatalf("search viewport dropped %q after moving selection:\n%s", expected, plain)
		}
	}
	if got := lipgloss.Height(rendered); got > paneHeight {
		t.Fatalf("search pane height = %d, want <= %d", got, paneHeight)
	}

	m.repoHits = append(m.repoHits, gitrepo.SearchMatch{
		Path: "src/result3.go", Line: 4,
		Context: []gitrepo.SearchLine{{Number: 4, Text: "SharedMethod()", Match: true}},
	})
	for range 2 {
		updated, _ = m.handleRepositorySearch(tea.KeyPressMsg{Code: tea.KeyDown})
		m = updated.(Model)
	}
	plain = xansi.Strip(m.renderRepositorySearch(80, paneHeight))
	if strings.Contains(plain, "src/result0.go") || !strings.Contains(plain, "●  src/result3.go") || m.repoSearchTop != 1 {
		t.Fatalf("search window did not advance exactly once at the bottom boundary (top=%d):\n%s", m.repoSearchTop, plain)
	}
}

func TestFileTreeBuildsHierarchyAndCollapsesFolders(t *testing.T) {
	files := []diff.File{
		{Path: "src/app.go"},
		{Path: "src/lib/util.go"},
		{Path: "README.md"},
	}
	nodes := buildFileTree(files, map[string]bool{})
	want := []struct {
		name      string
		depth     int
		directory bool
	}{
		{"src", 0, true},
		{"lib", 1, true},
		{"util.go", 2, false},
		{"app.go", 1, false},
		{"README.md", 0, false},
	}
	if len(nodes) != len(want) {
		t.Fatalf("got %d tree nodes, want %d: %#v", len(nodes), len(want), nodes)
	}
	for i, expected := range want {
		if nodes[i].name != expected.name || nodes[i].depth != expected.depth || nodes[i].directory != expected.directory {
			t.Fatalf("node %d = %#v, want %#v", i, nodes[i], expected)
		}
	}
	collapsed := buildFileTree(files, map[string]bool{"src": true})
	if len(collapsed) != 2 || collapsed[0].name != "src" || collapsed[1].name != "README.md" {
		t.Fatalf("unexpected collapsed tree: %#v", collapsed)
	}
}

func TestTreeTogglePreservesFileAndNavigatesVisibleNodes(t *testing.T) {
	repo := &gitrepo.Repository{
		Root:       t.TempDir(),
		Branch:     "feature",
		Base:       "main",
		ReviewPath: filepath.Join(t.TempDir(), "review.json"),
		Files: []diff.File{
			{Path: "src/app.go", Lines: []diff.Line{{Kind: diff.Addition, Text: "app", NewNumber: 1}}},
			{Path: "src/lib/util.go", Lines: []diff.Line{{Kind: diff.Addition, Text: "util", NewNumber: 1}}},
			{Path: "README.md", Lines: []diff.Line{{Kind: diff.Addition, Text: "readme", NewNumber: 1}}},
		},
	}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.file = 1
	m.toggleFileLayout()
	if m.fileLayout != treeFiles || m.currentTreeNodes()[m.treeCursor].fileIndex != 1 {
		t.Fatalf("tree toggle lost selected file: cursor=%d file=%d", m.treeCursor, m.file)
	}
	m.moveTreeCursor(1)
	if m.file != 0 {
		t.Fatalf("moving to the next visible file selected index %d, want 0", m.file)
	}
	m.treeCursor = 0
	if !m.activateTreeNode() || !m.collapsed["src"] {
		t.Fatal("activating a folder did not collapse it")
	}
	if got := len(m.currentTreeNodes()); got != 2 {
		t.Fatalf("collapsed tree has %d visible nodes, want 2", got)
	}
	m.file = 1
	m.syncTreeCursorToFile()
	selected := m.currentTreeNodes()[m.treeCursor]
	if selected.directory || selected.fileIndex != 1 || m.collapsed["src"] {
		t.Fatalf("syncing to a hidden file did not expand and select it: %#v", selected)
	}
}

func TestViewPreferencesRestoreAndUpdateAcrossLaunches(t *testing.T) {
	root := t.TempDir()
	preferencesPath := filepath.Join(root, "preferences.json")
	if err := config.Save(preferencesPath, config.Preferences{
		FileLayout: "tree", FileScope: "all", WideFiles: true, DiffView: "split", IgnoreWhitespace: true, IgnoreMoved: true, SemanticReflow: true,
	}); err != nil {
		t.Fatal(err)
	}
	repo := &gitrepo.Repository{
		Root: root, Branch: "feature", Base: "main",
		ReviewPath: filepath.Join(root, "review.json"), PreferencesPath: preferencesPath,
		Files: []diff.File{{Path: "service.go", Lines: []diff.Line{{Kind: diff.Addition, Text: "new", NewNumber: 1}}}},
	}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	if m.fileLayout != treeFiles || m.fileScope != allRepositoryFiles || !m.wideFiles || m.view != split || !m.ignoreWhitespace || !m.ignoreMoved || !m.semanticReflow {
		t.Fatalf("preferences were not restored: layout=%v scope=%v wide=%v view=%v whitespace=%v moved=%v reflow=%v", m.fileLayout, m.fileScope, m.wideFiles, m.view, m.ignoreWhitespace, m.ignoreMoved, m.semanticReflow)
	}

	if err := config.Save(preferencesPath, config.Preferences{}); err != nil {
		t.Fatal(err)
	}
	m, err = newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.width = 140
	for _, key := range []string{"t", "A", "w", "s", "i", "m", "e"} {
		updated, _ := m.Update(tea.KeyPressMsg{Text: key, Code: rune(key[0])})
		m = updated.(Model)
	}
	got, err := config.Load(preferencesPath)
	if err != nil {
		t.Fatal(err)
	}
	if got.FileLayout != "tree" || got.FileScope != "context" || !got.WideFiles || got.DiffView != "split" || !got.IgnoreWhitespace || !got.IgnoreMoved || !got.SemanticReflow {
		t.Fatalf("updated preferences were not persisted: %#v", got)
	}
}

func TestFileScopeCyclesContextAllAndChanged(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "other"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "context.go"), []byte("package context\n\nfunc Existing() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "other", "unrelated.go"), []byte("package other\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	repo := &gitrepo.Repository{
		Root: root, Branch: "feature", Base: "main", ReviewPath: filepath.Join(root, "review.json"),
		Files:    []diff.File{{Path: "src/changed.go", Status: "M", Additions: 1, Lines: []diff.Line{{Kind: diff.Addition, Text: "func Changed() {}", NewNumber: 1}}}},
		AllPaths: []string{"other/unrelated.go", "src/changed.go", "src/context.go"},
	}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height = 120, 24
	updated, _ := m.Update(tea.KeyPressMsg{Text: "A", Code: 'A', Mod: tea.ModShift})
	m = updated.(Model)
	if m.fileScope != contextFiles || m.fileLayout != treeFiles {
		t.Fatalf("context toggle selected scope=%v layout=%v", m.fileScope, m.fileLayout)
	}
	nodes := m.currentTreeNodes()
	unchangedAt := -1
	for index, node := range nodes {
		if node.path == "src/context.go" {
			unchangedAt = index
			if node.fileIndex != -1 {
				t.Fatalf("unchanged node has changed-file index %d", node.fileIndex)
			}
		}
	}
	if unchangedAt < 0 {
		t.Fatalf("context tree is missing unchanged sibling: %#v", nodes)
	}
	for _, node := range nodes {
		if node.path == "other/unrelated.go" {
			t.Fatalf("context tree included unrelated file: %#v", nodes)
		}
	}
	m.treeCursor = unchangedAt
	m.moveTreeCursor(0)
	if m.sourcePath != "src/context.go" || !strings.Contains(strings.Join(m.sourceLines, "\n"), "func Existing") {
		t.Fatalf("unchanged source did not open: path=%q lines=%q", m.sourcePath, m.sourceLines)
	}
	plain := xansi.Strip(m.View().Content)
	for _, expected := range []string{"TREE  CONTEXT", "context.go", "SOURCE", "func Existing"} {
		if !strings.Contains(plain, expected) {
			t.Fatalf("all-files view is missing %q:\n%s", expected, plain)
		}
	}

	updated, _ = m.Update(tea.KeyPressMsg{Text: "A", Code: 'A', Mod: tea.ModShift})
	m = updated.(Model)
	if m.fileScope != allRepositoryFiles {
		t.Fatalf("second scope = %v, want all", m.fileScope)
	}
	foundUnrelated := false
	for _, node := range m.currentTreeNodes() {
		foundUnrelated = foundUnrelated || node.path == "other/unrelated.go"
	}
	if !foundUnrelated {
		t.Fatalf("all-files tree is missing unrelated source: %#v", m.currentTreeNodes())
	}

	updated, _ = m.Update(tea.KeyPressMsg{Text: "A", Code: 'A', Mod: tea.ModShift})
	m = updated.(Model)
	if m.fileScope != changedFiles || m.sourcePath != "" || len(m.currentTreeNodes()) != 2 {
		t.Fatalf("changed-only restore scope=%v source=%q nodes=%#v", m.fileScope, m.sourcePath, m.currentTreeNodes())
	}
}

func TestAllFilesTreeCompactsUnchangedDirectoryChains(t *testing.T) {
	files := []diff.File{{Path: "src/changed.go"}}
	paths := []string{"generated/client/models/item.go", "src/changed.go"}
	nodes := buildFileTreeView(files, paths, allRepositoryFiles, map[string]bool{})
	var compact *fileTreeNode
	for index := range nodes {
		if nodes[index].name == "generated/client/models" {
			compact = &nodes[index]
		}
		if nodes[index].name == "client" || nodes[index].name == "models" {
			t.Fatalf("unchanged chain was not compacted: %#v", nodes)
		}
	}
	if compact == nil || compact.path != "generated/client/models" || compact.changed {
		t.Fatalf("missing compact unchanged directory: %#v", nodes)
	}
}

func TestReviewedFilePersistsAndResetsWhenDiffChanges(t *testing.T) {
	reviewPath := filepath.Join(t.TempDir(), "review.json")
	repo := &gitrepo.Repository{
		Root: t.TempDir(), Branch: "feature", Base: "main", ReviewPath: reviewPath,
		Files: []diff.File{{Path: "service.go", Status: "M", Additions: 1, Lines: []diff.Line{{Kind: diff.Addition, Text: "first", NewNumber: 1}}}},
	}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height = 120, 24
	updated, _ := m.Update(tea.KeyPressMsg{Text: " ", Code: tea.KeySpace})
	m = updated.(Model)
	if !m.fileReviewed(0) || !strings.Contains(xansi.Strip(m.renderFiles(42, 20)), "1/1 REVIEWED") {
		t.Fatalf("file was not rendered reviewed:\n%s", xansi.Strip(m.renderFiles(42, 20)))
	}
	loaded, err := review.Load(reviewPath, "feature", "main")
	if err != nil || !loaded.IsReviewed("service.go", fileReviewFingerprint(repo.Files[0])) {
		t.Fatalf("reviewed state did not persist: session=%#v err=%v", loaded, err)
	}

	m.repo.Files[0].Lines[0].Text = "second"
	if m.fileReviewed(0) || !strings.Contains(xansi.Strip(m.renderFiles(42, 20)), "0/1 REVIEWED") {
		t.Fatalf("changed diff remained reviewed:\n%s", xansi.Strip(m.renderFiles(42, 20)))
	}
}

func TestWideFilePaneFitsVisibleTreeNames(t *testing.T) {
	const filename = "pipeline_commander_configuration_service_test.go"
	repo := &gitrepo.Repository{
		Root:       t.TempDir(),
		Branch:     "feature",
		Base:       "main",
		ReviewPath: filepath.Join(t.TempDir(), "review.json"),
		Files: []diff.File{{
			Path:      "packages/frontend/src/features/pipeline/" + filename,
			Status:    "M",
			Additions: 12,
			Deletions: 3,
			Lines:     []diff.Line{{Kind: diff.Addition, Text: "changed", NewNumber: 1}},
		}},
	}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height, m.fileLayout = 180, 30, treeFiles
	compact := m.filePaneWidth()
	m.toggleFilePaneWidth()
	expanded := m.filePaneWidth()
	if expanded <= compact {
		t.Fatalf("expanded file pane width %d did not exceed compact width %d", expanded, compact)
	}
	if expanded > m.width-48 {
		t.Fatalf("expanded pane width %d leaves less than 48 columns for the diff", expanded)
	}
	plain := xansi.Strip(m.renderFiles(expanded, 24))
	if !strings.Contains(plain, filename) || !strings.Contains(plain, "TREE  CHANGED  WIDE") {
		t.Fatalf("expanded tree did not show its full filename and state:\n%s", plain)
	}
}

func TestWideFilePaneExplainsNarrowTerminalLimit(t *testing.T) {
	repo := &gitrepo.Repository{Root: t.TempDir(), Branch: "feature", Base: "main", ReviewPath: filepath.Join(t.TempDir(), "review.json")}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.width = 72
	m.toggleFilePaneWidth()
	if m.wideFiles || !strings.Contains(m.status, "already fills") {
		t.Fatalf("narrow terminal toggle changed width state: wide=%v status=%q", m.wideFiles, m.status)
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

func TestDiffRowsUseStrongSemanticMarkerAndBackgroundColours(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	repo := &gitrepo.Repository{
		Root: t.TempDir(), Branch: "feature", Base: "main", ReviewPath: filepath.Join(t.TempDir(), "review.json"),
		Files: []diff.File{{Path: "service.go", Lines: []diff.Line{
			{Kind: diff.Deletion, Text: "removedValue", OldNumber: 10},
			{Kind: diff.Addition, Text: "addedValue", NewNumber: 10},
		}}},
	}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.focus = focusFiles

	checks := []struct {
		line          diff.Line
		marker        string
		foreground    [3]uint8
		backgroundHex string
		background    func(red, green, blue uint8) bool
	}{
		{repo.Files[0].Lines[0], "-", [3]uint8{0xff, 0x7b, 0x72}, deletedLineBackground, func(red, green, _ uint8) bool { return int(red)-int(green) >= 35 }},
		{repo.Files[0].Lines[1], "+", [3]uint8{0x56, 0xd3, 0x64}, addedLineBackground, func(red, green, _ uint8) bool { return int(green)-int(red) >= 8 }},
	}
	for _, check := range checks {
		rendered := m.renderUnifiedLine([]diff.Line{check.line}, 0, check.line, 60)
		markerX := strings.Index(xansi.Strip(rendered), check.marker)
		if markerX < 0 {
			t.Fatalf("rendered row has no %q marker: %q", check.marker, xansi.Strip(rendered))
		}
		buffer := uv.NewScreenBuffer(60, 1)
		uv.NewStyledString(rendered).Draw(buffer, buffer.Bounds())
		cell := buffer.CellAt(markerX, 0)
		if cell == nil || cell.Style.Fg == nil || cell.Style.Bg == nil {
			t.Fatalf("marker %q has incomplete terminal style", check.marker)
		}
		red, green, blue, _ := cell.Style.Fg.RGBA()
		gotForeground := [3]uint8{uint8(red >> 8), uint8(green >> 8), uint8(blue >> 8)}
		if gotForeground != check.foreground {
			t.Fatalf("marker %q foreground = #%02x%02x%02x, want #%02x%02x%02x", check.marker, gotForeground[0], gotForeground[1], gotForeground[2], check.foreground[0], check.foreground[1], check.foreground[2])
		}
		red, green, blue, _ = cell.Style.Bg.RGBA()
		if !check.background(uint8(red>>8), uint8(green>>8), uint8(blue>>8)) {
			t.Fatalf("marker %q background lacks semantic colour: #%02x%02x%02x", check.marker, red>>8, green>>8, blue>>8)
		}
		numberX := strings.Index(xansi.Strip(rendered), "10")
		red, green, blue, _ = buffer.CellAt(numberX, 0).Style.Fg.RGBA()
		if got := [3]uint8{uint8(red >> 8), uint8(green >> 8), uint8(blue >> 8)}; got != check.foreground {
			t.Fatalf("unified %q line number foreground = #%02x%02x%02x", check.marker, got[0], got[1], got[2])
		}
		sourceX := strings.Index(xansi.Strip(rendered), check.line.Text)
		for column := numberX; column < sourceX; column++ {
			gutterCell := buffer.CellAt(column, 0)
			if gutterCell == nil || gutterCell.Style.Bg == nil {
				t.Fatalf("unified %q gutter column %d has no background", check.marker, column)
			}
			red, green, blue, _ = gutterCell.Style.Bg.RGBA()
			if got := fmt.Sprintf("#%02X%02X%02X", red>>8, green>>8, blue>>8); got != check.backgroundHex {
				t.Fatalf("unified %q gutter column %d background = %s, want %s", check.marker, column, got, check.backgroundHex)
			}
		}

		split := m.renderSplitCell(&check.line, 40, false, false)
		splitMarkerX := strings.Index(xansi.Strip(split), check.marker)
		splitBuffer := uv.NewScreenBuffer(40, 1)
		uv.NewStyledString(split).Draw(splitBuffer, splitBuffer.Bounds())
		splitCell := splitBuffer.CellAt(splitMarkerX, 0)
		if splitCell == nil || splitCell.Style.Fg == nil || splitCell.Style.Bg == nil {
			t.Fatalf("split marker %q has incomplete terminal style", check.marker)
		}
		red, green, blue, _ = splitCell.Style.Fg.RGBA()
		if got := [3]uint8{uint8(red >> 8), uint8(green >> 8), uint8(blue >> 8)}; got != check.foreground {
			t.Fatalf("split marker %q foreground = #%02x%02x%02x", check.marker, got[0], got[1], got[2])
		}
		red, green, blue, _ = splitCell.Style.Bg.RGBA()
		if !check.background(uint8(red>>8), uint8(green>>8), uint8(blue>>8)) {
			t.Fatalf("split marker %q background lacks semantic colour: #%02x%02x%02x", check.marker, red>>8, green>>8, blue>>8)
		}
		splitNumberX := strings.Index(xansi.Strip(split), "10")
		red, green, blue, _ = splitBuffer.CellAt(splitNumberX, 0).Style.Fg.RGBA()
		if got := [3]uint8{uint8(red >> 8), uint8(green >> 8), uint8(blue >> 8)}; got != check.foreground {
			t.Fatalf("split %q line number foreground = #%02x%02x%02x", check.marker, got[0], got[1], got[2])
		}
		splitSourceX := strings.Index(xansi.Strip(split), check.line.Text)
		for column := splitNumberX; column < splitSourceX; column++ {
			gutterCell := splitBuffer.CellAt(column, 0)
			if gutterCell == nil || gutterCell.Style.Bg == nil {
				t.Fatalf("split %q gutter column %d has no background", check.marker, column)
			}
			red, green, blue, _ = gutterCell.Style.Bg.RGBA()
			if got := fmt.Sprintf("#%02X%02X%02X", red>>8, green>>8, blue>>8); got != check.backgroundHex {
				t.Fatalf("split %q gutter column %d background = %s, want %s", check.marker, column, got, check.backgroundHex)
			}
		}
	}
}

func TestDiffSyntaxPreservesBlockCommentStateAcrossLines(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	lines := []diff.Line{
		{Kind: diff.Addition, Text: "/**", NewNumber: 1, OriginalIndex: 0},
		{Kind: diff.Addition, Text: " * Record facts only", NewNumber: 2, OriginalIndex: 1},
		{Kind: diff.Addition, Text: " */", NewNumber: 3, OriginalIndex: 2},
		{Kind: diff.Addition, Text: "export const value = true", NewNumber: 4, OriginalIndex: 3},
	}
	repo := &gitrepo.Repository{
		Root: t.TempDir(), Branch: "feature", Base: "main", ReviewPath: filepath.Join(t.TempDir(), "review.json"),
		Files: []diff.File{{Path: "schema.ts", Additions: len(lines), Lines: lines}},
	}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.focus = focusFiles
	commentColour := styles.Get("github-dark").Get(chroma.CommentMultiline).Colour
	want := [3]uint8{commentColour.Red(), commentColour.Green(), commentColour.Blue()}
	for index := 0; index < 3; index++ {
		rendered := m.renderUnifiedLine(lines, index, lines[index], 80)
		plain := xansi.Strip(rendered)
		sourceColumn := strings.Index(plain, strings.TrimLeft(lines[index].Text, " "))
		buffer := uv.NewScreenBuffer(80, 1)
		uv.NewStyledString(rendered).Draw(buffer, buffer.Bounds())
		red, green, blue, _ := buffer.CellAt(sourceColumn, 0).Style.Fg.RGBA()
		if got := [3]uint8{uint8(red >> 8), uint8(green >> 8), uint8(blue >> 8)}; got != want {
			t.Fatalf("block comment line %d foreground = #%02x%02x%02x, want #%02x%02x%02x: %q", index+1, got[0], got[1], got[2], want[0], want[1], want[2], plain)
		}
	}
	after := m.renderUnifiedLine(lines, 3, lines[3], 80)
	afterPlain := xansi.Strip(after)
	afterColumn := strings.Index(afterPlain, "export")
	afterBuffer := uv.NewScreenBuffer(80, 1)
	uv.NewStyledString(after).Draw(afterBuffer, afterBuffer.Bounds())
	red, green, blue, _ := afterBuffer.CellAt(afterColumn, 0).Style.Fg.RGBA()
	if got := [3]uint8{uint8(red >> 8), uint8(green >> 8), uint8(blue >> 8)}; got == want {
		t.Fatalf("code after block comment retained comment colour: %q", afterPlain)
	}

	split := m.renderSplit(80, 8)
	splitRows := strings.Split(split, "\n")
	splitColumn := strings.Index(xansi.Strip(splitRows[1]), "* Record")
	splitBuffer := uv.NewScreenBuffer(80, len(splitRows))
	uv.NewStyledString(split).Draw(splitBuffer, splitBuffer.Bounds())
	red, green, blue, _ = splitBuffer.CellAt(splitColumn, 1).Style.Fg.RGBA()
	if got := [3]uint8{uint8(red >> 8), uint8(green >> 8), uint8(blue >> 8)}; got != want {
		t.Fatalf("split block comment foreground = #%02x%02x%02x, want #%02x%02x%02x", got[0], got[1], got[2], want[0], want[1], want[2])
	}
}

func TestSplitDiffIntralineHighlightIgnoresReformattedTernary(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	lines := []diff.Line{
		{Kind: diff.Deletion, Hunk: 1, Text: "const effectiveLimit = isFullpage", OldNumber: 292, OriginalIndex: 0},
		{Kind: diff.Deletion, Hunk: 1, Text: "  ? fullpageLimit", OldNumber: 293, OriginalIndex: 1},
		{Kind: diff.Deletion, Hunk: 1, Text: "  : widgetSettings.widgetLimit;", OldNumber: 294, OriginalIndex: 2},
		{Kind: diff.Addition, Hunk: 1, Text: "const effectiveLimit = isFullpage ? fullpageLimit : settings.config.limit;", NewNumber: 281, OriginalIndex: 3},
	}
	repo := &gitrepo.Repository{
		Root: t.TempDir(), Branch: "feature", Base: "main", ReviewPath: filepath.Join(t.TempDir(), "review.json"),
		Files: []diff.File{{Path: "limits.ts", Additions: 1, Deletions: 3, Lines: lines}},
	}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.focus = focusFiles
	m.semanticReflow = true
	const width = 180
	rendered := m.renderSplit(width, 3)
	plainLines := strings.Split(xansi.Strip(rendered), "\n")
	buffer := uv.NewScreenBuffer(width, 3)
	uv.NewStyledString(rendered).Draw(buffer, buffer.Bounds())

	assertCellBackground(t, buffer.CellAt(strings.Index(plainLines[0], "const"), 0), deletedLineBackground)
	assertCellBackground(t, buffer.CellAt(strings.Index(plainLines[0], "settings.config.limit"), 0), addedWordBackground)
	assertCellBackground(t, buffer.CellAt(strings.Index(plainLines[2], "widgetSettings.widgetLimit"), 2), deletedWordBackground)
}

func TestFocusedFileRowUsesStrongSelectionBackground(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	repo := &gitrepo.Repository{
		Root: t.TempDir(), Branch: "feature", Base: "main", ReviewPath: filepath.Join(t.TempDir(), "review.json"),
		Files: []diff.File{{Path: "internal/review/session.go", Status: "M", Additions: 2, Deletions: 1}},
	}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.focus = focusFiles
	rendered := m.renderFlatFile(0, true, true, 48)
	if got := lipgloss.Width(rendered); got != 45 {
		t.Fatalf("focused row width = %d, want 45", got)
	}
	buffer := uv.NewScreenBuffer(48, 1)
	uv.NewStyledString(rendered).Draw(buffer, buffer.Bounds())
	for column := 0; column < 45; column++ {
		cell := buffer.CellAt(column, 0)
		if cell == nil || cell.Style.Bg == nil {
			t.Fatalf("focused row column %d has no selection background: %q", column, rendered)
		}
		red, green, blue, _ := cell.Style.Bg.RGBA()
		if got := fmt.Sprintf("#%02X%02X%02X", red>>8, green>>8, blue>>8); got != selectedRowBackground {
			t.Fatalf("focused row column %d background = %s, want %s", column, got, selectedRowBackground)
		}
	}
}

func TestFileRowHasUniformPanelAndFullyColouredCounts(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	repo := &gitrepo.Repository{
		Root: t.TempDir(), Branch: "feature", Base: "main", ReviewPath: filepath.Join(t.TempDir(), "review.json"),
		Files: []diff.File{{Path: "internal/review/session.go", Status: "M", Additions: 18, Deletions: 4}},
	}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	rendered := m.renderFlatFile(0, false, false, 48)
	plain := xansi.Strip(rendered)
	buffer := uv.NewScreenBuffer(48, 1)
	uv.NewStyledString(rendered).Draw(buffer, buffer.Bounds())
	for column := 0; column < lipgloss.Width(rendered); column++ {
		cell := buffer.CellAt(column, 0)
		if cell == nil || cell.Style.Bg == nil {
			t.Fatalf("file row column %d has no panel background: %q", column, rendered)
		}
		red, green, blue, _ := cell.Style.Bg.RGBA()
		if got := fmt.Sprintf("#%02X%02X%02X", red>>8, green>>8, blue>>8); got != panelBackground {
			t.Fatalf("file row column %d background = %s, want %s", column, got, panelBackground)
		}
	}
	for _, token := range []struct {
		text string
		rgb  [3]uint8
	}{
		{text: "+18", rgb: [3]uint8{0x56, 0xd3, 0x64}},
		{text: "-4", rgb: [3]uint8{0xff, 0x7b, 0x72}},
	} {
		start := strings.Index(plain, token.text)
		if start < 0 {
			t.Fatalf("file row missing %q: %q", token.text, plain)
		}
		for column := start; column < start+len(token.text); column++ {
			red, green, blue, _ := buffer.CellAt(column, 0).Style.Fg.RGBA()
			if got := [3]uint8{uint8(red >> 8), uint8(green >> 8), uint8(blue >> 8)}; got != token.rgb {
				t.Fatalf("%q column %d foreground = #%02x%02x%02x", token.text, column-start, got[0], got[1], got[2])
			}
		}
	}
}

func TestFileNamesUseTheirGitStatusColour(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	files := []diff.File{
		{Path: "added.go", Status: "A"},
		{Path: "modified.go", Status: "M"},
		{Path: "deleted.go", Status: "D"},
		{Path: "renamed.go", Status: "R"},
	}
	repo := &gitrepo.Repository{
		Root: t.TempDir(), Branch: "feature", Base: "main", ReviewPath: filepath.Join(t.TempDir(), "review.json"), Files: files,
	}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{"#56D364", "#79C0FF", "#FF7B72", "#D2A8FF"}
	for index, file := range files {
		rows := []string{
			m.renderFlatFile(index, false, true, 48),
			m.renderTreeNode(fileTreeNode{name: file.Path, fileIndex: index}, false, true, 48),
		}
		for _, row := range rows {
			plain := xansi.Strip(row)
			if strings.Contains(plain, "+0") || strings.Contains(plain, "-0") {
				t.Fatalf("%s row rendered a zero count: %q", file.Status, plain)
			}
			column := strings.Index(plain, file.Path)
			if column < 0 {
				t.Fatalf("%s row is missing filename: %q", file.Status, plain)
			}
			buffer := uv.NewScreenBuffer(48, 1)
			uv.NewStyledString(row).Draw(buffer, buffer.Bounds())
			red, green, blue, _ := buffer.CellAt(column, 0).Style.Fg.RGBA()
			if got := fmt.Sprintf("#%02X%02X%02X", red>>8, green>>8, blue>>8); got != expected[index] {
				t.Fatalf("%s filename colour = %s, want %s: %q", file.Status, got, expected[index], plain)
			}
		}
	}
}

func TestFileCountsKeepTheirColumnsWhenRowIsSelected(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	repo := &gitrepo.Repository{
		Root: t.TempDir(), Branch: "feature", Base: "main", ReviewPath: filepath.Join(t.TempDir(), "review.json"),
		Files: []diff.File{
			{Path: "create-list-widget-hook.test.tsx", Status: "A", Additions: 244},
			{Path: "create-list-widget-hook.ts", Status: "A", Additions: 54},
			{Path: "a.go", Status: "M", Additions: 7},
		},
	}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	const width = 48
	unselected := xansi.Strip(m.renderFlatFile(0, false, true, width))
	selected := xansi.Strip(m.renderFlatFile(0, true, true, width))
	for _, count := range []string{"+244"} {
		unselectedColumn := strings.Index(unselected, count)
		selectedColumn := strings.Index(selected, count)
		if unselectedColumn < 0 || selectedColumn < 0 {
			t.Fatalf("missing %q: unselected=%q selected=%q", count, unselected, selected)
		}
		if selectedColumn != unselectedColumn {
			t.Fatalf("%q moved from column %d to %d when selected:\nunselected %q\nselected   %q", count, unselectedColumn, selectedColumn, unselected, selected)
		}
	}
	if strings.Contains(selected, "-0") || strings.Contains(unselected, "-0") {
		t.Fatalf("zero deletion count remained visible:\nunselected %q\nselected   %q", unselected, selected)
	}
	selectedTree := xansi.Strip(m.renderTreeNode(fileTreeNode{name: "create-list-widget-hook.test.tsx", depth: 4, fileIndex: 0}, true, true, width))
	sameTreeUnselected := xansi.Strip(m.renderTreeNode(fileTreeNode{name: "create-list-widget-hook.test.tsx", depth: 4, fileIndex: 0}, false, true, width))
	for _, marker := range []string{"+"} {
		selectedColumn := lastVisibleColumn(selectedTree, marker)
		unselectedColumn := lastVisibleColumn(sameTreeUnselected, marker)
		if selectedColumn != unselectedColumn {
			t.Fatalf("tree %q count moved from column %d to %d when the same row was selected:\nunselected %q\nselected   %q", marker, unselectedColumn, selectedColumn, sameTreeUnselected, selectedTree)
		}
	}
	unselectedTree := xansi.Strip(m.renderTreeNode(fileTreeNode{name: "create-list-widget-hook.ts", depth: 4, fileIndex: 1}, false, true, width))
	for _, marker := range []string{"+"} {
		selectedColumn := lastVisibleColumn(selectedTree, marker)
		unselectedColumn := lastVisibleColumn(unselectedTree, marker)
		if selectedColumn != unselectedColumn {
			t.Fatalf("tree %q count moved from column %d to %d when selected:\nunselected %q width=%d\nselected   %q width=%d", marker, unselectedColumn, selectedColumn, unselectedTree, lipgloss.Width(unselectedTree), selectedTree, lipgloss.Width(selectedTree))
		}
	}
	for _, row := range []struct {
		name       string
		selected   string
		unselected string
	}{
		{name: "flat short name", selected: xansi.Strip(m.renderFlatFile(2, true, true, width)), unselected: xansi.Strip(m.renderFlatFile(2, false, true, width))},
		{name: "tree short name", selected: xansi.Strip(m.renderTreeNode(fileTreeNode{name: "a.go", depth: 1, fileIndex: 2}, true, true, width)), unselected: xansi.Strip(m.renderTreeNode(fileTreeNode{name: "a.go", depth: 1, fileIndex: 2}, false, true, width))},
	} {
		for _, marker := range []string{"+"} {
			selectedColumn := lastVisibleColumn(row.selected, marker)
			unselectedColumn := lastVisibleColumn(row.unselected, marker)
			if selectedColumn != unselectedColumn {
				t.Fatalf("%s %q count moved from column %d to %d when selected:\nunselected %q\nselected   %q", row.name, marker, unselectedColumn, selectedColumn, row.unselected, row.selected)
			}
		}
	}
}

func TestUnifiedCursorUsesReservedGutterWithoutChangingRowWidth(t *testing.T) {
	repo := &gitrepo.Repository{
		Root: t.TempDir(), Branch: "feature", Base: "main", ReviewPath: filepath.Join(t.TempDir(), "review.json"),
		Files: []diff.File{{Path: "main.go", Lines: []diff.Line{{Kind: diff.Context, Text: "return value", OldNumber: 4, NewNumber: 4}}}},
	}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.focus = focusDiff
	const width = 60
	rendered := m.renderUnifiedLine(repo.Files[0].Lines, 0, repo.Files[0].Lines[0], width)
	if got := lipgloss.Width(rendered); got != width {
		t.Fatalf("selected row width = %d, want %d", got, width)
	}
	if !strings.Contains(xansi.Strip(rendered), "▌") {
		t.Fatalf("selected row has no gutter focus rail: %q", xansi.Strip(rendered))
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
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	output := m.renderSplit(78, 10)
	if got := len(strings.Split(output, "\n")); got != 2 {
		t.Fatalf("split row wrapped into %d lines:\n%s", got, output)
	}
}

func TestAdditionOnlySplitRowsStayInsideViewport(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	lines := []diff.Line{{Kind: diff.Meta, Text: "@@ -0,0 +1,60 @@"}}
	for line := 1; line <= 60; line++ {
		text := "const value = true"
		if line == 1 {
			text = "// @vitest-environment jsdom"
		}
		lines = append(lines, diff.Line{Kind: diff.Addition, Text: text, NewNumber: line})
	}
	repo := &gitrepo.Repository{
		Root: t.TempDir(), Branch: "feature", Base: "main", ReviewPath: filepath.Join(t.TempDir(), "review.json"),
		Files: []diff.File{{Path: "create-list-widget-hook.test.tsx", Additions: 60, Lines: lines}},
	}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.focus, m.view, m.width, m.height = focusDiff, split, 204, 60
	m.line, m.splitCursor = 1, 1
	const viewportWidth = 202
	halfWidth := (viewportWidth - 1) / 2
	leftCell := m.renderSplitCell(nil, halfWidth, true, true)
	rightCell := m.renderSplitCell(&repo.Files[0].Lines[1], viewportWidth-halfWidth-1, true, false)
	if strings.Contains(leftCell, "\n") || strings.Contains(rightCell, "\n") {
		t.Fatalf("split cell wrapped internally: left=%d×%d right=%d×%d", lipgloss.Width(leftCell), lipgloss.Height(leftCell), lipgloss.Width(rightCell), lipgloss.Height(rightCell))
	}
	output := m.renderSplit(viewportWidth, 54)
	rows := strings.Split(output, "\n")
	if len(rows) != 54 {
		var sample []string
		for row := 0; row < min(5, len(rows)); row++ {
			sample = append(sample, fmt.Sprintf("%d width=%d %q", row, lipgloss.Width(rows[row]), xansi.Strip(rows[row])))
		}
		t.Fatalf("addition-only split rendered %d logical rows, want 54:\n%s", len(rows), strings.Join(sample, "\n"))
	}
	for row, rendered := range rows {
		if got := lipgloss.Width(rendered); got > viewportWidth {
			t.Fatalf("addition-only split row %d crossed viewport: width %d, want <= %d", row, got, viewportWidth)
		}
	}
	buffer := uv.NewScreenBuffer(viewportWidth, len(rows))
	uv.NewStyledString(output).Draw(buffer, buffer.Bounds())
	half := (viewportWidth - 1) / 2
	for row := 1; row < len(rows); row++ {
		for column := 0; column < half; column++ {
			cell := buffer.CellAt(column, row)
			if cell == nil || cell.Style.Bg == nil {
				t.Fatalf("addition-only row %d left column %d has no neutral background", row, column)
			}
			red, green, blue, _ := cell.Style.Bg.RGBA()
			if got := fmt.Sprintf("#%02X%02X%02X", red>>8, green>>8, blue>>8); got != panelBackground {
				t.Fatalf("addition-only row %d bled into left column %d: background %s", row, column, got)
			}
		}
	}
	pane := m.renderDiff(204, 56)
	if got := lipgloss.Width(pane); got > 204 {
		t.Fatalf("split diff pane crossed viewport: width %d, want <= 204", got)
	}
	if got := lipgloss.Height(pane); got > 56 {
		t.Fatalf("split diff pane crossed viewport: height %d, want <= 56", got)
	}
}

func TestSplitNavigationMovesByVisualRow(t *testing.T) {
	repo := &gitrepo.Repository{
		Root:       t.TempDir(),
		Branch:     "feature",
		Base:       "main",
		ReviewPath: filepath.Join(t.TempDir(), "review.json"),
		Files: []diff.File{{
			Path: "app.go",
			Lines: []diff.Line{
				{Kind: diff.Meta, Text: "@@ -1,3 +1,3 @@"},
				{Kind: diff.Deletion, Text: "old one", OldNumber: 1},
				{Kind: diff.Deletion, Text: "old two", OldNumber: 2},
				{Kind: diff.Addition, Text: "new one", NewNumber: 1},
				{Kind: diff.Context, Text: "same", OldNumber: 3, NewNumber: 2},
			},
		}},
	}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.focus, m.view = focusDiff, split
	m.syncSplitCursorToLine()

	wantLines := []int{0, 1, 2, 4}
	for row, wantLine := range wantLines {
		if m.splitCursor != row || m.line != wantLine {
			t.Fatalf("row %d selected split row %d / logical line %d, want row %d / line %d", row, m.splitCursor, m.line, row, wantLine)
		}
		m.move(1)
	}
}

func TestSplitScrollingKeepsStyledCellsInsidePaneBoundaries(t *testing.T) {
	var lines []diff.Line
	lines = append(lines, diff.Line{Kind: diff.Meta, Text: "@@ -220,12 +220,12 @@"})
	for i := 0; i < 12; i++ {
		lines = append(lines,
			diff.Line{Kind: diff.Deletion, Text: "\taccount_id: str | None = resolve_user(account, assignee, owner)", OldNumber: 220 + i},
			diff.Line{Kind: diff.Addition, Text: "\taccount_id: str | None = resolve_requested_assignee_mode(account, assignee, owner) → active", NewNumber: 220 + i},
		)
	}
	repo := &gitrepo.Repository{
		Root:       t.TempDir(),
		Branch:     "feature",
		Base:       "main",
		ReviewPath: filepath.Join(t.TempDir(), "review.json"),
		Files:      []diff.File{{Path: "service.py", Lines: lines}},
	}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.focus, m.view, m.width, m.height = focusDiff, split, 140, 12
	m.syncSplitCursorToLine()
	var terminalOutput bytes.Buffer
	renderer := uv.NewTerminalRenderer(&terminalOutput, []string{"TERM=xterm-256color", "COLORTERM=truecolor"})
	renderer.SetFullscreen(true)
	renderer.SetScrollOptim(true)

	for step := 0; step < len(m.currentSplitRows()); step++ {
		output := m.renderSplit(138, 6)
		physicalLines := strings.Split(output, "\n")
		if len(physicalLines) > 6 {
			t.Fatalf("step %d rendered %d rows into a 6-row viewport", step, len(physicalLines))
		}
		for i, rendered := range physicalLines {
			if got := lipgloss.Width(rendered); got > 138 {
				t.Fatalf("step %d row %d crossed the split viewport: width %d", step, i, got)
			}
		}
		plain := xansi.Strip(output)
		if strings.Contains(plain, "[38;2;") || strings.Contains(plain, "[0m") {
			t.Fatalf("step %d exposed ANSI control text:\n%s", step, plain)
		}

		buffer := uv.NewScreenBuffer(138, 6)
		uv.NewStyledString(output).Draw(buffer, buffer.Bounds())
		renderer.Render(buffer.RenderBuffer)
		if err := renderer.Flush(); err != nil {
			t.Fatal(err)
		}
		terminalText := xansi.Strip(terminalOutput.String())
		if strings.Contains(terminalText, "[38;2;") || strings.Contains(terminalText, "[0m") {
			t.Fatalf("step %d incremental render exposed ANSI control text:\n%s", step, terminalText)
		}
		terminalOutput.Reset()
		m.move(1)
	}
	if m.splitScroll == 0 {
		t.Fatal("split viewport did not scroll with the visual-row cursor")
	}
}
