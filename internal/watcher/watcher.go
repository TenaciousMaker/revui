package watcher

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const debounceDelay = 250 * time.Millisecond

type Event struct{ Err error }

type Watcher struct {
	root   string
	inner  *fsnotify.Watcher
	events chan Event
	done   chan struct{}
	once   sync.Once
}

func New(root string, paths []string) (*Watcher, error) {
	inner, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w := &Watcher{root: filepath.Clean(root), inner: inner, events: make(chan Event, 1), done: make(chan struct{})}
	directories := map[string]bool{w.root: true}
	for _, path := range paths {
		directory := filepath.Dir(filepath.Join(w.root, filepath.FromSlash(path)))
		for withinRoot(w.root, directory) {
			directories[directory] = true
			if directory == w.root {
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
	go w.run()
	return w, nil
}

func (w *Watcher) Events() <-chan Event { return w.events }

func (w *Watcher) Close() error {
	var err error
	w.once.Do(func() {
		close(w.done)
		err = w.inner.Close()
	})
	return err
}

func (w *Watcher) run() {
	defer close(w.events)
	var timer *time.Timer
	var timerC <-chan time.Time
	reset := func() {
		if timer == nil {
			timer = time.NewTimer(debounceDelay)
		} else {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(debounceDelay)
		}
		timerC = timer.C
	}
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()

	for {
		select {
		case <-w.done:
			return
		case err, ok := <-w.inner.Errors:
			if !ok {
				return
			}
			w.send(Event{Err: err})
		case event, ok := <-w.inner.Events:
			if !ok {
				return
			}
			if w.ignored(event.Name) || event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) == 0 {
				continue
			}
			if event.Op&fsnotify.Create != 0 {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					w.addDirectoryTree(event.Name)
				}
			}
			reset()
		case <-timerC:
			timerC = nil
			w.send(Event{})
		}
	}
}

func (w *Watcher) addDirectoryTree(root string) {
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			return nil
		}
		if w.ignored(path) {
			return filepath.SkipDir
		}
		_ = w.inner.Add(path)
		return nil
	})
}

func (w *Watcher) ignored(path string) bool {
	relative, err := filepath.Rel(w.root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return true
	}
	return relative == ".git" || strings.HasPrefix(relative, ".git"+string(filepath.Separator))
}

func (w *Watcher) send(event Event) {
	select {
	case w.events <- event:
	default:
	}
}

func withinRoot(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
