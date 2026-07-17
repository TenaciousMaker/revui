package ui

import (
	"github.com/TenaciousMaker/revui/internal/config"
	"github.com/TenaciousMaker/revui/internal/review"
)

// preferenceStore is the seam for user-wide display choices.
type preferenceStore interface {
	UserPath() (string, error)
	LoadWithFallback(path, fallback string) (config.Preferences, error)
	Save(path string, preferences config.Preferences) error
}

type filePreferenceStore struct{}

func (filePreferenceStore) UserPath() (string, error) { return config.UserPath() }
func (filePreferenceStore) LoadWithFallback(path, fallback string) (config.Preferences, error) {
	return config.LoadWithFallback(path, fallback)
}
func (filePreferenceStore) Save(path string, preferences config.Preferences) error {
	return config.Save(path, preferences)
}

// reviewStore is the seam for repository-local reviewed-file progress.
type reviewStore interface {
	Load(path, branch, base string) (review.Session, error)
	Save(path string, session review.Session) error
}

type fileReviewStore struct{}

func (fileReviewStore) Load(path, branch, base string) (review.Session, error) {
	return review.Load(path, branch, base)
}
func (fileReviewStore) Save(path string, session review.Session) error {
	return review.Save(path, session)
}
