package review

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoadAndUpdateSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "review.json")
	session := Session{Branch: "feature/review", Base: "main"}
	session.SetReviewed("main.go", "diff-v1", []byte("package main\n"), true)
	if session.Status("main.go", "diff-v1") != Reviewed {
		t.Fatal("file was not marked reviewed")
	}
	if err := Save(path, session); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path, "ignored", "ignored")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Branch != "feature/review" || loaded.Status("main.go", "diff-v1") != Reviewed {
		t.Fatalf("unexpected session: %#v", loaded)
	}
	if loaded.Status("main.go", "diff-v2") != ChangedSinceReview {
		t.Fatal("changed fingerprint did not retain its prior review baseline")
	}
	baseline, ok := loaded.Baseline("main.go", "diff-v2")
	if !ok || !baseline.Exists || string(baseline.Source) != "package main\n" {
		t.Fatalf("review baseline = %#v, %v", baseline, ok)
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

func TestLoadMigratesVersionOneFingerprintsWithoutInventingBaselines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "review.json")
	legacy := `{"version":1,"branch":"feature","base":"main","reviewed_files":{"main.go":"diff-v1"}}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	session, err := Load(path, "feature", "main")
	if err != nil {
		t.Fatal(err)
	}
	if session.Version != Version || session.Status("main.go", "diff-v1") != Reviewed {
		t.Fatalf("legacy session was not migrated: %#v", session)
	}
	if _, ok := session.Baseline("main.go", "diff-v2"); ok {
		t.Fatal("legacy fingerprint unexpectedly gained a source baseline")
	}
}

func TestUnreviewRemovesFingerprintAndBaselineTogether(t *testing.T) {
	session := Session{}
	session.SetReviewed("main.go", "diff-v1", []byte("one\n"), true)
	session.Unreview("main.go")
	if session.Status("main.go", "diff-v1") != Unreviewed {
		t.Fatalf("file remained reviewed: %#v", session)
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
