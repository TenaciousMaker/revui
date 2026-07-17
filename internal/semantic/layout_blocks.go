package semantic

import (
	"sort"
	"strings"
	"unicode"
)

type layoutOwner struct {
	key, role string
	span      Range
	lines     []VirtualLine
}

// buildLayout is the sole structural-layout policy. It emits monotonic exact
// owner pairs and conservative same-role composites. Anything else is omitted
// and therefore remains a literal Git diff in the renderer.
func buildLayout(oldRoot, newRoot *syntaxNode, oldLines, newLines []VirtualLine) *Layout {
	oldOwners := collectLayoutOwners(oldRoot, oldLines)
	newOwners := collectLayoutOwners(newRoot, newLines)
	pairs := pairLayoutOwners(oldOwners, newOwners)
	if len(pairs) == 0 {
		return &Layout{}
	}
	blocks := make([]LayoutBlock, 0, len(pairs))
	for _, pair := range pairs {
		oldGroup, newGroup := oldOwners[pair.old:pair.oldEnd], newOwners[pair.new:pair.newEnd]
		blocks = append(blocks, LayoutBlock{
			Old:        Range{Start: oldGroup[0].span.Start, End: oldGroup[len(oldGroup)-1].span.End},
			New:        Range{Start: newGroup[0].span.Start, End: newGroup[len(newGroup)-1].span.End},
			Role:       oldGroup[0].role,
			Confidence: pair.confidence,
			Rows:       alignLayoutOwnerGroups(oldGroup, newGroup),
		})
	}
	return &Layout{Blocks: blocks}
}

func collectLayoutOwners(root *syntaxNode, lines []VirtualLine) []layoutOwner {
	var candidates []layoutOwner
	var walk func(*syntaxNode)
	walk = func(node *syntaxNode) {
		if node == nil {
			return
		}
		switch node.role {
		case "import_statement":
			if module := moduleSpecifier(node); module != "" {
				candidates = append(candidates, layoutOwner{key: node.role + "\x00" + module, role: node.role, span: node.span})
			}
		case "export_statement":
			if module := moduleSpecifier(node); module != "" {
				candidates = append(candidates, layoutOwner{key: node.role + "\x00" + module, role: node.role, span: node.span})
			} else if owner := directExportedLexicalOwner(node); owner != "" {
				// Keep the `export` prefix attached to exported const/let declarations.
				// Exported functions are deliberately not owners: their body is too
				// broad and is handled by its nested declaration owners instead.
				candidates = append(candidates, layoutOwner{key: node.role + "\x00" + owner, role: node.role, span: node.span})
			}
		case "lexical_declaration":
			if owner := lexicalDeclarationOwner(node); owner != "" {
				candidates = append(candidates, layoutOwner{key: owner, role: node.role, span: node.span})
			}
		case "jsx_self_closing_element", "jsx_element":
			// Exact parser fingerprints let an unchanged JSX subtree remain
			// aligned when conditional branches or parent wrappers are inserted
			// around it. JSX owners never participate in speculative matching.
			candidates = append(candidates, layoutOwner{key: node.role + "\x00" + node.fingerprint, role: node.role, span: node.span})
		}
		for _, child := range node.children {
			walk(child)
		}
	}
	walk(root)

	for _, line := range lines {
		if line.Start < 0 {
			continue
		}
		best, bestSize := -1, int(^uint(0)>>1)
		for index := range candidates {
			owner := candidates[index]
			if line.Start >= owner.span.Start && line.Start < owner.span.End {
				if strings.HasPrefix(owner.role, "jsx_") {
					// Nested JSX owners need a complete independent line set. A
					// parent subtree can cross a misleading Git context match even
					// when its atomic children cannot.
					candidates[index].lines = append(candidates[index].lines, line)
					continue
				}
				if size := owner.span.End - owner.span.Start; size < bestSize {
					best, bestSize = index, size
				}
			}
		}
		if best >= 0 {
			candidates[best].lines = append(candidates[best].lines, line)
		}
	}
	owners := candidates[:0]
	for _, owner := range candidates {
		if len(owner.lines) > 0 {
			owners = append(owners, owner)
		}
	}
	sort.Slice(owners, func(i, j int) bool {
		if owners[i].span.Start != owners[j].span.Start {
			return owners[i].span.Start < owners[j].span.Start
		}
		// Keep an outer exact subtree before its descendants on both sides.
		// This makes nested owner ordering deterministic for monotonic pairing.
		return owners[i].span.End > owners[j].span.End
	})
	return owners
}

type layoutOwnerPair struct {
	old, new       int
	oldEnd, newEnd int
	confidence     uint8
}

func pairLayoutOwners(oldOwners, newOwners []layoutOwner) []layoutOwnerPair {
	oldByKey, newByKey := map[string][]int{}, map[string][]int{}
	for index, owner := range oldOwners {
		oldByKey[owner.key] = append(oldByKey[owner.key], index)
	}
	for index, owner := range newOwners {
		newByKey[owner.key] = append(newByKey[owner.key], index)
	}
	var anchors []layoutOwnerPair
	for key, oldIndices := range oldByKey {
		newIndices := newByKey[key]
		if key != "" && len(oldIndices) == 1 && len(newIndices) == 1 && oldOwners[oldIndices[0]].role == newOwners[newIndices[0]].role {
			anchors = append(anchors, layoutOwnerPair{
				old: oldIndices[0], new: newIndices[0], oldEnd: oldIndices[0] + 1, newEnd: newIndices[0] + 1, confidence: 100,
			})
		}
	}
	sort.Slice(anchors, func(i, j int) bool { return anchors[i].old < anchors[j].old })
	monotonic := anchors[:0]
	lastNew := -1
	for _, anchor := range anchors {
		if anchor.new > lastNew {
			monotonic = append(monotonic, anchor)
			lastNew = anchor.new
		}
	}

	var result []layoutOwnerPair
	oldAt, newAt := 0, 0
	for _, anchor := range append(monotonic, layoutOwnerPair{old: len(oldOwners), new: len(newOwners)}) {
		oldCount, newCount := anchor.old-oldAt, anchor.new-newAt
		if oldCount > 0 && newCount > 0 && sameLayoutOwnerRole(oldOwners[oldAt:anchor.old], newOwners[newAt:anchor.new]) {
			switch {
			case oldCount == 1 && newCount == 1:
				result = append(result, layoutOwnerPair{old: oldAt, new: newAt, oldEnd: anchor.old, newEnd: anchor.new, confidence: 70})
			case oldCount == 1 || newCount == 1:
				result = append(result, layoutOwnerPair{old: oldAt, new: newAt, oldEnd: anchor.old, newEnd: anchor.new, confidence: 50})
			}
		}
		if anchor.old < len(oldOwners) && anchor.new < len(newOwners) {
			result = append(result, anchor)
		}
		oldAt, newAt = anchor.old+1, anchor.new+1
	}
	sort.Slice(result, func(i, j int) bool { return result[i].old < result[j].old })
	return result
}

func sameLayoutOwnerRole(oldOwners, newOwners []layoutOwner) bool {
	if len(oldOwners) == 0 || len(newOwners) == 0 {
		return false
	}
	role := oldOwners[0].role
	// JSX layout owners are identity anchors only. Similar-looking elements are
	// too easy to pair incorrectly, so they never participate in speculative
	// same-role replacement or composite matching.
	if strings.HasPrefix(role, "jsx_") {
		return false
	}
	for _, owner := range oldOwners {
		if owner.role != role {
			return false
		}
	}
	for _, owner := range newOwners {
		if owner.role != role {
			return false
		}
	}
	return true
}

func alignLayoutOwnerGroups(oldOwners, newOwners []layoutOwner) []LayoutRow {
	var rows []LayoutRow
	paired := min(len(oldOwners), len(newOwners))
	for index := 0; index < paired; index++ {
		rows = append(rows, alignLayoutLines(oldOwners[index].lines, newOwners[index].lines)...)
	}
	for _, owner := range oldOwners[paired:] {
		rows = append(rows, zipLayoutLines(owner.lines, nil)...)
	}
	for _, owner := range newOwners[paired:] {
		rows = append(rows, zipLayoutLines(nil, owner.lines)...)
	}
	return rows
}

func alignLayoutLines(oldLines, newLines []VirtualLine) []LayoutRow {
	matches := exactLayoutLineMatches(oldLines, newLines)
	var rows []LayoutRow
	oldAt, newAt := 0, 0
	for _, match := range append(matches, [2]int{len(oldLines), len(newLines)}) {
		rows = append(rows, alignChangedLayoutLines(oldLines[oldAt:match[0]], newLines[newAt:match[1]])...)
		if match[0] < len(oldLines) && match[1] < len(newLines) {
			oldLine, newLine := oldLines[match[0]], newLines[match[1]]
			rows = append(rows, LayoutRow{Old: linePointer(oldLine), New: linePointer(newLine), Kind: Unchanged})
		}
		oldAt, newAt = match[0]+1, match[1]+1
	}
	return rows
}

func alignChangedLayoutLines(oldLines, newLines []VirtualLine) []LayoutRow {
	if len(oldLines) == 0 || len(newLines) == 0 {
		return zipLayoutLines(oldLines, newLines)
	}
	bestOld, bestNew, bestScore := -1, -1, 0.0
	for oldIndex := range oldLines {
		for newIndex := range newLines {
			if score := layoutLineSimilarity(oldLines[oldIndex].Text, newLines[newIndex].Text); score > bestScore {
				bestOld, bestNew, bestScore = oldIndex, newIndex, score
			}
		}
	}
	if bestScore < 0.5 {
		return zipLayoutLines(oldLines, newLines)
	}
	rows := alignChangedLayoutLines(oldLines[:bestOld], newLines[:bestNew])
	oldLine, newLine := oldLines[bestOld], newLines[bestNew]
	rows = append(rows, LayoutRow{Old: linePointer(oldLine), New: linePointer(newLine), Kind: Replaced})
	return append(rows, alignChangedLayoutLines(oldLines[bestOld+1:], newLines[bestNew+1:])...)
}

func layoutLineSimilarity(oldText, newText string) float64 {
	oldTokens, newTokens := layoutLineTokens(oldText), layoutLineTokens(newText)
	if len(oldTokens) == 0 || len(newTokens) == 0 {
		return 0
	}
	counts := make(map[string]int, len(oldTokens))
	for _, token := range oldTokens {
		counts[token]++
	}
	common := 0
	for _, token := range newTokens {
		if counts[token] > 0 {
			counts[token]--
			common++
		}
	}
	return float64(2*common) / float64(len(oldTokens)+len(newTokens))
}

func layoutLineTokens(text string) []string {
	return strings.FieldsFunc(text, func(current rune) bool {
		return current != '_' && !unicode.IsLetter(current) && !unicode.IsDigit(current)
	})
}

func zipLayoutLines(oldLines, newLines []VirtualLine) []LayoutRow {
	rows := make([]LayoutRow, 0, max(len(oldLines), len(newLines)))
	for index := 0; index < max(len(oldLines), len(newLines)); index++ {
		row := LayoutRow{}
		if index < len(oldLines) {
			row.Old = linePointer(oldLines[index])
			row.Kind = Removed
		}
		if index < len(newLines) {
			row.New = linePointer(newLines[index])
			if row.Old == nil {
				row.Kind = Added
			} else if normalizedLayoutLineKey(row.Old.Text) == normalizedLayoutLineKey(row.New.Text) {
				row.Kind = Unchanged
			} else {
				row.Kind = Replaced
			}
		}
		rows = append(rows, row)
	}
	return rows
}

func exactLayoutLineMatches(oldLines, newLines []VirtualLine) [][2]int {
	table := make([][]uint16, len(oldLines)+1)
	for index := range table {
		table[index] = make([]uint16, len(newLines)+1)
	}
	for oldIndex := len(oldLines) - 1; oldIndex >= 0; oldIndex-- {
		for newIndex := len(newLines) - 1; newIndex >= 0; newIndex-- {
			if normalizedLayoutLineKey(oldLines[oldIndex].Text) == normalizedLayoutLineKey(newLines[newIndex].Text) {
				table[oldIndex][newIndex] = table[oldIndex+1][newIndex+1] + 1
			} else {
				table[oldIndex][newIndex] = max(table[oldIndex+1][newIndex], table[oldIndex][newIndex+1])
			}
		}
	}
	var matches [][2]int
	for oldIndex, newIndex := 0, 0; oldIndex < len(oldLines) && newIndex < len(newLines); {
		if normalizedLayoutLineKey(oldLines[oldIndex].Text) == normalizedLayoutLineKey(newLines[newIndex].Text) {
			matches = append(matches, [2]int{oldIndex, newIndex})
			oldIndex++
			newIndex++
		} else if table[oldIndex+1][newIndex] >= table[oldIndex][newIndex+1] {
			oldIndex++
		} else {
			newIndex++
		}
	}
	return matches
}

func normalizedLayoutLineKey(text string) string {
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(text), ","))
}

func linePointer(line VirtualLine) *VirtualLine {
	copy := line
	return &copy
}

func lexicalDeclarationOwner(node *syntaxNode) string {
	var callOwner func(*syntaxNode) string
	callOwner = func(current *syntaxNode) string {
		if current == nil {
			return ""
		}
		if current.role == "call_expression" && len(current.children) > 0 {
			return "callee\x00" + current.children[0].fingerprint
		}
		for _, child := range current.children {
			if owner := callOwner(child); owner != "" {
				return owner
			}
		}
		return ""
	}
	if binding := declarationBinding(node); binding != "" {
		if owner := callOwner(node); owner != "" {
			return "binding\x00" + binding + "\x00" + owner
		}
		return "binding\x00" + binding
	}
	if owner := callOwner(node); owner != "" {
		return owner
	}
	return ""
}

func declarationBinding(node *syntaxNode) string {
	if node == nil {
		return ""
	}
	if node.role == "variable_declarator" && len(node.children) > 0 {
		return firstIdentifier(node.children[0])
	}
	for _, child := range node.children {
		if binding := declarationBinding(child); binding != "" {
			return binding
		}
	}
	return ""
}

func firstIdentifier(node *syntaxNode) string {
	if node == nil {
		return ""
	}
	if node.kind == atomNode && strings.Contains(node.role, "identifier") {
		return node.content
	}
	for _, child := range node.children {
		if identifier := firstIdentifier(child); identifier != "" {
			return identifier
		}
	}
	return ""
}

func moduleSpecifier(node *syntaxNode) string {
	if node == nil {
		return ""
	}
	// In tree-sitter's import/export grammar, the module string is a direct
	// child. Descending into an exported declaration would mistake any string
	// in a function body for a module specifier and create a file-sized owner.
	for index := len(node.children) - 1; index >= 0; index-- {
		child := node.children[index]
		if child.role == "string" && child.content != "" {
			return child.content
		}
	}
	return ""
}

func directExportedLexicalOwner(node *syntaxNode) string {
	if node == nil {
		return ""
	}
	for _, child := range node.children {
		if child.role == "lexical_declaration" {
			return lexicalDeclarationOwner(child)
		}
	}
	return ""
}
