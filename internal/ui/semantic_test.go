package ui

import (
	"context"
	"errors"
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
	m.semantic.analyzer = stubSemanticAnalyzer{plan: semantic.Plan{
		Engine: semantic.EngineAST,
		Old:    []semantic.Range{{Start: oldChanged, End: oldChanged + len("widgetSettings.widgetLimit")}},
		New:    []semantic.Range{{Start: newChanged, End: newChanged + len("settings.config.limit")}},
	}}
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
	if got := selectedSpanText(lines[2].Text, spans[2]); got != "widgetSettings.widgetLimit" {
		t.Fatalf("old projected span = %q", got)
	}
	if got := selectedSpanText(lines[3].Text, spans[3]); got != "settings.config.limit" {
		t.Fatalf("new projected span = %q", got)
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
