# What revui can learn from Difftastic

This note reviews Difftastic at commit
[`ba9573c`](https://github.com/Wilfred/difftastic/tree/ba9573cb1956269868ef2a4bc0d9aee623629335).
It focuses on architectural lessons for revui's experimental semantic diff rather than on
reimplementing Difftastic wholesale.

## Executive recommendation

Revui should adopt Difftastic's **pipeline shape**, not immediately adopt its complete
shortest-path algorithm:

1. Parse both sources through a language adapter.
2. Convert parser-specific trees into a compact, position-preserving semantic tree.
3. Fingerprint and remove large unchanged regions before doing expensive matching.
4. Match only the ambiguous regions with an explicitly costed algorithm.
5. Produce source-range correspondences as the engine result; let the TUI decide layout,
   line pairing, density, and highlighting.
6. Fall back conservatively and visibly whenever parsing or matching exceeds a budget.

This would directly improve revui's formatter-noise problem. The largest immediate gap is
step 2: revui currently compares a flattened stream, while Difftastic keeps delimiter and
nesting structure in a small language-neutral tree.

## Parsing and the intermediate representation

Difftastic does not diff raw Tree-sitter trees. It recursively converts them to a uniform
tree whose nodes are either:

- an **atom** with content, source position, and token kind; or
- a **list** with opening delimiter, children, closing delimiter, and their source positions.

Whitespace between nodes is intentionally absent, and positions are ignored for equality
but retained for display. This is the central separation that lets formatting move tokens
without making them semantically novel while still allowing the renderer to show literal
source. See the [internals documentation](https://github.com/Wilfred/difftastic/blob/ba9573cb1956269868ef2a4bc0d9aee623629335/manual/src/parsing.md)
and the [`Syntax` representation](https://github.com/Wilfred/difftastic/blob/ba9573cb1956269868ef2a4bc0d9aee623629335/src/parse/syntax.rs#L81-L108).

The conversion is deliberately configurable by language. A `TreeSitterConfig` supplies:

- nodes to collapse into indivisible atoms, especially strings;
- delimiter pairs that belong to their enclosing list;
- trailing tokens that may be ignored only in specific contexts;
- a Tree-sitter highlight query; and
- optional embedded-language parsers.

These policies are not cosmetic. Treating delimiters as independent atoms can match a `(`
against an unrelated `(` and produce unbalanced output. Flattening a string's children into
one atom ensures whitespace inside the literal remains meaningful, at the cost of less
granular matching. The tradeoff is documented directly in
[`TreeSitterConfig`](https://github.com/Wilfred/difftastic/blob/ba9573cb1956269868ef2a4bc0d9aee623629335/src/parse/tree_sitter_parser.rs#L31-L81)
and the [parser-extension guide](https://github.com/Wilfred/difftastic/blob/ba9573cb1956269868ef2a4bc0d9aee623629335/manual/src/adding_a_parser.md#configure-parsing).

### Recommendation for revui

Replace the flattened semantic entry stream with a small internal model such as:

```text
Node = Atom(kind, content, sourceRanges)
     | List(role, open, children, close, sourceRanges)
```

Keep Tree-sitter node names only as optional `role` metadata. Equality should primarily
use normalized atom content and recursively computed subtree IDs, while rendering and
copying always use original source ranges. Start with TypeScript/TSX, but define language
configuration separately from the matcher so Apex and other grammars do not require
matcher changes.

Revui should also add language fixtures for strings, template strings, comments, JSX,
delimiters, and trailing commas. Difftastic's source makes clear that a parser merely being
available is insufficient; each grammar needs explicit adaptation.

## Matching: shrink first, search second

Difftastic models a diff as a path through a graph. A graph vertex identifies the next
unmatched node on each side plus entered delimiter state; edges represent matching a node,
entering matching delimiters, inserting/removing syntax, or replacing similar strings and
comments. Its cost model makes unchanged syntax cheapest, penalizes punctuation matches,
and prefers a replacement to two independent novel nodes. See the
[`Vertex` and `Edge` model](https://github.com/Wilfred/difftastic/blob/ba9573cb1956269868ef2a4bc0d9aee623629335/src/diff/graph.rs#L16-L64)
and [edge costs](https://github.com/Wilfred/difftastic/blob/ba9573cb1956269868ef2a4bc0d9aee623629335/src/diff/graph.rs#L322-L373).

It uses Dijkstra rather than A*. The source explains that cheap whole-subtree edges jump a
long distance through the apparent grid, making a useful admissible A* heuristic difficult.
More importantly, Difftastic says preprocessing into smaller sections is more effective
than heuristic search. See [`shortest_path.rs`](https://github.com/Wilfred/difftastic/blob/ba9573cb1956269868ef2a4bc0d9aee623629335/src/diff/shortest_path.rs#L1-L42).

That preprocessing is substantial:

- equal prefixes and suffixes are removed;
- sufficiently large equal top-level subtrees are found by LCS over subtree content IDs;
- adjacent, mostly unchanged lists are split into independent problems; and
- only the remaining sections enter graph search.

The code intentionally refuses to anchor on tiny equal nodes such as a lone comma because
doing so can make the resulting diff worse. See
[`mark_unchanged`](https://github.com/Wilfred/difftastic/blob/ba9573cb1956269868ef2a4bc0d9aee623629335/src/diff/unchanged.rs#L12-L33)
and [`split_unchanged_toplevel`](https://github.com/Wilfred/difftastic/blob/ba9573cb1956269868ef2a4bc0d9aee623629335/src/diff/unchanged.rs#L243-L302).

After matching, Difftastic applies **slider corrections**: when multiple equally valid
minimal diffs exist, it shifts which delimiter or repeated token is considered novel to
produce a more contiguous, readable result. The choice can be language-family-specific;
Lisp-like languages prefer an outer delimiter while most C-like syntax prefers the inner
one. See [`sliders.rs`](https://github.com/Wilfred/difftastic/blob/ba9573cb1956269868ef2a4bc0d9aee623629335/src/diff/sliders.rs#L1-L76).

### Recommendation for revui

Implement the unchanged-subtree pass before considering a general graph matcher. It offers
both the largest likely performance win and better anchors for the current normalization
view. A staged matcher is appropriate:

1. byte-identical early exit;
2. exact subtree fingerprints and unique large anchors;
3. role-aware pairing of nearby declarations, calls, object members, array items, and JSX
   nodes;
4. a bounded graph search only inside unresolved paired regions;
5. a presentation-oriented correction pass for ambiguous repeated punctuation/tokens.

Do **not** start by porting Dijkstra over the entire file. The state space is roughly
quadratic when changes are large, and Difftastic's implementation contains years of cost
model and edge-case tuning.

## Reformatting, unchanged content, and moves

Difftastic can match equal nodes at different tree depths. This is why adding a wrapper can
highlight only its delimiters rather than its entire contents. Its manual documents the
desired behavior for adding, changing, and expanding delimiters in
[tricky cases](https://github.com/Wilfred/difftastic/blob/ba9573cb1956269868ef2a4bc0d9aee623629335/manual/src/tricky_cases.md#adding-delimiters).

However, Difftastic is deliberately **order-sensitive**. It does not offer an
"ignore moved lines" mode; reordering list elements is reported as change. The project
explains that evaluation order may be semantically meaningful, unordered tree matching is
computationally difficult, and even ostensibly unordered data formats can expose order.
See the [unordered-data discussion](https://github.com/Wilfred/difftastic/blob/ba9573cb1956269868ef2a4bc0d9aee623629335/manual/src/tricky_cases.md#unordered-data-types)
and the [FAQ answer](https://github.com/Wilfred/difftastic/blob/ba9573cb1956269868ef2a4bc0d9aee623629335/README.md#can-difftastic-ignore-reordering).

### Decision for revui

Revui follows Difftastic's order-sensitive model and does not suppress moved lines. Even a
conservative identity-based filter can hide meaningful evaluation-order changes and makes
the semantic plan harder to trust. Reordering therefore remains an ordinary visible
removal and addition; language-specific unordered matching can be reconsidered only with
stronger semantic evidence.

## Engine output and display

Difftastic's matcher does not emit formatted source. It produces a richer intermediate
result than independent changed ranges: matched old/new token positions, novel tokens,
unchanged pieces of changed items, novel words, and deliberately ignored tokens. Display
code then derives corresponding line pairs, enforces monotonic line
ordering, groups nearby changed lines into hunks, adds context, and independently renders
inline or side-by-side layouts. See the
[`MatchKind`/position model](https://github.com/Wilfred/difftastic/blob/ba9573cb1956269868ef2a4bc0d9aee623629335/src/parse/syntax.rs#L679-L735),
[line correspondence logic](https://github.com/Wilfred/difftastic/blob/ba9573cb1956269868ef2a4bc0d9aee623629335/src/display/context.rs#L72-L118),
and [hunk construction](https://github.com/Wilfred/difftastic/blob/ba9573cb1956269868ef2a4bc0d9aee623629335/src/display/hunks.rs#L1-L31).

This supports an important trust property: Difftastic displays original source, not a
pretty-printed surrogate. Its side-by-side renderer also caps excessively wide columns,
uses equal content widths so wrapping happens consistently, and inserts explicit missing
line markers. See [`SourceDimensions`](https://github.com/Wilfred/difftastic/blob/ba9573cb1956269868ef2a4bc0d9aee623629335/src/display/side_by_side.rs#L126-L224).

### Recommendation for revui

Make the semantic engine return a layout-neutral `Plan` containing:

- old/new source ranges and node identities;
- correspondence pairs;
- change kind (`Unchanged`, `Added`, `Removed`, `Replaced`, `Ignored`);
- confidence and engine/fallback provenance.

For normalized presentation, attach confidence-scored structural layout blocks to that
plan. Each block owns its original ranges and completed row correspondences. A contiguous
same-role one-to-many rewrite may be represented as one conservative composite; reordered,
duplicate, mixed-role, and otherwise ambiguous owners remain literal. The UI must not
rediscover owners or run a second matcher.

Raw, unified, split, and normalized views should all consume the same plan. A normalized
block may insert virtual breaks, but every virtual row must map back to original ranges.
Highlight density belongs to the renderer: a pure addition needs a row marker, not a bright
box around every token; a sparse replacement benefits from intraline emphasis.

Difftastic narrows word-level comparison mostly to paired comments and strings, and only
uses it when more than two non-space words match and the common content is sufficiently
dense. Otherwise the atom remains one novel region. Revui need not copy those exact
thresholds, but should copy the principle that intraline emphasis requires a credible
replacement pair rather than merely two nearby changed lines. See
[`split_atom_words` and `has_common_words`](https://github.com/Wilfred/difftastic/blob/ba9573cb1956269868ef2a4bc0d9aee623629335/src/parse/syntax.rs#L737-L855).

## Performance, limits, and fallback

Difftastic uses several defensive layers:

- byte-identical files return before parsing;
- Tree-sitter configuration and highlight queries are cached per language because query
  construction can take tens of milliseconds;
- unchanged preprocessing reduces graph inputs;
- graph search lazily constructs neighbours and stops at a configurable vertex limit;
- exceeding the graph limit falls back to a line/word diff;
- too many parse errors also trigger the text fallback; and
- word-level fallback has its own maximum size before highlighting whole changed regions.

The pipeline and graph fallback are visible in
[`main.rs`](https://github.com/Wilfred/difftastic/blob/ba9573cb1956269868ef2a4bc0d9aee623629335/src/main.rs#L629-L750),
the graph limit in
[`shortest_path.rs`](https://github.com/Wilfred/difftastic/blob/ba9573cb1956269868ef2a4bc0d9aee623629335/src/diff/shortest_path.rs#L58-L107),
parse-error accounting in
[`to_syntax_with_limit`](https://github.com/Wilfred/difftastic/blob/ba9573cb1956269868ef2a4bc0d9aee623629335/src/parse/tree_sitter_parser.rs#L1516-L1555),
and the text fallback limit in
[`line_parser.rs`](https://github.com/Wilfred/difftastic/blob/ba9573cb1956269868ef2a4bc0d9aee623629335/src/line_parser.rs#L9-L12).

Difftastic's own README still lists large-change performance, memory use, display ambiguity,
and robustness as known issues. That is evidence against treating a full structural matcher
as an unbounded synchronous UI operation. See its [known issues](https://github.com/Wilfred/difftastic/blob/ba9573cb1956269868ef2a4bc0d9aee623629335/README.md#known-issues).

### Recommendation for revui

Retain revui's asynchronous, cancellable analysis and content-hash cache. Add explicit
budgets for parse time, node count, unresolved-region size, and graph vertices. A completed
analysis should be immutable and atomically replace the previous plan. The UI should show
`AST`, `TOKEN*`, or a concise fallback reason, never hang on an unbounded matcher.

Cache immutable grammar/query configuration globally, but cache analysis results by source
pair and engine version. Do not rebuild Tree-sitter queries, AST projections, normalized
rows, or highlighting during scrolling.

## What not to copy

- **Do not embed Difftastic as a subprocess dependency.** It would complicate installation,
  cancellation, source-range integration, and revui's single-binary promise. Its terminal
  output is intended for humans, not as a stable machine interface.
- **Do not copy its whole-file Dijkstra implementation first.** Adopt segmentation and a
  compact syntax model, benchmark unresolved regions, and add bounded search only where the
  simpler matcher is demonstrably insufficient.
- **Do not equate syntactic equality with safe move suppression.** Difftastic explicitly
  keeps ordering meaningful.
- **Do not let semantic analysis dictate presentation.** The same correspondences should
  support raw and normalized views without changing what `y` copies or what line numbers
  mean.
- **Do not hide fallback.** A conservative textual diff is preferable to a semantic engine
  incorrectly claiming equivalence.

Difftastic's JSON output includes aligned line pairs and positional change chunks. It could
serve as a development-time oracle for revui's golden corpus and benchmarks without becoming
a runtime dependency. See [`display/json.rs`](https://github.com/Wilfred/difftastic/blob/ba9573cb1956269868ef2a4bc0d9aee623629335/src/display/json.rs#L14-L45).

## Suggested implementation order

1. Introduce the atom/list semantic tree behind revui's existing analyzer interface.
2. Add TypeScript/TSX grammar policy for literals, comments, JSX, delimiters, and optional
   trailing commas.
3. Compute recursive content IDs and unique-subtree metadata.
4. Split out exact and mostly unchanged subtrees before matching.
5. Emit source-range correspondences and migrate both raw and normalized renderers to them.
6. Add bounded role-aware matching inside unresolved regions.
7. Add a small slider/readability correction pass.
8. Implement conservative unique-subtree move detection as a distinct optional phase.
9. Expand languages only through adapter fixtures and performance budgets.

The strongest near-term improvement is therefore not more normalization rules. It is a
better semantic intermediate representation with exact subtree anchoring. That foundation
will make raw highlighting quieter, normalized alignment more reliable, and future move
detection safer.
