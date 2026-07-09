package gitrepo

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestOpenIncludesCommittedWorkingAndUntrackedChanges(t *testing.T) {
	root := t.TempDir()
	run(t, root, "git", "init", "-b", "main")
	run(t, root, "git", "config", "user.email", "revui@example.test")
	run(t, root, "git", "config", "user.name", "Revui Test")
	run(t, root, "git", "config", "commit.gpgsign", "false")
	write(t, filepath.Join(root, "app.go"), "package app\n\nfunc Name() string { return \"main\" }\n")
	run(t, root, "git", "add", "app.go")
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
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(repo.ReviewPath) != filepath.Join(realRoot, ".git", "revui") {
		t.Fatalf("review path is not Git-local: %s", repo.ReviewPath)
	}
}

func TestOpenOutsideRepository(t *testing.T) {
	_, err := Open(t.TempDir(), "")
	if err == nil {
		t.Fatal("expected an error outside a Git repository")
	}
}

func run(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, output)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
