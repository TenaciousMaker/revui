package gitrepo

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/mattwalker/revui/internal/diff"
)

type Repository struct {
	Root        string
	Branch      string
	Base        string
	MergeBase   string
	ReviewPath  string
	Files       []diff.File
	RawDiff     string
	DefaultBase string
}

func Open(start, requestedBase string) (*Repository, error) {
	root, err := git(start, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, errors.New("revui must be launched inside a Git repository")
	}
	root = strings.TrimSpace(root)
	branch, _ := git(root, "branch", "--show-current")
	branch = strings.TrimSpace(branch)
	if branch == "" {
		branch, _ = git(root, "rev-parse", "--short", "HEAD")
		branch = strings.TrimSpace(branch)
	}
	base := requestedBase
	if base == "" {
		base = detectBase(root)
	}
	mergeBase, err := git(root, "merge-base", "HEAD", base)
	if err != nil {
		return nil, fmt.Errorf("find merge base with %s: %w", base, err)
	}
	mergeBase = strings.TrimSpace(mergeBase)
	raw, err := git(root, "diff", "--no-ext-diff", "--no-color", "--find-renames", "--unified=3", mergeBase, "--")
	if err != nil {
		return nil, fmt.Errorf("load diff: %w", err)
	}
	untracked, _ := git(root, "ls-files", "--others", "--exclude-standard")
	for _, path := range strings.Fields(untracked) {
		piece, pieceErr := gitDiffNoIndex(root, path)
		if pieceErr == nil && piece != "" {
			raw += "\n" + normalizeUntracked(piece, root, path)
		}
	}
	files, err := diff.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse diff: %w", err)
	}
	reviewDir, err := git(root, "rev-parse", "--git-path", "revui")
	if err != nil {
		return nil, fmt.Errorf("locate Git metadata: %w", err)
	}
	reviewDir = strings.TrimSpace(reviewDir)
	if !filepath.IsAbs(reviewDir) {
		reviewDir = filepath.Join(root, reviewDir)
	}
	return &Repository{
		Root: root, Branch: branch, Base: base, MergeBase: mergeBase,
		ReviewPath: filepath.Join(reviewDir, safeName(branch)+".json"),
		Files:      files, RawDiff: raw, DefaultBase: base,
	}, nil
}

func detectBase(root string) string {
	if ref, err := git(root, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil {
		return strings.TrimSpace(ref)
	}
	for _, candidate := range []string{"origin/main", "main", "origin/master", "master"} {
		if _, err := git(root, "rev-parse", "--verify", candidate+"^{commit}"); err == nil {
			return candidate
		}
	}
	return "HEAD^"
}

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return stdout.String(), errors.New(message)
	}
	return stdout.String(), nil
}

func gitDiffNoIndex(root, path string) (string, error) {
	cmd := exec.Command("git", "diff", "--no-index", "--no-color", "--unified=3", "--", "/dev/null", path)
	cmd.Dir = root
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 {
			return "", fmt.Errorf("diff untracked file %s: %s", path, strings.TrimSpace(stderr.String()))
		}
	}
	return stdout.String(), nil
}

func normalizeUntracked(raw, root, path string) string {
	abs := filepath.Join(root, path)
	raw = strings.ReplaceAll(raw, "b/"+abs, "b/"+path)
	raw = strings.ReplaceAll(raw, abs, path)
	return raw
}

func safeName(value string) string {
	value = regexp.MustCompile(`[^a-zA-Z0-9._-]+`).ReplaceAllString(value, "-")
	if value == "" {
		return "detached"
	}
	return value
}

func (r *Repository) Totals() (additions, deletions int) {
	for _, file := range r.Files {
		additions += file.Additions
		deletions += file.Deletions
	}
	return
}

func (r *Repository) Paths() []string {
	paths := make([]string, 0, len(r.Files))
	for _, file := range r.Files {
		paths = append(paths, file.Path)
	}
	sort.Strings(paths)
	return paths
}
