# Security policy

## Supported versions

The latest published release receives security fixes. During the v0.x beta, fixes may require upgrading to the newest minor release.

## Reporting a vulnerability

Do not open a public issue for a vulnerability or include sensitive repository content in a report. Use GitHub's **Security → Report a vulnerability** flow for [TenaciousMaker/revui](https://github.com/TenaciousMaker/revui/security/advisories/new).

Include the affected version, operating system, terminal environment, reproduction steps, and potential impact. You should receive an acknowledgement within seven days. We will coordinate disclosure after a fix is available.

## Security model

revui reads local Git metadata and working-tree files, executes the local `git` binary, watches repository directories, writes user preferences and `.git/revui` review state, and emits selected text through OSC52. It does not make network requests or execute repository content.
