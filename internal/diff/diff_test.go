package diff

import "testing"

func TestParseModifiedFile(t *testing.T) {
	input := `diff --git a/internal/example.go b/internal/example.go
index 1234567..89abcde 100644
--- a/internal/example.go
+++ b/internal/example.go
@@ -10,3 +10,4 @@ func example() {
     before()
-	oldCall()
+	newCall()
+	verify()
     after()
`
	files, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1", len(files))
	}
	file := files[0]
	if file.Path != "internal/example.go" || file.Status != "M" {
		t.Fatalf("unexpected file: %#v", file)
	}
	if file.Additions != 2 || file.Deletions != 1 {
		t.Fatalf("got +%d -%d, want +2 -1", file.Additions, file.Deletions)
	}
	if got := file.Lines[2]; got.Kind != Deletion || got.OldNumber != 11 || got.NewNumber != 0 {
		t.Fatalf("unexpected deletion: %#v", got)
	}
	if got := file.Lines[3]; got.Kind != Addition || got.NewNumber != 11 || got.OldNumber != 0 {
		t.Fatalf("unexpected addition: %#v", got)
	}
}

func TestParseRenameAndBinary(t *testing.T) {
	input := `diff --git a/old.png b/new.png
similarity index 100%
rename from old.png
rename to new.png
Binary files a/old.png and b/new.png differ
`
	files, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Status != "R" || !files[0].Binary {
		t.Fatalf("unexpected binary rename: %#v", files)
	}
}

func TestParseRejectsInvalidHunk(t *testing.T) {
	_, err := Parse("diff --git a/a b/a\n@@ broken @@\n")
	if err == nil {
		t.Fatal("expected invalid hunk error")
	}
}
