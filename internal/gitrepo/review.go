package gitrepo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/TenaciousMaker/revui/internal/diff"
)

// SourceSnapshot is a text file state captured at one review instant.
// Available distinguishes binary/legacy states from valid empty content.
type SourceSnapshot struct {
	Content   []byte
	Exists    bool
	Available bool
}

// CaptureReviewSourceContext reads the working-tree side of a changed file.
// Deleted files are represented as an available, non-existent empty source;
// binary files remain reviewable but do not expose a textual baseline.
func (r *Repository) CaptureReviewSourceContext(ctx context.Context, file diff.File) (SourceSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return SourceSnapshot{}, err
	}
	if file.Binary {
		return SourceSnapshot{Exists: file.Status != "D"}, nil
	}
	if file.Status == "D" {
		return SourceSnapshot{Content: []byte{}, Available: true}, nil
	}
	content, err := os.ReadFile(filepath.Join(r.Root, filepath.FromSlash(file.Path)))
	if err != nil {
		return SourceSnapshot{}, fmt.Errorf("capture review source %s: %w", file.Path, err)
	}
	return SourceSnapshot{Content: content, Exists: true, Available: true}, nil
}

// DiffFromReviewContext compares a captured review baseline with the current
// working-tree side while preserving Git-compatible hunks and line numbers.
func (r *Repository) DiffFromReviewContext(ctx context.Context, file diff.File, before SourceSnapshot) (diff.File, SourceSnapshot, error) {
	current, err := r.CaptureReviewSourceContext(ctx, file)
	if err != nil {
		return diff.File{}, SourceSnapshot{}, err
	}
	if !before.Available || !current.Available {
		return diff.File{}, current, fmt.Errorf("text comparison unavailable for %s", file.Path)
	}
	delta, err := r.diffSourceSnapshots(ctx, file.Path, before, current)
	return delta, current, err
}

func (r *Repository) diffSourceSnapshots(ctx context.Context, path string, before, after SourceSnapshot) (diff.File, error) {
	workspace, err := os.MkdirTemp("", "revui-review-*")
	if err != nil {
		return diff.File{}, fmt.Errorf("create review comparison: %w", err)
	}
	defer func() { _ = os.RemoveAll(workspace) }()

	extension := filepath.Ext(path)
	beforePath, afterPath := os.DevNull, os.DevNull
	if before.Exists {
		beforePath = filepath.Join(workspace, "before"+extension)
		if err := os.WriteFile(beforePath, before.Content, 0o600); err != nil {
			return diff.File{}, fmt.Errorf("write review baseline: %w", err)
		}
	}
	if after.Exists {
		afterPath = filepath.Join(workspace, "after"+extension)
		if err := os.WriteFile(afterPath, after.Content, 0o600); err != nil {
			return diff.File{}, fmt.Errorf("write current review source: %w", err)
		}
	}

	stdout, exitCode, runErr := r.commandRunner().Run(ctx, r.Root, "diff", "--no-index", "--no-ext-diff", "--no-color", "--unified=3", "--", beforePath, afterPath)
	if runErr != nil && exitCode != 1 {
		return diff.File{}, fmt.Errorf("compare with last review: %w", runErr)
	}
	files, err := diff.Parse(stdout)
	if err != nil {
		return diff.File{}, fmt.Errorf("parse review comparison: %w", err)
	}
	if len(files) == 0 {
		return diff.File{OldPath: path, Path: path, Status: "M"}, nil
	}
	delta := files[0]
	delta.OldPath, delta.Path = path, path
	switch {
	case !before.Exists && after.Exists:
		delta.Status = "A"
	case before.Exists && !after.Exists:
		delta.Status = "D"
	default:
		delta.Status = "M"
	}
	return delta, nil
}
