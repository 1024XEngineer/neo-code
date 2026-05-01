package context

import (
	"context"
	"strings"

	"neo-code/internal/rules"
)

const projectRuleFileName = "AGENTS.md"

// Sections 加载项目根与全局 AGENTS.md，并渲染统一 Rules section。
func (s *rulesPromptSource) Sections(ctx context.Context, input BuildInput) ([]promptSection, error) {
	snapshot, err := s.loader.Load(ctx, resolveProjectRoot(input.Metadata))
	if err != nil {
		return nil, err
	}

	section := renderRulesSection(snapshot)
	if renderPromptSection(section) == "" {
		return nil, nil
	}
	return []promptSection{section}, nil
}

// resolveProjectRoot 优先返回稳定项目根，缺失时回退到当前工作目录。
func resolveProjectRoot(metadata Metadata) string {
	if projectRoot := strings.TrimSpace(metadata.ProjectRoot); projectRoot != "" {
		return projectRoot
	}
	return strings.TrimSpace(metadata.Workdir)
}

// renderRulesSection 将项目与全局规则渲染为统一 prompt section。
func renderRulesSection(snapshot rules.Snapshot) promptSection {
	var blocks []string
	if block := renderRulesDocumentBlock("Project Rules", snapshot.ProjectAGENTS); block != "" {
		blocks = append(blocks, block)
	}
	if block := renderRulesDocumentBlock("Global Rules", snapshot.GlobalAGENTS); block != "" {
		blocks = append(blocks, block)
	}
	if len(blocks) == 0 {
		return promptSection{}
	}

	intro := strings.Join([]string{
		"These are explicit rules and default behaviors. Treat them as higher priority than memory.",
		"If rules conflict, prefer project rules over global rules.",
	}, "\n")

	return promptSection{
		Title:   "Rules",
		Content: intro + "\n\n" + strings.Join(blocks, "\n\n"),
	}
}

// renderRulesDocumentBlock 渲染单个规则来源，缺失时返回空串。
func renderRulesDocumentBlock(title string, document rules.Document) string {
	if strings.TrimSpace(document.Content) == "" {
		return ""
	}

	var builder strings.Builder
	builder.WriteString("### ")
	builder.WriteString(title)
	builder.WriteString("\n")
	if path := strings.TrimSpace(document.Path); path != "" {
		builder.WriteString("Source: ")
		builder.WriteString(path)
		builder.WriteString("\n")
	}
	builder.WriteString("\n")
	builder.WriteString(document.Content)
	if document.Truncated {
		builder.WriteString(rules.DefaultTruncationNotice)
	}
	return strings.TrimSpace(builder.String())
}
