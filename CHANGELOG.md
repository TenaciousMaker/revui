# Changelog

All notable changes to revui are documented here. The project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Mark or clear every changed file's reviewed state with one shortcut.
- Preserve text baselines for reviewed files, flag files changed afterward, and show a focused diff against the last reviewed version.
- Expand omitted unchanged lines between individual diff hunks with the keyboard or mouse.

## [0.1.0] - 2026-07-17

### Added

- GitHub-like unified and split branch diffs with syntax highlighting.
- Intraline change emphasis, optional raw-diff whitespace filtering, and experimental whitespace-insensitive whole-source semantic highlighting with order-sensitive Tree-sitter matching for TypeScript/TSX, JavaScript/JSX, Go, Python, Rust, Java, JSON, C, C++, and Ruby, plus bounded fallback and a universal token engine.
- Experimental multi-language normalized split layout driven by declarative grammar profiles and confidence-scored semantic owner blocks, including exact JSX-subtree alignment across misleading Git context matches, with literal Git fallback for ambiguous rewrites.
- Optional Difftastic structural split mode with cancellable local analysis, validated JSON spans, whole-file line alignment, and lossless raw-Git fallback.
- Flat, tree, contextual, and all-files repository exploration.
- Full-file source view, fuzzy file jump, and repository text search.
- Reviewed-file fingerprints and user-wide display preferences.
- Live repository watching with debounced refresh.
- Keyboard and mouse navigation, selection, and accelerated scrolling.
- OSC52 copying with file and branch/base source locations.
- macOS and Linux release archives, checksums, SBOMs, and build provenance.

### Fixed

- Replaced macOS kqueue directory watches with one recursive FSEvents stream, preventing large or long-running revui sessions from exhausting the system file table. Native watcher tests now bound descriptors across startup, edit bursts, and shutdown; non-CGO macOS builds fail closed to manual refresh instead of opening per-file descriptors.

### Security

- Local-only operation with no GitHub authentication, network requests, or repository mutation.

[0.1.0]: https://github.com/TenaciousMaker/revui/releases/tag/v0.1.0
