# revui

`revui` is a local, GitHub-like review desk for the moment before a branch becomes a pull request. It shows changed files beside a syntax-highlighted diff, lets you leave review comments on lines or ranges, and gives an LLM a precise queue of unresolved feedback to address.

Nothing is sent to GitHub. Review state lives under `.git/revui`, and the agent is explicitly told not to commit, push, open a PR, or post comments.

## What it does

- Compares the current branch with the merge base of the repository's default branch.
- Includes committed branch changes, staged and unstaged edits, renames, deletions, binary files, and untracked files.
- Provides unified and split GitHub-style diffs with syntax highlighting.
- Adds editable comments to a line, line range, or the whole review.
- Persists comments per branch without adding files to the working tree.
- Fuzzy-searches changed paths for instant file jumps.
- Previews the complete LLM prompt before making any changes.
- Runs Codex by default, then refreshes the diff while leaving resolution decisions to the reviewer.
- Adapts to narrow terminals with a keyboard-switchable single-pane layout.

## Install

Go 1.26 or newer is required.

```sh
go install github.com/mattwalker/revui/cmd/revui@latest
```

For local development:

```sh
make build
./revui --help
```

## Use

From anywhere inside a Git repository:

```sh
revui
```

Override the detected base when needed:

```sh
revui --base origin/develop
```

The default agent command is:

```sh
codex exec --sandbox workspace-write -
```

Configure any command that reads a prompt from standard input:

```sh
REVUI_AGENT_COMMAND='claude -p' revui
```

The configured command runs in the repository root. Treat it as trusted local configuration.

## Keys

| Key | Action |
| --- | --- |
| `j` / `k`, arrows | Move through files or diff lines |
| `tab`, `h` / `l` | Switch pane |
| `/` | Fuzzy-search changed files |
| `[` / `]` | Previous or next hunk |
| `s` | Toggle unified and split diff |
| `v`, then move | Select a line range |
| `c` | Comment on the current line or range |
| `shift+c` | Comment on the whole review |
| `e` / `r` / `d` | Edit, resolve/reopen, or delete a comment |
| `n` / `p` | Next or previous comment |
| `a` | Preview the prompt and run the agent |
| `R` | Refresh the Git diff |
| `?` | Open the complete keymap |
| `q` | Quit |

Inside the comment editor, `enter` adds a line and `ctrl+s` saves.

## Review workflow

1. Inspect each changed file and hunk.
2. Add comments describing concrete changes.
3. Press `a` and inspect the exact compiled prompt.
4. Run the agent and review the refreshed diff.
5. Resolve comments manually only when the result is satisfactory.

## Development

```sh
make check
```

The test suite creates real temporary Git repositories to exercise merge-base comparison and tracked, staged, unstaged, and untracked changes. Core packages are separated by responsibility:

- `internal/gitrepo`: Git discovery and diff collection
- `internal/diff`: unified-diff parser and line model
- `internal/review`: atomic, Git-local review persistence
- `internal/agent`: prompt compiler and configurable command runner
- `internal/ui`: Bubble Tea state, interaction, and Lip Gloss rendering

## Scope

`revui` is intentionally a pre-PR tool. It does not authenticate with GitHub, import pull requests, submit reviews, create commits, or push branches.
