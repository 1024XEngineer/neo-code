package urlscheme

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"neo-code/internal/gateway"
	"neo-code/internal/gateway/launcher"
	"neo-code/internal/gateway/protocol"
	"neo-code/internal/gateway/transport"
)

const (
	// ErrorCodeGatewayUnavailable 表示网关不可达。
	ErrorCodeGatewayUnavailable = "gateway_unreachable"
	// ErrorCodeUnexpectedResponse 表示网关响应结构不符合预期。
	ErrorCodeUnexpectedResponse = "unexpected_response"
	// ErrorCodeNotSupported 表示当前平台或能力不支持。
	ErrorCodeNotSupported = "not_supported"
	// ErrorCodeInternal 表示内部错误。
	ErrorCodeInternal = "internal_error"

	// defaultDispatchIOTimeout 是 URL 派发读写超时时间。
	defaultDispatchIOTimeout = 10 * time.Second
	// defaultGatewayLaunchTimeout 是自动拉起网关后的就绪等待时间。
	defaultGatewayLaunchTimeout = 3 * time.Second
	// defaultGatewayLaunchRetryInterval 是拉起后拨号重试间隔。
	defaultGatewayLaunchRetryInterval = 100 * time.Millisecond
	wakeReviewStartupPromptTemplate   = "请审查文件 %s"
)

var dispatchRequestCounter uint64

// WakeDispatchRequest 表示基于已标准化 WakeIntent 的派发请求。
type WakeDispatchRequest struct {
	Intent        protocol.WakeIntent
	ListenAddress string
	AuthToken     string
}

// DispatchResult 表示 URL Scheme 派发结果。
type DispatchResult struct {
	ListenAddress    string               `json:"listen_address"`
	Request          gateway.MessageFrame `json:"request"`
	Response         gateway.MessageFrame `json:"response"`
	TerminalLaunched bool                 `json:"terminal_launched,omitempty"`
}

// DispatchError 表示 URL 派发错误。
type DispatchError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Error 将 DispatchError 转成 error 字符串。
func (e *DispatchError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Dispatcher 负责将 Wake 请求通过 IPC 转发到网关。
type Dispatcher struct {
	resolveListenAddressFn func(string) (string, error)
	dialFn                 func(address string) (net.Conn, error)
	requestIDFn            func() string
	resolveLaunchSpecFn    func() (launcher.LaunchSpec, error)
	startGatewayFn         func(launcher.LaunchSpec) error
	launchTerminalFn       func(string) error
	nowFn                  func() time.Time
	sleepFn                func(time.Duration)
	autoLaunchGateway      bool
	logger                 *log.Logger
}

// NewDispatcher 构建默认的 URL 派发器。
func NewDispatcher() *Dispatcher {
	return &Dispatcher{
		resolveListenAddressFn: transport.ResolveListenAddress,
		dialFn:                 transport.Dial,
		requestIDFn:            nextDispatchRequestID,
		resolveLaunchSpecFn: func() (launcher.LaunchSpec, error) {
			return launcher.ResolveGatewayLaunchSpec(launcher.ResolveOptions{
				ExplicitBinary: strings.TrimSpace(os.Getenv(launcher.EnvGatewayBinary)),
			})
		},
		startGatewayFn:    launcher.StartDetachedGateway,
		launchTerminalFn:  launcher.LaunchTerminal,
		nowFn:             time.Now,
		sleepFn:           time.Sleep,
		autoLaunchGateway: true,
		logger:            log.New(os.Stderr, "wake-dispatch: ", log.LstdFlags),
	}
}

// DispatchWakeIntent 直接派发 WakeIntent，避免 URL 二次编解码。
func (d *Dispatcher) DispatchWakeIntent(ctx context.Context, request WakeDispatchRequest) (DispatchResult, error) {
	intent, err := normalizeWakeDispatchIntent(request.Intent)
	if err != nil {
		return DispatchResult{}, err
	}

	resolveListenAddressFn := d.resolveListenAddressFn
	if resolveListenAddressFn == nil {
		resolveListenAddressFn = transport.ResolveListenAddress
	}
	listenAddress, err := resolveListenAddressFn(request.ListenAddress)
	if err != nil {
		return DispatchResult{}, newDispatchError(ErrorCodeInternal, fmt.Sprintf("resolve listen address: %v", err))
	}

	requestIDFn := d.requestIDFn
	if requestIDFn == nil {
		requestIDFn = nextDispatchRequestID
	}
	requestID := requestIDFn()
	conn, err := d.dialGatewayWithFallback(ctx, listenAddress, requestID, request.AuthToken)
	if err != nil {
		return DispatchResult{}, err
	}
	defer func() {
		_ = conn.Close()
	}()

	if err := applyDispatchDeadline(conn, ctx); err != nil {
		return DispatchResult{}, newDispatchError(ErrorCodeInternal, fmt.Sprintf("set connection deadline: %v", err))
	}
	if err := ensureDispatchContextActive(ctx); err != nil {
		return DispatchResult{}, toDispatchError(err)
	}
	stopCancelWatcher := watchDispatchCancellation(ctx, conn)
	defer stopCancelWatcher()

	authToken := strings.TrimSpace(request.AuthToken)
	if authToken != "" {
		if err := d.authenticate(ctx, conn, authToken); err != nil {
			return DispatchResult{}, err
		}
	}

	requestFrame := gateway.MessageFrame{
		Type:      gateway.FrameTypeRequest,
		Action:    gateway.FrameActionWakeOpenURL,
		RequestID: requestID,
		SessionID: intent.SessionID,
		Workdir:   intent.Workdir,
		Payload:   intent,
	}
	rpcRequest, err := buildWakeOpenURLRPCRequest(requestFrame.RequestID, intent)
	if err != nil {
		return DispatchResult{}, err
	}

	if err := ensureDispatchContextActive(ctx); err != nil {
		return DispatchResult{}, toDispatchError(err)
	}
	rpcResponse, err := d.callRPC(ctx, conn, rpcRequest)
	if err != nil {
		return DispatchResult{}, err
	}
	responseFrame, err := validateRPCFrameResponse(
		rpcResponse,
		rpcRequest.ID,
		"unexpected response jsonrpc version",
		"rpc correlation failed: id mismatch",
		"unexpected response payload: both result and error are present",
		"gateway response missing result payload",
		"decode response frame: %v",
	)
	if err != nil {
		return DispatchResult{}, err
	}
	if responseFrame.Action != requestFrame.Action || responseFrame.RequestID != requestFrame.RequestID {
		return DispatchResult{}, newDispatchError(
			ErrorCodeUnexpectedResponse,
			"frame correlation failed: action or request_id mismatch",
		)
	}

	switch responseFrame.Type {
	case gateway.FrameTypeAck:
		terminalLaunched := false
		normalizedWakeAction := strings.ToLower(strings.TrimSpace(intent.Action))
		if normalizedWakeAction == protocol.WakeActionRun || normalizedWakeAction == protocol.WakeActionReview {
			sessionID := strings.TrimSpace(responseFrame.SessionID)
			if sessionID == "" {
				return DispatchResult{}, newDispatchError(
					ErrorCodeUnexpectedResponse,
					fmt.Sprintf("wake.%s response missing session_id", normalizedWakeAction),
				)
			}
			if d.launchTerminalFn == nil {
				return DispatchResult{}, newDispatchError(ErrorCodeInternal, "terminal launcher is unavailable")
			}
			launchCommand, launchCommandErr := buildWakeLaunchCommand(intent, sessionID)
			if launchCommandErr != nil {
				return DispatchResult{}, newDispatchError(ErrorCodeInternal, launchCommandErr.Error())
			}
			if launchErr := d.launchTerminalFn(launchCommand); launchErr != nil {
				if errors.Is(launchErr, launcher.ErrTerminalUnsupported) {
					return DispatchResult{}, newDispatchError(ErrorCodeNotSupported, launchErr.Error())
				}
				return DispatchResult{}, newDispatchError(
					ErrorCodeInternal,
					fmt.Sprintf("launch terminal failed: %v", launchErr),
				)
			}
			terminalLaunched = true
		}
		return DispatchResult{
			ListenAddress:    listenAddress,
			Request:          requestFrame,
			Response:         responseFrame,
			TerminalLaunched: terminalLaunched,
		}, nil
	case gateway.FrameTypeError:
		if responseFrame.Error == nil {
			return DispatchResult{}, newDispatchError(ErrorCodeUnexpectedResponse, "gateway error frame missing error payload")
		}
		return DispatchResult{}, newDispatchError(responseFrame.Error.Code, responseFrame.Error.Message)
	default:
		return DispatchResult{}, newDispatchError(
			ErrorCodeUnexpectedResponse,
			fmt.Sprintf("unsupported response frame type: %s", responseFrame.Type),
		)
	}
}

// normalizeWakeDispatchIntent 统一整理并校验 daemon 等入口共用的意图数据。
func normalizeWakeDispatchIntent(intent protocol.WakeIntent) (protocol.WakeIntent, error) {
	normalizedAction := strings.ToLower(strings.TrimSpace(intent.Action))
	if !protocol.IsSupportedWakeAction(normalizedAction) {
		return protocol.WakeIntent{}, newDispatchError(
			gateway.ErrorCodeInvalidAction.String(),
			fmt.Sprintf("invalid wake action: %s", strings.TrimSpace(intent.Action)),
		)
	}
	intent.Action = normalizedAction
	intent.SessionID = strings.TrimSpace(intent.SessionID)
	intent.Workdir = strings.TrimSpace(intent.Workdir)
	if len(intent.Params) == 0 {
		intent.Params = nil
	}
	return intent, nil
}

// buildWakeLaunchCommand 构造终端拉起命令，并在首次点击时附带一次性启动输入参数。
func buildWakeLaunchCommand(intent protocol.WakeIntent, sessionID string) (string, error) {
	normalizedSessionID := strings.TrimSpace(sessionID)
	if normalizedSessionID == "" {
		return "", errors.New("wake response session_id is empty")
	}
	command := fmt.Sprintf("neocode --session %s", normalizedSessionID)
	if strings.TrimSpace(intent.SessionID) != "" {
		return command, nil
	}
	payload, err := buildWakeStartupPayload(intent)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s --wake-input-b64 %s", command, payload), nil
}

// buildWakeStartupPayload 将首次点击的 wake intent 转换为 TUI 启动后自动提交所需负载。
func buildWakeStartupPayload(intent protocol.WakeIntent) (string, error) {
	action := strings.ToLower(strings.TrimSpace(intent.Action))
	text := ""
	switch action {
	case protocol.WakeActionRun:
		text = strings.TrimSpace(intent.Params["prompt"])
	case protocol.WakeActionReview:
		path := strings.TrimSpace(intent.Params["path"])
		if path != "" {
			text = fmt.Sprintf(wakeReviewStartupPromptTemplate, path)
		}
	}
	if text == "" {
		return "", fmt.Errorf("wake.%s startup input is empty", action)
	}
	return protocol.EncodeWakeStartupInput(protocol.WakeStartupInput{
		Text:    text,
		Workdir: strings.TrimSpace(intent.Workdir),
	})
}

// buildWakeOpenURLRPCRequest 将 WakeIntent 编码为 JSON-RPC 请求。
func buildWakeOpenURLRPCRequest(requestID string, intent protocol.WakeIntent) (protocol.JSONRPCRequest, error) {
	requestIDRaw, err := marshalJSONRawMessage(requestID)
	if err != nil {
		return protocol.JSONRPCRequest{}, newDispatchError(ErrorCodeInternal, fmt.Sprintf("encode request id: %v", err))
	}
	requestParamsRaw, err := marshalJSONRawMessage(intent)
	if err != nil {
		return protocol.JSONRPCRequest{}, newDispatchError(ErrorCodeInternal, fmt.Sprintf("encode request params: %v", err))
	}
	return protocol.JSONRPCRequest{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      requestIDRaw,
		Method:  protocol.MethodWakeOpenURL,
		Params:  requestParamsRaw,
	}, nil
}

// authenticate 在连接上执行 gateway.authenticate 建立连接级认证态。
func (d *Dispatcher) authenticate(ctx context.Context, conn net.Conn, token string) error {
	requestIDFn := d.requestIDFn
	if requestIDFn == nil {
		requestIDFn = nextDispatchRequestID
	}
	authRequestID := requestIDFn() + "-auth"
	authRequestIDRaw, err := marshalJSONRawMessage(authRequestID)
	if err != nil {
		return newDispatchError(ErrorCodeInternal, fmt.Sprintf("encode authenticate id: %v", err))
	}
	authParamsRaw, err := marshalJSONRawMessage(protocol.AuthenticateParams{Token: token})
	if err != nil {
		return newDispatchError(ErrorCodeInternal, fmt.Sprintf("encode authenticate params: %v", err))
	}

	authRequest := protocol.JSONRPCRequest{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      authRequestIDRaw,
		Method:  protocol.MethodGatewayAuthenticate,
		Params:  authParamsRaw,
	}
	authResponse, err := d.callRPC(ctx, conn, authRequest)
	if err != nil {
		return err
	}
	frame, err := validateRPCFrameResponse(
		authResponse,
		authRequest.ID,
		"unexpected auth response jsonrpc version",
		"rpc correlation failed: auth id mismatch",
		"unexpected response payload: both result and error are present",
		"gateway auth response missing result payload",
		"decode auth response frame: %v",
	)
	if err != nil {
		return err
	}
	if frame.Type != gateway.FrameTypeAck || frame.Action != gateway.FrameActionAuthenticate || frame.RequestID != authRequestID {
		return newDispatchError(ErrorCodeUnexpectedResponse, "unexpected auth response frame")
	}
	return nil
}

// callRPC 在已建立连接上发送并接收单次 JSON-RPC 请求。
func (d *Dispatcher) callRPC(ctx context.Context, conn net.Conn, request protocol.JSONRPCRequest) (protocol.JSONRPCResponse, error) {
	if err := ensureDispatchContextActive(ctx); err != nil {
		return protocol.JSONRPCResponse{}, toDispatchError(err)
	}
	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(request); err != nil {
		if ctx != nil && ctx.Err() != nil {
			return protocol.JSONRPCResponse{}, toDispatchError(ctx.Err())
		}
		return protocol.JSONRPCResponse{}, newDispatchError(ErrorCodeInternal, fmt.Sprintf("write request rpc: %v", err))
	}

	if err := ensureDispatchContextActive(ctx); err != nil {
		return protocol.JSONRPCResponse{}, toDispatchError(err)
	}
	var response protocol.JSONRPCResponse
	decoder := json.NewDecoder(conn)
	if err := decoder.Decode(&response); err != nil {
		if ctx != nil && ctx.Err() != nil {
			return protocol.JSONRPCResponse{}, toDispatchError(ctx.Err())
		}
		return protocol.JSONRPCResponse{}, newDispatchError(ErrorCodeUnexpectedResponse, fmt.Sprintf("decode response rpc: %v", err))
	}
	return response, nil
}

type launchDecisionLogEntry struct {
	RequestID     string `json:"request_id"`
	Method        string `json:"method"`
	Source        string `json:"source"`
	Status        string `json:"status"`
	GatewayCode   string `json:"gateway_code"`
	ListenAddress string `json:"listen_address"`
	AuthMode      string `json:"auth_mode"`
	LaunchMode    string `json:"launch_mode,omitempty"`
	ResolvedExec  string `json:"resolved_exec,omitempty"`
	Message       string `json:"message,omitempty"`
}

// dialGatewayWithFallback 首拨失败时仅执行一次拉起回退并重拨。
func (d *Dispatcher) dialGatewayWithFallback(
	ctx context.Context,
	listenAddress string,
	requestID string,
	authToken string,
) (net.Conn, error) {
	dialFn := d.dialFn
	if dialFn == nil {
		dialFn = transport.Dial
	}
	connection, err := dialFn(listenAddress)
	if err == nil {
		return connection, nil
	}
	if !d.autoLaunchGateway {
		return nil, newDispatchError(ErrorCodeGatewayUnavailable, fmt.Sprintf("dial gateway failed: %v", err))
	}
	if launchErr := d.launchGateway(ctx, listenAddress, requestID, authToken); launchErr != nil {
		return nil, newDispatchError(
			ErrorCodeGatewayUnavailable,
			fmt.Sprintf("dial gateway failed: %v; launch gateway failed: %v", err, launchErr),
		)
	}
	retriedConnection, retryErr := dialFn(listenAddress)
	if retryErr != nil {
		return nil, newDispatchError(
			ErrorCodeGatewayUnavailable,
			fmt.Sprintf("dial gateway failed after single fallback: %v", retryErr),
		)
	}
	return retriedConnection, nil
}

// launchGateway 通过 launcher 启动网关并等待就绪。
func (d *Dispatcher) launchGateway(ctx context.Context, listenAddress string, requestID string, authToken string) error {
	if err := ensureDispatchContextActive(ctx); err != nil {
		return err
	}

	resolveLaunchSpecFn := d.resolveLaunchSpecFn
	if resolveLaunchSpecFn == nil {
		return errors.New("gateway launcher is unavailable")
	}
	startGatewayFn := d.startGatewayFn
	if startGatewayFn == nil {
		return errors.New("gateway launcher start function is unavailable")
	}

	spec, err := resolveLaunchSpecFn()
	if err != nil {
		d.emitLaunchFailureLog(requestID, listenAddress, authToken, launcher.LaunchSpec{}, err)
		return err
	}

	d.emitLaunchDecisionLog(newLaunchDecisionLogEntry(
		requestID,
		listenAddress,
		authToken,
		"launch_attempt",
		"",
		spec,
		"",
	))
	launchSpec := spec
	launchSpec.Args = buildGatewayLaunchArgs(spec.Args, listenAddress)
	if err := startGatewayFn(launchSpec); err != nil {
		d.emitLaunchFailureLog(requestID, listenAddress, authToken, spec, err)
		return err
	}

	if err := d.waitGatewayReady(ctx, listenAddress); err != nil {
		d.emitLaunchFailureLog(requestID, listenAddress, authToken, spec, err)
		return err
	}

	d.emitLaunchDecisionLog(newLaunchDecisionLogEntry(
		requestID,
		listenAddress,
		authToken,
		"launch_ready",
		"",
		spec,
		"",
	))
	return nil
}

// validateRPCFrameResponse 校验 RPC 响应并解析为 MessageFrame。
func validateRPCFrameResponse(
	response protocol.JSONRPCResponse,
	expectedID json.RawMessage,
	versionMismatchMessage string,
	idMismatchMessage string,
	dualPayloadMessage string,
	missingResultMessage string,
	decodeFrameMessageFormat string,
) (gateway.MessageFrame, error) {
	if strings.TrimSpace(response.JSONRPC) != protocol.JSONRPCVersion {
		return gateway.MessageFrame{}, newDispatchError(ErrorCodeUnexpectedResponse, versionMismatchMessage)
	}
	if !rawJSONMessageEqual(response.ID, expectedID) {
		return gateway.MessageFrame{}, newDispatchError(ErrorCodeUnexpectedResponse, idMismatchMessage)
	}
	if response.Error != nil && response.Result != nil {
		return gateway.MessageFrame{}, newDispatchError(ErrorCodeUnexpectedResponse, dualPayloadMessage)
	}
	if response.Error != nil {
		return gateway.MessageFrame{}, toDispatchErrorFromJSONRPC(response.Error)
	}
	if response.Result == nil {
		return gateway.MessageFrame{}, newDispatchError(ErrorCodeUnexpectedResponse, missingResultMessage)
	}

	frame, err := decodeResponseFrameResult(response.Result)
	if err != nil {
		return gateway.MessageFrame{}, newDispatchError(
			ErrorCodeUnexpectedResponse,
			fmt.Sprintf(decodeFrameMessageFormat, err),
		)
	}
	return frame, nil
}

// buildGatewayLaunchArgs 在原有参数后补充 --listen。
func buildGatewayLaunchArgs(baseArgs []string, listenAddress string) []string {
	args := append([]string(nil), baseArgs...)
	normalizedListenAddress := strings.TrimSpace(listenAddress)
	if normalizedListenAddress == "" {
		return args
	}
	return append(args, "--listen", normalizedListenAddress)
}

// waitGatewayReady 轮询等待网关可达。
func (d *Dispatcher) waitGatewayReady(ctx context.Context, listenAddress string) error {
	nowFn := d.nowFn
	if nowFn == nil {
		nowFn = time.Now
	}
	sleepFn := d.sleepFn
	if sleepFn == nil {
		sleepFn = time.Sleep
	}
	dialFn := d.dialFn
	if dialFn == nil {
		dialFn = transport.Dial
	}

	startTime := nowFn()
	deadline := startTime.Add(defaultGatewayLaunchTimeout)
	if ctx != nil {
		if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
			deadline = ctxDeadline
		}
	}
	effectiveTimeout := deadline.Sub(startTime)
	if effectiveTimeout < 0 {
		effectiveTimeout = 0
	}

	for {
		if err := ensureDispatchContextActive(ctx); err != nil {
			return err
		}
		connection, err := dialFn(listenAddress)
		if err == nil {
			_ = connection.Close()
			return nil
		}
		if !nowFn().Before(deadline) {
			return fmt.Errorf("gateway did not become reachable within %s", effectiveTimeout)
		}
		sleepFn(defaultGatewayLaunchRetryInterval)
	}
}

// emitLaunchDecisionLog 输出结构化 launcher 决策日志。
func (d *Dispatcher) emitLaunchDecisionLog(entry launchDecisionLogEntry) {
	if d == nil || d.logger == nil {
		return
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		d.logger.Printf(`{"status":"launch_log_encode_failed","message":"%s"}`, strings.TrimSpace(err.Error()))
		return
	}
	d.logger.Print(string(raw))
}

// newLaunchDecisionLogEntry 生成统一字段的 launcher 决策日志记录。
func newLaunchDecisionLogEntry(
	requestID string,
	listenAddress string,
	authToken string,
	status string,
	gatewayCode string,
	spec launcher.LaunchSpec,
	message string,
) launchDecisionLogEntry {
	return launchDecisionLogEntry{
		RequestID:     requestID,
		Method:        string(protocol.MethodWakeOpenURL),
		Source:        "wake-dispatch",
		Status:        status,
		GatewayCode:   gatewayCode,
		ListenAddress: listenAddress,
		AuthMode:      resolveAuthMode(authToken),
		LaunchMode:    spec.LaunchMode,
		ResolvedExec:  spec.Executable,
		Message:       message,
	}
}

// emitLaunchFailureLog 输出 launcher 失败决策日志。
func (d *Dispatcher) emitLaunchFailureLog(
	requestID string,
	listenAddress string,
	authToken string,
	spec launcher.LaunchSpec,
	err error,
) {
	d.emitLaunchDecisionLog(newLaunchDecisionLogEntry(
		requestID,
		listenAddress,
		authToken,
		"launch_failed",
		ErrorCodeGatewayUnavailable,
		spec,
		err.Error(),
	))
}

// resolveAuthMode 返回日志维度所需的认证模式值。
func resolveAuthMode(authToken string) string {
	if strings.TrimSpace(authToken) == "" {
		return "disabled"
	}
	return "required"
}

// DispatchWakeIntent 是便捷入口，直接创建默认 Dispatcher 执行派发。
func DispatchWakeIntent(ctx context.Context, request WakeDispatchRequest) (DispatchResult, error) {
	return NewDispatcher().DispatchWakeIntent(ctx, request)
}

// applyDispatchDeadline 为连接设置派发 IO 截止时间。
func applyDispatchDeadline(conn net.Conn, ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if deadline, ok := ctx.Deadline(); ok {
		return conn.SetDeadline(deadline)
	}
	return conn.SetDeadline(time.Now().Add(defaultDispatchIOTimeout))
}

// ensureDispatchContextActive 检查派发上下文是否已取消。
func ensureDispatchContextActive(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

// watchDispatchCancellation 在上下文取消时打断连接阻塞 IO。
func watchDispatchCancellation(ctx context.Context, conn net.Conn) func() {
	if ctx == nil {
		return func() {}
	}

	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.SetDeadline(time.Now())
		case <-done:
		}
	}()
	return func() {
		close(done)
	}
}

// toDispatchErrorFromJSONRPC 将 JSON-RPC 错误映射为稳定 DispatchError。
func toDispatchErrorFromJSONRPC(rpcError *protocol.JSONRPCError) error {
	if rpcError == nil {
		return newDispatchError(ErrorCodeUnexpectedResponse, "gateway returned empty rpc error payload")
	}

	code := strings.TrimSpace(protocol.GatewayCodeFromJSONRPCError(rpcError))
	if code == "" {
		code = mapJSONRPCCodeToDispatchCode(rpcError.Code)
	}
	message := strings.TrimSpace(rpcError.Message)
	if message == "" {
		message = "gateway returned empty rpc error message"
	}
	return newDispatchError(code, message)
}

// mapJSONRPCCodeToDispatchCode 将标准 JSON-RPC code 映射为稳定派发错误码。
func mapJSONRPCCodeToDispatchCode(code int) string {
	switch code {
	case protocol.JSONRPCCodeMethodNotFound:
		return gateway.ErrorCodeUnsupportedAction.String()
	case protocol.JSONRPCCodeInvalidRequest, protocol.JSONRPCCodeInvalidParams, protocol.JSONRPCCodeParseError:
		return gateway.ErrorCodeInvalidFrame.String()
	case protocol.JSONRPCCodeInternalError:
		return gateway.ErrorCodeInternalError.String()
	default:
		return ErrorCodeInternal
	}
}

// decodeResponseFrameResult 解析 RPC result 为 MessageFrame。
func decodeResponseFrameResult(result json.RawMessage) (gateway.MessageFrame, error) {
	var frame gateway.MessageFrame
	if err := json.Unmarshal(result, &frame); err != nil {
		return gateway.MessageFrame{}, err
	}
	return frame, nil
}

// rawJSONMessageEqual 在忽略两端空白后比较 JSON RawMessage。
func rawJSONMessageEqual(left, right json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(left), bytes.TrimSpace(right))
}

// marshalJSONRawMessage 将任意对象编码为 json.RawMessage。
func marshalJSONRawMessage(payload any) (json.RawMessage, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
}

// toDispatchError 将任意错误转换为稳定 DispatchError。
func toDispatchError(err error) error {
	if err == nil {
		return nil
	}
	var dispatchErr *DispatchError
	if errors.As(err, &dispatchErr) {
		return dispatchErr
	}

	return newDispatchError(ErrorCodeInternal, err.Error())
}

// nextDispatchRequestID 生成 wake 请求 ID。
func nextDispatchRequestID() string {
	sequence := atomic.AddUint64(&dispatchRequestCounter, 1)
	return fmt.Sprintf("wake-%d", sequence)
}

// newDispatchError 构建稳定的 DispatchError。
func newDispatchError(code, message string) *DispatchError {
	return &DispatchError{
		Code:    code,
		Message: message,
	}
}
