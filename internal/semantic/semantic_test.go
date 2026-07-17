package semantic

import (
	"container/list"
	"context"
	"strings"
	"testing"
)

type countingAdapter struct {
	count int
	plan  Plan
}

func (*countingAdapter) supports(string) bool { return true }
func (a *countingAdapter) analyze(context.Context, Input) (Plan, error) {
	a.count++
	return a.plan, nil
}

func TestTokenAnalyzerIgnoresFormatterReflow(t *testing.T) {
	oldSource := []byte("const effectiveLimit = isFullpage\n  ? fullpageLimit\n  : widgetSettings.widgetLimit;\n")
	newSource := []byte("const effectiveLimit = isFullpage ? fullpageLimit : settings.config.limit;\n")
	plan, err := tokenAdapter{}.analyze(context.Background(), Input{Path: "limits.apex", Old: oldSource, New: newSource})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Engine != EngineToken || selected(oldSource, plan.Old) != "widgetSettings.widgetLimit" || selected(newSource, plan.New) != "settings.config.limit" {
		t.Fatalf("unexpected token plan: %#v old=%q new=%q", plan, selected(oldSource, plan.Old), selected(newSource, plan.New))
	}
}

func TestTokenAnalyzerPreservesMeaningfulStringWhitespace(t *testing.T) {
	oldSource := []byte("message = \"hello world\"\n")
	newSource := []byte("message = \"helloworld\"\n")
	plan, err := tokenAdapter{}.analyze(context.Background(), Input{Path: "message.py", Old: oldSource, New: newSource})
	if err != nil {
		t.Fatal(err)
	}
	if selected(oldSource, plan.Old) != "\"hello world\"" || selected(newSource, plan.New) != "\"helloworld\"" {
		t.Fatalf("string change was hidden: old=%q new=%q", selected(oldSource, plan.Old), selected(newSource, plan.New))
	}
}

func TestTokenAnalyzerTreatsBlockCommentReflowAsFormatting(t *testing.T) {
	oldSource := []byte("/** record facts only */\nvalue = 1\n")
	newSource := []byte("/**\n * record facts only\n */\nvalue = 2\n")
	plan, err := tokenAdapter{}.analyze(context.Background(), Input{Path: "value.go", Old: oldSource, New: newSource})
	if err != nil {
		t.Fatal(err)
	}
	if selected(oldSource, plan.Old) != "1" || selected(newSource, plan.New) != "2" {
		t.Fatalf("comment reflow was treated as meaningful: old=%q new=%q", selected(oldSource, plan.Old), selected(newSource, plan.New))
	}
}

func TestTreeSitterAnalyzerFindsOnlyMeaningfulTypeScriptChange(t *testing.T) {
	adapter := newTreeSitterAdapter()
	if !adapter.supports("limits.ts") {
		t.Skip("tree-sitter unavailable without cgo")
	}
	oldSource := []byte("const effectiveLimit = isFullpage\n  ? fullpageLimit\n  : widgetSettings.widgetLimit;\n")
	newSource := []byte("const effectiveLimit = isFullpage ? fullpageLimit : settings.config.limit;\n")
	plan, err := adapter.analyze(context.Background(), Input{Path: "limits.ts", Old: oldSource, New: newSource})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Engine != EngineAST || selected(oldSource, plan.Old) != "widgetSettings.widgetLimit" || selected(newSource, plan.New) != "settings.config.limit" {
		t.Fatalf("unexpected AST plan: %#v old=%q new=%q", plan, selected(oldSource, plan.Old), selected(newSource, plan.New))
	}
}

func TestTreeSitterAnalyzerTreatsCommentReflowAsFormatting(t *testing.T) {
	adapter := newTreeSitterAdapter()
	if !adapter.supports("comments.ts") {
		t.Skip("tree-sitter unavailable without cgo")
	}
	oldSource := []byte("/** record facts only */\nexport const value = 1;\n")
	newSource := []byte("/**\n * record facts only\n */\nexport const value = 2;\n")
	plan, err := adapter.analyze(context.Background(), Input{Path: "comments.ts", Old: oldSource, New: newSource})
	if err != nil {
		t.Fatal(err)
	}
	if got := selected(oldSource, plan.Old); got != "1" {
		t.Fatalf("old semantic change = %q, want only value", got)
	}
	if got := selected(newSource, plan.New); got != "2" {
		t.Fatalf("new semantic change = %q, want only value", got)
	}
}

func TestAnalyzerFallsBackWhenTypeScriptIsTemporarilyInvalid(t *testing.T) {
	if !newTreeSitterAdapter().supports("editing.ts") {
		t.Skip("tree-sitter unavailable without cgo")
	}
	analyzer := New(0)
	plan, err := analyzer.Analyze(context.Background(), Input{
		Path: "editing.ts", Old: []byte("const value = 1;\n"), New: []byte("const value = ;\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Engine != EngineToken || plan.Warning == "" {
		t.Fatalf("fallback plan = %#v", plan)
	}
}

func TestAnalyzerFallsBackForUnsupportedLanguage(t *testing.T) {
	plan, err := New(4).Analyze(context.Background(), Input{Path: "service.cls", Old: []byte("String a;"), New: []byte("String b;")})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Engine != EngineToken {
		t.Fatalf("engine = %q, want token", plan.Engine)
	}
}

func TestAnalyzerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New(1).Analyze(ctx, Input{Path: "x.ts", Old: []byte("const x = 1"), New: []byte("const x = 2")}); err == nil {
		t.Fatal("cancelled analysis succeeded")
	}
}

func TestAnalyzerCachesPlansByContentAndEvictsLeastRecentlyUsed(t *testing.T) {
	adapter := &countingAdapter{plan: Plan{Engine: EngineToken}}
	analyzer := &analyzer{
		ast: adapter, token: adapter, capacity: 1,
		cache: make(map[string]*list.Element), lru: list.New(),
	}
	first := Input{Path: "value.ts", Old: []byte("one"), New: []byte("two")}
	second := Input{Path: "value.ts", Old: []byte("two"), New: []byte("three")}
	for _, input := range []Input{first, first, second, first} {
		if _, err := analyzer.Analyze(context.Background(), input); err != nil {
			t.Fatal(err)
		}
	}
	if adapter.count != 3 {
		t.Fatalf("adapter calls = %d, want one cache hit and two misses after eviction", adapter.count)
	}
}

func selected(source []byte, ranges []Range) string {
	var result []string
	for _, current := range ranges {
		result = append(result, string(source[current.Start:current.End]))
	}
	return strings.Join(result, "")
}

func BenchmarkSemanticTypeScriptReflow(b *testing.B) {
	analyzer := New(0)
	oldSource := []byte(strings.Repeat("const value = source.item;\n", 100))
	newSource := append([]byte(nil), oldSource...)
	newSource = append(newSource, []byte("const next = source.other;\n")...)
	input := Input{Path: "fixture.ts", Old: oldSource, New: newSource}
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := analyzer.Analyze(context.Background(), input); err != nil {
			b.Fatal(err)
		}
	}
}
