package review

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const Version = 1

type Session struct {
	Version  int               `json:"version"`
	Branch   string            `json:"branch"`
	Base     string            `json:"base"`
	Reviewed map[string]string `json:"reviewed_files,omitempty"`
	Updated  time.Time         `json:"updated_at"`
}

func (s Session) IsReviewed(path, fingerprint string) bool {
	return fingerprint != "" && s.Reviewed[path] == fingerprint
}

func (s *Session) ToggleReviewed(path, fingerprint string) bool {
	if s.Reviewed == nil {
		s.Reviewed = map[string]string{}
	}
	if s.IsReviewed(path, fingerprint) {
		delete(s.Reviewed, path)
		return false
	}
	s.Reviewed[path] = fingerprint
	return true
}

func Load(path, branch, base string) (Session, error) {
	s := Session{Version: Version, Branch: branch, Base: base}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return s, fmt.Errorf("read review session: %w", err)
	}
	if s.Version != Version {
		return s, fmt.Errorf("unsupported review version %d", s.Version)
	}
	return s, nil
}

func Save(path string, session Session) error {
	session.Version = Version
	session.Updated = time.Now().UTC()
	return saveJSON(path, ".review-*.json", session)
}

func saveJSON(path, pattern string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), pattern)
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
