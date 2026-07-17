package ui

import (
	"path/filepath"

	"github.com/TenaciousMaker/revui/internal/gitrepo"
)

type testContext interface {
	Helper()
	TempDir() string
}

// newTestModel prevents UI tests from reading or mutating the user's global
// preferences. Tests that need a specific preference file can still provide
// PreferencesPath explicitly.
func newTestModel(t testContext, repo *gitrepo.Repository) (Model, error) {
	t.Helper()
	if repo.PreferencesPath == "" {
		repo.PreferencesPath = filepath.Join(t.TempDir(), "preferences.json")
	}
	return New(repo)
}
