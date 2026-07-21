package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/TenaciousMaker/revui/internal/diff"
)

// clipboardWriter is the clipboard seam used by the UI and its tests.
// The terminal adapter uses OSC52, which works locally and across SSH without
// requiring an operating-system-specific helper process.
type clipboardWriter interface {
	Write(string) tea.Cmd
}

type terminalClipboard struct{}

func (terminalClipboard) Write(text string) tea.Cmd { return tea.SetClipboard(text) }

type clipboardResultMsg struct {
	lineCount int
	err       error
}

type sourceRange struct {
	start int
	end   int
}

func (r *sourceRange) include(line int) {
	if line <= 0 {
		return
	}
	if r.start == 0 || line < r.start {
		r.start = line
	}
	if line > r.end {
		r.end = line
	}
}

func (m Model) clipboardText() (string, int) {
	text, lineCount, branchLines, baseLines := m.clipboardSelection()
	if text == "" {
		return "", 0
	}

	path := m.currentPath()
	context := []string{"File: " + path}
	if branchLines.start > 0 {
		context = append(context, "Location: branch "+formatSourceRange(branchLines))
	}
	if baseLines.start > 0 {
		base := m.repo.Base
		if m.reviewComparisonActive() {
			base = "last review"
		}
		if base == "" {
			base = "base"
		}
		context = append(context, "Location: "+base+" "+formatSourceRange(baseLines))
	}
	if branchLines.start == 0 && baseLines.start == 0 {
		context = append(context, "Location: current diff hunk")
	}
	return strings.Join(context, "\n") + "\n\n" + text, lineCount
}

func (m Model) clipboardSelection() (string, int, sourceRange, sourceRange) {
	if text := m.selectedText; text != "" {
		if (m.normalizedLayout || m.difftasticMode) && m.view == split && m.sourcePath == "" {
			return m.normalizedMouseClipboardSelection()
		}
		branchLines, baseLines := m.mouseSelectionRanges()
		return text, strings.Count(text, "\n") + 1, branchLines, baseLines
	}
	if m.sourcePath != "" {
		if m.sourceLine < 0 || m.sourceLine >= len(m.sourceLines) {
			return "", 0, sourceRange{}, sourceRange{}
		}
		line := sourceRange{start: m.sourceLine + 1, end: m.sourceLine + 1}
		if m.sourceFromBase {
			return m.sourceLines[m.sourceLine], 1, sourceRange{}, line
		}
		return m.sourceLines[m.sourceLine], 1, line, sourceRange{}
	}

	lines := m.currentLines()
	if len(lines) == 0 {
		return "", 0, sourceRange{}, sourceRange{}
	}
	start, end := m.line, m.line
	if m.selectFrom >= 0 {
		start, end = min(m.selectFrom, m.line), max(m.selectFrom, m.line)
	}
	start = clamp(start, 0, len(lines)-1)
	end = clamp(end, start, len(lines)-1)
	selected := lines[start : end+1]
	branchLines, baseLines := diffSourceRanges(selected)
	text := make([]string, 0, len(selected))
	for _, line := range selected {
		text = append(text, line.Text)
	}
	return strings.Join(text, "\n"), len(text), branchLines, baseLines
}

func (m Model) normalizedMouseClipboardSelection() (string, int, sourceRange, sourceRange) {
	rows := m.currentSplitRows()
	lines := m.currentLines()
	if len(rows) == 0 || len(lines) == 0 {
		return "", 0, sourceRange{}, sourceRange{}
	}
	start, end := orderedMousePoints(m.mouseSelectStart, m.mouseSelectEnd)
	first := clamp(m.splitScroll+max(0, start.y-5), 0, len(rows)-1)
	last := clamp(m.splitScroll+max(0, end.y-5), first, len(rows)-1)
	codeLeft := 0
	if m.width >= 90 {
		codeLeft = m.filePaneWidth()
	}
	leftSide := start.x < codeLeft+(m.width-codeLeft)/2
	seen := map[int]bool{}
	var selected []diff.Line
	for _, row := range rows[first : last+1] {
		indices := row.newIndices
		if len(indices) == 0 && row.newIndex >= 0 {
			indices = []int{row.newIndex}
		}
		if leftSide {
			indices = row.oldIndices
			if len(indices) == 0 && row.oldIndex >= 0 {
				indices = []int{row.oldIndex}
			}
		}
		for _, index := range indices {
			if index < 0 || index >= len(lines) || seen[index] {
				continue
			}
			seen[index] = true
			selected = append(selected, lines[index])
		}
	}
	branchLines, baseLines := diffSourceRanges(selected)
	text := make([]string, len(selected))
	for index, line := range selected {
		text[index] = line.Text
	}
	return strings.Join(text, "\n"), len(text), branchLines, baseLines
}

func (m Model) mouseSelectionRanges() (sourceRange, sourceRange) {
	start, end := orderedMousePoints(m.mouseSelectStart, m.mouseSelectEnd)
	startRow, endRow := max(0, start.y-5), max(0, end.y-5)
	if m.sourcePath != "" {
		lines := sourceRange{
			start: clamp(m.sourceScroll+startRow+1, 1, max(1, len(m.sourceLines))),
			end:   clamp(m.sourceScroll+endRow+1, 1, max(1, len(m.sourceLines))),
		}
		if m.sourceFromBase {
			return sourceRange{}, lines
		}
		return lines, sourceRange{}
	}

	if m.view == split {
		rows := m.currentSplitRows()
		if len(rows) == 0 {
			return sourceRange{}, sourceRange{}
		}
		first := clamp(m.splitScroll+startRow, 0, max(0, len(rows)-1))
		last := clamp(m.splitScroll+endRow, first, max(0, len(rows)-1))
		var selected []diff.Line
		for _, row := range rows[first : last+1] {
			if row.old != nil {
				selected = append(selected, *row.old)
			}
			if row.new != nil && row.newIndex != row.oldIndex {
				selected = append(selected, *row.new)
			}
		}
		return diffSourceRanges(selected)
	}

	lines := m.currentLines()
	if len(lines) == 0 {
		return sourceRange{}, sourceRange{}
	}
	first := clamp(m.lineScroll+startRow, 0, max(0, len(lines)-1))
	last := clamp(m.lineScroll+endRow, first, max(0, len(lines)-1))
	return diffSourceRanges(lines[first : last+1])
}

func diffSourceRanges(lines []diff.Line) (sourceRange, sourceRange) {
	var branchLines, baseLines sourceRange
	for _, line := range lines {
		switch line.Kind {
		case diff.Addition:
			branchLines.include(line.NewNumber)
		case diff.Deletion:
			baseLines.include(line.OldNumber)
		case diff.Context:
			branchLines.include(line.NewNumber)
		}
	}
	return branchLines, baseLines
}

func formatSourceRange(lines sourceRange) string {
	if lines.start == lines.end {
		return fmt.Sprintf("L%d", lines.start)
	}
	return fmt.Sprintf("L%d-L%d", lines.start, lines.end)
}

func (m Model) copySelectionCmd() tea.Cmd {
	text, lines := m.clipboardText()
	if text == "" {
		return func() tea.Msg { return clipboardResultMsg{err: fmt.Errorf("nothing selected to copy")} }
	}
	writer := m.clipboard
	if writer == nil {
		writer = terminalClipboard{}
	}
	return tea.Batch(
		writer.Write(text),
		func() tea.Msg { return clipboardResultMsg{lineCount: lines} },
	)
}

func clipboardStatus(lineCount int) string {
	if lineCount == 1 {
		return "Copied current line with location to clipboard."
	}
	return fmt.Sprintf("Copied %d lines with location to clipboard.", lineCount)
}
