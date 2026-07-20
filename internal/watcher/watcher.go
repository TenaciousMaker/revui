package watcher

import (
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const debounceDelay = 250 * time.Millisecond

type Event struct{ Err error }

type platformEvent struct {
	path string
	err  error
}

type platformWatcher interface {
	Events() <-chan platformEvent
	Close() error
}

type Watcher struct {
	root   string
	inner  platformWatcher
	events chan Event
	done   chan struct{}
	once   sync.Once
}

func New(root string, paths []string) (*Watcher, error) {
	root = filepath.Clean(root)
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	root = resolvedRoot
	inner, err := newPlatformWatcher(root, paths)
	if err != nil {
		return nil, err
	}
	w := &Watcher{root: root, inner: inner, events: make(chan Event, 1), done: make(chan struct{})}
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

	platformEvents := w.inner.Events()
	for {
		select {
		case <-w.done:
			return
		case event, ok := <-platformEvents:
			if !ok {
				return
			}
			if event.err != nil {
				w.send(Event{Err: event.err})
				continue
			}
			if ignoredPath(w.root, event.path) {
				continue
			}
			reset()
		case <-timerC:
			timerC = nil
			w.send(Event{})
		}
	}
}

func (w *Watcher) send(event Event) {
	select {
	case w.events <- event:
	default:
	}
}

func ignoredPath(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return true
	}
	return relative == ".git" || strings.HasPrefix(relative, ".git"+string(filepath.Separator))
}

func withinRoot(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
