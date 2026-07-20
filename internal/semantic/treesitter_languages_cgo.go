//go:build cgo

package semantic

import (
	"path/filepath"
	"strings"
	"unsafe"

	treesitter "github.com/tree-sitter/go-tree-sitter"
	cgrammar "github.com/tree-sitter/tree-sitter-c/bindings/go"
	cppgrammar "github.com/tree-sitter/tree-sitter-cpp/bindings/go"
	gogrammar "github.com/tree-sitter/tree-sitter-go/bindings/go"
	javagrammar "github.com/tree-sitter/tree-sitter-java/bindings/go"
	javascriptgrammar "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	jsongrammar "github.com/tree-sitter/tree-sitter-json/bindings/go"
	pythongrammar "github.com/tree-sitter/tree-sitter-python/bindings/go"
	rubygrammar "github.com/tree-sitter/tree-sitter-ruby/bindings/go"
	rustgrammar "github.com/tree-sitter/tree-sitter-rust/bindings/go"
	typescriptgrammar "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

type treeSitterLanguage struct {
	language *treesitter.Language
	profile  layoutProfile
}

var builtInLanguageProfiles = languageProfiles()

func treeSitterLanguageForPath(path string) (treeSitterLanguage, bool) {
	extension := strings.ToLower(filepath.Ext(path))
	base := strings.ToLower(filepath.Base(path))
	var grammar unsafeLanguage
	profile := ""
	switch extension {
	case ".ts", ".mts", ".cts":
		grammar, profile = typescriptgrammar.LanguageTypescript, "typescript"
	case ".tsx":
		grammar, profile = typescriptgrammar.LanguageTSX, "typescript"
	case ".js", ".mjs", ".cjs", ".jsx":
		grammar, profile = javascriptgrammar.Language, "javascript"
	case ".go":
		grammar, profile = gogrammar.Language, "go"
	case ".py", ".pyi":
		grammar, profile = pythongrammar.Language, "python"
	case ".rs":
		grammar, profile = rustgrammar.Language, "rust"
	case ".java":
		grammar, profile = javagrammar.Language, "java"
	case ".json":
		grammar, profile = jsongrammar.Language, "json"
	case ".c":
		grammar, profile = cgrammar.Language, "c"
	case ".h":
		grammar, profile = cgrammar.Language, "c"
	case ".cc", ".cpp", ".cxx", ".hh", ".hpp", ".hxx":
		grammar, profile = cppgrammar.Language, "cpp"
	case ".rb", ".rake":
		grammar, profile = rubygrammar.Language, "ruby"
	default:
		if base == "gemfile" || base == "rakefile" {
			grammar, profile = rubygrammar.Language, "ruby"
		}
	}
	if grammar == nil || profile == "" {
		return treeSitterLanguage{}, false
	}
	return treeSitterLanguage{language: treesitter.NewLanguage(grammar()), profile: builtInLanguageProfiles[profile]}, true
}

// All official grammar bindings currently expose the same unsafe.Pointer
// constructor shape. Naming that shape keeps selection declarative without
// leaking grammar packages into the parser module.
type unsafeLanguage func() unsafe.Pointer
