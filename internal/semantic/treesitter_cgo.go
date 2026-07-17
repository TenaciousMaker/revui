//go:build cgo

package semantic

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	treesitter "github.com/tree-sitter/go-tree-sitter"
	typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

type treeSitterAdapter struct{}

func newTreeSitterAdapter() adapter { return treeSitterAdapter{} }

func (treeSitterAdapter) supports(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".ts", ".tsx", ".mts", ".cts":
		return true
	default:
		return false
	}
}

func (treeSitterAdapter) analyze(ctx context.Context, input Input) (Plan, error) {
	if err := ctx.Err(); err != nil {
		return Plan{}, err
	}
	language := treesitter.NewLanguage(typescript.LanguageTypescript())
	if strings.EqualFold(filepath.Ext(input.Path), ".tsx") {
		language = treesitter.NewLanguage(typescript.LanguageTSX())
	}
	oldEntries, err := parseTreeSitterEntries(input.Old, language)
	if err != nil {
		return Plan{}, fmt.Errorf("parse old source: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return Plan{}, err
	}
	newEntries, err := parseTreeSitterEntries(input.New, language)
	if err != nil {
		return Plan{}, fmt.Errorf("parse new source: %w", err)
	}
	oldRanges, newRanges, moves := compareEntries(oldEntries, newEntries)
	return Plan{Engine: EngineAST, Old: oldRanges, New: newRanges, Moves: moves}, nil
}

func parseTreeSitterEntries(source []byte, language *treesitter.Language) ([]entry, error) {
	if len(source) == 0 {
		return nil, nil
	}
	parser := treesitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(language); err != nil {
		return nil, err
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, fmt.Errorf("parser returned no tree")
	}
	defer tree.Close()
	root := tree.RootNode()
	if root == nil || root.HasError() {
		return nil, fmt.Errorf("source contains syntax errors")
	}
	var entries []entry
	collectTreeSitterEntries(root, source, &entries)
	return entries, nil
}

func collectTreeSitterEntries(node *treesitter.Node, source []byte, destination *[]entry) {
	if node == nil || node.EndByte() <= node.StartByte() {
		return
	}
	kind := node.Kind()
	if isAtomicTreeSitterNode(kind) || node.ChildCount() == 0 {
		start, end := int(node.StartByte()), int(node.EndByte())
		text := string(source[start:end])
		if strings.TrimSpace(text) != "" {
			key := text
			if kind == "comment" {
				key = normalizedComment(text)
			}
			*destination = append(*destination, entry{key: kind + "\x00" + key, start: start, end: end})
		}
		return
	}
	for index := uint(0); index < node.ChildCount(); index++ {
		collectTreeSitterEntries(node.Child(index), source, destination)
	}
}

func isAtomicTreeSitterNode(kind string) bool {
	switch kind {
	case "member_expression", "nested_identifier":
		return true
	default:
		return false
	}
}
