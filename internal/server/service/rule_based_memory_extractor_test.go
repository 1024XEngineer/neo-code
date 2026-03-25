package service

import (
	"context"
	"testing"

	"go-llm-demo/internal/server/domain"
	"go-llm-demo/internal/server/infra/repository"
)

func TestRuleBasedMemoryExtractorExtractsUserPreference(t *testing.T) {
	extractor := NewRuleBasedMemoryExtractor()

	items, err := extractor.Extract(context.Background(), "以后回答中文，命令和说明都用中文。", "好的，后续我会统一使用中文回复。")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	if len(items) == 0 {
		t.Fatalf("expected extracted items, got none")
	}

	var preferenceCount, projectRuleCount int
	for _, item := range items {
		if item.Type == domain.TypeUserPreference {
			preferenceCount++
		}
		if item.Type == domain.TypeProjectRule {
			projectRuleCount++
		}
	}

	if preferenceCount != 1 {
		t.Fatalf("expected exactly one user_preference, got %d (%+v)", preferenceCount, items)
	}
	if projectRuleCount != 0 {
		t.Fatalf("expected no project_rule for a user preference, got %d (%+v)", projectRuleCount, items)
	}
}

func TestRuleBasedMemoryExtractorSkipsToolPayload(t *testing.T) {
	extractor := NewRuleBasedMemoryExtractor()

	items, err := extractor.Extract(context.Background(), "请读取 memory_service.go 看看实现。", `{"tool":"read","params":{"filePath":"internal/server/service/memory_service.go"}}`)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected tool payload to be skipped, got %+v", items)
	}
}

func TestRuleBasedMemoryExtractorExtractsProjectRuleAndCodeFact(t *testing.T) {
	extractor := NewRuleBasedMemoryExtractor()

	projectItems, err := extractor.Extract(
		context.Background(),
		"The project convention is to run go test ./... from the repo root.",
		"Documented: run go test ./... from the repository root as the default test command.",
	)
	if err != nil {
		t.Fatalf("extract project rule: %v", err)
	}

	var hasProjectRule bool
	for _, item := range projectItems {
		if item.Type == domain.TypeProjectRule {
			hasProjectRule = true
		}
	}
	if !hasProjectRule {
		t.Fatalf("expected a project_rule item, got %+v", projectItems)
	}

	codeItems, err := extractor.Extract(
		context.Background(),
		"What does memory_repository.go do?",
		"internal/server/infra/repository/memory_repository.go is responsible for persistent memory storage and retrieval.",
	)
	if err != nil {
		t.Fatalf("extract code fact: %v", err)
	}

	var hasCodeFact bool
	for _, item := range codeItems {
		if item.Type == domain.TypeCodeFact {
			hasCodeFact = true
		}
	}
	if !hasCodeFact {
		t.Fatalf("expected a code_fact item, got %+v", codeItems)
	}
}

type stubMemoryExtractor struct {
	items []domain.MemoryItem
}

func (s stubMemoryExtractor) Extract(context.Context, string, string) ([]domain.MemoryItem, error) {
	return s.items, nil
}

func TestMemoryServiceUsesInjectedExtractor(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/memory.json"

	svc := NewMemoryServiceWithExtractor(
		repository.NewFileMemoryStore(path, 100),
		repository.NewSessionMemoryStore(100),
		stubMemoryExtractor{
			items: []domain.MemoryItem{
				{
					Type:       domain.TypeCodeFact,
					Summary:    "custom extractor item",
					Scope:      domain.ScopeProject,
					Confidence: 0.9,
				},
			},
		},
		5,
		2.2,
		1800,
		path,
		[]string{"code_fact"},
	)

	if err := svc.Save(ctx, "ignored", "ignored"); err != nil {
		t.Fatalf("save: %v", err)
	}

	stats, err := svc.GetStats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.ByType[domain.TypeCodeFact] != 1 {
		t.Fatalf("expected custom extractor item to be persisted, got %+v", stats.ByType)
	}
}
