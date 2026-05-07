package feishuadapter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"neo-code/internal/gateway/protocol"
)

const defaultSignatureMaxSkew = 5 * time.Minute
const defaultProgressNotifyInterval = 2 * time.Second
const defaultCardRefreshInterval = 1500 * time.Millisecond

type sessionBinding struct {
	SessionID       string
	ChatID          string
	RunID           string
	CardID          string
	TaskName        string
	Status          string
	ApprovalStatus  string
	Result          string
	LastSummary     string
	AsyncRewakeHint string
	RunStartTime    time.Time
}

// Adapter 负责桥接飞书回调与 Gateway JSON-RPC 长连接。
type Adapter struct {
	cfg       Config
	gateway   GatewayClient
	messenger Messenger
	logger    *log.Logger
	idem      *idempotencyStore

	nowFn func() time.Time

	mu             sync.RWMutex
	activeRuns     map[string]sessionBinding
	sessionChats   map[string]string
	requestRuns    map[string]string
	lastProgressAt map[string]time.Time
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
		cfg:            cfg,
		gateway:        gateway,
		messenger:      messenger,
		logger:         logger,
		idem:           newIdempotencyStore(cfg.IdempotencyTTL),
		nowFn:          func() time.Time { return time.Now().UTC() },
		activeRuns:     make(map[string]sessionBinding),
		sessionChats:   make(map[string]string),
		requestRuns:    make(map[string]string),
		lastProgressAt: make(map[string]time.Time),
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
	if chatType == "group" && !isMentionCurrentBot(event, a.cfg) {
		return nil
	}
	if strings.TrimSpace(event.MessageID) == "" || strings.TrimSpace(event.ChatID) == "" {
		return fmt.Errorf("missing message_id or chat_id")
	}
	dedupeKey := "msg:" + strings.TrimSpace(event.EventID) + ":" + strings.TrimSpace(event.MessageID)
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

	text := strings.TrimSpace(event.ContentText)
	if text == "" {
		return nil
	}
	if handled, err := a.tryHandleTextPermission(ctx, event.ChatID, text); handled {
		if err == nil {
			succeeded = true
		}
		return err
	}

	sessionID := BuildSessionID(event.ChatID)
	runID := BuildRunID(event.MessageID)
	if err := a.bindThenRun(ctx, sessionID, runID, event.ChatID, text); err != nil {
		a.safeLog("handle message failed: %v", err)
		_ = a.messenger.SendText(context.Background(), event.ChatID, "任务受理失败，请稍后重试。")
		return err
	}
	succeeded = true
	return nil
}

// HandleCardAction 处理标准化后的审批动作事件并映射到网关授权接口。
func (a *Adapter) HandleCardAction(ctx context.Context, event FeishuCardActionEvent) error {
	requestID := strings.TrimSpace(event.RequestID)
	decision := strings.TrimSpace(strings.ToLower(event.Decision))
	if requestID == "" || (decision != "allow_once" && decision != "reject") {
		return nil
	}
	dedupeKey := "card:" + requestID + ":" + decision
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
	if err := a.gateway.ResolvePermission(callCtx, requestID, decision); err != nil {
		a.safeLog("resolve permission failed: %v", err)
		return err
	}
	a.updateApprovalStatus(requestID, decision)
	succeeded = true
	return nil
}

// bindThenRun 按 authenticate -> bindStream -> run 的顺序提交一次请求并记录会话绑定。
func (a *Adapter) bindThenRun(ctx context.Context, sessionID string, runID string, chatID string, text string) error {
	callCtx, cancel := context.WithTimeout(ctx, a.cfg.RequestTimeout)
	defer cancel()
	if err := a.gateway.Authenticate(callCtx); err != nil {
		return err
	}
	if err := a.gateway.BindStream(callCtx, sessionID, runID); err != nil {
		return err
	}
	a.trackSession(sessionID, runID, chatID, text)
	if err := a.gateway.Run(callCtx, sessionID, runID, text); err != nil {
		// run 受理失败时及时回收活跃绑定，避免重连阶段反复重绑无效 run。
		a.untrackRun(sessionID, runID)
		return err
	}
	if err := a.ensureRunCard(context.Background(), sessionID, runID); err != nil {
		a.safeLog("send status card failed: %v", err)
		_ = a.messenger.SendText(context.Background(), chatID, "任务已受理，正在执行。")
	}
	return nil
}

// trackSession 记录 session 到飞书 chat 的映射，用于事件回推。
func (a *Adapter) trackSession(sessionID string, runID string, chatID string, taskName string) {
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
		RunStartTime:   a.nowFn(),
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
			}
		}
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
	chatID := a.lookupChatID(sessionID, runID)
	if chatID == "" {
		return
	}
	switch strings.ToLower(strings.TrimSpace(eventType)) {
	case "run_progress":
		if envelope != nil {
			if runtimeType := readString(envelope, "runtime_event_type"); runtimeType != "" {
				if strings.EqualFold(runtimeType, "permission_requested") {
					requestID, reason := extractPermissionRequest(envelope)
					if requestID != "" {
						a.markPermissionPending(sessionID, runID, requestID, reason)
						_ = a.messenger.SendPermissionCard(ctx, chatID, PermissionCardPayload{
							RequestID: requestID,
							Message:   reason,
						})
						return
					}
				}
				a.handleRunProgressCard(ctx, sessionID, runID, runtimeType, envelope)
			}
		}
		// 除审批请求外，内部 runtime_event_type 不直接透出到飞书用户视图，避免暴露控制面细节。
		return
	case "run_done":
		a.markRunTerminal(sessionID, runID, "success", extractSummaryText(envelope), "")
		a.untrackRun(sessionID, runID)
	case "run_error":
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
func (a *Adapter) refreshActiveCards(ctx context.Context) {
	a.mu.RLock()
	snapshot := make([]sessionBinding, 0, len(a.activeRuns))
	for _, binding := range a.activeRuns {
		if strings.TrimSpace(binding.CardID) != "" {
			snapshot = append(snapshot, binding)
		}
	}
	a.mu.RUnlock()

	for _, binding := range snapshot {
		callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		if err := a.messenger.UpdateCard(callCtx, binding.CardID, binding.statusCardPayload()); err != nil {
			a.safeLog("refresh card failed card_id=%s err=%v", binding.CardID, err)
		}
		cancel()
	}
}

// ensureRunCard 为新受理的 run 发送单独状态卡片，集中展示执行状态与审批结果。
func (a *Adapter) ensureRunCard(ctx context.Context, sessionID string, runID string) error {
	a.mu.RLock()
	binding, ok := a.activeRuns[runBindingKey(sessionID, runID)]
	a.mu.RUnlock()
	if !ok || strings.TrimSpace(binding.ChatID) == "" {
		return nil
	}
	if strings.TrimSpace(binding.CardID) != "" {
		return a.messenger.UpdateCard(ctx, binding.CardID, binding.statusCardPayload())
	}
	cardID, err := a.messenger.SendStatusCard(ctx, binding.ChatID, binding.statusCardPayload())
	if err != nil {
		return err
	}
	if strings.TrimSpace(cardID) == "" {
		return nil
	}
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
		return
	}
	if err := a.messenger.UpdateCard(ctx, cardID, updated.statusCardPayload()); err != nil {
		a.safeLog("update status card failed: %v", err)
	}
}

// markPermissionPending 将权限请求映射到 run 卡片，便于用户在同一卡片观察审批状态。
func (a *Adapter) markPermissionPending(sessionID string, runID string, requestID string, reason string) {
	key := runBindingKey(sessionID, runID)
	a.mu.Lock()
	binding, ok := a.activeRuns[key]
	if ok {
		binding.ApprovalStatus = "pending"
		if strings.TrimSpace(reason) != "" {
			binding.LastSummary = strings.TrimSpace(reason)
		}
		a.activeRuns[key] = binding
	}
	if strings.TrimSpace(requestID) != "" {
		a.requestRuns[requestID] = key
	}
	cardID := ""
	payload := StatusCardPayload{}
	if ok {
		cardID = strings.TrimSpace(binding.CardID)
		payload = binding.statusCardPayload()
	}
	a.mu.Unlock()
	if cardID != "" {
		if err := a.messenger.UpdateCard(context.Background(), cardID, payload); err != nil {
			a.safeLog("update pending approval card failed: %v", err)
		}
	}
}

// updateApprovalStatus 在审批动作被网关受理后更新 run 卡片中的审批结论。
func (a *Adapter) updateApprovalStatus(requestID string, decision string) {
	normalizedDecision := strings.TrimSpace(strings.ToLower(decision))
	if normalizedDecision == "" {
		return
	}
	a.mu.Lock()
	key := a.requestRuns[strings.TrimSpace(requestID)]
	binding, ok := a.activeRuns[key]
	if ok {
		switch normalizedDecision {
		case "allow_once", "allow_session":
			binding.ApprovalStatus = "approved"
		case "reject":
			binding.ApprovalStatus = "rejected"
		}
		a.activeRuns[key] = binding
	}
	cardID := ""
	payload := StatusCardPayload{}
	if ok {
		cardID = strings.TrimSpace(binding.CardID)
		payload = binding.statusCardPayload()
	}
	a.mu.Unlock()
	if cardID != "" {
		if err := a.messenger.UpdateCard(context.Background(), cardID, payload); err != nil {
			a.safeLog("update approval status card failed: %v", err)
		}
	}
}

// markRunTerminal 在 run 结束时合并结果摘要并刷新状态卡片。
func (a *Adapter) markRunTerminal(sessionID string, runID string, result string, summary string, fallback string) {
	key := runBindingKey(sessionID, runID)
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
	binding.Result = strings.TrimSpace(result)
	binding.Status = "running"
	cardID := strings.TrimSpace(binding.CardID)
	payload := binding.statusCardPayload()
	a.activeRuns[key] = binding
	a.mu.Unlock()
	if cardID != "" {
		if err := a.messenger.UpdateCard(context.Background(), cardID, payload); err != nil {
			a.safeLog("update terminal card failed: %v", err)
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

// tryHandleTextPermission 处理 SDK 模式下的文本审批降级指令。
func (a *Adapter) tryHandleTextPermission(ctx context.Context, chatID string, text string) (bool, error) {
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
		err := a.HandleCardAction(ctx, FeishuCardActionEvent{RequestID: requestID, Decision: "allow_once"})
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
		err := a.HandleCardAction(ctx, FeishuCardActionEvent{RequestID: requestID, Decision: "reject"})
		if err != nil {
			_ = a.messenger.SendText(context.Background(), chatID, "审批提交失败，请稍后重试。")
			return true, err
		}
		_ = a.messenger.SendText(context.Background(), chatID, "审批已提交：拒绝。")
		return true, nil
	default:
		return false, nil
	}
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
func extractPermissionRequest(envelope map[string]any) (string, string) {
	payload, _ := envelope["payload"].(map[string]any)
	if payload == nil {
		return "", "需要审批"
	}
	requestID := readString(payload, "request_id")
	reason := readString(payload, "reason")
	if reason == "" {
		reason = "工具执行请求审批，请确认是否放行。"
	}
	return requestID, reason
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
	return "任务失败：" + message
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
		"verification_started", "verification_finished", "verification_completed", "verification_failed",
		"acceptance_decided", "hook_notification":
		return "running"
	}
	if strings.TrimSpace(current) == "" {
		return "thinking"
	}
	return current
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
	return StatusCardPayload{
		TaskName:        b.TaskName,
		Status:          b.Status,
		ApprovalStatus:  b.ApprovalStatus,
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
