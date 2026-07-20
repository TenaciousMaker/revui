package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/TenaciousMaker/revui/internal/diff"
	"github.com/TenaciousMaker/revui/internal/gitrepo"
	"github.com/TenaciousMaker/revui/internal/semantic"
)

type stubSemanticAnalyzer struct {
	plan semantic.Plan
	err  error
}

type cancellationSemanticAnalyzer struct {
	started chan struct{}
}

func (s cancellationSemanticAnalyzer) Analyze(ctx context.Context, _ semantic.Input) (semantic.Plan, error) {
	close(s.started)
	<-ctx.Done()
	return semantic.Plan{}, ctx.Err()
}

func (s stubSemanticAnalyzer) Analyze(context.Context, semantic.Input) (semantic.Plan, error) {
	return s.plan, s.err
}

type stubSemanticRepository struct {
	oldSource []byte
	newSource []byte
}

func (s stubSemanticRepository) Refresh(context.Context, string, string) (*gitrepo.Repository, error) {
	return nil, errors.New("unexpected refresh")
}
func (s stubSemanticRepository) Search(context.Context, *gitrepo.Repository, string, int) ([]gitrepo.SearchMatch, error) {
	return nil, errors.New("unexpected search")
}
func (s stubSemanticRepository) ReadSource(context.Context, *gitrepo.Repository, string) ([]byte, bool, error) {
	return nil, false, errors.New("unexpected source read")
}
func (s stubSemanticRepository) ReadPair(context.Context, *gitrepo.Repository, diff.File) ([]byte, []byte, error) {
	return s.oldSource, s.newSource, nil
}

func TestSemanticAnalysisRunsOffRenderPathAndProjectsRanges(t *testing.T) {
	oldSource := []byte("const effectiveLimit = isFullpage\n  ? fullpageLimit\n  : widgetSettings.widgetLimit;\n")
	newSource := []byte("const effectiveLimit = isFullpage ? fullpageLimit : settings.config.limit;\n")
	oldChanged := strings.Index(string(oldSource), "widgetSettings.widgetLimit")
	newChanged := strings.Index(string(newSource), "settings.config.limit")
	lines := []diff.Line{
		{Kind: diff.Deletion, Hunk: 0, Text: "const effectiveLimit = isFullpage", OldNumber: 1},
		{Kind: diff.Deletion, Hunk: 0, Text: "  ? fullpageLimit", OldNumber: 2},
		{Kind: diff.Deletion, Hunk: 0, Text: "  : widgetSettings.widgetLimit;", OldNumber: 3},
		{Kind: diff.Addition, Hunk: 0, Text: "const effectiveLimit = isFullpage ? fullpageLimit : settings.config.limit;", NewNumber: 1},
	}
	repo := &gitrepo.Repository{Root: t.TempDir(), Branch: "feature", Base: "main", Files: []diff.File{{Path: "limits.ts", Lines: lines}}}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.semanticReflow = true
	m.repositories = stubSemanticRepository{oldSource: oldSource, newSource: newSource}
	plan := semantic.Plan{
		Engine: semantic.EngineAST,
		Old:    []semantic.Range{{Start: oldChanged, End: oldChanged + len("widgetSettings.widgetLimit")}},
		New:    []semantic.Range{{Start: newChanged, End: newChanged + len("settings.config.limit")}},
	}
	m.semantic.analyzer = stubSemanticAnalyzer{plan: plan}
	command := m.ensureSemanticAnalysis()
	if command == nil || !m.semantic.loading || m.semantic.ready {
		t.Fatalf("analysis was not scheduled: %#v", m.semantic)
	}
	updated, _ := m.Update(command())
	m = updated.(Model)
	if !m.semantic.ready || m.semantic.engine != semantic.EngineAST {
		t.Fatalf("analysis result not applied: %#v", m.semantic)
	}
	spans := m.currentIntralineSpans()
	if got := selectedSpanText(lines[2].Text, spans[2]); got != "" {
		t.Fatalf("dense old-line emphasis was not suppressed: %q", got)
	}
	if got := selectedSpanText(lines[3].Text, spans[3]); got != "settings.config.limit" {
		t.Fatalf("new projected span = %q", got)
	}
	raw := projectSemanticPlan(lines, plan, oldSource, newSource)
	if got := selectedSpanText(lines[2].Text, raw[2]); got != "widgetSettings.widgetLimit" {
		t.Fatalf("raw old projection = %q", got)
	}
}

func TestDifftasticToggleSelectsExternalAnalyzerAndForcesSplit(t *testing.T) {
	oldSource, newSource := []byte("old\n"), []byte("new\n")
	lines := []diff.Line{
		{Kind: diff.Meta, Hunk: 0, Text: "@@ -1 +1 @@"},
		{Kind: diff.Deletion, Hunk: 0, OldNumber: 1, Text: "old"},
		{Kind: diff.Addition, Hunk: 0, NewNumber: 1, Text: "new"},
	}
	repo := &gitrepo.Repository{Root: t.TempDir(), Files: []diff.File{{Path: "value.ts", Lines: lines}}}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.repositories = stubSemanticRepository{oldSource: oldSource, newSource: newSource}
	m.semantic.analyzer = stubSemanticAnalyzer{err: errors.New("built-in analyzer should not run")}
	m.semantic.difftasticAnalyzer = stubSemanticAnalyzer{plan: semantic.Plan{
		Engine: semantic.EngineDifftastic,
		Old:    []semantic.Range{{Start: 0, End: 3}}, New: []semantic.Range{{Start: 0, End: 3}},
		Alignment: []semantic.LineAlignment{{Old: 1, New: 1}},
	}}

	updated, _ := m.handleKey(tea.KeyPressMsg{Text: "d", Code: 'd'})
	m = updated.(Model)
	if !m.difftasticMode || !m.semanticReflow || m.normalizedLayout || m.view != split {
		t.Fatalf("Difftastic toggle state: difft=%v semantic=%v normalized=%v view=%v", m.difftasticMode, m.semanticReflow, m.normalizedLayout, m.view)
	}
	command := m.ensureSemanticAnalysis()
	if command == nil || m.semantic.provider != difftasticSemantic {
		t.Fatalf("Difftastic analysis not scheduled: %#v", m.semantic)
	}
	updated, repaint := m.Update(command())
	m = updated.(Model)
	if !m.semantic.ready || m.semantic.engine != semantic.EngineDifftastic || len(m.semantic.alignment) != 1 {
		t.Fatalf("Difftastic result not applied: %#v", m.semantic)
	}
	if !strings.Contains(m.status, "Difftastic alignment ready") {
		t.Fatalf("Difftastic status = %q", m.status)
	}
	if repaint == nil || fmt.Sprintf("%T", repaint()) != "tea.clearScreenMsg" {
		t.Fatalf("Difftastic frame replacement did not request a full repaint: %T", repaint)
	}

	updated, _ = m.handleKey(tea.KeyPressMsg{Text: "d", Code: 'd'})
	m = updated.(Model)
	if m.difftasticMode || m.semanticReflow || m.view != split {
		t.Fatalf("Difftastic did not return to raw split: difft=%v semantic=%v view=%v", m.difftasticMode, m.semanticReflow, m.view)
	}
}

func TestDifftasticFailureFallsBackVisibly(t *testing.T) {
	repo := &gitrepo.Repository{Files: []diff.File{{Path: "value.ts", Lines: []diff.Line{{Kind: diff.Addition, NewNumber: 1, Text: "new"}}}}}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.difftasticMode, m.semanticReflow = true, true
	m.semantic.id, m.semantic.repo, m.semantic.file, m.semantic.provider = 1, repo, 0, difftasticSemantic
	m.applySemanticResult(semanticResultMsg{id: 1, repo: repo, file: 0, provider: difftasticSemantic, err: errors.New("difft not found")})
	if m.semantic.ready || !strings.Contains(m.status, "Showing raw split diff") || m.semanticLabel() != "DIFFT!" {
		t.Fatalf("failure was not visible and safe: state=%#v status=%q label=%q", m.semantic, m.status, m.semanticLabel())
	}
}

func TestSemanticHighlightRefinesChangedStringLiteralTokens(t *testing.T) {
	oldSource := []byte("className=\"whitespace-pre-wrap font-sans text-sm leading-relaxed text-color-default\"\n")
	newSource := []byte("className=\"max-w-full min-w-0 whitespace-pre-wrap break-words font-sans text-sm leading-relaxed text-color-default\"\n")
	plan, err := semantic.New(0).Analyze(context.Background(), semantic.Input{Path: "widget.tsx", Old: oldSource, New: newSource})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Engine != semantic.EngineAST {
		t.Skip("AST semantic analysis unavailable without cgo")
	}
	lines := []diff.Line{
		{Kind: diff.Deletion, Hunk: 0, Text: strings.TrimSuffix(string(oldSource), "\n"), OldNumber: 1},
		{Kind: diff.Addition, Hunk: 0, Text: strings.TrimSuffix(string(newSource), "\n"), NewNumber: 1},
	}
	repo := &gitrepo.Repository{Files: []diff.File{{Path: "widget.tsx", Lines: lines}}}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.semanticReflow = true
	m.semantic.id, m.semantic.repo, m.semantic.file = 1, repo, 0
	m.applySemanticResult(semanticResultMsg{id: 1, repo: repo, file: 0, oldSource: oldSource, newSource: newSource, plan: plan})

	selected := selectedSpanText(lines[1].Text, m.semantic.spans[1])
	for _, added := range []string{"max", "min", "break"} {
		if !strings.Contains(selected, added) {
			t.Fatalf("added class %q was not emphasized: %q", added, selected)
		}
	}
	for _, unchanged := range []string{"whitespace-pre-wrap", "font-sans", "text-color-default"} {
		if strings.Contains(selected, unchanged) {
			t.Fatalf("unchanged class %q was emphasized: %q", unchanged, selected)
		}
	}
}

func TestStaleSemanticResultIsIgnored(t *testing.T) {
	repo := &gitrepo.Repository{Root: t.TempDir(), Files: []diff.File{{Path: "a.ts"}, {Path: "b.ts"}}}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.semanticReflow = true
	m.semantic.id = 2
	m.semantic.repo = repo
	m.semantic.file = 1
	m.applySemanticResult(semanticResultMsg{id: 1, repo: repo, file: 0, plan: semantic.Plan{Engine: semantic.EngineAST}})
	if m.semantic.ready || m.semantic.file != 1 {
		t.Fatalf("stale result changed state: %#v", m.semantic)
	}
}

func TestSemanticResultPreservesUnifiedCursorPosition(t *testing.T) {
	lines := []diff.Line{
		{Kind: diff.Meta, Text: "@@ -1,6 +1,6 @@", Hunk: 0},
		{Kind: diff.Context, Text: "one", OldNumber: 1, NewNumber: 1, Hunk: 0},
		{Kind: diff.Context, Text: "two", OldNumber: 2, NewNumber: 2, Hunk: 0},
		{Kind: diff.Deletion, Text: "old", OldNumber: 3, Hunk: 0},
		{Kind: diff.Addition, Text: "new", NewNumber: 3, Hunk: 0},
		{Kind: diff.Context, Text: "four", OldNumber: 4, NewNumber: 4, Hunk: 0},
		{Kind: diff.Context, Text: "five", OldNumber: 5, NewNumber: 5, Hunk: 0},
		{Kind: diff.Context, Text: "six", OldNumber: 6, NewNumber: 6, Hunk: 0},
	}
	repo := &gitrepo.Repository{Files: []diff.File{{Path: "a.ts", Lines: lines}}}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.focus, m.view, m.height = focusDiff, unified, 10
	m.line, m.lineScroll = 4, 2
	m.semanticReflow = true
	m.semantic.id, m.semantic.repo, m.semantic.file, m.semantic.loading = 1, repo, 0, true
	oldSource := []byte("one\ntwo\nold\nfour\nfive\nsix\n")
	newSource := []byte("one\ntwo\nnew\nfour\nfive\nsix\n")
	m.applySemanticResult(semanticResultMsg{
		id: 1, repo: repo, file: 0, oldSource: oldSource, newSource: newSource,
		plan: semantic.Plan{Engine: semantic.EngineToken},
	})
	if m.line != 4 || m.lineScroll != 2 {
		t.Fatalf("semantic result moved unified position to line=%d scroll=%d, want 4/2", m.line, m.lineScroll)
	}
}

func TestSemanticResultAfterToggleOffIsIgnored(t *testing.T) {
	repo := &gitrepo.Repository{Files: []diff.File{{Path: "a.ts"}}}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.semanticReflow = true
	m.semantic.id, m.semantic.repo, m.semantic.file = 1, repo, 0
	m.semanticReflow = false
	m.cancelSemanticAnalysis()
	m.applySemanticResult(semanticResultMsg{id: 1, repo: repo, file: 0, plan: semantic.Plan{Engine: semantic.EngineAST}})
	if m.semantic.ready || m.semantic.repo != nil {
		t.Fatalf("disabled semantic state accepted late result: %#v", m.semantic)
	}
}

func TestSupersededSemanticAnalysisIsCancelled(t *testing.T) {
	repo := &gitrepo.Repository{Files: []diff.File{
		{Path: "first.ts", Lines: []diff.Line{{Kind: diff.Addition, NewNumber: 1, Text: "first"}}},
		{Path: "second.ts", Lines: []diff.Line{{Kind: diff.Addition, NewNumber: 1, Text: "second"}}},
	}}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.semanticReflow = true
	m.repositories = stubSemanticRepository{newSource: []byte("first\n")}
	started := make(chan struct{})
	m.semantic.analyzer = cancellationSemanticAnalyzer{started: started}

	first := m.ensureSemanticAnalysis()
	result := make(chan tea.Msg, 1)
	go func() { result <- first() }()
	<-started
	m.file = 1
	if second := m.ensureSemanticAnalysis(); second == nil {
		t.Fatal("replacement analysis was not scheduled")
	}
	message := (<-result).(semanticResultMsg)
	if !errors.Is(message.err, context.Canceled) {
		t.Fatalf("superseded analysis error = %v, want context cancellation", message.err)
	}
}

func TestProjectSemanticPlanHandlesTabsAndUnicode(t *testing.T) {
	source := []byte("\tconst café = before;\n")
	start := strings.Index(string(source), "before")
	lines := []diff.Line{{Kind: diff.Addition, Text: "\tconst café = before;", NewNumber: 1}}
	spans := projectSemanticPlan(lines, semantic.Plan{New: []semantic.Range{{Start: start, End: start + len("before")}}}, nil, source)
	expanded := expandTabs(lines[0].Text)
	if got := selectedSpanText(expanded, spans[0]); got != "before" {
		t.Fatalf("projected tab/unicode span = %q (%#v)", got, spans[0])
	}
}

func TestSemanticLabelReportsLifecycleAndEngine(t *testing.T) {
	repo := &gitrepo.Repository{Files: []diff.File{{Path: "value.ts"}}}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.semanticLabel(); got != "" {
		t.Fatalf("disabled label = %q", got)
	}
	m.semanticReflow = true
	if got := m.semanticLabel(); got != "SEM…" {
		t.Fatalf("pending label = %q", got)
	}
	m.semantic.repo, m.semantic.file, m.semantic.ready, m.semantic.engine = repo, 0, true, semantic.EngineAST
	if got := m.semanticLabel(); got != "AST" {
		t.Fatalf("ready label = %q", got)
	}
	m.semantic.ready, m.semantic.warning = false, "source unavailable"
	if got := m.semanticLabel(); got != "SEM!" {
		t.Fatalf("failed label = %q", got)
	}
}

func TestSemanticPresentationDoesNotEmphasizePureInsertionTokens(t *testing.T) {
	line := diff.Line{Kind: diff.Addition, Text: "const [pendingRemovalId, setPendingRemovalId] = useState(null);", NewNumber: 1, OriginalIndex: 0}
	spans := filterSemanticSpans([]diff.Line{line}, map[int][]textSpan{0: {{start: 0, end: len([]rune(line.Text))}}})
	if len(spans[0]) != 0 {
		t.Fatalf("pure insertion received redundant intraline emphasis: %#v", spans[0])
	}
}

func TestSemanticPresentationKeepsSparseReplacementEmphasis(t *testing.T) {
	lines := []diff.Line{
		{Kind: diff.Deletion, Text: "<Dialog open={open} onOpenChange={onOpenChange}>", OldNumber: 1, Hunk: 0},
		{Kind: diff.Addition, Text: "<Dialog open={open} onOpenChange={handleOpenChange}>", NewNumber: 1, Hunk: 0},
	}
	start := strings.Index(lines[1].Text, "handleOpenChange")
	spans := filterSemanticSpans(lines, map[int][]textSpan{1: {{start: start, end: start + len("handleOpenChange")}}})
	if len(spans[1]) != 1 {
		t.Fatalf("useful replacement emphasis was removed: %#v", spans)
	}
}

func TestSemanticPresentationShowsIdentifierRemovedDuringImportReflow(t *testing.T) {
	oldSource := []byte("import {\n  type ReactNode,\n  useEffect,\n  useLayoutEffect,\n  useMemo,\n  useRef,\n  useState,\n} from 'react';\n")
	newSource := []byte("import { type ReactNode, useEffect, useMemo, useRef, useState } from 'react';\n")
	lines := []diff.Line{
		{Kind: diff.Deletion, Hunk: 0, OldNumber: 1, Text: "import {"},
		{Kind: diff.Deletion, Hunk: 0, OldNumber: 2, Text: "  type ReactNode,"},
		{Kind: diff.Deletion, Hunk: 0, OldNumber: 3, Text: "  useEffect,"},
		{Kind: diff.Deletion, Hunk: 0, OldNumber: 4, Text: "  useLayoutEffect,"},
		{Kind: diff.Deletion, Hunk: 0, OldNumber: 5, Text: "  useMemo,"},
		{Kind: diff.Deletion, Hunk: 0, OldNumber: 6, Text: "  useRef,"},
		{Kind: diff.Deletion, Hunk: 0, OldNumber: 7, Text: "  useState,"},
		{Kind: diff.Deletion, Hunk: 0, OldNumber: 8, Text: "} from 'react';"},
		{Kind: diff.Addition, Hunk: 0, NewNumber: 1, Text: string(newSource[:len(newSource)-1])},
	}
	plan, err := semantic.New(0).Analyze(context.Background(), semantic.Input{Path: "component.tsx", Old: oldSource, New: newSource})
	if err != nil {
		t.Fatal(err)
	}
	raw := projectSemanticPlan(lines, plan, oldSource, newSource)
	if got := selectedSpanText(lines[3].Text, raw[3]); got != "useLayoutEffect," {
		t.Fatalf("removed import projection = %q, want useLayoutEffect,", got)
	}
	spans := filterSemanticSpans(lines, raw)
	if got := selectedSpanText(lines[3].Text, spans[3]); got != "useLayoutEffect," {
		t.Fatalf("removed import emphasis = %q, want useLayoutEffect, (plan: %#v)", got, plan.Correspondences)
	}
	if got := selectedSpanText(lines[8].Text, spans[8]); got != "" {
		t.Fatalf("unchanged reformatted import received noisy emphasis: %q", got)
	}
}

func TestSemanticPresentationSuppressesDenseReplacementEmphasis(t *testing.T) {
	lines := []diff.Line{
		{Kind: diff.Deletion, Text: "const oldValue = before();", OldNumber: 1, Hunk: 0},
		{Kind: diff.Addition, Text: "const entirelyDifferent = after();", NewNumber: 1, Hunk: 0},
	}
	spans := filterSemanticSpans(lines, map[int][]textSpan{1: {{start: 0, end: len([]rune(lines[1].Text))}}})
	if len(spans[1]) != 0 {
		t.Fatalf("dense replacement received noisy emphasis: %#v", spans[1])
	}
}

func TestDiffHeaderShowsSemanticEngine(t *testing.T) {
	repo := &gitrepo.Repository{Files: []diff.File{{
		Path: "value.ts", Lines: []diff.Line{{Kind: diff.Addition, NewNumber: 1, Text: "const value = 2;"}},
	}}}
	m, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	m.semanticReflow = true
	m.semantic.repo, m.semantic.file, m.semantic.ready, m.semantic.engine = repo, 0, true, semantic.EngineAST
	if rendered := ansi.Strip(m.renderDiff(100, 8)); !strings.Contains(rendered, "UNIFIED  AST") {
		t.Fatalf("diff header does not expose semantic engine:\n%s", rendered)
	}
}
