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

func TestTreeSitterPlanPreservesSourceCorrespondences(t *testing.T) {
	adapter := newTreeSitterAdapter()
	if !adapter.supports("settings.ts") {
		t.Skip("tree-sitter unavailable without cgo")
	}
	oldSource := []byte("const settings = { limit: oldLimit, enabled: true };\n")
	newSource := []byte("const settings = {\n  limit: newLimit,\n  enabled: true,\n};\n")
	plan, err := adapter.analyze(context.Background(), Input{Path: "settings.ts", Old: oldSource, New: newSource})
	if err != nil {
		t.Fatal(err)
	}
	if got := selected(oldSource, plan.ChangedRanges(OldSide)); got != "oldLimit" {
		t.Fatalf("old changed source = %q", got)
	}
	if got := selected(newSource, plan.ChangedRanges(NewSide)); got != "newLimit" {
		t.Fatalf("new changed source = %q", got)
	}
	foundPair := false
	for _, pair := range plan.Correspondences {
		if pair.Kind == Replaced && string(oldSource[pair.Old.Start:pair.Old.End]) == "oldLimit" &&
			string(newSource[pair.New.Start:pair.New.End]) == "newLimit" {
			foundPair = pair.Role == "identifier" && pair.Confidence > 0
		}
	}
	if !foundPair {
		t.Fatalf("plan has no source-preserving replacement: %#v", plan.Correspondences)
	}
}

func TestTreeSitterPlanIgnoresFormattingWithoutSynthesizingSource(t *testing.T) {
	adapter := newTreeSitterAdapter()
	if !adapter.supports("format.ts") {
		t.Skip("tree-sitter unavailable without cgo")
	}
	oldSource := []byte("call({ first: 1, second: 2 });\n")
	newSource := []byte("call({\n  first: 1,\n  second: 2,\n});\n")
	plan, err := adapter.analyze(context.Background(), Input{Path: "format.ts", Old: oldSource, New: newSource})
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.ChangedRanges(OldSide); len(got) != 0 {
		t.Fatalf("format-only old ranges = %#v", got)
	}
	if got := plan.ChangedRanges(NewSide); len(got) != 0 {
		t.Fatalf("format-only new ranges = %#v", got)
	}
	if len(plan.Correspondences) == 0 || plan.Correspondences[0].Kind != Unchanged {
		t.Fatalf("missing unchanged structural anchor: %#v", plan.Correspondences)
	}
}

func TestSemanticReorderingRemainsAChange(t *testing.T) {
	oldSource := []byte("one two three\n")
	newSource := []byte("three one two\n")
	plan, err := tokenAdapter{}.analyze(context.Background(), Input{Path: "order.txt", Old: oldSource, New: newSource})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.ChangedRanges(OldSide)) == 0 || len(plan.ChangedRanges(NewSide)) == 0 {
		t.Fatalf("reordering was incorrectly treated as semantic equality: %#v", plan)
	}
}

func TestTreeSitterLayoutStacksObjectPatternsAndProperties(t *testing.T) {
	adapter := newTreeSitterAdapter()
	if !adapter.supports("layout.ts") {
		t.Skip("tree-sitter unavailable without cgo")
	}
	source := []byte("const { items, isLoading, rowLimit } = useNextMoves({ focus: widgetFocus, sortBy: value });\n")
	plan, err := adapter.analyze(context.Background(), Input{Path: "layout.ts", Old: source, New: source})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Layout == nil {
		t.Fatal("AST plan has no normalized layout")
	}
	if len(plan.Layout.Blocks) != 1 || plan.Layout.Blocks[0].Confidence != 100 || plan.Layout.Blocks[0].Role != "lexical_declaration" {
		t.Fatalf("layout did not expose one confident declaration block: %#v", plan.Layout.Blocks)
	}
	got := layoutText(plan.Layout, NewSide)
	want := "const {\n  items,\n  isLoading,\n  rowLimit\n} = useNextMoves({\n  focus: widgetFocus,\n  sortBy: value\n});"
	if got != want {
		t.Fatalf("normalized layout:\n%s\nwant:\n%s", got, want)
	}
	for _, block := range plan.Layout.Blocks {
		for _, row := range block.Rows {
			line := row.New
			if line != nil && line.Text != "" && (line.Start < 0 || line.End <= line.Start) {
				t.Fatalf("virtual line lost source mapping: %#v", line)
			}
		}
	}
}

func TestTreeSitterLayoutBuildsCompositeForOneToManyOwners(t *testing.T) {
	adapter := newTreeSitterAdapter()
	if !adapter.supports("hooks.tsx") {
		t.Skip("tree-sitter unavailable without cgo")
	}
	oldSource := []byte("const { data: status, isLoading } = useGenerationStatus(id);\n")
	newSource := []byte("const { progress, isLoading } = useProgress(id);\nconst { data: status } = useOperationStatus(id);\n")
	plan, err := adapter.analyze(context.Background(), Input{Path: "hooks.tsx", Old: oldSource, New: newSource})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Layout == nil {
		t.Fatal("AST plan has no layout decision")
	}
	if len(plan.Layout.Blocks) != 1 || plan.Layout.Blocks[0].Confidence != 50 {
		t.Fatalf("one-to-many rewrite did not produce one conservative composite: %#v", plan.Layout.Blocks)
	}
	block := plan.Layout.Blocks[0]
	if block.Old != (Range{Start: 0, End: len(oldSource) - 1}) || block.New != (Range{Start: 0, End: len(newSource) - 1}) {
		t.Fatalf("composite ranges = old %#v new %#v", block.Old, block.New)
	}
}

func TestTreeSitterLayoutLeavesUnownedExpressionRaw(t *testing.T) {
	adapter := newTreeSitterAdapter()
	if !adapter.supports("layout.tsx") {
		t.Skip("tree-sitter unavailable without cgo")
	}
	source := []byte("getVisibleComponents([components, definitionsById, entityType, targetRecord]);\n")
	plan, err := adapter.analyze(context.Background(), Input{Path: "layout.tsx", Old: source, New: source})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Layout.Blocks) != 0 {
		t.Fatalf("unowned expression received speculative layout: %#v", plan.Layout.Blocks)
	}
}

func TestTreeSitterLayoutDoesNotTreatExportedFunctionBodyStringAsModule(t *testing.T) {
	oldSource := []byte("export function Widget() {\n  const value = useMemo(() => 'old', []);\n  return value;\n}\n")
	newSource := []byte("export function Widget() {\n  const value = useMemo(() => 'new', []);\n  return value;\n}\n")
	plan, err := New(0).Analyze(context.Background(), Input{Path: "widget.tsx", Old: oldSource, New: newSource})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Layout == nil {
		t.Skip("normalized AST layout unavailable without cgo")
	}
	for _, block := range plan.Layout.Blocks {
		if block.Role == "export_statement" {
			t.Fatalf("exported function body was mistaken for a module export: %#v", block)
		}
	}
}

func TestTreeSitterLayoutPairsRepeatedCallsByDeclarationBinding(t *testing.T) {
	oldSource := []byte("const first = useMemo(() => 1, []);\nconst visible = useMemo(() => 2, [a, b]);\n")
	newSource := []byte("const first = useMemo(() => 1, []);\nconst visible = useMemo(() => 2, [\n  a,\n  added,\n  b,\n]);\n")
	plan, err := New(0).Analyze(context.Background(), Input{Path: "widget.tsx", Old: oldSource, New: newSource})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Layout == nil {
		t.Skip("normalized AST layout unavailable without cgo")
	}
	found := false
	for _, block := range plan.Layout.Blocks {
		for _, row := range block.Rows {
			if row.New != nil && strings.TrimSpace(row.New.Text) == "added," {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("changed repeated call was not paired by its declaration: %#v", plan.Layout.Blocks)
	}
}

func TestTreeSitterLayoutPreservesExistingNestedIndentation(t *testing.T) {
	adapter := newTreeSitterAdapter()
	if !adapter.supports("labels.ts") {
		t.Skip("tree-sitter unavailable without cgo")
	}
	source := []byte("export const LABELS = {\n  section: {\n    existing: 'Existing',\n    added: 'Added',\n  },\n} as const;\n")
	plan, err := adapter.analyze(context.Background(), Input{Path: "labels.ts", Old: source, New: source})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := layoutText(plan.Layout, NewSide), strings.TrimSuffix(string(source), "\n"); got != want {
		t.Fatalf("normalized nested indentation changed:\n%s\nwant:\n%s", got, want)
	}
}

func TestTreeSitterLayoutKeepsEmptyCollectionsInline(t *testing.T) {
	adapter := newTreeSitterAdapter()
	if !adapter.supports("empty.ts") {
		t.Skip("tree-sitter unavailable without cgo")
	}
	source := []byte("const result = getVisibleComponents({ componentProfiles: [], filters: {}, populated: [first, second] });\n")
	plan, err := adapter.analyze(context.Background(), Input{Path: "empty.ts", Old: source, New: source})
	if err != nil {
		t.Fatal(err)
	}
	got := layoutText(plan.Layout, NewSide)
	if !strings.Contains(got, "componentProfiles: [],") || !strings.Contains(got, "filters: {},") {
		t.Fatalf("normalized empty collection was expanded:\n%s", got)
	}
	if !strings.Contains(got, "populated: [\n") {
		t.Fatalf("populated collection was not expanded:\n%s", got)
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

func TestAnalyzerReportsSemanticBudgetFallback(t *testing.T) {
	if !newTreeSitterAdapter().supports("large.ts") {
		t.Skip("tree-sitter unavailable without cgo")
	}
	large := []byte("// " + strings.Repeat("x", maxSemanticSourceBytes+1))
	plan, err := New(0).Analyze(context.Background(), Input{Path: "large.ts", Old: large, New: large})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Engine != EngineToken || !strings.Contains(plan.Warning, "semantic budget") {
		t.Fatalf("budget fallback = %#v", plan)
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

func layoutText(layout *Layout, side Side) string {
	var text []string
	for _, block := range layout.Blocks {
		for _, row := range block.Rows {
			line := row.New
			if side == OldSide {
				line = row.Old
			}
			if line != nil {
				text = append(text, line.Text)
			}
		}
	}
	return strings.Join(text, "\n")
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
