package domain

import (
	"context"
	"strings"
	"time"
)

const (
	TypeSessionMemory  = "session_memory"
	TypeProjectRule    = "project_rule"
	TypeCodeFact       = "code_fact"
	TypeUserPreference = "user_preference"
	TypeFixRecipe      = "fix_recipe"
	TypeLegacyChat     = "legacy_chat"

	ScopeSession = "session"
	ScopeProject = "project"
	ScopeUser    = "user"
)

type MemoryItem struct {
	ID             string    `json:"id"`
	Type           string    `json:"type,omitempty"`
	Summary        string    `json:"summary,omitempty"`
	Details        string    `json:"details,omitempty"`
	Scope          string    `json:"scope,omitempty"`
	Tags           []string  `json:"tags,omitempty"`
	Source         string    `json:"source,omitempty"`
	Confidence     float64   `json:"confidence,omitempty"`
	UserInput      string    `json:"user_input,omitempty"`
	AssistantReply string    `json:"assistant_reply,omitempty"`
	Text           string    `json:"text,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at,omitempty"`
}

type MemoryRepository interface {
	List(ctx context.Context) ([]MemoryItem, error)
	Add(ctx context.Context, item MemoryItem) error
	Clear(ctx context.Context) error
}

type MemoryService interface {
	BuildContext(ctx context.Context, userInput string) (string, error)
	Save(ctx context.Context, userInput, reply string) error
	GetStats(ctx context.Context) (*MemoryStats, error)
	Clear(ctx context.Context) error
	ClearSession(ctx context.Context) error
}

type MemoryStats struct {
	PersistentItems int
	SessionItems    int
	TotalItems      int
	TopK            int
	MinScore        float64
	Path            string
	ByType          map[string]int
}

func IsPersistentType(itemType string) bool {
	switch strings.TrimSpace(itemType) {
	case TypeUserPreference, TypeProjectRule, TypeCodeFact, TypeFixRecipe:
		return true
	default:
		return false
	}
}

func (i MemoryItem) Normalized() MemoryItem {
	normalized := i
	if normalized.Type == "" {
		normalized.Type = TypeLegacyChat
	}
	if normalized.Type == "project_memory" {
		normalized.Type = TypeProjectRule
	}
	if normalized.Type == "failure_note" {
		normalized.Type = TypeFixRecipe
	}
	if normalized.Scope == "" {
		switch normalized.Type {
		case TypeProjectRule, TypeCodeFact, TypeFixRecipe:
			normalized.Scope = ScopeProject
		case TypeUserPreference:
			normalized.Scope = ScopeUser
		default:
			normalized.Scope = ScopeSession
		}
	}
	if normalized.Confidence <= 0 {
		normalized.Confidence = 0.5
	}
	if normalized.UpdatedAt.IsZero() {
		normalized.UpdatedAt = normalized.CreatedAt
	}
	if normalized.Source == "" {
		normalized.Source = "conversation"
	}
	if normalized.Summary == "" {
		normalized.Summary = SummarizeText(firstNonEmpty(normalized.UserInput, normalized.Text), 160)
	}
	if normalized.Details == "" {
		normalized.Details = strings.TrimSpace(firstNonEmpty(normalized.AssistantReply, normalized.Text))
	}
	normalized.Details = SummarizeText(normalized.Details, 320)
	if normalized.Text == "" {
		normalized.Text = normalized.SearchText()
	}
	if len(normalized.Tags) == 0 {
		normalized.Tags = InferTags(normalized.Summary + "\n" + normalized.Details)
	}
	return normalized
}

func (i MemoryItem) SearchText() string {
	parts := make([]string, 0, 4)
	if strings.TrimSpace(i.Type) != "" {
		parts = append(parts, i.Type)
	}
	if strings.TrimSpace(i.Summary) != "" {
		parts = append(parts, i.Summary)
	}
	if strings.TrimSpace(i.Details) != "" {
		parts = append(parts, i.Details)
	}
	if len(i.Tags) > 0 {
		parts = append(parts, strings.Join(i.Tags, " "))
	}
	if len(parts) == 0 {
		return strings.TrimSpace(firstNonEmpty(i.Text, i.UserInput+"\n"+i.AssistantReply))
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func (i MemoryItem) PromptBlock() string {
	parts := []string{
		"Type: " + i.Type,
		"Scope: " + i.Scope,
		"Summary: " + i.Summary,
	}
	if strings.TrimSpace(i.Details) != "" {
		parts = append(parts, "Details: "+SummarizeText(i.Details, 180))
	}
	if len(i.Tags) > 0 {
		parts = append(parts, "Tags: "+strings.Join(i.Tags, ", "))
	}
	return strings.Join(parts, "\n")
}

func SummarizeText(text string, maxLen int) string {
	trimmed := strings.TrimSpace(text)
	if len(trimmed) <= maxLen {
		return trimmed
	}
	if maxLen <= 3 {
		return trimmed[:maxLen]
	}
	return trimmed[:maxLen-3] + "..."
}

func InferTags(text string) []string {
	trimmed := strings.ToLower(text)
	tags := make([]string, 0, 6)
	appendTag := func(tag string) {
		for _, existing := range tags {
			if existing == tag {
				return
			}
		}
		tags = append(tags, tag)
	}
	keywords := map[string][]string{
		"go":       {"go ", "golang", "go.mod", "go test", "go build"},
		"config":   {"config", "yaml", "env", "api_key"},
		"memory":   {"memory", "rule", "recall", "preference"},
		"testing":  {"test", "testing", "assert", "coverage"},
		"build":    {"build", "compile", "binary"},
		"bugfix":   {"bug", "fix", "error", "failed", "panic"},
		"workflow": {"todo", "plan", "command", "cli"},
		"path":     {"config.yaml", "readme", "services/", "memory/", "main.go"},
	}
	for tag, words := range keywords {
		for _, word := range words {
			if strings.Contains(trimmed, word) {
				appendTag(tag)
				break
			}
		}
	}
	return tags
}

func Keywords(text string) []string {
	text = strings.ToLower(text)
	replacer := strings.NewReplacer(
		",", " ", ".", " ", ":", " ", ";", " ",
		"(", " ", ")", " ", "[", " ", "]", " ",
		"{", " ", "}", " ", "\n", " ", "\t", " ",
	)
	cleaned := replacer.Replace(text)
	parts := strings.Fields(cleaned)
	seen := map[string]struct{}{}
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if len(part) < 2 {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		result = append(result, part)
	}
	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}
