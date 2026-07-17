package review

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoadAndUpdateSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "review.json")
	session := Session{Branch: "feature/review", Base: "main"}
	if !session.ToggleReviewed("main.go", "diff-v1") || !session.IsReviewed("main.go", "diff-v1") {
		t.Fatal("file was not marked reviewed")
	}
	if err := Save(path, session); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path, "ignored", "ignored")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Branch != "feature/review" || !loaded.IsReviewed("main.go", "diff-v1") {
		t.Fatalf("unexpected session: %#v", loaded)
	}
	if loaded.IsReviewed("main.go", "diff-v2") {
		t.Fatal("changed fingerprint remained reviewed")
	}
}

func TestLoadMissingStartsSession(t *testing.T) {
	session, err := Load(filepath.Join(t.TempDir(), "missing.json"), "feature", "main")
	if err != nil {
		t.Fatal(err)
	}
	if session.Version != Version || session.Branch != "feature" || session.Base != "main" {
		t.Fatalf("unexpected new session: %#v", session)
	}
}

func TestLoadRejectsCorruptAndUnsupportedSession(t *testing.T) {
	for name, content := range map[string]string{
		"corrupt":     "{",
		"unsupported": `{"version":99}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "review.json")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path, "feature", "main"); err == nil {
				t.Fatal("invalid review session loaded")
			}
		})
	}
}
