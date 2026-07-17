package ui

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/styles"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/TenaciousMaker/revui/internal/diff"
)

type highlighter struct {
	cache         sync.Map
	diffDocuments sync.Map
}

type indexedSyntaxLine struct {
	index int
	text  string
}

type diffSyntaxDocument struct {
	oldLines map[int][]chroma.Token
	newLines map[int][]chroma.Token
}

func (m Model) highlightLine(filename, source, background string) string {
	if !m.theme.color || m.highlight == nil {
		return source
	}
	return m.highlight.line(filename, source, background)
}

func (m Model) highlightDiffLine(index int, source, background string, spans []textSpan) string {
	if !m.theme.color || m.highlight == nil || m.repo == nil || m.file < 0 || m.file >= len(m.repo.Files) {
		return source
	}
	return m.highlight.diffLine(&m.repo.Files[m.file], index, source, background, spans)
}

func (m Model) highlightVirtualDiffLine(source, background string, spans []textSpan) string {
	if !m.theme.color || m.highlight == nil {
		return source
	}
	return m.highlight.virtualLine(m.currentPath(), source, background, spans)
}

func (m Model) highlightVirtualSyntax(tokens []chroma.Token, background string, spans []textSpan) string {
	if !m.theme.color || m.highlight == nil {
		var plain strings.Builder
		for _, token := range tokens {
			plain.WriteString(token.Value)
		}
		return plain.String()
	}
	return styleSyntaxTokens(tokens, background, m.currentPath(), spans)
}

func (h *highlighter) line(filename, source, background string) string {
	if source == "" {
		return ""
	}
	key := filename + "\x00" + background + "\x00" + source
	if cached, ok := h.cache.Load(key); ok {
		return cached.(string)
	}
	lexer := lexerForFilename(filename)
	lexer = chroma.Coalesce(lexer)
	iterator, err := lexer.Tokenise(nil, source+"\n")
	if err != nil {
		return source
	}
	var tokens []chroma.Token
	for token := iterator(); token != chroma.EOF; token = iterator() {
		tokens = append(tokens, token)
	}
	result := styleSyntaxTokens(tokens, background, filename, nil)
	h.cache.Store(key, result)
	return result
}

func (h *highlighter) virtualLine(filename, source, background string, spans []textSpan) string {
	if source == "" {
		return ""
	}
	var key strings.Builder
	key.WriteString(filename + "\x00virtual\x00" + background + "\x00" + source)
	for _, span := range spans {
		key.WriteByte('\x00')
		key.WriteString(strconv.Itoa(span.start))
		key.WriteByte(':')
		key.WriteString(strconv.Itoa(span.end))
	}
	cacheKey := key.String()
	if cached, ok := h.cache.Load(cacheKey); ok {
		return cached.(string)
	}
	lexer := chroma.Coalesce(lexerForFilename(filename))
	iterator, err := lexer.Tokenise(nil, source+"\n")
	if err != nil {
		return source
	}
	var tokens []chroma.Token
	for token := iterator(); token != chroma.EOF; token = iterator() {
		tokens = append(tokens, token)
	}
	result := styleSyntaxTokens(tokens, background, filename, spans)
	h.cache.Store(cacheKey, result)
	return result
}

func (h *highlighter) diffLine(file *diff.File, index int, source, background string, spans []textSpan) string {
	if file == nil || index < 0 || index >= len(file.Lines) {
		return h.line("", source, background)
	}
	cached, ok := h.diffDocuments.Load(file)
	if !ok {
		document := buildDiffSyntaxDocument(file)
		cached, _ = h.diffDocuments.LoadOrStore(file, document)
	}
	document := cached.(*diffSyntaxDocument)
	line := file.Lines[index]
	tokens := document.newLines[index]
	if line.Kind == diff.Deletion {
		tokens = document.oldLines[index]
	}
	if len(tokens) == 0 {
		return h.line(file.Path, source, background)
	}
	highlighted := styleSyntaxTokens(tokens, background, file.Path, spans)
	width := xansi.StringWidth(source)
	if xansi.StringWidth(highlighted) > width {
		tail := ""
		if strings.HasSuffix(source, "…") {
			tail = "…"
		}
		highlighted = xansi.Truncate(highlighted, width, tail)
	}
	return highlighted
}

func buildDiffSyntaxDocument(file *diff.File) *diffSyntaxDocument {
	document := &diffSyntaxDocument{oldLines: map[int][]chroma.Token{}, newLines: map[int][]chroma.Token{}}
	var oldSegment, newSegment []indexedSyntaxLine
	flush := func() {
		tokeniseSyntaxSegment(file.Path, oldSegment, document.oldLines)
		tokeniseSyntaxSegment(file.Path, newSegment, document.newLines)
		oldSegment = nil
		newSegment = nil
	}
	for index, line := range file.Lines {
		if line.Kind == diff.Meta {
			flush()
			continue
		}
		text := expandTabs(line.Text)
		if line.Kind != diff.Addition {
			oldSegment = append(oldSegment, indexedSyntaxLine{index: index, text: text})
		}
		if line.Kind != diff.Deletion {
			newSegment = append(newSegment, indexedSyntaxLine{index: index, text: text})
		}
	}
	flush()
	return document
}

func tokeniseSyntaxSegment(filename string, lines []indexedSyntaxLine, destination map[int][]chroma.Token) {
	if len(lines) == 0 {
		return
	}
	sources := make([]string, len(lines))
	for index, line := range lines {
		sources[index] = line.text
	}
	lexer := chroma.Coalesce(lexerForFilename(filename))
	iterator, err := lexer.Tokenise(nil, strings.Join(sources, "\n")+"\n")
	if err != nil {
		return
	}
	lineIndex := 0
	for token := iterator(); token != chroma.EOF && lineIndex < len(lines); token = iterator() {
		value := strings.ReplaceAll(strings.ReplaceAll(token.Value, "\r\n", "\n"), "\r", "\n")
		parts := strings.Split(value, "\n")
		for partIndex, part := range parts {
			if part != "" && lineIndex < len(lines) {
				destination[lines[lineIndex].index] = append(destination[lines[lineIndex].index], chroma.Token{Type: token.Type, Value: part})
			}
			if partIndex < len(parts)-1 {
				lineIndex++
			}
		}
	}
}

func styleSyntaxTokens(tokens []chroma.Token, background, filename string, spans []textSpan) string {
	style := styles.Get("github-dark")
	if style == nil {
		var plain strings.Builder
		for _, token := range tokens {
			plain.WriteString(token.Value)
		}
		return plain.String()
	}
	backgroundColour := chroma.ParseColour(strings.TrimPrefix(background, "#"))
	defaultColour := chroma.ParseColour("c9d1d9")
	var output strings.Builder
	position := 0
	for _, token := range tokens {
		entry := style.Get(token.Type)
		if isMarkdownFilename(filename) && token.Type == chroma.Keyword {
			// Markdown uses Keyword for list, task, numbered-list, and quote
			// markers. Keep structure legible without borrowing deletion red.
			entry.Colour = chroma.ParseColour("8b949e")
		}
		foreground := entry.Colour
		if !foreground.IsSet() {
			foreground = defaultColour
		}
		value := []rune(strings.TrimRight(token.Value, "\r\n"))
		for start := 0; start < len(value); {
			emphasized := positionInSpans(position+start, spans)
			end := start + 1
			for end < len(value) && positionInSpans(position+end, spans) == emphasized {
				end++
			}
			segmentBackground := backgroundColour
			if emphasized {
				segmentBackground = chroma.ParseColour(strings.TrimPrefix(intralineBackground(background), "#"))
			}
			writeSyntaxSegment(&output, entry, foreground, segmentBackground, string(value[start:end]))
			start = end
		}
		position += len(value)
	}
	// Chroma may include the synthetic newline it uses to terminate a token
	// stream in the final token value. Keep this line-oriented API strictly
	// single-line so a highlighted source line cannot grow a rendered diff row.
	result := strings.TrimRight(output.String(), "\r\n") + "\x1b[0m"
	return result
}

func writeSyntaxSegment(output *strings.Builder, entry chroma.StyleEntry, foreground, background chroma.Colour, value string) {
	params := []string{"0"}
	if entry.Bold == chroma.Yes {
		params = append(params, "1")
	}
	if entry.Italic == chroma.Yes {
		params = append(params, "3")
	}
	if entry.Underline == chroma.Yes {
		params = append(params, "4")
	}
	params = append(params, rgbANSI("38", foreground))
	if background.IsSet() {
		params = append(params, rgbANSI("48", background))
	}
	output.WriteString("\x1b[")
	output.WriteString(strings.Join(params, ";"))
	output.WriteByte('m')
	output.WriteString(value)
}

func positionInSpans(position int, spans []textSpan) bool {
	for _, span := range spans {
		if position >= span.start && position < span.end {
			return true
		}
	}
	return false
}

func intralineBackground(background string) string {
	switch background {
	case addedLineBackground:
		return addedWordBackground
	case deletedLineBackground:
		return deletedWordBackground
	default:
		return background
	}
}

func isMarkdownFilename(filename string) bool {
	lower := strings.ToLower(filename)
	return strings.HasSuffix(lower, ".md") || strings.HasSuffix(lower, ".mkd") || strings.HasSuffix(lower, ".markdown")
}

func rgbANSI(channel string, colour chroma.Colour) string {
	return fmt.Sprintf("%s;2;%d;%d;%d", channel, colour.Red(), colour.Green(), colour.Blue())
}
