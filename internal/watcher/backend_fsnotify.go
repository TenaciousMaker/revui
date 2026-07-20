//go:build !darwin

package watcher

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
)

type fsnotifyBackend struct {
	root   string
	inner  *fsnotify.Watcher
	events chan platformEvent
	done   chan struct{}
	once   sync.Once
}

func newPlatformWatcher(root string, paths []string) (platformWatcher, error) {
	inner, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	backend := &fsnotifyBackend{
		root: root, inner: inner, events: make(chan platformEvent, 1), done: make(chan struct{}),
	}
	directories := map[string]bool{root: true}
	for _, path := range paths {
		directory := filepath.Dir(filepath.Join(root, filepath.FromSlash(path)))
		for withinRoot(root, directory) {
			directories[directory] = true
			if directory == root {
				break
			}
			directory = filepath.Dir(directory)
		}
	}
	for directory := range directories {
		if err := inner.Add(directory); err != nil && !errors.Is(err, fs.ErrNotExist) {
			_ = inner.Close()
			return nil, err
		}
	}
	go backend.run()
	return backend, nil
}

func (w *fsnotifyBackend) Events() <-chan platformEvent { return w.events }

func (w *fsnotifyBackend) Close() error {
	var err error
	w.once.Do(func() {
		close(w.done)
		err = w.inner.Close()
	})
	return err
}

func (w *fsnotifyBackend) run() {
	defer close(w.events)
	for {
		select {
		case <-w.done:
			return
		case err, ok := <-w.inner.Errors:
			if !ok {
				return
			}
			w.send(platformEvent{err: err})
		case event, ok := <-w.inner.Events:
			if !ok {
				return
			}
			if ignoredPath(w.root, event.Name) || event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) == 0 {
				continue
			}
			if event.Op&fsnotify.Create != 0 {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					w.addDirectoryTree(event.Name)
				}
			}
			w.send(platformEvent{path: event.Name})
		}
	}
}

func (w *fsnotifyBackend) addDirectoryTree(root string) {
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			return nil
		}
		if ignoredPath(w.root, path) {
			return filepath.SkipDir
		}
		_ = w.inner.Add(path)
		return nil
	})
}

func (w *fsnotifyBackend) send(event platformEvent) {
	select {
	case w.events <- event:
	default:
	}
}
