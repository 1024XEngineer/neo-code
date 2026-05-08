package ptyproxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"
	"sync"
	"time"

	gatewayclient "neo-code/internal/gateway/client"
	"neo-code/internal/tools"
)

// diagRuntimeConfig 描述诊断循环在当前平台上的超时与渲染策略。
type diagRuntimeConfig struct {
	CallTimeout     time.Duration
	AutoCallTimeout time.Duration
	RenderInitial   func(output io.Writer, prepared preparedDiagnosisRequest, isAuto bool)
	RenderFinal     func(output io.Writer, content string, isError bool)
	RenderError     func(output io.Writer, message string)
	RenderCachedHit func(output io.Writer, isAuto bool)
}

const (
	diagnosisCursorSaveVT    = "\0337"
	diagnosisCursorRestoreVT = "\0338"
)

// withDiagnosisCursorGuard 在 Windows 下用 DECSC/DECRC 包裹诊断输出，避免异步写屏破坏 ConPTY 光标状态。
func withDiagnosisCursorGuard(output io.Writer, render func()) {
	if render == nil {
		return
	}
	if output == nil || runtime.GOOS != "windows" {
		render()
		return
	}
	_, _ = io.WriteString(output, diagnosisCursorSaveVT)
	defer func() {
		_, _ = io.WriteString(output, diagnosisCursorRestoreVT)
	}()
	render()
}

// renderInitialFeedback 以可配置方式输出诊断首响，未配置时回退到默认终端渲染。
func (config diagRuntimeConfig) renderInitialFeedback(output io.Writer, prepared preparedDiagnosisRequest, isAuto bool) {
	if config.RenderInitial != nil {
		config.RenderInitial(output, prepared, isAuto)
		return
	}
	renderDiagnosisInitialFeedback(output, prepared, isAuto)
}

// renderFinalDiagnosis 以可配置方式输出诊断最终结果，未配置时回退到默认终端渲染。
func (config diagRuntimeConfig) renderFinalDiagnosis(output io.Writer, content string, isError bool) {
	if config.RenderFinal != nil {
		config.RenderFinal(output, content, isError)
		return
	}
	renderDiagnosis(output, content, isError)
}

// renderErrorDiagnosis 以可配置方式输出诊断错误，未配置时回退到默认终端渲染。
func (config diagRuntimeConfig) renderErrorDiagnosis(output io.Writer, message string) {
	if config.RenderError != nil {
		config.RenderError(output, message)
		return
	}
	renderDiagnosis(output, strings.TrimSpace(message), true)
}

// renderCachedNotice 以可配置方式输出缓存命中提示，未配置时沿用默认文案。
func (config diagRuntimeConfig) renderCachedNotice(output io.Writer, isAuto bool) {
	if config.RenderCachedHit != nil {
		config.RenderCachedHit(output, isAuto)
		return
	}
	if !isAuto {
		writeProxyLine(output, "\n\033[36m[NeoCode Diagnosis]\033[0m using cached diagnosis result")
	}
}

// consumeDiagSignals 消费手动与自动诊断任务，复用统一诊断协调器避免重复请求。
func consumeDiagSignals(
	ctx context.Context,
	rpcClient *gatewayclient.GatewayRPCClient,
	_ <-chan gatewayclient.Notification,
	jobCh <-chan diagnoseJob,
	output io.Writer,
	buffer *UTF8RingBuffer,
	options ManualShellOptions,
	shellSessionID string,
	recentTriggerStore *diagnosisTriggerStore,
	autoState *autoRuntimeState,
	onAutoDiagnoseFailure func(error),
	coordinator *diagnosisCoordinator,
	config diagRuntimeConfig,
) {
	if config.CallTimeout <= 0 {
		config.CallTimeout = 90 * time.Second
	}
	if config.AutoCallTimeout <= 0 {
		config.AutoCallTimeout = config.CallTimeout
	}

	var autoWG sync.WaitGroup
	autoSlots := make(chan struct{}, 1)
	defer autoWG.Wait()

	for {
		select {
		case <-ctx.Done():
			return
		case job, ok := <-jobCh:
			if !ok {
				return
			}
			if job.IsAuto {
				if coordinator != nil {
					prepared, prepareErr := prepareDiagnoseRequest(buffer, options, shellSessionID, job.Trigger)
					if prepareErr == nil && coordinator.shouldDropAuto(prepared.Fingerprint) {
						continue
					}
				}
				select {
				case autoSlots <- struct{}{}:
				case <-ctx.Done():
					return
				default:
					continue
				}
				autoWG.Add(1)
				go func(autoJob diagnoseJob) {
					defer autoWG.Done()
					defer func() { <-autoSlots }()
					diagnoseErr := runSingleDiagnosisWithCoordinator(
						ctx,
						coordinator,
						rpcClient,
						nil,
						output,
						buffer,
						options,
						shellSessionID,
						autoJob.Trigger,
						true,
						autoState,
						config,
					)
					if diagnoseErr != nil && onAutoDiagnoseFailure != nil && shouldTerminateShellOnAutoDiagnoseError(diagnoseErr) {
						onAutoDiagnoseFailure(diagnoseErr)
					}
				}(job)
				continue
			}

			_ = runSingleDiagnosisWithCoordinator(
				ctx,
				coordinator,
				rpcClient,
				nil,
				output,
				buffer,
				options,
				shellSessionID,
				resolveManualDiagnoseTrigger(job.Trigger, recentTriggerStore),
				false,
				autoState,
				config,
			)
		}
	}
}

// shouldTerminateShellOnAutoDiagnoseError 判断自动诊断错误是否代表代理链路不可恢复。
func shouldTerminateShellOnAutoDiagnoseError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	if message == "" {
		return false
	}
	if strings.Contains(message, "context deadline exceeded") {
		return false
	}
	if strings.Contains(message, "rate limit") || strings.Contains(message, "rate_limited") {
		return false
	}
	if strings.Contains(message, "provider generate") || strings.Contains(message, "sdk stream error") {
		return false
	}
	if strings.Contains(message, "unauthorized") {
		return true
	}
	if strings.Contains(message, "transport error") ||
		strings.Contains(message, "connection refused") ||
		strings.Contains(message, "no such file") ||
		strings.Contains(message, "use of closed network connection") {
		return true
	}
	return false
}

// runSingleDiagnosisWithCoordinator 执行一次诊断，并统一渲染快速反馈与最终结果。
func runSingleDiagnosisWithCoordinator(
	ctx context.Context,
	coordinator *diagnosisCoordinator,
	rpcClient *gatewayclient.GatewayRPCClient,
	eventStream <-chan gatewayclient.Notification,
	output io.Writer,
	buffer *UTF8RingBuffer,
	options ManualShellOptions,
	shellSessionID string,
	trigger diagnoseTrigger,
	isAuto bool,
	autoState *autoRuntimeState,
	config diagRuntimeConfig,
) error {
	if output == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if autoState != nil && isAuto && !autoState.Enabled.Load() {
		return nil
	}

	if config.CallTimeout <= 0 {
		config.CallTimeout = 90 * time.Second
	}
	if config.AutoCallTimeout <= 0 {
		config.AutoCallTimeout = config.CallTimeout
	}

	prepared, prepareErr := prepareDiagnoseRequest(buffer, options, shellSessionID, trigger)
	if prepareErr != nil {
		if options.Stderr != nil {
			writeProxyf(options.Stderr, "neocode diag: build diagnose payload failed: %v\n", prepareErr)
		}
		err := errors.New("failed to build diagnosis payload")
		if !isAuto {
			config.renderErrorDiagnosis(output, err.Error())
		}
		return err
	}

	if coordinator != nil {
		if cached, ok := coordinator.cached(prepared.Fingerprint); ok {
			if cached.Err != nil {
				if !isAuto {
					config.renderErrorDiagnosis(output, cached.Err.Error())
				}
				return cached.Err
			}
			config.renderCachedNotice(output, isAuto)
			config.renderFinalDiagnosis(output, cached.Result.Content, cached.Result.IsError)
			return nil
		}
	}

	config.renderInitialFeedback(output, prepared, isAuto)
	timeout := config.CallTimeout
	if isAuto {
		timeout = config.AutoCallTimeout
	}
	execute := func() (tools.ToolResult, error) {
		result, runErr := executePreparedDiagnoseToolWithTimeout(
			rpcClient,
			eventStream,
			options,
			prepared,
			timeout,
		)
		return result, runErr
	}

	outcome := diagnosisOutcome{}
	if coordinator != nil {
		outcome = coordinator.run(ctx, prepared.Fingerprint, execute)
	} else {
		result, runErr := execute()
		outcome = diagnosisOutcome{Result: result, Err: runErr}
	}
	if outcome.Err != nil {
		if !isAuto {
			config.renderErrorDiagnosis(output, outcome.Err.Error())
		}
		return outcome.Err
	}
	if isAuto && autoState != nil && !autoState.Enabled.Load() {
		return nil
	}
	config.renderFinalDiagnosis(output, outcome.Result.Content, outcome.Result.IsError)
	return nil
}

// runSingleDiagnosis 在不启用 coordinator 时执行一次诊断，保持旧接口兼容。
func runSingleDiagnosis(
	rpcClient *gatewayclient.GatewayRPCClient,
	output io.Writer,
	buffer *UTF8RingBuffer,
	options ManualShellOptions,
	shellSessionID string,
	trigger diagnoseTrigger,
	isAuto bool,
	autoState *autoRuntimeState,
	config diagRuntimeConfig,
) error {
	return runSingleDiagnosisWithCoordinator(
		context.Background(),
		nil,
		rpcClient,
		nil,
		output,
		buffer,
		options,
		shellSessionID,
		trigger,
		isAuto,
		autoState,
		config,
	)
}

// callDiagnoseToolWithTimeout 构建并执行一次 diagnose 调用。
func callDiagnoseToolWithTimeout(
	rpcClient *gatewayclient.GatewayRPCClient,
	buffer *UTF8RingBuffer,
	options ManualShellOptions,
	shellSessionID string,
	trigger diagnoseTrigger,
	timeout time.Duration,
) (tools.ToolResult, error) {
	prepared, err := prepareDiagnoseRequest(buffer, options, shellSessionID, trigger)
	if err != nil {
		if options.Stderr != nil {
			writeProxyf(options.Stderr, "neocode diag: build diagnose payload failed: %v\n", err)
		}
		return tools.ToolResult{}, errors.New("failed to build diagnosis payload")
	}
	return executePreparedDiagnoseToolWithTimeout(rpcClient, nil, options, prepared, timeout)
}

// callDiagnoseTool 使用默认诊断超时执行 diagnose。
func callDiagnoseTool(
	rpcClient *gatewayclient.GatewayRPCClient,
	buffer *UTF8RingBuffer,
	options ManualShellOptions,
	shellSessionID string,
	trigger diagnoseTrigger,
	config diagRuntimeConfig,
) (tools.ToolResult, error) {
	if config.CallTimeout <= 0 {
		config.CallTimeout = 90 * time.Second
	}
	return callDiagnoseToolWithTimeout(rpcClient, buffer, options, shellSessionID, trigger, config.CallTimeout)
}

// renderDiagErrorLine 输出统一的诊断错误行。
func renderDiagErrorLine(output io.Writer, message string) {
	withDiagnosisCursorGuard(output, func() {
		writeProxyf(output, "\n\033[31m[NeoCode Diagnosis]\033[0m %s\n", strings.TrimSpace(message))
	})
}

// formatBindStreamError 统一封装 bind_stream 错误输出。
func formatBindStreamError(code, message string) error {
	return fmt.Errorf("gateway bind_stream failed (%s): %s", strings.TrimSpace(code), strings.TrimSpace(message))
}
