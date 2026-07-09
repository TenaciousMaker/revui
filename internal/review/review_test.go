package review

import (
	"path/filepath"
	"testing"
)

func TestSaveLoadAndUpdateSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "review.json")
	session := Session{Branch: "feature/review", Base: "main"}
	comment := NewComment("Handle the error", Anchor{Path: "main.go", NewStart: 12, NewEnd: 12})
	session.Upsert(comment)
	if err := Save(path, session); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path, "ignored", "ignored")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Branch != "feature/review" || len(loaded.Comments) != 1 {
		t.Fatalf("unexpected session: %#v", loaded)
	}
	loaded.Comments[0].Resolved = true
	loaded.Upsert(loaded.Comments[0])
	if len(loaded.Unresolved()) != 0 {
		t.Fatal("resolved comment returned as unresolved")
	}
	loaded.Delete(comment.ID)
	if len(loaded.Comments) != 0 {
		t.Fatal("comment was not deleted")
	}
}

func TestLoadMissingStartsSession(t *testing.T) {
	session, err := Load(filepath.Join(t.TempDir(), "missing.json"), "feature", "main")
	if err != nil {
		t.Fatal(err)
	}
	if session.Version != Version || session.Branch != "feature" || session.Base != "main" {
		t.Fatalf("unexpected new session: %#v", session)
	}
}
