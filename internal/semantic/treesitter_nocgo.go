//go:build !cgo

package semantic

import "context"

type unavailableTreeSitterAdapter struct{}

func newTreeSitterAdapter() adapter                       { return unavailableTreeSitterAdapter{} }
func (unavailableTreeSitterAdapter) supports(string) bool { return false }
func (unavailableTreeSitterAdapter) analyze(context.Context, Input) (Plan, error) {
	return Plan{}, nil
}
