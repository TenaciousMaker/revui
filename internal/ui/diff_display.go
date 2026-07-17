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
	lines            []diff.Line
	intraline        map[int][]textSpan
	semanticReflow   bool
}

func (c *diffDisplayCache) linesFor(repo *gitrepo.Repository, file int, ignoreWhitespace bool) []diff.Line {
	if c.repo == repo && c.file == file && c.ignoreWhitespace == ignoreWhitespace {
		return c.lines
	}
	c.repo = repo
	c.file = file
	c.ignoreWhitespace = ignoreWhitespace
	c.lines = buildVisibleDiffLines(repo.Files[file].Lines, ignoreWhitespace)
	c.intraline = nil
	return c.lines
}

func (c *diffDisplayCache) intralineFor(repo *gitrepo.Repository, file int, ignoreWhitespace, semanticReflow bool) map[int][]textSpan {
	c.linesFor(repo, file, ignoreWhitespace)
	if c.intraline == nil || c.semanticReflow != semanticReflow {
		c.semanticReflow = semanticReflow
		c.intraline = buildIntralineSpanSet(c.lines, semanticReflow)
	}
	return c.intraline
}

func buildVisibleDiffLines(lines []diff.Line, ignoreWhitespace bool) []diff.Line {
	hidden := make([]bool, len(lines))
	if ignoreWhitespace {
		hideWhitespaceOnlyChanges(lines, hidden)
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
		if ignoreWhitespace && !visibleHunks[line.Hunk] {
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

func whitespaceKey(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, value)
}
