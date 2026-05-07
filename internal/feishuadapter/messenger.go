package feishuadapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	feishuAPIBase = "https://open.feishu.cn"
)

// HTTPClient 定义发送飞书 API 请求所需的最小接口。
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type feishuMessenger struct {
	appID      string
	appSecret  string
	baseURL    string
	httpClient HTTPClient

	mu          sync.Mutex
	cachedToken string
	expireAt    time.Time
}

type feishuAPIResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		MessageID string `json:"message_id"`
	} `json:"data"`
}

// NewFeishuMessenger 创建默认飞书消息发送器。
func NewFeishuMessenger(appID string, appSecret string, httpClient HTTPClient) Messenger {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &feishuMessenger{
		appID:      strings.TrimSpace(appID),
		appSecret:  strings.TrimSpace(appSecret),
		baseURL:    feishuAPIBase,
		httpClient: httpClient,
	}
}

// SendText 向指定 chat_id 发送文本消息。
func (m *feishuMessenger) SendText(ctx context.Context, chatID string, text string) error {
	content, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return err
	}
	_, err = m.sendMessage(ctx, chatID, "text", string(content))
	return err
}

// SendPermissionCard 向指定 chat_id 发送最小审批卡片。
func (m *feishuMessenger) SendPermissionCard(ctx context.Context, chatID string, payload PermissionCardPayload) error {
	card := map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"elements": []map[string]any{
			{
				"tag":  "div",
				"text": map[string]any{"tag": "lark_md", "content": payload.Message},
			},
			{
				"tag": "action",
				"actions": []map[string]any{
					{
						"tag":  "button",
						"text": map[string]any{"tag": "plain_text", "content": "允许一次"},
						"type": "primary",
						"value": map[string]string{
							"decision":   "allow_once",
							"request_id": payload.RequestID,
						},
					},
					{
						"tag":  "button",
						"text": map[string]any{"tag": "plain_text", "content": "拒绝"},
						"type": "default",
						"value": map[string]string{
							"decision":   "reject",
							"request_id": payload.RequestID,
						},
					},
				},
			},
		},
	}
	content, err := json.Marshal(card)
	if err != nil {
		return err
	}
	_, err = m.sendMessage(ctx, chatID, "interactive", string(content))
	return err
}

// SendStatusCard 发送 run 维度的轻量级状态卡片，并返回可后续更新的 card_id。
func (m *feishuMessenger) SendStatusCard(ctx context.Context, chatID string, payload StatusCardPayload) (string, error) {
	content, err := json.Marshal(buildStatusCard(payload))
	if err != nil {
		return "", err
	}
	return m.sendMessage(ctx, chatID, "interactive", string(content))
}

// UpdateCard 根据 card_id 覆盖更新当前 run 的状态卡片内容。
func (m *feishuMessenger) UpdateCard(ctx context.Context, cardID string, payload StatusCardPayload) error {
	token, err := m.tenantAccessToken(ctx)
	if err != nil {
		return err
	}
	content, err := json.Marshal(buildStatusCard(payload))
	if err != nil {
		return err
	}
	body := map[string]string{
		"content": string(content),
	}
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	url := strings.TrimRight(m.baseURL, "/") + "/open-apis/im/v1/messages/" + cardID
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	return m.doJSONRequest(req)
}

// sendMessage 统一封装飞书消息发送请求，复用鉴权与错误处理。
func (m *feishuMessenger) sendMessage(ctx context.Context, chatID string, msgType string, content string) (string, error) {
	token, err := m.tenantAccessToken(ctx)
	if err != nil {
		return "", err
	}
	body := map[string]string{
		"receive_id": chatID,
		"msg_type":   msgType,
		"content":    content,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	url := strings.TrimRight(m.baseURL, "/") + "/open-apis/im/v1/messages?receive_id_type=chat_id"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	return m.doJSONRequestWithMessageID(req)
}

// doJSONRequestWithMessageID 执行飞书消息接口并返回 message_id。
func (m *feishuMessenger) doJSONRequestWithMessageID(req *http.Request) (string, error) {
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var payload feishuAPIResponse
	decodeErr := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload)
	if resp.StatusCode/100 != 2 {
		if decodeErr == nil {
			return "", fmt.Errorf("send feishu message failed: status=%d code=%d message=%s", resp.StatusCode, payload.Code, payload.Msg)
		}
		return "", fmt.Errorf("send feishu message failed: status=%d body=invalid_json", resp.StatusCode)
	}
	if decodeErr != nil {
		return "", fmt.Errorf("send feishu message failed: invalid response body: %w", decodeErr)
	}
	if payload.Code != 0 {
		return "", fmt.Errorf("send feishu message failed: status=%d code=%d message=%s", resp.StatusCode, payload.Code, payload.Msg)
	}
	return strings.TrimSpace(payload.Data.MessageID), nil
}

// doJSONRequest 执行不关心 message_id 的飞书 JSON API 请求。
func (m *feishuMessenger) doJSONRequest(req *http.Request) error {
	_, err := m.doJSONRequestWithMessageID(req)
	return err
}

// tenantAccessToken 获取并缓存 tenant access token，避免每次发送都重复换取。
func (m *feishuMessenger) tenantAccessToken(ctx context.Context) (string, error) {
	m.mu.Lock()
	if m.cachedToken != "" && time.Now().UTC().Before(m.expireAt) {
		token := m.cachedToken
		m.mu.Unlock()
		return token, nil
	}
	m.mu.Unlock()

	body := map[string]string{
		"app_id":     m.appID,
		"app_secret": m.appSecret,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	url := strings.TrimRight(m.baseURL, "/") + "/open-apis/auth/v3/tenant_access_token/internal"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var payload struct {
		Code              int    `json:"code"`
		Message           string `json:"msg"`
		TenantAccessToken string `json:"tenant_access_token"`
		Expire            int    `json:"expire"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return "", err
	}
	if resp.StatusCode/100 != 2 || payload.Code != 0 || strings.TrimSpace(payload.TenantAccessToken) == "" {
		return "", fmt.Errorf("fetch feishu tenant token failed: status=%d code=%d message=%s", resp.StatusCode, payload.Code, payload.Message)
	}
	expire := time.Duration(payload.Expire) * time.Second
	if expire <= 0 {
		expire = time.Hour
	}
	refreshAt := time.Now().UTC().Add(expire - 30*time.Second)
	m.mu.Lock()
	m.cachedToken = strings.TrimSpace(payload.TenantAccessToken)
	m.expireAt = refreshAt
	token := m.cachedToken
	m.mu.Unlock()
	return token, nil
}

// buildStatusCard 构造轻量级 run 状态卡片，避免聊天窗口被多条进度消息刷屏。
func buildStatusCard(payload StatusCardPayload) map[string]any {
	taskName := fallbackStatusField(payload.TaskName, "未命名任务")
	status := fallbackStatusField(payload.Status, "thinking")
	approval := fallbackStatusField(payload.ApprovalStatus, "none")
	result := fallbackStatusField(payload.Result, "pending")

	statusIcon, statusColor := statusIconAndColor(status)
	approvalIcon, _ := statusIconAndColor(approval)
	resultIcon, _ := statusIconAndColor(result)

	elements := []map[string]any{
		statusNoteElement(taskName),
		statusBarElement(statusIcon, "状态", status),
		statusBarElement(approvalIcon, "审批", approval),
		statusBarElement(resultIcon, "结果", result),
	}

	if elapsed := strings.TrimSpace(payload.Elapsed); elapsed != "" {
		elements = append(elements, map[string]any{
			"tag": "note",
			"elements": []map[string]any{
				{"tag": "plain_text", "content": "⏱ " + elapsed},
			},
		})
	}

	if summary := strings.TrimSpace(payload.Summary); summary != "" {
		elements = append(elements, map[string]any{
			"tag":  "div",
			"text": map[string]any{"tag": "lark_md", "content": "---\n**摘要**\n" + summary},
		})
	}

	if hint := strings.TrimSpace(payload.AsyncRewakeHint); hint != "" {
		elements = append(elements, map[string]any{
			"tag": "note",
			"elements": []map[string]any{
				{"tag": "plain_text", "content": "↩ " + hint},
			},
		})
	}

	return map[string]any{
		"config": map[string]any{
			"wide_screen_mode": true,
			"update_multi":     true,
		},
		"header": map[string]any{
			"title": map[string]string{
				"tag":     "plain_text",
				"content": "NeoCode 任务状态",
			},
			"template": statusColor,
		},
		"elements": elements,
	}
}

func statusNoteElement(taskName string) map[string]any {
	return map[string]any{
		"tag": "note",
		"elements": []map[string]any{
			{"tag": "plain_text", "content": "📋 " + taskName},
		},
	}
}

func statusBarElement(icon string, label string, value string) map[string]any {
	return map[string]any{
		"tag": "column_set",
		"flex_mode": "bisect",
		"background_style": "default",
		"columns": []map[string]any{
			{
				"tag":    "column",
				"width":  "weighted",
				"weight": 1,
				"elements": []map[string]any{
					{
						"tag":  "div",
						"text": map[string]any{"tag": "plain_text", "content": icon + " " + label},
					},
				},
			},
			{
				"tag":    "column",
				"width":  "weighted",
				"weight": 1,
				"elements": []map[string]any{
					{
						"tag":  "div",
						"text": map[string]any{"tag": "lark_md", "content": "**" + value + "**"},
					},
				},
			},
		},
	}
}

func statusIconAndColor(status string) (string, string) {
	switch strings.TrimSpace(strings.ToLower(status)) {
	case "thinking":
		return "💭", "blue"
	case "planning":
		return "📝", "wathet"
	case "running":
		return "⚙️", "indigo"
	case "pending":
		return "⏳", "yellow"
	case "approved":
		return "✅", "green"
	case "rejected":
		return "❌", "red"
	case "success":
		return "🎉", "green"
	case "failure":
		return "💥", "red"
	default:
		return "🔵", "blue"
	}
}

func fallbackStatusField(value string, fallback string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}
	return trimmed
}
