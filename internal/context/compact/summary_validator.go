package compact

import (
	"errors"
	"fmt"
	"strings"

	"neo-code/internal/context/internalcompact"
)

var summarySections = internalcompact.SummarySections()

type parsedSummary struct {
	Sections []parsedSummarySection
}

type parsedSummarySection struct {
	Name    string
	Bullets []string
}

// compactSummaryValidator 负责 compact 摘要的规范化、结构校验与结构化收缩。
type compactSummaryValidator struct{}

// Validate 校验摘要结构与长度，并在可修复时按 section 收缩到目标上限内。
func (compactSummaryValidator) Validate(summary string, maxChars int) (string, error) {
	document, err := parseCompactSummary(summary)
	if err != nil {
		return "", err
	}

	if maxChars > 0 {
		document, err = shrinkSummaryDocument(document, maxChars)
		if err != nil {
			return "", err
		}
	}

	return renderSummaryDocument(document), nil
}

// normalizeSummary 统一 compact 摘要换行与首尾空白，便于后续结构校验。
func normalizeSummary(summary string) string {
	summary = strings.ReplaceAll(summary, "\r\n", "\n")
	return strings.TrimSpace(summary)
}

// parseCompactSummary 按固定协议解析 compact 摘要，并拒绝缺失 section 或非法尾部内容。
func parseCompactSummary(summary string) (parsedSummary, error) {
	summary = normalizeSummary(summary)
	if summary == "" {
		return parsedSummary{}, errors.New("compact: summary is empty")
	}

	lines := strings.Split(summary, "\n")
	index := nextNonEmptyLine(lines, 0)
	if index >= len(lines) || strings.TrimSpace(lines[index]) != internalcompact.SummaryMarker {
		return parsedSummary{}, fmt.Errorf("compact: summary must start with %s", internalcompact.SummaryMarker)
	}
	index++

	document := parsedSummary{
		Sections: make([]parsedSummarySection, 0, len(summarySections)),
	}
	for _, section := range summarySections {
		index = nextNonEmptyLine(lines, index)
		if index >= len(lines) || strings.TrimSpace(lines[index]) != section+":" {
			return parsedSummary{}, fmt.Errorf("compact: summary missing required section %q", section)
		}
		index++

		bullets := make([]string, 0, 2)
		for index < len(lines) {
			rawLine := strings.TrimRight(lines[index], " \t")
			line := strings.TrimSpace(rawLine)
			bulletLine := strings.TrimLeft(rawLine, " \t")

			switch {
			case line == "":
				index++
			case isSummarySectionHeader(line):
				if len(bullets) == 0 {
					return parsedSummary{}, fmt.Errorf("compact: summary section %q requires at least one bullet", section)
				}
				goto nextSection
			case bulletLine == "-":
				return parsedSummary{}, fmt.Errorf("compact: summary section %q contains an empty bullet", section)
			case !strings.HasPrefix(bulletLine, "- "):
				return parsedSummary{}, fmt.Errorf("compact: summary section %q contains invalid line %q", section, line)
			default:
				content := strings.TrimSpace(strings.TrimPrefix(bulletLine, "- "))
				if content == "" {
					return parsedSummary{}, fmt.Errorf("compact: summary section %q contains an empty bullet", section)
				}
				bullets = append(bullets, content)
				index++
			}
		}

		if len(bullets) == 0 {
			return parsedSummary{}, fmt.Errorf("compact: summary section %q requires at least one bullet", section)
		}

	nextSection:
		document.Sections = append(document.Sections, parsedSummarySection{
			Name:    section,
			Bullets: bullets,
		})
	}

	index = nextNonEmptyLine(lines, index)
	if index < len(lines) {
		return parsedSummary{}, fmt.Errorf("compact: summary contains unexpected trailing content %q", strings.TrimSpace(lines[index]))
	}
	return document, nil
}

// shrinkSummaryDocument 尝试在不破坏协议的前提下收缩摘要，优先保留结构合法性。
func shrinkSummaryDocument(document parsedSummary, maxChars int) (parsedSummary, error) {
	normalized := normalizeSummaryDocument(document)
	if maxChars <= 0 || runeCount(renderSummaryDocument(normalized)) <= maxChars {
		return normalized, nil
	}

	singleBullet := keepFirstBulletPerSection(normalized)
	if runeCount(renderSummaryDocument(singleBullet)) <= maxChars {
		return singleBullet, nil
	}

	shrunk, ok := fitSummaryDocument(singleBullet, maxChars)
	if !ok {
		return parsedSummary{}, fmt.Errorf("compact: summary exceeds max_summary_chars=%d even after structured truncation", maxChars)
	}
	return shrunk, nil
}

// normalizeSummaryDocument 规整 section bullet 的空白形式，减少无意义字符占用。
func normalizeSummaryDocument(document parsedSummary) parsedSummary {
	normalized := parsedSummary{
		Sections: make([]parsedSummarySection, 0, len(document.Sections)),
	}
	for _, section := range document.Sections {
		next := parsedSummarySection{
			Name:    section.Name,
			Bullets: make([]string, 0, len(section.Bullets)),
		}
		for _, bullet := range section.Bullets {
			text := collapseWhitespace(bullet)
			if text == "" {
				text = "none"
			}
			next.Bullets = append(next.Bullets, text)
		}
		if len(next.Bullets) == 0 {
			next.Bullets = append(next.Bullets, "none")
		}
		normalized.Sections = append(normalized.Sections, next)
	}
	return normalized
}

// keepFirstBulletPerSection 将每个 section 收敛为一条 bullet，控制摘要在超长时的体积。
func keepFirstBulletPerSection(document parsedSummary) parsedSummary {
	condensed := parsedSummary{
		Sections: make([]parsedSummarySection, 0, len(document.Sections)),
	}
	for _, section := range document.Sections {
		bullet := "none"
		if len(section.Bullets) > 0 {
			bullet = section.Bullets[0]
		}
		condensed.Sections = append(condensed.Sections, parsedSummarySection{
			Name:    section.Name,
			Bullets: []string{bullet},
		})
	}
	return condensed
}

// fitSummaryDocument 逐步收缩最长 bullet，直到摘要落入预算或确认预算不可满足。
func fitSummaryDocument(document parsedSummary, maxChars int) (parsedSummary, bool) {
	fitted := cloneParsedSummary(document)
	if runeCount(renderSummaryDocument(withAllBullets(fitted, "none"))) > maxChars {
		return parsedSummary{}, false
	}

	for runeCount(renderSummaryDocument(fitted)) > maxChars {
		sectionIndex := longestBulletSection(fitted)
		if sectionIndex < 0 {
			break
		}

		current := fitted.Sections[sectionIndex].Bullets[0]
		if runeCount(current) <= len([]rune("none")) {
			fitted.Sections[sectionIndex].Bullets[0] = "none"
			if runeCount(renderSummaryDocument(fitted)) > maxChars {
				break
			}
			continue
		}

		overflow := runeCount(renderSummaryDocument(fitted)) - maxChars
		currentRunes := runeCount(current)
		target := currentRunes - overflow
		minimum := len([]rune("none"))
		if target < minimum {
			target = minimum
		}
		fitted.Sections[sectionIndex].Bullets[0] = truncateSummaryBullet(current, target)
	}

	return fitted, runeCount(renderSummaryDocument(fitted)) <= maxChars
}

// withAllBullets 生成所有 section 都使用同一条 bullet 的文档，用于评估最小合法体积。
func withAllBullets(document parsedSummary, bullet string) parsedSummary {
	out := cloneParsedSummary(document)
	for index := range out.Sections {
		out.Sections[index].Bullets = []string{bullet}
	}
	return out
}

// cloneParsedSummary 深拷贝解析后的摘要结构，避免收缩阶段共享底层切片。
func cloneParsedSummary(document parsedSummary) parsedSummary {
	out := parsedSummary{
		Sections: make([]parsedSummarySection, 0, len(document.Sections)),
	}
	for _, section := range document.Sections {
		out.Sections = append(out.Sections, parsedSummarySection{
			Name:    section.Name,
			Bullets: append([]string(nil), section.Bullets...),
		})
	}
	return out
}

// longestBulletSection 返回当前最长首条 bullet 所在 section 下标。
func longestBulletSection(document parsedSummary) int {
	index := -1
	longest := 0
	for sectionIndex, section := range document.Sections {
		if len(section.Bullets) == 0 {
			continue
		}
		length := runeCount(section.Bullets[0])
		if length > longest {
			index = sectionIndex
			longest = length
		}
	}
	return index
}

// truncateSummaryBullet 截断单条 bullet，并在预算允许时保留省略号。
func truncateSummaryBullet(input string, max int) string {
	input = collapseWhitespace(input)
	if max <= 0 {
		return ""
	}
	if runeCount(input) <= max {
		return input
	}
	if max <= len([]rune("none")) {
		return "none"
	}
	if max <= 3 {
		truncated, _ := truncateRunes(input, max)
		return truncated
	}

	truncated, _ := truncateRunes(input, max-3)
	return strings.TrimSpace(truncated) + "..."
}

// renderSummaryDocument 将解析后的摘要结构还原为持久化使用的协议文本。
func renderSummaryDocument(document parsedSummary) string {
	lines := []string{internalcompact.SummaryMarker}
	for index, section := range document.Sections {
		if index > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, section.Name+":")
		for _, bullet := range section.Bullets {
			lines = append(lines, "- "+strings.TrimSpace(bullet))
		}
	}
	return normalizeSummary(strings.Join(lines, "\n"))
}

// collapseWhitespace 折叠连续空白，减少摘要内容中的无效体积。
func collapseWhitespace(input string) string {
	fields := strings.Fields(strings.TrimSpace(input))
	if len(fields) == 0 {
		return ""
	}
	return strings.Join(fields, " ")
}

// nextNonEmptyLine 返回从给定位置开始的下一条非空白行下标。
func nextNonEmptyLine(lines []string, start int) int {
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	return start
}

// isSummarySectionHeader 判断当前行是否为 compact 摘要协议中的 section 头。
func isSummarySectionHeader(line string) bool {
	for _, section := range summarySections {
		if line == section+":" {
			return true
		}
	}
	return false
}

// truncateRunes 按 rune 数量截断字符串，避免破坏多字节字符。
func truncateRunes(input string, max int) (string, bool) {
	if max <= 0 {
		return "", input != ""
	}
	if runeCount(input) <= max {
		return input, false
	}

	runes := []rune(input)
	return string(runes[:max]), true
}

// runeCount 统一按 rune 计算长度，确保摘要预算对中文等多字节文本稳定生效。
func runeCount(input string) int {
	return len([]rune(input))
}
