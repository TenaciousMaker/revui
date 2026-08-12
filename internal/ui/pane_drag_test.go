package ui

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/TenaciousMaker/revui/internal/config"
	"github.com/TenaciousMaker/revui/internal/diff"
	"github.com/TenaciousMaker/revui/internal/gitrepo"
)

func paneDragTestRepository(t *testing.T) *gitrepo.Repository {
	t.Helper()
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
	return repo
}

func paneDragTestModel(t *testing.T) Model {
	t.Helper()
	m, err := newTestModel(t, paneDragTestRepository(t))
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height = 140, 20
	return m
}

func dragDivider(t *testing.T, m Model, fromX, toX, y int) Model {
	t.Helper()
	updated, _ := m.Update(tea.MouseClickMsg{X: fromX, Y: y, Button: tea.MouseLeft})
	m = updated.(Model)
	updated, _ = m.Update(tea.MouseMotionMsg{X: toX, Y: y, Button: tea.MouseLeft})
	m = updated.(Model)
	updated, _ = m.Update(tea.MouseReleaseMsg{X: toX, Y: y, Button: tea.MouseLeft})
	return updated.(Model)
}

func TestPaneDividerDragResizesFilePane(t *testing.T) {
	m := paneDragTestModel(t)
	divider := m.filePaneWidth() - 1
	m = dragDivider(t, m, divider, 60, 8)
	if m.filePaneWidth() != 61 {
		t.Fatalf("file pane width = %d after dragging divider to column 60, want 61", m.filePaneWidth())
	}
	if m.paneDragging || m.mouseSelecting || m.selectedText != "" {
		t.Fatalf("divider drag left stray state: dragging=%v selecting=%v selected=%q", m.paneDragging, m.mouseSelecting, m.selectedText)
	}
	if m.status != "File pane width set to 61 columns." {
		t.Fatalf("divider drag status = %q", m.status)
	}
}

func TestPaneDividerGrabToleranceCoversAdjacentColumns(t *testing.T) {
	m := paneDragTestModel(t)
	divider := m.filePaneWidth() - 1
	for _, x := range []int{divider - 1, divider, divider + 1} {
		updated, _ := m.Update(tea.MouseClickMsg{X: x, Y: 8, Button: tea.MouseLeft})
		clicked := updated.(Model)
		if !clicked.paneDragging {
			t.Fatalf("click at column %d (divider %d) did not start a pane drag", x, divider)
		}
		if clicked.mouseSelecting || clicked.file != m.file {
			t.Fatalf("divider click at column %d leaked into pane handling: selecting=%v file=%d", x, clicked.mouseSelecting, clicked.file)
		}
	}
}

func TestPaneDividerDragClampsToLayoutBounds(t *testing.T) {
	m := paneDragTestModel(t)
	divider := m.filePaneWidth() - 1
	m = dragDivider(t, m, divider, 5, 8)
	if m.filePaneWidth() != 26 {
		t.Fatalf("file pane width = %d after dragging far left, want minimum 26", m.filePaneWidth())
	}
	divider = m.filePaneWidth() - 1
	m = dragDivider(t, m, divider, 135, 8)
	if m.filePaneWidth() != m.width-48 {
		t.Fatalf("file pane width = %d after dragging far right, want maximum %d", m.filePaneWidth(), m.width-48)
	}
}

func TestDraggedPaneWidthReclampsAfterTerminalResize(t *testing.T) {
	m := paneDragTestModel(t)
	m = dragDivider(t, m, m.filePaneWidth()-1, 135, 8)
	if m.filePaneWidth() != 92 {
		t.Fatalf("file pane width = %d before resize, want 92", m.filePaneWidth())
	}
	m.width = 100
	if m.filePaneWidth() != 52 {
		t.Fatalf("file pane width = %d after shrinking terminal to 100, want 52", m.filePaneWidth())
	}
}

func TestPaneDividerClicksOutsideDividerStillHitPanes(t *testing.T) {
	m := paneDragTestModel(t)
	updated, _ := m.Update(tea.MouseClickMsg{X: 5, Y: 7, Button: tea.MouseLeft})
	m = updated.(Model)
	if m.paneDragging || m.file != 2 {
		t.Fatalf("file pane click: dragging=%v file=%d, want plain row selection of file 2", m.paneDragging, m.file)
	}
	updated, _ = m.Update(tea.MouseClickMsg{X: 100, Y: 8, Button: tea.MouseLeft})
	m = updated.(Model)
	if m.paneDragging || !m.mouseSelecting {
		t.Fatalf("code pane click: dragging=%v selecting=%v, want text selection start", m.paneDragging, m.mouseSelecting)
	}
}

func TestPaneDividerDragIgnoredOnNarrowTerminal(t *testing.T) {
	m := paneDragTestModel(t)
	m.width = 80
	updated, _ := m.Update(tea.MouseClickMsg{X: m.filePaneWidth() - 1, Y: 8, Button: tea.MouseLeft})
	m = updated.(Model)
	if m.paneDragging {
		t.Fatal("narrow single-pane layout started a divider drag")
	}
}

func TestPaneDividerReleaseDoesNotExpandHunkGap(t *testing.T) {
	repo := hunkExpansionTestRepository(t)
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height, m.focus = 140, 24, focusDiff
	gap := collapsedLineIndex(m.currentLines())
	if gap < 0 {
		t.Fatal("collapsed gap missing")
	}
	m.line = gap
	divider := m.filePaneWidth() - 1
	updated, _ := m.Update(tea.MouseClickMsg{X: divider, Y: 8, Button: tea.MouseLeft})
	m = updated.(Model)
	updated, command := m.Update(tea.MouseReleaseMsg{X: 60, Y: 8, Button: tea.MouseLeft})
	m = updated.(Model)
	if command != nil {
		t.Fatal("releasing a divider drag scheduled a hunk expansion")
	}
	if collapsedLineIndex(m.currentLines()) < 0 {
		t.Fatal("releasing a divider drag expanded the selected hunk gap")
	}
}

func TestWideFilesToggleClearsDraggedWidth(t *testing.T) {
	m := paneDragTestModel(t)
	compact := m.filePaneWidth()
	m = dragDivider(t, m, compact-1, 60, 8)
	m.toggleFilePaneWidth()
	if m.filePaneWidth() == 61 {
		t.Fatal("wide-files toggle kept the dragged width override")
	}
	m.toggleFilePaneWidth()
	if m.filePaneWidth() != compact {
		t.Fatalf("file pane width = %d after toggling back, want automatic compact %d", m.filePaneWidth(), compact)
	}
}

func TestDraggedPaneWidthPersistsAcrossSessions(t *testing.T) {
	repo := paneDragTestRepository(t)
	repo.PreferencesPath = filepath.Join(t.TempDir(), "preferences.json")
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height = 140, 20
	m = dragDivider(t, m, m.filePaneWidth()-1, 60, 8)
	saved, err := config.Load(repo.PreferencesPath)
	if err != nil || saved.FilePaneWidth != 61 {
		t.Fatalf("persisted file pane width = %d, %v; want 61", saved.FilePaneWidth, err)
	}
	next, err := New(paneDragTestRepositoryAt(t, repo.PreferencesPath))
	if err != nil {
		t.Fatal(err)
	}
	next.width, next.height = 140, 20
	if next.filePaneWidth() != 61 {
		t.Fatalf("restored file pane width = %d, want 61", next.filePaneWidth())
	}
}

func paneDragTestRepositoryAt(t *testing.T, preferencesPath string) *gitrepo.Repository {
	t.Helper()
	repo := paneDragTestRepository(t)
	repo.PreferencesPath = preferencesPath
	return repo
}

func clickDivider(t *testing.T, m Model, x, y int) Model {
	t.Helper()
	updated, _ := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	m = updated.(Model)
	updated, _ = m.Update(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
	return updated.(Model)
}

func TestPaneDividerDoubleClickResetsWidth(t *testing.T) {
	repo := paneDragTestRepository(t)
	repo.PreferencesPath = filepath.Join(t.TempDir(), "preferences.json")
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height = 140, 20
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	compact := m.filePaneWidth()
	m = dragDivider(t, m, compact-1, 60, 8)
	if m.filePaneWidth() != 61 {
		t.Fatalf("file pane width = %d after drag, want 61", m.filePaneWidth())
	}
	now = now.Add(time.Second)
	m = clickDivider(t, m, m.filePaneWidth()-1, 8)
	now = now.Add(100 * time.Millisecond)
	m = clickDivider(t, m, m.filePaneWidth()-1, 8)
	if m.filePaneWidth() != compact {
		t.Fatalf("file pane width = %d after double click, want automatic %d", m.filePaneWidth(), compact)
	}
	if m.status != "File pane width reset to automatic." {
		t.Fatalf("double-click status = %q", m.status)
	}
	saved, err := config.Load(repo.PreferencesPath)
	if err != nil || saved.FilePaneWidth != 0 {
		t.Fatalf("persisted file pane width = %d, %v; want 0", saved.FilePaneWidth, err)
	}
}

func TestPaneDividerSlowSecondClickDoesNotReset(t *testing.T) {
	m := paneDragTestModel(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	m = dragDivider(t, m, m.filePaneWidth()-1, 60, 8)
	now = now.Add(time.Second)
	m = clickDivider(t, m, m.filePaneWidth()-1, 8)
	now = now.Add(time.Second)
	m = clickDivider(t, m, m.filePaneWidth()-1, 8)
	if m.filePaneWidth() != 61 {
		t.Fatalf("file pane width = %d after two slow clicks, want dragged 61", m.filePaneWidth())
	}
}

func TestPaneDividerDragThenQuickClickDoesNotReset(t *testing.T) {
	m := paneDragTestModel(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	m = dragDivider(t, m, m.filePaneWidth()-1, 60, 8)
	m = clickDivider(t, m, m.filePaneWidth()-1, 8)
	if m.filePaneWidth() != 61 {
		t.Fatalf("file pane width = %d after drag then quick click, want dragged 61", m.filePaneWidth())
	}
}

func TestPaneDividerDoubleClickReleaseDoesNotExpandHunkGap(t *testing.T) {
	repo := hunkExpansionTestRepository(t)
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height, m.focus = 140, 24, focusDiff
	gap := collapsedLineIndex(m.currentLines())
	if gap < 0 {
		t.Fatal("collapsed gap missing")
	}
	m.line = gap
	divider := m.filePaneWidth() - 1
	m = clickDivider(t, m, divider, 8)
	updated, _ := m.Update(tea.MouseClickMsg{X: divider, Y: 8, Button: tea.MouseLeft})
	m = updated.(Model)
	updated, command := m.Update(tea.MouseReleaseMsg{X: divider, Y: 8, Button: tea.MouseLeft})
	m = updated.(Model)
	if command != nil {
		t.Fatal("double-click release on the divider scheduled a hunk expansion")
	}
	if collapsedLineIndex(m.currentLines()) < 0 {
		t.Fatal("double-click release on the divider expanded the selected hunk gap")
	}
}
