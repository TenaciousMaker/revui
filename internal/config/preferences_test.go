package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoadAndDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "preferences.json")
	if got, err := Load(path); err != nil || got != Defaults() {
		t.Fatalf("missing preferences = %#v, %v", got, err)
	}
	want := Preferences{FileLayout: "tree", FileScope: "all", WideFiles: true, DiffView: "split", IgnoreWhitespace: true, SemanticReflow: true, NormalizedLayout: true}
	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.FileLayout != want.FileLayout || got.FileScope != want.FileScope || !got.WideFiles || got.DiffView != want.DiffView || !got.IgnoreWhitespace || !got.SemanticReflow || !got.NormalizedLayout {
		t.Fatalf("preferences = %#v, want %#v", got, want)
	}
}

func TestLoadWithFallbackMigrates(t *testing.T) {
	root := t.TempDir()
	primary := filepath.Join(root, "user", "preferences.json")
	legacy := filepath.Join(root, "repo", "preferences.json")
	want := Preferences{FileLayout: "tree", FileScope: "context", DiffView: "split"}
	if err := Save(legacy, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadWithFallback(primary, legacy)
	if err != nil || got.FileScope != "context" {
		t.Fatalf("migration = %#v, %v", got, err)
	}
	if _, err := os.Stat(primary); err != nil {
		t.Fatal(err)
	}
}

func TestCorruptPreferencesReturnDefaultsAndError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preferences.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err == nil || got != Defaults() {
		t.Fatalf("corrupt preferences = %#v, %v", got, err)
	}
}

func TestUnsupportedVersionAndSaveFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preferences.json")
	if err := os.WriteFile(path, []byte(`{"version":99}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := Load(path); err == nil || got != Defaults() {
		t.Fatalf("unsupported preferences = %#v, %v", got, err)
	}
	blockingFile := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blockingFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Save(filepath.Join(blockingFile, "preferences.json"), Defaults()); err == nil {
		t.Fatal("save below a file succeeded")
	}
	if err := Save("", Defaults()); err != nil {
		t.Fatalf("empty preference path: %v", err)
	}
}

func TestUserPath(t *testing.T) {
	path, err := UserPath()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "preferences.json" || filepath.Base(filepath.Dir(path)) != "revui" {
		t.Fatalf("unexpected user path %q", path)
	}
}
