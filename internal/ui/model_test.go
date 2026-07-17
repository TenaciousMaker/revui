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
		FileLayout: "tree", FileScope: "all", WideFiles: true, DiffView: "split",
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
	if m.fileLayout != treeFiles || m.fileScope != allRepositoryFiles || !m.wideFiles || m.view != split {
		t.Fatalf("preferences were not restored: layout=%v scope=%v wide=%v view=%v", m.fileLayout, m.fileScope, m.wideFiles, m.view)
	}

	if err := config.Save(preferencesPath, config.Preferences{}); err != nil {
		t.Fatal(err)
	}
	m, err = newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.width = 140
	for _, key := range []string{"t", "A", "w", "s"} {
		updated, _ := m.Update(tea.KeyPressMsg{Text: key, Code: rune(key[0])})
		m = updated.(Model)
	}
	got, err := config.Load(preferencesPath)
	if err != nil {
		t.Fatal(err)
	}
	if got.FileLayout != "tree" || got.FileScope != "context" || !got.WideFiles || got.DiffView != "split" {
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
		line       diff.Line
		marker     string
		foreground [3]uint8
		background func(red, green, blue uint8) bool
	}{
		{repo.Files[0].Lines[0], "-", [3]uint8{0xff, 0x7b, 0x72}, func(red, green, _ uint8) bool { return int(red)-int(green) >= 35 }},
		{repo.Files[0].Lines[1], "+", [3]uint8{0x56, 0xd3, 0x64}, func(red, green, _ uint8) bool { return int(green)-int(red) >= 8 }},
	}
	for _, check := range checks {
		rendered := m.renderUnifiedLine(0, check.line, 60)
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
