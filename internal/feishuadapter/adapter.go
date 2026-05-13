package feishuadapter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"neo-code/internal/gateway/protocol"
)

const defaultSignatureMaxSkew = 5 * time.Minute
const defaultProgressNotifyInterval = 2 * time.Second
const defaultCardRefreshInterval = 1500 * time.Millisecond
const defaultRunStallTimeout = 3 * time.Minute
const defaultPermissionCardDismissDelay = 2 * time.Second

type approvalEntry struct {
	RequestID string
	ToolName  string
	Reason    string
	Decision  string // "pending", "allow_once", "reject"
}

type userQuestionEntry struct {
	RequestID   string
	QuestionID  string
	Title       string
	Description string
	Kind        string
	Options     []UserQuestionCardOption
	AllowSkip   bool
	MaxChoices  int
}

type sessionBinding struct {
	SessionID       string
	ChatID          string
	RunID           string
	CardID          string
	TaskName        string
	Status          string
	ApprovalStatus  string
	ApprovalRecords []approvalEntry
	Result          string
	LastSummary     string
	AsyncRewakeHint string
	RunStartTime    time.Time
	LastEventTime   time.Time
}

// Adapter 负责桥接飞书回调与 Gateway JSON-RPC 长连接。
type Adapter struct {
	cfg       Config
	gateway   GatewayClient
	messenger Messenger
	logger    *log.Logger
	idem      *idempotencyStore

	nowFn func() time.Time

	mu                 sync.RWMutex
	activeRuns         map[string]sessionBinding
	sessionChats       map[string]string
	requestRuns        map[string]string
	lastProgressAt     map[string]time.Time
	permissionCards    map[string]string // requestID -> card message_id
	runPermissionCards map[string]string // runKey -> card message_id
	// resolvedPermissions 记录已完成审批的 request_id，避免重复事件将状态回滚为 pending。
	resolvedPermissions map[string]string // requestID -> decision
	userQuestionCards   map[string]string // requestID -> card message_id
	pendingQuestions    map[string]userQuestionEntry

	permissionCardDismissDelay time.Duration
}

// New 创建飞书适配器实例。
func New(cfg Config, gateway GatewayClient, messenger Messenger, logger *log.Logger) (*Adapter, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if gateway == nil {
		return nil, fmt.Errorf("gateway client is required")
	}
	if messenger == nil {
		return nil, fmt.Errorf("messenger is required")
	}
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	return &Adapter{
		cfg:                        cfg,
		gateway:                    gateway,
		messenger:                  messenger,
		logger:                     logger,
		idem:                       newIdempotencyStore(cfg.IdempotencyTTL),
		nowFn:                      func() time.Time { return time.Now().UTC() },
		activeRuns:                 make(map[string]sessionBinding),
		sessionChats:               make(map[string]string),
		requestRuns:                make(map[string]string),
		lastProgressAt:             make(map[string]time.Time),
		permissionCards:            make(map[string]string),
		runPermissionCards:         make(map[string]string),
		resolvedPermissions:        make(map[string]string),
		userQuestionCards:          make(map[string]string),
		pendingQuestions:           make(map[string]userQuestionEntry),
		permissionCardDismissDelay: defaultPermissionCardDismissDelay,
	}, nil
}

// Run 启动飞书适配器 HTTP 服务与网关事件消费循环。
func (a *Adapter) Run(ctx context.Context) error {
	if err := a.gateway.Authenticate(ctx); err != nil {
		return fmt.Errorf("authenticate gateway: %w", err)
	}

	go a.consumeGatewayEvents(ctx)
	go a.reconnectAndRebindLoop(ctx)
	go a.refreshActiveCardsLoop(ctx)
	ingress := a.buildIngress()
	err := ingress.Run(ctx, a)
	_ = a.gateway.Close()
	if err != nil && err != context.Canceled {
		return err
	}
	return nil
}

// buildIngress 根据配置模式构建飞书事件入站实现。
func (a *Adapter) buildIngress() Ingress {
	switch normalizeIngressMode(a.cfg.IngressMode) {
	case IngressModeSDK:
		return NewSDKIngress(a.cfg, a.safeLog)
	default:
		return NewWebhookIngress(a.cfg, a.nowFn)
	}
}

// handleFeishuEvent 保留给现有测试使用，实际逻辑委托给 WebhookIngress。
func (a *Adapter) handleFeishuEvent(writer http.ResponseWriter, request *http.Request) {
	ingress := NewWebhookIngress(a.cfg, a.nowFn)
	webhook, ok := ingress.(*WebhookIngress)
	if !ok {
		http.Error(writer, "ingress unavailable", http.StatusInternalServerError)
		return
	}
	webhook.handleFeishuEvent(a)(writer, request)
}

// handleCardCallback 保留给现有测试使用，实际逻辑委托给 WebhookIngress。
func (a *Adapter) handleCardCallback(writer http.ResponseWriter, request *http.Request) {
	ingress := NewWebhookIngress(a.cfg, a.nowFn)
	webhook, ok := ingress.(*WebhookIngress)
	if !ok {
		http.Error(writer, "ingress unavailable", http.StatusInternalServerError)
		return
	}
	webhook.handleCardCallback(a)(writer, request)
}

// HandleMessage 处理标准化后的飞书消息事件，并复用统一的网关执行链路。
func (a *Adapter) HandleMessage(ctx context.Context, event FeishuMessageEvent) error {
	chatType := strings.TrimSpace(strings.ToLower(event.ChatType))
	if chatType == "" {
		chatType = "p2p"
	}
	a.safeLog(
		"feishu message received event_id=%s message_id=%s chat_id=%s chat_type=%s mentions=%d",
		strings.TrimSpace(event.EventID),
		strings.TrimSpace(event.MessageID),
		strings.TrimSpace(event.ChatID),
		chatType,
		len(event.Mentions),
	)
	if strings.TrimSpace(event.MessageID) == "" || strings.TrimSpace(event.ChatID) == "" {
		a.safeLog("feishu message rejected: missing message_id or chat_id")
		return fmt.Errorf("missing message_id or chat_id")
	}
	dedupeKey := "msg:" + strings.TrimSpace(event.EventID) + ":" + strings.TrimSpace(event.MessageID)
	if !a.idem.TryStart(dedupeKey, a.nowFn()) {
		a.safeLog("feishu message skipped by idempotency dedupe_key=%s", dedupeKey)
		return nil
	}
	succeeded := false
	defer func() {
		if succeeded {
			a.idem.MarkDone(dedupeKey, a.nowFn())
			return
		}
		a.idem.MarkFailed(dedupeKey)
	}()

	text := strings.TrimSpace(event.ContentText)
	if text == "" {
		a.safeLog("feishu message ignored: empty text content message_id=%s", strings.TrimSpace(event.MessageID))
		return nil
	}
	if handled, err := a.tryHandleTextAction(ctx, event.ChatID, text); handled {
		a.safeLog("feishu text action handled chat_id=%s err=%v", strings.TrimSpace(event.ChatID), err)
		if err == nil {
			succeeded = true
		}
		return err
	}

	sessionID := BuildSessionID(event.ChatID)
	runID := BuildRunID(event.MessageID)
	a.safeLog("feishu message dispatching run session_id=%s run_id=%s chat_id=%s", sessionID, runID, strings.TrimSpace(event.ChatID))
	if err := a.bindThenRun(ctx, sessionID, runID, event.ChatID, text); err != nil {
		a.safeLog("handle message failed: %v", err)
		_ = a.messenger.SendText(context.Background(), event.ChatID, "任务受理失败，请稍后重试。")
		return err
	}
	a.safeLog("feishu message accepted session_id=%s run_id=%s", sessionID, runID)
	succeeded = true
	return nil
}

// HandleCardAction 处理标准化后的审批动作事件并映射到网关授权接口。
func (a *Adapter) HandleCardAction(ctx context.Context, event FeishuCardActionEvent) error {
	requestID := strings.TrimSpace(event.RequestID)
	if requestID == "" {
		return nil
	}
	actionType := strings.TrimSpace(strings.ToLower(event.ActionType))
	if actionType == "" {
		if decision := strings.TrimSpace(strings.ToLower(event.Decision)); decision != "" {
			actionType = "permission"
		} else {
			actionType = "user_question"
		}
	}

	dedupeKey := buildCardActionDedupeKey(event, actionType)
	if !a.idem.TryStart(dedupeKey, a.nowFn()) {
		return nil
	}
	succeeded := false
	defer func() {
		if succeeded {
			a.idem.MarkDone(dedupeKey, a.nowFn())
			return
		}
		a.idem.MarkFailed(dedupeKey)
	}()

	callCtx, cancel := context.WithTimeout(ctx, a.cfg.RequestTimeout)
	defer cancel()
	switch actionType {
	case "permission":
		decision := strings.TrimSpace(strings.ToLower(event.Decision))
		if decision != "allow_once" && decision != "reject" {
			return nil
		}
		if err := a.gateway.ResolvePermission(callCtx, requestID, decision); err != nil {
			a.safeLog("resolve permission failed: %v", err)
			return err
		}
		a.updateApprovalStatus(requestID, decision)
	case "user_question":
		status := strings.TrimSpace(strings.ToLower(event.Status))
		if status == "" {
			status = "answered"
		}
		if status != "answered" && status != "skipped" {
			return nil
		}
		values := append([]string(nil), event.Values...)
		message := strings.TrimSpace(event.Message)
		if err := a.gateway.ResolveUserQuestion(callCtx, requestID, status, values, message); err != nil {
			a.safeLog("resolve user question failed: %v", err)
			return err
		}
		a.updateUserQuestionStatus(requestID, status, values, message)
	default:
		return nil
	}
	succeeded = true
	return nil
}

// bindThenRun 按 authenticate -> bindStream -> run 的顺序提交一次请求并记录会话绑定。
func (a *Adapter) bindThenRun(ctx context.Context, sessionID string, runID string, chatID string, text string) error {
	callCtx, cancel := context.WithTimeout(ctx, a.cfg.RequestTimeout)
	defer cancel()
	a.safeLog("bindThenRun authenticate start session_id=%s run_id=%s", sessionID, runID)
	if err := a.gateway.Authenticate(callCtx); err != nil {
		a.safeLog("bindThenRun authenticate failed session_id=%s run_id=%s err=%v", sessionID, runID, err)
		return err
	}
	a.safeLog("bindThenRun bind stream start session_id=%s run_id=%s", sessionID, runID)
	if err := a.gateway.BindStream(callCtx, sessionID, runID); err != nil {
		a.safeLog("bindThenRun bind stream failed session_id=%s run_id=%s err=%v", sessionID, runID, err)
		return err
	}
	a.trackSession(sessionID, runID, chatID, text)
	a.safeLog("bindThenRun gateway run start session_id=%s run_id=%s", sessionID, runID)
	if err := a.gateway.Run(callCtx, sessionID, runID, text); err != nil {
		// run 受理失败时及时回收活跃绑定，避免重连阶段反复重绑无效 run。
		a.untrackRun(sessionID, runID)
		a.safeLog("bindThenRun gateway run failed session_id=%s run_id=%s err=%v", sessionID, runID, err)
		return err
	}
	a.safeLog("bindThenRun gateway run accepted session_id=%s run_id=%s", sessionID, runID)
	if err := a.ensureRunCard(context.Background(), sessionID, runID); err != nil {
		a.safeLog("send status card failed: %v", err)
		_ = a.messenger.SendText(context.Background(), chatID, "任务已受理，正在执行。")
	}
	return nil
}

// trackSession 记录 session 到飞书 chat 的映射，用于事件回推。
func (a *Adapter) trackSession(sessionID string, runID string, chatID string, taskName string) {
	now := a.nowFn()
	a.mu.Lock()
	defer a.mu.Unlock()
	key := runBindingKey(sessionID, runID)
	a.activeRuns[key] = sessionBinding{
		SessionID:      sessionID,
		ChatID:         chatID,
		RunID:          runID,
		TaskName:       buildTaskName(taskName),
		Status:         "thinking",
		ApprovalStatus: "none",
		Result:         "pending",
		RunStartTime:   now,
		LastEventTime:  now,
	}
	if sessionID != "" && chatID != "" {
		a.sessionChats[sessionID] = chatID
	}
}

// untrackRun 在 run 终态事件到达后移除活跃 run 绑定，避免重连重绑与内存累积。
func (a *Adapter) untrackRun(sessionID string, runID string) {
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(runID) == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	key := runBindingKey(sessionID, runID)
	if binding, ok := a.activeRuns[key]; ok {
		for requestID, requestRunKey := range a.requestRuns {
			if requestRunKey == key {
				delete(a.requestRuns, requestID)
				delete(a.permissionCards, requestID)
				delete(a.userQuestionCards, requestID)
				delete(a.pendingQuestions, requestID)
			}
		}
		delete(a.runPermissionCards, key)
		delete(a.lastProgressAt, key)
		_ = binding
	}
	delete(a.activeRuns, key)
}

// lookupChatID 根据 session_id 查找需要回推的飞书 chat_id。
func (a *Adapter) lookupChatID(sessionID string, runID string) string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if sessionID != "" && runID != "" {
		if binding, ok := a.activeRuns[runBindingKey(sessionID, runID)]; ok {
			return binding.ChatID
		}
	}
	return a.sessionChats[sessionID]
}

// consumeGatewayEvents 持续消费网关通知流并转发到飞书侧展示。
func (a *Adapter) consumeGatewayEvents(ctx context.Context) {
	notifications := a.gateway.Notifications()
	for {
		select {
		case <-ctx.Done():
			return
		case notification, ok := <-notifications:
			if !ok {
				return
			}
			if strings.TrimSpace(notification.Method) != protocol.MethodGatewayEvent {
				continue
			}
			a.handleGatewayEvent(ctx, notification.Params)
		}
	}
}

// handleGatewayEvent 将 gateway.event 映射成飞书文本或审批卡片。
func (a *Adapter) handleGatewayEvent(ctx context.Context, raw json.RawMessage) {
	eventType, sessionID, runID, envelope, err := parseGatewayRuntimeEvent(raw)
	if err != nil {
		a.safeLog("decode gateway event failed: %v", err)
		return
	}
	a.safeLog("gateway event received type=%s session_id=%s run_id=%s", eventType, sessionID, runID)
	chatID := a.lookupChatID(sessionID, runID)
	if chatID == "" {
		a.safeLog("gateway event ignored: no chat binding type=%s session_id=%s run_id=%s", eventType, sessionID, runID)
		return
	}
	switch strings.ToLower(strings.TrimSpace(eventType)) {
	case "run_progress":
		a.touchRunEvent(sessionID, runID)
		if envelope != nil {
			if runtimeType := readString(envelope, "runtime_event_type"); runtimeType != "" {
				if strings.EqualFold(runtimeType, "permission_requested") {
					requestID, toolName, operation, target, reason := extractPermissionRequest(envelope)
					if requestID != "" {
						if a.markPermissionPending(sessionID, runID, requestID, toolName, reason) {
							a.upsertPermissionCard(
								ctx,
								sessionID,
								runID,
								chatID,
								PermissionCardPayload{
									RequestID: requestID,
									ToolName:  toolName,
									Operation: operation,
									Target:    target,
									Message:   reason,
								},
							)
						}
						return
					}
				} else if strings.EqualFold(runtimeType, "permission_resolved") {
					requestID, resolvedDecision := extractPermissionResolved(envelope)
					if requestID != "" && resolvedDecision != "" {
						a.safeLog("permission resolved event request_id=%s decision=%s", requestID, resolvedDecision)
						a.updateApprovalStatus(requestID, resolvedDecision)
					}
				} else if strings.EqualFold(runtimeType, "user_question_requested") {
					question := extractUserQuestionRequest(envelope)
					if question.RequestID != "" {
						if !a.markUserQuestionPending(sessionID, runID, question) {
							return
						}
						if shouldSendAskUserCard(question) {
							cardID, err := a.messenger.SendUserQuestionCard(ctx, chatID, UserQuestionCardPayload{
								RequestID:   question.RequestID,
								QuestionID:  question.QuestionID,
								Title:       question.Title,
								Description: question.Description,
								Kind:        question.Kind,
								Options:     append([]UserQuestionCardOption(nil), question.Options...),
								AllowSkip:   question.AllowSkip,
							})
							if err == nil && strings.TrimSpace(cardID) != "" {
								a.mu.Lock()
								a.userQuestionCards[question.RequestID] = cardID
								a.mu.Unlock()
							}
						} else {
							_ = a.messenger.SendText(ctx, chatID, buildAskUserTextPrompt(question))
						}
						return
					}
				} else if isUserQuestionResolvedEvent(runtimeType) {
					resolved := extractUserQuestionResolved(envelope)
					if resolved.Status == "" {
						resolved.Status = userQuestionStatusFromRuntimeType(runtimeType)
					}
					if resolved.RequestID != "" {
						a.updateUserQuestionStatus(resolved.RequestID, resolved.Status, resolved.Values, resolved.Message)
					}
				}
				a.handleRunProgressCard(ctx, sessionID, runID, runtimeType, envelope)
			}
		}
		// 除审批请求外，内部 runtime_event_type 不直接透出到飞书用户视图，避免暴露控制面细节。
		return
	case "run_done":
		a.touchRunEvent(sessionID, runID)
		a.markRunTerminal(sessionID, runID, "success", extractSummaryText(envelope), "")
		a.untrackRun(sessionID, runID)
	case "run_error":
		a.touchRunEvent(sessionID, runID)
		a.markRunTerminal(sessionID, runID, "failure", "", extractUserVisibleErrorText(envelope))
		a.untrackRun(sessionID, runID)
	}
}

// reconnectAndRebindLoop 定期保活网关连接，并在重连后重绑活跃会话。
func (a *Adapter) reconnectAndRebindLoop(ctx context.Context) {
	delay := a.cfg.ReconnectBackoffMin
	ticker := time.NewTicker(a.cfg.RebindInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			callCtx, cancel := context.WithTimeout(ctx, a.cfg.RequestTimeout)
			err := a.gateway.Ping(callCtx)
			cancel()
			if err == nil {
				delay = a.cfg.ReconnectBackoffMin
				continue
			}
			a.safeLog("gateway ping failed, will reconnect: %v", err)
			if !a.retryAuthenticateAndRebind(ctx, delay) {
				return
			}
			delay = nextBackoff(delay, a.cfg.ReconnectBackoffMax)
		}
	}
}

// retryAuthenticateAndRebind 在连接异常后执行一次认证重试与会话重绑。
func (a *Adapter) retryAuthenticateAndRebind(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delayWithJitter(delay))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
	}
	callCtx, cancel := context.WithTimeout(ctx, a.cfg.RequestTimeout)
	defer cancel()
	if err := a.gateway.Authenticate(callCtx); err != nil {
		a.safeLog("gateway re-authenticate failed: %v", err)
		return true
	}
	a.rebindActiveSessions(callCtx)
	return true
}

// rebindActiveSessions 对当前活跃会话重新执行 bindStream，恢复事件订阅关系。
func (a *Adapter) rebindActiveSessions(ctx context.Context) {
	a.mu.RLock()
	snapshot := make([]sessionBinding, 0, len(a.activeRuns))
	for _, binding := range a.activeRuns {
		snapshot = append(snapshot, binding)
	}
	a.mu.RUnlock()

	for _, binding := range snapshot {
		callCtx, cancel := context.WithTimeout(ctx, a.cfg.RequestTimeout)
		err := a.gateway.BindStream(callCtx, binding.SessionID, binding.RunID)
		cancel()
		if err != nil {
			a.safeLog("rebind session failed session_id=%s run_id=%s err=%v", binding.SessionID, binding.RunID, err)
		}
	}
}

// refreshActiveCardsLoop 定时刷新活跃 run 的状态卡片，保持 1.5s 刷新频率以展示实时耗时。
func (a *Adapter) refreshActiveCardsLoop(ctx context.Context) {
	ticker := time.NewTicker(defaultCardRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.refreshActiveCards(ctx)
		}
	}
}

// refreshActiveCards 对当前所有活跃 run 更新卡片，仅刷新耗时字段变化。
// 说明：超过静默阈值仅做告警，不在适配器侧强行终止 run，避免误伤长耗时但无中间事件的合法执行。
func (a *Adapter) refreshActiveCards(ctx context.Context) {
	now := a.nowFn()
	staleRuns := make([]sessionBinding, 0)
	snapshot := make([]sessionBinding, 0)
	a.mu.RLock()
	for _, binding := range a.activeRuns {
		if shouldMarkRunStalled(binding, now) {
			staleRuns = append(staleRuns, binding)
		}
		if strings.TrimSpace(binding.CardID) != "" {
			snapshot = append(snapshot, binding)
		}
	}
	a.mu.RUnlock()

	for _, stale := range staleRuns {
		a.safeLog(
			"run stalled: no force-fail, waiting for terminal event session_id=%s run_id=%s idle_for=%s",
			stale.SessionID,
			stale.RunID,
			now.Sub(stale.LastEventTime).String(),
		)
	}

	for _, binding := range snapshot {
		callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		if err := a.messenger.UpdateCard(callCtx, binding.CardID, binding.statusCardPayload()); err != nil {
			a.safeLog("refresh card failed card_id=%s err=%v", binding.CardID, err)
		}
		cancel()
	}
}

// touchRunEvent 记录 run 的最近事件时间，用于识别“长时间无事件”的僵尸运行态。
func (a *Adapter) touchRunEvent(sessionID string, runID string) {
	key := runBindingKey(sessionID, runID)
	a.mu.Lock()
	defer a.mu.Unlock()
	binding, ok := a.activeRuns[key]
	if !ok {
		return
	}
	binding.LastEventTime = a.nowFn()
	a.activeRuns[key] = binding
}

// ensureRunCard 为新受理的 run 发送单独状态卡片，集中展示执行状态与审批结果。
func (a *Adapter) ensureRunCard(ctx context.Context, sessionID string, runID string) error {
	a.mu.RLock()
	binding, ok := a.activeRuns[runBindingKey(sessionID, runID)]
	a.mu.RUnlock()
	if !ok || strings.TrimSpace(binding.ChatID) == "" {
		a.safeLog("ensureRunCard skipped: no binding or empty chat session_id=%s run_id=%s", sessionID, runID)
		return nil
	}
	if strings.TrimSpace(binding.CardID) != "" {
		a.safeLog("ensureRunCard updating existing card session_id=%s run_id=%s card_id=%s", sessionID, runID, strings.TrimSpace(binding.CardID))
		return a.messenger.UpdateCard(ctx, binding.CardID, binding.statusCardPayload())
	}
	a.safeLog("ensureRunCard creating status card session_id=%s run_id=%s chat_id=%s", sessionID, runID, strings.TrimSpace(binding.ChatID))
	cardID, err := a.messenger.SendStatusCard(ctx, binding.ChatID, binding.statusCardPayload())
	if err != nil {
		a.safeLog("ensureRunCard create failed session_id=%s run_id=%s err=%v", sessionID, runID, err)
		return err
	}
	if strings.TrimSpace(cardID) == "" {
		a.safeLog("ensureRunCard create returned empty card_id session_id=%s run_id=%s", sessionID, runID)
		return fmt.Errorf("create status card returned empty card id")
	}
	a.safeLog("ensureRunCard created status card session_id=%s run_id=%s card_id=%s", sessionID, runID, strings.TrimSpace(cardID))
	a.mu.Lock()
	defer a.mu.Unlock()
	current := a.activeRuns[runBindingKey(sessionID, runID)]
	current.CardID = cardID
	a.activeRuns[runBindingKey(sessionID, runID)] = current
	return nil
}

// handleRunProgressCard 将 runtime 进度事件压缩为卡片状态更新，避免连续文本刷屏。
func (a *Adapter) handleRunProgressCard(ctx context.Context, sessionID string, runID string, runtimeType string, envelope map[string]any) {
	key := runBindingKey(sessionID, runID)
	a.mu.Lock()
	binding, ok := a.activeRuns[key]
	if !ok {
		a.mu.Unlock()
		return
	}
	updated := binding
	updated.Status = deriveRunStatus(runtimeType, envelope, binding.Status)
	if strings.EqualFold(runtimeType, "hook_notification") {
		updated.LastSummary = extractHookNotificationSummary(envelope)
		updated.AsyncRewakeHint = extractHookNotificationHint(envelope)
	}
	changed := updated.Status != binding.Status ||
		updated.LastSummary != binding.LastSummary ||
		updated.AsyncRewakeHint != binding.AsyncRewakeHint
	cardID := strings.TrimSpace(binding.CardID)
	a.activeRuns[key] = updated
	a.mu.Unlock()
	if !changed || cardID == "" {
		if changed && cardID == "" {
			a.safeLog("handleRunProgressCard skipped update: empty card_id session_id=%s run_id=%s runtime_type=%s", sessionID, runID, runtimeType)
		}
		return
	}
	if err := a.messenger.UpdateCard(ctx, cardID, updated.statusCardPayload()); err != nil {
		a.safeLog("update status card failed: %v", err)
	}
}

// markPermissionPending 将权限请求映射到 run 卡片，返回当前事件是否应继续刷新审批交互卡片。
func (a *Adapter) markPermissionPending(sessionID string, runID string, requestID string, toolName string, reason string) bool {
	normalizedRequestID := strings.TrimSpace(requestID)
	if normalizedRequestID == "" {
		return false
	}
	key := runBindingKey(sessionID, runID)
	a.mu.Lock()
	binding, ok := a.activeRuns[key]
	if !ok {
		a.mu.Unlock()
		return false
	}
	if _, resolved := a.resolvedPermissions[normalizedRequestID]; resolved {
		a.mu.Unlock()
		return false
	}

	alreadyPending := false
	binding.ApprovalStatus = "pending"
	found := false
	for i := range binding.ApprovalRecords {
		if binding.ApprovalRecords[i].RequestID != normalizedRequestID {
			continue
		}
		found = true
		alreadyPending = isApprovalPendingDecision(binding.ApprovalRecords[i].Decision)
		if strings.TrimSpace(binding.ApprovalRecords[i].ToolName) == "" && strings.TrimSpace(toolName) != "" {
			binding.ApprovalRecords[i].ToolName = strings.TrimSpace(toolName)
		}
		if strings.TrimSpace(reason) != "" {
			binding.ApprovalRecords[i].Reason = strings.TrimSpace(reason)
		}
		break
	}
	if !found {
		binding.ApprovalRecords = append(binding.ApprovalRecords, approvalEntry{
			RequestID: normalizedRequestID,
			ToolName:  strings.TrimSpace(toolName),
			Reason:    strings.TrimSpace(reason),
			Decision:  "pending",
		})
	}
	if strings.TrimSpace(reason) != "" {
		binding.LastSummary = strings.TrimSpace(reason)
	}
	a.activeRuns[key] = binding
	a.requestRuns[normalizedRequestID] = key

	cardID := ""
	payload := StatusCardPayload{}
	cardID = strings.TrimSpace(binding.CardID)
	payload = binding.statusCardPayload()
	a.mu.Unlock()
	if cardID != "" {
		if err := a.messenger.UpdateCard(context.Background(), cardID, payload); err != nil {
			a.safeLog("update pending approval card failed: %v", err)
		}
	}
	return !(found && alreadyPending)
}

// upsertPermissionCard 在同一 run 内复用一张审批卡片，后续审批请求覆盖刷新该卡片。
func (a *Adapter) upsertPermissionCard(
	ctx context.Context,
	sessionID string,
	runID string,
	chatID string,
	payload PermissionCardPayload,
) {
	key := runBindingKey(sessionID, runID)
	normalizedRequestID := strings.TrimSpace(payload.RequestID)
	if key == "|" || normalizedRequestID == "" {
		return
	}

	a.mu.RLock()
	existingCardID := strings.TrimSpace(a.runPermissionCards[key])
	a.mu.RUnlock()

	if existingCardID == "" {
		cardID, err := a.messenger.SendPermissionCard(ctx, chatID, payload)
		if err != nil {
			a.safeLog("permission card create failed request_id=%s err=%v", normalizedRequestID, err)
			return
		}
		cardID = strings.TrimSpace(cardID)
		if cardID == "" {
			return
		}
		a.mu.Lock()
		a.runPermissionCards[key] = cardID
		a.permissionCards[normalizedRequestID] = cardID
		a.mu.Unlock()
		a.safeLog("permission card created request_id=%s card_id=%s run_key=%s", normalizedRequestID, cardID, key)
		return
	}

	a.safeLog("permission card update start request_id=%s card_id=%s", normalizedRequestID, existingCardID)
	if err := a.messenger.UpdatePendingPermissionCard(ctx, existingCardID, payload); err != nil {
		a.safeLog("permission card update failed request_id=%s card_id=%s err=%v", normalizedRequestID, existingCardID, err)
		return
	}
	a.mu.Lock()
	a.permissionCards[normalizedRequestID] = existingCardID
	a.mu.Unlock()
	a.safeLog("permission card update done request_id=%s card_id=%s", normalizedRequestID, existingCardID)
}

// markUserQuestionPending 记录 ask_user 待回答问题，并挂接到 run 状态卡上下文。
func (a *Adapter) markUserQuestionPending(sessionID string, runID string, question userQuestionEntry) bool {
	requestID := strings.TrimSpace(question.RequestID)
	if requestID == "" {
		return false
	}
	key := runBindingKey(sessionID, runID)
	a.mu.Lock()
	if _, exists := a.pendingQuestions[requestID]; exists {
		a.mu.Unlock()
		return false
	}
	if binding, ok := a.activeRuns[key]; ok {
		summary := strings.TrimSpace(question.Title)
		if summary == "" {
			summary = strings.TrimSpace(question.Description)
		}
		if summary != "" {
			binding.LastSummary = "等待用户回答：" + summary
			a.activeRuns[key] = binding
		}
	}
	a.requestRuns[requestID] = key
	a.pendingQuestions[requestID] = userQuestionEntry{
		RequestID:   requestID,
		QuestionID:  strings.TrimSpace(question.QuestionID),
		Title:       strings.TrimSpace(question.Title),
		Description: strings.TrimSpace(question.Description),
		Kind:        strings.TrimSpace(strings.ToLower(question.Kind)),
		Options:     append([]UserQuestionCardOption(nil), question.Options...),
		AllowSkip:   question.AllowSkip,
		MaxChoices:  question.MaxChoices,
	}
	a.mu.Unlock()
	return true
}

// updateUserQuestionStatus 在 ask_user 提交后更新状态卡摘要，并将提问卡片更新为已处理态。
func (a *Adapter) updateUserQuestionStatus(requestID string, status string, values []string, message string) {
	normalizedRequestID := strings.TrimSpace(requestID)
	if normalizedRequestID == "" {
		return
	}
	normalizedStatus := strings.TrimSpace(strings.ToLower(status))
	if normalizedStatus == "" {
		normalizedStatus = "answered"
	}

	a.mu.Lock()
	key := a.requestRuns[normalizedRequestID]
	binding, ok := a.activeRuns[key]
	question := a.pendingQuestions[normalizedRequestID]
	if ok {
		binding.LastSummary = buildUserQuestionResolvedSummary(question, normalizedStatus, values, message)
		a.activeRuns[key] = binding
	}
	statusCardID := ""
	statusPayload := StatusCardPayload{}
	if ok {
		statusCardID = strings.TrimSpace(binding.CardID)
		statusPayload = binding.statusCardPayload()
	}
	cardID := strings.TrimSpace(a.userQuestionCards[normalizedRequestID])
	delete(a.pendingQuestions, normalizedRequestID)
	delete(a.userQuestionCards, normalizedRequestID)
	delete(a.requestRuns, normalizedRequestID)
	a.mu.Unlock()

	if statusCardID != "" {
		if err := a.messenger.UpdateCard(context.Background(), statusCardID, statusPayload); err != nil {
			a.safeLog("update ask_user status card failed: %v", err)
		}
	}
	if cardID != "" {
		if err := a.messenger.UpdateUserQuestionCard(context.Background(), cardID, ResolvedUserQuestionCardPayload{
			RequestID: normalizedRequestID,
			Title:     question.Title,
			Status:    normalizedStatus,
			Summary:   buildUserQuestionResolvedSummary(question, normalizedStatus, values, message),
		}); err != nil {
			a.safeLog("update ask_user card failed: %v", err)
		}
	}
}

// updateApprovalStatus 在审批动作被网关受理后更新 run 卡片中的审批结论，并更新权限卡片为已处理状态。
func (a *Adapter) updateApprovalStatus(requestID string, decision string) {
	normalizedRequestID := strings.TrimSpace(requestID)
	if normalizedRequestID == "" {
		return
	}
	normalizedDecision := normalizeApprovalDecision(decision)
	if normalizedDecision == "" {
		return
	}
	a.mu.Lock()
	if alreadyDecision, resolved := a.resolvedPermissions[normalizedRequestID]; resolved &&
		normalizeApprovalDecision(alreadyDecision) == normalizedDecision {
		a.mu.Unlock()
		return
	}
	key := a.requestRuns[normalizedRequestID]
	binding, ok := a.activeRuns[key]
	var resolvedApproval *approvalEntry
	if ok {
		for i := range binding.ApprovalRecords {
			if binding.ApprovalRecords[i].RequestID == normalizedRequestID {
				binding.ApprovalRecords[i].Decision = normalizedDecision
				entry := binding.ApprovalRecords[i]
				resolvedApproval = &entry
			}
		}
		approved := 0
		rejected := 0
		pending := 0
		for _, entry := range binding.ApprovalRecords {
			switch {
			case isApprovalApprovedDecision(entry.Decision):
				approved++
			case isApprovalRejectedDecision(entry.Decision):
				rejected++
			case isApprovalPendingDecision(entry.Decision):
				pending++
			}
		}
		switch {
		case pending > 0:
			binding.ApprovalStatus = "pending"
		case rejected > 0 && approved == 0:
			binding.ApprovalStatus = "rejected"
		case approved > 0 && rejected == 0:
			binding.ApprovalStatus = "approved"
		case approved > 0 && rejected > 0:
			binding.ApprovalStatus = "mixed"
		default:
			binding.ApprovalStatus = "none"
		}
		a.activeRuns[key] = binding
	}
	statusCardID := ""
	statusPayload := StatusCardPayload{}
	if ok {
		statusCardID = strings.TrimSpace(binding.CardID)
		statusPayload = binding.statusCardPayload()
	}
	permCardID := strings.TrimSpace(a.permissionCards[normalizedRequestID])
	if permCardID == "" {
		permCardID = strings.TrimSpace(a.runPermissionCards[key])
	}
	a.resolvedPermissions[normalizedRequestID] = normalizedDecision
	a.mu.Unlock()

	// 更新状态卡片
	if statusCardID != "" {
		if err := a.messenger.UpdateCard(context.Background(), statusCardID, statusPayload); err != nil {
			a.safeLog("update approval status card failed: %v", err)
		}
	}

	// 更新权限卡片为已处理状态（去掉按钮，显示结果）。
	// 这里不先删映射，避免更新失败时丢失后续收敛机会。
	resolvedCardUpdated := false
	if permCardID != "" {
		a.safeLog("permission card update start request_id=%s card_id=%s decision=%s", normalizedRequestID, permCardID, normalizedDecision)
		resolvedPayload := ResolvedPermissionCardPayload{
			RequestID: normalizedRequestID,
			Approved:  normalizedDecision != "reject",
		}
		if resolvedApproval != nil {
			resolvedPayload.ToolName = resolvedApproval.ToolName
			resolvedPayload.Message = resolvedApproval.Reason
		}
		if err := a.messenger.UpdatePermissionCard(context.Background(), permCardID, resolvedPayload); err != nil {
			a.safeLog("update permission card failed: %v", err)
		} else {
			resolvedCardUpdated = true
			a.safeLog("permission card update done request_id=%s card_id=%s decision=%s", normalizedRequestID, permCardID, normalizedDecision)
		}
	}
	if resolvedCardUpdated || permCardID == "" {
		a.mu.Lock()
		delete(a.permissionCards, normalizedRequestID)
		delete(a.requestRuns, normalizedRequestID)
		a.mu.Unlock()
	}
}

// schedulePermissionCardDismiss 在审批结果展示短暂停留后收起卡片，避免页面残留。
func (a *Adapter) schedulePermissionCardDismiss(requestID string, cardID string) {
	normalizedRequestID := strings.TrimSpace(requestID)
	normalizedCardID := strings.TrimSpace(cardID)
	if normalizedRequestID == "" || normalizedCardID == "" {
		return
	}
	delay := a.permissionCardDismissDelay
	if delay <= 0 {
		delay = defaultPermissionCardDismissDelay
	}
	go func() {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		<-timer.C
		callCtx, cancel := context.WithTimeout(context.Background(), a.cfg.RequestTimeout)
		defer cancel()
		if err := a.messenger.DeleteMessage(callCtx, normalizedCardID); err != nil {
			a.safeLog("delete permission card failed: %v", err)
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		if strings.TrimSpace(a.permissionCards[normalizedRequestID]) == normalizedCardID {
			delete(a.permissionCards, normalizedRequestID)
		}
	}()
}

// markRunTerminal 在 run 结束时合并结果摘要并刷新状态卡片。
func (a *Adapter) markRunTerminal(sessionID string, runID string, result string, summary string, fallback string) {
	key := runBindingKey(sessionID, runID)
	type permissionFinalize struct {
		requestID string
		cardID    string
		payload   ResolvedPermissionCardPayload
	}
	a.mu.Lock()
	binding, ok := a.activeRuns[key]
	if !ok {
		a.mu.Unlock()
		return
	}
	if strings.TrimSpace(summary) != "" {
		binding.LastSummary = strings.TrimSpace(summary)
	} else if strings.TrimSpace(fallback) != "" {
		binding.LastSummary = strings.TrimSpace(fallback)
	}
	normalizedResult := strings.TrimSpace(strings.ToLower(result))
	binding.Result = normalizedResult
	binding.Status = terminalStatusFromResult(normalizedResult)
	finalizeCards := make([]permissionFinalize, 0)
	for _, entry := range binding.ApprovalRecords {
		requestID := strings.TrimSpace(entry.RequestID)
		if requestID == "" {
			continue
		}
		cardID := strings.TrimSpace(a.permissionCards[requestID])
		if cardID == "" {
			continue
		}
		decision := normalizeApprovalDecision(entry.Decision)
		if !isApprovalApprovedDecision(decision) && !isApprovalRejectedDecision(decision) {
			continue
		}
		finalizeCards = append(finalizeCards, permissionFinalize{
			requestID: requestID,
			cardID:    cardID,
			payload: ResolvedPermissionCardPayload{
				RequestID: requestID,
				ToolName:  strings.TrimSpace(entry.ToolName),
				Message:   strings.TrimSpace(entry.Reason),
				Approved:  isApprovalApprovedDecision(decision),
			},
		})
	}
	cardID := strings.TrimSpace(binding.CardID)
	chatID := strings.TrimSpace(binding.ChatID)
	payload := binding.statusCardPayload()
	a.activeRuns[key] = binding
	a.mu.Unlock()
	for _, item := range finalizeCards {
		callCtx, cancel := context.WithTimeout(context.Background(), a.cfg.RequestTimeout)
		err := a.messenger.UpdatePermissionCard(callCtx, item.cardID, item.payload)
		cancel()
		if err != nil {
			a.safeLog("finalize permission card failed request_id=%s card_id=%s err=%v", item.requestID, item.cardID, err)
			continue
		}
		a.mu.Lock()
		delete(a.permissionCards, item.requestID)
		delete(a.requestRuns, item.requestID)
		a.mu.Unlock()
	}
	if cardID != "" {
		callCtx, cancel := context.WithTimeout(context.Background(), a.cfg.RequestTimeout)
		err := a.messenger.UpdateCard(callCtx, cardID, payload)
		cancel()
		if err != nil {
			a.safeLog("update terminal card failed: %v", err)
			if chatID != "" {
				terminalText := buildTerminalFallbackText(normalizedResult, binding.LastSummary)
				if sendErr := a.messenger.SendText(context.Background(), chatID, terminalText); sendErr != nil {
					a.safeLog("send terminal fallback text failed: %v", sendErr)
				}
			}
		}
		return
	}
	a.safeLog("markRunTerminal skipped card update: empty card_id session_id=%s run_id=%s result=%s", sessionID, runID, strings.TrimSpace(result))
	if chatID != "" {
		terminalText := buildTerminalFallbackText(normalizedResult, binding.LastSummary)
		if err := a.messenger.SendText(context.Background(), chatID, terminalText); err != nil {
			a.safeLog("send terminal fallback text failed: %v", err)
		}
	}
}

// shouldEmitProgress 控制普通运行进度消息推送频率，避免飞书侧刷屏。
func (a *Adapter) shouldEmitProgress(sessionID string, runID string, runtimeEventType string) bool {
	key := sessionID + "|" + runID + "|" + strings.TrimSpace(strings.ToLower(runtimeEventType))
	now := a.nowFn()
	a.mu.Lock()
	defer a.mu.Unlock()
	last, ok := a.lastProgressAt[key]
	if ok && now.Sub(last) < defaultProgressNotifyInterval {
		return false
	}
	a.lastProgressAt[key] = now
	return true
}

// isMentionCurrentBot 判断群聊消息是否明确 @ 到当前机器人。
// 说明：app_id 仅用于匹配 mention.app_id；user/open/union 需使用 bot 身份标识匹配。
func isMentionCurrentBot(event FeishuMessageEvent, cfg Config) bool {
	expectedAppID := strings.TrimSpace(strings.ToLower(cfg.AppID))
	if expectedAppID == "" {
		expectedAppID = strings.TrimSpace(strings.ToLower(event.HeaderAppID))
	}
	expectedUserID := strings.TrimSpace(strings.ToLower(cfg.BotUserID))
	expectedOpenID := strings.TrimSpace(strings.ToLower(cfg.BotOpenID))
	if expectedAppID == "" && expectedUserID == "" && expectedOpenID == "" {
		return false
	}

	for _, mention := range event.Mentions {
		appID := strings.TrimSpace(strings.ToLower(mention.AppID))
		userID := strings.TrimSpace(strings.ToLower(mention.UserID))
		openID := strings.TrimSpace(strings.ToLower(mention.OpenID))
		if expectedAppID != "" && appID != "" && appID == expectedAppID {
			return true
		}
		if expectedUserID != "" && userID != "" && userID == expectedUserID {
			return true
		}
		if expectedOpenID != "" && openID != "" && openID == expectedOpenID {
			return true
		}
	}

	normalizedText := strings.TrimSpace(strings.ToLower(event.ContentText))
	if expectedUserID != "" && (strings.Contains(normalizedText, `<at user_id="`+expectedUserID+`"`) ||
		strings.Contains(normalizedText, `<at user_id='`+expectedUserID+`'`) ||
		strings.Contains(normalizedText, `<at id="`+expectedUserID+`"`) ||
		strings.Contains(normalizedText, `<at id='`+expectedUserID+`'`)) {
		return true
	}
	if expectedOpenID != "" && (strings.Contains(normalizedText, `<at user_id="`+expectedOpenID+`"`) ||
		strings.Contains(normalizedText, `<at user_id='`+expectedOpenID+`'`) ||
		strings.Contains(normalizedText, `<at id="`+expectedOpenID+`"`) ||
		strings.Contains(normalizedText, `<at id='`+expectedOpenID+`'`)) {
		return true
	}
	return false
}

// tryHandleTextAction 处理权限审批与 ask_user 的文本降级指令。
func (a *Adapter) tryHandleTextAction(ctx context.Context, chatID string, text string) (bool, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false, nil
	}
	normalized := strings.ToLower(trimmed)
	switch {
	case strings.HasPrefix(normalized, "允许 "):
		requestID := strings.TrimSpace(trimmed[len("允许 "):])
		if requestID == "" {
			return true, nil
		}
		err := a.HandleCardAction(ctx, FeishuCardActionEvent{
			ActionType: "permission",
			RequestID:  requestID,
			Decision:   "allow_once",
		})
		if err != nil {
			_ = a.messenger.SendText(context.Background(), chatID, "审批提交失败，请稍后重试。")
			return true, err
		}
		_ = a.messenger.SendText(context.Background(), chatID, "审批已提交：允许一次。")
		return true, nil
	case strings.HasPrefix(normalized, "拒绝 "):
		requestID := strings.TrimSpace(trimmed[len("拒绝 "):])
		if requestID == "" {
			return true, nil
		}
		err := a.HandleCardAction(ctx, FeishuCardActionEvent{
			ActionType: "permission",
			RequestID:  requestID,
			Decision:   "reject",
		})
		if err != nil {
			_ = a.messenger.SendText(context.Background(), chatID, "审批提交失败，请稍后重试。")
			return true, err
		}
		_ = a.messenger.SendText(context.Background(), chatID, "审批已提交：拒绝。")
		return true, nil
	case strings.HasPrefix(normalized, "跳过 "):
		requestID := strings.TrimSpace(trimmed[len("跳过 "):])
		if requestID == "" {
			return true, nil
		}
		err := a.HandleCardAction(ctx, FeishuCardActionEvent{
			ActionType: "user_question",
			RequestID:  requestID,
			Status:     "skipped",
		})
		if err != nil {
			_ = a.messenger.SendText(context.Background(), chatID, "回答提交失败，请稍后重试。")
			return true, err
		}
		_ = a.messenger.SendText(context.Background(), chatID, "已提交：跳过当前问题。")
		return true, nil
	case strings.HasPrefix(normalized, "回答 "):
		remainder := strings.TrimSpace(trimmed[len("回答 "):])
		requestID, answer := splitRequestAndBody(remainder)
		if requestID == "" {
			return true, nil
		}
		values, message, ok := a.parseUserQuestionTextAnswer(requestID, answer)
		if !ok {
			_ = a.messenger.SendText(context.Background(), chatID, "回答格式无效，请使用：回答 <request_id> <内容>")
			return true, nil
		}
		err := a.HandleCardAction(ctx, FeishuCardActionEvent{
			ActionType: "user_question",
			RequestID:  requestID,
			Status:     "answered",
			Values:     values,
			Message:    message,
		})
		if err != nil {
			_ = a.messenger.SendText(context.Background(), chatID, "回答提交失败，请稍后重试。")
			return true, err
		}
		_ = a.messenger.SendText(context.Background(), chatID, "回答已提交。")
		return true, nil
	default:
		return false, nil
	}
}

// tryHandleTextPermission 为兼容旧测试与调用入口保留，内部复用统一文本动作处理。
func (a *Adapter) tryHandleTextPermission(ctx context.Context, chatID string, text string) (bool, error) {
	return a.tryHandleTextAction(ctx, chatID, text)
}

// parseUserQuestionTextAnswer 根据 pending 问题元数据解析文本回答指令。
func (a *Adapter) parseUserQuestionTextAnswer(requestID string, answer string) ([]string, string, bool) {
	trimmedRequestID := strings.TrimSpace(requestID)
	trimmedAnswer := strings.TrimSpace(answer)
	a.mu.RLock()
	question, ok := a.pendingQuestions[trimmedRequestID]
	a.mu.RUnlock()
	if !ok {
		if trimmedAnswer == "" {
			return nil, "", false
		}
		return []string{trimmedAnswer}, trimmedAnswer, true
	}

	switch strings.TrimSpace(strings.ToLower(question.Kind)) {
	case "text":
		if trimmedAnswer == "" {
			return nil, "", false
		}
		return []string{trimmedAnswer}, trimmedAnswer, true
	case "single_choice":
		if trimmedAnswer == "" {
			return nil, "", false
		}
		if len(question.Options) == 0 {
			return []string{trimmedAnswer}, "", true
		}
		matched, ok := resolveChoiceLabel(trimmedAnswer, question.Options)
		if !ok {
			return nil, "", false
		}
		return []string{matched}, "", true
	case "multi_choice":
		if trimmedAnswer == "" {
			return nil, "", false
		}
		rawTokens := splitMultiChoiceTokens(trimmedAnswer)
		if len(rawTokens) == 0 {
			return nil, "", false
		}
		selected := make([]string, 0, len(rawTokens))
		for _, token := range rawTokens {
			if len(question.Options) == 0 {
				selected = append(selected, token)
				continue
			}
			matched, ok := resolveChoiceLabel(token, question.Options)
			if !ok {
				return nil, "", false
			}
			selected = append(selected, matched)
		}
		selected = uniqueNonEmptyStrings(selected)
		if question.MaxChoices > 0 && len(selected) > question.MaxChoices {
			return nil, "", false
		}
		return selected, "", true
	default:
		if trimmedAnswer == "" {
			return nil, "", false
		}
		return []string{trimmedAnswer}, trimmedAnswer, true
	}
}

// resolveChoiceLabel 解析单个选项输入，支持按标签文本或 1-based 序号匹配。
func resolveChoiceLabel(raw string, options []UserQuestionCardOption) (string, bool) {
	token := strings.TrimSpace(raw)
	if token == "" {
		return "", false
	}
	if index, err := strconv.Atoi(token); err == nil {
		if index >= 1 && index <= len(options) {
			label := strings.TrimSpace(options[index-1].Label)
			if label != "" {
				return label, true
			}
		}
	}
	normalizedToken := normalizeChoiceToken(token)
	for _, option := range options {
		label := strings.TrimSpace(option.Label)
		if normalizeChoiceToken(label) == normalizedToken {
			return label, true
		}
	}
	return "", false
}

// splitRequestAndBody 将“<request_id> <body>”文本分离为 request_id 与正文。
func splitRequestAndBody(input string) (string, string) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "", ""
	}
	parts := strings.Fields(trimmed)
	if len(parts) == 0 {
		return "", ""
	}
	requestID := strings.TrimSpace(parts[0])
	body := ""
	if len(parts) > 1 {
		body = strings.TrimSpace(strings.TrimPrefix(trimmed, parts[0]))
	}
	return requestID, body
}

// splitMultiChoiceTokens 支持“逗号/中文逗号/竖线/空格”分隔的多选文本输入。
func splitMultiChoiceTokens(raw string) []string {
	replacer := strings.NewReplacer("，", ",", "|", ",", "、", ",", ";", ",", "；", ",")
	normalized := replacer.Replace(raw)
	segments := strings.Split(normalized, ",")
	if len(segments) == 1 {
		return uniqueNonEmptyStrings(strings.Fields(normalized))
	}
	tokens := make([]string, 0, len(segments))
	for _, segment := range segments {
		trimmed := strings.TrimSpace(segment)
		if trimmed != "" {
			tokens = append(tokens, trimmed)
		}
	}
	if len(tokens) == 0 {
		return uniqueNonEmptyStrings(strings.Fields(normalized))
	}
	return uniqueNonEmptyStrings(tokens)
}

// normalizeChoiceToken 对选项文本做归一化比较，避免大小写与多空格影响匹配。
func normalizeChoiceToken(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}

// uniqueNonEmptyStrings 去重并保序保留非空字符串。
func uniqueNonEmptyStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		key := normalizeChoiceToken(trimmed)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

// runBindingKey 生成稳定的 session/run 复合键，避免同会话多 run 相互覆盖。
func runBindingKey(sessionID string, runID string) string {
	return strings.TrimSpace(sessionID) + "|" + strings.TrimSpace(runID)
}

// decodeMessageText 从飞书消息 content JSON 中提取文本内容。
func decodeMessageText(rawContent string) (string, error) {
	trimmed := strings.TrimSpace(rawContent)
	if trimmed == "" {
		return "", nil
	}
	var payload inboundMessageContent
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return "", err
	}
	return strings.TrimSpace(payload.Text), nil
}

// extractPermissionRequest 从 permission_requested 事件中抽取审批请求关键信息。
func extractPermissionRequest(envelope map[string]any) (requestID, toolName, operation, target, reason string) {
	payload, _ := envelope["payload"].(map[string]any)
	if payload == nil {
		return "", "", "", "", "需要审批"
	}
	requestID = readString(payload, "request_id")
	toolName = readString(payload, "tool_name")
	operation = readString(payload, "operation")
	target = readString(payload, "target")
	reason = readString(payload, "reason")
	if reason == "" {
		reason = "工具执行请求审批，请确认是否放行。"
	}
	return
}

// extractPermissionResolved 从 permission_resolved 事件中抽取 request_id 与决议结果。
func extractPermissionResolved(envelope map[string]any) (requestID, decision string) {
	payload, _ := envelope["payload"].(map[string]any)
	if payload == nil {
		return "", ""
	}
	requestID = strings.TrimSpace(readString(payload, "request_id"))
	decision = strings.TrimSpace(strings.ToLower(readString(payload, "decision")))
	return requestID, decision
}

// normalizeApprovalDecision 统一审批决议值，兼容 runtime 与卡片动作的不同枚举。
func normalizeApprovalDecision(decision string) string {
	switch strings.TrimSpace(strings.ToLower(decision)) {
	case "allow", "allow_once":
		return "allow_once"
	case "allow_session":
		return "allow_session"
	case "deny", "denied", "reject", "rejected":
		return "reject"
	case "pending":
		return "pending"
	default:
		return strings.TrimSpace(strings.ToLower(decision))
	}
}

// isApprovalApprovedDecision 判断审批是否为“通过”态。
func isApprovalApprovedDecision(decision string) bool {
	switch normalizeApprovalDecision(decision) {
	case "allow_once", "allow_session":
		return true
	default:
		return false
	}
}

// isApprovalRejectedDecision 判断审批是否为“拒绝”态。
func isApprovalRejectedDecision(decision string) bool {
	return normalizeApprovalDecision(decision) == "reject"
}

// isApprovalPendingDecision 判断审批是否为“等待”态。
func isApprovalPendingDecision(decision string) bool {
	return normalizeApprovalDecision(decision) == "pending"
}

// extractUserQuestionRequest 从 user_question_requested 事件中抽取关键信息。
func extractUserQuestionRequest(envelope map[string]any) userQuestionEntry {
	payload, _ := envelope["payload"].(map[string]any)
	if payload == nil {
		return userQuestionEntry{}
	}
	entry := userQuestionEntry{
		RequestID:   strings.TrimSpace(readString(payload, "request_id")),
		QuestionID:  strings.TrimSpace(readString(payload, "question_id")),
		Title:       strings.TrimSpace(readString(payload, "title")),
		Description: strings.TrimSpace(readString(payload, "description")),
		Kind:        strings.TrimSpace(strings.ToLower(readString(payload, "kind"))),
		AllowSkip:   readBool(payload, "allow_skip"),
		MaxChoices:  readInt(payload, "max_choices"),
	}
	rawOptions, _ := payload["options"].([]any)
	if len(rawOptions) > 0 {
		options := make([]UserQuestionCardOption, 0, len(rawOptions))
		for _, raw := range rawOptions {
			switch typed := raw.(type) {
			case string:
				label := strings.TrimSpace(typed)
				if label != "" {
					options = append(options, UserQuestionCardOption{Label: label})
				}
			case map[string]any:
				label := strings.TrimSpace(readString(typed, "label"))
				if label == "" {
					continue
				}
				options = append(options, UserQuestionCardOption{
					Label:       label,
					Description: strings.TrimSpace(readString(typed, "description")),
				})
			}
		}
		entry.Options = options
	}
	return entry
}

type userQuestionResolved struct {
	RequestID string
	Status    string
	Values    []string
	Message   string
}

// extractUserQuestionResolved 从 user_question_* resolved 事件中抽取回传结果。
func extractUserQuestionResolved(envelope map[string]any) userQuestionResolved {
	payload, _ := envelope["payload"].(map[string]any)
	if payload == nil {
		return userQuestionResolved{}
	}
	resolved := userQuestionResolved{
		RequestID: strings.TrimSpace(readString(payload, "request_id")),
		Status:    strings.TrimSpace(strings.ToLower(readString(payload, "status"))),
		Message:   strings.TrimSpace(readString(payload, "message")),
	}
	rawValues, _ := payload["values"].([]any)
	values := make([]string, 0, len(rawValues))
	for _, raw := range rawValues {
		value, _ := raw.(string)
		value = strings.TrimSpace(value)
		if value != "" {
			values = append(values, value)
		}
	}
	resolved.Values = values
	return resolved
}

// shouldSendAskUserCard 根据问题形态判断是否展示飞书交互卡片。
func shouldSendAskUserCard(question userQuestionEntry) bool {
	kind := strings.TrimSpace(strings.ToLower(question.Kind))
	if kind == "single_choice" && len(question.Options) > 0 {
		return true
	}
	return question.AllowSkip
}

// isUserQuestionResolvedEvent 判断 runtime event 是否为 ask_user 终态事件。
func isUserQuestionResolvedEvent(runtimeType string) bool {
	switch strings.TrimSpace(strings.ToLower(runtimeType)) {
	case "user_question_answered", "user_question_skipped", "user_question_timeout":
		return true
	default:
		return false
	}
}

// userQuestionStatusFromRuntimeType 将 runtime 终态事件类型映射为 status 字段。
func userQuestionStatusFromRuntimeType(runtimeType string) string {
	switch strings.TrimSpace(strings.ToLower(runtimeType)) {
	case "user_question_skipped":
		return "skipped"
	case "user_question_timeout":
		return "timeout"
	default:
		return "answered"
	}
}

// buildAskUserTextPrompt 构造文本降级指令，覆盖 text/multi_choice 及无按钮场景。
func buildAskUserTextPrompt(question userQuestionEntry) string {
	title := strings.TrimSpace(question.Title)
	if title == "" {
		title = "请回答以下问题"
	}
	lines := []string{title}
	if description := strings.TrimSpace(question.Description); description != "" {
		lines = append(lines, description)
	}
	if len(question.Options) > 0 {
		optionLabels := make([]string, 0, len(question.Options))
		for _, option := range question.Options {
			label := strings.TrimSpace(option.Label)
			if label != "" {
				optionLabels = append(optionLabels, label)
			}
		}
		if len(optionLabels) > 0 {
			lines = append(lines, "可选项："+strings.Join(optionLabels, " / "))
		}
	}
	lines = append(lines, fmt.Sprintf("请回复：回答 %s <内容>", strings.TrimSpace(question.RequestID)))
	if question.AllowSkip {
		lines = append(lines, fmt.Sprintf("如需跳过：跳过 %s", strings.TrimSpace(question.RequestID)))
	}
	return strings.Join(lines, "\n")
}

// buildUserQuestionResolvedSummary 生成 ask_user 终态摘要文案，写入状态卡与已处理卡片。
func buildUserQuestionResolvedSummary(question userQuestionEntry, status string, values []string, message string) string {
	switch strings.TrimSpace(strings.ToLower(status)) {
	case "skipped":
		return "用户已跳过该问题"
	case "timeout":
		return "问题等待超时"
	default:
		if trimmed := strings.TrimSpace(message); trimmed != "" {
			return "用户回答：" + trimmed
		}
		if len(values) > 0 {
			return "用户回答：" + strings.Join(values, ", ")
		}
		if strings.TrimSpace(question.Title) != "" {
			return "用户已回答：" + strings.TrimSpace(question.Title)
		}
		return "用户已回答问题"
	}
}

// buildCardActionDedupeKey 生成卡片动作幂等键，优先使用 event_id 避免重复回调重放。
func buildCardActionDedupeKey(event FeishuCardActionEvent, actionType string) string {
	if eventID := strings.TrimSpace(event.EventID); eventID != "" {
		return "card:event:" + eventID
	}
	requestID := strings.TrimSpace(event.RequestID)
	if actionType == "permission" {
		return "card:permission:" + requestID + ":" + strings.TrimSpace(strings.ToLower(event.Decision))
	}
	return "card:user_question:" + requestID + ":" + strings.TrimSpace(strings.ToLower(event.Status))
}

// readBool 从松散 map 中读取 bool 字段并提供 false 默认值。
func readBool(m map[string]any, key string) bool {
	if m == nil {
		return false
	}
	value, _ := m[key].(bool)
	return value
}

// readInt 从松散 map 中读取 int 字段，不可解析时返回 0。
func readInt(m map[string]any, key string) int {
	if m == nil {
		return 0
	}
	switch typed := m[key].(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		value, err := typed.Int64()
		if err == nil {
			return int(value)
		}
	}
	return 0
}

// extractHookNotificationSummary 提取 async_rewake 等通知摘要并写入卡片，便于下轮继续追踪。
func extractHookNotificationSummary(envelope map[string]any) string {
	payload, _ := envelope["payload"].(map[string]any)
	if payload == nil {
		return ""
	}
	if summary := strings.TrimSpace(readString(payload, "summary")); summary != "" {
		return summary
	}
	if summary := strings.TrimSpace(readString(payload, "notification")); summary != "" {
		return summary
	}
	return strings.TrimSpace(readString(payload, "message"))
}

// extractHookNotificationHint 提取 async_rewake 原因，用于提示用户本轮外部异步事件来源。
func extractHookNotificationHint(envelope map[string]any) string {
	payload, _ := envelope["payload"].(map[string]any)
	if payload == nil {
		return ""
	}
	if reason := strings.TrimSpace(readString(payload, "reason")); reason != "" {
		return reason
	}
	return strings.TrimSpace(readString(payload, "status"))
}

// extractSummaryText 从 run_done / run_error 载荷中提取卡片摘要，优先复用用户可见文本。
func extractSummaryText(envelope map[string]any) string {
	if text := extractUserVisibleDoneText(envelope); strings.TrimSpace(text) != "" {
		return strings.TrimSpace(text)
	}
	return strings.TrimSpace(extractUserVisibleErrorText(envelope))
}

// extractUserVisibleDoneText 从 run_done 事件中提取可展示给飞书用户的最终文本。
func extractUserVisibleDoneText(envelope map[string]any) string {
	if envelope == nil {
		return ""
	}
	payload, _ := envelope["payload"].(map[string]any)
	if payload == nil {
		return ""
	}
	if text := strings.TrimSpace(readString(payload, "content")); text != "" {
		return text
	}
	if text := strings.TrimSpace(readString(payload, "text")); text != "" {
		return text
	}
	parts, _ := payload["parts"].([]any)
	if len(parts) == 0 {
		return ""
	}
	lines := make([]string, 0, len(parts))
	for _, raw := range parts {
		part, _ := raw.(map[string]any)
		if part == nil {
			continue
		}
		partType := strings.TrimSpace(strings.ToLower(readString(part, "type")))
		if partType != "" && partType != "text" {
			continue
		}
		text := strings.TrimSpace(readString(part, "text"))
		if text == "" {
			text = strings.TrimSpace(readString(part, "content"))
		}
		if text != "" {
			lines = append(lines, text)
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// extractUserVisibleErrorText 从 run_error 事件中提取对用户友好的失败摘要。
func extractUserVisibleErrorText(envelope map[string]any) string {
	if envelope == nil {
		return ""
	}
	payload, _ := envelope["payload"].(map[string]any)
	if payload == nil {
		return ""
	}
	message := strings.TrimSpace(readString(payload, "message"))
	if message == "" {
		message = strings.TrimSpace(readString(payload, "error"))
	}
	if message == "" {
		return ""
	}

	// 翻译 runner 相关错误码为用户可读消息
	if translated := translateRunnerError(message); translated != "" {
		return translated
	}

	return "任务失败：" + message
}

// translateRunnerError 将 runner 相关错误码翻译为中文提示。
func translateRunnerError(message string) string {
	switch {
	case strings.Contains(message, "runner_offline") || strings.Contains(message, "runner not online"):
		return "本机 Runner 未连接，请在电脑上启动 `neocode runner`"
	case strings.Contains(message, "capability_denied"):
		return "权限不足：当前能力令牌不允许此操作"
	case strings.Contains(message, "tool_execution_failed"):
		return "工具执行失败：" + message
	case strings.Contains(message, "timed out waiting for runner"):
		return "本机 Runner 响应超时，请检查网络连接和 Runner 状态"
	default:
		return ""
	}
}

// nextBackoff 计算指数退避下一步等待时间。
func nextBackoff(current time.Duration, max time.Duration) time.Duration {
	next := current * 2
	if next > max {
		return max
	}
	return next
}

// delayWithJitter 为退避时间添加轻量随机抖动，减少重连风暴。
func delayWithJitter(delay time.Duration) time.Duration {
	if delay <= 0 {
		return 200 * time.Millisecond
	}
	span := int64(delay / 4)
	if span <= 0 {
		return delay
	}
	jitter := rand.Int63n(span)
	return delay - time.Duration(span/2) + time.Duration(jitter)
}

// deriveRunStatus 将 runtime 过程事件压缩为用户可读的轻量级状态标签。
func deriveRunStatus(runtimeType string, envelope map[string]any, current string) string {
	switch strings.TrimSpace(strings.ToLower(runtimeType)) {
	case "phase_changed":
		payload, _ := envelope["payload"].(map[string]any)
		if to := strings.TrimSpace(strings.ToLower(readString(payload, "to"))); strings.Contains(to, "plan") {
			return "planning"
		}
		if to := strings.TrimSpace(strings.ToLower(readString(payload, "to"))); to != "" {
			return "running"
		}
	case "tool_call_thinking", "agent_chunk":
		return "thinking"
	case "permission_requested", "permission_resolved", "tool_start", "tool_result", "tool_chunk", "tool_diff",
		"user_question_requested", "user_question_answered", "user_question_skipped", "user_question_timeout",
		"verification_started", "verification_finished", "verification_completed", "verification_failed",
		"acceptance_decided", "hook_notification":
		return "running"
	}
	if strings.TrimSpace(current) == "" {
		return "thinking"
	}
	return current
}

// shouldMarkRunStalled 判断 run 是否进入“僵尸态”：未结束且长时间未收到事件。
func shouldMarkRunStalled(binding sessionBinding, now time.Time) bool {
	if strings.TrimSpace(strings.ToLower(binding.Result)) != "pending" {
		return false
	}
	if strings.TrimSpace(strings.ToLower(binding.ApprovalStatus)) == "pending" {
		return false
	}
	if binding.LastEventTime.IsZero() {
		return false
	}
	return now.Sub(binding.LastEventTime) > defaultRunStallTimeout
}

// terminalStatusFromResult 将终态结果映射为卡片状态字段，避免 run 已结束仍显示 running。
func terminalStatusFromResult(result string) string {
	switch strings.TrimSpace(strings.ToLower(result)) {
	case "success":
		return "success"
	case "failure":
		return "failure"
	default:
		return "running"
	}
}

// buildTerminalFallbackText 在终态卡片更新失败时提供可见文本回退，避免飞书侧感知为“无响应”。
func buildTerminalFallbackText(result string, summary string) string {
	trimmedSummary := strings.TrimSpace(summary)
	if strings.TrimSpace(strings.ToLower(result)) == "success" {
		if trimmedSummary != "" {
			return "任务已完成：\n" + trimmedSummary
		}
		return "任务已完成。"
	}
	if trimmedSummary != "" {
		return "任务执行失败：\n" + trimmedSummary
	}
	return "任务执行失败，请稍后重试。"
}

// buildTaskName 生成卡片标题中使用的任务摘要，保留原始输入首行信息且控制长度。
func buildTaskName(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "未命名任务"
	}
	line := strings.Split(trimmed, "\n")[0]
	runes := []rune(strings.TrimSpace(line))
	if len(runes) > 40 {
		return string(runes[:40]) + "..."
	}
	return string(runes)
}

// formatElapsed 格式化运行耗时，空 start 返回空字符串。
func formatElapsed(start time.Time) string {
	if start.IsZero() {
		return ""
	}
	d := time.Since(start)
	if d < time.Second {
		return "刚刚开始"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh %dm %ds", int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60)
}

// statusCardPayload 将 run 绑定状态映射为卡片更新载荷。
func (b sessionBinding) statusCardPayload() StatusCardPayload {
	pendingCount := 0
	records := make([]ApprovalRecord, 0, len(b.ApprovalRecords))
	for _, e := range b.ApprovalRecords {
		normalizedDecision := normalizeApprovalDecision(e.Decision)
		if isApprovalPendingDecision(normalizedDecision) {
			pendingCount++
		}
		records = append(records, ApprovalRecord{
			ToolName: e.ToolName,
			Decision: normalizedDecision,
		})
	}
	return StatusCardPayload{
		TaskName:        b.TaskName,
		Status:          b.Status,
		ApprovalStatus:  b.ApprovalStatus,
		ApprovalRecords: records,
		PendingCount:    pendingCount,
		Result:          b.Result,
		Summary:         b.LastSummary,
		AsyncRewakeHint: b.AsyncRewakeHint,
		Elapsed:         formatElapsed(b.RunStartTime),
	}
}

// writeJSON 向回调响应写入 JSON 内容。
func writeJSON(writer http.ResponseWriter, status int, payload any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(payload)
}

// safeLog 输出适配器日志，并避免 nil logger 导致 panic。
func (a *Adapter) safeLog(format string, args ...any) {
	if a.logger == nil {
		return
	}
	a.logger.Printf(format, args...)
}
