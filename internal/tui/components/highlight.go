package components

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

type TokenType int

const (
	TokenDefault TokenType = iota
	TokenKeyword
	TokenString
	TokenComment
	TokenNumber
	TokenFunction
	TokenOperator
	TokenBuiltin
	TokenConstant
	TokenAnnotation
	TokenPreProc
)

type Token struct {
	Type    TokenType
	Content string
}

type LexRule struct {
	Type    TokenType
	Pattern *regexp.Regexp
}

type LanguageDefinition struct {
	Name      string
	Rules     []LexRule
	Keywords  []string
	Types     []string
	Constants []string
	Builtins  []string
}

var Languages = map[string]*LanguageDefinition{
	"go": {
		Name: "Go",
		Keywords: []string{
			"break", "case", "chan", "const", "continue", "default", "defer", "else",
			"fallthrough", "for", "func", "go", "goto", "if", "import", "interface",
			"map", "package", "range", "return", "select", "struct", "switch", "type", "var",
		},
		Types: []string{
			"bool", "byte", "complex64", "complex128", "error", "float32", "float64",
			"int", "int8", "int16", "int32", "int64", "rune", "string", "uint",
			"uint8", "uint16", "uint32", "uint64", "uintptr", "any", "comparable",
		},
		Constants: []string{"true", "false", "iota", "nil"},
		Builtins: []string{
			"append", "cap", "close", "complex", "copy", "delete", "imag", "len",
			"make", "new", "panic", "print", "println", "real", "recover",
		},
		Rules: []LexRule{
			{TokenComment, regexp.MustCompile(`//.*$`)},
			{TokenComment, regexp.MustCompile(`/\*[\s\S]*?\*/`)},
			{TokenString, regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)},
			{TokenString, regexp.MustCompile("`[^`]*`")},
			{TokenString, regexp.MustCompile(`'(?:[^'\\]|\\.)'`)},
			{TokenNumber, regexp.MustCompile(`\b0x[0-9a-fA-F]+\b|\b\d+\.?\d*\b`)},
			{TokenAnnotation, regexp.MustCompile(`@\w+`)},
			{TokenPreProc, regexp.MustCompile(`#\w+`)},
		},
	},
	"python": {
		Name: "Python",
		Keywords: []string{
			"and", "as", "assert", "async", "await", "break", "class", "continue",
			"def", "del", "elif", "else", "except", "finally", "for", "from",
			"global", "if", "import", "in", "is", "lambda", "nonlocal", "not",
			"or", "pass", "raise", "return", "try", "while", "with", "yield",
			"True", "False", "None",
		},
		Rules: []LexRule{
			{TokenComment, regexp.MustCompile(`#.*$`)},
			{TokenString, regexp.MustCompile(`"""[\s\S]*?"""`)},
			{TokenString, regexp.MustCompile(`'''[\s\S]*?'''`)},
			{TokenString, regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)},
			{TokenString, regexp.MustCompile(`'(?:[^'\\]|\\.)'`)},
			{TokenNumber, regexp.MustCompile(`\b0x[0-9a-fA-F]+\b|\b\d+\.?\d*\b`)},
		},
	},
	"javascript": {
		Name: "JavaScript",
		Keywords: []string{
			"async", "await", "break", "case", "catch", "class", "const", "continue",
			"debugger", "default", "delete", "do", "else", "export", "extends",
			"finally", "for", "function", "if", "import", "in", "instanceof",
			"let", "new", "of", "return", "static", "super", "switch", "this",
			"throw", "try", "typeof", "var", "void", "while", "with", "yield",
			"true", "false", "null", "undefined",
		},
		Rules: []LexRule{
			{TokenComment, regexp.MustCompile(`//.*$`)},
			{TokenComment, regexp.MustCompile(`/\*[\s\S]*?\*/`)},
			{TokenString, regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)},
			{TokenString, regexp.MustCompile("'(?:[^'\\\\]|\\\\.)*'")},
			{TokenString, regexp.MustCompile("`(?:[^`\\\\]|\\\\.)*`")},
			{TokenNumber, regexp.MustCompile(`\b0x[0-9a-fA-F]+\b|\b\d+\.?\d*\b`)},
		},
	},
	"bash": {
		Name:      "Bash",
		Keywords:  []string{"if", "then", "else", "elif", "fi", "case", "esac", "for", "while", "do", "done", "function", "return", "exit"},
		Constants: []string{"true", "false"},
		Rules: []LexRule{
			{TokenComment, regexp.MustCompile(`#.*$`)},
			{TokenString, regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)},
			{TokenNumber, regexp.MustCompile(`\$\{?\w+\}?|\b\d+\b`)},
		},
	},
}

var identifierPat = regexp.MustCompile(`^[a-zA-Z_]\w*`)
var operatorChars = map[rune]bool{
	'+': true, '-': true, '*': true, '/': true, '%': true, '=': true,
	'<': true, '>': true, '!': true, '&': true, '|': true, '^': true,
	'~': true, ':': true, ';': true, ',': true, '.': true, '(': true,
	')': true, '[': true, ']': true, '{': true, '}': true, '?': true,
	'@': true, '#': true, '$': true,
}

func getLanguage(lang string) *LanguageDefinition {
	lang = strings.ToLower(lang)
	if lang == "golang" {
		lang = "go"
	}
	if lang == "typescript" || lang == "ts" {
		lang = "javascript"
	}
	if def, ok := Languages[lang]; ok {
		return def
	}
	return Languages["go"]
}

func DetectLanguage(code string) string {
	code = strings.TrimSpace(code)
	if strings.Contains(code, "func ") && strings.Contains(code, "package ") {
		return "go"
	}
	if strings.Contains(code, "def ") || strings.Contains(code, "class ") {
		return "python"
	}
	if strings.Contains(code, "function ") || strings.Contains(code, "const ") || strings.Contains(code, "let ") {
		return "javascript"
	}
	if strings.HasPrefix(code, "#!/") || strings.HasPrefix(code, "echo ") {
		return "bash"
	}
	return "go"
}

func tokenizeLine(line string, lang string) []Token {
	def := getLanguage(lang)
	tokens := []Token{}
	runes := []rune(line)
	pos := 0

	for pos < len(runes) {
		matched := false
		remaining := string(runes[pos:])

		if m := matchRule(remaining, def.Rules, TokenComment); m != "" {
			tokens = append(tokens, Token{Type: TokenComment, Content: m})
			pos += utf8.RuneCountInString(m)
			matched = true
		} else if m := matchRule(remaining, def.Rules, TokenString); m != "" {
			tokens = append(tokens, Token{Type: TokenString, Content: m})
			pos += utf8.RuneCountInString(m)
			matched = true
		} else if m := matchKeyword(runes, pos, def.Keywords); m != "" {
			tokens = append(tokens, Token{Type: TokenKeyword, Content: m})
			pos += len(m)
			matched = true
		}

		if matched {
			continue
		}

		if op := string(runes[pos]); len(op) > 0 && operatorChars[rune(op[0])] {
			tokens = append(tokens, Token{Type: TokenOperator, Content: op})
			pos++
		} else {
			tokens = append(tokens, Token{Type: TokenDefault, Content: string(runes[pos])})
			pos++
		}
	}

	return tokens
}

func matchRule(s string, rules []LexRule, tokenType TokenType) string {
	for _, r := range rules {
		if r.Type == tokenType {
			if m := r.Pattern.FindStringIndex(s); m != nil && m[0] == 0 {
				return s[:m[1]]
			}
		}
	}
	return ""
}

func matchKeyword(runes []rune, pos int, keywords []string) string {
	for _, kw := range keywords {
		if hasKeywordByRune(runes, pos, kw) {
			return kw
		}
	}
	return ""
}

func hasKeywordByRune(runes []rune, pos int, kw string) bool {
	kwRunes := []rune(kw)
	if pos+len(kwRunes) > len(runes) {
		return false
	}
	for i := 0; i < len(kwRunes); i++ {
		if runes[pos+i] != kwRunes[i] {
			return false
		}
	}
	return true
}

func Tokenize(code string, lang string) []Token {
	tokens := []Token{}
	lines := strings.Split(code, "\n")
	for _, line := range lines {
		tokens = append(tokens, tokenizeLine(line, lang)...)
	}
	return tokens
}

func HighlightCode(code string, lang string) string {
	tokens := Tokenize(code, lang)
	var b strings.Builder
	for _, t := range tokens {
		b.WriteString(t.Content)
	}
	return b.String()
}

func HighlightCodeInline(code string, lang string) string {
	return HighlightCode(code, lang)
}
