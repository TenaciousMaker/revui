//go:build darwin && cgo

package watcher

import (
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsevents"
)

type fseventsBackend struct {
	root   string
	stream *fsevents.EventStream
	events chan platformEvent
	done   chan struct{}
	once   sync.Once
}

func newPlatformWatcher(root string, _ []string) (platformWatcher, error) {
	nativeEvents := make(chan []fsevents.Event, 16)
	stream := &fsevents.EventStream{
		Events:  nativeEvents,
		Paths:   []string{root},
		Flags:   fsevents.FileEvents | fsevents.NoDefer | fsevents.WatchRoot,
		Latency: 100 * time.Millisecond,
	}
	if err := stream.Start(); err != nil {
		return nil, err
	}
	backend := &fseventsBackend{root: root, stream: stream, events: make(chan platformEvent, 1), done: make(chan struct{})}
	go backend.run(nativeEvents)
	return backend, nil
}

func (w *fseventsBackend) Events() <-chan platformEvent { return w.events }

func (w *fseventsBackend) Close() error {
	w.once.Do(func() {
		// Keep the receiver alive until Stop has quiesced native callbacks;
		// the FSEvents binding delivers through a blocking channel send.
		w.stream.Stop()
		close(w.done)
	})
	return nil
}

func (w *fseventsBackend) run(nativeEvents <-chan []fsevents.Event) {
	defer close(w.events)
	for {
		select {
		case <-w.done:
			return
		case batch, ok := <-nativeEvents:
			if !ok {
				return
			}
			for _, event := range batch {
				path := filepath.Clean(event.Path)
				if path == w.root && event.Flags&(fsevents.MustScanSubDirs|fsevents.RootChanged|fsevents.KernelDropped|fsevents.UserDropped) == 0 {
					// File-level streams also report pending metadata changes for
					// the watched root. Child events carry the actionable path;
					// accepting this ancestor event would make .git writes look
					// like working-tree changes.
					continue
				}
				select {
				case w.events <- platformEvent{path: path}:
				default:
				}
			}
		}
	}
}
