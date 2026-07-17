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
	"sync"
)

type Engine string

const (
	EngineToken Engine = "TOKEN*"
	EngineAST   Engine = "AST"
)

type Range struct {
	Start int
	End   int
}

type Move struct {
	Old Range
	New Range
}

type Input struct {
	Path string
	Old  []byte
	New  []byte
}

// Plan is an immutable semantic diff. Old and New are sorted, non-overlapping
// byte ranges in their respective source buffers. Moves pair content that is
// equal but changed position.
type Plan struct {
	Engine  Engine
	Old     []Range
	New     []Range
	Moves   []Move
	Warning string
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
	hash.Write([]byte("semantic-v1\x00" + input.Path + "\x00"))
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
