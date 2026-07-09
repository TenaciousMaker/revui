package agent

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/mattwalker/revui/internal/review"
)

const DefaultCommand = "codex exec --sandbox workspace-write -"

func Command() string {
	if configured := strings.TrimSpace(os.Getenv("REVUI_AGENT_COMMAND")); configured != "" {
		return configured
	}
	return DefaultCommand
}

func Prompt(branch, base string, comments []review.Comment) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are addressing a local pre-PR review of branch %q against %q.\n", branch, base)
	b.WriteString("Make the requested changes directly in this repository. Inspect surrounding code before editing, preserve unrelated work, and run focused verification. Do not create a commit, push, open a PR, or post comments anywhere. Do not mark review comments resolved; the reviewer will do that after inspecting the refreshed diff.\n\n")
	b.WriteString("Unresolved review comments:\n")
	for i, comment := range comments {
		if comment.Anchor.WholeRepo {
			fmt.Fprintf(&b, "\n%d. [whole review] %s\n", i+1, comment.Body)
			continue
		}
		line := comment.Anchor.NewStart
		if line == 0 {
			line = comment.Anchor.OldStart
		}
		end := comment.Anchor.NewEnd
		if end == 0 {
			end = comment.Anchor.OldEnd
		}
		location := fmt.Sprintf("%s:%d", comment.Anchor.Path, line)
		if end > line {
			location = fmt.Sprintf("%s-%d", location, end)
		}
		fmt.Fprintf(&b, "\n%d. [%s] %s\n", i+1, location, comment.Body)
		if comment.Anchor.Context != "" {
			fmt.Fprintf(&b, "   Context: %s\n", comment.Anchor.Context)
		}
	}
	return b.String()
}

func Run(ctx context.Context, root, command, prompt string) (string, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = root
	cmd.Stdin = strings.NewReader(prompt)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	err := cmd.Run()
	return strings.TrimSpace(output.String()), err
}
