package review

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const Version = 1

type Anchor struct {
	Path      string `json:"path,omitempty"`
	OldStart  int    `json:"old_start,omitempty"`
	OldEnd    int    `json:"old_end,omitempty"`
	NewStart  int    `json:"new_start,omitempty"`
	NewEnd    int    `json:"new_end,omitempty"`
	Context   string `json:"context,omitempty"`
	WholeRepo bool   `json:"whole_repo,omitempty"`
}

type Comment struct {
	ID        string    `json:"id"`
	Body      string    `json:"body"`
	Anchor    Anchor    `json:"anchor"`
	Resolved  bool      `json:"resolved"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Session struct {
	Version  int       `json:"version"`
	Branch   string    `json:"branch"`
	Base     string    `json:"base"`
	Comments []Comment `json:"comments"`
	Updated  time.Time `json:"updated_at"`
}

type Store struct{ Path string }

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
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	session.Version = Version
	session.Updated = time.Now().UTC()
	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".review-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func NewComment(body string, anchor Anchor) Comment {
	var bytes [8]byte
	_, _ = rand.Read(bytes[:])
	now := time.Now().UTC()
	return Comment{ID: hex.EncodeToString(bytes[:]), Body: body, Anchor: anchor, CreatedAt: now, UpdatedAt: now}
}

func (s *Session) Upsert(comment Comment) {
	comment.UpdatedAt = time.Now().UTC()
	for i := range s.Comments {
		if s.Comments[i].ID == comment.ID {
			s.Comments[i] = comment
			return
		}
	}
	s.Comments = append(s.Comments, comment)
}

func (s *Session) Delete(id string) {
	for i := range s.Comments {
		if s.Comments[i].ID == id {
			s.Comments = append(s.Comments[:i], s.Comments[i+1:]...)
			return
		}
	}
}

func (s Session) Unresolved() []Comment {
	comments := make([]Comment, 0, len(s.Comments))
	for _, comment := range s.Comments {
		if !comment.Resolved {
			comments = append(comments, comment)
		}
	}
	sort.SliceStable(comments, func(i, j int) bool {
		if comments[i].Anchor.Path != comments[j].Anchor.Path {
			return comments[i].Anchor.Path < comments[j].Anchor.Path
		}
		return anchorLine(comments[i].Anchor) < anchorLine(comments[j].Anchor)
	})
	return comments
}

func anchorLine(anchor Anchor) int {
	if anchor.NewStart > 0 {
		return anchor.NewStart
	}
	return anchor.OldStart
}
