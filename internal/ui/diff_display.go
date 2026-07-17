package ui

import (
	"strings"
	"unicode"

	"github.com/TenaciousMaker/revui/internal/diff"
	"github.com/TenaciousMaker/revui/internal/gitrepo"
)

// diffDisplayCache owns the derived, immutable line view for one repository
// snapshot and filter combination. Ordinary cursor movement and scrolling only
// read this slice; they never rebuild Git data or the filter result.
type diffDisplayCache struct {
	repo             *gitrepo.Repository
	file             int
	ignoreWhitespace bool
	ignoreMoved      bool
	lines            []diff.Line
	intraline        map[int][]textSpan
	semanticReflow   bool
}

func (c *diffDisplayCache) linesFor(repo *gitrepo.Repository, file int, ignoreWhitespace, ignoreMoved bool) []diff.Line {
	if c.repo == repo && c.file == file && c.ignoreWhitespace == ignoreWhitespace && c.ignoreMoved == ignoreMoved {
		return c.lines
	}
	c.repo = repo
	c.file = file
	c.ignoreWhitespace = ignoreWhitespace
	c.ignoreMoved = ignoreMoved
	c.lines = buildVisibleDiffLines(repo.Files[file].Lines, ignoreWhitespace, ignoreMoved)
	c.intraline = nil
	return c.lines
}

func (c *diffDisplayCache) intralineFor(repo *gitrepo.Repository, file int, ignoreWhitespace, ignoreMoved, semanticReflow bool) map[int][]textSpan {
	c.linesFor(repo, file, ignoreWhitespace, ignoreMoved)
	if c.intraline == nil || c.semanticReflow != semanticReflow {
		c.semanticReflow = semanticReflow
		c.intraline = buildIntralineSpanSet(c.lines, semanticReflow)
	}
	return c.intraline
}

func buildVisibleDiffLines(lines []diff.Line, ignoreWhitespace, ignoreMoved bool) []diff.Line {
	hidden := make([]bool, len(lines))
	if ignoreWhitespace {
		hideWhitespaceOnlyChanges(lines, hidden)
	}
	if ignoreMoved {
		hideMovedLines(lines, hidden, ignoreWhitespace)
	}

	visibleHunks := map[int]bool{}
	for index, line := range lines {
		if hidden[index] {
			continue
		}
		if line.Kind == diff.Addition || line.Kind == diff.Deletion {
			visibleHunks[line.Hunk] = true
		}
	}

	visible := make([]diff.Line, 0, len(lines))
	for index, line := range lines {
		if hidden[index] {
			continue
		}
		if (ignoreWhitespace || ignoreMoved) && !visibleHunks[line.Hunk] {
			continue
		}
		line.OriginalIndex = index
		visible = append(visible, line)
	}
	return visible
}

func hideWhitespaceOnlyChanges(lines []diff.Line, hidden []bool) {
	for index, line := range lines {
		if (line.Kind == diff.Addition || line.Kind == diff.Deletion) && whitespaceKey(line.Text) == "" {
			hidden[index] = true
		}
	}
	for index := 0; index < len(lines); {
		if lines[index].Kind != diff.Deletion {
			index++
			continue
		}
		deletionStart := index
		for index < len(lines) && lines[index].Kind == diff.Deletion {
			index++
		}
		additionStart := index
		for index < len(lines) && lines[index].Kind == diff.Addition {
			index++
		}
		for offset := 0; offset < min(additionStart-deletionStart, index-additionStart); offset++ {
			deletion, addition := deletionStart+offset, additionStart+offset
			if whitespaceKey(lines[deletion].Text) == whitespaceKey(lines[addition].Text) {
				hidden[deletion] = true
				hidden[addition] = true
			}
		}
	}
}

func hideMovedLines(lines []diff.Line, hidden []bool, ignoreWhitespace bool) {
	deletions := map[string][]int{}
	for index, line := range lines {
		if hidden[index] || line.Kind != diff.Deletion {
			continue
		}
		key := line.Text
		if ignoreWhitespace {
			key = whitespaceKey(key)
		}
		deletions[key] = append(deletions[key], index)
	}
	for index, line := range lines {
		if hidden[index] || line.Kind != diff.Addition {
			continue
		}
		key := line.Text
		if ignoreWhitespace {
			key = whitespaceKey(key)
		}
		matches := deletions[key]
		if len(matches) == 0 {
			continue
		}
		hidden[matches[0]] = true
		hidden[index] = true
		deletions[key] = matches[1:]
	}
}

func whitespaceKey(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, value)
}
