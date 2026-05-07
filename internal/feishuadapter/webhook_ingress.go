package feishuadapter

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// WebhookIngress 基于 HTTP 回调接收飞书消息与审批动作事件。
type WebhookIngress struct {
	cfg   Config
	nowFn func() time.Time
}

// NewWebhookIngress 创建 Webhook 入站实例。
func NewWebhookIngress(cfg Config, nowFn func() time.Time) Ingress {
	if nowFn == nil {
		nowFn = func() time.Time { return time.Now().UTC() }
	}
	return &WebhookIngress{
		cfg:   cfg,
		nowFn: nowFn,
	}
}

// Run 启动 HTTP 服务并将回调转换为标准化事件交给 handler。
func (w *WebhookIngress) Run(ctx context.Context, handler IngressHandler) error {
	mux := http.NewServeMux()
	mux.HandleFunc(w.cfg.EventPath, w.handleFeishuEvent(handler))
	mux.HandleFunc(w.cfg.CardPath, w.handleCardCallback(handler))
	server := &http.Server{
		Addr:              w.cfg.ListenAddress,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
	}

	done := make(chan error, 1)
	go func() {
		done <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		return ctx.Err()
	case err := <-done:
		if err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	}
}

// handleFeishuEvent 处理飞书消息回调并转发为标准化消息事件。
func (w *WebhookIngress) handleFeishuEvent(handler IngressHandler) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		body, ok := w.readAndVerifyRequest(writer, request)
		if !ok {
			return
		}
		var envelope inboundEnvelope
		if err := json.Unmarshal(body, &envelope); err != nil {
			http.Error(writer, "invalid json body", http.StatusBadRequest)
			return
		}
		if strings.EqualFold(strings.TrimSpace(envelope.Type), "url_verification") {
			if !w.verifyCallbackToken(envelope.Token, envelope.Header.Token) {
				http.Error(writer, "invalid verify token", http.StatusUnauthorized)
				return
			}
			writeJSON(writer, http.StatusOK, map[string]string{"challenge": envelope.Challenge})
			return
		}
		if strings.TrimSpace(envelope.Header.EventType) != "im.message.receive_v1" {
			writeJSON(writer, http.StatusOK, map[string]string{"message": "ignored"})
			return
		}
		if !w.verifyCallbackToken(envelope.Token, envelope.Header.Token) {
			http.Error(writer, "invalid verify token", http.StatusUnauthorized)
			return
		}

		var event inboundMessageEvent
		if err := json.Unmarshal(envelope.Event, &event); err != nil {
			http.Error(writer, "invalid event body", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(event.Message.MessageID) == "" || strings.TrimSpace(event.Message.ChatID) == "" {
			http.Error(writer, "missing message_id or chat_id", http.StatusBadRequest)
			return
		}
		text, err := decodeMessageText(event.Message.Content)
		if err != nil {
			http.Error(writer, "invalid message content", http.StatusBadRequest)
			return
		}
		messageEvent := FeishuMessageEvent{
			EventID:     strings.TrimSpace(envelope.Header.EventID),
			MessageID:   strings.TrimSpace(event.Message.MessageID),
			ChatID:      strings.TrimSpace(event.Message.ChatID),
			ChatType:    firstNonEmpty(strings.TrimSpace(event.Message.ChatType), strings.TrimSpace(event.ChatType)),
			ContentText: text,
			HeaderAppID: strings.TrimSpace(envelope.Header.AppID),
			Mentions:    convertMentions(event.Message.Mentions),
		}
		if err := handler.HandleMessage(request.Context(), messageEvent); err != nil {
			writeJSON(writer, http.StatusInternalServerError, map[string]string{"message": "retryable_error"})
			return
		}
		writeJSON(writer, http.StatusOK, map[string]string{"message": "accepted"})
	}
}

// handleCardCallback 处理飞书卡片回调并转发标准化审批动作事件。
func (w *WebhookIngress) handleCardCallback(handler IngressHandler) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		body, ok := w.readAndVerifyRequest(writer, request)
		if !ok {
			return
		}
		var envelope inboundEnvelope
		if err := json.Unmarshal(body, &envelope); err == nil {
			if strings.EqualFold(strings.TrimSpace(envelope.Type), "url_verification") {
				if !w.verifyCallbackToken(envelope.Token, envelope.Header.Token) {
					http.Error(writer, "invalid verify token", http.StatusUnauthorized)
					return
				}
				writeJSON(writer, http.StatusOK, map[string]string{"challenge": envelope.Challenge})
				return
			}
		}

		var callback inboundCardCallback
		if err := json.Unmarshal(body, &callback); err != nil {
			http.Error(writer, "invalid card callback body", http.StatusBadRequest)
			return
		}
		if !w.verifyCallbackToken(callback.Token, callback.Header.Token) {
			http.Error(writer, "invalid verify token", http.StatusUnauthorized)
			return
		}
		requestID := strings.TrimSpace(callback.Action.Value["request_id"])
		decision := strings.TrimSpace(strings.ToLower(callback.Action.Value["decision"]))
		if requestID == "" || (decision != "allow_once" && decision != "reject") {
			writeJSON(writer, http.StatusOK, map[string]any{"toast": map[string]string{"type": "info", "content": "callback ready"}})
			return
		}
		if err := handler.HandleCardAction(request.Context(), FeishuCardActionEvent{
			EventID:   strings.TrimSpace(callback.Header.EventID),
			RequestID: requestID,
			Decision:  decision,
		}); err != nil {
			http.Error(writer, "card action failed", http.StatusInternalServerError)
			return
		}
		writeJSON(writer, http.StatusOK, map[string]any{"toast": map[string]string{"type": "success", "content": "审批已提交"}})
	}
}

// readAndVerifyRequest 读取回调请求并执行签名校验。
func (w *WebhookIngress) readAndVerifyRequest(writer http.ResponseWriter, request *http.Request) ([]byte, bool) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return nil, false
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, 1<<20))
	if err != nil {
		http.Error(writer, "read request failed", http.StatusBadRequest)
		return nil, false
	}
	if !verifyFeishuSignature(
		w.cfg.SigningSecret,
		defaultSignatureMaxSkew,
		request.Header,
		body,
		w.nowFn(),
		w.cfg.InsecureSkipSignVerify,
	) {
		http.Error(writer, "invalid signature", http.StatusUnauthorized)
		return nil, false
	}
	return body, true
}

// verifyCallbackToken 校验回调 token，兼容 body/header 双位置。
func (w *WebhookIngress) verifyCallbackToken(rawToken string, headerToken string) bool {
	expected := strings.TrimSpace(w.cfg.VerifyToken)
	if expected == "" {
		return false
	}
	for _, candidate := range []string{strings.TrimSpace(rawToken), strings.TrimSpace(headerToken)} {
		if candidate == expected {
			return true
		}
	}
	return false
}

// convertMentions 将回调 mentions 转换为标准化提及身份结构。
func convertMentions(mentions []inboundMention) []FeishuMention {
	if len(mentions) == 0 {
		return nil
	}
	out := make([]FeishuMention, 0, len(mentions))
	for _, mention := range mentions {
		out = append(out, FeishuMention{
			AppID:   strings.TrimSpace(mention.ID.AppID),
			UserID:  strings.TrimSpace(mention.ID.UserID),
			OpenID:  strings.TrimSpace(mention.ID.OpenID),
			UnionID: strings.TrimSpace(mention.ID.UnionID),
		})
	}
	return out
}

// firstNonEmpty 返回第一个非空字符串。
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
