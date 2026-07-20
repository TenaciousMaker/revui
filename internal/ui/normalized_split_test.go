package ui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/TenaciousMaker/revui/internal/diff"
	"github.com/TenaciousMaker/revui/internal/gitrepo"
	"github.com/TenaciousMaker/revui/internal/semantic"
)

func TestNormalizedToggleEnablesSemanticSplitAndSemanticOffRestoresRaw(t *testing.T) {
	repo := &gitrepo.Repository{Files: []diff.File{{Path: "widget.ts", Lines: []diff.Line{{Kind: diff.Addition, NewNumber: 1, Text: "const value = 1;"}}}}}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	updated, _ := m.Update(tea.KeyPressMsg{Text: "n", Code: 'n'})
	m = updated.(Model)
	if !m.normalizedLayout || !m.semanticReflow || m.view != split {
		t.Fatalf("normalized toggle state: normalized=%v semantic=%v view=%v", m.normalizedLayout, m.semanticReflow, m.view)
	}
	updated, _ = m.Update(tea.KeyPressMsg{Text: "e", Code: 'e'})
	m = updated.(Model)
	if m.normalizedLayout || m.semanticReflow {
		t.Fatalf("semantic off left normalized mode enabled: normalized=%v semantic=%v", m.normalizedLayout, m.semanticReflow)
	}
}

func TestDifftasticSplitProjectsAlignmentWithoutHidingGitRows(t *testing.T) {
	oldSource := []byte("import {\n  type ReactNode,\n  useEffect,\n  useLayoutEffect,\n  useMemo,\n} from 'react';\nconst value = 1;\n")
	newSource := []byte("import { type ReactNode, useEffect, useMemo } from 'react';\nconst value = 1;\n")
	lines := []diff.Line{
		{Kind: diff.Meta, Hunk: 0, Text: "@@ -1,7 +1,2 @@", OriginalIndex: 0},
		{Kind: diff.Deletion, Hunk: 0, OldNumber: 1, Text: "import {", OriginalIndex: 1},
		{Kind: diff.Deletion, Hunk: 0, OldNumber: 2, Text: "  type ReactNode,", OriginalIndex: 2},
		{Kind: diff.Deletion, Hunk: 0, OldNumber: 3, Text: "  useEffect,", OriginalIndex: 3},
		{Kind: diff.Deletion, Hunk: 0, OldNumber: 4, Text: "  useLayoutEffect,", OriginalIndex: 4},
		{Kind: diff.Deletion, Hunk: 0, OldNumber: 5, Text: "  useMemo,", OriginalIndex: 5},
		{Kind: diff.Deletion, Hunk: 0, OldNumber: 6, Text: "} from 'react';", OriginalIndex: 6},
		{Kind: diff.Addition, Hunk: 0, NewNumber: 1, Text: "import { type ReactNode, useEffect, useMemo } from 'react';", OriginalIndex: 7},
		{Kind: diff.Context, Hunk: 0, OldNumber: 7, NewNumber: 2, Text: "const value = 1;", OriginalIndex: 8},
	}
	alignment := []semantic.LineAlignment{{Old: 1, New: 1}, {Old: 2}, {Old: 3}, {Old: 4}, {Old: 5}, {Old: 6}, {Old: 7, New: 2}}
	spans := map[int][]textSpan{4: {{start: 2, end: 17}}}
	rows, ok := buildDifftasticSplitRows(lines, alignment, oldSource, newSource, spans)
	if !ok {
		t.Fatal("complete Difftastic alignment was rejected")
	}
	if len(rows) != 8 {
		t.Fatalf("row count = %d, want 8", len(rows))
	}
	var removed *splitRow
	for index := range rows {
		if rows[index].old != nil && strings.Contains(rows[index].old.Text, "useLayoutEffect") {
			removed = &rows[index]
			break
		}
	}
	if removed == nil || removed.old.Kind != diff.Deletion || removed.new != nil {
		t.Fatalf("removed import row = %#v", removed)
	}
	if got := selectedSpanText(removed.old.Text, removed.oldSpans); got != "useLayoutEffect" {
		t.Fatalf("removed import emphasis = %q", got)
	}
	if rows[2].old == nil || rows[2].old.Kind != diff.Context {
		t.Fatalf("unchanged reformatted import was not neutral: %#v", rows[2])
	}
	if rows[len(rows)-1].oldIndex != 8 || rows[len(rows)-1].newIndex != 8 {
		t.Fatalf("context coverage = %#v", rows[len(rows)-1])
	}

	if _, complete := buildDifftasticSplitRows(lines, alignment[:len(alignment)-1], oldSource, newSource, spans); complete {
		t.Fatal("incomplete alignment was accepted")
	}
}

func TestDifftasticSplitClipsLongRightRowsToTheirPane(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	oldLines := []string{
		"## Week 2026-W30 (Jul 20 - Jul 26)",
		"",
		"**[Context]**: Resolve Order and OrderItem through describes.",
		"## Week 2026-W29 (Jul 13 - Jul 19)",
	}
	newLines := []string{
		"## Week 2026-W30 (Jul 20 - Jul 26)",
		"",
		"**[Workspace]**: Priority Accounts stops flagging its full rank-ordered top-N as partial; other list widgets name the row cap in the partial response and keep the surrounding explanation visible.",
		"**[Context]**: Resolve Order and OrderItem through describes.",
	}
	lines := []diff.Line{
		{Kind: diff.Meta, Hunk: 0, Text: "@@ -1,4 +1,5 @@", OriginalIndex: 0},
		{Kind: diff.Context, Hunk: 0, OldNumber: 1, NewNumber: 1, Text: oldLines[0], OriginalIndex: 1},
		{Kind: diff.Context, Hunk: 0, OldNumber: 2, NewNumber: 2, Text: oldLines[1], OriginalIndex: 2},
		{Kind: diff.Addition, Hunk: 0, NewNumber: 3, Text: newLines[2], OriginalIndex: 3},
		{Kind: diff.Context, Hunk: 0, OldNumber: 3, NewNumber: 4, Text: oldLines[2], OriginalIndex: 4},
		{Kind: diff.Context, Hunk: 0, OldNumber: 4, NewNumber: 5, Text: oldLines[3], OriginalIndex: 5},
	}
	repo := &gitrepo.Repository{Files: []diff.File{{Path: "CHANGELOG.md", Lines: lines}}}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.focus, m.view, m.difftasticMode, m.semanticReflow = focusDiff, split, true, true
	m.semantic.repo, m.semantic.file, m.semantic.ready = repo, 0, true
	m.semantic.engine, m.semantic.provider = semantic.EngineDifftastic, difftasticSemantic
	m.semantic.oldSource = []byte(strings.Join(oldLines, "\n") + "\n")
	m.semantic.newSource = []byte(strings.Join(newLines, "\n") + "\n")
	m.semantic.spans = map[int][]textSpan{3: {{start: 0, end: len([]rune(newLines[2]))}}}
	m.semantic.alignment = []semantic.LineAlignment{
		{Old: 1, New: 1},
		{Old: 2, New: 2},
		{New: 3},
		{Old: 3, New: 4},
		{Old: 4, New: 5},
	}

	const width = 120
	rendered := m.renderSplit(width, 8)
	for row, text := range strings.Split(rendered, "\n") {
		if got := ansi.StringWidth(text); got > width {
			t.Fatalf("Difftastic split row %d crossed viewport: width %d, want <= %d\n%s", row, got, width, ansi.Strip(rendered))
		}
	}
	plainRows := strings.Split(ansi.Strip(rendered), "\n")
	addedRow := -1
	for row, text := range plainRows {
		if strings.Contains(text, "Priority Accounts") {
			addedRow = row
			break
		}
	}
	if addedRow < 0 {
		t.Fatalf("long added row missing:\n%s", ansi.Strip(rendered))
	}
	buffer := uv.NewScreenBuffer(width, len(plainRows))
	uv.NewStyledString(rendered).Draw(buffer, buffer.Bounds())
	for column := 0; column < (width-1)/2; column++ {
		cell := buffer.CellAt(column, addedRow)
		if cell == nil || cell.Style.Bg == nil {
			continue
		}
		red, green, blue, _ := cell.Style.Bg.RGBA()
		if got := fmt.Sprintf("#%02X%02X%02X", red>>8, green>>8, blue>>8); got == addedLineBackground {
			t.Fatalf("right-side addition bled into left pane at row %d column %d:\n%s", addedRow, column, ansi.Strip(rendered))
		}
	}

	m.width, m.height = 204, 65
	for row, text := range strings.Split(m.View().Content, "\n") {
		if got := ansi.StringWidth(text); got > m.width {
			t.Fatalf("full Difftastic view row %d crossed terminal: width %d, want <= %d\n%s", row, got, m.width, ansi.Strip(m.View().Content))
		}
	}
}

func TestSplitCellUsesNumberForRenderedSide(t *testing.T) {
	repo := &gitrepo.Repository{Files: []diff.File{{Path: "value.ts"}}}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	line := diff.Line{Kind: diff.Context, NewNumber: 12, Text: "new-only neutral alignment"}
	rendered := ansi.Strip(m.renderSplitCellAt(&line, -1, 40, false, false, nil, true, nil))
	if !strings.Contains(rendered, " 12   new-only") {
		t.Fatalf("new-side line number missing:\n%s", rendered)
	}
}

func TestNormalizedToggleDoesNotReportAnalyzingWhenASTIsAlreadyReady(t *testing.T) {
	repo := &gitrepo.Repository{Files: []diff.File{{Path: "widget.ts", Lines: []diff.Line{{Kind: diff.Addition, NewNumber: 1, Text: "const value = 1;"}}}}}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.semanticReflow = true
	m.semantic.repo, m.semantic.file, m.semantic.ready = repo, 0, true
	m.semantic.engine, m.semantic.layout = semantic.EngineAST, &semantic.Layout{}
	updated, _ := m.Update(tea.KeyPressMsg{Text: "n", Code: 'n'})
	m = updated.(Model)
	if strings.Contains(m.status, "analyzing") {
		t.Fatalf("ready AST layout reports stale progress: %q", m.status)
	}
}

func TestNormalizedTogglePreservesCursorAndViewportSourceAnchors(t *testing.T) {
	oldSource := []byte("const { items, isLoading, rowLimit } = useNextMoves({ focus: widgetFocus });\n")
	newSource := []byte("const {\n  items,\n  isLoading,\n  rowLimit,\n  settings,\n} = useNextMoves();\n")
	plan, err := semantic.New(0).Analyze(context.Background(), semantic.Input{Path: "widget.ts", Old: oldSource, New: newSource})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Layout == nil {
		t.Skip("normalized AST layout unavailable without cgo")
	}
	lines := []diff.Line{{Kind: diff.Meta, Text: "@@ -1 +1,6 @@", Hunk: 0}}
	lines = append(lines, diff.Line{Kind: diff.Deletion, Text: strings.TrimSuffix(string(oldSource), "\n"), OldNumber: 1, Hunk: 0})
	for index, text := range strings.Split(strings.TrimSuffix(string(newSource), "\n"), "\n") {
		lines = append(lines, diff.Line{Kind: diff.Addition, Text: text, NewNumber: index + 1, Hunk: 0})
	}
	repo := &gitrepo.Repository{Files: []diff.File{{Path: "widget.ts", Lines: lines}}}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.focus, m.view, m.width, m.height = focusDiff, split, 120, 10
	m.semanticReflow = true
	m.semantic.repo, m.semantic.file, m.semantic.ready = repo, 0, true
	m.semantic.engine, m.semantic.layout = semantic.EngineAST, plan.Layout
	m.semantic.oldSource, m.semantic.newSource = oldSource, newSource

	rawRows := m.currentSplitRows()
	m.splitCursor, m.splitScroll = 4, 2
	wantCursorNew, wantTopNew := rawRows[m.splitCursor].newIndex, rawRows[m.splitScroll].newIndex
	updated, _ := m.Update(tea.KeyPressMsg{Text: "n", Code: 'n'})
	m = updated.(Model)
	normalizedRows := m.currentSplitRows()
	if got := normalizedRows[m.splitCursor].newIndex; got != wantCursorNew {
		t.Fatalf("normalized cursor source index = %d, want %d", got, wantCursorNew)
	}
	if got := normalizedRows[m.splitScroll].newIndex; got != wantTopNew {
		t.Fatalf("normalized viewport source index = %d, want %d", got, wantTopNew)
	}

	wantCursorNew, wantTopNew = normalizedRows[m.splitCursor].newIndex, normalizedRows[m.splitScroll].newIndex
	updated, _ = m.Update(tea.KeyPressMsg{Text: "n", Code: 'n'})
	m = updated.(Model)
	rawRows = m.currentSplitRows()
	if got := rawRows[m.splitCursor].newIndex; got != wantCursorNew {
		t.Fatalf("raw cursor source index = %d, want %d", got, wantCursorNew)
	}
	if got := rawRows[m.splitScroll].newIndex; got != wantTopNew {
		t.Fatalf("raw viewport source index = %d, want %d", got, wantTopNew)
	}
}

func TestNormalizedSplitAlignsDestructuringAndCallChanges(t *testing.T) {
	oldSource := []byte("const { items, isLoading, rowLimit } = useNextMoves({ focus: widgetFocus });\n")
	newSource := []byte("const {\n  items,\n  isLoading,\n  rowLimit,\n  settings,\n} = useNextMoves();\n")
	plan, err := semantic.New(0).Analyze(context.Background(), semantic.Input{Path: "widget.ts", Old: oldSource, New: newSource})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Layout == nil {
		t.Skip("normalized AST layout unavailable without cgo")
	}
	lines := []diff.Line{{Kind: diff.Meta, Text: "@@ -1 +1,6 @@", Hunk: 0}}
	lines = append(lines, diff.Line{Kind: diff.Deletion, Text: strings.TrimSuffix(string(oldSource), "\n"), OldNumber: 1, Hunk: 0, OriginalIndex: 1})
	for index, text := range strings.Split(strings.TrimSuffix(string(newSource), "\n"), "\n") {
		lines = append(lines, diff.Line{Kind: diff.Addition, Text: text, NewNumber: index + 1, Hunk: 0, OriginalIndex: index + 2})
	}

	rows := buildNormalizedSplitRows(lines, plan.Layout, oldSource, newSource)
	assertNormalizedPair(t, rows, "items,", "items,", diff.Context, diff.Context)
	assertNormalizedPair(t, rows, "rowLimit", "rowLimit,", diff.Context, diff.Context)
	assertNormalizedPair(t, rows, "", "settings,", diff.Context, diff.Addition)
	assertNormalizedPair(t, rows, "} = useNextMoves({", "} = useNextMoves();", diff.Deletion, diff.Addition)
	assertNormalizedPair(t, rows, "focus: widgetFocus", "", diff.Deletion, diff.Context)
}

func TestNormalizedSplitShowsOnlyAddedDestructuredBinding(t *testing.T) {
	oldSource := []byte("const { data: componentProfiles, isLoading: isProfilesLoading } =\n  useComponentProfiles({ sellerProfileId });\n")
	newSource := []byte("const {\n  data: componentProfiles,\n  isLoading: isProfilesLoading,\n  error: profilesError,\n} = useComponentProfiles({ sellerProfileId });\n")
	plan, err := semantic.New(0).Analyze(context.Background(), semantic.Input{Path: "widget.tsx", Old: oldSource, New: newSource})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Layout == nil {
		t.Skip("normalized AST layout unavailable without cgo")
	}
	lines := []diff.Line{{Kind: diff.Meta, Text: "@@ -1,2 +1,5 @@", Hunk: 0}}
	for index, text := range strings.Split(strings.TrimSuffix(string(oldSource), "\n"), "\n") {
		lines = append(lines, diff.Line{Kind: diff.Deletion, Text: text, OldNumber: index + 1, Hunk: 0})
	}
	for index, text := range strings.Split(strings.TrimSuffix(string(newSource), "\n"), "\n") {
		lines = append(lines, diff.Line{Kind: diff.Addition, Text: text, NewNumber: index + 1, Hunk: 0})
	}

	rows := buildNormalizedSplitRows(lines, plan.Layout, oldSource, newSource)
	assertNormalizedPair(t, rows, "const {", "const {", diff.Context, diff.Context)
	assertNormalizedPair(t, rows, "data: componentProfiles,", "data: componentProfiles,", diff.Context, diff.Context)
	assertNormalizedPair(t, rows, "isLoading: isProfilesLoading", "isLoading: isProfilesLoading,", diff.Context, diff.Context)
	assertNormalizedPair(t, rows, "", "error: profilesError,", diff.Context, diff.Addition)
	assertNormalizedPair(t, rows, "} = useComponentProfiles({", "} = useComponentProfiles({", diff.Context, diff.Context)
	for _, row := range rows {
		if row.old != nil && strings.TrimSpace(row.old.Text) == "} = useComponentProfiles({" {
			if len(row.oldIndices) != 2 || len(row.newIndices) != 1 {
				t.Fatalf("joined row source coverage = old %v new %v, want two old lines and one new line", row.oldIndices, row.newIndices)
			}
			return
		}
	}
	t.Fatal("joined initializer row not found")
}

func TestNormalizedSplitFallsBackForUnownedArrayExpression(t *testing.T) {
	oldSource := []byte("[components, definitionsById, entityType, targetRecord]\n")
	newSource := []byte("[\n  components,\n  componentProfiles,\n  definitionsById,\n  entityType,\n  sellerProfileId,\n  targetRecord,\n]\n")
	plan, err := semantic.New(0).Analyze(context.Background(), semantic.Input{Path: "widget.tsx", Old: oldSource, New: newSource})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Layout == nil {
		t.Skip("normalized AST layout unavailable without cgo")
	}
	lines := []diff.Line{{Kind: diff.Meta, Text: "@@ -1 +1,8 @@", Hunk: 0}}
	lines = append(lines, diff.Line{Kind: diff.Deletion, Text: strings.TrimSuffix(string(oldSource), "\n"), OldNumber: 1, Hunk: 0})
	for index, text := range strings.Split(strings.TrimSuffix(string(newSource), "\n"), "\n") {
		lines = append(lines, diff.Line{Kind: diff.Addition, Text: text, NewNumber: index + 1, Hunk: 0})
	}
	rows := buildNormalizedSplitRows(lines, plan.Layout, oldSource, newSource)
	assertNormalizedPair(t, rows, "[components, definitionsById, entityType, targetRecord]", "[", diff.Deletion, diff.Addition)
	for _, row := range rows {
		if row.normalized {
			t.Fatalf("ambiguous expression was normalized:\n%s", normalizedRowsText(rows))
		}
	}
}

func TestNormalizedSplitStacksNamedImportsOnBothSides(t *testing.T) {
	oldSource := []byte("import {\n  type ReactNode,\n  useEffect,\n  useLayoutEffect,\n  useMemo,\n  useRef,\n  useState,\n} from 'react';\n")
	newSource := []byte("import { type ReactNode, useEffect, useMemo, useRef, useState } from 'react';\n")
	plan, err := semantic.New(0).Analyze(context.Background(), semantic.Input{Path: "component.tsx", Old: oldSource, New: newSource})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Layout == nil {
		t.Skip("normalized AST layout unavailable without cgo")
	}
	lines := []diff.Line{{Kind: diff.Meta, Text: "@@ -1,8 +1 @@", Hunk: 0}}
	for index, text := range strings.Split(strings.TrimSuffix(string(oldSource), "\n"), "\n") {
		lines = append(lines, diff.Line{Kind: diff.Deletion, Text: text, OldNumber: index + 1, Hunk: 0})
	}
	lines = append(lines, diff.Line{Kind: diff.Addition, Text: strings.TrimSuffix(string(newSource), "\n"), NewNumber: 1, Hunk: 0})

	rows := buildNormalizedSplitRows(lines, plan.Layout, oldSource, newSource)
	assertNormalizedPair(t, rows, "type ReactNode,", "type ReactNode,", diff.Context, diff.Context)
	assertNormalizedPair(t, rows, "useEffect,", "useEffect,", diff.Context, diff.Context)
	assertNormalizedPair(t, rows, "useLayoutEffect,", "", diff.Deletion, diff.Context)
	assertNormalizedPair(t, rows, "useState,", "useState", diff.Context, diff.Context)
	assertNormalizedPair(t, rows, "} from 'react';", "} from 'react';", diff.Context, diff.Context)
}

func TestNormalizedSplitKeepsAdjacentImportsWithTheirModule(t *testing.T) {
	oldSource := []byte("// @vitest-environment jsdom\nimport { cleanup, render, screen } from '@testing-library/react';\n")
	newSource := []byte("// @vitest-environment jsdom\nimport { useState } from 'react';\n\nimport { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';\n")
	plan, err := semantic.New(0).Analyze(context.Background(), semantic.Input{Path: "component.test.tsx", Old: oldSource, New: newSource})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Layout == nil {
		t.Skip("normalized AST layout unavailable without cgo")
	}
	lines := []diff.Line{
		{Kind: diff.Meta, Text: "@@ -1,2 +1,4 @@", Hunk: 0},
		{Kind: diff.Context, Text: "// @vitest-environment jsdom", OldNumber: 1, NewNumber: 1, Hunk: 0},
		{Kind: diff.Deletion, Text: "import { cleanup, render, screen } from '@testing-library/react';", OldNumber: 2, Hunk: 0},
		{Kind: diff.Addition, Text: "import { useState } from 'react';", NewNumber: 2, Hunk: 0},
		{Kind: diff.Addition, Text: "", NewNumber: 3, Hunk: 0},
		{Kind: diff.Addition, Text: "import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';", NewNumber: 4, Hunk: 0},
	}
	rows := buildNormalizedSplitRows(lines, plan.Layout, oldSource, newSource)
	assertNormalizedPair(t, rows, "", "import { useState } from 'react';", diff.Context, diff.Addition)
	assertNormalizedPair(t, rows, "cleanup,", "cleanup,", diff.Context, diff.Context)
	assertNormalizedPair(t, rows, "", "fireEvent,", diff.Context, diff.Addition)
	assertNormalizedPair(t, rows, "", "waitFor", diff.Context, diff.Addition)
}

func TestNormalizedSplitRendersOneToManyDeclarationComposite(t *testing.T) {
	oldSource := []byte("const { data: status, isLoading: isStatusLoading } = useMessagePackGenerationStatus(strategyId);\n")
	newSource := []byte("const { progress, isLoading: isProgressLoading, artifactRefreshStatus, retryArtifactRefresh } = useMessagePackProgress(strategyId);\nconst { data: stakeholderOperationStatus } = useMessagePackOperationStatus(strategyId, progress?.operationId, progress?.operationCode === 'stakeholder');\n")
	plan, err := semantic.New(0).Analyze(context.Background(), semantic.Input{Path: "hooks.tsx", Old: oldSource, New: newSource})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Layout == nil {
		t.Skip("normalized AST layout unavailable without cgo")
	}
	lines := []diff.Line{{Kind: diff.Meta, Text: "@@ -1 +1,2 @@", Hunk: 0}}
	lines = append(lines, diff.Line{Kind: diff.Deletion, Text: strings.TrimSuffix(string(oldSource), "\n"), OldNumber: 1, Hunk: 0})
	for index, text := range strings.Split(strings.TrimSuffix(string(newSource), "\n"), "\n") {
		lines = append(lines, diff.Line{Kind: diff.Addition, Text: text, NewNumber: index + 1, Hunk: 0})
	}
	rows := buildNormalizedSplitRows(lines, plan.Layout, oldSource, newSource)
	assertNormalizedPair(t, rows, "data: status,", "progress,", diff.Deletion, diff.Addition)
	assertNormalizedPair(t, rows, "} = useMessagePackGenerationStatus(strategyId);", "} = useMessagePackProgress(strategyId);", diff.Deletion, diff.Addition)
	assertNormalizedPair(t, rows, "", "data: stakeholderOperationStatus", diff.Context, diff.Addition)
}

func TestNormalizedSplitProjectsPartialOwnerChange(t *testing.T) {
	oldSource := []byte("const visible = useMemo(\n  () => getVisibleComponents({ components }),\n  [components, definitionsById, entityType, targetRecord],\n);\n")
	newSource := []byte("const visible = useMemo(\n  () => getVisibleComponents({ components }),\n  [\n    components,\n    componentProfiles,\n    definitionsById,\n    entityType,\n    sellerProfileId,\n    targetRecord,\n  ],\n);\n")
	plan, err := semantic.New(0).Analyze(context.Background(), semantic.Input{Path: "widget.tsx", Old: oldSource, New: newSource})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Layout == nil {
		t.Skip("normalized AST layout unavailable without cgo")
	}
	if len(plan.Layout.Blocks) == 0 {
		t.Fatalf("same declaration has no semantic layout block: %#v", plan.Layout)
	}
	lines := []diff.Line{
		{Kind: diff.Meta, Text: "@@ -1,4 +1,11 @@", Hunk: 0},
		{Kind: diff.Context, Text: "const visible = useMemo(", OldNumber: 1, NewNumber: 1, Hunk: 0},
		{Kind: diff.Context, Text: "  () => getVisibleComponents({ components }),", OldNumber: 2, NewNumber: 2, Hunk: 0},
		{Kind: diff.Deletion, Text: "  [components, definitionsById, entityType, targetRecord],", OldNumber: 3, Hunk: 0},
	}
	for index, text := range []string{"  [", "    components,", "    componentProfiles,", "    definitionsById,", "    entityType,", "    sellerProfileId,", "    targetRecord,", "  ],"} {
		lines = append(lines, diff.Line{Kind: diff.Addition, Text: text, NewNumber: index + 3, Hunk: 0})
	}
	lines = append(lines, diff.Line{Kind: diff.Context, Text: ");", OldNumber: 4, NewNumber: 11, Hunk: 0})

	rows := buildNormalizedSplitRows(lines, plan.Layout, oldSource, newSource)
	assertNormalizedPair(t, rows, "components,", "components,", diff.Context, diff.Context)
	assertNormalizedPair(t, rows, "", "componentProfiles,", diff.Context, diff.Addition)
	assertNormalizedPair(t, rows, "", "sellerProfileId,", diff.Context, diff.Addition)
}

func TestNormalizedSplitNeverDropsChangedRowsFromIncompleteLayoutBlock(t *testing.T) {
	oldSource := []byte("before\nold value\n")
	newSource := []byte("before\nnew value\n")
	lines := []diff.Line{
		{Kind: diff.Meta, Text: "@@ -2 +2 @@", Hunk: 0},
		{Kind: diff.Deletion, Text: "old value", OldNumber: 2, Hunk: 0},
		{Kind: diff.Addition, Text: "new value", NewNumber: 2, Hunk: 0},
	}
	layout := &semantic.Layout{Blocks: []semantic.LayoutBlock{{
		Old: semantic.Range{Start: 7, End: 16}, New: semantic.Range{Start: 7, End: 16},
		Role: "lexical_declaration", Confidence: 100,
		Rows: []semantic.LayoutRow{{
			Old: &semantic.VirtualLine{Text: "unrelated old", Start: 0, End: 6},
			New: &semantic.VirtualLine{Text: "unrelated new", Start: 0, End: 6},
		}},
	}}}

	rows := buildNormalizedSplitRows(lines, layout, oldSource, newSource)
	assertNormalizedPair(t, rows, "old value", "new value", diff.Deletion, diff.Addition)
	if rows[1].oldIndex != 1 || rows[1].newIndex != 2 {
		t.Fatalf("raw fallback lost source indices: %#v", rows[1])
	}
}

func TestNormalizedSplitAlignsUnchangedJSXChildInsideInsertedWrapper(t *testing.T) {
	oldSource := []byte("function Widget() {\n  return isTyping ? (\n    <TypewriterText value={displayText} />\n  ) : null;\n}\n")
	newSource := []byte("function Widget() {\n  return isTyping ? (\n    <div className=\"wrapper\">\n      <TypewriterText value={displayText} />\n    </div>\n  ) : null;\n}\n")
	plan, err := semantic.New(0).Analyze(context.Background(), semantic.Input{Path: "widget.tsx", Old: oldSource, New: newSource})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Layout == nil {
		t.Skip("normalized AST layout unavailable without cgo")
	}
	lines := []diff.Line{
		{Kind: diff.Meta, Text: "@@ -1,5 +1,7 @@", Hunk: 0},
		{Kind: diff.Context, Text: "function Widget() {", OldNumber: 1, NewNumber: 1, Hunk: 0},
		{Kind: diff.Context, Text: "  return isTyping ? (", OldNumber: 2, NewNumber: 2, Hunk: 0},
		{Kind: diff.Deletion, Text: "    <TypewriterText value={displayText} />", OldNumber: 3, Hunk: 0},
		{Kind: diff.Addition, Text: "    <div className=\"wrapper\">", NewNumber: 3, Hunk: 0},
		{Kind: diff.Addition, Text: "      <TypewriterText value={displayText} />", NewNumber: 4, Hunk: 0},
		{Kind: diff.Addition, Text: "    </div>", NewNumber: 5, Hunk: 0},
		{Kind: diff.Context, Text: "  ) : null;", OldNumber: 4, NewNumber: 6, Hunk: 0},
		{Kind: diff.Context, Text: "}", OldNumber: 5, NewNumber: 7, Hunk: 0},
	}

	rows := buildNormalizedSplitRows(lines, plan.Layout, oldSource, newSource)
	assertNormalizedPair(t, rows, "<TypewriterText value={displayText} />", "<TypewriterText value={displayText} />", diff.Context, diff.Context)
}

func TestNormalizedSplitRealignsNestedJSXAcrossMisleadingGitContext(t *testing.T) {
	oldSource := []byte("function Pending() {\n  return (\n    <section>\n      <div className=\"frame\">\n        <div className=\"flex items-start gap-2\">\n          <Loader2\n            className=\"spinner\"\n            aria-hidden=\"true\"\n          />\n          <div className=\"space-y-1\">\n            <p>Working</p>\n          </div>\n        </div>\n      </div>\n    </section>\n  );\n}\n")
	newSource := []byte("function Pending() {\n  return (\n    <section>\n      <div className=\"frame\">\n        {progress ? (\n          <Progress\n            progress={progress}\n          />\n        ) : (\n          <div className=\"flex items-start gap-2\">\n            <Loader2\n              className=\"spinner\"\n              aria-hidden=\"true\"\n            />\n            <div className=\"space-y-1\">\n              <p>Working</p>\n            </div>\n          </div>\n        )}\n      </div>\n    </section>\n  );\n}\n")
	plan, err := semantic.New(0).Analyze(context.Background(), semantic.Input{Path: "pending.tsx", Old: oldSource, New: newSource})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Layout == nil {
		t.Skip("normalized AST layout unavailable without cgo")
	}
	lines := []diff.Line{
		{Kind: diff.Meta, Text: "@@ -1,17 +1,23 @@", Hunk: 0},
		{Kind: diff.Context, Text: "function Pending() {", OldNumber: 1, NewNumber: 1, Hunk: 0},
		{Kind: diff.Context, Text: "  return (", OldNumber: 2, NewNumber: 2, Hunk: 0},
		{Kind: diff.Context, Text: "    <section>", OldNumber: 3, NewNumber: 3, Hunk: 0},
		{Kind: diff.Context, Text: "      <div className=\"frame\">", OldNumber: 4, NewNumber: 4, Hunk: 0},
	}
	for number, text := range []string{
		"        <div className=\"flex items-start gap-2\">",
		"          <Loader2",
		"            className=\"spinner\"",
		"            aria-hidden=\"true\"",
	} {
		lines = append(lines, diff.Line{Kind: diff.Deletion, Text: text, OldNumber: number + 5, Hunk: 0})
	}
	for number, text := range []string{
		"        {progress ? (",
		"          <Progress",
		"            progress={progress}",
	} {
		lines = append(lines, diff.Line{Kind: diff.Addition, Text: text, NewNumber: number + 5, Hunk: 0})
	}
	lines = append(lines, diff.Line{Kind: diff.Context, Text: "          />", OldNumber: 9, NewNumber: 8, Hunk: 0})
	for number, text := range []string{
		"          <div className=\"space-y-1\">",
		"            <p>Working</p>",
		"          </div>",
	} {
		lines = append(lines, diff.Line{Kind: diff.Deletion, Text: text, OldNumber: number + 10, Hunk: 0})
	}
	for number, text := range []string{
		"        ) : (",
		"          <div className=\"flex items-start gap-2\">",
		"            <Loader2",
		"              className=\"spinner\"",
		"              aria-hidden=\"true\"",
		"            />",
		"            <div className=\"space-y-1\">",
		"              <p>Working</p>",
		"            </div>",
	} {
		lines = append(lines, diff.Line{Kind: diff.Addition, Text: text, NewNumber: number + 9, Hunk: 0})
	}
	lines = append(lines,
		diff.Line{Kind: diff.Context, Text: "        </div>", OldNumber: 13, NewNumber: 18, Hunk: 0},
		diff.Line{Kind: diff.Addition, Text: "        )}", NewNumber: 19, Hunk: 0},
		diff.Line{Kind: diff.Context, Text: "      </div>", OldNumber: 14, NewNumber: 20, Hunk: 0},
		diff.Line{Kind: diff.Context, Text: "    </section>", OldNumber: 15, NewNumber: 21, Hunk: 0},
		diff.Line{Kind: diff.Context, Text: "  );", OldNumber: 16, NewNumber: 22, Hunk: 0},
		diff.Line{Kind: diff.Context, Text: "}", OldNumber: 17, NewNumber: 23, Hunk: 0},
	)

	rows := buildNormalizedSplitRows(lines, plan.Layout, oldSource, newSource)
	assertNormalizedPair(t, rows, `<div className="flex items-start gap-2">`, `<div className="flex items-start gap-2">`, diff.Context, diff.Context)
	assertNormalizedPair(t, rows, "<Loader2", "<Loader2", diff.Context, diff.Context)
	assertNormalizedPair(t, rows, `<div className="space-y-1">`, `<div className="space-y-1">`, diff.Context, diff.Context)
	assertNormalizedPair(t, rows, "", "<Progress", diff.Context, diff.Addition)
}

func TestNormalizedMouseCopyReturnsLiteralSourceLine(t *testing.T) {
	oldSource := []byte("const { items, isLoading, rowLimit } = useNextMoves({ focus: widgetFocus });\n")
	newSource := []byte("const {\n  items,\n  isLoading,\n  rowLimit,\n  settings,\n} = useNextMoves();\n")
	plan, err := semantic.New(0).Analyze(context.Background(), semantic.Input{Path: "widget.ts", Old: oldSource, New: newSource})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Layout == nil {
		t.Skip("normalized AST layout unavailable without cgo")
	}
	lines := []diff.Line{{Kind: diff.Meta, Text: "@@ -1 +1,6 @@", Hunk: 0}}
	lines = append(lines, diff.Line{Kind: diff.Deletion, Text: strings.TrimSuffix(string(oldSource), "\n"), OldNumber: 1, Hunk: 0})
	for index, text := range strings.Split(strings.TrimSuffix(string(newSource), "\n"), "\n") {
		lines = append(lines, diff.Line{Kind: diff.Addition, Text: text, NewNumber: index + 1, Hunk: 0})
	}
	repo := &gitrepo.Repository{Root: t.TempDir(), Files: []diff.File{{Path: "widget.ts", Lines: lines}}}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height, m.view, m.normalizedLayout, m.semanticReflow = 120, 30, split, true, true
	m.semantic.repo, m.semantic.file, m.semantic.ready = repo, 0, true
	m.semantic.layout, m.semantic.oldSource, m.semantic.newSource = plan.Layout, oldSource, newSource
	m.selectedText = "items,"
	m.mouseSelectStart = mousePoint{x: m.filePaneWidth() + 2, y: 7}
	m.mouseSelectEnd = m.mouseSelectStart

	text, count, _, base := m.clipboardSelection()
	if text != strings.TrimSuffix(string(oldSource), "\n") || count != 1 || base.start != 1 || base.end != 1 {
		t.Fatalf("normalized copy = %q count=%d base=%#v", text, count, base)
	}
}

func assertNormalizedPair(t *testing.T, rows []splitRow, oldText, newText string, oldKind, newKind diff.LineKind) {
	t.Helper()
	for _, row := range rows {
		gotOld, gotNew := "", ""
		if row.old != nil {
			gotOld = strings.TrimSpace(row.old.Text)
		}
		if row.new != nil {
			gotNew = strings.TrimSpace(row.new.Text)
		}
		if gotOld != oldText || gotNew != newText {
			continue
		}
		if row.old != nil && row.old.Kind != oldKind {
			t.Fatalf("old kind for %q = %v, want %v", oldText, row.old.Kind, oldKind)
		}
		if row.new != nil && row.new.Kind != newKind {
			t.Fatalf("new kind for %q = %v, want %v", newText, row.new.Kind, newKind)
		}
		return
	}
	t.Fatalf("normalized pair not found: %q | %q\n%v", oldText, newText, normalizedRowsText(rows))
}

func normalizedRowsText(rows []splitRow) string {
	var result []string
	for _, row := range rows {
		oldText, newText := "", ""
		if row.old != nil {
			oldText = strings.TrimSpace(row.old.Text)
		}
		if row.new != nil {
			newText = strings.TrimSpace(row.new.Text)
		}
		result = append(result, oldText+" | "+newText)
	}
	return strings.Join(result, "\n")
}
