//go:build !windows

package ptyproxy

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
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
	idmSkillID                    = "terminal-diagnosis"
	idmSkillRelativePath          = ".neocode/skills/terminal-diagnosis/SKILL.md"
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

const terminalDiagnosisSkillMarkdown = `---
name: "terminal-diagnosis"
description: "Terminal error diagnosis agent. Text-only analysis, no tool calls."
scope: "session"
---

## Instruction

You are a terminal diagnosis agent running in a restricted sandbox. Your
only input is the error log snapshot and environment context provided by
the user.

**Hard constraints:**
- Do NOT call any tools (file read, command execution, network, etc.).
  You cannot and must not gather additional information yourself.
- Base your analysis solely on the provided error log. Do not speculate
  about content that does not appear in the log.
- Do NOT output plan_spec, summary_candidate, or any planning JSON.
- Do NOT perform build or write actions. Return diagnosis text only.
- If the log appears heavily truncated or information is insufficient,
  lower your confidence. You may suggest commands for the user to run
  manually in next_actions to gather more context, but do not pretend
  you know the exact root cause.

**Analysis priority:**
1. Locate error line numbers, file paths, function names.
2. Classify error type: syntax / permission / dependency / network /
   disk / memory / config.
3. Cross-validate with the provided exit code.

**Output Constraints:**
- The content of all output fields MUST be entirely in Chinese (except
  for code snippets, paths, or command literals).
- DO NOT wrap the output in markdown code blocks.

**Output must populate these SubAgent fields:**
- summary: One-sentence root cause (actionable).
- findings: First item MUST be confidence=<0.0~1.0> format, remainder
  as step-by-step evidence extracted from the log.
- patches: Fix commands ready to copy-paste (max 3). Leave empty if no
  safe fix exists.
- next_actions: Investigation commands for the user to run manually
  for further validation (max 3).
- risks: Limitations of your analysis or potential dangers of running
  the patches.

## References

**Common exit codes:**
| Code  | Meaning |
|-------|---------|
| 0     | Success |
| 1     | General error |
| 2     | Syntax error / misuse |
| 126   | No execute permission |
| 127   | Command not found |
| 128+N | Killed by signal N (130=SIGINT, 137=SIGKILL) |

**Typical patterns:**
- No such file or directory -> wrong path or working directory.
- Permission denied -> insufficient permissions or ownership mismatch.
- command not found -> PATH or toolchain not installed.
- undefined reference / cannot find package -> missing or conflicting
  dependency.
`

// idmRuntimeMode 负责 idmRuntimeMode 相关逻辑。
type idmRuntimeMode int

const (
	idmModeIdle idmRuntimeMode = iota
	idmModeStreaming
	idmModeNativeCmd
)

// idmControllerOptions 负责 idmControllerOptions 相关逻辑。
type idmControllerOptions struct {
	PTYWriter  io.Writer
	Output     io.Writer
	Stderr     io.Writer
	RPCClient  *gatewayclient.GatewayRPCClient
	AutoState  *autoRuntimeState
	LogBuffer  *UTF8RingBuffer
	DefaultCap int
	Workdir    string
}

// idmController 负责 idmController 相关逻辑。
type idmController struct {
	ptyWriter io.Writer
	output    io.Writer
	stderr    io.Writer
	rpcClient *gatewayclient.GatewayRPCClient
	autoState *autoRuntimeState
	logBuffer *UTF8RingBuffer
	workdir   string

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
}

// newIDMController 负责 newIDMController 相关逻辑。
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
		autoState:           options.AutoState,
		logBuffer:           options.LogBuffer,
		workdir:             strings.TrimSpace(options.Workdir),
		defaultRingCapacity: defaultCap,
		lineBuffer:          make([]byte, 0, 128),
		utf8Pending:         make([]byte, 0, utf8.UTFMax),
		pendingEcho:         make([]byte, 0, 128),
	}
}

// Enter 负责 Enter 相关逻辑。
func (c *idmController) Enter() error {
	if c == nil {
		return errors.New("idm controller is nil")
	}

	c.mu.Lock()
	if c.active {
		c.mu.Unlock()
		return nil
	}
	if c.autoState != nil && !c.autoState.OSCReady.Load() {
		c.mu.Unlock()
		return errors.New("shell integration is not ready (OSC133 unavailable)")
	}

	c.active = true
	c.mode = idmModeIdle
	c.lineBuffer = c.lineBuffer[:0]
	c.utf8Pending = c.utf8Pending[:0]
	c.pendingEcho = c.pendingEcho[:0]
	c.sessionReady = false
	c.currentRunID = ""
	c.streamCancel = nil
	c.sessionID = generateIDMSessionID(os.Getpid())
	enterSessionID := c.sessionID

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

	if err := ensureTerminalDiagnosisSkillFile(); err != nil {
		c.rollbackEnter(enterSessionID)
		return fmt.Errorf("prepare terminal-diagnosis skill failed: %w", err)
	}
	if err := c.ensureSessionReady(); err != nil {
		c.rollbackEnter(enterSessionID)
		return err
	}

	c.writeSystemMessage("[NeoCode] 已进入 IDM 交互式诊断模式，输入 `exit` 或空闲态按 Ctrl+C 退出。")
	c.writePrompt()
	return nil
}

// rollbackEnter 负责 rollbackEnter 相关逻辑。
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
		c.deleteSession(sessionID)
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

// Exit 负责 Exit 相关逻辑。
func (c *idmController) Exit() {
	if c == nil {
		return
	}

	var (
		cancelFunc     context.CancelFunc
		runID          string
		sessionID      string
		sessionReady   bool
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
	sessionReady = c.sessionReady
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
		c.cancelRun(sessionID, runID)
	}
	if sessionReady && sessionID != "" {
		c.deleteSession(sessionID)
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
	c.writeSystemMessage("[NeoCode] 已退出 IDM，恢复普通 shell 透传模式。")
}

// IsActive 负责 IsActive 相关逻辑。
func (c *idmController) IsActive() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.active
}

// ShouldPassthroughInput 负责 ShouldPassthroughInput 相关逻辑。
func (c *idmController) ShouldPassthroughInput() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.active && c.mode == idmModeNativeCmd
}

// HandleSignal 负责 HandleSignal 相关逻辑。
func (c *idmController) HandleSignal(signalValue os.Signal) bool {
	if c == nil {
		return false
	}
	if signalValue == nil || signalValue != syscall.SIGINT {
		return false
	}

	var (
		mode       idmRuntimeMode
		cancelFunc context.CancelFunc
		runID      string
		sessionID  string
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
		sessionID = strings.TrimSpace(c.sessionID)
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
			c.cancelRun(sessionID, runID)
		}
		c.writeSystemMessage("[NeoCode] 已取消当前 @ai 请求。")
		c.writePrompt()
		return true
	case idmModeNativeCmd:
		return false
	default:
		return true
	}
}

// HandleInputByte 负责 HandleInputByte 相关逻辑。
func (c *idmController) HandleInputByte(inputByte byte) {
	if c == nil {
		return
	}

	// Raw 模式下 Ctrl+C / Ctrl+Z 会以字节输入，不会触发宿主信号；这里显式转成 SIGINT 语义。
	switch inputByte {
	case 0x03, 0x1A:
		if c.HandleSignal(syscall.SIGINT) {
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

// FilterPTYOutput 负责 FilterPTYOutput 相关逻辑。
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

// OnShellEvent 负责 OnShellEvent 相关逻辑。
func (c *idmController) OnShellEvent(event ShellEvent) {
	if c == nil || event.Type != ShellEventCommandDone {
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

// handleInputLine 负责 handleInputLine 相关逻辑。
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
			c.writeFriendlyMessage(fmt.Sprintf("[NeoCode: 原生命令透传失败 (%v)]", err))
			c.writePrompt()
		}
	default:
		c.writePrompt()
	}
}

// handleAskAIAsync 以异步方式执行 @ai，避免阻塞输入循环导致 Ctrl+C 无法生效。
func (c *idmController) handleAskAIAsync(question string) {
	if c == nil {
		return
	}
	go func() {
		if err := c.sendAIMessage(question); err != nil {
			c.writeFriendlyMessage(fmt.Sprintf("[NeoCode: @ai 请求失败 (%v)]", err))
			c.writePrompt()
		}
	}()
}

// sendNativeCommand 负责 sendNativeCommand 相关逻辑。
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
	c.pendingEcho = append(c.pendingEcho[:0], []byte(commandLine+"\r\n")...)
	c.mu.Unlock()

	if _, err := io.WriteString(c.ptyWriter, commandLine+"\n"); err != nil {
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

// sendAIMessage 负责 sendAIMessage 相关逻辑。
func (c *idmController) sendAIMessage(question string) error {
	question = strings.TrimSpace(question)
	if question == "" {
		return errors.New("empty ai question")
	}
	if err := c.ensureSessionReady(); err != nil {
		return err
	}

	runID := generateIDMRunID(idmSessionRunAskPrefix, os.Getpid())
	sessionID := c.currentSessionID()
	if sessionID == "" {
		return errors.New("idm session id is empty")
	}

	streamCtx, streamCancel := context.WithCancel(context.Background())
	c.mu.Lock()
	if !c.active {
		c.mu.Unlock()
		streamCancel()
		return errors.New("idm is not active")
	}
	c.mode = idmModeStreaming
	c.currentRunID = runID
	c.streamCancel = streamCancel
	c.mu.Unlock()

	finishStreaming := func() {
		streamCancel()
		c.finishStreaming(runID)
	}

	var bindAck gateway.MessageFrame
	if err := c.rpcClient.CallWithOptions(
		streamCtx,
		protocol.MethodGatewayBindStream,
		protocol.BindStreamParams{
			SessionID: sessionID,
			RunID:     runID,
			Channel:   "all",
		},
		&bindAck,
		gatewayclient.GatewayRPCCallOptions{
			Timeout: diagnoseCallTimeout,
			Retries: 0,
		},
	); err != nil {
		finishStreaming()
		return err
	}
	if bindAck.Type == gateway.FrameTypeError && bindAck.Error != nil {
		finishStreaming()
		return fmt.Errorf("gateway bind_stream failed (%s): %s", strings.TrimSpace(bindAck.Error.Code), strings.TrimSpace(bindAck.Error.Message))
	}
	if bindAck.Type != gateway.FrameTypeAck {
		finishStreaming()
		return fmt.Errorf("unexpected gateway frame type for bind_stream: %s", bindAck.Type)
	}

	var runAck gateway.MessageFrame
	err := c.rpcClient.CallWithOptions(
		streamCtx,
		protocol.MethodGatewayRun,
		protocol.RunParams{
			SessionID: sessionID,
			RunID:     runID,
			InputText: question,
			Workdir:   c.workdir,
			Mode:      resolveIDMRunMode(),
		},
		&runAck,
		gatewayclient.GatewayRPCCallOptions{
			Timeout: diagnoseCallTimeout,
			Retries: 0,
		},
	)
	if err != nil {
		finishStreaming()
		return err
	}
	if err := validateIDMAckFrame(runAck, "run"); err != nil {
		finishStreaming()
		return err
	}

	waitErr := c.waitRunStream(streamCtx, sessionID, runID)
	c.cancelRun(sessionID, runID)
	finishStreaming()
	if waitErr != nil && !errors.Is(waitErr, context.Canceled) {
		return waitErr
	}
	c.writePrompt()
	return nil
}

// resolveIDMRunMode 返回 IDM @ai 本次运行应注入的 Runtime mode。
func resolveIDMRunMode() string {
	if !IsIDMPlanModeEnabledFromEnv() {
		return ""
	}
	return idmPlanMode
}

// validateIDMAckFrame 校验 IDM RPC 调用返回的 ACK 语义，避免失败后继续等待流事件。
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

// finishStreaming 负责 finishStreaming 相关逻辑。
func (c *idmController) finishStreaming(runID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.active {
		return
	}
	if strings.TrimSpace(c.currentRunID) != strings.TrimSpace(runID) {
		return
	}
	c.currentRunID = ""
	c.streamCancel = nil
	c.mode = idmModeIdle
}

// ensureSessionReady 负责 ensureSessionReady 相关逻辑。
func (c *idmController) ensureSessionReady() error {
	if c == nil {
		return errors.New("idm controller is nil")
	}
	if c.rpcClient == nil {
		return errors.New("gateway rpc client is not ready")
	}

	c.mu.Lock()
	sessionID := strings.TrimSpace(c.sessionID)
	ready := c.sessionReady
	c.mu.Unlock()
	if ready && sessionID != "" {
		return nil
	}
	if sessionID == "" {
		return errors.New("idm session id is empty")
	}

	if err := c.createSession(sessionID); err != nil {
		return err
	}
	if err := c.activateSessionSkill(sessionID, idmSkillID); err != nil {
		c.deleteSession(sessionID)
		return err
	}

	c.mu.Lock()
	if strings.EqualFold(strings.TrimSpace(c.sessionID), sessionID) {
		c.sessionReady = true
	}
	c.mu.Unlock()
	return nil
}

// createSession 负责 createSession 相关逻辑。
func (c *idmController) createSession(sessionID string) error {
	if c == nil || c.rpcClient == nil {
		return errors.New("gateway rpc client is not ready")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return errors.New("idm session id is empty")
	}

	var ack gateway.MessageFrame
	if err := c.rpcClient.CallWithOptions(
		context.Background(),
		protocol.MethodGatewayCreateSession,
		protocol.CreateSessionParams{SessionID: sessionID},
		&ack,
		gatewayclient.GatewayRPCCallOptions{
			Timeout: diagnoseCallTimeout,
			Retries: 0,
		},
	); err != nil {
		return err
	}
	if ack.Type == gateway.FrameTypeError && ack.Error != nil {
		return fmt.Errorf("gateway create_session failed (%s): %s", strings.TrimSpace(ack.Error.Code), strings.TrimSpace(ack.Error.Message))
	}
	if ack.Type != gateway.FrameTypeAck {
		return fmt.Errorf("unexpected gateway frame type for create_session: %s", ack.Type)
	}
	return nil
}

// activateSessionSkill 负责 activateSessionSkill 相关逻辑。
func (c *idmController) activateSessionSkill(sessionID string, skillID string) error {
	if c == nil || c.rpcClient == nil {
		return errors.New("gateway rpc client is not ready")
	}
	sessionID = strings.TrimSpace(sessionID)
	skillID = strings.TrimSpace(skillID)
	if sessionID == "" {
		return errors.New("idm session id is empty")
	}
	if skillID == "" {
		return errors.New("idm skill id is empty")
	}

	var skillAck gateway.MessageFrame
	if err := c.rpcClient.CallWithOptions(
		context.Background(),
		protocol.MethodGatewayActivateSessionSkill,
		protocol.ActivateSessionSkillParams{
			SessionID: sessionID,
			SkillID:   skillID,
		},
		&skillAck,
		gatewayclient.GatewayRPCCallOptions{
			Timeout: diagnoseCallTimeout,
			Retries: 0,
		},
	); err != nil {
		return err
	}
	if skillAck.Type == gateway.FrameTypeError && skillAck.Error != nil {
		return fmt.Errorf("gateway activate_session_skill failed (%s): %s", strings.TrimSpace(skillAck.Error.Code), strings.TrimSpace(skillAck.Error.Message))
	}
	if skillAck.Type != gateway.FrameTypeAck {
		return fmt.Errorf("unexpected gateway frame type for activate_session_skill: %s", skillAck.Type)
	}
	return nil
}

// currentSessionID 负责 currentSessionID 相关逻辑。
func (c *idmController) currentSessionID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.TrimSpace(c.sessionID)
}

// waitRunStream 负责 waitRunStream 相关逻辑。
func (c *idmController) waitRunStream(ctx context.Context, sessionID string, runID string) error {
	var markdownBuffer strings.Builder
	streamedChunks := false
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case notification, ok := <-c.rpcClient.Notifications():
			if !ok {
				return errors.New("gateway notification channel closed")
			}
			if !strings.EqualFold(strings.TrimSpace(notification.Method), protocol.MethodGatewayEvent) {
				continue
			}
			var frame gateway.MessageFrame
			if err := json.Unmarshal(notification.Params, &frame); err != nil {
				continue
			}
			if strings.TrimSpace(frame.SessionID) != strings.TrimSpace(sessionID) || strings.TrimSpace(frame.RunID) != strings.TrimSpace(runID) {
				continue
			}
			envelope, ok := extractIDMRuntimeEnvelope(frame.Payload)
			if !ok {
				continue
			}
			eventType := strings.TrimSpace(readMapStringValue(envelope, "runtime_event_type"))
			payload, _ := readMapAnyValue(envelope, "payload")
			switch eventType {
			case "agent_chunk":
				chunk := stringifyRuntimePayload(payload)
				if chunk == "" {
					continue
				}
				markdownBuffer.WriteString(chunk)
				c.renderIDMStreamChunk(chunk)
				streamedChunks = true
			case "agent_done":
				answer := markdownBuffer.String()
				if strings.TrimSpace(answer) == "" {
					answer = extractIDMDonePayloadText(payload)
				}
				if !streamedChunks {
					c.renderIDMAnswer(answer)
				}
				c.writeRawOutput([]byte("\r\n"))
				return nil
			case "run_canceled":
				return context.Canceled
			case "permission_requested":
				return c.rejectPermissionInIDM(payload)
			case "error":
				message := strings.TrimSpace(stringifyRuntimePayload(payload))
				if message == "" {
					message = "runtime error"
				}
				return errors.New(message)
			}
		}
	}
}

// cancelRun 负责 cancelRun 相关逻辑。
func (c *idmController) cancelRun(sessionID string, runID string) {
	if c == nil || c.rpcClient == nil || strings.TrimSpace(runID) == "" {
		return
	}
	var ack gateway.MessageFrame
	_ = c.rpcClient.CallWithOptions(
		context.Background(),
		protocol.MethodGatewayCancel,
		protocol.CancelParams{
			SessionID: strings.TrimSpace(sessionID),
			RunID:     strings.TrimSpace(runID),
		},
		&ack,
		gatewayclient.GatewayRPCCallOptions{
			Timeout: diagnoseCallTimeout,
			Retries: 0,
		},
	)
}

// deleteSession 负责 deleteSession 相关逻辑。
func (c *idmController) deleteSession(sessionID string) {
	if c == nil || c.rpcClient == nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	var ack gateway.MessageFrame
	_ = c.rpcClient.CallWithOptions(
		context.Background(),
		protocol.MethodGatewayDeleteSession,
		protocol.DeleteSessionParams{SessionID: strings.TrimSpace(sessionID)},
		&ack,
		gatewayclient.GatewayRPCCallOptions{
			Timeout: diagnoseCallTimeout,
			Retries: 0,
		},
	)
}

// flushPendingUTF8 负责 flushPendingUTF8 相关逻辑。
func (c *idmController) flushPendingUTF8() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.utf8Pending) == 0 {
		return
	}
	c.lineBuffer = append(c.lineBuffer, c.utf8Pending...)
	c.utf8Pending = c.utf8Pending[:0]
}

// handleBackspace 负责 handleBackspace 相关逻辑。
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

// handleUTF8Byte 负责 handleUTF8Byte 相关逻辑。
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

// writePrompt 负责 writePrompt 相关逻辑。
func (c *idmController) writePrompt() {
	c.writeRawOutput([]byte(idmSystemColor + idmPromptText + idmColorReset))
}

// writeSystemMessage 负责 writeSystemMessage 相关逻辑。
func (c *idmController) writeSystemMessage(text string) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return
	}
	c.writeRawOutput([]byte(idmSystemColor + trimmed + idmColorReset + "\r\n"))
}

// writeFriendlyMessage 负责 writeFriendlyMessage 相关逻辑。
func (c *idmController) writeFriendlyMessage(text string) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return
	}
	c.writeRawOutput([]byte(idmAIColor + trimmed + idmColorReset + "\r\n"))
}

// writeRawOutput 负责 writeRawOutput 相关逻辑。
func (c *idmController) writeRawOutput(payload []byte) {
	if c == nil || c.output == nil || len(payload) == 0 {
		return
	}
	_, _ = c.output.Write(payload)
}

// serveIDMSocket 负责 serveIDMSocket 相关逻辑。
func serveIDMSocket(ctx context.Context, listener net.Listener, controller *idmController, errWriter io.Writer) {
	for {
		connection, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil || isClosedNetworkError(err) {
				return
			}
			if errWriter != nil {
				writeProxyf(errWriter, "neocode idm: accept signal error: %v\n", err)
			}
			continue
		}
		handleIDMSocketConnection(connection, controller)
	}
}

// handleIDMSocketConnection 负责 handleIDMSocketConnection 相关逻辑。
func handleIDMSocketConnection(connection net.Conn, controller *idmController) {
	if connection == nil {
		return
	}
	defer connection.Close()

	_ = connection.SetReadDeadline(time.Now().Add(diagSocketReadDeadline))
	reader := bufio.NewReader(io.LimitReader(connection, 8*1024))
	line, err := reader.ReadBytes('\n')
	if err != nil {
		writeDiagIPCResponse(connection, diagIPCResponse{OK: false, Message: "read request failed"})
		return
	}
	_ = connection.SetReadDeadline(time.Time{})

	var request diagIPCRequest
	if unmarshalErr := json.Unmarshal(line, &request); unmarshalErr != nil {
		writeDiagIPCResponse(connection, diagIPCResponse{OK: false, Message: "invalid request"})
		return
	}
	if normalizeDiagIPCCommand(request.Cmd) != diagCommandIDMEnter {
		writeDiagIPCResponse(connection, diagIPCResponse{OK: false, Message: "unsupported command"})
		return
	}
	if controller == nil {
		writeDiagIPCResponse(connection, diagIPCResponse{OK: false, Message: "idm controller is unavailable"})
		return
	}
	if err := controller.Enter(); err != nil {
		writeDiagIPCResponse(connection, diagIPCResponse{OK: false, Message: err.Error()})
		return
	}
	writeDiagIPCResponse(connection, diagIPCResponse{OK: true, Message: "idm entered"})
}

// extractIDMRuntimeEnvelope 负责 extractIDMRuntimeEnvelope 相关逻辑。
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

// readMapAnyValue 负责 readMapAnyValue 相关逻辑。
func readMapAnyValue(container map[string]any, key string) (any, bool) {
	if container == nil {
		return nil, false
	}
	value, ok := container[strings.TrimSpace(key)]
	return value, ok
}

// readMapStringValue 负责 readMapStringValue 相关逻辑。
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

// stringifyRuntimePayload 负责 stringifyRuntimePayload 相关逻辑。
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

// extractIDMDonePayloadText 从 agent_done 负载中提取可展示文本，兜底无 chunk 的完成事件。
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

// extractIDMTextFromMap 按 Runtime message 常见结构读取 text/content/parts 文本。
func extractIDMTextFromMap(container map[string]any) string {
	if container == nil {
		return ""
	}
	for _, key := range []string{"text", "content", "summary"} {
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

// renderIDMStreamChunk 将模型流式片段直接写入终端，避免等待完整 Markdown 渲染完成。
func (c *idmController) renderIDMStreamChunk(rawText string) {
	if c == nil || rawText == "" {
		return
	}
	c.writeRawOutput([]byte(idmAIColor))
	c.writeRawOutput([]byte(proxyOutputLineEndingNormalizer.Replace(rawText)))
	c.writeRawOutput([]byte(idmColorReset))
}

// renderIDMAnswer 将模型回复按 Markdown 渲染后输出，渲染失败时回退原始文本。
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
		c.writeRawOutput([]byte(idmAIColor))
		c.writeRawOutput([]byte(proxyOutputLineEndingNormalizer.Replace(trimmed)))
		c.writeRawOutput([]byte(idmColorReset))
		return
	}
	c.writeRawOutput([]byte(proxyOutputLineEndingNormalizer.Replace(rendered)))
}

// renderIDMMarkdown 使用终端渲染器把 Markdown 文本转换为 ANSI 终端可读格式。
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

// rejectPermissionInIDM 在 IDM 收到权限请求时自动拒绝，避免因缺少审批交互导致卡死。
func (c *idmController) rejectPermissionInIDM(payload any) error {
	requestID, toolName := readPermissionRequestFromPayload(payload)
	if requestID == "" {
		return errors.New("IDM 检测到工具权限请求，但未找到 request_id，已取消当前 @ai 请求")
	}
	if err := c.resolvePermission(requestID, "reject"); err != nil {
		return fmt.Errorf("IDM 自动拒绝工具权限失败: %w", err)
	}
	if strings.TrimSpace(toolName) == "" {
		toolName = "unknown"
	}
	return fmt.Errorf("IDM 暂不支持工具权限审批，已自动拒绝工具 %s 请求", strings.TrimSpace(toolName))
}

// readPermissionRequestFromPayload 解析权限请求事件中的 request_id 与工具名。
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

// resolvePermission 向 gateway 提交权限决策，供 IDM 的自动拒绝流程复用。
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

// generateIDMSessionID 负责 generateIDMSessionID 相关逻辑。
func generateIDMSessionID(pid int) string {
	if pid <= 0 {
		pid = os.Getpid()
	}
	sequence := idmRunSequence.Add(1)
	return fmt.Sprintf("%s%d-%d", idmSessionPrefix, pid, sequence)
}

// generateIDMRunID 负责 generateIDMRunID 相关逻辑。
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

// ensureTerminalDiagnosisSkillFile 负责 ensureTerminalDiagnosisSkillFile 相关逻辑。
func ensureTerminalDiagnosisSkillFile() error {
	skillPath, err := resolveTerminalDiagnosisSkillPath()
	if err != nil {
		return err
	}
	if info, statErr := os.Stat(skillPath); statErr == nil && !info.IsDir() {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(skillPath), 0o700); err != nil {
		return fmt.Errorf("create skill directory: %w", err)
	}
	if err := os.WriteFile(skillPath, []byte(terminalDiagnosisSkillMarkdown), 0o600); err != nil {
		return fmt.Errorf("write skill file: %w", err)
	}
	return nil
}

// resolveTerminalDiagnosisSkillPath 负责 resolveTerminalDiagnosisSkillPath 相关逻辑。
func resolveTerminalDiagnosisSkillPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home dir: %w", err)
	}
	return filepath.Join(homeDir, idmSkillRelativePath), nil
}

// cleanupZombieIDMSessions 负责 cleanupZombieIDMSessions 相关逻辑。
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
		if err := rpcClient.CallWithOptions(
			context.Background(),
			protocol.MethodGatewayDeleteSession,
			protocol.DeleteSessionParams{SessionID: sessionID},
			&deleteAck,
			gatewayclient.GatewayRPCCallOptions{Timeout: diagnoseCallTimeout, Retries: 0},
		); err == nil && errWriter != nil {
			writeProxyf(errWriter, "neocode shell: cleaned stale idm session %s\n", sessionID)
		}
	}
}

// parseIDMSessionPID 负责 parseIDMSessionPID 相关逻辑。
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

// isProcessAlive 负责 isProcessAlive 相关逻辑。
func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	return errors.Is(err, syscall.EPERM)
}
