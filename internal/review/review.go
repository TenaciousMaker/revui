package review

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const Version = 2

type Status uint8

const (
	Unreviewed Status = iota
	Reviewed
	ChangedSinceReview
)

// FileReview is the exact working-tree state a reviewer last accepted. Source
// is nil when a legacy session or binary file has no textual baseline; an
// empty non-nil slice represents an empty or deleted text file.
type FileReview struct {
	Fingerprint string `json:"fingerprint"`
	Source      []byte `json:"source"`
	Exists      bool   `json:"exists"`
}

type Session struct {
	Version  int                   `json:"version"`
	Branch   string                `json:"branch"`
	Base     string                `json:"base"`
	Reviewed map[string]FileReview `json:"reviewed_files,omitempty"`
	Updated  time.Time             `json:"updated_at"`
}

func (s Session) IsReviewed(path, fingerprint string) bool {
	return s.Status(path, fingerprint) == Reviewed
}

func (s Session) Status(path, fingerprint string) Status {
	reviewed, ok := s.Reviewed[path]
	if !ok || reviewed.Fingerprint == "" || fingerprint == "" {
		return Unreviewed
	}
	if reviewed.Fingerprint == fingerprint {
		return Reviewed
	}
	return ChangedSinceReview
}

func (s Session) Baseline(path, currentFingerprint string) (FileReview, bool) {
	if s.Status(path, currentFingerprint) != ChangedSinceReview {
		return FileReview{}, false
	}
	reviewed := s.Reviewed[path]
	if reviewed.Source == nil {
		return FileReview{}, false
	}
	reviewed.Source = append([]byte(nil), reviewed.Source...)
	return reviewed, true
}

func (s *Session) SetReviewed(path, fingerprint string, source []byte, exists bool) {
	if path == "" || fingerprint == "" {
		return
	}
	if s.Reviewed == nil {
		s.Reviewed = map[string]FileReview{}
	}
	var snapshot []byte
	if source != nil {
		snapshot = append([]byte{}, source...)
	}
	s.Reviewed[path] = FileReview{Fingerprint: fingerprint, Source: snapshot, Exists: exists}
}

func (s *Session) Unreview(path string) { delete(s.Reviewed, path) }

// ToggleReviewed remains for embedded callers that only track fingerprints.
// New UI code uses SetReviewed so it can retain a comparison baseline.
func (s *Session) ToggleReviewed(path, fingerprint string) bool {
	if s.IsReviewed(path, fingerprint) {
		s.Unreview(path)
		return false
	}
	s.SetReviewed(path, fingerprint, nil, true)
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
	var header struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return s, fmt.Errorf("read review session: %w", err)
	}
	switch header.Version {
	case 1:
		var legacy struct {
			Version  int               `json:"version"`
			Branch   string            `json:"branch"`
			Base     string            `json:"base"`
			Reviewed map[string]string `json:"reviewed_files,omitempty"`
			Updated  time.Time         `json:"updated_at"`
		}
		if err := json.Unmarshal(data, &legacy); err != nil {
			return s, fmt.Errorf("read review session: %w", err)
		}
		s = Session{Version: Version, Branch: legacy.Branch, Base: legacy.Base, Updated: legacy.Updated}
		for path, fingerprint := range legacy.Reviewed {
			s.SetReviewed(path, fingerprint, nil, true)
		}
	case Version:
		if err := json.Unmarshal(data, &s); err != nil {
			return s, fmt.Errorf("read review session: %w", err)
		}
	default:
		return s, fmt.Errorf("unsupported review version %d", header.Version)
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
