package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/alecthomas/chroma/v2"
	uv "github.com/charmbracelet/ultraviolet"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/TenaciousMaker/revui/internal/diff"
)

func TestLexerForSalesforceFileTypes(t *testing.T) {
	tests := []struct {
		filename string
		wantName string
	}{
		{"classes/AccountService.cls", "Salesforce Apex"},
		{"triggers/Account.trigger", "Salesforce Apex"},
		{"scripts/check.apex", "Salesforce Apex"},
		{"classes/AccountService.cls-meta.xml", "XML"},
		{"objects/Account.object", "XML"},
		{"aura/Panel/Panel.cmp", "HTML"},
		{"queries/accounts.soql", "SQL"},
		{"internal/ui/model.go", "Go"},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			got := lexerForFilename(tt.filename).Config().Name
			if !strings.Contains(strings.ToLower(got), strings.ToLower(tt.wantName)) {
				t.Fatalf("lexerForFilename(%q) = %q, want a %q lexer", tt.filename, got, tt.wantName)
			}
		})
	}
}

func TestApexLexerRecognisesCommentsAnnotationsTypesAndSOQL(t *testing.T) {
	tests := []struct {
		source string
		value  string
		kind   chroma.TokenType
	}{
		{"/**", "/**", chroma.CommentMultiline},
		{" * Ranked-membership reads for WorkspaceRankedAccountReader.", " * Ranked-membership reads for WorkspaceRankedAccountReader.", chroma.CommentMultiline},
		{" */", " */", chroma.CommentMultiline},
		{"@NamespaceAccessible", "@NamespaceAccessible", chroma.NameDecorator},
		{"public class Reader {}", "public", chroma.Keyword},
		{"String whereClause;", "String", chroma.KeywordType},
		{"[SELECT Id FROM Account WHERE Name != null]", "SELECT", chroma.KeywordNamespace},
	}

	for _, tt := range tests {
		t.Run(tt.source, func(t *testing.T) {
			tokens, err := chroma.Tokenise(apexLexer, nil, tt.source)
			if err != nil {
				t.Fatal(err)
			}
			for _, token := range tokens {
				if token.Value == tt.value && token.Type == tt.kind {
					return
				}
			}
			t.Fatalf("did not find %q as %v in %#v", tt.value, tt.kind, tokens)
		})
	}
}

func TestHighlighterPreservesDiffBackgroundAcrossTokens(t *testing.T) {
	var h highlighter
	source := "public class Reader { String value; }"
	added := h.line("Reader.cls", source, addedLineBackground)
	deleted := h.line("Reader.cls", source, deletedLineBackground)

	if got := xansi.Strip(added); got != source {
		t.Fatalf("highlighted text = %q, want %q", got, source)
	}
	if added == deleted {
		t.Fatal("highlight cache reused output for a different diff background")
	}
	if strings.Count(added, "\x1b[0m") != 1 {
		t.Fatalf("token resets can expose the canvas background: %q", added)
	}
	for _, segment := range strings.Split(strings.TrimSuffix(added, "\x1b[0m"), "\x1b[") {
		if segment == "" {
			continue
		}
		if !strings.Contains(strings.SplitN(segment, "m", 2)[0], "48;2;31;42;36") {
			t.Fatalf("token is missing the added-line background: %q", segment)
		}
	}
}

func TestHighlighterDoesNotAddALineForTrailingCommentTokens(t *testing.T) {
	var h highlighter
	source := "// @vitest-environment jsdom"

	got := h.line("example.test.tsx", source, addedLineBackground)

	if strings.ContainsAny(got, "\r\n") {
		t.Fatalf("highlighted line contains a line break: %q", got)
	}
	if stripped := xansi.Strip(got); stripped != source {
		t.Fatalf("highlighted text = %q, want %q", stripped, source)
	}
}

func TestDiffHighlighterEmphasizesOnlyChangedTokens(t *testing.T) {
	var h highlighter
	oldText := "from package import get_pool"
	newText := "from package import get_pool, get_service_factory"
	file := diff.File{Path: "imports.py", Lines: []diff.Line{
		{Kind: diff.Deletion, Text: oldText, OldNumber: 1},
		{Kind: diff.Addition, Text: newText, NewNumber: 1},
	}}
	_, spans := intralineChanges(oldText, newText)
	highlighted := h.diffLine(&file, 1, newText, addedLineBackground, spans)
	buffer := uv.NewScreenBuffer(len([]rune(newText)), 1)
	uv.NewStyledString(highlighted).Draw(buffer, buffer.Bounds())

	assertCellBackground(t, buffer.CellAt(strings.Index(newText, "from"), 0), addedLineBackground)
	assertCellBackground(t, buffer.CellAt(strings.Index(newText, "get_service_factory"), 0), addedWordBackground)
}

func assertCellBackground(t *testing.T, cell *uv.Cell, want string) {
	t.Helper()
	if cell == nil || cell.Style.Bg == nil {
		t.Fatal("rendered syntax cell has no background colour")
	}
	red, green, blue, _ := cell.Style.Bg.RGBA()
	got := fmt.Sprintf("#%02X%02X%02X", red>>8, green>>8, blue>>8)
	if got != strings.ToUpper(want) {
		t.Fatalf("cell background = %s, want %s", got, want)
	}
}

func TestMarkdownStructuralMarkersUseMutedColour(t *testing.T) {
	var h highlighter
	file := diff.File{Path: "README.md", Lines: []diff.Line{
		{Kind: diff.Addition, Text: "- **from the repository**", NewNumber: 1},
		{Kind: diff.Addition, Text: "- **from a release archive**", NewNumber: 2},
	}}
	highlighted := h.diffLine(&file, 0, file.Lines[0].Text, addedLineBackground, nil)
	if xansi.Strip(highlighted) != file.Lines[0].Text {
		t.Fatalf("highlighted markdown changed text: %q", xansi.Strip(highlighted))
	}
	buffer := uv.NewScreenBuffer(32, 1)
	uv.NewStyledString(highlighted).Draw(buffer, buffer.Bounds())
	cell := buffer.CellAt(0, 0)
	if cell == nil || cell.Style.Fg == nil {
		t.Fatal("markdown list marker has no foreground colour")
	}
	red, green, blue, _ := cell.Style.Fg.RGBA()
	if got := fmt.Sprintf("#%02X%02X%02X", red>>8, green>>8, blue>>8); got != "#8B949E" {
		t.Fatalf("markdown list marker colour = %s, want muted #8B949E", got)
	}
}

func BenchmarkSyntaxHighlighting(b *testing.B) {
	var h highlighter
	source := "public static List<Account> find(Set<Id> ids) { return [SELECT Id, Name FROM Account WHERE Id IN :ids]; }"
	for index := 0; index < b.N; index++ {
		// Vary the line so the benchmark measures lexing and styling rather than only cache reads.
		h.line("AccountService.cls", fmt.Sprintf("%s // %d", source, index), addedLineBackground)
	}
}
