package gitrepo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/TenaciousMaker/revui/internal/diff"
)

type Repository struct {
	Root            string
	Branch          string
	Base            string
	MergeBase       string
	ReviewPath      string
	PreferencesPath string // Optional view-preference path override for embedded callers and tests.
	Files           []diff.File
	AllPaths        []string
	RawDiff         string
	DefaultBase     string
	runner          Runner
}

func Open(start, requestedBase string) (*Repository, error) {
	return OpenContext(context.Background(), start, requestedBase)
}

// OpenContext creates an immutable snapshot of the repository's current review state.
func OpenContext(ctx context.Context, start, requestedBase string) (*Repository, error) {
	return open(ctx, ExecRunner{}, start, requestedBase)
}

func open(ctx context.Context, runner Runner, start, requestedBase string) (*Repository, error) {
	root, err := git(ctx, runner, start, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, errors.New("revui must be launched inside a Git repository")
	}
	root = strings.TrimSpace(root)
	branch, _ := git(ctx, runner, root, "branch", "--show-current")
	branch = strings.TrimSpace(branch)
	if branch == "" {
		branch, _ = git(ctx, runner, root, "rev-parse", "--short", "HEAD")
		branch = strings.TrimSpace(branch)
	}
	base := requestedBase
	if base == "" {
		base = detectBase(ctx, runner, root)
	}
	mergeBase, err := git(ctx, runner, root, "merge-base", "HEAD", base)
	if err != nil {
		return nil, fmt.Errorf("find merge base with %s: %w", base, err)
	}
	mergeBase = strings.TrimSpace(mergeBase)
	raw, err := git(ctx, runner, root, "diff", "--no-ext-diff", "--no-color", "--find-renames", "--unified=3", mergeBase, "--")
	if err != nil {
		return nil, fmt.Errorf("load diff: %w", err)
	}
	untracked, _ := git(ctx, runner, root, "ls-files", "--others", "--exclude-standard")
	for _, path := range strings.Fields(untracked) {
		piece, pieceErr := gitDiffNoIndex(ctx, runner, root, path)
		if pieceErr == nil && piece != "" {
			raw += "\n" + normalizeUntracked(piece, root, path)
		}
	}
	files, err := diff.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse diff: %w", err)
	}
	allOutput, err := git(ctx, runner, root, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, fmt.Errorf("list repository files: %w", err)
	}
	allPaths := nulPaths(allOutput)
	seenPaths := make(map[string]bool, len(allPaths)+len(files))
	for _, path := range allPaths {
		seenPaths[path] = true
	}
	for _, file := range files {
		if !seenPaths[file.Path] {
			allPaths = append(allPaths, file.Path)
			seenPaths[file.Path] = true
		}
	}
	sort.Strings(allPaths)
	reviewDir, err := git(ctx, runner, root, "rev-parse", "--git-path", "revui")
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
		Files:      files, AllPaths: allPaths, RawDiff: raw, DefaultBase: base, runner: runner,
	}, nil
}

func nulPaths(output string) []string {
	parts := strings.Split(output, "\x00")
	paths := make([]string, 0, len(parts))
	for _, path := range parts {
		if path = filepath.ToSlash(path); path != "" {
			paths = append(paths, path)
		}
	}
	return paths
}

// ReadSource returns the working-tree file, or the merge-base version for a deleted file.
func (r *Repository) ReadSource(path string) (content []byte, fromBase bool, err error) {
	return r.ReadSourceContext(context.Background(), path)
}

// ReadSourceContext returns working-tree source, or merge-base source for a deletion.
func (r *Repository) ReadSourceContext(ctx context.Context, path string) (content []byte, fromBase bool, err error) {
	var changed *diff.File
	for index := range r.Files {
		if r.Files[index].Path == path {
			changed = &r.Files[index]
			break
		}
	}
	if changed != nil && changed.Binary {
		return nil, false, fmt.Errorf("%s is binary", path)
	}
	content, err = os.ReadFile(filepath.Join(r.Root, filepath.FromSlash(path)))
	if err == nil {
		return content, false, nil
	}
	if changed == nil || changed.Status != "D" {
		return nil, false, err
	}
	basePath := changed.OldPath
	if basePath == "" {
		basePath = changed.Path
	}
	output, showErr := git(ctx, r.commandRunner(), r.Root, "show", r.MergeBase+":"+basePath)
	if showErr != nil {
		return nil, false, fmt.Errorf("read base source %s: %w", basePath, showErr)
	}
	return []byte(output), true, nil
}

// ReadPairContext returns the merge-base and working-tree versions of a
// changed file. Added and deleted sides are represented by empty content.
func (r *Repository) ReadPairContext(ctx context.Context, file diff.File) (oldSource, newSource []byte, err error) {
	if file.Binary {
		return nil, nil, fmt.Errorf("%s is binary", file.Path)
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if file.Status != "A" {
		oldPath := file.OldPath
		if oldPath == "" {
			oldPath = file.Path
		}
		output, showErr := git(ctx, r.commandRunner(), r.Root, "show", r.MergeBase+":"+oldPath)
		if showErr != nil {
			return nil, nil, fmt.Errorf("read base source %s: %w", oldPath, showErr)
		}
		oldSource = []byte(output)
	}
	if file.Status != "D" {
		content, readErr := os.ReadFile(filepath.Join(r.Root, filepath.FromSlash(file.Path)))
		if readErr != nil {
			return nil, nil, fmt.Errorf("read working source %s: %w", file.Path, readErr)
		}
		newSource = content
	}
	return oldSource, newSource, nil
}

func detectBase(ctx context.Context, runner Runner, root string) string {
	if ref, err := git(ctx, runner, root, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil {
		return strings.TrimSpace(ref)
	}
	for _, candidate := range []string{"origin/main", "main", "origin/master", "master"} {
		if _, err := git(ctx, runner, root, "rev-parse", "--verify", candidate+"^{commit}"); err == nil {
			return candidate
		}
	}
	return "HEAD^"
}

func git(ctx context.Context, runner Runner, dir string, args ...string) (string, error) {
	stdout, _, err := runner.Run(ctx, dir, args...)
	return stdout, err
}

func gitDiffNoIndex(ctx context.Context, runner Runner, root, path string) (string, error) {
	stdout, exitCode, err := runner.Run(ctx, root, "diff", "--no-index", "--no-color", "--unified=3", "--", os.DevNull, path)
	if err != nil && exitCode != 1 {
		return "", fmt.Errorf("diff untracked file %s: %w", path, err)
	}
	return stdout, nil
}

func (r *Repository) commandRunner() Runner {
	if r.runner != nil {
		return r.runner
	}
	return ExecRunner{}
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
