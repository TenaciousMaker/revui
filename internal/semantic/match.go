package semantic

import "sort"

type entry struct {
	key        string
	role       string
	start, end int
}

func compareEntries(oldEntries, newEntries []entry) (oldRanges, newRanges []Range) {
	oldMatched, newMatched := matchEntries(oldEntries, newEntries)
	oldRanges, newRanges, _ = classifyEntries(oldEntries, newEntries, oldMatched, newMatched)
	return oldRanges, newRanges
}

func compareEntriesDetailed(oldEntries, newEntries []entry) (oldRanges, newRanges []Range, pairs []Correspondence) {
	oldMatched, newMatched := matchEntries(oldEntries, newEntries)
	return classifyEntries(oldEntries, newEntries, oldMatched, newMatched)
}

func classifyEntries(oldEntries, newEntries []entry, oldMatched, newMatched []bool) (oldRanges, newRanges []Range, pairs []Correspondence) {
	pairedOld, pairedNew := make([]bool, len(oldEntries)), make([]bool, len(newEntries))
	for oldIndex, oldEntry := range oldEntries {
		if oldMatched[oldIndex] {
			continue
		}
		best, bestDistance := -1, int(^uint(0)>>1)
		for newIndex, newEntry := range newEntries {
			if newMatched[newIndex] || pairedNew[newIndex] || oldEntry.role != newEntry.role {
				continue
			}
			distance := oldIndex - newIndex
			if distance < 0 {
				distance = -distance
			}
			if distance < bestDistance {
				best, bestDistance = newIndex, distance
			}
		}
		if best >= 0 && bestDistance <= 8 {
			pairedOld[oldIndex], pairedNew[best] = true, true
			pairs = append(pairs, Correspondence{
				Old:  Range{Start: oldEntry.start, End: oldEntry.end},
				New:  Range{Start: newEntries[best].start, End: newEntries[best].end},
				Kind: Replaced, Confidence: 70, Role: oldEntry.role,
			})
		}
	}
	for index, current := range oldEntries {
		if !oldMatched[index] {
			changed := Range{Start: current.start, End: current.end}
			oldRanges = appendRange(oldRanges, changed)
			if !pairedOld[index] {
				pairs = append(pairs, Correspondence{Old: changed, Kind: Removed, Confidence: 100, Role: current.role})
			}
		}
	}
	for index, current := range newEntries {
		if !newMatched[index] {
			changed := Range{Start: current.start, End: current.end}
			newRanges = appendRange(newRanges, changed)
			if !pairedNew[index] {
				pairs = append(pairs, Correspondence{New: changed, Kind: Added, Confidence: 100, Role: current.role})
			}
		}
	}
	oldRanges = canonicalRanges(oldRanges)
	newRanges = canonicalRanges(newRanges)
	return oldRanges, newRanges, pairs
}

func canonicalRanges(ranges []Range) []Range {
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].Start < ranges[j].Start })
	result := ranges[:0]
	for _, current := range ranges {
		result = appendRange(result, current)
	}
	return result
}

func matchEntries(oldEntries, newEntries []entry) ([]bool, []bool) {
	oldMatched := make([]bool, len(oldEntries))
	newMatched := make([]bool, len(newEntries))
	prefix := 0
	for prefix < len(oldEntries) && prefix < len(newEntries) && oldEntries[prefix].key == newEntries[prefix].key {
		oldMatched[prefix], newMatched[prefix] = true, true
		prefix++
	}
	oldEnd, newEnd := len(oldEntries), len(newEntries)
	for oldEnd > prefix && newEnd > prefix && oldEntries[oldEnd-1].key == newEntries[newEnd-1].key {
		oldEnd--
		newEnd--
		oldMatched[oldEnd], newMatched[newEnd] = true, true
	}
	matchSegment(oldEntries, newEntries, prefix, oldEnd, prefix, newEnd, oldMatched, newMatched)
	return oldMatched, newMatched
}

func matchSegment(oldEntries, newEntries []entry, oldStart, oldEnd, newStart, newEnd int, oldMatched, newMatched []bool) {
	n, m := oldEnd-oldStart, newEnd-newStart
	if n == 0 || m == 0 {
		return
	}
	// Exact dynamic matching gives excellent intraline results for ordinary
	// replacement blocks while the cap prevents formatter rewrites from
	// allocating an unbounded matrix.
	if n*m <= 1_000_000 {
		table := make([][]uint32, n+1)
		for index := range table {
			table[index] = make([]uint32, m+1)
		}
		for oldIndex := n - 1; oldIndex >= 0; oldIndex-- {
			for newIndex := m - 1; newIndex >= 0; newIndex-- {
				if oldEntries[oldStart+oldIndex].key == newEntries[newStart+newIndex].key {
					matched := table[oldIndex+1][newIndex+1] + entryMatchWeight(oldEntries[oldStart+oldIndex])
					table[oldIndex][newIndex] = max(matched, max(table[oldIndex+1][newIndex], table[oldIndex][newIndex+1]))
				} else {
					table[oldIndex][newIndex] = max(table[oldIndex+1][newIndex], table[oldIndex][newIndex+1])
				}
			}
		}
		for oldIndex, newIndex := 0, 0; oldIndex < n && newIndex < m; {
			matched := table[oldIndex+1][newIndex+1] + entryMatchWeight(oldEntries[oldStart+oldIndex])
			if oldEntries[oldStart+oldIndex].key == newEntries[newStart+newIndex].key && table[oldIndex][newIndex] == matched {
				oldMatched[oldStart+oldIndex], newMatched[newStart+newIndex] = true, true
				oldIndex++
				newIndex++
			} else if table[oldIndex+1][newIndex] >= table[oldIndex][newIndex+1] {
				oldIndex++
			} else {
				newIndex++
			}
		}
		return
	}
	matchUniqueAnchors(oldEntries, newEntries, oldStart, oldEnd, newStart, newEnd, oldMatched, newMatched)
}

func entryMatchWeight(current entry) uint32 {
	switch current.key {
	case ",", ";", ".", "token\x00,", "token\x00;", "token\x00.":
		return 1
	default:
		return 3
	}
}

type anchor struct{ old, new int }

func matchUniqueAnchors(oldEntries, newEntries []entry, oldStart, oldEnd, newStart, newEnd int, oldMatched, newMatched []bool) {
	oldPositions, newPositions := map[string][]int{}, map[string][]int{}
	for index := oldStart; index < oldEnd; index++ {
		oldPositions[oldEntries[index].key] = append(oldPositions[oldEntries[index].key], index)
	}
	for index := newStart; index < newEnd; index++ {
		newPositions[newEntries[index].key] = append(newPositions[newEntries[index].key], index)
	}
	var candidates []anchor
	for key, old := range oldPositions {
		if len(old) == 1 && len(newPositions[key]) == 1 {
			candidates = append(candidates, anchor{old: old[0], new: newPositions[key][0]})
		}
	}
	sortAnchors(candidates)
	anchors := longestIncreasingAnchors(candidates)
	previousOld, previousNew := oldStart, newStart
	for _, current := range anchors {
		matchSegment(oldEntries, newEntries, previousOld, current.old, previousNew, current.new, oldMatched, newMatched)
		oldMatched[current.old], newMatched[current.new] = true, true
		previousOld, previousNew = current.old+1, current.new+1
	}
	if len(anchors) > 0 {
		matchSegment(oldEntries, newEntries, previousOld, oldEnd, previousNew, newEnd, oldMatched, newMatched)
	}
}

func sortAnchors(anchors []anchor) {
	for index := 1; index < len(anchors); index++ {
		for position := index; position > 0 && anchors[position].old < anchors[position-1].old; position-- {
			anchors[position], anchors[position-1] = anchors[position-1], anchors[position]
		}
	}
}

func longestIncreasingAnchors(candidates []anchor) []anchor {
	if len(candidates) == 0 {
		return nil
	}
	length, previous, best := make([]int, len(candidates)), make([]int, len(candidates)), 0
	for index := range candidates {
		length[index], previous[index] = 1, -1
		for prior := 0; prior < index; prior++ {
			if candidates[prior].new < candidates[index].new && length[prior]+1 > length[index] {
				length[index], previous[index] = length[prior]+1, prior
			}
		}
		if length[index] > length[best] {
			best = index
		}
	}
	result := make([]anchor, length[best])
	for index := len(result) - 1; index >= 0; index-- {
		result[index] = candidates[best]
		best = previous[best]
	}
	return result
}

func appendRange(ranges []Range, incoming Range) []Range {
	if incoming.End <= incoming.Start {
		return ranges
	}
	if len(ranges) > 0 && incoming.Start <= ranges[len(ranges)-1].End {
		if incoming.End > ranges[len(ranges)-1].End {
			ranges[len(ranges)-1].End = incoming.End
		}
		return ranges
	}
	return append(ranges, incoming)
}
