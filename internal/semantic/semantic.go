// Package semantic identifies meaningful source changes independently of Git's
// line-oriented presentation. Callers provide complete old/new source and
// receive immutable byte ranges; parser, matching, fallback, and caching detail
// remain inside this module.
package semantic

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"sync"
)

type Engine string

const (
	EngineToken Engine = "TOKEN*"
	EngineAST   Engine = "AST"

	maxSemanticSourceBytes = 2 << 20
)

type Range struct {
	Start int
	End   int
}

type Side uint8

const (
	OldSide Side = iota
	NewSide
)

// ChangeKind describes the relationship between an old and new source range.
// A zero range means that side does not exist (for example, an addition).
type ChangeKind string

const (
	Unchanged ChangeKind = "unchanged"
	Added     ChangeKind = "added"
	Removed   ChangeKind = "removed"
	Replaced  ChangeKind = "replaced"
	Ignored   ChangeKind = "ignored"
)

// Correspondence is the layout-neutral output of semantic matching. Ranges
// always address the original source buffers, even when a view inserts virtual
// whitespace. Confidence is 0-100 and Role is parser-provided, when known.
type Correspondence struct {
	Old, New   Range
	Kind       ChangeKind
	Confidence uint8
	Role       string
}

// VirtualLine is display-only source text with an origin range in the real
// source. Synthetic whitespace may change Text, but non-whitespace tokens are
// copied verbatim from [Start, End].
type VirtualLine struct {
	Text       string
	Start, End int
}

// LayoutRow is a source-preserving visual correspondence. Old and New always
// point into their original buffers; a nil side represents an insertion or
// deletion. Kind is one of Unchanged, Added, Removed, or Replaced.
type LayoutRow struct {
	Old, New *VirtualLine
	Kind     ChangeKind
}

// LayoutBlock is a confidently paired structural owner or a conservative
// same-role one-to-many composite. Other ambiguous rewrites are absent so
// callers render their literal diff.
type LayoutBlock struct {
	Old, New   Range
	Rows       []LayoutRow
	Confidence uint8
	Role       string
}

type Layout struct {
	Blocks []LayoutBlock
}

type Input struct {
	Path string
	Old  []byte
	New  []byte
}

// Plan is an immutable semantic diff. Old and New are sorted, non-overlapping
// byte ranges in their respective source buffers. Matching is order-sensitive:
// reordered syntax remains visible as removal and addition.
type Plan struct {
	Engine          Engine
	Old             []Range
	New             []Range
	Correspondences []Correspondence
	Layout          *Layout
	Warning         string
}

// ChangedRanges projects the richer correspondence model for renderers that
// only need emphasis spans. Old/New remain a compatibility fallback for plans
// produced by older adapters or simple test fakes.
func (p Plan) ChangedRanges(side Side) []Range {
	if len(p.Correspondences) == 0 {
		if side == OldSide {
			return p.Old
		}
		return p.New
	}
	var ranges []Range
	for _, pair := range p.Correspondences {
		if pair.Kind != Removed && pair.Kind != Added && pair.Kind != Replaced {
			continue
		}
		current := pair.New
		if side == OldSide {
			current = pair.Old
		}
		if current.End > current.Start {
			ranges = append(ranges, current)
		}
	}
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].Start < ranges[j].Start })
	merged := ranges[:0]
	for _, current := range ranges {
		merged = appendRange(merged, current)
	}
	return merged
}

type Analyzer interface {
	Analyze(context.Context, Input) (Plan, error)
}

type adapter interface {
	supports(string) bool
	analyze(context.Context, Input) (Plan, error)
}

type analyzer struct {
	ast      adapter
	token    adapter
	capacity int
	mu       sync.Mutex
	cache    map[string]*list.Element
	lru      *list.List
}

type cacheEntry struct {
	key  string
	plan Plan
}

// New returns the production analyzer. At most capacity immutable plans are
// retained; non-positive capacity disables caching.
func New(capacity int) Analyzer {
	return &analyzer{
		ast: newTreeSitterAdapter(), token: tokenAdapter{}, capacity: capacity,
		cache: make(map[string]*list.Element), lru: list.New(),
	}
}

func (a *analyzer) Analyze(ctx context.Context, input Input) (Plan, error) {
	if err := ctx.Err(); err != nil {
		return Plan{}, err
	}
	key := inputKey(input)
	if plan, ok := a.cached(key); ok {
		return plan, nil
	}
	var astErr error
	if a.ast.supports(input.Path) {
		plan, err := a.ast.analyze(ctx, input)
		if err == nil {
			a.store(key, plan)
			return plan, nil
		}
		astErr = err
	}
	plan, err := a.token.analyze(ctx, input)
	if err != nil {
		return Plan{}, err
	}
	if astErr != nil {
		plan.Warning = "AST analysis fell back to tokens: " + astErr.Error()
	}
	a.store(key, plan)
	return plan, nil
}

func inputKey(input Input) string {
	hash := sha256.New()
	hash.Write([]byte("semantic-v3\x00" + input.Path + "\x00"))
	hash.Write(input.Old)
	hash.Write([]byte{0})
	hash.Write(input.New)
	return hex.EncodeToString(hash.Sum(nil))
}

func (a *analyzer) cached(key string) (Plan, bool) {
	if a.capacity <= 0 {
		return Plan{}, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	element, ok := a.cache[key]
	if !ok {
		return Plan{}, false
	}
	a.lru.MoveToFront(element)
	return element.Value.(cacheEntry).plan, true
}

func (a *analyzer) store(key string, plan Plan) {
	if a.capacity <= 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if element, ok := a.cache[key]; ok {
		element.Value = cacheEntry{key: key, plan: plan}
		a.lru.MoveToFront(element)
		return
	}
	element := a.lru.PushFront(cacheEntry{key: key, plan: plan})
	a.cache[key] = element
	for a.lru.Len() > a.capacity {
		oldest := a.lru.Back()
		delete(a.cache, oldest.Value.(cacheEntry).key)
		a.lru.Remove(oldest)
	}
}
