# Changelog

All notable changes to revui are documented here. The project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2026-07-17

### Added

- GitHub-like unified and split branch diffs with syntax highlighting.
- Intraline change emphasis, optional raw-diff whitespace filtering, and experimental whitespace-insensitive whole-source semantic highlighting with order-sensitive TypeScript/TSX syntax-tree matching, bounded fallback, and a universal token engine.
- Experimental normalized TypeScript/TSX split layout driven by confidence-scored semantic owner blocks, including exact JSX-subtree alignment across misleading Git context matches, with literal Git fallback for ambiguous rewrites.
- Flat, tree, contextual, and all-files repository exploration.
- Full-file source view, fuzzy file jump, and repository text search.
- Reviewed-file fingerprints and user-wide display preferences.
- Live repository watching with debounced refresh.
- Keyboard and mouse navigation, selection, and accelerated scrolling.
- OSC52 copying with file and branch/base source locations.
- macOS and Linux release archives, checksums, SBOMs, and build provenance.

### Security

- Local-only operation with no GitHub authentication, network requests, or repository mutation.

[0.1.0]: https://github.com/TenaciousMaker/revui/releases/tag/v0.1.0
