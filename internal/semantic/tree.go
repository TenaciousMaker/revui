package semantic

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
)

type nodeKind uint8

const (
	atomNode nodeKind = iota
	listNode
)

// syntaxNode is the parser-neutral representation used by the matcher. It
// deliberately keeps only structure, semantic content and original positions.
type syntaxNode struct {
	kind        nodeKind
	role        string
	content     string
	span        Range
	children    []*syntaxNode
	fingerprint string
	weight      int
}

func (n *syntaxNode) finish() {
	hash := sha256.New()
	hash.Write([]byte{byte(n.kind)})
	hash.Write([]byte(n.role))
	hash.Write([]byte{0})
	hash.Write([]byte(n.content))
	n.weight = 1
	for _, child := range n.children {
		child.finish()
		hash.Write([]byte{0})
		hash.Write([]byte(child.fingerprint))
		n.weight += child.weight
	}
	n.fingerprint = hex.EncodeToString(hash.Sum(nil))
}

func (n *syntaxNode) leaves(destination *[]entry) {
	if n == nil {
		return
	}
	if n.kind == atomNode {
		*destination = append(*destination, entry{
			key: n.role + "\x00" + n.content, role: n.role,
			start: n.span.Start, end: n.span.End,
		})
		return
	}
	for _, child := range n.children {
		child.leaves(destination)
	}
}

type treeAnchor struct {
	old, new *syntaxNode
}

// compareTrees first anchors unique, unchanged subtrees and only runs the
// token matcher in the gaps. This keeps formatting-insensitive matching fast
// without making the renderer depend on parser-specific node types.
func compareTrees(ctx context.Context, oldRoot, newRoot *syntaxNode) (oldRanges, newRanges []Range, pairs []Correspondence, err error) {
	var oldEntries, newEntries []entry
	oldRoot.leaves(&oldEntries)
	newRoot.leaves(&newEntries)
	oldMatched, newMatched := make([]bool, len(oldEntries)), make([]bool, len(newEntries))

	anchors := unchangedTreeAnchors(oldRoot, newRoot)
	previousOld, previousNew := 0, 0
	for _, anchor := range anchors {
		if err := ctx.Err(); err != nil {
			return nil, nil, nil, err
		}
		oldStart, oldEnd := entryBounds(oldEntries, anchor.old.span)
		newStart, newEnd := entryBounds(newEntries, anchor.new.span)
		if oldStart < previousOld || newStart < previousNew {
			continue
		}
		matchSegment(oldEntries, newEntries, previousOld, oldStart, previousNew, newStart, oldMatched, newMatched)
		for index := oldStart; index < oldEnd; index++ {
			oldMatched[index] = true
		}
		for index := newStart; index < newEnd; index++ {
			newMatched[index] = true
		}
		pairs = append(pairs, Correspondence{
			Old: anchor.old.span, New: anchor.new.span, Kind: Unchanged,
			Confidence: 100, Role: anchor.old.role,
		})
		previousOld, previousNew = oldEnd, newEnd
	}
	matchSegment(oldEntries, newEntries, previousOld, len(oldEntries), previousNew, len(newEntries), oldMatched, newMatched)
	oldRanges, newRanges, leafPairs := classifyEntries(oldEntries, newEntries, oldMatched, newMatched)
	pairs = append(pairs, leafPairs...)
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].Old.Start != pairs[j].Old.Start {
			return pairs[i].Old.Start < pairs[j].Old.Start
		}
		return pairs[i].New.Start < pairs[j].New.Start
	})
	return oldRanges, newRanges, pairs, nil
}

func unchangedTreeAnchors(oldRoot, newRoot *syntaxNode) []treeAnchor {
	oldByHash, newByHash := map[string][]*syntaxNode{}, map[string][]*syntaxNode{}
	collectAnchorCandidates(oldRoot, oldByHash)
	collectAnchorCandidates(newRoot, newByHash)
	var candidates []treeAnchor
	for fingerprint, oldNodes := range oldByHash {
		newNodes := newByHash[fingerprint]
		if len(oldNodes) == 1 && len(newNodes) == 1 && oldNodes[0].role == newNodes[0].role {
			candidates = append(candidates, treeAnchor{old: oldNodes[0], new: newNodes[0]})
		}
	}
	// Prefer larger outer anchors, then keep a monotonic, non-overlapping set.
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].old.weight != candidates[j].old.weight {
			return candidates[i].old.weight > candidates[j].old.weight
		}
		return candidates[i].old.span.Start < candidates[j].old.span.Start
	})
	var selected []treeAnchor
	for _, candidate := range candidates {
		overlaps := false
		for _, existing := range selected {
			if rangesOverlap(candidate.old.span, existing.old.span) || rangesOverlap(candidate.new.span, existing.new.span) {
				overlaps = true
				break
			}
		}
		if !overlaps {
			selected = append(selected, candidate)
		}
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].old.span.Start < selected[j].old.span.Start })
	// Do not use reordered anchors for equality. Order changes stay visible as
	// ordinary removals and additions.
	monotonic := selected[:0]
	lastNew := -1
	for _, candidate := range selected {
		if candidate.new.span.Start > lastNew {
			monotonic = append(monotonic, candidate)
			lastNew = candidate.new.span.End
		}
	}
	return monotonic
}

func collectAnchorCandidates(node *syntaxNode, destination map[string][]*syntaxNode) {
	if node == nil {
		return
	}
	// Tiny punctuation-heavy nodes are poor anchors. Three leaves is enough to
	// anchor a call/member while avoiding a lone comma or identifier.
	if node.kind == listNode && node.weight >= 4 {
		destination[node.fingerprint] = append(destination[node.fingerprint], node)
	}
	for _, child := range node.children {
		collectAnchorCandidates(child, destination)
	}
}

func entryBounds(entries []entry, target Range) (int, int) {
	start := sort.Search(len(entries), func(index int) bool { return entries[index].end > target.Start })
	end := sort.Search(len(entries), func(index int) bool { return entries[index].start >= target.End })
	return start, end
}

func rangesOverlap(left, right Range) bool {
	return left.Start < right.End && right.Start < left.End
}
