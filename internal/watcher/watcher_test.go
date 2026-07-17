package watcher

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatcherDebouncesRepositoryChanges(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "src", "service.go")
	if err := os.WriteFile(path, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	w, err := New(root, []string{"src/service.go"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })

	for _, content := range []string{"two", "three", "four"} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case event := <-w.Events():
		if event.Err != nil {
			t.Fatal(event.Err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not report a repository edit")
	}
	select {
	case event := <-w.Events():
		t.Fatalf("save burst produced an extra event: %#v", event)
	case <-time.After(2 * debounceDelay):
	}
}

func TestWatcherNoticesNewDirectoryAndIgnoresGitMetadata(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git", "revui"), 0o700); err != nil {
		t.Fatal(err)
	}
	w, err := New(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })

	if err := os.WriteFile(filepath.Join(root, ".git", "revui", "review.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-w.Events():
		t.Fatalf("Git-local review state triggered watcher: %#v", event)
	case <-time.After(2 * debounceDelay):
	}

	if err := os.MkdirAll(filepath.Join(root, "new", "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "new", "nested", "file.go"), []byte("package nested"), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-w.Events():
		if event.Err != nil {
			t.Fatal(event.Err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not report a newly created directory")
	}
}
