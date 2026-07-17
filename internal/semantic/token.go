package semantic

import (
	"context"
	"strings"
	"unicode"
	"unicode/utf8"
)

type tokenAdapter struct{}

func (tokenAdapter) supports(string) bool { return true }

func (tokenAdapter) analyze(ctx context.Context, input Input) (Plan, error) {
	if err := ctx.Err(); err != nil {
		return Plan{}, err
	}
	oldRanges, newRanges, moves := compareEntries(tokenEntries(input.Old), tokenEntries(input.New))
	return Plan{Engine: EngineToken, Old: oldRanges, New: newRanges, Moves: moves}, nil
}

func tokenEntries(source []byte) []entry {
	var entries []entry
	for start := 0; start < len(source); {
		current, size := utf8.DecodeRune(source[start:])
		if unicode.IsSpace(current) {
			start += size
			continue
		}
		end := start + size
		key := ""
		switch {
		case current == '\'' || current == '"' || current == '`':
			end = quotedLiteralEnd(source, start, byte(current))
			key = "literal\x00" + string(source[start:end])
		case start+1 < len(source) && source[start] == '/' && source[start+1] == '/':
			end = start + 2
			for end < len(source) && source[end] != '\n' {
				end++
			}
			key = "comment\x00" + normalizedComment(string(source[start:end]))
		case start+1 < len(source) && source[start] == '/' && source[start+1] == '*':
			end = start + 2
			for end+1 < len(source) && !(source[end] == '*' && source[end+1] == '/') {
				end++
			}
			if end+1 < len(source) {
				end += 2
			} else {
				end = len(source)
			}
			key = "comment\x00" + normalizedComment(string(source[start:end]))
		case isIdentifierRune(current):
			for end < len(source) {
				next, nextSize := utf8.DecodeRune(source[end:])
				if !isIdentifierRune(next) {
					break
				}
				end += nextSize
			}
			for end < len(source) && source[end] == '.' {
				nextStart := end + 1
				if nextStart >= len(source) {
					break
				}
				next, nextSize := utf8.DecodeRune(source[nextStart:])
				if !isIdentifierRune(next) {
					break
				}
				end = nextStart + nextSize
				for end < len(source) {
					next, nextSize = utf8.DecodeRune(source[end:])
					if !isIdentifierRune(next) {
						break
					}
					end += nextSize
				}
			}
		}
		if key == "" {
			key = string(source[start:end])
		}
		entries = append(entries, entry{key: key, start: start, end: end})
		start = end
	}
	return entries
}

func quotedLiteralEnd(source []byte, start int, quote byte) int {
	escaped := false
	for end := start + 1; end < len(source); end++ {
		if escaped {
			escaped = false
			continue
		}
		if source[end] == '\\' {
			escaped = true
			continue
		}
		if source[end] == quote {
			return end + 1
		}
	}
	return len(source)
}

func normalizedComment(text string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "/*")
	text = strings.TrimSuffix(text, "*/")
	var words []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "//")
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "*"))
		words = append(words, strings.Fields(line)...)
	}
	return strings.Join(words, " ")
}

func isIdentifierRune(value rune) bool {
	return unicode.IsLetter(value) || unicode.IsDigit(value) || value == '_' || value == '$'
}
