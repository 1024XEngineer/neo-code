package ptyproxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"

	"github.com/charmbracelet/glamour"

	"neo-code/internal/gateway"
	gatewayclient "neo-code/internal/gateway/client"
	"neo-code/internal/gateway/protocol"
)

const (
	idmSystemColor                = "\033[96m"
	idmAIColor                    = "\033[93m"
	idmColorReset                 = "\033[0m"
	idmPromptText                 = "IDM> "
	idmExpandedRingBufferCapacity = 256 * 1024
	idmSessionPrefix              = "idm-"
	idmSessionRunAskPrefix        = "idm-ask"
	idmMarkdownWrapWidth          = 96
	idmPlanMode                   = "plan"
)

var idmRunSequence atomic.Uint64
var (
	idmMarkdownRendererOnce sync.Once
	idmMarkdownRenderer     *glamour.TermRenderer
	idmMarkdownRendererErr  error
)

const idmNativeCommandLineEnding = "\r\n"

// idmRuntimeMode 璐熻矗 idmRuntimeMode 鐩稿叧閫昏緫銆
type idmRuntimeMode int

const (
	idmModeIdle idmRuntimeMode = iota
	idmModeStreaming
	idmModeNativeCmd
)

// idmControllerOptions 璐熻矗 idmControllerOptions 鐩稿叧閫昏緫銆
type idmControllerOptions struct {
	PTYWriter          io.Writer
	Output             io.Writer
	Stderr             io.Writer
	RPCClient          *gatewayclient.GatewayRPCClient
	NotificationStream <-chan gatewayclient.Notification
	AutoState          *autoRuntimeState
	LogBuffer          *UTF8RingBuffer
	DefaultCap         int
	Workdir            string
	ShellSessionID     string
}

// idmController 璐熻矗 idmController 鐩稿叧閫昏緫銆
type idmController struct {
	ptyWriter          io.Writer
	output             io.Writer
	stderr             io.Writer
	rpcClient          *gatewayclient.GatewayRPCClient
	notificationStream <-chan gatewayclient.Notification
	autoState          *autoRuntimeState
	logBuffer          *UTF8RingBuffer
	workdir            string
	shellSessionID     string

	mu                  sync.Mutex
	active              bool
	mode                idmRuntimeMode
	autoSnapshot        bool
	defaultRingCapacity int
	lineBuffer          []byte
	utf8Pending         []byte
	pendingEcho         []byte
	sessionID           string
	sessionReady        bool
	currentRunID        string
	streamCancel        context.CancelFunc
	abandonedRunIDs     map[string]struct{}
}

// newIDMController 璐熻矗 newIDMController 鐩稿叧閫昏緫銆
func newIDMController(options idmControllerOptions) *idmController {
	defaultCap := options.DefaultCap
	if defaultCap <= 0 {
		defaultCap = DefaultRingBufferCapacity
	}
	return &idmController{
		ptyWriter:           options.PTYWriter,
		output:              options.Output,
		stderr:              options.Stderr,
		rpcClient:           options.RPCClient,
		notificationStream:  options.NotificationStream,
		autoState:           options.AutoState,
		logBuffer:           options.LogBuffer,
		workdir:             strings.TrimSpace(options.Workdir),
		shellSessionID:      strings.TrimSpace(options.ShellSessionID),
		defaultRingCapacity: defaultCap,
		lineBuffer:          make([]byte, 0, 128),
		utf8Pending:         make([]byte, 0, utf8.UTFMax),
		pendingEcho:         make([]byte, 0, 128),
		abandonedRunIDs:     make(map[string]struct{}),
	}
}

// Enter 璐熻矗 Enter 鐩稿叧閫昏緫銆
func (c *idmController) Enter() error {
	if c == nil {
		return errors.New("idm controller is nil")
	}

	c.mu.Lock()
	if c.active {
		c.mu.Unlock()
		return nil
	}
	if c.autoState != nil && !c.autoState.OSCReady.Load() && c.ptyWriter == nil {
		c.mu.Unlock()
		return errors.New("shell integration is not ready (OSC133 unavailable)")
	}

	c.active = true
	c.mode = idmModeIdle
	c.lineBuffer = c.lineBuffer[:0]
	c.utf8Pending = c.utf8Pending[:0]
	c.pendingEcho = c.pendingEcho[:0]
	c.currentRunID = ""
	c.streamCancel = nil
	c.sessionID = generateIDMSessionID(os.Getpid())
	c.sessionReady = true
	if c.autoState != nil {
		c.autoSnapshot = c.autoState.Enabled.Load()
		c.autoState.Enabled.Store(false)
	}
	if c.logBuffer != nil {
		if currentCap := c.logBuffer.Capacity(); currentCap > 0 {
			c.defaultRingCapacity = currentCap
		}
		c.logBuffer.Resize(idmExpandedRingBufferCapacity)
	}
	c.mu.Unlock()

	c.writeSystemMessage("[NeoCode] IDM mode enabled. Use `@ai <question>` for follow-up chat; other input passes to the shell.")
	c.writeSystemMessage("[NeoCode] Type `exit` or press Ctrl+C in idle mode to leave.")
	c.writePrompt()
	return nil
}

// rollbackEnter 璐熻矗 rollbackEnter 鐩稿叧閫昏緫銆
func (c *idmController) rollbackEnter(sessionID string) {
	if c == nil {
		return
	}

	var (
		autoSnapshot  bool
		shouldRestore bool
		defaultCap    int
	)
	c.mu.Lock()
	autoSnapshot = c.autoSnapshot
	shouldRestore = c.logBuffer != nil
	defaultCap = c.defaultRingCapacity

	c.active = false
	c.mode = idmModeIdle
	c.currentRunID = ""
	c.streamCancel = nil
	c.sessionID = ""
	c.sessionReady = false
	c.lineBuffer = c.lineBuffer[:0]
	c.utf8Pending = c.utf8Pending[:0]
	c.pendingEcho = c.pendingEcho[:0]
	c.mu.Unlock()

	if strings.TrimSpace(sessionID) != "" {
		c.deleteAskSession(sessionID)
	}
	if shouldRestore {
		if defaultCap <= 0 {
			defaultCap = DefaultRingBufferCapacity
		}
		c.logBuffer.Resize(defaultCap)
		c.logBuffer.Reset()
	}
	if c.autoState != nil {
		c.autoState.Enabled.Store(autoSnapshot)
	}
}

// Exit 璐熻矗 Exit 鐩稿叧閫昏緫銆
func (c *idmController) Exit() {
	if c == nil {
		return
	}

	var (
		cancelFunc     context.CancelFunc
		runID          string
		sessionID      string
		autoSnapshot   bool
		shouldRestore  bool
		defaultBufSize int
	)

	c.mu.Lock()
	if !c.active {
		c.mu.Unlock()
		return
	}
	c.active = false
	c.mode = idmModeIdle
	cancelFunc = c.streamCancel
	runID = strings.TrimSpace(c.currentRunID)
	sessionID = strings.TrimSpace(c.sessionID)
	autoSnapshot = c.autoSnapshot
	shouldRestore = c.logBuffer != nil
	defaultBufSize = c.defaultRingCapacity

	c.currentRunID = ""
	c.streamCancel = nil
	c.sessionID = ""
	c.sessionReady = false
	c.lineBuffer = c.lineBuffer[:0]
	c.utf8Pending = c.utf8Pending[:0]
	c.pendingEcho = c.pendingEcho[:0]
	c.mu.Unlock()

	if cancelFunc != nil {
		cancelFunc()
	}
	if runID != "" {
		c.markAskRunAbandoned(runID)
	}
	if sessionID != "" {
		c.deleteAskSession(sessionID)
	}
	if shouldRestore {
		if defaultBufSize <= 0 {
			defaultBufSize = DefaultRingBufferCapacity
		}
		c.logBuffer.Resize(defaultBufSize)
		c.logBuffer.Reset()
	}
	if c.autoState != nil {
		c.autoState.Enabled.Store(autoSnapshot)
	}
	c.writeSystemMessage("[NeoCode] IDM mode exited, shell passthrough restored.")
}

// IsActive 璐熻矗 IsActive 鐩稿叧閫昏緫銆
func (c *idmController) IsActive() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.active
}

// ShouldPassthroughInput 璐熻矗 ShouldPassthroughInput 鐩稿叧閫昏緫銆
func (c *idmController) ShouldPassthroughInput() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.active && c.mode == idmModeNativeCmd
}

// HandleSignal 璐熻矗 HandleSignal 鐩稿叧閫昏緫銆
func (c *idmController) HandleSignal(signalValue os.Signal) bool {
	if c == nil {
		return false
	}
	if !isInterruptSignal(signalValue) {
		return false
	}

	var (
		mode       idmRuntimeMode
		cancelFunc context.CancelFunc
		runID      string
	)
	c.mu.Lock()
	if !c.active {
		c.mu.Unlock()
		return false
	}
	mode = c.mode
	if mode == idmModeStreaming {
		cancelFunc = c.streamCancel
		runID = strings.TrimSpace(c.currentRunID)
		c.mode = idmModeIdle
		c.streamCancel = nil
		c.currentRunID = ""
	}
	c.mu.Unlock()

	switch mode {
	case idmModeIdle:
		c.Exit()
		return true
	case idmModeStreaming:
		if cancelFunc != nil {
			cancelFunc()
		}
		if runID != "" {
			c.markAskRunAbandoned(runID)
		}
		c.writeSystemMessage("[NeoCode] Current @ai request canceled.")
		c.writePrompt()
		return true
	case idmModeNativeCmd:
		return false
	default:
		return true
	}
}

// HandleInputByte 璐熻矗 HandleInputByte 鐩稿叧閫昏緫銆
func (c *idmController) HandleInputByte(inputByte byte) {
	if c == nil {
		return
	}

	// Raw 模式下 Ctrl+C / Ctrl+Z 会作为字节输入，这里显式转换为 SIGINT 语义。
	switch inputByte {
	case 0x03, 0x1A:
		if c.HandleSignal(interruptSignal()) {
			return
		}
	}

	c.mu.Lock()
	if !c.active {
		c.mu.Unlock()
		return
	}
	mode := c.mode
	c.mu.Unlock()

	if mode == idmModeStreaming || mode == idmModeNativeCmd {
		return
	}

	switch inputByte {
	case 0x04:
		c.Exit()
		return
	case '\r', '\n':
		c.flushPendingUTF8()
		c.writeRawOutput([]byte("\r\n"))

		c.mu.Lock()
		line := strings.TrimSpace(string(c.lineBuffer))
		c.lineBuffer = c.lineBuffer[:0]
		c.mu.Unlock()

		c.handleInputLine(line)
		return
	case 0x7f, 0x08:
		c.handleBackspace()
		return
	default:
		c.handleUTF8Byte(inputByte)
	}
}

// FilterPTYOutput 璐熻矗 FilterPTYOutput 鐩稿叧閫昏緫銆
func (c *idmController) FilterPTYOutput(chunk []byte) []byte {
	if c == nil || len(chunk) == 0 {
		return chunk
	}
	c.mu.Lock()
	if !c.active {
		c.mu.Unlock()
		return chunk
	}
	if c.mode != idmModeNativeCmd {
		c.mu.Unlock()
		return nil
	}
	if len(c.pendingEcho) == 0 {
		c.mu.Unlock()
		return chunk
	}
	filtered := make([]byte, 0, len(chunk))
	for _, item := range chunk {
		if len(c.pendingEcho) > 0 {
			if item == c.pendingEcho[0] {
				c.pendingEcho = c.pendingEcho[1:]
				continue
			}
			c.pendingEcho = c.pendingEcho[:0]
		}
		filtered = append(filtered, item)
	}
	c.mu.Unlock()
	return filtered
}

// OnShellEvent 璐熻矗 OnShellEvent 鐩稿叧閫昏緫銆
func (c *idmController) OnShellEvent(event ShellEvent) {
	if c == nil {
		return
	}
	if event.Type != ShellEventCommandDone && event.Type != ShellEventPromptReady {
		return
	}
	c.mu.Lock()
	if !c.active || c.mode != idmModeNativeCmd {
		c.mu.Unlock()
		return
	}
	c.mode = idmModeIdle
	c.mu.Unlock()
	c.writePrompt()
}

// handleInputLine 璐熻矗 handleInputLine 鐩稿叧閫昏緫銆
func (c *idmController) handleInputLine(line string) {
	decision := routeIDMInput(line)
	switch decision.Kind {
	case idmRouteExit:
		c.Exit()
	case idmRouteAskAI:
		c.handleAskAIAsync(decision.Payload)
	case idmRoutePassThrough:
		if strings.TrimSpace(decision.Payload) == "" {
			c.writePrompt()
			return
		}
		if err := c.sendNativeCommand(decision.Payload); err != nil {
			c.writeFriendlyMessage(fmt.Sprintf("[NeoCode: 鍘熺敓鍛戒护閫忎紶澶辫触 (%v)]", err))
			c.writePrompt()
		}
	default:
		c.writePrompt()
	}
}

// handleAskAIAsync 浠ュ紓姝ユ柟寮忔墽琛?@ai锛岄伩鍏嶉樆濉炶緭鍏ュ惊鐜鑷?Ctrl+C 鏃犳硶鐢熸晥銆
func (c *idmController) handleAskAIAsync(question string) {
	if c == nil {
		return
	}
	go func() {
		if err := c.sendAIMessage(question); err != nil {
			c.writeFriendlyMessage(fmt.Sprintf("[NeoCode: @ai 璇锋眰澶辫触 (%v)]", err))
			c.writePrompt()
		}
	}()
}

// sendNativeCommand 璐熻矗 sendNativeCommand 鐩稿叧閫昏緫銆
func (c *idmController) sendNativeCommand(commandLine string) error {
	trimmed := strings.TrimSpace(commandLine)
	if trimmed == "" {
		return nil
	}
	c.mu.Lock()
	if !c.active {
		c.mu.Unlock()
		return errors.New("idm is not active")
	}
	c.mode = idmModeNativeCmd
	payload := commandLine + idmNativeCommandLineEnding
	c.pendingEcho = append(c.pendingEcho[:0], []byte(payload)...)
	c.mu.Unlock()

	if _, err := io.WriteString(c.ptyWriter, payload); err != nil {
		c.mu.Lock()
		if c.active {
			c.mode = idmModeIdle
		}
		c.pendingEcho = c.pendingEcho[:0]
		c.mu.Unlock()
		return err
	}
	return nil
}

// sendAIMessage 璐熻矗 sendAIMessage 鐩稿叧閫昏緫銆
func (c *idmController) sendAIMessage(question string) error {
	question = strings.TrimSpace(question)
	if question == "" {
		return errors.New("empty ai question")
	}
	if c == nil {
		return errors.New("idm controller is nil")
	}
	if c.rpcClient == nil {
		return errors.New("gateway rpc client is not ready")
	}
	sessionID := c.ensureAskSessionID()

	streamCtx, streamCancel := context.WithCancel(context.Background())
	c.mu.Lock()
	if !c.active {
		c.mu.Unlock()
		streamCancel()
		return errors.New("idm is not active")
	}
	c.mode = idmModeStreaming
	c.currentRunID = ""
	c.streamCancel = streamCancel
	c.mu.Unlock()

	finishStreaming := func(runID string) {
		streamCancel()
		c.finishStreaming(runID)
	}

	if err := c.bindIDMAskStream(streamCtx, sessionID); err != nil {
		finishStreaming("")
		return err
	}

	var askAck gateway.MessageFrame
	err := c.rpcClient.Ask(
		streamCtx,
		protocol.AskParams{
			SessionID: sessionID,
			UserQuery: question,
			Workdir:   c.workdir,
		},
		&askAck,
		gatewayclient.GatewayRPCCallOptions{
			Timeout: diagnoseCallTimeout,
			Retries: 0,
		},
	)
	if err != nil {
		finishStreaming("")
		return err
	}
	if askAck.Type == gateway.FrameTypeError && askAck.Error != nil {
		finishStreaming("")
		return fmt.Errorf("gateway ask failed (%s): %s", strings.TrimSpace(askAck.Error.Code), strings.TrimSpace(askAck.Error.Message))
	}
	if askAck.Type != gateway.FrameTypeAck {
		finishStreaming("")
		return fmt.Errorf("unexpected gateway frame type for ask: %s", askAck.Type)
	}

	runID, waitErr := c.waitAskStream(streamCtx, sessionID)
	finishStreaming(runID)
	if waitErr != nil && !errors.Is(waitErr, context.Canceled) {
		return waitErr
	}
	c.writePrompt()
	return nil
}

// resolveIDMRunMode 杩斿洖 IDM @ai 鏈杩愯搴旀敞鍏ョ殑 Runtime mode銆
func resolveIDMRunMode() string {
	if !IsIDMPlanModeEnabledFromEnv() {
		return ""
	}
	return idmPlanMode
}

// validateIDMAckFrame 鏍￠獙 IDM RPC 璋冪敤杩斿洖鐨?ACK 璇箟锛岄伩鍏嶅け璐ュ悗缁х画绛夊緟娴佷簨浠躲€
func validateIDMAckFrame(frame gateway.MessageFrame, operation string) error {
	operation = strings.TrimSpace(operation)
	if operation == "" {
		operation = "operation"
	}
	if frame.Type == gateway.FrameTypeError && frame.Error != nil {
		return fmt.Errorf(
			"gateway %s failed (%s): %s",
			operation,
			strings.TrimSpace(frame.Error.Code),
			strings.TrimSpace(frame.Error.Message),
		)
	}
	if frame.Type != gateway.FrameTypeAck {
		return fmt.Errorf("unexpected gateway frame type for %s: %s", operation, frame.Type)
	}
	return nil
}

// finishStreaming 结束当前流式状态，并把控制器切回空闲模式。
func (c *idmController) finishStreaming(runID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.active {
		return
	}
	if strings.TrimSpace(runID) != "" && strings.TrimSpace(c.currentRunID) != strings.TrimSpace(runID) {
		return
	}
	c.currentRunID = ""
	c.streamCancel = nil
	c.mode = idmModeIdle
}

// currentSessionID 返回当前 IDM 控制器维护的 Ask 会话标识。
func (c *idmController) currentSessionID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.TrimSpace(c.sessionID)
}

// ensureAskSessionID 确保控制器存在可复用的 Ask 会话标识。
func (c *idmController) ensureAskSessionID() string {
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if current := strings.TrimSpace(c.sessionID); current != "" {
		return current
	}
	c.sessionID = generateIDMSessionID(os.Getpid())
	c.sessionReady = true
	return c.sessionID
}

// bindIDMAskStream 为 IDM Ask 会话绑定事件流，接收 ask_chunk/ask_done/ask_error。
func (c *idmController) bindIDMAskStream(ctx context.Context, sessionID string) error {
	if c == nil || c.rpcClient == nil {
		return errors.New("gateway rpc client is not ready")
	}
	normalizedSessionID := strings.TrimSpace(sessionID)
	if normalizedSessionID == "" {
		return errors.New("idm ask session id is empty")
	}

	var bindAck gateway.MessageFrame
	if err := c.rpcClient.CallWithOptions(
		ctx,
		protocol.MethodGatewayBindStream,
		protocol.BindStreamParams{
			SessionID: normalizedSessionID,
			Channel:   "all",
			Role:      "shell",
		},
		&bindAck,
		gatewayclient.GatewayRPCCallOptions{
			Timeout: diagnoseCallTimeout,
			Retries: 0,
		},
	); err != nil {
		return err
	}
	if bindAck.Type == gateway.FrameTypeError && bindAck.Error != nil {
		return fmt.Errorf("gateway bind_stream failed (%s): %s", strings.TrimSpace(bindAck.Error.Code), strings.TrimSpace(bindAck.Error.Message))
	}
	if bindAck.Type != gateway.FrameTypeAck {
		return fmt.Errorf("unexpected gateway frame type for bind_stream: %s", bindAck.Type)
	}
	return nil
}

// waitAskStream 等待 Ask 事件流完成，并拼接流式输出文本。
func (c *idmController) waitAskStream(ctx context.Context, sessionID string) (string, error) {
	notifications := c.notificationStream
	if notifications == nil && c.rpcClient != nil {
		notifications = c.rpcClient.Notifications()
	}
	if notifications == nil {
		return "", errors.New("gateway notification stream is not ready")
	}

	targetSessionID := strings.TrimSpace(sessionID)
	var (
		runID         string
		markdown      strings.Builder
		streamedChunk bool
	)
	for {
		select {
		case <-ctx.Done():
			return runID, ctx.Err()
		case notification, ok := <-notifications:
			if !ok {
				return runID, errors.New("gateway notification channel closed")
			}
			if !strings.EqualFold(strings.TrimSpace(notification.Method), protocol.MethodGatewayEvent) {
				continue
			}
			var frame gateway.MessageFrame
			if err := json.Unmarshal(notification.Params, &frame); err != nil {
				continue
			}
			if targetSessionID != "" && !strings.EqualFold(strings.TrimSpace(frame.SessionID), targetSessionID) {
				continue
			}

			payloadMap, ok := normalizeIDMEventPayload(frame.Payload)
			if !ok {
				continue
			}
			eventType := strings.ToLower(strings.TrimSpace(readMapStringValue(payloadMap, "event_type")))
			if eventType != string(gateway.RuntimeEventTypeAskChunk) &&
				eventType != string(gateway.RuntimeEventTypeAskDone) &&
				eventType != string(gateway.RuntimeEventTypeAskError) {
				continue
			}

			frameRunID := strings.TrimSpace(frame.RunID)
			if c.shouldIgnoreAskRun(frameRunID, eventType) {
				continue
			}
			if runID == "" && frameRunID != "" {
				runID = frameRunID
				c.mu.Lock()
				if c.active && c.mode == idmModeStreaming {
					c.currentRunID = frameRunID
				}
				c.mu.Unlock()
			} else if runID != "" && frameRunID != "" && !strings.EqualFold(frameRunID, runID) {
				continue
			}

			eventPayload, _ := readMapAnyValue(payloadMap, "payload")
			switch eventType {
			case string(gateway.RuntimeEventTypeAskChunk):
				chunk := extractIDMAskText(eventPayload)
				if strings.TrimSpace(chunk) == "" {
					chunk = extractIDMAskText(payloadMap)
				}
				if chunk == "" {
					continue
				}
				markdown.WriteString(chunk)
				c.renderIDMStreamChunk(chunk)
				streamedChunk = true
			case string(gateway.RuntimeEventTypeAskDone):
				answer := markdown.String()
				if strings.TrimSpace(answer) == "" {
					answer = extractIDMAskText(eventPayload)
					if strings.TrimSpace(answer) == "" {
						answer = extractIDMAskText(payloadMap)
					}
				}
				if !streamedChunk {
					c.renderIDMAnswer(answer)
				}
				c.writeRawOutput([]byte("\r\n"))
				return runID, nil
			case string(gateway.RuntimeEventTypeAskError):
				message := extractIDMAskText(eventPayload)
				if strings.TrimSpace(message) == "" {
					message = extractIDMAskText(payloadMap)
				}
				if strings.TrimSpace(message) == "" {
					message = "ask failed"
				}
				return runID, errors.New(strings.TrimSpace(message))
			}
		}
	}
}

// currentSessionID 璐熻矗 currentSessionID 鐩稿叧閫昏緫銆
func (c *idmController) markAskRunAbandoned(runID string) {
	if c == nil {
		return
	}
	normalizedRunID := strings.TrimSpace(runID)
	if normalizedRunID == "" {
		return
	}
	c.mu.Lock()
	if c.abandonedRunIDs == nil {
		c.abandonedRunIDs = make(map[string]struct{})
	}
	c.abandonedRunIDs[normalizedRunID] = struct{}{}
	c.mu.Unlock()
}

// waitRunStream 璐熻矗 waitRunStream 鐩稿叧閫昏緫銆
func (c *idmController) shouldIgnoreAskRun(runID string, eventType string) bool {
	if c == nil {
		return false
	}
	normalizedRunID := strings.TrimSpace(runID)
	if normalizedRunID == "" {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.abandonedRunIDs) == 0 {
		return false
	}
	if _, exists := c.abandonedRunIDs[normalizedRunID]; !exists {
		return false
	}
	if eventType == string(gateway.RuntimeEventTypeAskDone) || eventType == string(gateway.RuntimeEventTypeAskError) {
		delete(c.abandonedRunIDs, normalizedRunID)
	}
	return true
}

// cancelRun 璐熻矗 cancelRun 鐩稿叧閫昏緫銆
func normalizeIDMEventPayload(payload any) (map[string]any, bool) {
	switch typed := payload.(type) {
	case map[string]any:
		return typed, true
	case nil:
		return nil, false
	default:
		raw, err := json.Marshal(typed)
		if err != nil {
			return nil, false
		}
		decoded := map[string]any{}
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return nil, false
		}
		return decoded, true
	}
}

// deleteSession 璐熻矗 deleteSession 鐩稿叧閫昏緫銆
func (c *idmController) deleteAskSession(sessionID string) {
	if c == nil || c.rpcClient == nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	var ack gateway.MessageFrame
	_ = c.rpcClient.DeleteAskSession(
		context.Background(),
		protocol.DeleteAskSessionParams{SessionID: strings.TrimSpace(sessionID)},
		&ack,
		gatewayclient.GatewayRPCCallOptions{
			Timeout: diagnoseCallTimeout,
			Retries: 0,
		},
	)
}

func extractIDMAskText(payload any) string {
	switch typed := payload.(type) {
	case nil:
		return ""
	case string:
		return typed
	case map[string]any:
		if text, ok := readMapRawStringValue(typed, "delta"); ok {
			return text
		}
		for _, key := range []string{"delta", "full_response", "message", "text", "content", "summary"} {
			if text := strings.TrimSpace(readMapStringValue(typed, key)); text != "" {
				return text
			}
		}
		if nested, exists := readMapAnyValue(typed, "payload"); exists {
			return extractIDMAskText(nested)
		}
		return ""
	default:
		raw, err := json.Marshal(typed)
		if err != nil {
			return strings.TrimSpace(fmt.Sprint(typed))
		}
		decoded := map[string]any{}
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return strings.TrimSpace(fmt.Sprint(typed))
		}
		return extractIDMAskText(decoded)
	}
}

// flushPendingUTF8 璐熻矗 flushPendingUTF8 鐩稿叧閫昏緫銆
func (c *idmController) flushPendingUTF8() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.utf8Pending) == 0 {
		return
	}
	c.lineBuffer = append(c.lineBuffer, c.utf8Pending...)
	c.utf8Pending = c.utf8Pending[:0]
}

// handleBackspace 璐熻矗 handleBackspace 鐩稿叧閫昏緫銆
func (c *idmController) handleBackspace() {
	c.mu.Lock()
	if len(c.utf8Pending) > 0 {
		c.utf8Pending = c.utf8Pending[:len(c.utf8Pending)-1]
		c.mu.Unlock()
		return
	}
	if len(c.lineBuffer) == 0 {
		c.mu.Unlock()
		return
	}
	_, size := utf8.DecodeLastRune(c.lineBuffer)
	if size <= 0 || size > len(c.lineBuffer) {
		size = 1
	}
	c.lineBuffer = c.lineBuffer[:len(c.lineBuffer)-size]
	c.mu.Unlock()
	c.writeRawOutput([]byte("\b \b"))
}

// handleUTF8Byte 璐熻矗 handleUTF8Byte 鐩稿叧閫昏緫銆
func (c *idmController) handleUTF8Byte(inputByte byte) {
	c.mu.Lock()
	c.utf8Pending = append(c.utf8Pending, inputByte)
	shouldFlush := utf8.FullRune(c.utf8Pending) || len(c.utf8Pending) >= utf8.UTFMax
	if !shouldFlush {
		c.mu.Unlock()
		return
	}
	token := append([]byte(nil), c.utf8Pending...)
	c.lineBuffer = append(c.lineBuffer, token...)
	c.utf8Pending = c.utf8Pending[:0]
	c.mu.Unlock()
	c.writeRawOutput(token)
}

// writePrompt 璐熻矗 writePrompt 鐩稿叧閫昏緫銆
func (c *idmController) writePrompt() {
	c.writeRawOutput([]byte(idmSystemColor + idmPromptText + idmColorReset))
}

// writeSystemMessage 璐熻矗 writeSystemMessage 鐩稿叧閫昏緫銆
func (c *idmController) writeSystemMessage(text string) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return
	}
	c.writeRawOutput([]byte(idmSystemColor + trimmed + idmColorReset + "\r\n"))
}

// writeFriendlyMessage 璐熻矗 writeFriendlyMessage 鐩稿叧閫昏緫銆
func (c *idmController) writeFriendlyMessage(text string) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return
	}
	c.writeRawOutput([]byte(idmAIColor + trimmed + idmColorReset + "\r\n"))
}

// writeRawOutput 璐熻矗 writeRawOutput 鐩稿叧閫昏緫銆
func (c *idmController) writeRawOutput(payload []byte) {
	if c == nil || c.output == nil || len(payload) == 0 {
		return
	}
	_, _ = c.output.Write(payload)
}

// extractIDMRuntimeEnvelope 璐熻矗 extractIDMRuntimeEnvelope 鐩稿叧閫昏緫銆
func extractIDMRuntimeEnvelope(payload any) (map[string]any, bool) {
	switch typed := payload.(type) {
	case map[string]any:
		if _, exists := readMapAnyValue(typed, "runtime_event_type"); exists {
			return typed, true
		}
		if nested, exists := readMapAnyValue(typed, "payload"); exists {
			if nestedMap, ok := nested.(map[string]any); ok {
				if _, hasEventType := readMapAnyValue(nestedMap, "runtime_event_type"); hasEventType {
					return nestedMap, true
				}
			}
		}
	case nil:
		return nil, false
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, false
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, false
	}
	if _, exists := readMapAnyValue(decoded, "runtime_event_type"); exists {
		return decoded, true
	}
	if nested, exists := readMapAnyValue(decoded, "payload"); exists {
		if nestedMap, ok := nested.(map[string]any); ok {
			if _, hasEventType := readMapAnyValue(nestedMap, "runtime_event_type"); hasEventType {
				return nestedMap, true
			}
		}
	}
	return nil, false
}

// readMapAnyValue 璐熻矗 readMapAnyValue 鐩稿叧閫昏緫銆
func readMapAnyValue(container map[string]any, key string) (any, bool) {
	if container == nil {
		return nil, false
	}
	value, ok := container[strings.TrimSpace(key)]
	return value, ok
}

// readMapStringValue 璐熻矗 readMapStringValue 鐩稿叧閫昏緫銆
func readMapStringValue(container map[string]any, key string) string {
	value, ok := readMapAnyValue(container, key)
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

// readMapRawStringValue 读取字符串值但保留首尾空白，用于流式 delta 拼接。
func readMapRawStringValue(container map[string]any, key string) (string, bool) {
	value, ok := readMapAnyValue(container, key)
	if !ok || value == nil {
		return "", false
	}
	typed, ok := value.(string)
	if !ok {
		return fmt.Sprint(value), true
	}
	return typed, true
}

// stringifyRuntimePayload 璐熻矗 stringifyRuntimePayload 鐩稿叧閫昏緫銆
func stringifyRuntimePayload(payload any) string {
	switch typed := payload.(type) {
	case nil:
		return ""
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	default:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return strings.TrimSpace(fmt.Sprint(typed))
		}
		return string(encoded)
	}
}

// extractIDMDonePayloadText 浠?agent_done 璐熻浇涓彁鍙栧彲灞曠ず鏂囨湰锛屽厹搴曟棤 chunk 鐨勫畬鎴愪簨浠躲€
func extractIDMDonePayloadText(payload any) string {
	switch typed := payload.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case map[string]any:
		return extractIDMTextFromMap(typed)
	default:
		raw, err := json.Marshal(typed)
		if err != nil {
			return strings.TrimSpace(fmt.Sprint(typed))
		}
		var decoded map[string]any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return strings.TrimSpace(fmt.Sprint(typed))
		}
		return extractIDMTextFromMap(decoded)
	}
}

// extractIDMTextFromMap 鎸?Runtime message 甯歌缁撴瀯璇诲彇 text/content/parts 鏂囨湰銆
func extractIDMTextFromMap(container map[string]any) string {
	if container == nil {
		return ""
	}
	for _, key := range []string{"full_response", "text", "content", "summary"} {
		if value := strings.TrimSpace(readMapStringValue(container, key)); value != "" {
			return value
		}
	}
	parts, ok := readMapAnyValue(container, "parts")
	if !ok {
		return ""
	}
	items, ok := parts.([]any)
	if !ok {
		return ""
	}
	var builder strings.Builder
	for _, item := range items {
		part, ok := item.(map[string]any)
		if !ok {
			continue
		}
		text := readMapStringValue(part, "text")
		if strings.TrimSpace(text) == "" {
			text = readMapStringValue(part, "content")
		}
		if strings.TrimSpace(text) == "" {
			continue
		}
		builder.WriteString(text)
	}
	return strings.TrimSpace(builder.String())
}

// renderIDMStreamChunk 灏嗘ā鍨嬫祦寮忕墖娈电洿鎺ュ啓鍏ョ粓绔紝閬垮厤绛夊緟瀹屾暣 Markdown 娓叉煋瀹屾垚銆
func (c *idmController) renderIDMStreamChunk(rawText string) {
	if c == nil || rawText == "" {
		return
	}
	chunk := rawText

	chunk = proxyOutputLineEndingNormalizer.Replace(rawText)

	c.writeRawOutput([]byte(idmAIColor))
	c.writeRawOutput([]byte(chunk))
	c.writeRawOutput([]byte(idmColorReset))
}

// renderIDMAnswer 灏嗘ā鍨嬪洖澶嶆寜 Markdown 娓叉煋鍚庤緭鍑猴紝娓叉煋澶辫触鏃跺洖閫€鍘熷鏂囨湰銆
func (c *idmController) renderIDMAnswer(rawText string) {
	if c == nil {
		return
	}
	trimmed := strings.TrimSpace(rawText)
	if trimmed == "" {
		return
	}
	rendered, err := renderIDMMarkdown(trimmed)
	if err != nil {
		plain := trimmed

		plain = proxyOutputLineEndingNormalizer.Replace(trimmed)

		c.writeRawOutput([]byte(idmAIColor))
		c.writeRawOutput([]byte(plain))
		c.writeRawOutput([]byte(idmColorReset))
		return
	}

	rendered = proxyOutputLineEndingNormalizer.Replace(rendered)

	c.writeRawOutput([]byte(rendered))
}

// renderIDMMarkdown 浣跨敤缁堢娓叉煋鍣ㄦ妸 Markdown 鏂囨湰杞崲涓?ANSI 缁堢鍙鏍煎紡銆
func renderIDMMarkdown(markdown string) (string, error) {
	idmMarkdownRendererOnce.Do(func() {
		idmMarkdownRenderer, idmMarkdownRendererErr = glamour.NewTermRenderer(
			glamour.WithStandardStyle("dark"),
			glamour.WithWordWrap(idmMarkdownWrapWidth),
		)
	})
	if idmMarkdownRendererErr != nil {
		return "", idmMarkdownRendererErr
	}
	if idmMarkdownRenderer == nil {
		return "", errors.New("idm markdown renderer is nil")
	}
	return idmMarkdownRenderer.Render(markdown)
}

// rejectPermissionInIDM 鍦?IDM 鏀跺埌鏉冮檺璇锋眰鏃惰嚜鍔ㄦ嫆缁濓紝閬垮厤鍥犵己灏戝鎵逛氦浜掑鑷村崱姝汇€
func (c *idmController) rejectPermissionInIDM(payload any) error {
	requestID, toolName := readPermissionRequestFromPayload(payload)
	if requestID == "" {
		return errors.New("IDM 妫€娴嬪埌宸ュ叿鏉冮檺璇锋眰锛屼絾鏈壘鍒?request_id锛屽凡鍙栨秷褰撳墠 @ai 璇锋眰")
	}
	if err := c.resolvePermission(requestID, "reject"); err != nil {
		return fmt.Errorf("IDM 鑷姩鎷掔粷宸ュ叿鏉冮檺澶辫触: %w", err)
	}
	if strings.TrimSpace(toolName) == "" {
		toolName = "unknown"
	}
	return fmt.Errorf("IDM 鏆備笉鏀寔宸ュ叿鏉冮檺瀹℃壒锛屽凡鑷姩鎷掔粷宸ュ叿 %s 璇锋眰", strings.TrimSpace(toolName))
}

// readPermissionRequestFromPayload 瑙ｆ瀽鏉冮檺璇锋眰浜嬩欢涓殑 request_id 涓庡伐鍏峰悕銆
func readPermissionRequestFromPayload(payload any) (string, string) {
	container, ok := payload.(map[string]any)
	if !ok {
		raw, err := json.Marshal(payload)
		if err != nil {
			return "", ""
		}
		decoded := map[string]any{}
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return "", ""
		}
		container = decoded
	}
	requestID := readMapStringValue(container, "request_id")
	if requestID == "" {
		requestID = readMapStringValue(container, "RequestID")
	}
	toolName := readMapStringValue(container, "tool_name")
	if toolName == "" {
		toolName = readMapStringValue(container, "ToolName")
	}
	return strings.TrimSpace(requestID), strings.TrimSpace(toolName)
}

// resolvePermission 鍚?gateway 鎻愪氦鏉冮檺鍐崇瓥锛屼緵 IDM 鐨勮嚜鍔ㄦ嫆缁濇祦绋嬪鐢ㄣ€
func (c *idmController) resolvePermission(requestID string, decision string) error {
	if c == nil || c.rpcClient == nil {
		return errors.New("gateway rpc client is not ready")
	}
	requestID = strings.TrimSpace(requestID)
	decision = strings.TrimSpace(strings.ToLower(decision))
	if requestID == "" {
		return errors.New("request id is empty")
	}
	if decision == "" {
		decision = "reject"
	}

	var ack gateway.MessageFrame
	if err := c.rpcClient.CallWithOptions(
		context.Background(),
		protocol.MethodGatewayResolvePermission,
		protocol.ResolvePermissionParams{
			RequestID: requestID,
			Decision:  decision,
		},
		&ack,
		gatewayclient.GatewayRPCCallOptions{
			Timeout: diagnoseCallTimeout,
			Retries: 0,
		},
	); err != nil {
		return err
	}
	if ack.Type == gateway.FrameTypeError && ack.Error != nil {
		return fmt.Errorf(
			"gateway resolve_permission failed (%s): %s",
			strings.TrimSpace(ack.Error.Code),
			strings.TrimSpace(ack.Error.Message),
		)
	}
	if ack.Type != gateway.FrameTypeAck {
		return fmt.Errorf("unexpected gateway frame type for resolve_permission: %s", ack.Type)
	}
	return nil
}

// generateIDMSessionID 璐熻矗 generateIDMSessionID 鐩稿叧閫昏緫銆
func generateIDMSessionID(pid int) string {
	if pid <= 0 {
		pid = os.Getpid()
	}
	sequence := idmRunSequence.Add(1)
	return fmt.Sprintf("%s%d-%d", idmSessionPrefix, pid, sequence)
}

// generateIDMRunID 璐熻矗 generateIDMRunID 鐩稿叧閫昏緫銆
func generateIDMRunID(prefix string, pid int) string {
	if strings.TrimSpace(prefix) == "" {
		prefix = idmSessionRunAskPrefix
	}
	if pid <= 0 {
		pid = os.Getpid()
	}
	sequence := idmRunSequence.Add(1)
	return fmt.Sprintf("%s-%d-%d", strings.TrimSpace(prefix), pid, sequence)
}

// ensureTerminalDiagnosisSkillFile 璐熻矗 ensureTerminalDiagnosisSkillFile 鐩稿叧閫昏緫銆
// resolveTerminalDiagnosisSkillPath 璐熻矗 resolveTerminalDiagnosisSkillPath 鐩稿叧閫昏緫銆
// cleanupZombieIDMSessions 璐熻矗 cleanupZombieIDMSessions 鐩稿叧閫昏緫銆
func cleanupZombieIDMSessions(rpcClient *gatewayclient.GatewayRPCClient, errWriter io.Writer) {
	if rpcClient == nil {
		return
	}

	var frame gateway.MessageFrame
	if err := rpcClient.CallWithOptions(
		context.Background(),
		protocol.MethodGatewayListSessions,
		nil,
		&frame,
		gatewayclient.GatewayRPCCallOptions{Timeout: diagnoseCallTimeout, Retries: 0},
	); err != nil {
		return
	}

	payloadMap, ok := frame.Payload.(map[string]any)
	if !ok {
		raw, err := json.Marshal(frame.Payload)
		if err != nil {
			return
		}
		decoded := map[string]any{}
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return
		}
		payloadMap = decoded
	}

	rawSessions, exists := readMapAnyValue(payloadMap, "sessions")
	if !exists {
		return
	}
	serialized, err := json.Marshal(rawSessions)
	if err != nil {
		return
	}
	var sessions []gateway.SessionSummary
	if err := json.Unmarshal(serialized, &sessions); err != nil {
		return
	}

	for _, sessionSummary := range sessions {
		sessionID := strings.TrimSpace(sessionSummary.ID)
		pid, ok := parseIDMSessionPID(sessionID)
		if !ok || isProcessAlive(pid) {
			continue
		}
		var deleteAck gateway.MessageFrame
		if err := rpcClient.DeleteAskSession(
			context.Background(),
			protocol.DeleteAskSessionParams{SessionID: sessionID},
			&deleteAck,
			gatewayclient.GatewayRPCCallOptions{Timeout: diagnoseCallTimeout, Retries: 0},
		); err == nil && errWriter != nil {
			writeProxyf(errWriter, "neocode shell: cleaned stale idm session %s\n", sessionID)
		}
	}
}

// parseIDMSessionPID 璐熻矗 parseIDMSessionPID 鐩稿叧閫昏緫銆
func parseIDMSessionPID(sessionID string) (int, bool) {
	trimmed := strings.TrimSpace(sessionID)
	if !strings.HasPrefix(trimmed, idmSessionPrefix) {
		return 0, false
	}
	parts := strings.Split(trimmed, "-")
	if len(parts) < 3 {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

// isProcessAlive 璐熻矗 isProcessAlive 鐩稿叧閫昏緫銆
func isProcessAlive(pid int) bool {
	return isProcessAliveByPID(pid)
}
