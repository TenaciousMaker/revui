package semantic

import (
	"sort"
	"strings"
)

type layoutEdit struct {
	start, end int
	text       string
}

func virtualLinesFromEdits(source []byte, edits []layoutEdit) []VirtualLine {
	sort.Slice(edits, func(i, j int) bool { return edits[i].start < edits[j].start })
	builder := virtualLineBuilder{start: -1, end: -1}
	cursor := 0
	for _, edit := range edits {
		if edit.start < cursor || edit.start < 0 || edit.end < edit.start || edit.end > len(source) {
			continue
		}
		builder.appendSource(source, cursor, edit.start)
		builder.appendSynthetic(edit.text)
		cursor = edit.end
	}
	builder.appendSource(source, cursor, len(source))
	builder.flush(false)
	return builder.lines
}

type virtualLineBuilder struct {
	text       strings.Builder
	start, end int
	lines      []VirtualLine
}

func (b *virtualLineBuilder) appendSource(source []byte, start, end int) {
	for start < end {
		newline := start
		for newline < end && source[newline] != '\n' {
			newline++
		}
		chunk := source[start:newline]
		b.text.Write(chunk)
		for index, value := range chunk {
			if value != ' ' && value != '\t' && value != '\r' {
				if b.start < 0 {
					b.start = start + index
				}
				b.end = start + index + 1
			}
		}
		if newline < end {
			b.flush(true)
			newline++
		}
		start = newline
	}
}

func (b *virtualLineBuilder) appendSynthetic(text string) {
	for {
		before, after, found := strings.Cut(text, "\n")
		b.text.WriteString(before)
		if !found {
			return
		}
		b.flush(true)
		text = after
	}
}

func (b *virtualLineBuilder) flush(force bool) {
	text := strings.TrimRight(b.text.String(), " \t\r")
	if force || text != "" || b.start >= 0 {
		b.lines = append(b.lines, VirtualLine{Text: text, Start: b.start, End: b.end})
	}
	b.text.Reset()
	b.start, b.end = -1, -1
}
