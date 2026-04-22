package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"neo-code/internal/gateway"
	"neo-code/internal/gateway/protocol"
	providertypes "neo-code/internal/provider/types"
	agentruntime "neo-code/internal/runtime"
	agentsession "neo-code/internal/session"
	"neo-code/internal/tools"
)

const (
	unsupportedActionInGatewayMode = "unsupported_action_in_gateway_mode"
	defaultRemoteRuntimeTimeout    = 8 * time.Second
)

var (
	newGatewayRPCClientFactory    = NewGatewayRPCClient
	newGatewayStreamClientFactory = NewGatewayStreamClient
	// ErrUnsupportedActionInGatewayMode 标记 gateway runtime 当前不支持的本地动作。
	ErrUnsupportedActionInGatewayMode = errors.New(unsupportedActionInGatewayMode)
)

// RemoteRuntimeAdapterOptions 描述远程 Runtime 适配器的初始化参数。
type RemoteRuntimeAdapterOptions struct {
	ListenAddress  string
	TokenFile      string
	RequestTimeout time.Duration
	RetryCount     int
}

type remoteGatewayRPCClient interface {
	Authenticate(ctx context.Context) error
	CallWithOptions(ctx context.Context, method string, params any, result any, options GatewayRPCCallOptions) error
	Notifications() <-chan gatewayRPCNotification
	Close() error
}

type remoteGatewayStreamClient interface {
	Events() <-chan agentruntime.RuntimeEvent
	Close() error
}

// RemoteRuntimeAdapter 将 TUI runtime 调用转发到 Gateway JSON-RPC 控制面。
type RemoteRuntimeAdapter struct {
	rpcClient    remoteGatewayRPCClient
	streamClient remoteGatewayStreamClient
	timeout      time.Duration
	retryCount   int

	closeOnce sync.Once
	closeCh   chan struct{}
	done      chan struct{}
	events    chan agentruntime.RuntimeEvent

	activeMu      sync.Mutex
	activeRunID   string
	activeSession string
}

// NewRemoteRuntimeAdapter 创建远程 Runtime 适配器，并在启动阶段执行 fail-fast 认证连通性检查。
func NewRemoteRuntimeAdapter(options RemoteRuntimeAdapterOptions) (*RemoteRuntimeAdapter, error) {
	timeout := options.RequestTimeout
	if timeout <= 0 {
		timeout = defaultRemoteRuntimeTimeout
	}
	retryCount := normalizeRemoteRuntimeRetryCount(options.RetryCount)

	rpcClient, err := newGatewayRPCClientFactory(GatewayRPCClientOptions{
		ListenAddress:  strings.TrimSpace(options.ListenAddress),
		TokenFile:      strings.TrimSpace(options.TokenFile),
		RequestTimeout: timeout,
		RetryCount:     retryCount,
	})
	if err != nil {
		return nil, err
	}

	streamClient := newGatewayStreamClientFactory(rpcClient.Notifications())
	adapter := newRemoteRuntimeAdapterWithClients(rpcClient, streamClient, timeout, retryCount)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := adapter.authenticate(ctx); err != nil {
		_ = adapter.Close()
		return nil, err
	}
	return adapter, nil
}

func newRemoteRuntimeAdapterWithClients(
	rpcClient remoteGatewayRPCClient,
	streamClient remoteGatewayStreamClient,
	timeout time.Duration,
	retryCount int,
) *RemoteRuntimeAdapter {
	retryCount = normalizeRemoteRuntimeRetryCount(retryCount)
	adapter := &RemoteRuntimeAdapter{
		rpcClient:    rpcClient,
		streamClient: streamClient,
		timeout:      timeout,
		retryCount:   retryCount,
		closeCh:      make(chan struct{}),
		done:         make(chan struct{}),
		events:       make(chan agentruntime.RuntimeEvent, 128),
	}
	go adapter.forwardEvents()
	return adapter
}

// Submit 将用户输入提交到网关：先 authenticate，再 bindStream，随后 loadSession，最后 run。
func (r *RemoteRuntimeAdapter) Submit(ctx context.Context, input agentruntime.PrepareInput) error {
	sessionID := strings.TrimSpace(input.SessionID)
	if sessionID == "" {
		sessionID = agentsession.NewID("session")
	}
	runID := strings.TrimSpace(input.RunID)
	if runID == "" {
		runID = fmt.Sprintf("run-%d", time.Now().UnixNano())
	}

	if err := r.authenticate(ctx); err != nil {
		return err
	}
	if err := r.bindStream(ctx, sessionID, runID); err != nil {
		return err
	}
	if err := r.preloadSession(ctx, sessionID); err != nil {
		return err
	}

	params := buildGatewayRunParams(sessionID, runID, input)
	frame, err := r.callFrame(ctx, protocol.MethodGatewayRun, params, GatewayRPCCallOptions{
		Timeout: r.timeout,
		Retries: 0,
	})
	if err != nil {
		return err
	}

	ackRunID := strings.TrimSpace(frame.RunID)
	if ackRunID == "" {
		ackRunID = runID
	}
	r.setActiveRun(ackRunID, sessionID)
	return nil
}

// PrepareUserInput 在 gateway 模式下提供最小可用输入归一化结果，保持接口兼容。
func (r *RemoteRuntimeAdapter) PrepareUserInput(ctx context.Context, input agentruntime.PrepareInput) (agentruntime.UserInput, error) {
	if err := ctx.Err(); err != nil {
		return agentruntime.UserInput{}, err
	}

	sessionID := strings.TrimSpace(input.SessionID)
	if sessionID == "" {
		sessionID = agentsession.NewID("session")
	}
	runID := strings.TrimSpace(input.RunID)
	if runID == "" {
		runID = fmt.Sprintf("run-%d", time.Now().UnixNano())
	}

	parts := make([]providertypes.ContentPart, 0, 1+len(input.Images))
	if strings.TrimSpace(input.Text) != "" {
		parts = append(parts, providertypes.NewTextPart(input.Text))
	}
	for _, image := range input.Images {
		path := strings.TrimSpace(image.Path)
		if path == "" {
			continue
		}
		parts = append(parts, providertypes.NewRemoteImagePart(path))
	}

	return agentruntime.UserInput{
		SessionID: sessionID,
		RunID:     runID,
		Parts:     parts,
		Workdir:   strings.TrimSpace(input.Workdir),
	}, nil
}

// Run 保持 runtime 接口兼容，在 gateway 模式下回落到 Submit 通道。
func (r *RemoteRuntimeAdapter) Run(ctx context.Context, input agentruntime.UserInput) error {
	prepareInput := agentruntime.PrepareInput{
		SessionID: strings.TrimSpace(input.SessionID),
		RunID:     strings.TrimSpace(input.RunID),
		Workdir:   strings.TrimSpace(input.Workdir),
		Text:      renderInputTextFromParts(input.Parts),
		Images:    renderInputImagesFromParts(input.Parts),
	}
	return r.Submit(ctx, prepareInput)
}

// Compact 转发 gateway.compact 请求并映射回 runtime CompactResult。
func (r *RemoteRuntimeAdapter) Compact(ctx context.Context, input agentruntime.CompactInput) (agentruntime.CompactResult, error) {
	sessionID := strings.TrimSpace(input.SessionID)
	if sessionID == "" {
		return agentruntime.CompactResult{}, errors.New("gateway runtime adapter: compact session_id is empty")
	}
	if err := r.authenticate(ctx); err != nil {
		return agentruntime.CompactResult{}, err
	}
	if err := r.bindStream(ctx, sessionID, strings.TrimSpace(input.RunID)); err != nil {
		return agentruntime.CompactResult{}, err
	}

	frame, err := r.callFrame(ctx, protocol.MethodGatewayCompact, protocol.CompactParams{
		SessionID: sessionID,
		RunID:     strings.TrimSpace(input.RunID),
	}, GatewayRPCCallOptions{
		Timeout: r.timeout,
		Retries: r.retryCount,
	})
	if err != nil {
		return agentruntime.CompactResult{}, err
	}

	gatewayResult, err := decodeFramePayload[gateway.CompactResult](frame.Payload)
	if err != nil {
		return agentruntime.CompactResult{}, err
	}
	return agentruntime.CompactResult{
		Applied:        gatewayResult.Applied,
		BeforeChars:    gatewayResult.BeforeChars,
		AfterChars:     gatewayResult.AfterChars,
		SavedRatio:     gatewayResult.SavedRatio,
		TriggerMode:    gatewayResult.TriggerMode,
		TranscriptID:   gatewayResult.TranscriptID,
		TranscriptPath: gatewayResult.TranscriptPath,
	}, nil
}

// ExecuteSystemTool 在 gateway 模式下显式不支持，避免任何本地 fallback。
func (r *RemoteRuntimeAdapter) ExecuteSystemTool(context.Context, agentruntime.SystemToolInput) (tools.ToolResult, error) {
	return tools.ToolResult{}, unsupportedGatewayActionError()
}

// ResolvePermission 转发 gateway.resolvePermission 请求。
func (r *RemoteRuntimeAdapter) ResolvePermission(ctx context.Context, input agentruntime.PermissionResolutionInput) error {
	if err := r.authenticate(ctx); err != nil {
		return err
	}
	_, err := r.callFrame(ctx, protocol.MethodGatewayResolvePermission, protocol.ResolvePermissionParams{
		RequestID: strings.TrimSpace(input.RequestID),
		Decision:  strings.ToLower(strings.TrimSpace(string(input.Decision))),
	}, GatewayRPCCallOptions{
		Timeout: r.timeout,
		Retries: r.retryCount,
	})
	return err
}

// preloadSession 在 run 之前触发一次 gateway.loadSession，用于会话建档/预热。
func (r *RemoteRuntimeAdapter) preloadSession(ctx context.Context, sessionID string) error {
	_, err := r.callFrame(ctx, protocol.MethodGatewayLoadSession, protocol.LoadSessionParams{
		SessionID: strings.TrimSpace(sessionID),
	}, GatewayRPCCallOptions{
		Timeout: r.timeout,
		Retries: r.retryCount,
	})
	return err
}

// CancelActiveRun 尝试取消当前活跃 run，并返回是否成功发起取消请求。
func (r *RemoteRuntimeAdapter) CancelActiveRun() bool {
	runID, sessionID := r.activeRun()
	if runID == "" {
		return false
	}

	go func(runID string, sessionID string) {
		ctx, cancel := context.WithTimeout(context.Background(), r.timeout)
		defer cancel()
		if err := r.authenticate(ctx); err != nil {
			return
		}
		_, _ = r.callFrame(ctx, protocol.MethodGatewayCancel, protocol.CancelParams{
			SessionID: sessionID,
			RunID:     runID,
		}, GatewayRPCCallOptions{
			Timeout: r.timeout,
			Retries: 0,
		})
	}(runID, sessionID)
	return true
}

// Events 返回适配后的 runtime 事件流。
func (r *RemoteRuntimeAdapter) Events() <-chan agentruntime.RuntimeEvent {
	return r.events
}

// ListSessions 转发 gateway.listSessions，并映射为 runtime 层会话摘要。
func (r *RemoteRuntimeAdapter) ListSessions(ctx context.Context) ([]agentsession.Summary, error) {
	if err := r.authenticate(ctx); err != nil {
		return nil, err
	}
	frame, err := r.callFrame(ctx, protocol.MethodGatewayListSessions, nil, GatewayRPCCallOptions{
		Timeout: r.timeout,
		Retries: r.retryCount,
	})
	if err != nil {
		return nil, err
	}

	payload := struct {
		Sessions []gateway.SessionSummary `json:"sessions"`
	}{}
	if err := decodeIntoValue(frame.Payload, &payload); err != nil {
		return nil, err
	}

	summaries := make([]agentsession.Summary, 0, len(payload.Sessions))
	for _, item := range payload.Sessions {
		summaries = append(summaries, agentsession.Summary{
			ID:        strings.TrimSpace(item.ID),
			Title:     strings.TrimSpace(item.Title),
			CreatedAt: item.CreatedAt,
			UpdatedAt: item.UpdatedAt,
		})
	}
	return summaries, nil
}

// LoadSession 转发 gateway.loadSession，并执行最小可用语义映射。
func (r *RemoteRuntimeAdapter) LoadSession(ctx context.Context, id string) (agentsession.Session, error) {
	sessionID := strings.TrimSpace(id)
	if sessionID == "" {
		return agentsession.Session{}, errors.New("gateway runtime adapter: session id is empty")
	}
	if err := r.authenticate(ctx); err != nil {
		return agentsession.Session{}, err
	}
	frame, err := r.callFrame(ctx, protocol.MethodGatewayLoadSession, protocol.LoadSessionParams{
		SessionID: sessionID,
	}, GatewayRPCCallOptions{
		Timeout: r.timeout,
		Retries: r.retryCount,
	})
	if err != nil {
		return agentsession.Session{}, err
	}

	loaded, err := decodeFramePayload[gateway.Session](frame.Payload)
	if err != nil {
		return agentsession.Session{}, err
	}
	return mapGatewaySessionToRuntimeSession(loaded), nil
}

// ActivateSessionSkill 在 gateway 模式下显式不支持。
func (r *RemoteRuntimeAdapter) ActivateSessionSkill(context.Context, string, string) error {
	return unsupportedGatewayActionError()
}

// DeactivateSessionSkill 在 gateway 模式下显式不支持。
func (r *RemoteRuntimeAdapter) DeactivateSessionSkill(context.Context, string, string) error {
	return unsupportedGatewayActionError()
}

// ListSessionSkills 在 gateway 模式下显式不支持。
func (r *RemoteRuntimeAdapter) ListSessionSkills(context.Context, string) ([]agentruntime.SessionSkillState, error) {
	return nil, unsupportedGatewayActionError()
}

// ListAvailableSkills 在 gateway 模式下显式不支持。
func (r *RemoteRuntimeAdapter) ListAvailableSkills(
	context.Context,
	string,
) ([]agentruntime.AvailableSkillState, error) {
	return nil, unsupportedGatewayActionError()
}

// Close 关闭远程适配器并结束事件桥接。
func (r *RemoteRuntimeAdapter) Close() error {
	var closeErr error
	r.closeOnce.Do(func() {
		close(r.closeCh)
		if r.streamClient != nil {
			closeErr = errors.Join(closeErr, r.streamClient.Close())
		}
		if r.rpcClient != nil {
			closeErr = errors.Join(closeErr, r.rpcClient.Close())
		}
		<-r.done
	})
	return closeErr
}

func (r *RemoteRuntimeAdapter) authenticate(ctx context.Context) error {
	if r.rpcClient == nil {
		return errors.New("gateway runtime adapter: rpc client is nil")
	}
	return r.rpcClient.Authenticate(ctx)
}

func (r *RemoteRuntimeAdapter) bindStream(ctx context.Context, sessionID string, runID string) error {
	_, err := r.callFrame(ctx, protocol.MethodGatewayBindStream, protocol.BindStreamParams{
		SessionID: strings.TrimSpace(sessionID),
		RunID:     strings.TrimSpace(runID),
		Channel:   "all",
	}, GatewayRPCCallOptions{
		Timeout: r.timeout,
		Retries: r.retryCount,
	})
	return err
}

func (r *RemoteRuntimeAdapter) callFrame(
	ctx context.Context,
	method string,
	params any,
	options GatewayRPCCallOptions,
) (gateway.MessageFrame, error) {
	if r.rpcClient == nil {
		return gateway.MessageFrame{}, errors.New("gateway runtime adapter: rpc client is nil")
	}

	var frame gateway.MessageFrame
	if err := r.rpcClient.CallWithOptions(ctx, method, params, &frame, options); err != nil {
		return gateway.MessageFrame{}, err
	}
	if frame.Type == gateway.FrameTypeError {
		if frame.Error == nil {
			return gateway.MessageFrame{}, fmt.Errorf("gateway %s returned error frame without payload", method)
		}
		return gateway.MessageFrame{}, fmt.Errorf("%s: %s", strings.TrimSpace(frame.Error.Code), strings.TrimSpace(frame.Error.Message))
	}
	if frame.Type != gateway.FrameTypeAck {
		return gateway.MessageFrame{}, fmt.Errorf("gateway %s returned unexpected frame type %q", method, frame.Type)
	}
	return frame, nil
}

func (r *RemoteRuntimeAdapter) forwardEvents() {
	defer close(r.done)
	defer close(r.events)

	if r.streamClient == nil {
		return
	}

	source := r.streamClient.Events()
	for {
		select {
		case <-r.closeCh:
			return
		case event, ok := <-source:
			if !ok {
				return
			}
			r.observeEvent(event)
			select {
			case <-r.closeCh:
				return
			case r.events <- event:
			}
		}
	}
}

func (r *RemoteRuntimeAdapter) observeEvent(event agentruntime.RuntimeEvent) {
	runID := strings.TrimSpace(event.RunID)
	sessionID := strings.TrimSpace(event.SessionID)
	if runID != "" || sessionID != "" {
		r.setActiveRun(runID, sessionID)
	}

	switch event.Type {
	case agentruntime.EventAgentDone, agentruntime.EventError, agentruntime.EventRunCanceled, agentruntime.EventStopReasonDecided:
		r.clearActiveRun(runID)
	}
}

func (r *RemoteRuntimeAdapter) setActiveRun(runID string, sessionID string) {
	r.activeMu.Lock()
	defer r.activeMu.Unlock()
	normalizedRunID := strings.TrimSpace(runID)
	normalizedSessionID := strings.TrimSpace(sessionID)
	if normalizedRunID != "" {
		r.activeRunID = normalizedRunID
	}
	if normalizedSessionID != "" {
		r.activeSession = normalizedSessionID
	}
}

func (r *RemoteRuntimeAdapter) clearActiveRun(runID string) {
	r.activeMu.Lock()
	defer r.activeMu.Unlock()
	normalizedRunID := strings.TrimSpace(runID)
	if normalizedRunID == "" {
		return
	}
	if strings.EqualFold(normalizedRunID, strings.TrimSpace(r.activeRunID)) {
		r.activeRunID = ""
	}
}

// normalizeRemoteRuntimeRetryCount 统一归一化重试次数，避免零值关闭默认重试兜底。
func normalizeRemoteRuntimeRetryCount(retryCount int) int {
	if retryCount <= 0 {
		return defaultGatewayRPCRetryCount
	}
	return retryCount
}

func (r *RemoteRuntimeAdapter) activeRun() (string, string) {
	r.activeMu.Lock()
	defer r.activeMu.Unlock()
	return strings.TrimSpace(r.activeRunID), strings.TrimSpace(r.activeSession)
}

// unsupportedGatewayActionError 返回 gateway 模式下不支持本地动作时的统一错误。
func unsupportedGatewayActionError() error {
	return ErrUnsupportedActionInGatewayMode
}

func buildGatewayRunParams(sessionID string, runID string, input agentruntime.PrepareInput) protocol.RunParams {
	parts := make([]protocol.RunInputPart, 0, len(input.Images))
	for _, image := range input.Images {
		path := strings.TrimSpace(image.Path)
		if path == "" {
			continue
		}
		parts = append(parts, protocol.RunInputPart{
			Type: string(gateway.InputPartTypeImage),
			Media: &protocol.RunInputMedia{
				URI:      path,
				MimeType: strings.TrimSpace(image.MimeType),
			},
		})
	}

	return protocol.RunParams{
		SessionID:  strings.TrimSpace(sessionID),
		RunID:      strings.TrimSpace(runID),
		InputText:  strings.TrimSpace(input.Text),
		InputParts: parts,
		Workdir:    strings.TrimSpace(input.Workdir),
	}
}

func renderInputTextFromParts(parts []providertypes.ContentPart) string {
	textParts := make([]string, 0, len(parts))
	for _, part := range parts {
		if part.Kind != providertypes.ContentPartText {
			continue
		}
		text := strings.TrimSpace(part.Text)
		if text == "" {
			continue
		}
		textParts = append(textParts, text)
	}
	return strings.Join(textParts, "\n")
}

func renderInputImagesFromParts(parts []providertypes.ContentPart) []agentruntime.UserImageInput {
	images := make([]agentruntime.UserImageInput, 0, len(parts))
	for _, part := range parts {
		if part.Kind != providertypes.ContentPartImage || part.Image == nil {
			continue
		}
		path := strings.TrimSpace(part.Image.URL)
		if path == "" {
			continue
		}
		mimeType := ""
		if part.Image.Asset != nil {
			mimeType = strings.TrimSpace(part.Image.Asset.MimeType)
		}
		images = append(images, agentruntime.UserImageInput{
			Path:     path,
			MimeType: mimeType,
		})
	}
	return images
}

func mapGatewaySessionToRuntimeSession(source gateway.Session) agentsession.Session {
	messages := make([]providertypes.Message, 0, len(source.Messages))
	for _, item := range source.Messages {
		content := strings.TrimSpace(item.Content)
		message := providertypes.Message{
			Role:       strings.TrimSpace(item.Role),
			ToolCallID: strings.TrimSpace(item.ToolCallID),
			IsError:    item.IsError,
		}
		if content != "" {
			message.Parts = []providertypes.ContentPart{providertypes.NewTextPart(content)}
		}
		if len(item.ToolCalls) > 0 {
			message.ToolCalls = make([]providertypes.ToolCall, 0, len(item.ToolCalls))
			for _, call := range item.ToolCalls {
				message.ToolCalls = append(message.ToolCalls, providertypes.ToolCall{
					ID:        strings.TrimSpace(call.ID),
					Name:      strings.TrimSpace(call.Name),
					Arguments: call.Arguments,
				})
			}
		}
		messages = append(messages, message)
	}

	return agentsession.Session{
		ID:        strings.TrimSpace(source.ID),
		Title:     strings.TrimSpace(source.Title),
		CreatedAt: source.CreatedAt,
		UpdatedAt: source.UpdatedAt,
		Workdir:   strings.TrimSpace(source.Workdir),
		Messages:  messages,
	}
}

func decodeFramePayload[T any](payload any) (T, error) {
	var out T
	if err := decodeIntoValue(payload, &out); err != nil {
		return out, err
	}
	return out, nil
}

func decodeIntoValue(payload any, target any) error {
	if target == nil {
		return errors.New("decode payload target is nil")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode frame payload: %w", err)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("decode frame payload: %w", err)
	}
	return nil
}

var _ agentruntime.Runtime = (*RemoteRuntimeAdapter)(nil)
