package skills

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// SourceKind describes where a skill comes from.
type SourceKind string

const (
	SourceKindLocal   SourceKind = "local"
	SourceKindBuiltin SourceKind = "builtin"
)

// Validate reports whether the source kind is supported.
func (k SourceKind) Validate() error {
	switch k {
	case SourceKindLocal, SourceKindBuiltin:
		return nil
	default:
		return fmt.Errorf("skills: invalid source kind %q", k)
	}
}

// SourceLayer 表示技能来源所在的加载层级（project/global/builtin）。
type SourceLayer string

const (
	SourceLayerGlobal  SourceLayer = "global"
	SourceLayerProject SourceLayer = "project"
	SourceLayerBuiltin SourceLayer = "builtin"
)

// Validate 校验技能来源层级是否合法；空值用于兼容旧数据。
func (l SourceLayer) Validate() error {
	switch l {
	case "", SourceLayerGlobal, SourceLayerProject, SourceLayerBuiltin:
		return nil
	default:
		return fmt.Errorf("skills: invalid source layer %q", l)
	}
}

// ActivationScope controls where the skill should be visible/active.
type ActivationScope string

const (
	ScopeGlobal    ActivationScope = "global"
	ScopeWorkspace ActivationScope = "workspace"
	ScopeSession   ActivationScope = "session"
	ScopeExplicit  ActivationScope = "explicit"
)

// Validate reports whether the activation scope is supported.
func (s ActivationScope) Validate() error {
	switch s {
	case ScopeGlobal, ScopeWorkspace, ScopeSession, ScopeExplicit:
		return nil
	default:
		return fmt.Errorf("skills: invalid activation scope %q", s)
	}
}

// Source describes the origin of one skill.
type Source struct {
	Kind     SourceKind
	Layer    SourceLayer
	RootDir  string
	SkillDir string
	FilePath string
}

// Descriptor is the stable, indexable skill metadata.
type Descriptor struct {
	ID          string
	Name        string
	Description string
	Version     string
	Source      Source
	Scope       ActivationScope
}

// Validate reports whether the descriptor is well-formed.
func (d Descriptor) Validate() error {
	if strings.TrimSpace(d.ID) == "" {
		return fmt.Errorf("skills: descriptor id is empty")
	}
	if strings.TrimSpace(d.Name) == "" {
		return fmt.Errorf("skills: descriptor name is empty")
	}
	if err := d.Source.Kind.Validate(); err != nil {
		return err
	}
	if err := d.Source.Layer.Validate(); err != nil {
		return err
	}
	if err := d.Scope.Validate(); err != nil {
		return err
	}
	return nil
}

// Reference is one optional doc/reference entry declared by a skill.
type Reference struct {
	Path    string
	Title   string
	Summary string
}

// Content is the prompt-facing payload of a skill.
type Content struct {
	Instruction string
	References  []Reference
	Examples    []string
	ToolHints   []string
}

// Validate reports whether content is usable.
func (c Content) Validate() error {
	if strings.TrimSpace(c.Instruction) == "" {
		return ErrEmptyContent
	}
	return nil
}

// Skill bundles descriptor and content.
type Skill struct {
	Descriptor Descriptor
	Content    Content
}

// LoadIssueCode identifies one non-fatal loader/registry problem.
type LoadIssueCode string

const (
	IssueSkillFileMissing  LoadIssueCode = "skill_file_missing"
	IssueInvalidMetadata   LoadIssueCode = "invalid_metadata"
	IssueEmptyContent      LoadIssueCode = "empty_content"
	IssueDuplicateID       LoadIssueCode = "duplicate_id"
	IssueRefreshFailed     LoadIssueCode = "refresh_failed"
	IssueReadFailed        LoadIssueCode = "read_failed"
	IssueParseFailed       LoadIssueCode = "parse_failed"
	IssueUnsupportedSource LoadIssueCode = "unsupported_source"
)

// LoadIssue captures one non-fatal error so one bad skill does not break others.
type LoadIssue struct {
	Code    LoadIssueCode
	Path    string
	SkillID string
	Message string
	Err     error
}

// Error returns a readable issue message.
func (i LoadIssue) Error() string {
	base := strings.TrimSpace(i.Message)
	if base == "" && i.Err != nil {
		base = strings.TrimSpace(i.Err.Error())
	}
	if base == "" {
		base = string(i.Code)
	}
	if strings.TrimSpace(i.Path) == "" {
		return "skills: " + base
	}
	return fmt.Sprintf("skills: %s (%s)", base, i.Path)
}

// Snapshot is one loader output batch.
type Snapshot struct {
	Skills []Skill
	Issues []LoadIssue
}

// Loader loads skills from one or many sources.
type Loader interface {
	Load(ctx context.Context) (Snapshot, error)
}

// ListInput controls skill listing filters.
type ListInput struct {
	SourceKinds []SourceKind
	Scopes      []ActivationScope
	Workspace   string
}

// Registry is the read/refresh entrypoint used by upper layers.
type Registry interface {
	List(ctx context.Context, input ListInput) ([]Descriptor, error)
	Get(ctx context.Context, id string) (Descriptor, Content, error)
	Refresh(ctx context.Context) error
}

func normalizeWorkspace(workspace string) string {
	trimmed := strings.TrimSpace(workspace)
	if trimmed == "" {
		return ""
	}
	return filepath.Clean(trimmed)
}
