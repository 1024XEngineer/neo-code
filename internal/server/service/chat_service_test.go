package service

import (
	"context"
	"strings"
	"testing"

	"go-llm-demo/internal/server/domain"
)

type stubRoleService struct {
	prompt string
}

func (s stubRoleService) GetActivePrompt(context.Context) (string, error) { return s.prompt, nil }
func (s stubRoleService) SetActive(context.Context, string) error         { return nil }
func (s stubRoleService) List(context.Context) ([]domain.Role, error)     { return nil, nil }
func (s stubRoleService) Create(context.Context, string, string, string) (*domain.Role, error) {
	return nil, nil
}
func (s stubRoleService) Delete(context.Context, string) error { return nil }

type stubProjectMemoryService struct {
	context string
}

func (s stubProjectMemoryService) BuildContext(context.Context) (string, error) {
	return s.context, nil
}

func (s stubProjectMemoryService) ListSources(context.Context) ([]domain.ProjectMemorySource, error) {
	return nil, nil
}

type stubWorkingMemoryService struct{}

func (stubWorkingMemoryService) BuildContext(context.Context, []domain.Message) (string, error) {
	return "Current task: fix memory module", nil
}
func (stubWorkingMemoryService) Refresh(context.Context, []domain.Message) error { return nil }
func (stubWorkingMemoryService) Clear(context.Context) error                     { return nil }
func (stubWorkingMemoryService) Get(context.Context) (*domain.WorkingMemoryState, error) {
	return nil, nil
}

type stubMemoryService struct{}

func (stubMemoryService) BuildContext(context.Context, string) (string, error) {
	return "Type: code_fact", nil
}
func (stubMemoryService) Save(context.Context, string, string) error { return nil }
func (stubMemoryService) GetStats(context.Context) (*domain.MemoryStats, error) {
	return &domain.MemoryStats{}, nil
}
func (stubMemoryService) Clear(context.Context) error        { return nil }
func (stubMemoryService) ClearSession(context.Context) error { return nil }

type capturingChatProvider struct {
	messages []domain.Message
	reply    string
}

func (p *capturingChatProvider) GetModelName() string { return "stub" }

func (p *capturingChatProvider) Chat(_ context.Context, messages []domain.Message) (<-chan string, error) {
	p.messages = append([]domain.Message{}, messages...)
	out := make(chan string, 1)
	go func() {
		defer close(out)
		out <- p.reply
	}()
	return out, nil
}

func TestChatServiceInjectsProjectMemoryBeforeAutoMemory(t *testing.T) {
	provider := &capturingChatProvider{reply: "done"}
	chatSvc := NewChatService(
		stubMemoryService{},
		stubWorkingMemoryService{},
		stubProjectMemoryService{context: "Project memory file: AGENTS.md\nRun go test ./... before PR."},
		nil,
		stubRoleService{prompt: "You are NeoCode."},
		provider,
	)

	stream, err := chatSvc.Send(context.Background(), &domain.ChatRequest{
		Messages: []domain.Message{{Role: "user", Content: "help me check memory"}},
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	for range stream {
	}

	if len(provider.messages) == 0 || provider.messages[0].Role != "system" {
		t.Fatalf("expected injected system prompt, got %+v", provider.messages)
	}

	systemPrompt := provider.messages[0].Content
	projectIdx := strings.Index(systemPrompt, "Project memory file: AGENTS.md")
	workingIdx := strings.Index(systemPrompt, "Current task: fix memory module")
	autoIdx := strings.Index(systemPrompt, "Type: code_fact")
	if projectIdx == -1 || workingIdx == -1 || autoIdx == -1 {
		t.Fatalf("expected project, working, and auto memory in system prompt, got %q", systemPrompt)
	}
	if !(projectIdx < workingIdx && workingIdx < autoIdx) {
		t.Fatalf("expected project memory before working and auto memory, got %q", systemPrompt)
	}
}
