//go:build !windows

package ptyproxy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"
	"sync"
	"time"

	"neo-code/internal/gateway"
	gatewayclient "neo-code/internal/gateway/client"
	"neo-code/internal/gateway/protocol"
	"neo-code/internal/tools"
)

const (
	diagnosisCacheTTL           = 5 * time.Minute
	diagnosisCacheMaxEntries    = 64
	diagnosisAutoDedupeTTL      = 10 * time.Second
	diagnosisQuickMaxConfidence = 0.55
)

type preparedDiagnosisRequest struct {
	Payload           []byte
	Fingerprint       string
	SanitizedErrorLog string
	SanitizedCommand  string
}

type diagnosisOutcome struct {
	Result tools.ToolResult
	Err    error
}

type diagnosisFlight struct {
	done    chan struct{}
	outcome diagnosisOutcome
}

type diagnosisCacheEntry struct {
	outcome   diagnosisOutcome
	expiresAt time.Time
}

// diagnosisCoordinator 负责诊断请求去重、短期缓存与自动诊断去抖。
type diagnosisCoordinator struct {
	mu         sync.Mutex
	inFlight   map[string]*diagnosisFlight
	cache      map[string]diagnosisCacheEntry
	cacheOrder []string
	recentAuto map[string]time.Time
	now        func() time.Time
}

// newDiagnosisCoordinator 创建一次 shell 会话内复用的诊断调度器。
func newDiagnosisCoordinator() *diagnosisCoordinator {
	return &diagnosisCoordinator{
		inFlight:   make(map[string]*diagnosisFlight),
		cache:      make(map[string]diagnosisCacheEntry),
		recentAuto: make(map[string]time.Time),
		now:        time.Now,
	}
}

// prepareDiagnoseRequest 统一构建脱敏后的 diagnose payload 与 fingerprint。
func prepareDiagnoseRequest(
	buffer *UTF8RingBuffer,
	options ManualShellOptions,
	socketPath string,
	trigger diagnoseTrigger,
) (preparedDiagnosisRequest, error) {
	if buffer == nil {
		buffer = NewUTF8RingBuffer(DefaultRingBufferCapacity)
	}
	logSnapshot := buffer.SnapshotString()
	if strings.TrimSpace(trigger.OutputText) != "" {
		logSnapshot = trigger.OutputText
	}
	sanitizedErrorLog := SanitizeDiagnosisText(logSnapshot, defaultDiagnosisPayloadMaxBytes)
	if strings.TrimSpace(sanitizedErrorLog) == "" {
		sanitizedErrorLog = "no terminal output captured"
	}
	sanitizedCommand := SanitizeDiagnosisText(trigger.CommandText, 1024)

	requestArgs := diagnoseToolArgs{
		ErrorLog: sanitizedErrorLog,
		OSEnv: map[string]string{
			"os":     runtime.GOOS,
			"shell":  resolveShellPath(options.Shell),
			"cwd":    options.Workdir,
			"socket": socketPath,
		},
		CommandText: sanitizedCommand,
		ExitCode:    trigger.ExitCode,
	}
	requestPayload, err := json.Marshal(requestArgs)
	if err != nil {
		return preparedDiagnosisRequest{}, err
	}
	return preparedDiagnosisRequest{
		Payload:           requestPayload,
		Fingerprint:       fingerprintDiagnosisRequest(sanitizedCommand, trigger.ExitCode, sanitizedErrorLog),
		SanitizedErrorLog: sanitizedErrorLog,
		SanitizedCommand:  sanitizedCommand,
	}, nil
}

// fingerprintDiagnosisRequest 为脱敏后的诊断输入生成稳定指纹。
func fingerprintDiagnosisRequest(command string, exitCode int, errorLog string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		strings.TrimSpace(command),
		fmt.Sprint(exitCode),
		strings.TrimSpace(errorLog),
	}, "\x00")))
	return hex.EncodeToString(sum[:])
}

// executePreparedDiagnoseToolWithTimeout 执行已构建好的 diagnose payload。
func executePreparedDiagnoseToolWithTimeout(
	rpcClient *gatewayclient.GatewayRPCClient,
	options ManualShellOptions,
	prepared preparedDiagnosisRequest,
	timeout time.Duration,
) (tools.ToolResult, error) {
	if rpcClient == nil {
		return tools.ToolResult{}, errors.New("诊断服务未就绪，请确认 gateway 已连接后重试")
	}

	callContext, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var frame gateway.MessageFrame
	if err := rpcClient.CallWithOptions(
		callContext,
		protocol.MethodGatewayExecuteSystemTool,
		protocol.ExecuteSystemToolParams{
			Workdir:   options.Workdir,
			ToolName:  tools.ToolNameDiagnose,
			Arguments: prepared.Payload,
		},
		&frame,
		gatewayclient.GatewayRPCCallOptions{
			Timeout: timeout,
			Retries: 1,
		},
	); err != nil {
		if options.Stderr != nil {
			writeProxyf(options.Stderr, "neocode diag: executeSystemTool rpc failed: %v\n", err)
		}
		return tools.ToolResult{}, errors.New("诊断调用失败，请检查 gateway 连接后重试，或使用 `neocode diag -i` 继续排查")
	}

	if frame.Type == gateway.FrameTypeError && frame.Error != nil {
		if options.Stderr != nil {
			writeProxyf(
				options.Stderr,
				"neocode diag: gateway returned frame error code=%s message=%s\n",
				strings.TrimSpace(frame.Error.Code),
				strings.TrimSpace(frame.Error.Message),
			)
		}
		return tools.ToolResult{}, errors.New("诊断服务暂不可用，请稍后重试，或使用 `neocode diag -i` 继续排查")
	}
	if frame.Type != gateway.FrameTypeAck {
		if options.Stderr != nil {
			writeProxyf(options.Stderr, "neocode diag: unexpected gateway frame type: %s\n", frame.Type)
		}
		return tools.ToolResult{}, errors.New("诊断服务返回异常响应，请稍后重试")
	}

	toolResult, err := decodeToolResult(frame.Payload)
	if err != nil {
		if options.Stderr != nil {
			writeProxyf(options.Stderr, "neocode diag: decode diagnose payload failed: %v\n", err)
		}
		return tools.ToolResult{}, errors.New("诊断结果解析失败，请重试或更新 NeoCode")
	}
	return toolResult, nil
}

// shouldDropAuto 判断自动诊断是否命中短窗口去抖。
func (c *diagnosisCoordinator) shouldDropAuto(fingerprint string) bool {
	if c == nil || strings.TrimSpace(fingerprint) == "" {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.currentTime()
	for key, seenAt := range c.recentAuto {
		if now.Sub(seenAt) > diagnosisAutoDedupeTTL {
			delete(c.recentAuto, key)
		}
	}
	if seenAt, ok := c.recentAuto[fingerprint]; ok && now.Sub(seenAt) <= diagnosisAutoDedupeTTL {
		return true
	}
	c.recentAuto[fingerprint] = now
	return false
}

// cached 返回仍在有效期内的缓存诊断结果。
func (c *diagnosisCoordinator) cached(fingerprint string) (diagnosisOutcome, bool) {
	if c == nil || strings.TrimSpace(fingerprint) == "" || !IsDiagCacheEnabledFromEnv() {
		return diagnosisOutcome{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.cache[fingerprint]
	if !ok {
		return diagnosisOutcome{}, false
	}
	if c.currentTime().After(entry.expiresAt) {
		delete(c.cache, fingerprint)
		return diagnosisOutcome{}, false
	}
	return entry.outcome, true
}

// run 执行或复用一次诊断请求，成功结果会进入短期缓存。
func (c *diagnosisCoordinator) run(
	ctx context.Context,
	fingerprint string,
	execute func() (tools.ToolResult, error),
) diagnosisOutcome {
	if c == nil || strings.TrimSpace(fingerprint) == "" || !IsDiagCacheEnabledFromEnv() {
		result, err := execute()
		return diagnosisOutcome{Result: result, Err: err}
	}
	if cached, ok := c.cached(fingerprint); ok {
		return cached
	}

	c.mu.Lock()
	if flight, ok := c.inFlight[fingerprint]; ok {
		c.mu.Unlock()
		return waitDiagnosisFlight(ctx, flight)
	}
	flight := &diagnosisFlight{done: make(chan struct{})}
	c.inFlight[fingerprint] = flight
	c.mu.Unlock()

	result, err := execute()
	outcome := diagnosisOutcome{Result: result, Err: err}

	c.mu.Lock()
	flight.outcome = outcome
	delete(c.inFlight, fingerprint)
	if err == nil {
		c.storeCacheLocked(fingerprint, outcome)
	}
	close(flight.done)
	c.mu.Unlock()
	return outcome
}

// waitDiagnosisFlight 等待已存在的同指纹诊断完成。
func waitDiagnosisFlight(ctx context.Context, flight *diagnosisFlight) diagnosisOutcome {
	if flight == nil {
		return diagnosisOutcome{Err: errors.New("diagnosis flight is nil")}
	}
	select {
	case <-ctx.Done():
		return diagnosisOutcome{Err: ctx.Err()}
	case <-flight.done:
		return flight.outcome
	}
}

// storeCacheLocked 在持锁状态下写入缓存并维护容量上限。
func (c *diagnosisCoordinator) storeCacheLocked(fingerprint string, outcome diagnosisOutcome) {
	if c == nil || strings.TrimSpace(fingerprint) == "" {
		return
	}
	if _, exists := c.cache[fingerprint]; !exists {
		c.cacheOrder = append(c.cacheOrder, fingerprint)
	}
	c.cache[fingerprint] = diagnosisCacheEntry{
		outcome:   outcome,
		expiresAt: c.currentTime().Add(diagnosisCacheTTL),
	}
	for len(c.cacheOrder) > diagnosisCacheMaxEntries {
		oldest := c.cacheOrder[0]
		c.cacheOrder = c.cacheOrder[1:]
		delete(c.cache, oldest)
	}
}

// currentTime 返回可在测试中替换的当前时间。
func (c *diagnosisCoordinator) currentTime() time.Time {
	if c != nil && c.now != nil {
		return c.now()
	}
	return time.Now()
}

// renderDiagnosisInitialFeedback 输出诊断快速首响或低干扰预判。
func renderDiagnosisInitialFeedback(output io.Writer, prepared preparedDiagnosisRequest, isAuto bool) {
	if output == nil || !IsDiagFastResponseEnabledFromEnv() {
		return
	}
	hint, ok := buildDiagnosisQuickHint(prepared)
	if isAuto && !ok {
		return
	}
	if isAuto {
		writeProxyLine(output, "\n\033[36m[NeoCode Diagnosis]\033[0m 快速预判（低置信度，完整诊断稍后返回）")
	} else {
		writeProxyLine(output, "\n\033[36m[NeoCode Diagnosis]\033[0m 正在诊断，完整结果稍后返回。")
		if !ok {
			return
		}
		writeProxyLine(output, "快速预判（低置信度）:")
	}
	if !ok {
		return
	}
	writeProxyf(output, "置信度: %.2f\n", hint.Confidence)
	writeProxyf(output, "可能根因: %s\n", strings.TrimSpace(hint.RootCause))
	if len(hint.InvestigationCommands) > 0 {
		writeProxyLine(output, "建议先查:")
		for _, command := range hint.InvestigationCommands {
			writeProxyf(output, "- %s\n", strings.TrimSpace(command))
		}
	}
}

// buildDiagnosisQuickHint 根据常见终端错误模式生成低置信度快速预判。
func buildDiagnosisQuickHint(prepared preparedDiagnosisRequest) (diagnoseToolResult, bool) {
	text := strings.ToLower(strings.TrimSpace(prepared.SanitizedErrorLog + "\n" + prepared.SanitizedCommand))
	switch {
	case strings.Contains(text, "command not found") || strings.Contains(text, "not recognized as"):
		return quickHint("命令不存在或未加入 PATH。", []string{"which <command>", "echo $PATH"}), true
	case strings.Contains(text, "permission denied"):
		return quickHint("当前用户缺少执行或访问目标路径的权限。", []string{"ls -la", "id"}), true
	case strings.Contains(text, "no such file or directory") || strings.Contains(text, "cannot find the path"):
		return quickHint("路径或工作目录可能不正确，目标文件不存在。", []string{"pwd", "ls -la"}), true
	case strings.Contains(text, "address already in use") || strings.Contains(text, "port already in use"):
		return quickHint("端口已被其他进程占用。", []string{"lsof -i :<port>", "netstat -an | grep <port>"}), true
	case strings.Contains(text, "module not found") || strings.Contains(text, "cannot find module") ||
		strings.Contains(text, "cannot find package") || strings.Contains(text, "undefined reference"):
		return quickHint("依赖缺失或链接配置不完整。", []string{"go env GOPATH", "go mod tidy"}), true
	case strings.Contains(text, "context deadline exceeded") || strings.Contains(text, "connection refused"):
		return quickHint("外部服务或网络连接暂不可用。", []string{"ping 127.0.0.1", "curl -v <url>"}), true
	default:
		return diagnoseToolResult{}, false
	}
}

// quickHint 统一限制快速预判的置信度上限。
func quickHint(rootCause string, investigation []string) diagnoseToolResult {
	return diagnoseToolResult{
		Confidence:            diagnosisQuickMaxConfidence,
		RootCause:             rootCause,
		FixCommands:           []string{},
		InvestigationCommands: investigation,
	}
}
