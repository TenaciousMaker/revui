package gitrepo

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestSearchFindsTrackedAndUntrackedTextWithContext(t *testing.T) {
	root := t.TempDir()
	run(t, root, "git", "init", "-b", "main")
	run(t, root, "git", "config", "user.email", "revui@example.test")
	run(t, root, "git", "config", "user.name", "Revui Test")
	run(t, root, "git", "config", "commit.gpgsign", "false")
	write(t, filepath.Join(root, ".gitignore"), "ignored.go\n")
	write(t, filepath.Join(root, "service.go"), "package service\n\nfunc SharedMethod() {}\n\nfunc Call() { SharedMethod() }\n")
	run(t, root, "git", "add", ".gitignore", "service.go")
	run(t, root, "git", "commit", "-m", "base")
	write(t, filepath.Join(root, "new.go"), "package service\n\nfunc NewCall() { SharedMethod() }\n")
	write(t, filepath.Join(root, "ignored.go"), "func Ignored() { SharedMethod() }\n")

	repo := &Repository{Root: root}
	matches, err := repo.Search("SharedMethod", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 3 {
		t.Fatalf("got %d matches, want 3: %#v", len(matches), matches)
	}
	seen := map[string]int{}
	for _, match := range matches {
		seen[match.Path]++
		if len(match.Context) < 2 {
			t.Fatalf("match has no surrounding context: %#v", match)
		}
		matchLines := 0
		for _, line := range match.Context {
			if line.Match {
				matchLines++
				if line.Number != match.Line {
					t.Fatalf("context match line %d does not equal result line %d", line.Number, match.Line)
				}
			}
		}
		if matchLines != 1 {
			t.Fatalf("context has %d selected lines, want 1: %#v", matchLines, match.Context)
		}
	}
	if seen["service.go"] != 2 || seen["new.go"] != 1 || seen["ignored.go"] != 0 {
		t.Fatalf("unexpected search paths: %#v", seen)
	}
}

func TestSearchReturnsNoMatchesWithoutError(t *testing.T) {
	root := t.TempDir()
	run(t, root, "git", "init", "-b", "main")
	write(t, filepath.Join(root, "README.md"), "hello\n")
	run(t, root, "git", "add", "README.md")

	matches, err := (&Repository{Root: root}).Search("not present", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("got unexpected matches: %#v", matches)
	}
}

func BenchmarkRepositorySearch(b *testing.B) {
	root := b.TempDir()
	run(b, root, "git", "init", "-b", "main")
	for index := 0; index < 250; index++ {
		write(b, filepath.Join(root, fmt.Sprintf("pkg%03d.go", index)), fmt.Sprintf("package fixture\n\nfunc value%d() string { return \"review needle %d\" }\n", index, index))
	}
	run(b, root, "git", "add", ".")
	repo := &Repository{Root: root}
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := repo.Search("review needle", 2); err != nil {
			b.Fatal(err)
		}
	}
}
