package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"neo-code/internal/config"
	agentcontext "neo-code/internal/context"
	"neo-code/internal/partsrender"
	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/runtime/streaming"
	"neo-code/internal/skills"
)

const (
	askSessionPrefix     = "ask"
	askRunPrefix         = "ask-run"
	askSessionTTL        = 30 * time.Minute
	askSessionMaxTurns   = 64
	askSummaryUserPrompt = "之前对话摘要"
)

const askSystemPrompt = `You are NeoCode Ask mode assistant.
You must answer in one round without tools.
Do not claim to run any command or modify files directly.
Focus on diagnosis, root cause analysis, and practical next steps.`

// Ask 走 Ask 专用单轮推理链路：无预算、无工具、无验收、无 checkpoint。
func (s *Service) Ask(ctx context.Context, input AskInput) error {
	if s == nil {
		return fmt.Errorf("runtime: service is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	userQuery := strings.TrimSpace(input.UserQuery)
	if userQuery == "" {
		return fmt.Errorf("runtime: ask user query is empty")
	}
	if s.askStore == nil {
		s.askStore = newInMemoryAskSessionStore(askSessionTTL)
	}

	sessionID := strings.TrimSpace(input.SessionID)
	if sessionID == "" {
		sessionID = s.generateAskSessionID()
	}
	runID := strings.TrimSpace(input.RunID)
	if runID == "" {
		runID = s.generateAskRunID()
	}
	failAsk := func(err error) error {
		if err == nil {
			return nil
		}
		s.emitAskError(ctx, runID, sessionID, err)
		return err
	}

	session, loaded, err := s.askStore.Load(ctx, sessionID)
	if err != nil {
		return failAsk(err)
	}
	if !loaded {
		session = AskSession{
			ID:      sessionID,
			Workdir: strings.TrimSpace(input.Workdir),
			Skills:  normalizeAskSkillIDs(input.Skills),
		}
	} else {
		// 后续调用允许更新Skills 激活集
		if len(input.Skills) > 0 {
			session.Skills = normalizeAskSkillIDs(input.Skills)
		}
	}

	buildResult := agentcontext.BuildAskPrompt(
		askMessagesToTurns(session.Messages),
		userQuery,
		s.resolveAskPromptConfig(),
	)
	if strings.TrimSpace(buildResult.Prompt) == "" {
		return failAsk(errors.New("runtime: ask prompt is empty"))
	}

	if buildResult.Compacted {
		session.Messages = compactAskSessionMessages(buildResult)
	}
	session = appendAskMessage(session, "user", userQuery)
	if err := s.askStore.Save(ctx, session); err != nil {
		return failAsk(err)
	}

	cfg, err := s.loadConfigSnapshot(ctx)
	if err != nil {
		return failAsk(err)
	}
	selectedProvider, err := config.ResolveSelectedProvider(cfg)
	if err != nil {
		return failAsk(err)
	}
	providerCfg, err := selectedProvider.ToRuntimeConfig()
	if err != nil {
		return failAsk(err)
	}
	modelProvider, err := s.providerFactory.Build(ctx, providerCfg)
	if err != nil {
		return failAsk(err)
	}

	generateRequest := providertypes.GenerateRequest{
		Model:        strings.TrimSpace(cfg.CurrentModel),
		SystemPrompt: buildAskSystemPrompt(ctx, s.skillsRegistry, session.Skills),
		Messages: []providertypes.Message{
			{
				Role: providertypes.RoleUser,
				Parts: []providertypes.ContentPart{
					providertypes.NewTextPart(buildResult.Prompt),
				},
			},
		},
		Tools: nil,
		ThinkingConfig: &providertypes.ThinkingConfig{
			Enabled: false,
		},
	}

	streamOutcome := generateStreamingMessage(ctx, modelProvider, generateRequest, streaming.Hooks{
		OnTextDelta: func(text string) {
			if text == "" {
				return
			}
			_ = s.emit(ctx, EventAgentChunk, runID, session.ID, map[string]any{
				"delta": text,
			})
		},
	})
	if streamOutcome.err != nil {
		return failAsk(streamOutcome.err)
	}

	reply := strings.TrimSpace(partsrender.RenderDisplayParts(streamOutcome.message.Parts))
	session = appendAskMessage(session, "assistant", reply)
	if err := s.askStore.Save(ctx, session); err != nil {
		return failAsk(err)
	}

	_ = s.emit(ctx, EventAgentDone, runID, session.ID, map[string]any{
		"full_response": reply,
		"compacted":     buildResult.Compacted,
		"usage": map[string]any{
			"input_tokens":  streamOutcome.inputTokens,
			"output_tokens": streamOutcome.outputTokens,
		},
	})
	return nil
}

// DeleteAskSession 删除 Ask 轻量会话并返回删除结果。
func (s *Service) DeleteAskSession(ctx context.Context, input DeleteAskSessionInput) (bool, error) {
	if s == nil {
		return false, fmt.Errorf("runtime: service is nil")
	}
	sessionID := strings.TrimSpace(input.SessionID)
	if sessionID == "" {
		return false, nil
	}
	if s.askStore == nil {
		return false, nil
	}
	return s.askStore.Delete(ctx, sessionID)
}

// generateAskSessionID 生成 Ask 会话标识。
func (s *Service) generateAskSessionID() string {
	sequence := atomic.AddUint64(&s.askSequence, 1)
	return fmt.Sprintf("%s-%d-%d", askSessionPrefix, os.Getpid(), sequence)
}

// generateAskRunID 生成 Ask 运行标识。
func (s *Service) generateAskRunID() string {
	sequence := atomic.AddUint64(&s.askSequence, 1)
	return fmt.Sprintf("%s-%d-%d", askRunPrefix, os.Getpid(), sequence)
}

// resolveAskPromptConfig 读取 Ask Prompt 配置并补齐默认值。
func (s *Service) resolveAskPromptConfig() agentcontext.AskPromptConfig {
	out := agentcontext.AskPromptConfig{
		MaxInputTokens:  config.DefaultAskMaxInputTokens,
		RetainTurns:     config.DefaultAskRetainTurns,
		SummaryMaxChars: config.DefaultAskSummaryMaxChars,
	}
	if s == nil || s.configManager == nil {
		return out
	}
	current := s.configManager.Get()
	if current.Context.Ask.MaxInputTokens > 0 {
		out.MaxInputTokens = current.Context.Ask.MaxInputTokens
	}
	if current.Context.Ask.RetainTurns > 0 {
		out.RetainTurns = current.Context.Ask.RetainTurns
	}
	if current.Context.Ask.SummaryMaxChars > 0 {
		out.SummaryMaxChars = current.Context.Ask.SummaryMaxChars
	}
	return out
}

// normalizeAskSkillIDs 归一化并去重 Ask 会话绑定的技能列表。
func normalizeAskSkillIDs(skillIDs []string) []string {
	if len(skillIDs) == 0 {
		return nil
	}
	out := make([]string, 0, len(skillIDs))
	seen := make(map[string]struct{}, len(skillIDs))
	for _, skillID := range skillIDs {
		normalized := strings.TrimSpace(skillID)
		if normalized == "" {
			continue
		}
		key := strings.ToLower(normalized)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

// askMessagesToTurns 将 Ask 消息序列转换为问答轮次，便于构建 Ask Prompt。
func askMessagesToTurns(messages []AskMessage) []agentcontext.AskTurn {
	if len(messages) == 0 {
		return nil
	}
	turns := make([]agentcontext.AskTurn, 0, len(messages)/2+1)
	currentUser := ""
	for _, message := range messages {
		role := normalizeAskMessageRole(message.Role)
		content := strings.TrimSpace(message.Content)
		if role == "user" {
			if strings.TrimSpace(currentUser) != "" {
				turns = append(turns, agentcontext.AskTurn{UserQuery: currentUser})
			}
			currentUser = content
			continue
		}
		if strings.TrimSpace(currentUser) == "" {
			continue
		}
		turns = append(turns, agentcontext.AskTurn{
			UserQuery: currentUser,
			Assistant: content,
		})
		currentUser = ""
	}
	if strings.TrimSpace(currentUser) != "" {
		turns = append(turns, agentcontext.AskTurn{UserQuery: currentUser})
	}
	return turns
}

// compactAskSessionMessages 将压缩结果回写为 AskSession 消息，防止历史无限膨胀。
func compactAskSessionMessages(result agentcontext.AskPromptBuildResult) []AskMessage {
	out := make([]AskMessage, 0, len(result.RetainedTurns)*2+2)
	if summary := strings.TrimSpace(result.Summary); summary != "" {
		out = append(out, AskMessage{
			Role:    "user",
			Content: askSummaryUserPrompt,
		})
		out = append(out, AskMessage{
			Role:    "assistant",
			Content: summary,
		})
	}
	for _, turn := range result.RetainedTurns {
		query := strings.TrimSpace(turn.UserQuery)
		if query == "" {
			continue
		}
		out = append(out, AskMessage{
			Role:    "user",
			Content: query,
		})
		if answer := strings.TrimSpace(turn.Assistant); answer != "" {
			out = append(out, AskMessage{
				Role:    "assistant",
				Content: answer,
			})
		}
	}
	return out
}

// appendAskMessage 向 Ask 会话追加一条消息，并保持最大历史轮次数。
func appendAskMessage(session AskSession, role string, content string) AskSession {
	trimmedContent := strings.TrimSpace(content)
	if trimmedContent == "" {
		return session
	}
	session.Messages = append(session.Messages, AskMessage{
		Role:    normalizeAskMessageRole(role),
		Content: trimmedContent,
	})
	turns := askMessagesToTurns(session.Messages)
	if len(turns) > askSessionMaxTurns {
		turns = append([]agentcontext.AskTurn(nil), turns[len(turns)-askSessionMaxTurns:]...)
		session.Messages = compactAskSessionMessages(agentcontext.AskPromptBuildResult{
			RetainedTurns: turns,
		})
	}
	return session
}

// buildAskSystemPrompt 组装 Ask 模式 system prompt，并按需注入技能说明。
func buildAskSystemPrompt(ctx context.Context, registry skills.Registry, skillIDs []string) string {
	sections := []string{askSystemPrompt}
	if registry == nil || len(skillIDs) == 0 {
		return strings.TrimSpace(strings.Join(sections, "\n\n"))
	}
	seen := make(map[string]struct{}, len(skillIDs))
	for _, skillID := range skillIDs {
		normalizedSkillID := strings.TrimSpace(skillID)
		if normalizedSkillID == "" {
			continue
		}
		key := strings.ToLower(normalizedSkillID)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		descriptor, content, err := registry.Get(ctx, normalizedSkillID)
		if err != nil {
			continue
		}
		instruction := strings.TrimSpace(content.Instruction)
		if instruction == "" {
			continue
		}
		sections = append(sections, fmt.Sprintf("Skill `%s`:\n%s", strings.TrimSpace(descriptor.ID), instruction))
	}
	return strings.TrimSpace(strings.Join(sections, "\n\n"))
}

// mapAskErrorCode 将运行时错误归类为 Ask 协议错误码。
func mapAskErrorCode(err error) string {
	if err == nil {
		return "INTERNAL_ERROR"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "TIMEOUT"
	}
	if errors.Is(err, context.Canceled) {
		return "CANCELED"
	}
	var providerErr *provider.ProviderError
	if errors.As(err, &providerErr) {
		switch providerErr.Code {
		case provider.ErrorCodeRateLimit:
			return "RATE_LIMITED"
		case provider.ErrorCodeTimeout:
			return "TIMEOUT"
		default:
			return "PROVIDER_ERROR"
		}
	}
	return "INTERNAL_ERROR"
}

// emitAskError 统一发射 Ask 失败事件，供 gateway 侧映射为 ask_error 协议事件。
func (s *Service) emitAskError(ctx context.Context, runID string, sessionID string, err error) {
	if s == nil || err == nil {
		return
	}
	_ = s.emit(ctx, EventError, strings.TrimSpace(runID), strings.TrimSpace(sessionID), map[string]any{
		"code":    mapAskErrorCode(err),
		"message": strings.TrimSpace(err.Error()),
	})
}
