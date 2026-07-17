package ui

import (
	"path/filepath"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

var apexLexer = chroma.MustNewLexer(
	&chroma.Config{
		Name:      "Salesforce Apex",
		Aliases:   []string{"apex", "salesforce-apex"},
		Filenames: []string{"*.cls", "*.trigger", "*.apex"},
	},
	func() chroma.Rules {
		return chroma.Rules{
			"root": {
				// revui highlights individual diff lines, so ApexDoc continuations
				// need to be recognised without relying on lexer state from a prior line.
				{Pattern: `^\s*/\*.*$`, Type: chroma.CommentMultiline},
				{Pattern: `^\s*\*.*$`, Type: chroma.CommentMultiline},
				{Pattern: `//.*$`, Type: chroma.CommentSingle},
				{Pattern: `/\*.*?\*/`, Type: chroma.CommentMultiline},
				{Pattern: `\s+`, Type: chroma.TextWhitespace},
				{Pattern: `@[A-Za-z_]\w*`, Type: chroma.NameDecorator},
				{Pattern: `'(?:\\.|[^'\\])*'`, Type: chroma.LiteralString},
				{Pattern: `"(?:\\.|[^"\\])*"`, Type: chroma.LiteralString},
				{Pattern: apexWords(
					"abstract", "break", "case", "catch", "class", "const", "continue",
					"default", "delete", "do", "else", "enum", "extends", "final", "finally",
					"for", "global", "if", "implements", "inherited", "insert", "instanceof",
					"interface", "merge", "new", "on", "override", "private", "protected",
					"public", "return", "sharing", "static", "super", "switch", "testmethod",
					"this", "throw", "transient", "trigger", "try", "undelete", "update",
					"upsert", "virtual", "webservice", "when", "while", "with", "without",
				), Type: chroma.Keyword},
				{Pattern: apexWords(
					"select", "from", "where", "with", "group", "having", "order", "by", "limit",
					"offset", "asc", "desc", "nulls", "first", "last", "for", "view", "reference",
					"tracking", "all", "rows", "typeof", "when", "then", "else", "end", "find",
					"in", "returning", "scope", "using", "division", "data", "category", "at", "above",
					"below", "above_or_below", "network", "metadata",
				), Type: chroma.KeywordNamespace},
				{Pattern: apexWords(
					"Blob", "Boolean", "Date", "Datetime", "Decimal", "Double", "Id", "Integer", "Long",
					"Object", "String", "Time", "List", "Map", "Set", "SObject", "AggregateResult",
					"Database", "System", "Math", "JSON", "JSONParser", "JSONGenerator", "Pattern",
					"Matcher", "Type", "Schema", "PageReference", "Exception", "DmlException",
				), Type: chroma.KeywordType},
				{Pattern: `(?i)\b(?:true|false|null)\b`, Type: chroma.KeywordConstant},
				{Pattern: `\b(?:0[xX][0-9a-fA-F]+|\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)\b`, Type: chroma.LiteralNumber},
				{Pattern: `[A-Za-z_]\w*(?=\s*\()`, Type: chroma.NameFunction},
				{Pattern: `[A-Z][A-Za-z0-9_]*(?:__c|__r|__mdt|__e)?`, Type: chroma.NameClass},
				{Pattern: `[A-Za-z_]\w*`, Type: chroma.Name},
				{Pattern: `(?:===|!==|==|!=|<=|>=|&&|\|\||\+\+|--|\+=|-=|\*=|/=|=>|[+\-*/%&|^!<>=?:])`, Type: chroma.Operator},
				{Pattern: `[()\[\]{},.;]`, Type: chroma.Punctuation},
				{Pattern: `.`, Type: chroma.Text},
			},
		}
	},
)

func apexWords(words ...string) string {
	return `(?i)\b(?:` + strings.Join(words, "|") + `)\b`
}

func lexerForFilename(filename string) chroma.Lexer {
	lower := strings.ToLower(filepath.Base(filename))
	ext := strings.ToLower(filepath.Ext(lower))

	switch ext {
	case ".cls", ".trigger", ".apex":
		return apexLexer
	case ".soql", ".sosl":
		return namedLexer("sql")
	case ".cmp", ".app", ".evt", ".intf", ".design", ".auradoc", ".tokens":
		return namedLexer("html")
	case ".page", ".component", ".email":
		return namedLexer("html")
	case ".object", ".permissionset", ".profile", ".labels", ".layout", ".flow",
		".flexipage", ".workflow", ".sharingrules", ".settings", ".translations",
		".globalvalueset", ".standardvalueset", ".quickaction", ".report", ".dashboard",
		".customapplication", ".custompermission", ".remotesite":
		return namedLexer("xml")
	}

	if lexer := lexers.Match(filename); lexer != nil {
		return lexer
	}
	return lexers.Fallback
}

func namedLexer(alias string) chroma.Lexer {
	if lexer := lexers.Get(alias); lexer != nil {
		return lexer
	}
	return lexers.Fallback
}
