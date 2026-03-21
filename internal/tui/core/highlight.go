package core

import (
	"regexp"
	"strings"
	"unicode"
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

type TokenStyle struct {
	Bold       bool
	Italic     bool
	Underline  bool
	Foreground string
	Background string
	Reset      bool
}

type Lexer struct {
	Lang  string
	Rules []LexRule
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
			"map", "package", "range", "return", "select", "struct", "switch", "type",
			"var",
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
		Types: []string{
			"int", "float", "str", "bool", "list", "dict", "tuple", "set",
			"frozenset", "bytes", "bytearray", "memoryview", "range", "type",
			"object", "Exception",
		},
		Constants: []string{"True", "False", "None", "NotImplemented", "Ellipsis"},
		Builtins: []string{
			"abs", "all", "any", "ascii", "bin", "bool", "breakpoint", "bytearray",
			"bytes", "callable", "chr", "classmethod", "compile", "complex",
			"delattr", "dict", "dir", "divmod", "enumerate", "eval", "exec",
			"filter", "float", "format", "frozenset", "getattr", "globals",
			"hasattr", "hash", "help", "hex", "id", "input", "int", "isinstance",
			"issubclass", "iter", "len", "list", "locals", "map", "max",
			"memoryview", "min", "next", "object", "oct", "open", "ord", "pow",
			"print", "property", "range", "repr", "reversed", "round", "set",
			"setattr", "slice", "sorted", "staticmethod", "str", "sum", "super",
			"tuple", "type", "vars", "zip",
		},
		Rules: []LexRule{
			{TokenComment, regexp.MustCompile(`#.*$`)},
			{TokenString, regexp.MustCompile(`"""[\s\S]*?"""`)},
			{TokenString, regexp.MustCompile(`'''[\s\S]*?'''`)},
			{TokenString, regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)},
			{TokenString, regexp.MustCompile(`'(?:[^'\\]|\\.)*'`)},
			{TokenNumber, regexp.MustCompile(`\b0x[0-9a-fA-F]+\b|\b\d+\.?\d*\b`)},
			{TokenAnnotation, regexp.MustCompile(`@\w+`)},
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
		Types: []string{
			"Array", "Boolean", "Date", "Error", "Function", "JSON", "Map",
			"Math", "Number", "Object", "Promise", "Proxy", "RegExp", "Set",
			"String", "Symbol", "WeakMap", "WeakSet",
		},
		Constants: []string{"true", "false", "null", "undefined", "NaN", "Infinity"},
		Builtins: []string{
			"Array", "Boolean", "console", "Date", "decodeURI", "decodeURIComponent",
			"encodeURI", "encodeURIComponent", "Error", "eval", "Function",
			"isFinite", "isNaN", "JSON", "Math", "Number", "Object", "parseFloat",
			"parseInt", "Promise", "Proxy", "Reflect", "RegExp", "Set", "String",
			"Symbol", "Map", "WeakMap", "WeakSet", "setTimeout", "setInterval",
		},
		Rules: []LexRule{
			{TokenComment, regexp.MustCompile(`//.*$`)},
			{TokenComment, regexp.MustCompile(`/\*[\s\S]*?\*/`)},
			{TokenString, regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)},
			{TokenString, regexp.MustCompile("'(?:[^'\\\\]|\\\\.)*'")},
			{TokenString, regexp.MustCompile("`(?:[^`\\\\]|\\\\.)*`")},
			{TokenNumber, regexp.MustCompile(`\b0x[0-9a-fA-F]+\b|\b\d+\.?\d*\b`)},
			{TokenAnnotation, regexp.MustCompile(`@\w+`)},
		},
	},

	"bash": {
		Name: "Bash",
		Keywords: []string{
			"if", "then", "else", "elif", "fi", "case", "esac", "for", "while",
			"until", "do", "done", "in", "function", "select", "time", "coproc",
			"return", "exit", "break", "continue", "local", "declare", "typeset",
			"readonly", "export", "unset", "shift", "source", "alias", "unalias",
			"eval", "exec", "trap", "true", "false",
		},
		Types:     []string{},
		Constants: []string{"true", "false"},
		Builtins: []string{
			"echo", "printf", "read", "cd", "pwd", "ls", "mkdir", "rmdir", "rm",
			"cp", "mv", "cat", "grep", "sed", "awk", "find", "sort", "uniq",
			"wc", "head", "tail", "cut", "paste", "xargs", "tee", "which",
			"whereis", "type", "command", "builtin", "history", "jobs", "fg",
			"bg", "wait", "kill", "sleep", "date", "bc", "expr",
		},
		Rules: []LexRule{
			{TokenComment, regexp.MustCompile(`#.*$`)},
			{TokenString, regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)},
			{TokenString, regexp.MustCompile("'(?:[^'\\\\]|'')*'")},
			{TokenNumber, regexp.MustCompile(`\$\{?\w+\}?|\b\d+\b`)},
			{TokenAnnotation, regexp.MustCompile(`\$\{?[a-zA-Z_][a-zA-Z0-9_]*\}?`)},
		},
	},

	"sql": {
		Name: "SQL",
		Keywords: []string{
			"SELECT", "FROM", "WHERE", "INSERT", "INTO", "VALUES", "UPDATE", "SET",
			"DELETE", "CREATE", "TABLE", "DROP", "ALTER", "INDEX", "VIEW",
			"JOIN", "LEFT", "RIGHT", "INNER", "OUTER", "CROSS", "ON", "AND", "OR",
			"NOT", "NULL", "IS", "IN", "LIKE", "BETWEEN", "EXISTS", "CASE",
			"WHEN", "THEN", "ELSE", "END", "ORDER", "BY", "ASC", "DESC",
			"GROUP", "HAVING", "LIMIT", "OFFSET", "UNION", "ALL", "DISTINCT",
			"AS", "INTO", "PRIMARY", "KEY", "FOREIGN", "REFERENCES", "CONSTRAINT",
			"DEFAULT", "CHECK", "UNIQUE", "CASCADE", "TRIGGER", "STORED", "PROCEDURE",
			"BEGIN", "COMMIT", "ROLLBACK", "TRANSACTION", "GRANT", "REVOKE",
		},
		Types: []string{
			"INT", "INTEGER", "BIGINT", "SMALLINT", "TINYINT", "FLOAT", "DOUBLE",
			"DECIMAL", "NUMERIC", "CHAR", "VARCHAR", "TEXT", "BLOB", "DATE",
			"TIME", "DATETIME", "TIMESTAMP", "BOOLEAN", "BOOL", "JSON", "UUID",
		},
		Constants: []string{"NULL", "TRUE", "FALSE", "UNKNOWN"},
		Builtins: []string{
			"COUNT", "SUM", "AVG", "MIN", "MAX", "COALESCE", "NULLIF", "CAST",
			"CONVERT", "SUBSTRING", "TRIM", "UPPER", "LOWER", "LENGTH", "NOW",
			"CURRENT_DATE", "CURRENT_TIME", "CURRENT_TIMESTAMP", "ROW_NUMBER",
			"RANK", "DENSE_RANK", "LEAD", "LAG", "FIRST_VALUE", "LAST_VALUE",
		},
		Rules: []LexRule{
			{TokenComment, regexp.MustCompile(`--.*$`)},
			{TokenComment, regexp.MustCompile(`/\*[\s\S]*?\*/`)},
			{TokenString, regexp.MustCompile(`'(?:[^']|'')*'`)},
			{TokenNumber, regexp.MustCompile(`\b\d+\.?\d*\b`)},
			{TokenAnnotation, regexp.MustCompile(`--.*$`)},
		},
	},

	"rust": {
		Name: "Rust",
		Keywords: []string{
			"as", "async", "await", "break", "const", "continue", "crate", "dyn",
			"else", "enum", "extern", "false", "fn", "for", "if", "impl", "in",
			"let", "loop", "match", "mod", "move", "mut", "pub", "ref", "return",
			"self", "Self", "static", "struct", "super", "trait", "true", "type",
			"unsafe", "use", "where", "while",
		},
		Types: []string{
			"i8", "i16", "i32", "i64", "i128", "isize", "u8", "u16", "u32", "u64",
			"u128", "usize", "f32", "f64", "bool", "char", "str", "String", "Vec",
			"Box", "Option", "Result", "HashMap", "HashSet",
		},
		Constants: []string{"true", "false", "None", "Some", "Ok", "Err"},
		Builtins: []string{
			"println", "print", "eprintln", "eprint", "format", "panic", "assert",
			"assert_eq", "assert_ne", "debug_assert", "vec", "box", "dbg", "todo",
			"unimplemented", "unreachable", "mem", "thread", "spawn", "sleep",
		},
		Rules: []LexRule{
			{TokenComment, regexp.MustCompile(`//.*$`)},
			{TokenComment, regexp.MustCompile(`/\*[\s\S]*?\*/`)},
			{TokenString, regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)},
			{TokenString, regexp.MustCompile("r#*\"[\\s\\S]*?\"#*")},
			{TokenString, regexp.MustCompile(`'(?:[^'\\]|\\.)'`)},
			{TokenNumber, regexp.MustCompile(`\b0x[0-9a-fA-F_]+\b|\b\d+\.?\d*\b`)},
			{TokenAnnotation, regexp.MustCompile(`#\[[\w\s:,]+\]`)},
			{TokenPreProc, regexp.MustCompile(`#\w+`)},
		},
	},

	"c": {
		Name: "C",
		Keywords: []string{
			"auto", "break", "case", "char", "const", "continue", "default", "do",
			"double", "else", "enum", "extern", "float", "for", "goto", "if",
			"inline", "int", "long", "register", "restrict", "return", "short",
			"signed", "sizeof", "static", "struct", "switch", "typedef", "union",
			"unsigned", "void", "volatile", "while", "_Bool", "_Complex", "_Imaginary",
		},
		Types: []string{
			"int8_t", "int16_t", "int32_t", "int64_t", "uint8_t", "uint16_t",
			"uint32_t", "uint64_t", "size_t", "ssize_t", "ptrdiff_t", "intptr_t",
			"uintptr_t", "FILE", "bool", "char16_t", "char32_t",
		},
		Constants: []string{"NULL", "true", "false"},
		Builtins: []string{
			"printf", "scanf", "malloc", "calloc", "realloc", "free", "sizeof",
			"memcpy", "memmove", "memset", "memcmp", "strlen", "strcpy", "strncpy",
			"strcat", "strncat", "strcmp", "strncmp", "fopen", "fclose", "fread",
			"fwrite", "fprintf", "fscanf", "sprintf", "sscanf",
		},
		Rules: []LexRule{
			{TokenComment, regexp.MustCompile(`//.*$`)},
			{TokenComment, regexp.MustCompile(`/\*[\s\S]*?\*/`)},
			{TokenString, regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)},
			{TokenString, regexp.MustCompile(`'(?:[^'\\]|\\.)'`)},
			{TokenNumber, regexp.MustCompile(`\b0x[0-9a-fA-F]+\b|\b\d+\.?\d*[fFlL]?\b`)},
			{TokenPreProc, regexp.MustCompile(`#\w+`)},
			{TokenAnnotation, regexp.MustCompile(`@\w+`)},
		},
	},

	"php": {
		Name: "PHP",
		Keywords: []string{
			"__halt_compiler", "abstract", "and", "array", "as", "break", "callable",
			"case", "catch", "class", "clone", "const", "continue", "declare",
			"default", "die", "do", "echo", "else", "elseif", "empty", "enddeclare",
			"endfor", "endforeach", "endif", "endswitch", "endwhile", "eval", "exit",
			"extends", "final", "finally", "fn", "for", "foreach", "function",
			"global", "goto", "if", "implements", "include", "include_once", "instanceof",
			"insteadof", "interface", "isset", "list", "match", "namespace", "new",
			"or", "print", "private", "protected", "public", "require", "require_once",
			"return", "static", "switch", "throw", "trait", "try", "unset", "use",
			"var", "while", "xor", "yield", "yield from",
			"true", "false", "null",
		},
		Types: []string{
			"array", "bool", "boolean", "float", "int", "integer", "object",
			"string", "iterable", "void", "mixed", "false", "true", "null",
		},
		Constants: []string{"true", "false", "null", "__FILE__", "__LINE__", "__DIR__"},
		Builtins: []string{
			"echo", "print", "var_dump", "var_export", "print_r", "isset", "unset",
			"empty", "include", "require", "include_once", "require_once", "header",
			"session_start", "setcookie", "strlen", "strpos", "str_replace",
			"substr", "explode", "implode", "array_map", "array_filter", "array_reduce",
			"count", "in_array", "array_search", "array_merge", "array_push",
		},
		Rules: []LexRule{
			{TokenComment, regexp.MustCompile(`//.*$`)},
			{TokenComment, regexp.MustCompile(`#.*$`)},
			{TokenComment, regexp.MustCompile(`/\*[\s\S]*?\*/`)},
			{TokenString, regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)},
			{TokenString, regexp.MustCompile(`'(?:[^'\\]|\\.)*'`)},
			{TokenString, regexp.MustCompile("`(?:[^`\\\\]|\\\\.)*`")},
			{TokenNumber, regexp.MustCompile(`\b0x[0-9a-fA-F]+\b|\b\d+\.?\d*\b`)},
			{TokenAnnotation, regexp.MustCompile(`\$\w+`)},
		},
	},

	"html": {
		Name: "HTML",
		Keywords: []string{
			"html", "head", "body", "div", "span", "p", "a", "img", "ul", "ol", "li",
			"table", "tr", "td", "th", "form", "input", "button", "select",
			"option", "textarea", "label", "script", "style", "link", "meta",
			"title", "header", "footer", "nav", "main", "section", "article",
			"aside", "h1", "h2", "h3", "h4", "h5", "h6", "br", "hr", "iframe",
			"canvas", "svg", "video", "audio", "source", "embed", "object",
			"param", "area", "map", "details", "summary", "dialog", "menu",
		},
		Types:     []string{},
		Constants: []string{},
		Builtins: []string{
			"class", "id", "href", "src", "alt", "title", "style", "type", "name",
			"value", "placeholder", "disabled", "required", "readonly", "selected",
		},
		Rules: []LexRule{
			{TokenComment, regexp.MustCompile(`<!--[\s\S]*?-->`)},
			{TokenString, regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)},
			{TokenString, regexp.MustCompile("'(?:[^'\\\\]|\\\\.)*'")},
			{TokenAnnotation, regexp.MustCompile(`<\w+`)},
			{TokenAnnotation, regexp.MustCompile(`</\w+>`)},
		},
	},

	"css": {
		Name: "CSS",
		Keywords: []string{
			"@charset", "@import", "@media", "@keyframes", "@font-face", "@supports",
			"@page", "@namespace", "@document", "@viewport", "@color-profile",
			"!important", "inherit", "initial", "unset", "revert", "auto", "none",
		},
		Types: []string{
			"color", "background", "background-color", "background-image", "font",
			"font-size", "font-family", "font-weight", "font-style", "margin",
			"padding", "border", "width", "height", "display", "position", "top",
			"left", "right", "bottom", "z-index", "float", "clear", "overflow",
			"visibility", "opacity", "transform", "transition", "animation",
		},
		Constants: []string{"inherit", "initial", "unset", "revert", "auto", "none"},
		Builtins: []string{
			"px", "em", "rem", "vw", "vh", "vmin", "vmax", "pt", "pc", "ex", "ch",
			"mm", "cm", "in", "deg", "rad", "grad", "turn", "ms", "s", "Hz", "kHz",
			"%",
		},
		Rules: []LexRule{
			{TokenComment, regexp.MustCompile(`/\*[\s\S]*?\*/`)},
			{TokenString, regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)},
			{TokenString, regexp.MustCompile("'(?:[^'\\\\]|\\\\.)*'")},
			{TokenNumber, regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b`)},
			{TokenNumber, regexp.MustCompile(`\b\d+\.?\d*(?:px|em|rem|vw|vh|%|ms|s)?\b`)},
			{TokenAnnotation, regexp.MustCompile(`\.\w+`)},
			{TokenAnnotation, regexp.MustCompile(`#\w+`)},
		},
	},
}

func getLanguage(lang string) *LanguageDefinition {
	lang = strings.ToLower(lang)
	if lang == "golang" {
		lang = "go"
	}
	if lang == "typescript" || lang == "ts" {
		lang = "javascript"
	}
	if lang == "py" {
		lang = "python"
	}
	if lang == "sh" || lang == "shell" {
		lang = "bash"
	}
	if lang == "c++" || lang == "cpp" {
		lang = "c"
	}
	if lang == "rb" {
		lang = "ruby"
	}
	if lang == "yml" || lang == "yaml" {
		lang = "yaml"
	}
	if lang == "md" {
		lang = "markdown"
	}

	if def, ok := Languages[lang]; ok {
		return def
	}
	return Languages["go"]
}

// Tokenize 按指定语言将代码拆分为高亮所需的标记序列。
func Tokenize(code string, lang string) []Token {
	tokens := []Token{}
	lines := strings.Split(code, "\n")

	for _, line := range lines {
		lineTokens := tokenizeLine(line, lang)
		tokens = append(tokens, lineTokens...)
		tokens = append(tokens, Token{Type: TokenDefault, Content: "\n"})
	}

	if len(tokens) > 0 {
		tokens = tokens[:len(tokens)-1]
	}

	return tokens
}

func tokenizeLine(line string, lang string) []Token {
	def := getLanguage(lang)
	tokens := []Token{}

	runes := []rune(line)
	pos := 0
	for pos < len(runes) {
		matched := false

		for _, rule := range def.Rules {
			if rule.Type == TokenComment {
				m := rule.Pattern.FindStringIndex(string(runes[pos:]))
				if m != nil && m[0] == 0 {
					content := string(runes[pos:])[:m[1]]
					tokens = append(tokens, Token{Type: rule.Type, Content: content})
					pos += runeLen(content)
					matched = true
					break
				}
			}
		}
		if matched {
			continue
		}

		for _, rule := range def.Rules {
			if rule.Type == TokenString {
				m := rule.Pattern.FindStringIndex(string(runes[pos:]))
				if m != nil && m[0] == 0 {
					content := string(runes[pos:])[:m[1]]
					tokens = append(tokens, Token{Type: rule.Type, Content: content})
					pos += runeLen(content)
					matched = true
					break
				}
			}
		}
		if matched {
			continue
		}

		for _, rule := range def.Rules {
			if rule.Type == TokenNumber || rule.Type == TokenAnnotation || rule.Type == TokenPreProc {
				m := rule.Pattern.FindStringIndex(string(runes[pos:]))
				if m != nil && m[0] == 0 {
					content := string(runes[pos:])[:m[1]]
					tokens = append(tokens, Token{Type: rule.Type, Content: content})
					pos += runeLen(content)
					matched = true
					break
				}
			}
		}
		if matched {
			continue
		}

		for _, kw := range def.Keywords {
			if hasKeywordByRune(runes, pos, kw) {
				tokens = append(tokens, Token{Type: TokenKeyword, Content: kw})
				pos += len(kw)
				matched = true
				break
			}
		}
		if matched {
			continue
		}

		for _, kw := range def.Constants {
			if hasKeywordByRune(runes, pos, kw) {
				tokens = append(tokens, Token{Type: TokenConstant, Content: kw})
				pos += len(kw)
				matched = true
				break
			}
		}
		if matched {
			continue
		}

		for _, kw := range def.Types {
			if hasKeywordByRune(runes, pos, kw) {
				tokens = append(tokens, Token{Type: TokenBuiltin, Content: kw})
				pos += len(kw)
				matched = true
				break
			}
		}
		if matched {
			continue
		}

		for _, kw := range def.Builtins {
			if hasKeywordByRune(runes, pos, kw) {
				tokens = append(tokens, Token{Type: TokenFunction, Content: kw})
				pos += len(kw)
				matched = true
				break
			}
		}
		if matched {
			continue
		}

		m := regexp.MustCompile(`^[a-zA-Z_]\w*`).FindString(string(runes[pos:]))
		if m != "" {
			afterPos := pos + runeLen(m)
			if afterPos < len(runes) && string(runes[afterPos]) == "(" {
				tokens = append(tokens, Token{Type: TokenFunction, Content: m})
				pos = afterPos
				continue
			}
		}

		op := string(runes[pos])
		if isOperator(op) {
			tokens = append(tokens, Token{Type: TokenOperator, Content: op})
			pos++
			continue
		}

		if unicode.IsSpace(runes[pos]) {
			tokens = append(tokens, Token{Type: TokenDefault, Content: string(runes[pos])})
			pos++
			continue
		}

		tokens = append(tokens, Token{Type: TokenDefault, Content: string(runes[pos])})
		pos++
	}

	return tokens
}

func runeLen(s string) int {
	return len([]rune(s))
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
	beforeOK := pos == 0 || !isIdentChar(runes[pos-1])
	afterOK := pos+len(kwRunes) == len(runes) || !isIdentChar(runes[pos+len(kwRunes)])
	return beforeOK && afterOK
}

func isIdentChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

func isOperator(op string) bool {
	ops := "+-*/%=<>!&|^~:;,.()[]{}?@#$"
	return strings.Contains(ops, op)
}

func buildKeywordPattern(keywords []string) *regexp.Regexp {
	if len(keywords) == 0 {
		return regexp.MustCompile(`(?!)`)
	}
	var escaped []string
	for _, kw := range keywords {
		escaped = append(escaped, regexp.QuoteMeta(kw))
	}
	pattern := strings.Join(escaped, "|")
	return regexp.MustCompile(pattern)
}

// DetectLanguage 根据代码内容猜测语言类型。
func DetectLanguage(code string) string {
	code = strings.TrimSpace(code)

	lines := strings.Split(code, "\n")
	if len(lines) > 0 && strings.HasPrefix(lines[0], "#!") {
		shebang := lines[0]
		if strings.Contains(shebang, "python") {
			return "python"
		}
		if strings.Contains(shebang, "bash") || strings.Contains(shebang, "sh") {
			return "bash"
		}
		if strings.Contains(shebang, "node") {
			return "javascript"
		}
	}

	if strings.Contains(code, "func ") && strings.Contains(code, "package ") {
		return "go"
	}
	if strings.Contains(code, "def ") || strings.Contains(code, "class ") || strings.Contains(code, "import ") {
		return "python"
	}
	if strings.Contains(code, "function ") || strings.Contains(code, "const ") || strings.Contains(code, "let ") || strings.Contains(code, "=>") {
		return "javascript"
	}
	if strings.Contains(code, "SELECT ") || strings.Contains(code, "INSERT ") || strings.Contains(code, "CREATE ") || strings.Contains(code, "from ") {
		return "sql"
	}
	if strings.Contains(code, "fn ") || strings.Contains(code, "impl ") || strings.Contains(code, "let mut") {
		return "rust"
	}
	if strings.Contains(code, "public class ") || strings.Contains(code, "private ") || strings.Contains(code, "System.out") {
		return "java"
	}
	if strings.Contains(code, "<?php") || strings.HasPrefix(code, "<?php") {
		return "php"
	}
	if strings.HasPrefix(code, "<") && strings.Contains(code, ">") {
		if strings.Contains(code, "<!DOCTYPE") || strings.Contains(code, "<html") {
			return "html"
		}
		if strings.Contains(code, "{") && strings.Contains(code, ":") {
			return "css"
		}
	}
	if strings.HasPrefix(code, "#!/") || strings.HasPrefix(code, "if [") || strings.HasPrefix(code, "echo ") {
		return "bash"
	}
	if strings.Contains(code, "#include") || strings.Contains(code, "int main(") || strings.Contains(code, "printf(") {
		return "c"
	}

	return "go"
}
