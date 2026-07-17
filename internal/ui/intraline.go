package ui

import (
	"unicode"

	"github.com/TenaciousMaker/revui/internal/diff"
)

type textSpan struct {
	start int
	end   int
}

type intralineToken struct {
	text       string
	start, end int
}

func intralineSpansForLine(lines []diff.Line, index int) []textSpan {
	if index < 0 || index >= len(lines) {
		return nil
	}
	return buildIntralineSpanSet(lines, true)[index]
}

func buildIntralineSpanSet(lines []diff.Line, semanticReflow bool) map[int][]textSpan {
	result := make(map[int][]textSpan)
	for start := 0; start < len(lines); {
		if !isChangedLine(lines[start]) {
			start++
			continue
		}
		hunk := lines[start].Hunk
		end := start + 1
		for end < len(lines) && lines[end].Hunk == hunk && isChangedLine(lines[end]) {
			end++
		}
		var deletions, additions []int
		for index := start; index < end; index++ {
			switch lines[index].Kind {
			case diff.Deletion:
				deletions = append(deletions, index)
			case diff.Addition:
				additions = append(additions, index)
			}
		}
		if len(deletions) > 0 && len(additions) > 0 && !semanticReflow {
			for offset := 0; offset < min(len(deletions), len(additions)); offset++ {
				oldIndex, newIndex := deletions[offset], additions[offset]
				oldSpans, newSpans := intralineChanges(expandTabs(lines[oldIndex].Text), expandTabs(lines[newIndex].Text))
				result[oldIndex], result[newIndex] = oldSpans, newSpans
			}
		} else if len(deletions) > 0 && len(additions) > 0 {
			oldText, oldRanges := flattenIntralineLines(lines, deletions)
			newText, newRanges := flattenIntralineLines(lines, additions)
			oldSpans, newSpans := intralineChanges(oldText, newText)
			for _, index := range deletions {
				result[index] = projectIntralineSpans(oldSpans, oldRanges[index])
			}
			for _, index := range additions {
				result[index] = projectIntralineSpans(newSpans, newRanges[index])
			}
		}
		start = end
	}
	return result
}

func isChangedLine(line diff.Line) bool {
	return line.Kind == diff.Addition || line.Kind == diff.Deletion
}

func flattenIntralineLines(lines []diff.Line, indices []int) (string, map[int]textSpan) {
	var flattened []rune
	ranges := make(map[int]textSpan, len(indices))
	for position, index := range indices {
		if position > 0 {
			flattened = append(flattened, '\n')
		}
		start := len(flattened)
		flattened = append(flattened, []rune(expandTabs(lines[index].Text))...)
		ranges[index] = textSpan{start: start, end: len(flattened)}
	}
	return string(flattened), ranges
}

func projectIntralineSpans(spans []textSpan, line textSpan) []textSpan {
	var projected []textSpan
	for _, span := range spans {
		start, end := max(span.start, line.start), min(span.end, line.end)
		if start < end {
			projected = append(projected, textSpan{start: start - line.start, end: end - line.start})
		}
	}
	return projected
}

func intralineChanges(oldText, newText string) ([]textSpan, []textSpan) {
	if oldText == newText {
		return nil, nil
	}
	oldTokens, newTokens := tokenizeIntraline(oldText), tokenizeIntraline(newText)
	if len(oldTokens) == 0 || len(newTokens) == 0 || len(oldTokens) > 256 || len(newTokens) > 256 {
		return prefixSuffixChanges(oldText, newText)
	}
	table := make([][]uint16, len(oldTokens)+1)
	for index := range table {
		table[index] = make([]uint16, len(newTokens)+1)
	}
	for oldIndex := len(oldTokens) - 1; oldIndex >= 0; oldIndex-- {
		for newIndex := len(newTokens) - 1; newIndex >= 0; newIndex-- {
			if oldTokens[oldIndex].text == newTokens[newIndex].text {
				table[oldIndex][newIndex] = table[oldIndex+1][newIndex+1] + 1
			} else {
				table[oldIndex][newIndex] = max(table[oldIndex+1][newIndex], table[oldIndex][newIndex+1])
			}
		}
	}
	oldMatched, newMatched := make([]bool, len(oldTokens)), make([]bool, len(newTokens))
	for oldIndex, newIndex := 0, 0; oldIndex < len(oldTokens) && newIndex < len(newTokens); {
		if oldTokens[oldIndex].text == newTokens[newIndex].text {
			oldMatched[oldIndex], newMatched[newIndex] = true, true
			oldIndex++
			newIndex++
		} else if table[oldIndex+1][newIndex] >= table[oldIndex][newIndex+1] {
			oldIndex++
		} else {
			newIndex++
		}
	}
	return unmatchedSpans(oldText, oldTokens, oldMatched), unmatchedSpans(newText, newTokens, newMatched)
}

func tokenizeIntraline(value string) []intralineToken {
	runes := []rune(value)
	var tokens []intralineToken
	for start := 0; start < len(runes); {
		kind := intralineRuneKind(runes[start])
		if kind == 0 {
			start++
			continue
		}
		end := start + 1
		if kind == 1 {
			for end < len(runes) && intralineRuneKind(runes[end]) == kind {
				end++
			}
			// Treat qualified names as one semantic token. Otherwise the dots
			// can match independently and fragment a single renamed reference.
			for end+1 < len(runes) && runes[end] == '.' && intralineRuneKind(runes[end+1]) == 1 {
				end += 2
				for end < len(runes) && intralineRuneKind(runes[end]) == 1 {
					end++
				}
			}
		}
		tokens = append(tokens, intralineToken{text: string(runes[start:end]), start: start, end: end})
		start = end
	}
	return tokens
}

func intralineRuneKind(value rune) int {
	if unicode.IsSpace(value) {
		return 0
	}
	if unicode.IsLetter(value) || unicode.IsDigit(value) || value == '_' {
		return 1
	}
	return 2
}

func unmatchedSpans(value string, tokens []intralineToken, matched []bool) []textSpan {
	runes := []rune(value)
	var spans []textSpan
	for index, token := range tokens {
		if matched[index] {
			continue
		}
		if len(spans) > 0 && whitespaceOnly(runes[spans[len(spans)-1].end:token.start]) {
			spans[len(spans)-1].end = token.end
		} else {
			spans = append(spans, textSpan{start: token.start, end: token.end})
		}
	}
	return spans
}

func whitespaceOnly(value []rune) bool {
	for _, current := range value {
		if !unicode.IsSpace(current) {
			return false
		}
	}
	return true
}

func prefixSuffixChanges(oldText, newText string) ([]textSpan, []textSpan) {
	oldRunes, newRunes := []rune(oldText), []rune(newText)
	prefix := 0
	for prefix < len(oldRunes) && prefix < len(newRunes) && oldRunes[prefix] == newRunes[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(oldRunes)-prefix && suffix < len(newRunes)-prefix && oldRunes[len(oldRunes)-1-suffix] == newRunes[len(newRunes)-1-suffix] {
		suffix++
	}
	return changedMiddle(prefix, len(oldRunes)-suffix), changedMiddle(prefix, len(newRunes)-suffix)
}

func changedMiddle(start, end int) []textSpan {
	if end <= start {
		return nil
	}
	return []textSpan{{start: start, end: end}}
}
