package agent

import (
	"strings"
	"testing"

	"github.com/mattwalker/revui/internal/review"
)

func TestPromptIsLocalAndActionable(t *testing.T) {
	comments := []review.Comment{
		review.NewComment("Return the parsing error", review.Anchor{Path: "parser.go", NewStart: 42, NewEnd: 44, Context: "value, _ := parse()"}),
		review.NewComment("Run the focused tests", review.Anchor{WholeRepo: true}),
	}
	prompt := Prompt("feature/parser", "main", comments)
	for _, expected := range []string{"feature/parser", "parser.go:42-44", "Return the parsing error", "whole review", "Do not create a commit, push, open a PR, or post comments anywhere"} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("prompt does not contain %q:\n%s", expected, prompt)
		}
	}
}
