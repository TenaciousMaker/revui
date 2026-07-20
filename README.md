# revui

[![CI](https://github.com/TenaciousMaker/revui/actions/workflows/ci.yml/badge.svg)](https://github.com/TenaciousMaker/revui/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/TenaciousMaker/revui.svg)](https://pkg.go.dev/github.com/TenaciousMaker/revui)
[![License: MIT](https://img.shields.io/badge/license-MIT-58a6ff.svg)](LICENSE)

**Review your PR before it's a PR.**

revui is a fast, local review workspace for Git branches. It keeps the changed-file tree on the left and a GitHub-like, syntax-highlighted diff on the right, then lets you inspect complete files, search the repository, track reviewed files, and copy precise code context into any editor or LLM.

<p align="center">
  <img src="docs/assets/preview.svg" alt="revui showing a changed-file tree and unified diff" width="100%">
</p>

revui never connects to GitHub or sends code anywhere. It does not edit files, commit, push, or invoke an agent.

## Install

Download a macOS or Linux archive from [GitHub Releases](https://github.com/TenaciousMaker/revui/releases), extract it, and place `revui` on your `PATH`.

With Go 1.25 or newer:

```sh
go install github.com/TenaciousMaker/revui/cmd/revui@latest
```

Build from source:

```sh
git clone https://github.com/TenaciousMaker/revui.git
cd revui
make build
./revui --version
```

## Five-minute workflow

Run revui anywhere inside a Git repository:

```sh
revui
```

revui detects the repository's default branch and reviews everything since its merge base, including committed, staged, unstaged, renamed, deleted, binary, and untracked changes. Override the comparison when needed:

```sh
revui --base origin/develop
```

1. Move through changed files with `j`/`k` or the mouse.
2. Press `t` for the directory tree and `A` for changed, context, or all files.
3. Press `o` to switch between the diff and complete source.
4. Press `f` to search text across the repository.
5. Press `space` when a changed file is reviewed.
6. Press `y` to copy the current line or a selected range with its file and source location.

The watcher refreshes the review after save bursts without moving your current file or line. CGO-enabled macOS builds use one recursive FSEvents stream; Linux uses inotify through fsnotify.

## What makes it useful

- **One coherent branch view.** Merge-base comparison plus committed and working-tree changes.
- **Review in context.** Toggle changed files, their sibling context, or the entire non-ignored repository tree.
- **Diff or source.** Switch between unified/split diffs and the complete working or base file.
- **Signal controls.** Emphasize credible changed words inside replacement lines, normalize common structures across popular languages, or hide whitespace-only edits in the raw diff.
- **Repository search.** See grouped context around literal matches and jump directly to a source line.
- **Durable progress.** Reviewed files stay reviewed until their diff fingerprint changes.
- **Location-rich copy.** Keyboard ranges and pane-constrained mouse selections copy clean code with branch/base line locations.
- **Fast navigation.** Cached rendering, compact directory chains, and accelerated, frame-coalesced scrolling remain responsive in large repositories.
- **Local by design.** No account, token, network request, or working-tree mutation.

## Keys

| Key | Action |
| --- | --- |
| `j` / `k`, arrows | Move through files or code lines |
| Mouse click / wheel | Position the active row or scroll the pane under the pointer |
| Mouse drag | Select visible code inside the current pane |
| `tab`, `h` / `l` | Switch panes or navigate tree folders |
| `t` | Toggle flat and tree file layouts |
| `A` | Cycle changed, context, and all-files scopes |
| `space` | Toggle reviewed state for the selected changed file |
| `w` | Fit or restore the file-pane width |
| `enter` | Open a file, result, or folder |
| `/` | Fuzzy-jump to a changed file |
| `f` | Search text across the repository |
| `o` | Toggle complete source and diff |
| `i` | Toggle whitespace-only changes in the raw diff |
| `e` | Toggle experimental semantic highlighting |
| `n` | Toggle normalized AST layout in split view |
| `d` | Toggle optional Difftastic structural split |
| `v`, then move | Define a code range |
| `y` | Copy the current line or selected range with location |
| `[` / `]` | Jump to the previous or next hunk |
| `s` | Toggle unified and split diff |
| `R` | Refresh from Git |
| `?` | Show the complete keymap |
| `q` | Quit |

Search inputs support arrows, Home/End, `ctrl+a/e`, `ctrl+b/f`, `ctrl+u/k`, `ctrl+w`, Backspace/Delete, and bracketed paste. Up/down continue to navigate results.

### Copying code

`y` writes through the terminal's OSC52 clipboard protocol, which works in modern terminals locally and across SSH. The copied block contains the repository-relative file, branch or base line range, and plain source text—never rendered line numbers or diff markers.

If clipboard integration is disabled by your terminal or multiplexer, use its normal text-selection copy command. See [Troubleshooting](#troubleshooting).

## Files, state, and privacy

Global display preferences use the operating system's user configuration directory:

- macOS: `~/Library/Application Support/revui/preferences.json`
- Linux: `${XDG_CONFIG_HOME:-~/.config}/revui/preferences.json`

Flat/tree layout, file scope, pane width, unified/split mode, the raw whitespace filter, semantic highlighting, normalized layout, and Difftastic mode follow you across repositories. Cursor positions and expanded folders are temporary.

Experimental semantic highlighting (`e`) compares complete old and new source instead of pairing Git lines. The diff header reports the active engine: `AST` uses Tree-sitter, `TOKEN*` is the language-neutral fallback, and `SEM…` means analysis is still running. Built-in AST grammars cover TypeScript/TSX, JavaScript/JSX, Go, Python, Rust, Java, JSON, C, C++, and Ruby. Semantic modes inherently ignore formatting whitespace, so the raw `i` filter is inactive while they are enabled. The AST engine matches a compact syntax tree while retaining original source positions, so formatter-driven wrapping, indentation, and grammar-supported trailing commas stay visually quiet without replacing the source you are reviewing. Word emphasis is reserved for credible sparse replacements; pure insertions, deletions, mostly rewritten lines, and reordered syntax remain visibly added or removed. Unsupported languages such as Apex, temporarily invalid syntax, excessive input, or a non-CGO build fall back visibly and safely to tokens.

Tree-sitter support and the descriptor-safe macOS FSEvents watcher are compiled into normal CGO-enabled source builds and `go install` builds. Static non-CGO archives use the portable token fallback and require `R` for manual refresh on macOS. Semantic analysis runs locally, is cancellable, and is cached only in memory for the current process.

Normalized layout (`n`, shown as `NORMALIZED`) is an additional multi-language experiment. It inserts visual line breaks inside confidently related imports, declarations, bindings, arguments, objects, collections, and language equivalents, then renders the semantic engine's completed row alignment. Each grammar has a small declarative normalization profile; constructs without a confident structural owner remain in the literal Git layout instead of being guessed. Contiguous same-role one-to-many rewrites can be shown as a stacked composite; reordered, duplicate, mixed-role, and otherwise ambiguous owners also stay literal. Tokens are never rewritten: line navigation and `y` always address and copy the original source. Press `n` again for the raw Git layout. Unsupported languages continue to show the raw split diff as `NORM N/A`.

Difftastic mode (`d`, shown as `DIFFT`) is an optional experiment for users who have [`difft`](https://difftastic.wilfred.me.uk/) on `PATH`. It asks Difftastic for structural line correspondence and changed byte ranges, then projects that result onto revui's existing Git hunks. Git remains authoritative: files, hunk navigation, line locations, copying, and live refresh all continue to use the raw repository snapshot. If Difftastic is absent, fails, returns an unfamiliar JSON schema, or does not account for every visible Git row, revui displays a warning and preserves the literal raw split.

The adapter has been tested with Difftastic 0.69.0. Its JSON output is currently marked unstable upstream, so compatibility is intentionally guarded rather than assumed. Analysis is local and cancellable. revui writes the two source versions to a private temporary directory with user-only permissions, invokes `difft --display json`, and removes the directory immediately afterward. It never configures Git's external diff mechanism. Press `d` again to return to the raw split.

Reviewed-file fingerprints live under the repository's Git metadata at `.git/revui`. revui adds nothing to the working tree and makes no network requests. Repository search uses `git grep` and respects Git ignore rules.

Set [`NO_COLOR`](https://no-color.org/) to disable semantic colors. Added/deleted markers and focus labels remain visible without color.

## Supported environments

The v0.1 beta supports macOS and Linux on amd64 and arm64. revui needs Git and an interactive terminal with ANSI support. Mouse input and OSC52 clipboard behavior depend on the terminal; the full keyboard workflow remains available without either.

## Troubleshooting

**The wrong base was selected.** Run `revui --base <revision>`. By default revui tries `origin/HEAD`, `main`, and `master`, then falls back to `HEAD^`.

**Clipboard status says copied, but nothing appears.** Enable OSC52 in the terminal or multiplexer, or use terminal-native selection copy.

**Changes are not refreshing.** Press `R`. Some networked or virtual filesystems do not emit reliable filesystem events. On macOS, realtime refresh requires a CGO-enabled build such as a normal `go install`; non-CGO builds deliberately avoid the kqueue behavior that opens a descriptor for every watched entry.

**Colors are difficult to distinguish.** Set `NO_COLOR=1`; `+`, `-`, hunk headers, selected rows, and pane labels provide non-color cues.

**The terminal is narrow.** revui switches to a single visible pane. Use `tab`, `h`, or `l` to move between files and code.

Please use the [bug report form](https://github.com/TenaciousMaker/revui/issues/new?template=bug.yml) for reproducible problems.

## Development

```sh
make check          # formatting, vet, and tests
make test-race      # race detector
make coverage       # enforce the project coverage floor
make release-snapshot
```

See [CONTRIBUTING.md](CONTRIBUTING.md) and [the architecture guide](docs/architecture.md). The demo is reproducible with [VHS](https://github.com/charmbracelet/vhs): `make demo`.

## Scope

revui is intentionally a pre-PR tool. It does not authenticate with GitHub, import or submit reviews, invoke an LLM, manage comments, create commits, push branches, or manage terminal sessions. Copy context out; bring conclusions back to your normal development workflow.

## License

[MIT](LICENSE)
