package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/TenaciousMaker/revui/internal/diff"
	"github.com/TenaciousMaker/revui/internal/gitrepo"
)

func TestMouseWheelScrollsPaneUnderPointer(t *testing.T) {
	repo := &gitrepo.Repository{
		Root: t.TempDir(), Branch: "feature", Base: "main", ReviewPath: filepath.Join(t.TempDir(), "review.json"),
	}
	for file := 0; file < 8; file++ {
		var lines []diff.Line
		for line := 1; line <= 12; line++ {
			lines = append(lines, diff.Line{Kind: diff.Context, Text: fmt.Sprintf("file %d line %d", file, line), OldNumber: line, NewNumber: line})
		}
		repo.Files = append(repo.Files, diff.File{Path: fmt.Sprintf("file%d.go", file), Lines: lines})
	}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height = 140, 12

	updated, _ := m.Update(tea.MouseWheelMsg{X: 5, Y: 4, Button: tea.MouseWheelDown})
	m = updated.(Model)
	if m.focus != focusFiles || m.file != 0 || m.fileScroll != mouseWheelStep {
		t.Fatalf("left-pane wheel changed focus/cursor or missed viewport: focus=%v file=%d scroll=%d", m.focus, m.file, m.fileScroll)
	}

	updated, _ = m.Update(tea.MouseWheelMsg{X: 100, Y: 4, Button: tea.MouseWheelDown})
	m = updated.(Model)
	if m.focus != focusFiles || m.line != 0 || m.lineScroll != mouseWheelStep {
		t.Fatalf("right-pane wheel changed focus/cursor or missed viewport: focus=%v line=%d scroll=%d", m.focus, m.line, m.lineScroll)
	}

	updated, _ = m.Update(tea.MouseWheelMsg{X: 5, Y: 0, Button: tea.MouseWheelDown})
	m = updated.(Model)
	if m.fileScroll != mouseWheelStep || m.lineScroll != mouseWheelStep {
		t.Fatalf("wheel over header moved a viewport: file=%d line=%d", m.fileScroll, m.lineScroll)
	}
}

func TestMouseWheelBurstCoalescesIntoOneViewportUpdate(t *testing.T) {
	var lines []diff.Line
	for line := 1; line <= 500; line++ {
		lines = append(lines, diff.Line{Kind: diff.Context, Text: fmt.Sprintf("line %d", line), OldNumber: line, NewNumber: line})
	}
	repo := &gitrepo.Repository{
		Root: t.TempDir(), Branch: "feature", Base: "main", ReviewPath: filepath.Join(t.TempDir(), "review.json"),
		Files: []diff.File{{Path: "app.go", Lines: lines}},
	}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height, m.focus = 140, 30, focusDiff
	scheduled := 0
	for event := 0; event < 100; event++ {
		updated, cmd := m.Update(tea.MouseWheelMsg{X: 100, Y: 10, Button: tea.MouseWheelDown})
		m = updated.(Model)
		if cmd != nil {
			scheduled++
		}
	}
	if m.line != 0 || m.lineScroll > mouseWheelStep || scheduled != 1 {
		t.Fatalf("wheel burst cursor=%d scroll=%d scheduled=%d; want fixed cursor, at most one step, one frame", m.line, m.lineScroll, scheduled)
	}
	updated, _ := m.Update(mouseWheelFrameMsg{})
	m = updated.(Model)
	if m.line != 0 || m.lineScroll <= mouseWheelStep || m.lineScroll > m.pageSize()+mouseWheelStep || m.wheelScheduled {
		t.Fatalf("accelerated frame cursor=%d scroll=%d scheduled=%v page=%d", m.line, m.lineScroll, m.wheelScheduled, m.pageSize())
	}
}

func TestSustainedMouseWheelGestureAcceleratesAcrossFrames(t *testing.T) {
	var lines []diff.Line
	for line := 1; line <= 1000; line++ {
		lines = append(lines, diff.Line{Kind: diff.Context, Text: fmt.Sprintf("line %d", line), OldNumber: line, NewNumber: line})
	}
	repo := &gitrepo.Repository{
		Root: t.TempDir(), Branch: "feature", Base: "main", ReviewPath: filepath.Join(t.TempDir(), "review.json"),
		Files: []diff.File{{Path: "app.go", Lines: lines}},
	}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height, m.focus = 140, 40, focusDiff
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	steps := make([]int, 0, 5)
	for frame := 0; frame < 5; frame++ {
		now = now.Add(20 * time.Millisecond)
		before := m.lineScroll
		updated, _ := m.Update(tea.MouseWheelMsg{X: 100, Y: 10, Button: tea.MouseWheelDown})
		m = updated.(Model)
		updated, _ = m.Update(mouseWheelFrameMsg{})
		m = updated.(Model)
		steps = append(steps, m.lineScroll-before)
	}
	if steps[len(steps)-1] <= steps[0] || m.line != 0 {
		t.Fatalf("sustained gesture steps=%v cursor=%d; want increasing speed with fixed cursor", steps, m.line)
	}
}

func BenchmarkMouseWheelRendering(b *testing.B) {
	var lines []diff.Line
	for line := 1; line <= 2000; line++ {
		lines = append(lines, diff.Line{
			Kind: diff.Addition, NewNumber: line,
			Text: fmt.Sprintf("public static String method%d(String value) { return value + '%d'; }", line, line),
		})
	}
	repo := &gitrepo.Repository{
		Root: b.TempDir(), Branch: "feature", Base: "main", ReviewPath: filepath.Join(b.TempDir(), "review.json"),
		Files: []diff.File{{Path: "Large.cls", Lines: lines}},
	}
	m, err := newTestModel(b, repo)
	if err != nil {
		b.Fatal(err)
	}
	m.width, m.height, m.focus = 180, 60, focusDiff
	wheel := tea.MouseWheelMsg{X: 120, Y: 20, Button: tea.MouseWheelDown}
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		updated, _ := m.Update(wheel)
		m = updated.(Model)
		_ = m.View()
	}
}

func BenchmarkFileTreeWheelRendering(b *testing.B) {
	repo := &gitrepo.Repository{
		Root: b.TempDir(), Branch: "feature", Base: "main", ReviewPath: filepath.Join(b.TempDir(), "review.json"),
	}
	for index := 0; index < 5000; index++ {
		repo.AllPaths = append(repo.AllPaths, fmt.Sprintf("src/package%03d/module%03d/file%04d.cls", index%200, index%500, index))
	}
	m, err := newTestModel(b, repo)
	if err != nil {
		b.Fatal(err)
	}
	m.width, m.height, m.focus = 180, 60, focusFiles
	m.fileLayout, m.fileScope = treeFiles, allRepositoryFiles
	m.rebuildTreeNodes()
	wheel := tea.MouseWheelMsg{X: 20, Y: 20, Button: tea.MouseWheelDown}
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		updated, _ := m.Update(wheel)
		m = updated.(Model)
		_ = m.View()
		updated, _ = m.Update(mouseWheelFrameMsg{})
		m = updated.(Model)
		_ = m.View()
	}
}

func TestMouseWheelUsesVisualRowsInSplitAndSourceViews(t *testing.T) {
	var lines []diff.Line
	for line := 1; line <= 12; line++ {
		lines = append(lines, diff.Line{Kind: diff.Context, Text: fmt.Sprintf("line %d", line), OldNumber: line, NewNumber: line})
	}
	repo := &gitrepo.Repository{
		Root: t.TempDir(), Branch: "feature", Base: "main", ReviewPath: filepath.Join(t.TempDir(), "review.json"),
		Files: []diff.File{{Path: "app.go", Lines: lines}},
	}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height, m.focus, m.view = 140, 12, focusDiff, split

	updated, _ := m.Update(tea.MouseWheelMsg{X: 100, Y: 4, Button: tea.MouseWheelDown})
	m = updated.(Model)
	if m.splitCursor != 0 || m.line != 0 || m.splitScroll != mouseWheelStep {
		t.Fatalf("split wheel changed cursor or missed viewport: cursor=%d line=%d scroll=%d", m.splitCursor, m.line, m.splitScroll)
	}
	updated, _ = m.Update(mouseWheelFrameMsg{})
	m = updated.(Model)

	m.sourcePath = "unchanged.go"
	m.sourceLines = make([]string, 12)
	m.sourceLine, m.sourceScroll = 0, 0
	updated, _ = m.Update(tea.MouseWheelMsg{X: 100, Y: 4, Button: tea.MouseWheelDown})
	m = updated.(Model)
	if m.sourceLine != 0 || m.sourceScroll <= 0 {
		t.Fatalf("source wheel changed cursor or missed viewport: line=%d scroll=%d", m.sourceLine, m.sourceScroll)
	}
}

func TestMouseClickPositionsFileDiffSplitAndSourceRows(t *testing.T) {
	repo := &gitrepo.Repository{
		Root: t.TempDir(), Branch: "feature", Base: "main", ReviewPath: filepath.Join(t.TempDir(), "review.json"),
	}
	for file := 0; file < 6; file++ {
		var lines []diff.Line
		for line := 1; line <= 10; line++ {
			lines = append(lines, diff.Line{Kind: diff.Context, Text: fmt.Sprintf("line %d", line), OldNumber: line, NewNumber: line})
		}
		repo.Files = append(repo.Files, diff.File{Path: fmt.Sprintf("file%d.go", file), Lines: lines})
	}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height = 140, 20

	updated, _ := m.Update(tea.MouseClickMsg{X: 5, Y: 7, Button: tea.MouseLeft})
	m = updated.(Model)
	if m.focus != focusFiles || m.file != 2 {
		t.Fatalf("file click selected focus=%v file=%d, want files/2", m.focus, m.file)
	}

	updated, _ = m.Update(tea.MouseClickMsg{X: 100, Y: 8, Button: tea.MouseLeft})
	m = updated.(Model)
	updated, _ = m.Update(tea.MouseReleaseMsg{X: 100, Y: 8, Button: tea.MouseLeft})
	m = updated.(Model)
	if m.focus != focusDiff || m.line != 3 {
		t.Fatalf("unified click selected focus=%v line=%d, want diff/3", m.focus, m.line)
	}

	m.view = split
	m.syncSplitCursorToLine()
	updated, _ = m.Update(tea.MouseClickMsg{X: 100, Y: 10, Button: tea.MouseLeft})
	m = updated.(Model)
	updated, _ = m.Update(tea.MouseReleaseMsg{X: 100, Y: 10, Button: tea.MouseLeft})
	m = updated.(Model)
	if m.splitCursor != 5 || m.line != 5 {
		t.Fatalf("split click selected row=%d line=%d, want 5/5", m.splitCursor, m.line)
	}

	m.sourcePath = "unchanged.go"
	m.sourceLines = make([]string, 10)
	m.sourceLine, m.sourceScroll = 0, 0
	updated, _ = m.Update(tea.MouseClickMsg{X: 100, Y: 9, Button: tea.MouseLeft})
	m = updated.(Model)
	updated, _ = m.Update(tea.MouseReleaseMsg{X: 100, Y: 9, Button: tea.MouseLeft})
	m = updated.(Model)
	if m.sourceLine != 4 {
		t.Fatalf("source click selected line=%d, want 4", m.sourceLine)
	}
}

func TestMouseWheelScrollsSearchResultsAndViewEnablesMouse(t *testing.T) {
	repo := &gitrepo.Repository{Root: t.TempDir(), Branch: "feature", Base: "main", ReviewPath: filepath.Join(t.TempDir(), "review.json")}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height, m.mode = 120, 30, searchingRepository
	m.repoHits = make([]gitrepo.SearchMatch, 10)

	updated, _ := m.Update(tea.MouseWheelMsg{X: 60, Y: 10, Button: tea.MouseWheelDown})
	m = updated.(Model)
	if m.repoSearchAt != 0 || m.repoSearchTop != mouseWheelStep {
		t.Fatalf("repository search wheel changed cursor or missed viewport: selected=%d top=%d", m.repoSearchAt, m.repoSearchTop)
	}
	if got := m.View().MouseMode; got != tea.MouseModeCellMotion {
		t.Fatalf("view mouse mode=%v, want cell motion", got)
	}
}

func TestMouseWheelScrollsFuzzyResultsWithoutMovingSelection(t *testing.T) {
	repo := &gitrepo.Repository{Root: t.TempDir(), Branch: "feature", Base: "main", ReviewPath: filepath.Join(t.TempDir(), "review.json")}
	for index := 0; index < 15; index++ {
		repo.Files = append(repo.Files, diff.File{Path: fmt.Sprintf("file%d.go", index)})
	}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height, m.mode = 120, 30, searching
	m.updateSearch()

	updated, _ := m.Update(tea.MouseWheelMsg{X: 60, Y: 10, Button: tea.MouseWheelDown})
	m = updated.(Model)
	if m.searchAt != 0 || m.searchTop != mouseWheelStep {
		t.Fatalf("fuzzy wheel changed cursor or missed viewport: selected=%d top=%d", m.searchAt, m.searchTop)
	}
	plain := xansi.Strip(m.renderSearch())
	if strings.Contains(plain, "file0.go") || !strings.Contains(plain, "file3.go") {
		t.Fatalf("fuzzy viewport did not scroll independently:\n%s", plain)
	}

	_, _, top := m.modalLayout(m.renderSearch(), 72, 18)
	updated, _ = m.Update(tea.MouseClickMsg{X: 60, Y: top + 6, Button: tea.MouseLeft})
	m = updated.(Model)
	if m.searchAt != mouseWheelStep {
		t.Fatalf("click after fuzzy scroll selected %d, want %d", m.searchAt, mouseWheelStep)
	}
}

func TestMouseClickPositionsRepositorySearchResult(t *testing.T) {
	repo := &gitrepo.Repository{Root: t.TempDir(), Branch: "feature", Base: "main", ReviewPath: filepath.Join(t.TempDir(), "review.json")}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height, m.mode = 120, 30, searchingRepository
	m.repoSearchReady = true
	m.repoHits = []gitrepo.SearchMatch{
		{Path: "first.go", Line: 1, Context: []gitrepo.SearchLine{{Number: 1, Text: "first", Match: true}}},
		{Path: "second.go", Line: 2, Context: []gitrepo.SearchLine{{Number: 2, Text: "second", Match: true}}},
	}

	updated, _ := m.Update(tea.MouseClickMsg{X: 100, Y: 14, Button: tea.MouseLeft})
	m = updated.(Model)
	if m.repoSearchAt != 1 {
		t.Fatalf("repository result click selected %d, want 1", m.repoSearchAt)
	}
}

func TestMouseClickUsesScrolledRepositorySearchWindow(t *testing.T) {
	repo := &gitrepo.Repository{Root: t.TempDir(), Branch: "feature", Base: "main", ReviewPath: filepath.Join(t.TempDir(), "review.json")}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height, m.mode = 120, 24, searchingRepository
	m.repoSearchReady = true
	for index := 0; index < 4; index++ {
		m.repoHits = append(m.repoHits, gitrepo.SearchMatch{
			Path: fmt.Sprintf("result%d.go", index), Line: index + 1,
			Context: []gitrepo.SearchLine{{Number: index + 1, Text: "match", Match: true}},
		})
	}
	m.repoSearchAt = 3
	m.ensureRepositorySearchVisible()
	if m.repoSearchTop != 1 {
		t.Fatalf("search top = %d, want 1", m.repoSearchTop)
	}

	updated, _ := m.Update(tea.MouseClickMsg{X: 100, Y: 11, Button: tea.MouseLeft})
	m = updated.(Model)
	if m.repoSearchAt != 1 {
		t.Fatalf("click selected result %d, want first visible result 1", m.repoSearchAt)
	}
}

func TestMouseClickPositionsFuzzyFileSearchResult(t *testing.T) {
	repo := &gitrepo.Repository{Root: t.TempDir(), Branch: "feature", Base: "main", ReviewPath: filepath.Join(t.TempDir(), "review.json")}
	for index := 0; index < 4; index++ {
		repo.Files = append(repo.Files, diff.File{Path: fmt.Sprintf("file%d.go", index)})
	}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height, m.mode = 120, 30, searching
	m.searchHits = []int{0, 1, 2, 3}

	_, _, top := m.modalLayout(m.renderSearch(), 72, 18)
	updated, _ := m.Update(tea.MouseClickMsg{X: 60, Y: top + 8, Button: tea.MouseLeft})
	m = updated.(Model)
	if m.searchAt != 2 {
		t.Fatalf("fuzzy result click selected %d, want 2", m.searchAt)
	}
}

func TestMouseSelectedTextSeedsFindInRightPane(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	repo := &gitrepo.Repository{
		Root: t.TempDir(), Branch: "feature", Base: "main", ReviewPath: filepath.Join(t.TempDir(), "review.json"),
		Files: []diff.File{{
			Path: "service.go",
			Lines: []diff.Line{
				{Kind: diff.Meta, Text: "@@ -1 +1 @@"},
				{Kind: diff.Addition, Text: "func SharedMethod() {}", NewNumber: 1},
			},
		}},
	}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height, m.focus = 120, 20, focusDiff
	plainLines := strings.Split(xansi.Strip(m.View().Content), "\n")
	y, x := -1, -1
	for line, content := range plainLines {
		if byteIndex := strings.Index(content, "SharedMethod"); byteIndex >= 0 {
			y, x = line, lipgloss.Width(content[:byteIndex])
			break
		}
	}
	if y < 0 || x < m.filePaneWidth() {
		t.Fatalf("could not locate symbol in the rendered code pane: x=%d y=%d", x, y)
	}

	updated, _ := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	m = updated.(Model)
	updated, _ = m.Update(tea.MouseMotionMsg{X: x + len("SharedMethod") - 1, Y: y, Button: tea.MouseLeft})
	m = updated.(Model)
	updated, _ = m.Update(tea.MouseReleaseMsg{X: x + len("SharedMethod") - 1, Y: y, Button: tea.MouseLeft})
	m = updated.(Model)
	if m.selectedText != "SharedMethod" {
		t.Fatalf("mouse selection = %q, want SharedMethod", m.selectedText)
	}
	selectedView := m.View().Content
	if !strings.Contains(selectedView, "48;2;38;79;120") {
		t.Fatal("selected text did not receive the visible selection background")
	}
	buffer := uv.NewScreenBuffer(m.width, m.height)
	uv.NewStyledString(selectedView).Draw(buffer, buffer.Bounds())
	selectedCell := buffer.CellAt(x, y)
	if selectedCell == nil || selectedCell.Style.Bg == nil {
		t.Fatal("selected terminal cell has no background")
	}
	red, green, blue, _ := selectedCell.Style.Bg.RGBA()
	if red>>8 != 0x26 || green>>8 != 0x4f || blue>>8 != 0x78 {
		t.Fatalf("selected terminal cell background = #%02x%02x%02x, want #264f78", red>>8, green>>8, blue>>8)
	}
	if got, want := xansi.Strip(selectedView), strings.Join(plainLines, "\n"); got != want {
		t.Fatal("selection styling changed the rendered review text")
	}

	updated, cmd := m.Update(tea.KeyPressMsg{Text: "f", Code: 'f'})
	m = updated.(Model)
	if m.mode != searchingRepository || m.input != "SharedMethod" || !m.repoSearching || cmd == nil {
		t.Fatalf("selected find opened mode=%v input=%q searching=%v cmd=%v", m.mode, m.input, m.repoSearching, cmd)
	}
	findView := xansi.Strip(m.View().Content)
	if !strings.Contains(findView, "FILES") || !strings.Contains(findView, "FIND IN REPOSITORY") || strings.Contains(findView, "DIFF  UNIFIED") {
		t.Fatalf("find did not take over only the right pane:\n%s", findView)
	}
}

func TestMouseSelectionStaysInsideOriginatingCodePane(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	repo := &gitrepo.Repository{
		Root: t.TempDir(), Branch: "feature", Base: "main", ReviewPath: filepath.Join(t.TempDir(), "review.json"),
		Files: []diff.File{{
			Path: "deeply/nested/service.go",
			Lines: []diff.Line{
				{Kind: diff.Addition, Text: "first selected line", NewNumber: 1},
				{Kind: diff.Addition, Text: "second selected line", NewNumber: 2},
			},
		}},
	}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height, m.focus = 140, 20, focusDiff
	plainLines := strings.Split(xansi.Strip(m.View().Content), "\n")
	startY, startX, endY, endX := -1, -1, -1, -1
	for y, content := range plainLines {
		if byteIndex := strings.Index(content, "selected line"); byteIndex >= 0 {
			x := lipgloss.Width(content[:byteIndex])
			if startY < 0 {
				startY, startX = y, x
			} else {
				endY, endX = y, x+len("selected")-1
				break
			}
		}
	}
	if startY < 0 || endY < 0 || startX < m.filePaneWidth() {
		t.Fatalf("could not locate consecutive code rows: start=(%d,%d) end=(%d,%d)", startX, startY, endX, endY)
	}

	updated, _ := m.Update(tea.MouseClickMsg{X: startX, Y: startY, Button: tea.MouseLeft})
	m = updated.(Model)
	updated, _ = m.Update(tea.MouseMotionMsg{X: endX, Y: endY, Button: tea.MouseLeft})
	m = updated.(Model)
	updated, _ = m.Update(tea.MouseReleaseMsg{X: endX, Y: endY, Button: tea.MouseLeft})
	m = updated.(Model)

	if strings.Contains(m.selectedText, "service.go") {
		t.Fatalf("selected text leaked in from the file pane: %q", m.selectedText)
	}
	buffer := uv.NewScreenBuffer(m.width, m.height)
	uv.NewStyledString(m.View().Content).Draw(buffer, buffer.Bounds())
	if cellHasSelectionBackground(buffer.CellAt(5, endY)) {
		t.Fatal("selection background leaked into the file pane on a later selected row")
	}
	if !cellHasSelectionBackground(buffer.CellAt(m.filePaneWidth()+2, endY)) {
		t.Fatal("multiline selection did not highlight the start of the code pane on a later selected row")
	}
}

func TestSplitMouseSelectionStaysInsideOriginatingHalf(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	repo := &gitrepo.Repository{
		Root: t.TempDir(), Branch: "feature", Base: "main", ReviewPath: filepath.Join(t.TempDir(), "review.json"),
		Files: []diff.File{{Path: "service.go", Lines: []diff.Line{
			{Kind: diff.Deletion, Text: "old pane content", OldNumber: 1},
			{Kind: diff.Addition, Text: "new pane content", NewNumber: 1},
		}}},
	}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height, m.focus, m.view = 140, 20, focusDiff, split
	plainLines := strings.Split(xansi.Strip(m.View().Content), "\n")
	row, oldX, newX := -1, -1, -1
	for y, content := range plainLines {
		oldByte, newByte := strings.Index(content, "old pane content"), strings.Index(content, "new pane content")
		if oldByte >= 0 && newByte >= 0 {
			row = y
			oldX = lipgloss.Width(content[:oldByte])
			newX = lipgloss.Width(content[:newByte])
			break
		}
	}
	if row < 0 || oldX < m.filePaneWidth() || newX <= oldX {
		t.Fatalf("could not locate split content: row=%d old=%d new=%d", row, oldX, newX)
	}

	updated, _ := m.Update(tea.MouseClickMsg{X: newX + len("new pane content") - 1, Y: row, Button: tea.MouseLeft})
	m = updated.(Model)
	updated, _ = m.Update(tea.MouseMotionMsg{X: oldX, Y: row, Button: tea.MouseLeft})
	m = updated.(Model)
	updated, _ = m.Update(tea.MouseReleaseMsg{X: oldX, Y: row, Button: tea.MouseLeft})
	m = updated.(Model)
	if strings.Contains(m.selectedText, "old pane content") || !strings.Contains(m.selectedText, "new pane content") {
		t.Fatalf("right-origin selection crossed the divider: %q", m.selectedText)
	}
	buffer := uv.NewScreenBuffer(m.width, m.height)
	uv.NewStyledString(m.View().Content).Draw(buffer, buffer.Bounds())
	if cellHasSelectionBackground(buffer.CellAt(oldX, row)) {
		t.Fatal("right-origin selection background crossed into the left split pane")
	}
	if !cellHasSelectionBackground(buffer.CellAt(newX, row)) {
		t.Fatal("right-origin selection did not highlight its own split pane")
	}

	m.clearMouseSelection()
	m.invalidateRender()
	updated, _ = m.Update(tea.MouseClickMsg{X: oldX, Y: row, Button: tea.MouseLeft})
	m = updated.(Model)
	updated, _ = m.Update(tea.MouseMotionMsg{X: newX + len("new pane content") - 1, Y: row, Button: tea.MouseLeft})
	m = updated.(Model)
	updated, _ = m.Update(tea.MouseReleaseMsg{X: newX + len("new pane content") - 1, Y: row, Button: tea.MouseLeft})
	m = updated.(Model)
	if strings.Contains(m.selectedText, "new pane content") || !strings.Contains(m.selectedText, "old pane content") {
		t.Fatalf("left-origin selection crossed the divider: %q", m.selectedText)
	}
	buffer = uv.NewScreenBuffer(m.width, m.height)
	uv.NewStyledString(m.View().Content).Draw(buffer, buffer.Bounds())
	if !cellHasSelectionBackground(buffer.CellAt(oldX, row)) {
		t.Fatal("left-origin selection did not highlight its own split pane")
	}
	if cellHasSelectionBackground(buffer.CellAt(newX, row)) {
		t.Fatal("left-origin selection background crossed into the right split pane")
	}
}

func cellHasSelectionBackground(cell *uv.Cell) bool {
	if cell == nil || cell.Style.Bg == nil {
		return false
	}
	red, green, blue, _ := cell.Style.Bg.RGBA()
	return red>>8 == 0x26 && green>>8 == 0x4f && blue>>8 == 0x78
}
