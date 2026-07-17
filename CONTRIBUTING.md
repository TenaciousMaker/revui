# Contributing to revui

Thanks for helping make local code review clearer and faster.

## Before opening a change

- Search existing issues and pull requests.
- For a new workflow, open a feature request describing the review problem before designing controls.
- Keep revui local and read-only. GitHub integration, embedded agents, commits, pushes, and terminal-session management are intentionally outside the project scope.

## Development setup

revui requires Go 1.25 or newer and Git.

```sh
git clone https://github.com/TenaciousMaker/revui.git
cd revui
make check
make build
```

Use `make fmt` before submitting. `make check` never rewrites source files.

For UI changes, exercise keyboard and mouse behavior in unified, split, full-source, tree, and narrow-terminal views. Include a terminal capture when rendering changes.

## Design principles

- Put substantial behavior behind small interfaces at real seams.
- Keep state and rendering local to the pane or mode that owns it.
- Preserve keyboard access for every mouse interaction.
- Never use color as the only signal.
- Avoid work proportional to the entire repository during scrolling or cursor movement.
- Explain failures with a next action.

## Tests

```sh
make test
make test-race
make coverage
```

Git behavior should be tested with temporary repositories. Rendering tests should use deterministic dimensions and strip ANSI only when color is not under test. Performance work should include a benchmark that represents the reported repository size.

## Pull requests

Keep changes focused, explain the user-visible outcome, and update the README or keymap when behavior changes. By contributing, you agree that your contribution is licensed under the project's MIT license.
