package ui

import (
	"context"
	"fmt"
	"regexp"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"github.com/TenaciousMaker/revui/internal/diff"
	"github.com/TenaciousMaker/revui/internal/gitrepo"
)

var expandableHunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

type hunkExpansionContext struct {
	repo     *gitrepo.Repository
	file     int
	review   bool
	reviewID uint64
}

type hunkDisplayKey struct {
	hunkExpansionContext
	ignoreWhitespace bool
}

type hunkGapKey struct {
	oldStart int
	newStart int
	count    int
}

type hunkExpansionMsg struct {
	id        uint64
	context   hunkExpansionContext
	gap       hunkGapKey
	oldSource []byte
	newSource []byte
	err       error
}

// hunkExpansionState is a presentation cache over an immutable Git snapshot.
// It inserts gap rows once per display revision and reads full source only when
// the reviewer asks to expand one. Cursor movement and scrolling reuse lines.
type hunkExpansionState struct {
	cancel  context.CancelFunc
	id      uint64
	loading bool

	context  hunkExpansionContext
	expanded map[hunkGapKey]bool
	oldLines []string
	newLines []string
	revision uint64

	cacheKey      hunkDisplayKey
	cacheRevision uint64
	cacheLines    []diff.Line
	cacheHasGaps  bool
	spanSemantic  bool
	spanRevision  uint64
	spanKey       hunkDisplayKey
	spans         map[int][]textSpan
}

func newHunkExpansionState() *hunkExpansionState { return &hunkExpansionState{} }

func (m Model) hunkExpansionContext() hunkExpansionContext {
	context := hunkExpansionContext{repo: m.repo, file: m.file}
	if m.reviewComparisonActive() {
		context.review = true
		context.reviewID = m.reviewWork.comparisonID
	}
	return context
}

func (m Model) hunkDisplayKey() hunkDisplayKey {
	return hunkDisplayKey{
		hunkExpansionContext: m.hunkExpansionContext(),
		ignoreWhitespace:     m.ignoreWhitespace && !m.semanticReflow && !m.reviewComparisonActive(),
	}
}

func (s *hunkExpansionState) linesFor(key hunkDisplayKey, base []diff.Line) []diff.Line {
	if s == nil {
		return base
	}
	if s.cacheKey == key && s.cacheRevision == s.revision && s.cacheLines != nil {
		return s.cacheLines
	}
	if key.ignoreWhitespace {
		s.cacheKey, s.cacheRevision, s.cacheLines, s.cacheHasGaps = key, s.revision, base, false
		s.spans = nil
		return base
	}
	var expanded map[hunkGapKey]bool
	var oldLines, newLines []string
	if s.context == key.hunkExpansionContext {
		expanded, oldLines, newLines = s.expanded, s.oldLines, s.newLines
	}
	s.cacheLines, s.cacheHasGaps = buildExpandableDiffLines(base, expanded, oldLines, newLines)
	s.cacheKey, s.cacheRevision = key, s.revision
	s.spans = nil
	return s.cacheLines
}

func (s *hunkExpansionState) intralineFor(key hunkDisplayKey, lines []diff.Line, semantic bool) map[int][]textSpan {
	if s == nil {
		return buildIntralineSpanSet(lines, semantic)
	}
	if s.spans == nil || s.spanKey != key || s.spanRevision != s.revision || s.spanSemantic != semantic {
		s.spanKey, s.spanRevision, s.spanSemantic = key, s.revision, semantic
		s.spans = buildIntralineSpanSet(lines, semantic)
	}
	return s.spans
}

func (s *hunkExpansionState) hasGapPresentation(key hunkDisplayKey) bool {
	return s != nil && s.cacheKey == key && s.cacheRevision == s.revision && s.cacheHasGaps
}

func (s *hunkExpansionState) versionFor(context hunkExpansionContext) uint64 {
	if s == nil || s.context != context {
		return 0
	}
	return s.revision
}

func buildExpandableDiffLines(base []diff.Line, expanded map[hunkGapKey]bool, oldSource, newSource []string) ([]diff.Line, bool) {
	if len(base) == 0 {
		return base, false
	}
	result := make([]diff.Line, 0, len(base)+4)
	lastOld, lastNew, previousHunk := 0, 0, -1
	hasGaps := false
	for _, line := range base {
		if line.Kind == diff.Meta && line.Collapsed == 0 {
			oldStart, newStart, ok := hunkStarts(line.Text)
			if ok && previousHunk >= 0 && line.Hunk == previousHunk+1 {
				oldCount, newCount := oldStart-lastOld-1, newStart-lastNew-1
				if oldCount > 0 && oldCount == newCount {
					gap := hunkGapKey{oldStart: lastOld + 1, newStart: lastNew + 1, count: oldCount}
					hasGaps = true
					if expanded[gap] && gapFitsSource(gap, oldSource, newSource) {
						for offset := 0; offset < gap.count; offset++ {
							oldNumber, newNumber := gap.oldStart+offset, gap.newStart+offset
							result = append(result, diff.Line{
								Kind: diff.Context, Text: newSource[newNumber-1], OldNumber: oldNumber, NewNumber: newNumber,
								Hunk: previousHunk, OriginalIndex: -(oldNumber + 1),
							})
						}
					} else {
						result = append(result, diff.Line{
							Kind: diff.Meta, Text: fmt.Sprintf("⋯ %d unchanged lines · click or x to expand", gap.count),
							OldNumber: gap.oldStart, NewNumber: gap.newStart, Hunk: line.Hunk,
							OriginalIndex: -(gap.oldStart + 1), Collapsed: gap.count,
						})
					}
				}
			}
			previousHunk = line.Hunk
		}
		result = append(result, line)
		if line.OldNumber > 0 {
			lastOld = line.OldNumber
		}
		if line.NewNumber > 0 {
			lastNew = line.NewNumber
		}
	}
	return result, hasGaps
}

func hunkStarts(header string) (int, int, bool) {
	match := expandableHunkHeader.FindStringSubmatch(header)
	if len(match) != 3 {
		return 0, 0, false
	}
	oldStart, oldErr := strconv.Atoi(match[1])
	newStart, newErr := strconv.Atoi(match[2])
	return oldStart, newStart, oldErr == nil && newErr == nil
}

func gapFitsSource(gap hunkGapKey, oldSource, newSource []string) bool {
	if gap.oldStart <= 0 || gap.newStart <= 0 ||
		gap.oldStart+gap.count-1 > len(oldSource) || gap.newStart+gap.count-1 > len(newSource) {
		return false
	}
	for offset := 0; offset < gap.count; offset++ {
		if oldSource[gap.oldStart+offset-1] != newSource[gap.newStart+offset-1] {
			return false
		}
	}
	return true
}

func (m Model) selectedHunkGap() (hunkGapKey, bool) {
	lines := m.currentLines()
	if len(lines) == 0 {
		return hunkGapKey{}, false
	}
	index := clamp(m.line, 0, len(lines)-1)
	if lines[index].Collapsed == 0 && lines[index].Kind == diff.Meta && index > 0 {
		index--
	}
	line := lines[index]
	if line.Collapsed <= 0 {
		return hunkGapKey{}, false
	}
	return hunkGapKey{oldStart: line.OldNumber, newStart: line.NewNumber, count: line.Collapsed}, true
}

func (m *Model) expandSelectedHunkGap() tea.Cmd {
	gap, ok := m.selectedHunkGap()
	if !ok {
		m.status = "Select a ⋯ row, or the hunk header below it, to expand context."
		return nil
	}
	contextKey := m.hunkExpansionContext()
	m.prepareHunkExpansionContext(contextKey)
	if m.hunkExpansion.expanded[gap] {
		return nil
	}
	if len(m.hunkExpansion.oldLines) > 0 || len(m.hunkExpansion.newLines) > 0 {
		m.applyExpandedHunkGap(gap)
		return nil
	}
	if contextKey.review && m.reviewWork.comparisonBefore.Available && m.reviewWork.comparisonCurrent.Available {
		m.setHunkExpansionSources(m.reviewWork.comparisonBefore.Content, m.reviewWork.comparisonCurrent.Content)
		m.applyExpandedHunkGap(gap)
		return nil
	}
	if !contextKey.review && m.semantic.ready && m.semantic.repo == m.repo && m.semantic.file == m.file {
		m.setHunkExpansionSources(m.semantic.oldSource, m.semantic.newSource)
		m.applyExpandedHunkGap(gap)
		return nil
	}
	if m.hunkExpansion.cancel != nil {
		m.hunkExpansion.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.hunkExpansion.cancel = cancel
	m.hunkExpansion.id++
	m.hunkExpansion.loading = true
	id, repo, file, operations := m.hunkExpansion.id, m.repo, m.repo.Files[m.file], m.repositories
	m.status = "Loading unchanged context…"
	return func() tea.Msg {
		oldSource, newSource, err := operations.ReadPair(ctx, repo, file)
		return hunkExpansionMsg{id: id, context: contextKey, gap: gap, oldSource: oldSource, newSource: newSource, err: err}
	}
}

func (m *Model) applyHunkExpansion(msg hunkExpansionMsg) {
	if m.hunkExpansion == nil || msg.id != m.hunkExpansion.id || msg.context != m.hunkExpansionContext() {
		return
	}
	m.hunkExpansion.cancel = nil
	m.hunkExpansion.loading = false
	if msg.err != nil {
		m.status = "Expand context failed: " + msg.err.Error()
		return
	}
	m.setHunkExpansionSources(msg.oldSource, msg.newSource)
	m.applyExpandedHunkGap(msg.gap)
}

func (m *Model) prepareHunkExpansionContext(context hunkExpansionContext) {
	if m.hunkExpansion.context == context {
		if m.hunkExpansion.expanded == nil {
			m.hunkExpansion.expanded = map[hunkGapKey]bool{}
		}
		return
	}
	if m.hunkExpansion.cancel != nil {
		m.hunkExpansion.cancel()
	}
	m.hunkExpansion.context = context
	m.hunkExpansion.expanded = map[hunkGapKey]bool{}
	m.hunkExpansion.oldLines = nil
	m.hunkExpansion.newLines = nil
	m.hunkExpansion.loading = false
	m.hunkExpansion.revision++
}

func (m *Model) setHunkExpansionSources(oldSource, newSource []byte) {
	m.hunkExpansion.oldLines = physicalSourceLines(oldSource)
	m.hunkExpansion.newLines = physicalSourceLines(newSource)
}

func (m *Model) applyExpandedHunkGap(gap hunkGapKey) {
	if !gapFitsSource(gap, m.hunkExpansion.oldLines, m.hunkExpansion.newLines) {
		m.status = "Full source is unavailable for this collapsed region."
		return
	}
	m.hunkExpansion.expanded[gap] = true
	m.hunkExpansion.revision++
	m.hunkExpansion.cacheLines = nil
	m.hunkExpansion.spans = nil
	m.syncSplitCursorToLine()
	m.ensureVisible()
	m.status = fmt.Sprintf("Expanded %d unchanged lines.", gap.count)
}

func (m *Model) cancelHunkExpansion() {
	if m.hunkExpansion == nil {
		return
	}
	if m.hunkExpansion.cancel != nil {
		m.hunkExpansion.cancel()
	}
	m.hunkExpansion.cancel = nil
	m.hunkExpansion.loading = false
}
