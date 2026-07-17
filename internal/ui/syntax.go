package ui

import (
	"fmt"
	"strings"
	"sync"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/styles"
)

type highlighter struct{ cache sync.Map }

func (m Model) highlightLine(filename, source, background string) string {
	if !m.theme.color || m.highlight == nil {
		return source
	}
	return m.highlight.line(filename, source, background)
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
	iterator, err := lexer.Tokenise(nil, source)
	if err != nil {
		return source
	}
	style := styles.Get("github-dark")
	if style == nil {
		return source
	}

	backgroundColour := chroma.ParseColour(strings.TrimPrefix(background, "#"))
	defaultColour := chroma.ParseColour("c9d1d9")
	var output strings.Builder
	for token := iterator(); token != chroma.EOF; token = iterator() {
		entry := style.Get(token.Type)
		foreground := entry.Colour
		if !foreground.IsSet() {
			foreground = defaultColour
		}
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
		if backgroundColour.IsSet() {
			params = append(params, rgbANSI("48", backgroundColour))
		}
		output.WriteString("\x1b[")
		output.WriteString(strings.Join(params, ";"))
		output.WriteByte('m')
		output.WriteString(token.Value)
	}
	output.WriteString("\x1b[0m")
	result := strings.TrimSuffix(output.String(), "\n")
	h.cache.Store(key, result)
	return result
}

func rgbANSI(channel string, colour chroma.Colour) string {
	return fmt.Sprintf("%s;2;%d;%d;%d", channel, colour.Red(), colour.Green(), colour.Blue())
}
