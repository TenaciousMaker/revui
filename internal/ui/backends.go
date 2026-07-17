package ui

import (
	"context"

	"github.com/TenaciousMaker/revui/internal/gitrepo"
	"github.com/TenaciousMaker/revui/internal/watcher"
)

// repositoryOperations is the seam between interaction state and Git-backed
// repository work. Snapshots are replaced whole after Refresh.
type repositoryOperations interface {
	Refresh(ctx context.Context, root, base string) (*gitrepo.Repository, error)
	Search(ctx context.Context, snapshot *gitrepo.Repository, query string, contextLines int) ([]gitrepo.SearchMatch, error)
	ReadSource(ctx context.Context, snapshot *gitrepo.Repository, path string) ([]byte, bool, error)
}

type gitRepositoryOperations struct{}

func (gitRepositoryOperations) Refresh(ctx context.Context, root, base string) (*gitrepo.Repository, error) {
	return gitrepo.OpenContext(ctx, root, base)
}
func (gitRepositoryOperations) Search(ctx context.Context, snapshot *gitrepo.Repository, query string, contextLines int) ([]gitrepo.SearchMatch, error) {
	return snapshot.SearchContext(ctx, query, contextLines)
}
func (gitRepositoryOperations) ReadSource(ctx context.Context, snapshot *gitrepo.Repository, path string) ([]byte, bool, error) {
	return snapshot.ReadSourceContext(ctx, path)
}

type watcherFactory interface {
	New(root string, paths []string) (repositoryWatcher, error)
}

type filesystemWatcherFactory struct{}

func (filesystemWatcherFactory) New(root string, paths []string) (repositoryWatcher, error) {
	return watcher.New(root, paths)
}
