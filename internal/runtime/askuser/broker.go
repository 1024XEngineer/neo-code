package askuser

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	defaultRequestTimeout = 5 * time.Minute
	maxRequestTimeout     = time.Hour
)

// newRequestID generates an unguessable request ID using crypto/rand.
func newRequestID() string {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	return "ask-" + hex.EncodeToString(buf)
}

// pendingRequest 代表一个等待用户响应的 ask_user 请求。
type pendingRequest struct {
	resultCh  chan Result
	submitted bool
}

// Broker 负责管理 ask_user 请求的挂起与恢复生命周期。
type Broker struct {
	mu      sync.Mutex
	pending map[string]*pendingRequest
}

// NewBroker 创建一个新的 AskUserBroker。
func NewBroker() *Broker {
	return &Broker{
		pending: make(map[string]*pendingRequest),
	}
}

// Open 注册一个新的待处理 ask_user 请求，并阻塞等待结果、超时或 ctx 取消。
// 返回 (requestID, Result, error)。
func (b *Broker) Open(ctx context.Context, request Request) (string, Result, error) {
	if b == nil {
		return "", Result{}, errors.New("askuser: broker is nil")
	}

	b.mu.Lock()
	requestID := newRequestID()
	pr := &pendingRequest{
		resultCh: make(chan Result, 1),
	}
	b.pending[requestID] = pr
	b.mu.Unlock()

	defer b.Close(requestID)

	timeout := TimeoutForRequest(request)
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return requestID, Result{
			Status:     StatusTimeout,
			QuestionID: request.QuestionID,
			Message:    "context cancelled",
		}, ctx.Err()
	case <-timer.C:
		return requestID, Result{
			Status:     StatusTimeout,
			QuestionID: request.QuestionID,
			Message:    "request timed out",
		}, fmt.Errorf("askuser: request %q timed out", requestID)
	case result := <-pr.resultCh:
		result.QuestionID = request.QuestionID
		return requestID, result, nil
	}
}

// Resolve 向指定请求提交答案；重复提交被安全忽略。
func (b *Broker) Resolve(requestID string, result Result) error {
	if b == nil {
		return errors.New("askuser: broker is nil")
	}

	b.mu.Lock()
	pr := b.pending[requestID]
	if pr == nil {
		b.mu.Unlock()
		return fmt.Errorf("askuser: request %q not found", requestID)
	}
	if pr.submitted {
		b.mu.Unlock()
		return nil
	}
	pr.submitted = true
	resultCh := pr.resultCh
	b.mu.Unlock()

	select {
	case resultCh <- result:
		return nil
	default:
		return nil
	}
}

// Close 清理指定请求。
func (b *Broker) Close(requestID string) {
	if b == nil {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.pending, requestID)
}

// PendingIDs returns a copy of currently pending request IDs.
func (b *Broker) PendingIDs() []string {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	ids := make([]string, 0, len(b.pending))
	for id := range b.pending {
		ids = append(ids, id)
	}
	return ids
}

// TimeoutForRequest 根据请求配置返回有效超时。
func TimeoutForRequest(req Request) time.Duration {
	if req.TimeoutSec > 0 {
		d := time.Duration(req.TimeoutSec) * time.Second
		if d > maxRequestTimeout {
			return maxRequestTimeout
		}
		return d
	}
	return defaultRequestTimeout
}
