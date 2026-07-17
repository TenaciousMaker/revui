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
       │
       ▼
Lip Gloss terminal view + OSC52 clipboard
```

`internal/gitrepo` owns Git discovery, merge-base comparison, diff collection, repository search, and source reads. Git subprocess behavior sits behind the `Runner` seam so cancellation and errors remain local and tests can supply an adapter.

`internal/diff` parses unified diff text into the line model consumed by both unified and split renderers.

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
- Color reinforces meaning but never carries it alone.

## Extending revui

Add behavior behind an existing interface when possible. Introduce a new seam only when at least two adapters are real—for example production Git execution and a deterministic test runner. Avoid exposing subprocess details or renderer bookkeeping to the coordinator.
