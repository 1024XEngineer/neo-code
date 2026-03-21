package core

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type ECMA48Color int

const (
	ColorBlack   ECMA48Color = 30
	ColorRed     ECMA48Color = 31
	ColorGreen   ECMA48Color = 32
	ColorYellow  ECMA48Color = 33
	ColorBlue    ECMA48Color = 34
	ColorMagenta ECMA48Color = 35
	ColorCyan    ECMA48Color = 36
	ColorWhite   ECMA48Color = 37
	ColorDefault ECMA48Color = 39

	ColorBrightBlack   ECMA48Color = 90
	ColorBrightRed     ECMA48Color = 91
	ColorBrightGreen   ECMA48Color = 92
	ColorBrightYellow  ECMA48Color = 93
	ColorBrightBlue    ECMA48Color = 94
	ColorBrightMagenta ECMA48Color = 95
	ColorBrightCyan    ECMA48Color = 96
	ColorBrightWhite   ECMA48Color = 97
)

type SyntaxTheme struct {
	Name         string
	Comment      lipgloss.Style
	Keyword      lipgloss.Style
	String       lipgloss.Style
	Number       lipgloss.Style
	Function     lipgloss.Style
	Operator     lipgloss.Style
	Type         lipgloss.Style
	Constant     lipgloss.Style
	Annotation   lipgloss.Style
	PreProcessor lipgloss.Style
	Default      lipgloss.Style
	Background   lipgloss.Style
	CodeBlock    lipgloss.Style
}

var OneDarkTheme = SyntaxTheme{
	Name: "One Dark",

	Comment: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#5C6370")).
		Italic(true),

	Keyword: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#C678DD")).
		Bold(true),

	String: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#98C379")),

	Number: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#D19A66")),

	Function: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#61AFEF")),

	Operator: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#56B6C2")),

	Type: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#E5C07B")),

	Constant: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#D19A66")).
		Bold(true),

	Annotation: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#E5C07B")),

	PreProcessor: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#E5C07B")),

	Default: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#ABB2BF")),

	Background: lipgloss.NewStyle().
		Background(lipgloss.Color("#282C34")),

	CodeBlock: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#ABB2BF")).
		Background(lipgloss.Color("#282C34")).
		Width(100),
}

var NordTheme = SyntaxTheme{
	Name: "Nord",

	Comment: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#616E88")).
		Italic(true),

	Keyword: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#81A1C1")).
		Bold(true),

	String: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#A3BE8C")),

	Number: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#B48EAD")),

	Function: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#88C0D0")),

	Operator: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#81A1C1")),

	Type: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#EBCB8B")),

	Constant: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#B48EAD")).
		Bold(true),

	Annotation: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#88C0D0")),

	PreProcessor: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#81A1C1")),

	Default: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#D8DEE9")),

	Background: lipgloss.NewStyle().
		Background(lipgloss.Color("#2E3440")),

	CodeBlock: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#D8DEE9")).
		Background(lipgloss.Color("#2E3440")).
		Width(100),
}

var DraculaTheme = SyntaxTheme{
	Name: "Dracula",

	Comment: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6272A4")).
		Italic(true),

	Keyword: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FF79C6")).
		Bold(true),

	String: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#F1FA8C")),

	Number: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#BD93F9")),

	Function: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#50FA7B")),

	Operator: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FF79C6")),

	Type: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#8BE9FD")),

	Constant: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#BD93F9")).
		Bold(true),

	Annotation: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#50FA7B")),

	PreProcessor: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FF79C6")),

	Default: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#F8F8F2")),

	Background: lipgloss.NewStyle().
		Background(lipgloss.Color("#282A36")),

	CodeBlock: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#F8F8F2")).
		Background(lipgloss.Color("#282A36")).
		Width(100),
}

var GitHubDarkTheme = SyntaxTheme{
	Name: "GitHub Dark",

	Comment: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#8B949E")).
		Italic(true),

	Keyword: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FF7B72")).
		Bold(true),

	String: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#A5D6FF")),

	Number: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#79C0FF")),

	Function: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#D2A8FF")),

	Operator: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FF7B72")),

	Type: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#7EE787")),

	Constant: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#79C0FF")).
		Bold(true),

	Annotation: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#D2A8FF")),

	PreProcessor: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FF7B72")),

	Default: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#C9D1D9")),

	Background: lipgloss.NewStyle().
		Background(lipgloss.Color("#0D1117")),

	CodeBlock: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#C9D1D9")).
		Background(lipgloss.Color("#161B22")).
		Width(100),
}

var MonokaiTheme = SyntaxTheme{
	Name: "Monokai",

	Comment: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#75715E")).
		Italic(true),

	Keyword: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#F92672")).
		Bold(true),

	String: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#E6DB74")),

	Number: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#AE81FF")),

	Function: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#A6E22E")),

	Operator: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#F92672")),

	Type: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#66D9EF")),

	Constant: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#AE81FF")).
		Bold(true),

	Annotation: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#A6E22E")),

	PreProcessor: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#F92672")),

	Default: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#F8F8F2")),

	Background: lipgloss.NewStyle().
		Background(lipgloss.Color("#272822")),

	CodeBlock: lipgloss.NewStyle().
		Foreground(lipgloss.Color("#F8F8F2")).
		Background(lipgloss.Color("#272822")).
		Width(100),
}

var CurrentTheme = OneDarkTheme

// GetTheme 返回当前语法高亮主题。
func GetTheme() *SyntaxTheme {
	return &CurrentTheme
}

// SetTheme 替换当前语法高亮主题。
func SetTheme(theme SyntaxTheme) {
	CurrentTheme = theme
}

// RenderToken 使用主题中的对应样式渲染一个标记。
func RenderToken(token Token, theme *SyntaxTheme) string {
	switch token.Type {
	case TokenComment:
		return theme.Comment.Render(token.Content)
	case TokenKeyword:
		return theme.Keyword.Render(token.Content)
	case TokenString:
		return theme.String.Render(token.Content)
	case TokenNumber:
		return theme.Number.Render(token.Content)
	case TokenFunction:
		return theme.Function.Render(token.Content)
	case TokenOperator:
		return theme.Operator.Render(token.Content)
	case TokenBuiltin:
		return theme.Type.Render(token.Content)
	case TokenConstant:
		return theme.Constant.Render(token.Content)
	case TokenAnnotation:
		return theme.Annotation.Render(token.Content)
	case TokenPreProc:
		return theme.PreProcessor.Render(token.Content)
	default:
		return theme.Default.Render(token.Content)
	}
}

// HighlightWithTheme 使用指定语法主题渲染代码。
func HighlightWithTheme(code string, lang string, theme *SyntaxTheme) string {
	tokens := Tokenize(code, lang)
	var result strings.Builder

	for _, token := range tokens {
		result.WriteString(RenderToken(token, theme))
	}

	return result.String()
}

// HighlightCode 使用当前激活的语法主题渲染代码。
func HighlightCode(code string, lang string) string {
	return HighlightWithTheme(code, lang, GetTheme())
}

// HighlightCodeInline 使用当前激活的语法主题渲染行内代码。
func HighlightCodeInline(code string, lang string) string {
	return HighlightCode(code, lang)
}

func detectLang(code string) string {
	return DetectLanguage(code)
}
