//go:build cgo

package semantic

import (
	"context"
	"fmt"
	"strings"

	treesitter "github.com/tree-sitter/go-tree-sitter"
)

type treeSitterAdapter struct{}

const (
	maxSemanticNodes = 200_000
)

func newTreeSitterAdapter() adapter { return treeSitterAdapter{} }

func (treeSitterAdapter) supports(path string) bool {
	_, ok := treeSitterLanguageForPath(path)
	return ok
}

func (treeSitterAdapter) analyze(ctx context.Context, input Input) (Plan, error) {
	if err := ctx.Err(); err != nil {
		return Plan{}, err
	}
	if len(input.Old)+len(input.New) > maxSemanticSourceBytes {
		return Plan{}, fmt.Errorf("source exceeds %d MiB semantic budget", maxSemanticSourceBytes>>20)
	}
	language, ok := treeSitterLanguageForPath(input.Path)
	if !ok {
		return Plan{}, fmt.Errorf("unsupported tree-sitter language for %q", input.Path)
	}
	oldTree, oldLayout, err := parseTreeSitterSource(ctx, input.Old, language.language, language.profile)
	if err != nil {
		return Plan{}, fmt.Errorf("parse old source: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return Plan{}, err
	}
	newTree, newLayout, err := parseTreeSitterSource(ctx, input.New, language.language, language.profile)
	if err != nil {
		return Plan{}, fmt.Errorf("parse new source: %w", err)
	}
	oldRanges, newRanges, pairs, err := compareTrees(ctx, oldTree, newTree)
	if err != nil {
		return Plan{}, err
	}
	return Plan{
		Engine: EngineAST, Old: oldRanges, New: newRanges,
		Correspondences: pairs, Layout: buildLayout(oldTree, newTree, oldLayout, newLayout, language.profile),
	}, nil
}

func parseTreeSitterSource(ctx context.Context, source []byte, language *treesitter.Language, profile layoutProfile) (*syntaxNode, []VirtualLine, error) {
	if len(source) == 0 {
		root := &syntaxNode{kind: listNode, role: "root"}
		root.finish()
		return root, nil, nil
	}
	parser := treesitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(language); err != nil {
		return nil, nil, err
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, nil, fmt.Errorf("parser returned no tree")
	}
	defer tree.Close()
	root := tree.RootNode()
	if root == nil || root.HasError() {
		return nil, nil, fmt.Errorf("source contains syntax errors")
	}
	nodeCount := 0
	syntaxRoot, err := buildTreeSitterSyntax(ctx, root, source, &nodeCount, profile)
	if err != nil {
		return nil, nil, err
	}
	syntaxRoot.finish()
	var edits []layoutEdit
	collectTreeSitterLayoutEdits(root, source, 0, &edits, profile)
	layout := virtualLinesFromEdits(source, edits)
	return syntaxRoot, layout, nil
}

func buildTreeSitterSyntax(ctx context.Context, node *treesitter.Node, source []byte, count *int, profile layoutProfile) (*syntaxNode, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	*count++
	if *count > maxSemanticNodes {
		return nil, fmt.Errorf("syntax tree exceeds %d node budget", maxSemanticNodes)
	}
	start, end := int(node.StartByte()), int(node.EndByte())
	result := &syntaxNode{kind: listNode, role: node.Kind(), span: Range{Start: start, End: end}}
	if isAtomicTreeSitterNode(node.Kind()) || node.ChildCount() == 0 {
		result.kind = atomNode
		result.content = string(source[start:end])
		if node.Kind() == "comment" {
			result.content = normalizedComment(result.content)
		}
		return result, nil
	}
	for index := uint(0); index < node.ChildCount(); index++ {
		child := node.Child(index)
		if child == nil || child.EndByte() <= child.StartByte() {
			continue
		}
		if child.Kind() == "," && ignoresTrailingComma(profile, node.Kind()) && isTrailingComma(node, index) {
			continue
		}
		converted, err := buildTreeSitterSyntax(ctx, child, source, count, profile)
		if err != nil {
			return nil, err
		}
		result.children = append(result.children, converted)
	}
	return result, nil
}

func ignoresTrailingComma(profile layoutProfile, role string) bool {
	_, ok := profile.delimiters[role]
	return ok
}

func isTrailingComma(parent *treesitter.Node, comma uint) bool {
	for index := comma + 1; index < parent.ChildCount(); index++ {
		next := parent.Child(index)
		if next == nil {
			continue
		}
		switch next.Kind() {
		case "}", "]", ")":
			return true
		default:
			return false
		}
	}
	return false
}

func collectTreeSitterLayoutEdits(node *treesitter.Node, source []byte, minimumIndent int, edits *[]layoutEdit, profile layoutProfile) {
	if node == nil {
		return
	}
	if pattern, ok := profile.initializerPatterns[node.Kind()]; ok {
		appendPatternInitializerLayoutEdit(node, source, edits, pattern)
	}
	delimiter, target := profile.delimiters[node.Kind()]
	open, close := delimiter.open, delimiter.close
	target = target && layoutHasContent(node, source, open, close)
	childMinimum := minimumIndent
	if target {
		children := make([]*treesitter.Node, node.ChildCount())
		for index := range children {
			children[index] = node.Child(uint(index))
		}
		base := max(sourceIndent(source, int(node.StartByte())), minimumIndent)
		for index, child := range children {
			if child == nil || index+1 >= len(children) {
				continue
			}
			next := children[index+1]
			if next == nil {
				continue
			}
			if child.Kind() == open || child.Kind() == "," {
				indent := base + 2
				if next.Kind() == close {
					indent = base
				}
				appendWhitespaceLayoutEdit(source, int(child.EndByte()), int(next.StartByte()), indent, edits)
			}
			if next.Kind() == close && child.Kind() != open && child.Kind() != "," {
				appendWhitespaceLayoutEdit(source, int(child.EndByte()), int(next.StartByte()), base, edits)
			}
		}
		childMinimum = base + 2
	}
	for index := uint(0); index < node.ChildCount(); index++ {
		collectTreeSitterLayoutEdits(node.Child(index), source, childMinimum, edits, profile)
	}
}

func appendPatternInitializerLayoutEdit(node *treesitter.Node, source []byte, edits *[]layoutEdit, pattern initializerPattern) {
	children := make([]*treesitter.Node, node.ChildCount())
	hasPattern := false
	for index := range children {
		children[index] = node.Child(uint(index))
		if children[index] != nil && pattern.patterns[children[index].Kind()] {
			hasPattern = true
		}
	}
	if !hasPattern {
		return
	}
	for index, child := range children {
		if child == nil || !pattern.operators[child.Kind()] || index+1 >= len(children) || children[index+1] == nil {
			continue
		}
		appendInlineWhitespaceLayoutEdit(source, int(child.EndByte()), int(children[index+1].StartByte()), edits)
		return
	}
}

func layoutHasContent(node *treesitter.Node, source []byte, open, close string) bool {
	contentStart, contentEnd := -1, -1
	for index := uint(0); index < node.ChildCount(); index++ {
		child := node.Child(index)
		if child == nil {
			continue
		}
		if child.Kind() == open && contentStart < 0 {
			contentStart = int(child.EndByte())
			continue
		}
		if child.Kind() == close {
			contentEnd = int(child.StartByte())
		}
	}
	if contentStart < 0 || contentEnd < contentStart || contentEnd > len(source) {
		return false
	}
	return strings.TrimSpace(string(source[contentStart:contentEnd])) != ""
}

func appendWhitespaceLayoutEdit(source []byte, start, end, indent int, edits *[]layoutEdit) {
	if start > end || strings.TrimSpace(string(source[start:end])) != "" {
		return
	}
	*edits = append(*edits, layoutEdit{start: start, end: end, text: "\n" + strings.Repeat(" ", indent)})
}

func appendInlineWhitespaceLayoutEdit(source []byte, start, end int, edits *[]layoutEdit) {
	if start > end || strings.TrimSpace(string(source[start:end])) != "" {
		return
	}
	*edits = append(*edits, layoutEdit{start: start, end: end, text: " "})
}

func sourceIndent(source []byte, offset int) int {
	lineStart := offset
	for lineStart > 0 && source[lineStart-1] != '\n' {
		lineStart--
	}
	indent, position := 0, lineStart
	for position < len(source) {
		switch source[position] {
		case ' ':
			indent++
			position++
		case '\t':
			indent += 4
			position++
		default:
			return indent
		}
	}
	return indent
}

func isAtomicTreeSitterNode(kind string) bool {
	switch kind {
	case "member_expression", "nested_identifier", "string", "template_string":
		return true
	default:
		return false
	}
}
