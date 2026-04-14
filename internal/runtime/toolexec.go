package runtime

import (
	"context"
	"errors"
	"strings"
	"sync"

	providertypes "neo-code/internal/provider/types"
)

// executeAssistantToolCalls 并发执行 assistant 返回的全部工具调用并回写结果。
func (s *Service) executeAssistantToolCalls(
	ctx context.Context,
	state *runState,
	snapshot turnSnapshot,
	assistant providertypes.Message,
) error {
	if len(assistant.ToolCalls) == 0 {
		return nil
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	semaphore := make(chan struct{}, resolveToolParallelism(len(assistant.ToolCalls)))
	toolLocks := buildToolExecutionLocks(assistant.ToolCalls)

	checkContext := func() bool {
		if err := ctx.Err(); err != nil {
			rememberFirstError(&mu, &firstErr, err)
			return true
		}
		return false
	}

	for _, call := range assistant.ToolCalls {
		if err := ctx.Err(); err != nil {
			return err
		}
		wg.Add(1)
		go func(call providertypes.ToolCall, toolLock *sync.Mutex) {
			defer wg.Done()
			if checkContext() {
				return
			}
			if err := acquireToolSlot(ctx, semaphore); err != nil {
				rememberFirstError(&mu, &firstErr, err)
				return
			}
			defer releaseToolSlot(semaphore)

			toolLock.Lock()
			defer toolLock.Unlock()

			s.emitRunScoped(ctx, EventToolStart, state, call)

			result, execErr := s.executeToolCallWithPermission(ctx, permissionExecutionInput{
				RunID:       state.runID,
				SessionID:   state.session.ID,
				Call:        call,
				Workdir:     snapshot.workdir,
				ToolTimeout: snapshot.toolTimeout,
			})

			if errors.Is(execErr, context.Canceled) {
				rememberFirstError(&mu, &firstErr, execErr)
				return
			}
			if execErr == nil && checkContext() {
				return
			}

			if execErr != nil && strings.TrimSpace(result.Content) == "" {
				result.Content = execErr.Error()
			}

			if err := s.appendToolMessageAndSave(ctx, state, call, result); err != nil {
				if execErr != nil && errors.Is(err, context.Canceled) {
					s.emitRunScoped(ctx, EventToolResult, state, result)
				}
				rememberFirstError(&mu, &firstErr, err)
				return
			}

			if execErr == nil && checkContext() {
				return
			}

			s.emitRunScoped(ctx, EventToolResult, state, result)

			if isSuccessfulRememberToolCall(call.Name, result, execErr) {
				state.mu.Lock()
				state.rememberedThisRun = true
				state.mu.Unlock()
			}

			if execErr != nil && checkContext() {
				return
			}
		}(call, toolLocks[normalizeToolLockKey(call.Name)])
	}

	wg.Wait()
	return firstErr
}

// resolveToolParallelism 计算本轮工具执行的并发上限，避免无界 goroutine 扩散。
func resolveToolParallelism(toolCallCount int) int {
	if toolCallCount <= 0 {
		return 1
	}
	if toolCallCount < defaultToolParallelism {
		return toolCallCount
	}
	return defaultToolParallelism
}

// buildToolExecutionLocks 按工具名构造互斥锁，确保同名工具调用在单轮内串行执行。
func buildToolExecutionLocks(calls []providertypes.ToolCall) map[string]*sync.Mutex {
	locks := make(map[string]*sync.Mutex, len(calls))
	for _, call := range calls {
		key := normalizeToolLockKey(call.Name)
		if _, exists := locks[key]; !exists {
			locks[key] = &sync.Mutex{}
		}
	}
	return locks
}

// normalizeToolLockKey 将工具名规范化为锁键，防止大小写差异导致重复并发执行。
func normalizeToolLockKey(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// acquireToolSlot 获取一个并发槽位，并在上下文取消时尽快返回。
func acquireToolSlot(ctx context.Context, semaphore chan struct{}) error {
	select {
	case semaphore <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// releaseToolSlot 释放工具执行并发槽位。
func releaseToolSlot(semaphore chan struct{}) {
	<-semaphore
}

// rememberFirstError 记录首次错误，后续错误只保留用于日志和事件路径。
func rememberFirstError(mu *sync.Mutex, firstErr *error, err error) {
	if err == nil {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	if *firstErr == nil {
		*firstErr = err
	}
}
