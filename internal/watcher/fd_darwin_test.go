//go:build darwin && cgo

package watcher

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestWatcherKeepsDarwinFileDescriptorUseBounded(t *testing.T) {
	root := t.TempDir()
	var paths []string
	for directory := 0; directory < 24; directory++ {
		relativeDirectory := fmt.Sprintf("src/package-%02d", directory)
		if err := os.MkdirAll(filepath.Join(root, relativeDirectory), 0o700); err != nil {
			t.Fatal(err)
		}
		for file := 0; file < 8; file++ {
			relative := filepath.Join(relativeDirectory, fmt.Sprintf("file-%02d.go", file))
			if err := os.WriteFile(filepath.Join(root, relative), []byte("package fixture\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			paths = append(paths, relative)
		}
	}

	before := openDescriptorCount()
	w, err := New(root, paths)
	if err != nil {
		t.Fatal(err)
	}
	closed := false
	t.Cleanup(func() {
		if !closed {
			_ = w.Close()
		}
	})
	after := openDescriptorCount()
	if added := after - before; added > 32 {
		t.Fatalf("watching %d files opened %d descriptors; want at most 32", len(paths), added)
	}

	for iteration := 0; iteration < 100; iteration++ {
		path := filepath.Join(root, paths[iteration%len(paths)])
		if err := os.WriteFile(path, []byte(fmt.Sprintf("package fixture // %d\n", iteration)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case event := <-w.Events():
		if event.Err != nil {
			t.Fatal(event.Err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not report the edit burst")
	}
	if added := openDescriptorCount() - before; added > 32 {
		t.Fatalf("edit burst grew watcher to %d descriptors; want at most 32", added)
	}

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	closed = true
	deadline := time.Now().Add(time.Second)
	for openDescriptorCount() > before+4 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if remaining := openDescriptorCount() - before; remaining > 4 {
		t.Fatalf("closing watcher left %d descriptors open; want at most 4", remaining)
	}
}

func openDescriptorCount() int {
	count := 0
	for descriptor := 0; descriptor < 4096; descriptor++ {
		if _, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(descriptor), uintptr(syscall.F_GETFD), 0); errno == 0 {
			count++
		}
	}
	return count
}
