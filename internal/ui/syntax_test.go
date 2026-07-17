package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/alecthomas/chroma/v2"
	xansi "github.com/charmbracelet/x/ansi"
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

func BenchmarkSyntaxHighlighting(b *testing.B) {
	var h highlighter
	source := "public static List<Account> find(Set<Id> ids) { return [SELECT Id, Name FROM Account WHERE Id IN :ids]; }"
	for index := 0; index < b.N; index++ {
		// Vary the line so the benchmark measures lexing and styling rather than only cache reads.
		h.line("AccountService.cls", fmt.Sprintf("%s // %d", source, index), addedLineBackground)
	}
}
