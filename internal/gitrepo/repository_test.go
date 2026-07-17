package gitrepo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TenaciousMaker/revui/internal/diff"
)

func TestOpenIncludesCommittedWorkingAndUntrackedChanges(t *testing.T) {
	root := t.TempDir()
	run(t, root, "git", "init", "-b", "main")
	run(t, root, "git", "config", "user.email", "revui@example.test")
	run(t, root, "git", "config", "user.name", "Revui Test")
	run(t, root, "git", "config", "commit.gpgsign", "false")
	write(t, filepath.Join(root, "app.go"), "package app\n\nfunc Name() string { return \"main\" }\n")
	write(t, filepath.Join(root, "context.go"), "package app\n\nfunc Unchanged() {}\n")
	run(t, root, "git", "add", "app.go", "context.go")
	run(t, root, "git", "commit", "-m", "base")
	run(t, root, "git", "switch", "-c", "feature")
	write(t, filepath.Join(root, "app.go"), "package app\n\nfunc Name() string { return \"feature\" }\n")
	run(t, root, "git", "add", "app.go")
	write(t, filepath.Join(root, "app.go"), "package app\n\nfunc Name() string { return \"working tree\" }\n")
	write(t, filepath.Join(root, "new.go"), "package app\n")

	repo, err := Open(root, "main")
	if err != nil {
		t.Fatal(err)
	}
	if repo.Branch != "feature" || repo.Base != "main" {
		t.Fatalf("unexpected branch comparison: %s -> %s", repo.Branch, repo.Base)
	}
	seen := map[string]bool{}
	for _, file := range repo.Files {
		seen[file.Path] = true
	}
	for _, path := range []string{"app.go", "new.go"} {
		if !seen[path] {
			t.Fatalf("missing %s from changed files: %#v", path, repo.Paths())
		}
	}
	all := map[string]bool{}
	for _, path := range repo.AllPaths {
		all[path] = true
	}
	for _, path := range []string{"app.go", "context.go", "new.go"} {
		if !all[path] {
			t.Fatalf("missing %s from all repository paths: %#v", path, repo.AllPaths)
		}
	}
	content, fromBase, err := repo.ReadSource("app.go")
	if err != nil || fromBase || !strings.Contains(string(content), "working tree") {
		t.Fatalf("working source content=%q fromBase=%v err=%v", content, fromBase, err)
	}
	var appFile diff.File
	for _, file := range repo.Files {
		if file.Path == "app.go" {
			appFile = file
		}
	}
	oldSource, newSource, err := repo.ReadPairContext(context.Background(), appFile)
	if err != nil || !strings.Contains(string(oldSource), `"main"`) || !strings.Contains(string(newSource), `"working tree"`) {
		t.Fatalf("source pair old=%q new=%q err=%v", oldSource, newSource, err)
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(repo.ReviewPath) != filepath.Join(realRoot, ".git", "revui") {
		t.Fatalf("review path is not Git-local: %s", repo.ReviewPath)
	}
	if repo.PreferencesPath != "" {
		t.Fatalf("repository unexpectedly owns user preferences: %s", repo.PreferencesPath)
	}
}

func TestReadSourceFallsBackToMergeBaseForDeletedFile(t *testing.T) {
	root := t.TempDir()
	run(t, root, "git", "init", "-b", "main")
	run(t, root, "git", "config", "user.email", "revui@example.test")
	run(t, root, "git", "config", "user.name", "Revui Test")
	run(t, root, "git", "config", "commit.gpgsign", "false")
	write(t, filepath.Join(root, "deleted.go"), "package deleted\n\nfunc Before() {}\n")
	run(t, root, "git", "add", "deleted.go")
	run(t, root, "git", "commit", "-m", "base")
	run(t, root, "git", "switch", "-c", "feature")
	if err := os.Remove(filepath.Join(root, "deleted.go")); err != nil {
		t.Fatal(err)
	}

	repo, err := Open(root, "main")
	if err != nil {
		t.Fatal(err)
	}
	content, fromBase, err := repo.ReadSource("deleted.go")
	if err != nil || !fromBase || !strings.Contains(string(content), "func Before") {
		t.Fatalf("deleted source content=%q fromBase=%v err=%v", content, fromBase, err)
	}
}

func TestOpenOutsideRepository(t *testing.T) {
	_, err := Open(t.TempDir(), "")
	if err == nil {
		t.Fatal("expected an error outside a Git repository")
	}
}

func TestOpenHandlesRenameBinaryWhitespaceAndNoChanges(t *testing.T) {
	root := t.TempDir()
	run(t, root, "git", "init", "-b", "main")
	run(t, root, "git", "config", "user.email", "revui@example.test")
	run(t, root, "git", "config", "user.name", "Revui Test")
	run(t, root, "git", "config", "commit.gpgsign", "false")
	write(t, filepath.Join(root, "old name.txt"), "before\n")
	if err := os.WriteFile(filepath.Join(root, "image.bin"), []byte{0, 1, 2, 3}, 0o600); err != nil {
		t.Fatal(err)
	}
	run(t, root, "git", "add", ".")
	run(t, root, "git", "commit", "-m", "base")

	clean, err := Open(root, "main")
	if err != nil || len(clean.Files) != 0 {
		t.Fatalf("clean snapshot files=%v err=%v", clean.Paths(), err)
	}

	run(t, root, "git", "switch", "-c", "feature")
	run(t, root, "git", "mv", "old name.txt", "new name.txt")
	if err := os.WriteFile(filepath.Join(root, "image.bin"), []byte{0, 9, 8, 7}, 0o600); err != nil {
		t.Fatal(err)
	}
	repo, err := Open(root, "main")
	if err != nil {
		t.Fatal(err)
	}
	seenRename, seenBinary := false, false
	for _, file := range repo.Files {
		seenRename = seenRename || file.Path == "new name.txt" && file.OldPath == "old name.txt"
		seenBinary = seenBinary || file.Path == "image.bin" && file.Binary
	}
	if !seenRename || !seenBinary {
		t.Fatalf("rename=%v binary=%v files=%#v", seenRename, seenBinary, repo.Files)
	}
}

func TestOpenDetachedHeadAndInvalidBase(t *testing.T) {
	root := t.TempDir()
	run(t, root, "git", "init", "-b", "main")
	run(t, root, "git", "config", "user.email", "revui@example.test")
	run(t, root, "git", "config", "user.name", "Revui Test")
	run(t, root, "git", "config", "commit.gpgsign", "false")
	write(t, filepath.Join(root, "README.md"), "base\n")
	run(t, root, "git", "add", ".")
	run(t, root, "git", "commit", "-m", "base")
	run(t, root, "git", "checkout", "--detach")
	repo, err := Open(root, "HEAD")
	if err != nil || repo.Branch == "" || repo.Branch == "main" {
		t.Fatalf("detached branch=%q err=%v", repo.Branch, err)
	}
	if _, err := Open(root, "does-not-exist"); err == nil || !strings.Contains(err.Error(), "merge base") {
		t.Fatalf("invalid base error=%v", err)
	}
}

func TestOpenAndSearchRespectCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := OpenContext(ctx, t.TempDir(), "main"); err == nil {
		t.Fatal("cancelled open succeeded")
	}
	root := t.TempDir()
	run(t, root, "git", "init", "-b", "main")
	if _, err := (&Repository{Root: root}).SearchContext(ctx, "needle", 1); err == nil {
		t.Fatal("cancelled search succeeded")
	}
}

func run(t testing.TB, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, output)
	}
}

func write(t testing.TB, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
