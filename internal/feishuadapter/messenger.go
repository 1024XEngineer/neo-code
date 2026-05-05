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
	return m.sendMessage(ctx, chatID, "text", string(content))
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
	return m.sendMessage(ctx, chatID, "interactive", string(content))
}

// sendMessage 统一封装飞书消息发送请求，复用鉴权与错误处理。
func (m *feishuMessenger) sendMessage(ctx context.Context, chatID string, msgType string, content string) error {
	token, err := m.tenantAccessToken(ctx)
	if err != nil {
		return err
	}
	body := map[string]string{
		"receive_id": chatID,
		"msg_type":   msgType,
		"content":    content,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	url := strings.TrimRight(m.baseURL, "/") + "/open-apis/im/v1/messages?receive_id_type=chat_id"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var payload struct {
		Code    int    `json:"code"`
		Message string `json:"msg"`
	}
	decodeErr := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload)
	if resp.StatusCode/100 != 2 {
		if decodeErr == nil {
			return fmt.Errorf("send feishu message failed: status=%d code=%d message=%s", resp.StatusCode, payload.Code, payload.Message)
		}
		return fmt.Errorf("send feishu message failed: status=%d body=invalid_json", resp.StatusCode)
	}
	if decodeErr != nil {
		return fmt.Errorf("send feishu message failed: invalid response body: %w", decodeErr)
	}
	if payload.Code != 0 {
		return fmt.Errorf("send feishu message failed: status=%d code=%d message=%s", resp.StatusCode, payload.Code, payload.Message)
	}
	return nil
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
