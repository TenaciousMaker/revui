//go:build darwin && !cgo

package watcher

import "errors"

func newPlatformWatcher(string, []string) (platformWatcher, error) {
	return nil, errors.New("macOS realtime refresh requires a CGO-enabled build")
}
