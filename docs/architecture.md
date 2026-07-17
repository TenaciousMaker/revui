# Architecture

revui is a local, read-only branch review application built on Bubble Tea. Its design favors deep modules: small interfaces that hide Git, persistence, watching, and rendering detail.

## Data flow

```text
Git + working tree
       │
       ▼
gitrepo snapshot ── watcher refresh
       │
       ▼
Bubble Tea coordinator
  ├─ file-pane state
  ├─ content-pane state
  ├─ search state
  ├─ selection state
  └─ viewport state
       │              full old/new source
       │                      │
       │                      ▼
       │              semantic analyzer
       │               ├─ Tree-sitter adapters
       │               └─ token fallback
       │                      │
       │             immutable byte ranges
       │                      │
       ▼
Lip Gloss terminal view + OSC52 clipboard
```

`internal/gitrepo` owns Git discovery, merge-base comparison, diff collection, repository search, and source reads. Git subprocess behavior sits behind the `Runner` seam so cancellation and errors remain local and tests can supply an adapter.

`internal/diff` parses unified diff text into the line model consumed by both unified and split renderers.

`internal/semantic` identifies meaningful edits independently of Git's line wrapping. Its narrow `Analyzer` interface accepts complete old/new source and returns immutable byte ranges, move pairs, the engine used, and any fallback warning. Tree-sitter is an isolated adapter rather than a UI dependency; TypeScript and TSX are the first supported grammars. A language-neutral token adapter is always available, including non-CGO builds. Plans are cached by path and content hashes in a bounded in-memory LRU.

The UI schedules semantic work as a Bubble Tea command. Selecting another file, refreshing the repository, disabling the feature, or exiting cancels obsolete work; results carry snapshot, file, and request identities so late messages cannot mutate the current view. Only the final range-to-line projection lives in `internal/ui`. Rendering and scrolling never parse source.

`internal/config` owns versioned user-wide display preferences. `internal/review` separately owns repository/branch reviewed-file fingerprints under Git metadata. Both write atomically with user-only permissions.

`internal/watcher` converts noisy filesystem activity into debounced refresh events. The UI owns its lifecycle and cancels in-flight repository operations on replacement or exit.

`internal/ui` coordinates Bubble Tea messages while pane-specific state remains grouped by responsibility. Repository snapshots are replaced as a whole; navigation never observes a partially refreshed diff.

## Invariants

- revui never modifies working-tree content or Git history.
- Git comparisons start at the merge base, then include index, working-tree, and untracked changes.
- Reviewed state is valid only while a file's diff fingerprint matches.
- Every mouse workflow has a keyboard equivalent.
- Scrolling performs no Git, filesystem, syntax-highlighting, or tree-rebuild work.
- Search and refresh results are ignored when superseded.
- Semantic analysis is cancellable, never blocks rendering, and can only annotate the snapshot that requested it.
- Color reinforces meaning but never carries it alone.

## Extending revui

Add behavior behind an existing interface when possible. Introduce a new seam only when it isolates a volatile dependency or has at least two real adapters—for example Tree-sitter/token semantic analysis or production/test Git execution. Avoid exposing parser trees, subprocess details, or renderer bookkeeping to the coordinator.

New semantic languages belong in adapter files inside `internal/semantic`. An adapter must own parser lifecycle, reject syntax-error trees, honor cancellation between expensive phases, and emit source byte ranges only. Do not pass parser nodes into the UI. Add grammars deliberately: each one increases binary size and release complexity, so a language needs representative fixtures and a measurable review-quality improvement before becoming a dependency.

### Semantic dependency choice

revui embeds the official Tree-sitter Go binding behind the adapter seam. It does not shell out to `diffsitter` or `ast-grep`: both projects demonstrate the value of syntax-aware diffs, but an external executable would weaken revui's one-binary installation and make cancellation, byte-range projection, and release compatibility harder to control. The interface intentionally leaves room for a future alternative engine without coupling the UI to Tree-sitter queries or node types.
