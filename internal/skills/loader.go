package skills

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const skillFileName = "SKILL.md"
const defaultMaxSkillFileBytes int64 = 1 << 20

var headingPattern = regexp.MustCompile(`^\s{0,3}#{1,6}\s+(.+?)\s*$`)
var firstLevelHeadingPattern = regexp.MustCompile(`^\s{0,3}#\s+(.+?)\s*$`)
var errSkillFileTooLarge = errors.New("skill file exceeds size limit")

// LocalLoader scans one root directory and loads local skills.
type LocalLoader struct {
	root  string
	layer SourceLayer

	absPath            func(string) (string, error)
	statPath           func(string) (os.FileInfo, error)
	lstatPath          func(string) (os.FileInfo, error)
	readDir            func(string) ([]os.DirEntry, error)
	readSkillFile      func(string, int64) ([]byte, error)
	maxFileBytes       int64
	validateDescriptor func(Descriptor) error
}

// NewLocalLoader creates a loader for one local skills root.
func NewLocalLoader(root string) *LocalLoader {
	return newLocalLoader(root, "")
}

// NewLocalLoaderWithSourceLayer 创建本地 skills loader，并显式标注来源层级（project/global）。
func NewLocalLoaderWithSourceLayer(root string, layer SourceLayer) *LocalLoader {
	return newLocalLoader(root, normalizeSourceLayer(layer))
}

// newLocalLoader 统一构造本地 loader，避免不同入口的默认值分叉。
func newLocalLoader(root string, layer SourceLayer) *LocalLoader {
	return &LocalLoader{
		root:               strings.TrimSpace(root),
		layer:              layer,
		absPath:            filepath.Abs,
		statPath:           os.Stat,
		lstatPath:          os.Lstat,
		readDir:            os.ReadDir,
		readSkillFile:      readFileWithLimit,
		maxFileBytes:       defaultMaxSkillFileBytes,
		validateDescriptor: Descriptor.Validate,
	}
}

// normalizeSourceLayer 归一化来源层级，非法值降级为空以兼容历史数据与测试注入。
func normalizeSourceLayer(layer SourceLayer) SourceLayer {
	switch layer {
	case SourceLayerProject, SourceLayerGlobal, SourceLayerBuiltin:
		return layer
	default:
		return ""
	}
}

// Load scans local skill directories and returns parsed skills + non-fatal issues.
func (l *LocalLoader) Load(ctx context.Context) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	root := strings.TrimSpace(l.root)
	if root == "" {
		return Snapshot{}, fmt.Errorf("%w: empty root", ErrSkillRootNotFound)
	}

	absPath := l.absPath
	if absPath == nil {
		absPath = filepath.Abs
	}
	statPath := l.statPath
	if statPath == nil {
		statPath = os.Stat
	}
	lstatPath := l.lstatPath
	if lstatPath == nil {
		lstatPath = os.Lstat
	}
	readDir := l.readDir
	if readDir == nil {
		readDir = os.ReadDir
	}
	readSkillFile := l.readSkillFile
	if readSkillFile == nil {
		readSkillFile = readFileWithLimit
	}
	validateDescriptor := l.validateDescriptor
	if validateDescriptor == nil {
		validateDescriptor = Descriptor.Validate
	}
	maxFileBytes := l.maxFileBytes
	if maxFileBytes <= 0 {
		maxFileBytes = defaultMaxSkillFileBytes
	}

	absRoot, err := absPath(root)
	if err != nil {
		return Snapshot{}, fmt.Errorf("skills: resolve root %q: %w", root, err)
	}
	info, err := statPath(absRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Snapshot{}, fmt.Errorf("%w: %s", ErrSkillRootNotFound, absRoot)
		}
		return Snapshot{}, fmt.Errorf("skills: stat root %q: %w", absRoot, err)
	}
	if !info.IsDir() {
		return Snapshot{}, fmt.Errorf("skills: root %q is not directory", absRoot)
	}

	entries, err := readDir(absRoot)
	if err != nil {
		return Snapshot{}, fmt.Errorf("skills: read root %q: %w", absRoot, err)
	}

	snapshot := Snapshot{
		Skills: make([]Skill, 0, len(entries)+1),
		Issues: make([]LoadIssue, 0, len(entries)+1),
	}

	candidates := make([]string, 0, len(entries)+1)
	rootSkillFile := filepath.Join(absRoot, skillFileName)
	_, rootSkillErr := statPath(rootSkillFile)
	if rootSkillErr == nil {
		candidates = append(candidates, absRoot)
	} else if !errors.Is(rootSkillErr, os.ErrNotExist) {
		snapshot.Issues = append(snapshot.Issues, LoadIssue{
			Code:    IssueReadFailed,
			Path:    rootSkillFile,
			Message: "stat skill file failed",
			Err:     rootSkillErr,
		})
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := strings.TrimSpace(entry.Name())
		if name == "" || strings.HasPrefix(name, ".") {
			continue
		}
		candidates = append(candidates, filepath.Join(absRoot, name))
	}
	sort.Strings(candidates)
	for _, skillDir := range candidates {
		if err := ctx.Err(); err != nil {
			return Snapshot{}, err
		}

		skillPath := filepath.Join(skillDir, skillFileName)
		fileInfo, statErr := lstatPath(skillPath)
		if statErr != nil {
			if errors.Is(statErr, os.ErrNotExist) {
				snapshot.Issues = append(snapshot.Issues, LoadIssue{
					Code:    IssueSkillFileMissing,
					Path:    skillPath,
					Message: "missing SKILL.md",
					Err:     statErr,
				})
				continue
			}
			snapshot.Issues = append(snapshot.Issues, LoadIssue{
				Code:    IssueReadFailed,
				Path:    skillPath,
				Message: "stat skill file failed",
				Err:     statErr,
			})
			continue
		}
		if !fileInfo.Mode().IsRegular() {
			snapshot.Issues = append(snapshot.Issues, LoadIssue{
				Code:    IssueReadFailed,
				Path:    skillPath,
				Message: "skill file is not regular file",
			})
			continue
		}
		if fileInfo.Size() > maxFileBytes {
			snapshot.Issues = append(snapshot.Issues, LoadIssue{
				Code:    IssueReadFailed,
				Path:    skillPath,
				Message: fmt.Sprintf("skill file exceeds size limit (%d bytes)", maxFileBytes),
			})
			continue
		}

		data, readErr := readSkillFile(skillPath, maxFileBytes)
		if readErr != nil {
			if errors.Is(readErr, os.ErrNotExist) {
				snapshot.Issues = append(snapshot.Issues, LoadIssue{
					Code:    IssueSkillFileMissing,
					Path:    skillPath,
					Message: "missing SKILL.md",
					Err:     readErr,
				})
				continue
			}
			if errors.Is(readErr, errSkillFileTooLarge) {
				snapshot.Issues = append(snapshot.Issues, LoadIssue{
					Code:    IssueReadFailed,
					Path:    skillPath,
					Message: fmt.Sprintf("skill file exceeds size limit (%d bytes)", maxFileBytes),
					Err:     readErr,
				})
				continue
			}
			snapshot.Issues = append(snapshot.Issues, LoadIssue{
				Code:    IssueReadFailed,
				Path:    skillPath,
				Message: "read skill file failed",
				Err:     readErr,
			})
			continue
		}

		skill, parseIssues, parseErr := parseLocalSkillWithValidator(
			absRoot,
			skillDir,
			skillPath,
			string(data),
			l.layer,
			validateDescriptor,
		)
		if len(parseIssues) > 0 {
			snapshot.Issues = append(snapshot.Issues, parseIssues...)
		}
		if parseErr != nil {
			continue
		}
		snapshot.Skills = append(snapshot.Skills, skill)
	}

	return snapshot, nil
}

type skillFrontMatter struct {
	ID          string   `yaml:"id"`
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Version     string   `yaml:"version"`
	Source      string   `yaml:"source"`
	Scope       string   `yaml:"scope"`
	ToolHints   []string `yaml:"tool_hints"`
}

// parseLocalSkill 解析单个本地技能文件并返回结构化结果与非致命问题。
func parseLocalSkill(root, skillDir, skillPath, raw string) (Skill, []LoadIssue, error) {
	return parseLocalSkillWithValidator(root, skillDir, skillPath, raw, "", Descriptor.Validate)
}

// parseLocalSkillWithValidator 与 parseLocalSkill 逻辑一致，但允许注入校验器以覆盖异常分支测试。
func parseLocalSkillWithValidator(
	root, skillDir, skillPath, raw string,
	layer SourceLayer,
	validateDescriptor func(Descriptor) error,
) (Skill, []LoadIssue, error) {
	metaText, body, hasMeta, splitErr := splitFrontMatter(raw)
	if splitErr != nil {
		issue := LoadIssue{
			Code:    IssueInvalidMetadata,
			Path:    skillPath,
			Message: "invalid frontmatter",
			Err:     splitErr,
		}
		return Skill{}, []LoadIssue{issue}, splitErr
	}

	meta := skillFrontMatter{}
	if hasMeta {
		if err := yaml.Unmarshal([]byte(metaText), &meta); err != nil {
			issue := LoadIssue{
				Code:    IssueInvalidMetadata,
				Path:    skillPath,
				Message: "frontmatter parse failed",
				Err:     err,
			}
			return Skill{}, []LoadIssue{issue}, err
		}
	}

	scope := ScopeExplicit
	if strings.TrimSpace(meta.Scope) != "" {
		scope = ActivationScope(strings.TrimSpace(meta.Scope))
		if err := scope.Validate(); err != nil {
			issue := LoadIssue{
				Code:    IssueInvalidMetadata,
				Path:    skillPath,
				Message: "invalid scope",
				Err:     err,
			}
			return Skill{}, []LoadIssue{issue}, err
		}
	}

	if src := strings.TrimSpace(meta.Source); src != "" {
		sourceKind := SourceKind(src)
		if err := sourceKind.Validate(); err != nil {
			issue := LoadIssue{
				Code:    IssueInvalidMetadata,
				Path:    skillPath,
				Message: "invalid source kind",
				Err:     err,
			}
			return Skill{}, []LoadIssue{issue}, err
		}
		if sourceKind != SourceKindLocal {
			issue := LoadIssue{
				Code:    IssueUnsupportedSource,
				Path:    skillPath,
				Message: "local loader only supports source=local",
			}
			return Skill{}, []LoadIssue{issue}, errors.New(issue.Message)
		}
	}

	sections := parseSections(body)
	instruction := strings.TrimSpace(sections["instruction"])
	if instruction == "" {
		instruction = strings.TrimSpace(body)
	}
	content := Content{
		Instruction: instruction,
		References:  parseReferences(sections["references"]),
		Examples:    parseListItems(sections["examples"]),
		ToolHints:   mergeToolHints(parseListItems(sections["toolhints"]), meta.ToolHints),
	}
	if err := content.Validate(); err != nil {
		issue := LoadIssue{
			Code:    IssueEmptyContent,
			Path:    skillPath,
			Message: "skill content is empty",
			Err:     err,
		}
		return Skill{}, []LoadIssue{issue}, err
	}

	id := strings.TrimSpace(meta.ID)
	if id == "" {
		id = deriveSkillID(skillDir)
	}
	name := strings.TrimSpace(meta.Name)
	if name == "" {
		name = firstHeading(body)
	}
	if strings.TrimSpace(name) == "" {
		name = id
	}
	version := strings.TrimSpace(meta.Version)
	if version == "" {
		version = "v1"
	}

	descriptor := Descriptor{
		ID:          id,
		Name:        name,
		Description: strings.TrimSpace(meta.Description),
		Version:     version,
		Source: Source{
			Kind:     SourceKindLocal,
			Layer:    normalizeSourceLayer(layer),
			RootDir:  root,
			SkillDir: skillDir,
			FilePath: skillPath,
		},
		Scope: scope,
	}
	if err := validateDescriptor(descriptor); err != nil {
		issue := LoadIssue{
			Code:    IssueInvalidMetadata,
			Path:    skillPath,
			SkillID: id,
			Message: "descriptor validation failed",
			Err:     err,
		}
		return Skill{}, []LoadIssue{issue}, err
	}

	return Skill{
		Descriptor: descriptor,
		Content:    content,
	}, nil, nil
}

func splitFrontMatter(raw string) (meta string, body string, has bool, err error) {
	trimmed := strings.ReplaceAll(raw, "\r\n", "\n")
	if !strings.HasPrefix(trimmed, "---\n") {
		return "", trimmed, false, nil
	}

	lines := strings.Split(trimmed, "\n")
	endLine := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			endLine = i
			break
		}
	}
	if endLine < 0 {
		return "", "", false, errors.New("frontmatter end marker not found")
	}

	meta = strings.TrimSpace(strings.Join(lines[1:endLine], "\n"))
	body = strings.TrimSpace(strings.Join(lines[endLine+1:], "\n"))
	return meta, body, true, nil
}

func deriveSkillID(skillDir string) string {
	base := strings.ToLower(strings.TrimSpace(filepath.Base(skillDir)))
	base = strings.ReplaceAll(base, "_", "-")
	base = strings.ReplaceAll(base, " ", "-")
	base = strings.Trim(base, "-")
	if base == "" {
		return "skill"
	}
	return base
}

func firstHeading(body string) string {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	for _, line := range lines {
		m := firstLevelHeadingPattern.FindStringSubmatch(line)
		if len(m) != 2 {
			continue
		}
		return strings.TrimSpace(strings.Trim(m[1], "#"))
	}
	return ""
}

// readFileWithLimit 以受限方式读取技能文件，避免读取阶段超过内存阈值。
func readFileWithLimit(path string, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("%w: %d", errSkillFileTooLarge, maxBytes)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	limited := io.LimitReader(file, maxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("%w: %d", errSkillFileTooLarge, maxBytes)
	}
	return data, nil
}

func parseSections(body string) map[string]string {
	sections := map[string]string{
		"instruction": "",
		"references":  "",
		"examples":    "",
		"toolhints":   "",
	}
	current := "instruction"
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")

	appendLine := func(key string, line string) {
		if sections[key] == "" {
			sections[key] = line
			return
		}
		sections[key] += "\n" + line
	}

	for _, line := range lines {
		heading := normalizedHeading(line)
		if heading != "" {
			switch heading {
			case "instruction":
				current = "instruction"
				continue
			case "references", "reference":
				current = "references"
				continue
			case "examples", "example":
				current = "examples"
				continue
			case "toolhints", "tool-hints", "tool_hints":
				current = "toolhints"
				continue
			default:
				current = "instruction"
			}
		}
		appendLine(current, line)
	}

	for key := range sections {
		sections[key] = strings.TrimSpace(sections[key])
	}
	return sections
}

func normalizedHeading(line string) string {
	m := headingPattern.FindStringSubmatch(line)
	if len(m) != 2 {
		return ""
	}
	heading := strings.ToLower(strings.TrimSpace(m[1]))
	heading = strings.ReplaceAll(heading, " ", "")
	return heading
}

var markdownLinkPattern = regexp.MustCompile(`\[(?P<title>[^\]]+)\]\((?P<path>[^)]+)\)`)

func parseReferences(text string) []Reference {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	refs := make([]Reference, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(strings.TrimLeft(line, "-*"))
		if trimmed == "" {
			continue
		}

		m := markdownLinkPattern.FindStringSubmatch(trimmed)
		if len(m) == 3 {
			refs = append(refs, Reference{
				Title: strings.TrimSpace(m[1]),
				Path:  strings.TrimSpace(m[2]),
			})
			continue
		}
		refs = append(refs, Reference{
			Path: trimmed,
		})
	}
	return refs
}

func parseListItems(text string) []string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	items := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(strings.TrimLeft(line, "-*"))
		if trimmed == "" {
			continue
		}
		items = append(items, trimmed)
	}
	return items
}

func mergeToolHints(sectionHints []string, metadataHints []string) []string {
	out := make([]string, 0, len(sectionHints)+len(metadataHints))
	seen := make(map[string]struct{}, len(sectionHints)+len(metadataHints))
	appendHint := func(raw string) {
		h := strings.TrimSpace(raw)
		if h == "" {
			return
		}
		key := strings.ToLower(h)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, h)
	}
	for _, hint := range sectionHints {
		appendHint(hint)
	}
	for _, hint := range metadataHints {
		appendHint(hint)
	}
	return out
}
