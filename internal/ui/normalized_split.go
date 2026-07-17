package ui

import (
	"sort"

	"github.com/alecthomas/chroma/v2"

	"github.com/TenaciousMaker/revui/internal/diff"
	"github.com/TenaciousMaker/revui/internal/gitrepo"
	"github.com/TenaciousMaker/revui/internal/semantic"
)

type normalizedSplitCache struct {
	repo                       *gitrepo.Repository
	file                       int
	layout                     *semantic.Layout
	oldSourceLen, newSourceLen int
	rows                       []splitRow
}

type splitSourceAnchor struct {
	oldOriginal, newOriginal, metaOriginal int
	hasOld, hasNew, hasMeta                bool
	fallbackRow                            int
}

type splitLayoutPosition struct {
	cursor splitSourceAnchor
	top    splitSourceAnchor
}

func (m Model) captureSplitLayoutPosition() splitLayoutPosition {
	if m.view == split {
		rows := m.currentSplitRows()
		return splitLayoutPosition{
			cursor: splitAnchorForRow(rows, m.splitCursor),
			top:    splitAnchorForRow(rows, m.splitScroll),
		}
	}
	lines := m.currentLines()
	return splitLayoutPosition{
		cursor: splitAnchorForLine(lines, m.line),
		top:    splitAnchorForLine(lines, m.lineScroll),
	}
}

func (m *Model) restoreSplitLayoutPosition(position splitLayoutPosition) {
	if m.view != split {
		lines := m.currentLines()
		if len(lines) == 0 {
			m.line, m.lineScroll = 0, 0
			return
		}
		m.line = findLineAnchor(lines, position.cursor)
		m.lineScroll = clamp(findLineAnchor(lines, position.top), 0, max(0, len(lines)-m.pageSize()))
		return
	}
	rows := m.currentSplitRows()
	if len(rows) == 0 {
		m.line, m.splitCursor, m.splitScroll = 0, 0, 0
		return
	}
	m.splitCursor = findSplitAnchor(rows, position.cursor)
	m.splitScroll = clamp(findSplitAnchor(rows, position.top), 0, max(0, len(rows)-m.pageSize()))
	m.syncLineFromSplitCursor()
}

func findLineAnchor(lines []diff.Line, anchor splitSourceAnchor) int {
	wanted := []struct {
		original int
		valid    bool
	}{{anchor.metaOriginal, anchor.hasMeta}, {anchor.newOriginal, anchor.hasNew}, {anchor.oldOriginal, anchor.hasOld}}
	for _, candidate := range wanted {
		if !candidate.valid {
			continue
		}
		for index, line := range lines {
			if line.OriginalIndex == candidate.original {
				return index
			}
		}
	}
	return clamp(anchor.fallbackRow, 0, max(0, len(lines)-1))
}

func splitAnchorForRow(rows []splitRow, index int) splitSourceAnchor {
	anchor := splitSourceAnchor{fallbackRow: max(0, index)}
	if index < 0 || index >= len(rows) {
		return anchor
	}
	row := rows[index]
	if row.old != nil {
		anchor.oldOriginal, anchor.hasOld = row.old.OriginalIndex, true
	}
	if row.new != nil {
		anchor.newOriginal, anchor.hasNew = row.new.OriginalIndex, true
	}
	if row.meta != nil {
		anchor.metaOriginal, anchor.hasMeta = row.meta.OriginalIndex, true
	}
	return anchor
}

func splitAnchorForLine(lines []diff.Line, index int) splitSourceAnchor {
	anchor := splitSourceAnchor{fallbackRow: max(0, index)}
	if index < 0 || index >= len(lines) {
		return anchor
	}
	line := lines[index]
	switch line.Kind {
	case diff.Addition:
		anchor.newOriginal, anchor.hasNew = line.OriginalIndex, true
	case diff.Deletion:
		anchor.oldOriginal, anchor.hasOld = line.OriginalIndex, true
	case diff.Meta:
		anchor.metaOriginal, anchor.hasMeta = line.OriginalIndex, true
	default:
		anchor.oldOriginal, anchor.newOriginal = line.OriginalIndex, line.OriginalIndex
		anchor.hasOld, anchor.hasNew = true, true
	}
	return anchor
}

func findSplitAnchor(rows []splitRow, anchor splitSourceAnchor) int {
	if anchor.hasOld && anchor.hasNew {
		for index, row := range rows {
			if row.old != nil && row.new != nil && row.old.OriginalIndex == anchor.oldOriginal && row.new.OriginalIndex == anchor.newOriginal {
				return index
			}
		}
	}
	if anchor.hasMeta {
		for index, row := range rows {
			if row.meta != nil && row.meta.OriginalIndex == anchor.metaOriginal {
				return index
			}
		}
	}
	// Prefer the current (new) side when a raw row pairs an old line with a
	// different added line than the normalized layout does.
	if anchor.hasNew {
		for index, row := range rows {
			if row.new != nil && row.new.OriginalIndex == anchor.newOriginal {
				return index
			}
		}
	}
	if anchor.hasOld {
		for index, row := range rows {
			if row.old != nil && row.old.OriginalIndex == anchor.oldOriginal {
				return index
			}
		}
	}
	return clamp(anchor.fallbackRow, 0, max(0, len(rows)-1))
}

func (c *normalizedSplitCache) rowsFor(m Model, lines []diff.Line) []splitRow {
	if !m.normalizedLayout || !m.semantic.ready || m.semantic.layout == nil || m.semantic.repo != m.repo || m.semantic.file != m.file {
		return splitRows(lines)
	}
	if c.repo == m.repo && c.file == m.file && c.layout == m.semantic.layout &&
		c.oldSourceLen == len(m.semantic.oldSource) && c.newSourceLen == len(m.semantic.newSource) {
		return c.rows
	}
	c.repo, c.file, c.layout = m.repo, m.file, m.semantic.layout
	c.oldSourceLen, c.newSourceLen = len(m.semantic.oldSource), len(m.semantic.newSource)
	c.rows = buildNormalizedSplitRows(lines, m.semantic.layout, m.semantic.oldSource, m.semantic.newSource)
	attachNormalizedSyntax(c.rows, m.currentPath())
	return c.rows
}

func attachNormalizedSyntax(rows []splitRow, path string) {
	document := &diffSyntaxDocument{oldLines: map[int][]chroma.Token{}, newLines: map[int][]chroma.Token{}}
	var oldSegment, newSegment []indexedSyntaxLine
	flush := func() {
		tokeniseSyntaxSegment(path, oldSegment, document.oldLines)
		tokeniseSyntaxSegment(path, newSegment, document.newLines)
		oldSegment, newSegment = nil, nil
	}
	for index, row := range rows {
		if row.meta != nil {
			flush()
			continue
		}
		if row.old != nil {
			oldSegment = append(oldSegment, indexedSyntaxLine{index: index, text: expandTabs(row.old.Text)})
		}
		if row.new != nil {
			newSegment = append(newSegment, indexedSyntaxLine{index: index, text: expandTabs(row.new.Text)})
		}
	}
	flush()
	for index := range rows {
		rows[index].oldSyntax = document.oldLines[index]
		rows[index].newSyntax = document.newLines[index]
	}
}

func buildNormalizedSplitRows(lines []diff.Line, layout *semantic.Layout, oldSource, newSource []byte) []splitRow {
	if layout == nil || len(layout.Blocks) == 0 {
		return splitRows(lines)
	}
	oldStarts, newStarts := sourceLineStarts(oldSource), sourceLineStarts(newSource)
	crossContext := crossContextLayoutAnchors(lines, layout.Blocks, oldStarts, newStarts)
	crossAt := 0
	var rows []splitRow
	for index := 0; index < len(lines); {
		if crossAt < len(crossContext) && index == crossContext[crossAt].start {
			rows = append(rows, crossContext[crossAt].rows...)
			index = crossContext[crossAt].end
			crossAt++
			continue
		}
		boundary := len(lines)
		if crossAt < len(crossContext) {
			boundary = crossContext[crossAt].start
		}
		switch lines[index].Kind {
		case diff.Meta:
			line := lines[index]
			rows = append(rows, splitRow{meta: &line, metaIndex: index, oldIndex: -1, newIndex: -1})
			index++
		case diff.Context:
			line := lines[index]
			rows = append(rows, splitRow{old: &line, new: &line, oldIndex: index, newIndex: index, metaIndex: -1})
			index++
		default:
			start := index
			var deletions, additions []int
			for index < boundary && lines[index].Kind == diff.Deletion {
				deletions = append(deletions, index)
				index++
			}
			for index < boundary && lines[index].Kind == diff.Addition {
				additions = append(additions, index)
				index++
			}
			if len(deletions) == 0 && len(additions) == 0 {
				index = start + 1
				continue
			}
			rows = append(rows, structuredRowsForChange(lines, deletions, additions, layout.Blocks, oldStarts, newStarts)...)
		}
	}
	return rows
}

type crossContextLayoutAnchor struct {
	start, end int
	block      semantic.LayoutBlock
	rows       []splitRow
}

// crossContextLayoutAnchors repairs the rare case where Git pairs identical
// punctuation from different syntax nodes as context. A confidence-100 layout
// block may then straddle several raw change groups even though it is one
// unchanged subtree. Lower-confidence blocks stay within the ordinary Git
// grouping path.
func crossContextLayoutAnchors(lines []diff.Line, blocks []semantic.LayoutBlock, oldStarts, newStarts []int) []crossContextLayoutAnchor {
	var candidates []crossContextLayoutAnchor
	for _, block := range blocks {
		if block.Confidence != 100 {
			continue
		}
		oldFirst, oldLast := sourceRangeLineSpan(block.Old, oldStarts)
		newFirst, newLast := sourceRangeLineSpan(block.New, newStarts)
		if oldFirst <= 0 || newFirst <= 0 {
			continue
		}
		start, end, hunk := len(lines), -1, -1
		changedOld, changedNew, crossesContext := false, false, false
		for index, line := range lines {
			oldIn := lineHasOldSide(line) && line.OldNumber >= oldFirst && line.OldNumber <= oldLast
			newIn := lineHasNewSide(line) && line.NewNumber >= newFirst && line.NewNumber <= newLast
			if !oldIn && !newIn {
				continue
			}
			if hunk >= 0 && line.Hunk != hunk {
				hunk = -2
				break
			}
			hunk = line.Hunk
			start, end = min(start, index), max(end, index+1)
			changedOld = changedOld || (oldIn && line.Kind == diff.Deletion)
			changedNew = changedNew || (newIn && line.Kind == diff.Addition)
			crossesContext = crossesContext || (line.Kind == diff.Context && oldIn != newIn)
		}
		if hunk < 0 || end <= start || !changedOld || !changedNew || !crossesContext {
			continue
		}
		projected, ok := rowsForCrossContextBlock(lines, start, end, block, oldFirst, oldLast, newFirst, newLast, oldStarts, newStarts)
		if !ok {
			continue
		}
		candidates = append(candidates, crossContextLayoutAnchor{start: start, end: end, block: block, rows: projected})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		left, right := candidates[i], candidates[j]
		if left.start != right.start {
			return left.start < right.start
		}
		// An outer exact subtree resolves the false context match for all of
		// its children in one coherent projection.
		if left.end != right.end {
			return left.end > right.end
		}
		leftBytes := (left.block.Old.End - left.block.Old.Start) + (left.block.New.End - left.block.New.Start)
		rightBytes := (right.block.Old.End - right.block.Old.Start) + (right.block.New.End - right.block.New.Start)
		return leftBytes > rightBytes
	})
	anchors := make([]crossContextLayoutAnchor, 0, len(candidates))
	lastEnd := 0
	for _, candidate := range candidates {
		if candidate.start < lastEnd {
			continue
		}
		anchors = append(anchors, candidate)
		lastEnd = candidate.end
	}
	return anchors
}

func rowsForCrossContextBlock(lines []diff.Line, start, end int, block semantic.LayoutBlock, oldFirst, oldLast, newFirst, newLast int, oldStarts, newStarts []int) ([]splitRow, bool) {
	var oldBefore, newBefore, oldInside, newInside, oldAfter, newAfter []int
	oldByLine, newByLine := map[int]int{}, map[int]int{}
	for index := start; index < end; index++ {
		line := lines[index]
		if lineHasOldSide(line) {
			switch {
			case line.OldNumber < oldFirst:
				oldBefore = append(oldBefore, index)
			case line.OldNumber > oldLast:
				oldAfter = append(oldAfter, index)
			default:
				oldInside = append(oldInside, index)
				oldByLine[line.OldNumber] = index
			}
		}
		if lineHasNewSide(line) {
			switch {
			case line.NewNumber < newFirst:
				newBefore = append(newBefore, index)
			case line.NewNumber > newLast:
				newAfter = append(newAfter, index)
			default:
				newInside = append(newInside, index)
				newByLine[line.NewNumber] = index
			}
		}
	}
	projected := rowsFromLayoutBlock(lines, block, oldByLine, newByLine, oldStarts, newStarts)
	if !rowsCoverIndices(projected, oldInside, newInside) {
		return nil, false
	}
	rows := rawRowsForSideIndices(lines, oldBefore, newBefore)
	rows = append(rows, projected...)
	rows = append(rows, rawRowsForSideIndices(lines, oldAfter, newAfter)...)
	return rows, true
}

func lineHasOldSide(line diff.Line) bool {
	return line.OldNumber > 0 && line.Kind != diff.Addition && line.Kind != diff.Meta
}

func lineHasNewSide(line diff.Line) bool {
	return line.NewNumber > 0 && line.Kind != diff.Deletion && line.Kind != diff.Meta
}

func rawRowsForSideIndices(lines []diff.Line, oldIndices, newIndices []int) []splitRow {
	var rows []splitRow
	oldAt, newAt := 0, 0
	for oldAt < len(oldIndices) || newAt < len(newIndices) {
		oldMatch, newMatch := len(oldIndices), len(newIndices)
		left, right := oldAt, newAt
		for left < len(oldIndices) && right < len(newIndices) {
			switch {
			case oldIndices[left] == newIndices[right]:
				oldMatch, newMatch = left, right
				left, right = len(oldIndices), len(newIndices)
			case oldIndices[left] < newIndices[right]:
				left++
			default:
				right++
			}
		}
		rows = append(rows, rawSideGapRows(lines, oldIndices[oldAt:oldMatch], newIndices[newAt:newMatch])...)
		if oldMatch == len(oldIndices) || newMatch == len(newIndices) {
			break
		}
		index := oldIndices[oldMatch]
		oldLine, newLine := lines[index], lines[index]
		oldLine.Kind, newLine.Kind = diff.Context, diff.Context
		rows = append(rows, splitRow{old: &oldLine, new: &newLine, oldIndex: index, newIndex: index, metaIndex: -1})
		oldAt, newAt = oldMatch+1, newMatch+1
	}
	return rows
}

func rawSideGapRows(lines []diff.Line, oldIndices, newIndices []int) []splitRow {
	rows := make([]splitRow, 0, max(len(oldIndices), len(newIndices)))
	for offset := 0; offset < max(len(oldIndices), len(newIndices)); offset++ {
		row := splitRow{oldIndex: -1, newIndex: -1, metaIndex: -1}
		if offset < len(oldIndices) {
			index := oldIndices[offset]
			line := lines[index]
			line.Kind = diff.Deletion
			row.old, row.oldIndex = &line, index
		}
		if offset < len(newIndices) {
			index := newIndices[offset]
			line := lines[index]
			line.Kind = diff.Addition
			row.new, row.newIndex = &line, index
		}
		if row.old != nil && row.new != nil {
			row.oldSpans, row.newSpans = intralineChanges(expandTabs(row.old.Text), expandTabs(row.new.Text))
		}
		rows = append(rows, row)
	}
	return rows
}

type layoutAnchor struct {
	block            semantic.LayoutBlock
	oldStart, oldEnd int
	newStart, newEnd int
}

func structuredRowsForChange(lines []diff.Line, deletions, additions []int, blocks []semantic.LayoutBlock, oldStarts, newStarts []int) []splitRow {
	anchors := applicableLayoutBlocks(lines, deletions, additions, blocks, oldStarts, newStarts)
	if len(anchors) == 0 {
		return rawRowsForIndices(lines, deletions, additions)
	}
	oldByLine, newByLine := make(map[int]int, len(deletions)), make(map[int]int, len(additions))
	for _, index := range deletions {
		oldByLine[lines[index].OldNumber] = index
	}
	for _, index := range additions {
		newByLine[lines[index].NewNumber] = index
	}
	var rows []splitRow
	oldAt, newAt := 0, 0
	for _, anchor := range anchors {
		rows = append(rows, rawRowsForIndices(lines, deletions[oldAt:anchor.oldStart], additions[newAt:anchor.newStart])...)
		projected := rowsFromLayoutBlock(lines, anchor.block, oldByLine, newByLine, oldStarts, newStarts)
		if rowsCoverIndices(projected, deletions[anchor.oldStart:anchor.oldEnd], additions[anchor.newStart:anchor.newEnd]) {
			rows = append(rows, projected...)
		} else {
			// Layout is a presentation hint, never the source of truth. If an
			// owner projection does not account for every changed Git row, keep
			// the literal rows rather than silently consuming source changes.
			rows = append(rows, rawRowsForIndices(lines, deletions[anchor.oldStart:anchor.oldEnd], additions[anchor.newStart:anchor.newEnd])...)
		}
		oldAt, newAt = anchor.oldEnd, anchor.newEnd
	}
	rows = append(rows, rawRowsForIndices(lines, deletions[oldAt:], additions[newAt:])...)
	return rows
}

func rowsCoverIndices(rows []splitRow, deletions, additions []int) bool {
	oldSeen, newSeen := make(map[int]bool, len(deletions)), make(map[int]bool, len(additions))
	for _, row := range rows {
		if len(row.oldIndices) > 0 {
			for _, index := range row.oldIndices {
				oldSeen[index] = true
			}
		} else if row.oldIndex >= 0 {
			oldSeen[row.oldIndex] = true
		}
		if len(row.newIndices) > 0 {
			for _, index := range row.newIndices {
				newSeen[index] = true
			}
		} else if row.newIndex >= 0 {
			newSeen[row.newIndex] = true
		}
	}
	for _, index := range deletions {
		if !oldSeen[index] {
			return false
		}
	}
	for _, index := range additions {
		if !newSeen[index] {
			return false
		}
	}
	return true
}

func applicableLayoutBlocks(lines []diff.Line, deletions, additions []int, blocks []semantic.LayoutBlock, oldStarts, newStarts []int) []layoutAnchor {
	oldPositions, newPositions := map[int]int{}, map[int]int{}
	for position, index := range deletions {
		oldPositions[lines[index].OldNumber] = position
	}
	for position, index := range additions {
		newPositions[lines[index].NewNumber] = position
	}
	var candidates []layoutAnchor
	for _, block := range blocks {
		oldFirst, oldLast := sourceRangeLineSpan(block.Old, oldStarts)
		newFirst, newLast := sourceRangeLineSpan(block.New, newStarts)
		oldStart, oldEnd, oldOK := overlappingChangedSpan(oldPositions, oldFirst, oldLast)
		newStart, newEnd, newOK := overlappingChangedSpan(newPositions, newFirst, newLast)
		if !oldOK || !newOK {
			continue
		}
		candidates = append(candidates, layoutAnchor{block: block, oldStart: oldStart, oldEnd: oldEnd, newStart: newStart, newEnd: newEnd})
	}
	// Prefer the most local owner when a parent and child overlap the same
	// changed rows. Parent blocks are useful only for changes not claimed by a
	// precise declaration/import owner.
	sort.SliceStable(candidates, func(i, j int) bool {
		left, right := candidates[i], candidates[j]
		if left.oldStart != right.oldStart {
			return left.oldStart < right.oldStart
		}
		if left.newStart != right.newStart {
			return left.newStart < right.newStart
		}
		leftWidth := (left.oldEnd - left.oldStart) + (left.newEnd - left.newStart)
		rightWidth := (right.oldEnd - right.oldStart) + (right.newEnd - right.newStart)
		if leftWidth != rightWidth {
			return leftWidth < rightWidth
		}
		leftBytes := (left.block.Old.End - left.block.Old.Start) + (left.block.New.End - left.block.New.Start)
		rightBytes := (right.block.Old.End - right.block.Old.Start) + (right.block.New.End - right.block.New.Start)
		if leftBytes != rightBytes {
			return leftBytes < rightBytes
		}
		return left.block.Confidence > right.block.Confidence
	})
	anchors := make([]layoutAnchor, 0, len(candidates))
	lastOld, lastNew := 0, 0
	for _, candidate := range candidates {
		if candidate.oldStart < lastOld || candidate.newStart < lastNew {
			continue
		}
		anchors = append(anchors, candidate)
		lastOld, lastNew = candidate.oldEnd, candidate.newEnd
	}
	return anchors
}

func sourceRangeLineSpan(span semantic.Range, starts []int) (int, int) {
	if span.End <= span.Start || len(starts) == 0 {
		return 0, -1
	}
	return sourceLineForOffset(span.Start, starts), sourceLineForOffset(span.End-1, starts)
}

func sourceLineForOffset(offset int, starts []int) int {
	line := sort.Search(len(starts), func(index int) bool { return starts[index] > offset })
	if line == 0 {
		return 1
	}
	return line
}

func overlappingChangedSpan(positions map[int]int, first, last int) (int, int, bool) {
	if first <= 0 || last < first {
		return 0, 0, false
	}
	start, end, count := int(^uint(0)>>1), -1, 0
	for line, position := range positions {
		if line < first || line > last {
			continue
		}
		start = min(start, position)
		end = max(end, position)
		count++
	}
	if count == 0 || end-start+1 != count {
		return 0, 0, false
	}
	return start, end + 1, true
}

func rowsFromLayoutBlock(lines []diff.Line, block semantic.LayoutBlock, oldByLine, newByLine map[int]int, oldStarts, newStarts []int) []splitRow {
	oldUses, newUses := map[int]int{}, map[int]int{}
	rows := make([]splitRow, 0, len(block.Rows))
	for _, projected := range block.Rows {
		row := splitRow{oldIndex: -1, newIndex: -1, metaIndex: -1, normalized: true}
		if projected.Old != nil {
			indices := changedIndicesForVirtualLine(projected.Old, oldStarts, oldByLine)
			if len(indices) > 0 {
				index := indices[0]
				line := lines[index]
				line.Text, line.Kind = projected.Old.Text, diff.Deletion
				if oldUses[index] > 0 {
					line.OldNumber = 0
				}
				oldUses[index]++
				row.old, row.oldIndex = &line, index
				row.oldIndices = indices
			}
		}
		if projected.New != nil {
			indices := changedIndicesForVirtualLine(projected.New, newStarts, newByLine)
			if len(indices) > 0 {
				index := indices[0]
				line := lines[index]
				line.Text, line.Kind = projected.New.Text, diff.Addition
				if newUses[index] > 0 {
					line.NewNumber = 0
				}
				newUses[index]++
				row.new, row.newIndex = &line, index
				row.newIndices = indices
			}
		}
		if row.old == nil && row.new == nil {
			continue
		}
		if projected.Kind == semantic.Unchanged && row.old != nil && row.new != nil {
			row.old.Kind, row.new.Kind = diff.Context, diff.Context
		} else if row.old != nil && row.new != nil {
			row.oldSpans, row.newSpans = intralineChanges(expandTabs(row.old.Text), expandTabs(row.new.Text))
		}
		rows = append(rows, row)
	}
	return rows
}

func changedIndicesForVirtualLine(line *semantic.VirtualLine, starts []int, byLine map[int]int) []int {
	if line == nil || line.End <= line.Start || len(starts) == 0 {
		return nil
	}
	first := sourceLineForOffset(line.Start, starts)
	last := sourceLineForOffset(line.End-1, starts)
	indices := make([]int, 0, last-first+1)
	for number := first; number <= last; number++ {
		if index, ok := byLine[number]; ok {
			indices = append(indices, index)
		}
	}
	return indices
}

func rawRowsForIndices(lines []diff.Line, deletions, additions []int) []splitRow {
	rows := make([]splitRow, 0, max(len(deletions), len(additions)))
	for index := 0; index < max(len(deletions), len(additions)); index++ {
		row := splitRow{oldIndex: -1, newIndex: -1, metaIndex: -1}
		if index < len(deletions) {
			line := lines[deletions[index]]
			row.old, row.oldIndex = &line, deletions[index]
		}
		if index < len(additions) {
			line := lines[additions[index]]
			row.new, row.newIndex = &line, additions[index]
		}
		rows = append(rows, row)
	}
	return rows
}
