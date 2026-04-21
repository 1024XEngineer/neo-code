package app

import (
	"context"
	"strings"
	"sync"

	providertypes "neo-code/internal/provider/types"
	agentruntime "neo-code/internal/runtime"
	agentsession "neo-code/internal/session"
	"neo-code/internal/tools"
	tuiservices "neo-code/internal/tui/services"
)

type runtimeSessionLogPersistence interface {
	LoadSessionLogEntries(ctx context.Context, sessionID string) ([]agentruntime.SessionLogEntry, error)
	SaveSessionLogEntries(ctx context.Context, sessionID string, entries []agentruntime.SessionLogEntry) error
}

// runtimeContractAdapter 将 runtime.Runtime 适配为 TUI 侧契约接口。
type runtimeContractAdapter struct {
	runtime   agentruntime.Runtime
	closeOnce sync.Once
	closeCh   chan struct{}
	done      chan struct{}
	events    chan tuiservices.RuntimeEvent
}

// newRuntimeContractAdapter 创建本地 runtime 的契约适配器并启动事件桥接。
func newRuntimeContractAdapter(runtimeSvc agentruntime.Runtime) *runtimeContractAdapter {
	adapter := &runtimeContractAdapter{
		runtime: runtimeSvc,
		closeCh: make(chan struct{}),
		done:    make(chan struct{}),
		events:  make(chan tuiservices.RuntimeEvent, 128),
	}
	go adapter.forwardEvents()
	return adapter
}

// Submit 转发 submit 请求并做输入类型映射。
func (a *runtimeContractAdapter) Submit(ctx context.Context, input tuiservices.PrepareInput) error {
	if a == nil || a.runtime == nil {
		return context.Canceled
	}
	return a.runtime.Submit(ctx, convertPrepareInputToRuntime(input))
}

// PrepareUserInput 转发输入归一化请求并映射输出。
func (a *runtimeContractAdapter) PrepareUserInput(
	ctx context.Context,
	input tuiservices.PrepareInput,
) (tuiservices.UserInput, error) {
	if a == nil || a.runtime == nil {
		return tuiservices.UserInput{}, context.Canceled
	}
	prepared, err := a.runtime.PrepareUserInput(ctx, convertPrepareInputToRuntime(input))
	if err != nil {
		return tuiservices.UserInput{}, err
	}
	return convertUserInputFromRuntime(prepared), nil
}

// Run 转发 run 请求并做输入映射。
func (a *runtimeContractAdapter) Run(ctx context.Context, input tuiservices.UserInput) error {
	if a == nil || a.runtime == nil {
		return context.Canceled
	}
	return a.runtime.Run(ctx, convertUserInputToRuntime(input))
}

// Compact 转发 compact 请求并映射结果。
func (a *runtimeContractAdapter) Compact(
	ctx context.Context,
	input tuiservices.CompactInput,
) (tuiservices.CompactResult, error) {
	if a == nil || a.runtime == nil {
		return tuiservices.CompactResult{}, context.Canceled
	}
	result, err := a.runtime.Compact(ctx, agentruntime.CompactInput{
		SessionID: strings.TrimSpace(input.SessionID),
		RunID:     strings.TrimSpace(input.RunID),
	})
	if err != nil {
		return tuiservices.CompactResult{}, err
	}
	return tuiservices.CompactResult{
		Applied:        result.Applied,
		BeforeChars:    result.BeforeChars,
		AfterChars:     result.AfterChars,
		BeforeTokens:   result.BeforeTokens,
		SavedRatio:     result.SavedRatio,
		TriggerMode:    result.TriggerMode,
		TranscriptID:   result.TranscriptID,
		TranscriptPath: result.TranscriptPath,
	}, nil
}

// ExecuteSystemTool 转发系统工具执行请求。
func (a *runtimeContractAdapter) ExecuteSystemTool(
	ctx context.Context,
	input tuiservices.SystemToolInput,
) (tools.ToolResult, error) {
	if a == nil || a.runtime == nil {
		return tools.ToolResult{}, context.Canceled
	}
	return a.runtime.ExecuteSystemTool(ctx, agentruntime.SystemToolInput{
		SessionID: strings.TrimSpace(input.SessionID),
		RunID:     strings.TrimSpace(input.RunID),
		Workdir:   strings.TrimSpace(input.Workdir),
		ToolName:  strings.TrimSpace(input.ToolName),
		Arguments: append([]byte(nil), input.Arguments...),
	})
}

// ResolvePermission 转发权限决策。
func (a *runtimeContractAdapter) ResolvePermission(ctx context.Context, input tuiservices.PermissionResolutionInput) error {
	if a == nil || a.runtime == nil {
		return context.Canceled
	}
	return a.runtime.ResolvePermission(ctx, agentruntime.PermissionResolutionInput{
		RequestID: strings.TrimSpace(input.RequestID),
		Decision:  agentruntime.PermissionResolutionDecision(strings.TrimSpace(string(input.Decision))),
	})
}

// CancelActiveRun 转发取消请求。
func (a *runtimeContractAdapter) CancelActiveRun() bool {
	if a == nil || a.runtime == nil {
		return false
	}
	return a.runtime.CancelActiveRun()
}

// Events 返回契约化后的事件流。
func (a *runtimeContractAdapter) Events() <-chan tuiservices.RuntimeEvent {
	if a == nil {
		return nil
	}
	return a.events
}

// ListSessions 转发会话摘要查询。
func (a *runtimeContractAdapter) ListSessions(ctx context.Context) ([]agentsession.Summary, error) {
	if a == nil || a.runtime == nil {
		return nil, context.Canceled
	}
	return a.runtime.ListSessions(ctx)
}

// LoadSession 转发会话详情查询。
func (a *runtimeContractAdapter) LoadSession(ctx context.Context, id string) (agentsession.Session, error) {
	if a == nil || a.runtime == nil {
		return agentsession.Session{}, context.Canceled
	}
	return a.runtime.LoadSession(ctx, strings.TrimSpace(id))
}

// ActivateSessionSkill 转发技能激活请求。
func (a *runtimeContractAdapter) ActivateSessionSkill(ctx context.Context, sessionID string, skillID string) error {
	if a == nil || a.runtime == nil {
		return context.Canceled
	}
	return a.runtime.ActivateSessionSkill(ctx, strings.TrimSpace(sessionID), strings.TrimSpace(skillID))
}

// DeactivateSessionSkill 转发技能停用请求。
func (a *runtimeContractAdapter) DeactivateSessionSkill(ctx context.Context, sessionID string, skillID string) error {
	if a == nil || a.runtime == nil {
		return context.Canceled
	}
	return a.runtime.DeactivateSessionSkill(ctx, strings.TrimSpace(sessionID), strings.TrimSpace(skillID))
}

// ListSessionSkills 转发技能列表查询并映射状态结构。
func (a *runtimeContractAdapter) ListSessionSkills(ctx context.Context, sessionID string) ([]tuiservices.SessionSkillState, error) {
	if a == nil || a.runtime == nil {
		return nil, context.Canceled
	}
	states, err := a.runtime.ListSessionSkills(ctx, strings.TrimSpace(sessionID))
	if err != nil {
		return nil, err
	}
	mapped := make([]tuiservices.SessionSkillState, 0, len(states))
	for _, item := range states {
		mapped = append(mapped, tuiservices.SessionSkillState{
			SkillID:    item.SkillID,
			Missing:    item.Missing,
			Descriptor: item.Descriptor,
		})
	}
	return mapped, nil
}

// LoadSessionLogEntries 在本地模式下读取会话日志条目。
func (a *runtimeContractAdapter) LoadSessionLogEntries(
	ctx context.Context,
	sessionID string,
) ([]tuiservices.SessionLogEntry, error) {
	if a == nil || a.runtime == nil {
		return nil, nil
	}
	store, ok := a.runtime.(runtimeSessionLogPersistence)
	if !ok {
		return nil, nil
	}
	entries, err := store.LoadSessionLogEntries(ctx, strings.TrimSpace(sessionID))
	if err != nil {
		return nil, err
	}
	mapped := make([]tuiservices.SessionLogEntry, 0, len(entries))
	for _, item := range entries {
		mapped = append(mapped, tuiservices.SessionLogEntry{
			Timestamp: item.Timestamp,
			Level:     item.Level,
			Source:    item.Source,
			Message:   item.Message,
		})
	}
	return mapped, nil
}

// SaveSessionLogEntries 在本地模式下保存会话日志条目。
func (a *runtimeContractAdapter) SaveSessionLogEntries(
	ctx context.Context,
	sessionID string,
	entries []tuiservices.SessionLogEntry,
) error {
	if a == nil || a.runtime == nil {
		return nil
	}
	store, ok := a.runtime.(runtimeSessionLogPersistence)
	if !ok {
		return nil
	}
	mapped := make([]agentruntime.SessionLogEntry, 0, len(entries))
	for _, item := range entries {
		mapped = append(mapped, agentruntime.SessionLogEntry{
			Timestamp: item.Timestamp,
			Level:     item.Level,
			Source:    item.Source,
			Message:   item.Message,
		})
	}
	return store.SaveSessionLogEntries(ctx, strings.TrimSpace(sessionID), mapped)
}

// Close 停止事件桥接协程，避免 TUI 退出时泄漏 goroutine。
func (a *runtimeContractAdapter) Close() error {
	if a == nil {
		return nil
	}
	a.closeOnce.Do(func() {
		close(a.closeCh)
		<-a.done
	})
	return nil
}

// forwardEvents 持续消费 runtime 事件并映射为 TUI 契约事件。
func (a *runtimeContractAdapter) forwardEvents() {
	defer close(a.done)
	defer close(a.events)
	if a == nil || a.runtime == nil {
		return
	}

	source := a.runtime.Events()
	for {
		select {
		case <-a.closeCh:
			return
		case event, ok := <-source:
			if !ok {
				return
			}
			mapped := convertRuntimeEventToContract(event)
			select {
			case <-a.closeCh:
				return
			case a.events <- mapped:
			}
		}
	}
}

// convertPrepareInputToRuntime 将契约输入映射为 runtime 输入。
func convertPrepareInputToRuntime(input tuiservices.PrepareInput) agentruntime.PrepareInput {
	images := make([]agentruntime.UserImageInput, 0, len(input.Images))
	for _, image := range input.Images {
		images = append(images, agentruntime.UserImageInput{
			Path:     strings.TrimSpace(image.Path),
			MimeType: strings.TrimSpace(image.MimeType),
		})
	}
	return agentruntime.PrepareInput{
		SessionID: strings.TrimSpace(input.SessionID),
		RunID:     strings.TrimSpace(input.RunID),
		Workdir:   strings.TrimSpace(input.Workdir),
		Text:      input.Text,
		Images:    images,
	}
}

// convertUserInputToRuntime 将契约 UserInput 映射为 runtime UserInput。
func convertUserInputToRuntime(input tuiservices.UserInput) agentruntime.UserInput {
	parts := append([]providertypes.ContentPart(nil), input.Parts...)
	return agentruntime.UserInput{
		SessionID: strings.TrimSpace(input.SessionID),
		RunID:     strings.TrimSpace(input.RunID),
		Parts:     parts,
		Workdir:   strings.TrimSpace(input.Workdir),
		TaskID:    strings.TrimSpace(input.TaskID),
		AgentID:   strings.TrimSpace(input.AgentID),
	}
}

// convertUserInputFromRuntime 将 runtime UserInput 映射为契约 UserInput。
func convertUserInputFromRuntime(input agentruntime.UserInput) tuiservices.UserInput {
	parts := append([]providertypes.ContentPart(nil), input.Parts...)
	return tuiservices.UserInput{
		SessionID: strings.TrimSpace(input.SessionID),
		RunID:     strings.TrimSpace(input.RunID),
		Parts:     parts,
		Workdir:   strings.TrimSpace(input.Workdir),
		TaskID:    strings.TrimSpace(input.TaskID),
		AgentID:   strings.TrimSpace(input.AgentID),
	}
}

// convertRuntimeEventToContract 将 runtime 事件映射为 TUI 契约事件。
func convertRuntimeEventToContract(event agentruntime.RuntimeEvent) tuiservices.RuntimeEvent {
	return tuiservices.RuntimeEvent{
		Type:           tuiservices.EventType(event.Type),
		RunID:          strings.TrimSpace(event.RunID),
		SessionID:      strings.TrimSpace(event.SessionID),
		Turn:           event.Turn,
		Phase:          strings.TrimSpace(event.Phase),
		Timestamp:      event.Timestamp,
		PayloadVersion: event.PayloadVersion,
		Payload:        convertRuntimePayloadToContract(event.Payload),
	}
}

// convertRuntimePayloadToContract 将 runtime payload 规范化为契约 payload。
func convertRuntimePayloadToContract(payload any) any {
	switch typed := payload.(type) {
	case agentruntime.PermissionRequestPayload:
		return tuiservices.PermissionRequestPayload{
			RequestID:     typed.RequestID,
			ToolCallID:    typed.ToolCallID,
			ToolName:      typed.ToolName,
			ToolCategory:  typed.ToolCategory,
			ActionType:    typed.ActionType,
			Operation:     typed.Operation,
			TargetType:    typed.TargetType,
			Target:        typed.Target,
			Decision:      typed.Decision,
			Reason:        typed.Reason,
			RuleID:        typed.RuleID,
			RememberScope: typed.RememberScope,
		}
	case agentruntime.PermissionResolvedPayload:
		return tuiservices.PermissionResolvedPayload{
			RequestID:     typed.RequestID,
			ToolCallID:    typed.ToolCallID,
			ToolName:      typed.ToolName,
			ToolCategory:  typed.ToolCategory,
			ActionType:    typed.ActionType,
			Operation:     typed.Operation,
			TargetType:    typed.TargetType,
			Target:        typed.Target,
			Decision:      typed.Decision,
			Reason:        typed.Reason,
			RuleID:        typed.RuleID,
			RememberScope: typed.RememberScope,
			ResolvedAs:    typed.ResolvedAs,
		}
	case agentruntime.CompactResult:
		return tuiservices.CompactResult{
			Applied:        typed.Applied,
			BeforeChars:    typed.BeforeChars,
			AfterChars:     typed.AfterChars,
			BeforeTokens:   typed.BeforeTokens,
			SavedRatio:     typed.SavedRatio,
			TriggerMode:    typed.TriggerMode,
			TranscriptID:   typed.TranscriptID,
			TranscriptPath: typed.TranscriptPath,
		}
	case agentruntime.CompactErrorPayload:
		return tuiservices.CompactErrorPayload{TriggerMode: typed.TriggerMode, Message: typed.Message}
	case agentruntime.PhaseChangedPayload:
		return tuiservices.PhaseChangedPayload{From: typed.From, To: typed.To}
	case agentruntime.StopReasonDecidedPayload:
		return tuiservices.StopReasonDecidedPayload{
			Reason: tuiservices.StopReason(strings.TrimSpace(string(typed.Reason))),
			Detail: typed.Detail,
		}
	case agentruntime.TodoEventPayload:
		return tuiservices.TodoEventPayload{Action: typed.Action, Reason: typed.Reason}
	case agentruntime.InputNormalizedPayload:
		return tuiservices.InputNormalizedPayload{TextLength: typed.TextLength, ImageCount: typed.ImageCount}
	case agentruntime.AssetSavedPayload:
		return tuiservices.AssetSavedPayload{
			Index:    typed.Index,
			Path:     typed.Path,
			AssetID:  typed.AssetID,
			MimeType: typed.MimeType,
			Size:     typed.Size,
		}
	case agentruntime.AssetSaveFailedPayload:
		return tuiservices.AssetSaveFailedPayload{Index: typed.Index, Path: typed.Path, Message: typed.Message}
	default:
		return payload
	}
}

var _ tuiservices.Runtime = (*runtimeContractAdapter)(nil)
