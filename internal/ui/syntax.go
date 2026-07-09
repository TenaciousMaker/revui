package ui

import (
	"bytes"
	"strings"
	"sync"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

type highlighter struct{ cache sync.Map }

func (h *highlighter) line(filename, source string) string {
	if source == "" {
		return ""
	}
	key := filename + "\x00" + source
	if cached, ok := h.cache.Load(key); ok {
		return cached.(string)
	}
	lexer := lexers.Match(filename)
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)
	iterator, err := lexer.Tokenise(nil, source)
	if err != nil {
		return source
	}
	formatter := formatters.Get("terminal16m")
	style := styles.Get("github-dark")
	if formatter == nil || style == nil {
		return source
	}
	var output bytes.Buffer
	if err := formatter.Format(&output, style, iterator); err != nil {
		return source
	}
	result := strings.TrimSuffix(output.String(), "\n")
	h.cache.Store(key, result)
	return result
}
