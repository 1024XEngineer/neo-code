package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"neo-code/internal/app"
	"neo-code/internal/gateway"
	providertypes "neo-code/internal/provider/types"
	agentruntime "neo-code/internal/runtime"
	agentsession "neo-code/internal/session"
	"neo-code/internal/skills"
	"neo-code/internal/tools"
)

const bridgeLocalSubjectID = "local_admin"
const bridgeRuntimeUnavailableErrMsg = "gateway runtime bridge: runtime is unavailable"

type runtimeRunCanceler interface {
	CancelRun(runID string) bool
}

type runtimeSessionCreator interface {
	CreateSession(ctx context.Context, id string) (agentsession.Session, error)
}

// defaultBuildGatewayRuntimePort 构建网关运行时 RuntimePort 适配器，并返回对应资源清理函数。
func defaultBuildGatewayRuntimePort(ctx context.Context, workdir string) (gateway.RuntimePort, func() error, error) {
	bundle, err := app.BuildGatewayServerDeps(ctx, app.BootstrapOptions{Workdir: strings.TrimSpace(workdir)})
	if err != nil {
		return nil, nil, err
	}

	bridge, err := newGatewayRuntimePortBridge(ctx, bundle.Runtime)
	if err != nil {
		if bundle.Close != nil {
			_ = bundle.Close()
		}
		return nil, nil, err
	}

	cleanup := func() error {
		var closeErr error
		if bridge != nil {
			closeErr = errors.Join(closeErr, bridge.Close())
		}
		if bundle.Close != nil {
			closeErr = errors.Join(closeErr, bundle.Close())
		}
		return closeErr
	}

	return bridge, cleanup, nil
}

// gatewayRuntimePortBridge 将 runtime.Runtime 适配为 gateway.RuntimePort，并负责事件流桥接。
type gatewayRuntimePortBridge struct {
	runtime agentruntime.Runtime
	events  chan gateway.RuntimeEvent

	stopOnce sync.Once
	stopCh   chan struct{}
}

// newGatewayRuntimePortBridge 创建 RuntimePort 桥接器，用于把 runtime 事件转换为 gateway 统一事件。
func newGatewayRuntimePortBridge(ctx context.Context, runtimeSvc agentruntime.Runtime) (*gatewayRuntimePortBridge, error) {
	if runtimeSvc == nil {
		return nil, fmt.Errorf("gateway runtime bridge: runtime is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	bridge := &gatewayRuntimePortBridge{
		runtime: runtimeSvc,
		events:  make(chan gateway.RuntimeEvent, 128),
		stopCh:  make(chan struct{}),
	}
	go bridge.runEventBridge(ctx)
	return bridge, nil
}

// Run 将 gateway.run 输入转换为 runtime Submit 输入。
func (b *gatewayRuntimePortBridge) Run(ctx context.Context, input gateway.RunInput) error {
	if err := b.ensureRuntimeAccess(input.SubjectID); err != nil {
		return err
	}
	return b.runtime.Submit(ctx, convertGatewayRunInput(input))
}

// Compact 将 gateway.compact 请求映射到 runtime 紧凑化能力并回填统一结果。
func (b *gatewayRuntimePortBridge) Compact(ctx context.Context, input gateway.CompactInput) (gateway.CompactResult, error) {
	if err := b.ensureRuntimeAccess(input.SubjectID); err != nil {
		return gateway.CompactResult{}, err
	}

	result, err := b.runtime.Compact(ctx, agentruntime.CompactInput{
		SessionID: strings.TrimSpace(input.SessionID),
		RunID:     strings.TrimSpace(input.RunID),
	})
	if err != nil {
		return gateway.CompactResult{}, err
	}

	return gateway.CompactResult{
		Applied:        result.Applied,
		BeforeChars:    result.BeforeChars,
		AfterChars:     result.AfterChars,
		SavedRatio:     result.SavedRatio,
		TriggerMode:    result.TriggerMode,
		TranscriptID:   result.TranscriptID,
		TranscriptPath: result.TranscriptPath,
	}, nil
}

// ExecuteSystemTool 将 gateway.executeSystemTool 请求映射到 runtime 系统工具执行能力。
func (b *gatewayRuntimePortBridge) ExecuteSystemTool(
	ctx context.Context,
	input gateway.ExecuteSystemToolInput,
) (tools.ToolResult, error) {
	if err := b.ensureRuntimeAccess(input.SubjectID); err != nil {
		return tools.ToolResult{}, err
	}

	return b.runtime.ExecuteSystemTool(ctx, agentruntime.SystemToolInput{
		SessionID: strings.TrimSpace(input.SessionID),
		RunID:     strings.TrimSpace(input.RunID),
		Workdir:   strings.TrimSpace(input.Workdir),
		ToolName:  strings.TrimSpace(input.ToolName),
		Arguments: append([]byte(nil), input.Arguments...),
	})
}

// ActivateSessionSkill 将 gateway.activateSessionSkill 请求映射到 runtime 会话技能激活能力。
func (b *gatewayRuntimePortBridge) ActivateSessionSkill(
	ctx context.Context,
	input gateway.SessionSkillMutationInput,
) error {
	if err := b.ensureRuntimeAccess(input.SubjectID); err != nil {
		return err
	}
	return b.runtime.ActivateSessionSkill(
		ctx,
		strings.TrimSpace(input.SessionID),
		strings.TrimSpace(input.SkillID),
	)
}

// DeactivateSessionSkill 将 gateway.deactivateSessionSkill 请求映射到 runtime 会话技能停用能力。
func (b *gatewayRuntimePortBridge) DeactivateSessionSkill(
	ctx context.Context,
	input gateway.SessionSkillMutationInput,
) error {
	if err := b.ensureRuntimeAccess(input.SubjectID); err != nil {
		return err
	}
	return b.runtime.DeactivateSessionSkill(
		ctx,
		strings.TrimSpace(input.SessionID),
		strings.TrimSpace(input.SkillID),
	)
}

// ListSessionSkills 查询会话激活技能列表，并映射为 gateway 契约输出。
func (b *gatewayRuntimePortBridge) ListSessionSkills(
	ctx context.Context,
	input gateway.ListSessionSkillsInput,
) ([]gateway.SessionSkillState, error) {
	if err := b.ensureRuntimeAccess(input.SubjectID); err != nil {
		return nil, err
	}
	states, err := b.runtime.ListSessionSkills(ctx, strings.TrimSpace(input.SessionID))
	if err != nil {
		return nil, err
	}
	return convertRuntimeSessionSkillStates(states), nil
}

// ListAvailableSkills 查询可用技能列表，并映射为 gateway 契约输出。
func (b *gatewayRuntimePortBridge) ListAvailableSkills(
	ctx context.Context,
	input gateway.ListAvailableSkillsInput,
) ([]gateway.AvailableSkillState, error) {
	if err := b.ensureRuntimeAccess(input.SubjectID); err != nil {
		return nil, err
	}
	states, err := b.runtime.ListAvailableSkills(ctx, strings.TrimSpace(input.SessionID))
	if err != nil {
		return nil, err
	}
	return convertRuntimeAvailableSkillStates(states), nil
}

// ResolvePermission 将网关审批决策转换为 runtime 审批输入并提交。
func (b *gatewayRuntimePortBridge) ResolvePermission(ctx context.Context, input gateway.PermissionResolutionInput) error {
	if err := b.ensureRuntimeAccess(input.SubjectID); err != nil {
		return err
	}
	return b.runtime.ResolvePermission(ctx, agentruntime.PermissionResolutionInput{
		RequestID: strings.TrimSpace(input.RequestID),
		Decision:  agentruntime.PermissionResolutionDecision(strings.TrimSpace(string(input.Decision))),
	})
}

// CancelRun 转发 gateway.cancel 请求到 runtime 的 run_id 精确取消能力。
func (b *gatewayRuntimePortBridge) CancelRun(_ context.Context, input gateway.CancelInput) (bool, error) {
	if err := b.ensureRuntimeAccess(input.SubjectID); err != nil {
		return false, err
	}

	runID := strings.TrimSpace(input.RunID)
	if runID == "" {
		return false, gateway.ErrRuntimeResourceNotFound
	}
	canceler, ok := b.runtime.(runtimeRunCanceler)
	if !ok {
		return false, fmt.Errorf("gateway runtime bridge: runtime does not support cancel by run_id")
	}
	if !canceler.CancelRun(runID) {
		return false, gateway.ErrRuntimeResourceNotFound
	}
	return true, nil
}

// Events 返回桥接后的 gateway 统一事件流。
func (b *gatewayRuntimePortBridge) Events() <-chan gateway.RuntimeEvent {
	if b == nil {
		return nil
	}
	return b.events
}

// ListSessions 返回会话摘要列表，并转换为 gateway 契约结构。
func (b *gatewayRuntimePortBridge) ListSessions(ctx context.Context) ([]gateway.SessionSummary, error) {
	if b == nil || b.runtime == nil {
		return nil, fmt.Errorf("gateway runtime bridge: runtime is unavailable")
	}

	summaries, err := b.runtime.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	if len(summaries) == 0 {
		return nil, nil
	}

	converted := make([]gateway.SessionSummary, 0, len(summaries))
	for _, summary := range summaries {
		converted = append(converted, gateway.SessionSummary{
			ID:        strings.TrimSpace(summary.ID),
			Title:     strings.TrimSpace(summary.Title),
			CreatedAt: summary.CreatedAt,
			UpdatedAt: summary.UpdatedAt,
		})
	}
	return converted, nil
}

// LoadSession 加载单个会话详情，并做跨层消息结构映射。
func (b *gatewayRuntimePortBridge) LoadSession(ctx context.Context, input gateway.LoadSessionInput) (gateway.Session, error) {
	if err := b.ensureRuntimeAccess(input.SubjectID); err != nil {
		return gateway.Session{}, err
	}

	sessionID := strings.TrimSpace(input.SessionID)
	session, err := b.runtime.LoadSession(ctx, sessionID)
	if err != nil {
		if isRuntimeNotFoundError(err) {
			// TODO: 待 TUI Submit 显式调用 gateway.createSession 后移除此 upsert。
			creator, ok := b.runtime.(runtimeSessionCreator)
			if !ok {
				return gateway.Session{}, gateway.ErrRuntimeResourceNotFound
			}
			created, createErr := creator.CreateSession(ctx, sessionID)
			if createErr != nil {
				return gateway.Session{}, createErr
			}
			return convertRuntimeSessionToGatewaySession(created), nil
		}
		return gateway.Session{}, err
	}

	return convertRuntimeSessionToGatewaySession(session), nil
}

// CreateSession 创建或加载指定会话，并返回最终可用的会话标识。
func (b *gatewayRuntimePortBridge) CreateSession(ctx context.Context, input gateway.CreateSessionInput) (string, error) {
	if err := b.ensureRuntimeAccess(input.SubjectID); err != nil {
		return "", err
	}

	creator, ok := b.runtime.(runtimeSessionCreator)
	if !ok {
		return "", fmt.Errorf("gateway runtime bridge: runtime does not support create_session")
	}
	session, err := creator.CreateSession(ctx, strings.TrimSpace(input.SessionID))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(session.ID), nil
}

// Close 主动停止桥接事件泵，避免网关关闭后后台协程悬挂。
func (b *gatewayRuntimePortBridge) Close() error {
	if b == nil {
		return nil
	}
	b.stopOnce.Do(func() {
		close(b.stopCh)
	})
	return nil
}

// runEventBridge 持续消费 runtime 事件并输出到 gateway 统一事件通道。
func (b *gatewayRuntimePortBridge) runEventBridge(ctx context.Context) {
	defer close(b.events)
	if b == nil || b.runtime == nil {
		return
	}

	source := b.runtime.Events()
	if source == nil {
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-b.stopCh:
			return
		case event, ok := <-source:
			if !ok {
				return
			}
			mappedEvent := convertRuntimeEvent(event)
			select {
			case <-ctx.Done():
				return
			case <-b.stopCh:
				return
			case b.events <- mappedEvent:
			}
		}
	}
}

// convertGatewayRunInput 将 gateway.run 输入转换为 runtime PrepareInput。
func convertGatewayRunInput(input gateway.RunInput) agentruntime.PrepareInput {
	textParts := make([]string, 0, 1)
	if baseText := strings.TrimSpace(input.InputText); baseText != "" {
		textParts = append(textParts, baseText)
	}

	images := make([]agentruntime.UserImageInput, 0)
	for _, part := range input.InputParts {
		switch part.Type {
		case gateway.InputPartTypeText:
			if text := strings.TrimSpace(part.Text); text != "" {
				textParts = append(textParts, text)
			}
		case gateway.InputPartTypeImage:
			if part.Media == nil {
				continue
			}
			path := strings.TrimSpace(part.Media.URI)
			if path == "" {
				continue
			}
			images = append(images, agentruntime.UserImageInput{
				Path:     path,
				MimeType: strings.TrimSpace(part.Media.MimeType),
			})
		}
	}

	runID := strings.TrimSpace(input.RunID)
	if runID == "" {
		runID = strings.TrimSpace(input.RequestID)
	}

	return agentruntime.PrepareInput{
		SessionID: strings.TrimSpace(input.SessionID),
		RunID:     runID,
		Workdir:   strings.TrimSpace(input.Workdir),
		Text:      strings.Join(textParts, "\n"),
		Images:    images,
	}
}

// convertRuntimeEvent 将 runtime 事件映射为 gateway 事件，保证 stream relay 只关心统一契约。
func convertRuntimeEvent(event agentruntime.RuntimeEvent) gateway.RuntimeEvent {
	payload := map[string]any{
		"runtime_event_type": string(event.Type),
		"turn":               event.Turn,
		"phase":              strings.TrimSpace(event.Phase),
		"timestamp":          event.Timestamp,
		"payload_version":    event.PayloadVersion,
		"payload":            event.Payload,
	}
	return gateway.RuntimeEvent{
		Type:      mapRuntimeEventType(event.Type),
		RunID:     strings.TrimSpace(event.RunID),
		SessionID: strings.TrimSpace(event.SessionID),
		Payload:   payload,
	}
}

// mapRuntimeEventType 收敛 runtime 细粒度事件到网关约定的进度/完成/错误三态。
func mapRuntimeEventType(eventType agentruntime.EventType) gateway.RuntimeEventType {
	switch eventType {
	case agentruntime.EventAgentDone:
		return gateway.RuntimeEventTypeRunDone
	case agentruntime.EventError, agentruntime.EventRunCanceled:
		return gateway.RuntimeEventTypeRunError
	default:
		return gateway.RuntimeEventTypeRunProgress
	}
}

// convertSessionMessages 将会话消息列表转换为 gateway 统一输出格式。
func convertSessionMessages(messages []providertypes.Message) []gateway.SessionMessage {
	if len(messages) == 0 {
		return nil
	}

	converted := make([]gateway.SessionMessage, 0, len(messages))
	for _, message := range messages {
		convertedMessage := gateway.SessionMessage{
			Role:       strings.TrimSpace(message.Role),
			Content:    renderSessionMessageContent(message.Parts),
			ToolCallID: strings.TrimSpace(message.ToolCallID),
			IsError:    message.IsError,
		}
		if len(message.ToolCalls) > 0 {
			convertedMessage.ToolCalls = make([]gateway.ToolCall, 0, len(message.ToolCalls))
			for _, call := range message.ToolCalls {
				convertedMessage.ToolCalls = append(convertedMessage.ToolCalls, gateway.ToolCall{
					ID:        strings.TrimSpace(call.ID),
					Name:      strings.TrimSpace(call.Name),
					Arguments: call.Arguments,
				})
			}
		}
		converted = append(converted, convertedMessage)
	}
	return converted
}

// convertRuntimeSessionToGatewaySession 将 runtime 会话结构映射为 gateway 契约返回值。
func convertRuntimeSessionToGatewaySession(session agentsession.Session) gateway.Session {
	return gateway.Session{
		ID:        strings.TrimSpace(session.ID),
		Title:     strings.TrimSpace(session.Title),
		CreatedAt: session.CreatedAt,
		UpdatedAt: session.UpdatedAt,
		Workdir:   strings.TrimSpace(session.Workdir),
		Messages:  convertSessionMessages(session.Messages),
	}
}

// convertRuntimeSkillSource 将 runtime 技能来源映射为 gateway 输出结构。
func convertRuntimeSkillSource(source skills.Source) gateway.SkillSource {
	return gateway.SkillSource{
		Kind:     strings.TrimSpace(string(source.Kind)),
		Layer:    strings.TrimSpace(string(source.Layer)),
		RootDir:  strings.TrimSpace(source.RootDir),
		SkillDir: strings.TrimSpace(source.SkillDir),
		FilePath: strings.TrimSpace(source.FilePath),
	}
}

// convertRuntimeSkillDescriptor 将 runtime 技能描述映射为 gateway 输出结构。
func convertRuntimeSkillDescriptor(descriptor skills.Descriptor) gateway.SkillDescriptor {
	return gateway.SkillDescriptor{
		ID:          strings.TrimSpace(descriptor.ID),
		Name:        strings.TrimSpace(descriptor.Name),
		Description: strings.TrimSpace(descriptor.Description),
		Version:     strings.TrimSpace(descriptor.Version),
		Source:      convertRuntimeSkillSource(descriptor.Source),
		Scope:       strings.TrimSpace(string(descriptor.Scope)),
	}
}

// convertRuntimeSessionSkillStates 将 runtime 会话技能状态映射为 gateway 契约结构。
func convertRuntimeSessionSkillStates(states []agentruntime.SessionSkillState) []gateway.SessionSkillState {
	if len(states) == 0 {
		return nil
	}
	converted := make([]gateway.SessionSkillState, 0, len(states))
	for _, state := range states {
		item := gateway.SessionSkillState{
			SkillID: strings.TrimSpace(state.SkillID),
			Missing: state.Missing,
		}
		if state.Descriptor != nil {
			descriptor := convertRuntimeSkillDescriptor(*state.Descriptor)
			item.Descriptor = &descriptor
		}
		converted = append(converted, item)
	}
	return converted
}

// convertRuntimeAvailableSkillStates 将 runtime 可用技能状态映射为 gateway 契约结构。
func convertRuntimeAvailableSkillStates(states []agentruntime.AvailableSkillState) []gateway.AvailableSkillState {
	if len(states) == 0 {
		return nil
	}
	converted := make([]gateway.AvailableSkillState, 0, len(states))
	for _, state := range states {
		converted = append(converted, gateway.AvailableSkillState{
			Descriptor: convertRuntimeSkillDescriptor(state.Descriptor),
			Active:     state.Active,
		})
	}
	return converted
}

// renderSessionMessageContent 将 provider 多段内容渲染为对外展示的单段文本摘要。
func renderSessionMessageContent(parts []providertypes.ContentPart) string {
	if len(parts) == 0 {
		return ""
	}

	segments := make([]string, 0, len(parts))
	for _, part := range parts {
		switch part.Kind {
		case providertypes.ContentPartText:
			if text := strings.TrimSpace(part.Text); text != "" {
				segments = append(segments, text)
			}
		case providertypes.ContentPartImage:
			segments = append(segments, "[image]")
		}
	}
	return strings.Join(segments, "\n")
}

// ensureBridgeSubjectAllowed 在本地单用户 MVP 中执行最小主体校验。
func ensureBridgeSubjectAllowed(subjectID string) error {
	if strings.TrimSpace(subjectID) != bridgeLocalSubjectID {
		return gateway.ErrRuntimeAccessDenied
	}
	return nil
}

// ensureRuntimeAvailable 校验桥接器内部 runtime 是否可用。
func (b *gatewayRuntimePortBridge) ensureRuntimeAvailable() error {
	if b == nil || b.runtime == nil {
		return fmt.Errorf(bridgeRuntimeUnavailableErrMsg)
	}
	return nil
}

// ensureRuntimeAccess 组合校验 runtime 可用性与请求主体权限。
func (b *gatewayRuntimePortBridge) ensureRuntimeAccess(subjectID string) error {
	if err := b.ensureRuntimeAvailable(); err != nil {
		return err
	}
	return ensureBridgeSubjectAllowed(subjectID)
}

// isRuntimeNotFoundError 判断运行时错误是否属于目标不存在场景。
func isRuntimeNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, agentsession.ErrSessionNotFound) || errors.Is(err, os.ErrNotExist)
}

var _ gateway.RuntimePort = (*gatewayRuntimePortBridge)(nil)
