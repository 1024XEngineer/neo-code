package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"go-llm-demo/internal/server/domain"
)

const llmMemoryExtractorPrompt = `You are a memory extraction component for a coding assistant.
Return JSON only. Do not include markdown fences, prose, or explanations.

Extract at most 5 memory items from the conversation turn.
Only store memories that are useful for future coding assistance.
Prefer fewer, higher-quality items.

Allowed types:
- user_preference: explicit durable user preferences such as language, formatting, workflow, or response style
- project_rule: concrete project conventions, commands, config locations, or repository rules backed by evidence
- code_fact: concrete codebase facts like file responsibility, symbol purpose, or implementation location
- fix_recipe: a problem and the fix that resolved it
- session_memory: temporary but useful coding context for the current session

Rules:
- Do not store tool protocol payloads or raw tool JSON.
- Do not store generic pleasantries or assistant filler.
- Do not invent facts.
- Only create user_preference when the user clearly expresses a durable preference.
- Only create project_rule when the turn contains concrete repository or configuration evidence.
- Only create code_fact when the turn contains concrete file, symbol, or module evidence and an explanatory relationship.
- If nothing is worth storing, return {"items":[]}.

JSON schema:
{
  "items": [
    {
      "type": "user_preference | project_rule | code_fact | fix_recipe | session_memory",
      "scope": "user | project | session",
      "summary": "short summary",
      "details": "optional details",
      "confidence": 0.0
    }
  ]
}`

type llmMemoryExtractor struct {
	provider domain.ChatProvider
	timeout  time.Duration
	fallback domain.MemoryExtractor
}

type llmMemoryExtractionEnvelope struct {
	Items []llmMemoryCandidate `json:"items"`
}

type llmMemoryCandidate struct {
	Type       string  `json:"type"`
	Scope      string  `json:"scope"`
	Summary    string  `json:"summary"`
	Details    string  `json:"details"`
	Confidence float64 `json:"confidence"`
}

// NewLLMMemoryExtractor returns an extractor backed by a chat model.
func NewLLMMemoryExtractor(provider domain.ChatProvider, opts LLMMemoryExtractorOptions) domain.MemoryExtractor {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	return &llmMemoryExtractor{
		provider: provider,
		timeout:  timeout,
		fallback: opts.Fallback,
	}
}

// Extract uses an LLM to classify and structure memory candidates.
func (e *llmMemoryExtractor) Extract(ctx context.Context, userInput, assistantReply string) ([]domain.MemoryItem, error) {
	if shouldSkipConversationMemory(userInput, assistantReply) {
		return nil, nil
	}
	if e.provider == nil {
		return e.fallbackExtract(ctx, userInput, assistantReply, fmt.Errorf("llm memory extractor provider is nil"))
	}

	reqCtx := ctx
	cancel := func() {}
	if e.timeout > 0 {
		reqCtx, cancel = context.WithTimeout(ctx, e.timeout)
	}
	defer cancel()

	raw, err := e.runExtractionPrompt(reqCtx, userInput, assistantReply)
	if err != nil {
		return e.fallbackExtract(ctx, userInput, assistantReply, err)
	}

	items, err := e.parseExtraction(raw, userInput, assistantReply)
	if err != nil {
		return e.fallbackExtract(ctx, userInput, assistantReply, err)
	}
	return items, nil
}

func (e *llmMemoryExtractor) runExtractionPrompt(ctx context.Context, userInput, assistantReply string) (string, error) {
	prompt := fmt.Sprintf("Conversation turn:\nUser:\n%s\n\nAssistant:\n%s", strings.TrimSpace(userInput), strings.TrimSpace(assistantReply))
	messages := []domain.Message{
		{Role: "system", Content: llmMemoryExtractorPrompt},
		{Role: "user", Content: prompt},
	}

	out, err := e.provider.Chat(ctx, messages)
	if err != nil {
		return "", err
	}

	var builder strings.Builder
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case chunk, ok := <-out:
			if !ok {
				result := strings.TrimSpace(builder.String())
				if result == "" {
					return "", fmt.Errorf("llm memory extractor returned an empty response")
				}
				return result, nil
			}
			builder.WriteString(chunk)
		}
	}
}

func (e *llmMemoryExtractor) parseExtraction(raw, userInput, assistantReply string) ([]domain.MemoryItem, error) {
	jsonText := extractJSONObject(raw)
	if jsonText == "" {
		return nil, fmt.Errorf("llm memory extractor returned non-json content")
	}

	var envelope llmMemoryExtractionEnvelope
	if err := json.Unmarshal([]byte(jsonText), &envelope); err != nil {
		return nil, fmt.Errorf("decode llm memory extraction: %w", err)
	}

	now := time.Now().UTC()
	items := make([]domain.MemoryItem, 0, len(envelope.Items))
	for _, candidate := range envelope.Items {
		item, ok := normalizeLLMMemoryCandidate(candidate, now, userInput, assistantReply)
		if !ok {
			continue
		}
		items = append(items, item)
	}
	return dedupeMemoryItems(items), nil
}

func (e *llmMemoryExtractor) fallbackExtract(ctx context.Context, userInput, assistantReply string, cause error) ([]domain.MemoryItem, error) {
	if e.fallback == nil {
		return nil, cause
	}
	return e.fallback.Extract(ctx, userInput, assistantReply)
}

func normalizeLLMMemoryCandidate(candidate llmMemoryCandidate, now time.Time, userInput, assistantReply string) (domain.MemoryItem, bool) {
	itemType := strings.TrimSpace(candidate.Type)
	switch itemType {
	case domain.TypeUserPreference, domain.TypeProjectRule, domain.TypeCodeFact, domain.TypeFixRecipe, domain.TypeSessionMemory:
	default:
		return domain.MemoryItem{}, false
	}

	summary := strings.TrimSpace(candidate.Summary)
	if summary == "" {
		return domain.MemoryItem{}, false
	}

	scope := strings.TrimSpace(candidate.Scope)
	if scope == "" {
		scope = domain.MemoryItem{Type: itemType}.Normalized().Scope
	}

	confidence := candidate.Confidence
	if confidence <= 0 || confidence > 1 {
		confidence = 0.78
	}

	return newConversationMemoryItem(
		now,
		itemType,
		scope,
		summary,
		strings.TrimSpace(candidate.Details),
		userInput,
		assistantReply,
		conversationText(userInput, assistantReply),
		confidence,
	), true
}

func extractJSONObject(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "```") {
		trimmed = strings.TrimPrefix(trimmed, "```json")
		trimmed = strings.TrimPrefix(trimmed, "```JSON")
		trimmed = strings.TrimPrefix(trimmed, "```")
		trimmed = strings.TrimSuffix(trimmed, "```")
		trimmed = strings.TrimSpace(trimmed)
	}

	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start == -1 || end == -1 || end < start {
		return ""
	}
	return trimmed[start : end+1]
}
