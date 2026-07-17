package gitrepo

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const maxSearchMatches = 250

type SearchLine struct {
	Number int
	Text   string
	Match  bool
}

type SearchMatch struct {
	Path    string
	Line    int
	Text    string
	Context []SearchLine
}

// Search finds literal text in tracked and untracked, non-ignored working-tree files.
func (r *Repository) Search(query string, contextLines int) ([]SearchMatch, error) {
	return r.SearchContext(context.Background(), query, contextLines)
}

// SearchContext finds literal text and can be cancelled when a newer query supersedes it.
func (r *Repository) SearchContext(ctx context.Context, query string, contextLines int) ([]SearchMatch, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("search text cannot be empty")
	}
	contextLines = max(0, contextLines)

	output, exitCode, err := r.commandRunner().Run(ctx, r.Root, "grep", "-z", "-n", "-I", "--untracked", "--exclude-standard", "-F", "-e", query, "--")
	if err != nil {
		if exitCode == 1 {
			return nil, nil
		}
		return nil, fmt.Errorf("search repository: %w", err)
	}

	fileLines := map[string][]string{}
	var matches []SearchMatch
	scanner := bufio.NewScanner(bytes.NewBufferString(output))
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() && len(matches) < maxSearchMatches {
		parts := bytes.SplitN(scanner.Bytes(), []byte{0}, 3)
		if len(parts) != 3 {
			continue
		}
		line, parseErr := strconv.Atoi(string(parts[1]))
		if parseErr != nil || line < 1 {
			continue
		}
		path := filepath.ToSlash(string(parts[0]))
		lines, ok := fileLines[path]
		if !ok {
			content, readErr := os.ReadFile(filepath.Join(r.Root, filepath.FromSlash(path)))
			if readErr != nil {
				continue
			}
			lines = strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n")
			fileLines[path] = lines
		}
		matches = append(matches, SearchMatch{
			Path:    path,
			Line:    line,
			Text:    string(parts[2]),
			Context: searchContext(lines, line, contextLines),
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read search results: %w", err)
	}
	return matches, nil
}

func searchContext(lines []string, line, radius int) []SearchLine {
	start := max(1, line-radius)
	end := min(len(lines), line+radius)
	context := make([]SearchLine, 0, end-start+1)
	for number := start; number <= end; number++ {
		context = append(context, SearchLine{Number: number, Text: lines[number-1], Match: number == line})
	}
	return context
}
