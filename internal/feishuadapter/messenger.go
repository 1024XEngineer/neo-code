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

// SendPermissionCard 向指定 chat_id 发送审批卡片，返回 message_id 用于后续更新。
func (m *feishuMessenger) SendPermissionCard(ctx context.Context, chatID string, payload PermissionCardPayload) (string, error) {
	card := buildPermissionCard(payload)
	content, err := json.Marshal(card)
	if err != nil {
		return "", err
	}
	return m.sendMessage(ctx, chatID, "interactive", string(content))
}

// UpdatePermissionCard 根据 card_id 覆盖更新审批卡片为已处理状态。
func (m *feishuMessenger) UpdatePermissionCard(ctx context.Context, cardID string, payload ResolvedPermissionCardPayload) error {
	token, err := m.tenantAccessToken(ctx)
	if err != nil {
		return err
	}
	card := buildResolvedPermissionCard(payload)
	content, err := json.Marshal(card)
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

// SendUserQuestionCard 向指定 chat_id 发送 ask_user 交互卡片，返回 message_id 供后续更新。
func (m *feishuMessenger) SendUserQuestionCard(ctx context.Context, chatID string, payload UserQuestionCardPayload) (string, error) {
	card := buildUserQuestionCard(payload)
	content, err := json.Marshal(card)
	if err != nil {
		return "", err
	}
	return m.sendMessage(ctx, chatID, "interactive", string(content))
}

// UpdateUserQuestionCard 根据 card_id 覆盖更新 ask_user 卡片为已处理状态。
func (m *feishuMessenger) UpdateUserQuestionCard(ctx context.Context, cardID string, payload ResolvedUserQuestionCardPayload) error {
	token, err := m.tenantAccessToken(ctx)
	if err != nil {
		return err
	}
	card := buildResolvedUserQuestionCard(payload)
	content, err := json.Marshal(card)
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
	result := fallbackStatusField(payload.Result, "pending")

	statusIcon, statusColor := statusIconAndColor(status)
	resultIcon, _ := statusIconAndColor(result)

	elements := []map[string]any{
		statusNoteElement(taskName),
		statusBarElement(statusIcon, "状态", status),
	}

	if len(payload.ApprovalRecords) > 0 {
		elements = append(elements, buildApprovalRecordsElement(payload.ApprovalRecords, payload.PendingCount))
	} else {
		approval := fallbackStatusField(payload.ApprovalStatus, "none")
		approvalIcon, _ := statusIconAndColor(approval)
		elements = append(elements, statusBarElement(approvalIcon, "审批", approval))
	}

	elements = append(elements, statusBarElement(resultIcon, "结果", result))

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
		"tag":              "column_set",
		"flex_mode":        "bisect",
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

func buildApprovalRecordsElement(records []ApprovalRecord, pendingCount int) map[string]any {
	approvedCount := 0
	rejectedCount := 0
	for _, r := range records {
		switch r.Decision {
		case "allow_once":
			approvedCount++
		case "reject":
			rejectedCount++
		}
	}

	summaryParts := make([]string, 0, 3)
	if approvedCount > 0 {
		summaryParts = append(summaryParts, fmt.Sprintf("%d 通过", approvedCount))
	}
	if rejectedCount > 0 {
		summaryParts = append(summaryParts, fmt.Sprintf("%d 拒绝", rejectedCount))
	}
	if pendingCount > 0 {
		summaryParts = append(summaryParts, fmt.Sprintf("%d 等待", pendingCount))
	}
	summaryText := strings.Join(summaryParts, "，")

	detailLines := make([]string, 0, len(records))
	for _, r := range records {
		icon, _ := statusIconAndColor(r.Decision)
		label := fallbackStatusField(r.ToolName, "unknown_tool")
		detailLines = append(detailLines, fmt.Sprintf("%s %s → *%s*", icon, label, r.Decision))
	}
	fullText := fmt.Sprintf("**%s**\n%s", summaryText, strings.Join(detailLines, "\n"))

	return map[string]any{
		"tag":  "div",
		"text": map[string]any{"tag": "lark_md", "content": fullText},
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

// buildPermissionCard 构造带工具信息的审批卡片。
func buildPermissionCard(payload PermissionCardPayload) map[string]any {
	infoLines := make([]string, 0, 3)
	if toolName := strings.TrimSpace(payload.ToolName); toolName != "" {
		infoLines = append(infoLines, "**工具**: "+toolName)
	}
	if op := strings.TrimSpace(payload.Operation); op != "" {
		infoLines = append(infoLines, "**操作**: "+op)
	}
	if target := strings.TrimSpace(payload.Target); target != "" {
		infoLines = append(infoLines, "**目标**: "+target)
	}
	body := strings.Join(infoLines, "\n")
	if reason := strings.TrimSpace(payload.Message); reason != "" {
		if body != "" {
			body += "\n\n**理由**: " + reason
		} else {
			body = reason
		}
	}

	elements := []map[string]any{
		{
			"tag":  "div",
			"text": map[string]any{"tag": "lark_md", "content": body},
		},
		{
			"tag": "action",
			"actions": []map[string]any{
				{
					"tag":  "button",
					"text": map[string]any{"tag": "plain_text", "content": "允许一次"},
					"type": "primary",
					"value": map[string]string{
						"action_type": "permission",
						"decision":    "allow_once",
						"request_id":  payload.RequestID,
					},
				},
				{
					"tag":  "button",
					"text": map[string]any{"tag": "plain_text", "content": "拒绝"},
					"type": "default",
					"value": map[string]string{
						"action_type": "permission",
						"decision":    "reject",
						"request_id":  payload.RequestID,
					},
				},
			},
		},
	}

	return map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": map[string]any{
			"title":    map[string]string{"tag": "plain_text", "content": "工具审批"},
			"template": "yellow",
		},
		"elements": elements,
	}
}

// buildResolvedPermissionCard 构造已处理的审批卡片（去掉按钮，显示结果）。
func buildResolvedPermissionCard(payload ResolvedPermissionCardPayload) map[string]any {
	infoLines := make([]string, 0, 3)
	if toolName := strings.TrimSpace(payload.ToolName); toolName != "" {
		infoLines = append(infoLines, "**工具**: "+toolName)
	}
	if op := strings.TrimSpace(payload.Operation); op != "" {
		infoLines = append(infoLines, "**操作**: "+op)
	}
	if target := strings.TrimSpace(payload.Target); target != "" {
		infoLines = append(infoLines, "**目标**: "+target)
	}
	body := strings.Join(infoLines, "\n")
	if reason := strings.TrimSpace(payload.Message); reason != "" {
		if body != "" {
			body += "\n\n**理由**: " + reason
		} else {
			body = reason
		}
	}

	resultIcon := "✅"
	resultText := "已通过"
	headerColor := "green"
	if !payload.Approved {
		resultIcon = "❌"
		resultText = "已拒绝"
		headerColor = "red"
	}

	body += "\n\n" + resultIcon + " **" + resultText + "**"

	return map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": map[string]any{
			"title":    map[string]string{"tag": "plain_text", "content": "工具审批"},
			"template": headerColor,
		},
		"elements": []map[string]any{
			{
				"tag":  "div",
				"text": map[string]any{"tag": "lark_md", "content": body},
			},
		},
	}
}

// buildUserQuestionCard 构造 ask_user 交互卡片，支持单选按钮与跳过按钮。
func buildUserQuestionCard(payload UserQuestionCardPayload) map[string]any {
	title := strings.TrimSpace(payload.Title)
	if title == "" {
		title = "请回答问题"
	}
	description := strings.TrimSpace(payload.Description)
	kind := strings.TrimSpace(strings.ToLower(payload.Kind))

	bodyLines := make([]string, 0, 4)
	if description != "" {
		bodyLines = append(bodyLines, description)
	}
	if kind == "multi_choice" {
		bodyLines = append(bodyLines, "请通过消息回复：回答 "+strings.TrimSpace(payload.RequestID)+" <选项1,选项2>")
	} else if kind == "text" {
		bodyLines = append(bodyLines, "请通过消息回复：回答 "+strings.TrimSpace(payload.RequestID)+" <内容>")
	}
	if len(bodyLines) == 0 {
		bodyLines = append(bodyLines, "请完成以下问题回答。")
	}

	elements := []map[string]any{
		{
			"tag":  "div",
			"text": map[string]any{"tag": "lark_md", "content": strings.Join(bodyLines, "\n\n")},
		},
	}

	actions := make([]map[string]any, 0, len(payload.Options)+1)
	if kind == "single_choice" {
		for _, option := range payload.Options {
			label := strings.TrimSpace(option.Label)
			if label == "" {
				continue
			}
			actions = append(actions, map[string]any{
				"tag":  "button",
				"text": map[string]any{"tag": "plain_text", "content": label},
				"type": "default",
				"value": map[string]string{
					"action_type": "user_question",
					"request_id":  strings.TrimSpace(payload.RequestID),
					"status":      "answered",
					"value":       label,
				},
			})
		}
	}
	if payload.AllowSkip {
		actions = append(actions, map[string]any{
			"tag":  "button",
			"text": map[string]any{"tag": "plain_text", "content": "跳过"},
			"type": "default",
			"value": map[string]string{
				"action_type": "user_question",
				"request_id":  strings.TrimSpace(payload.RequestID),
				"status":      "skipped",
			},
		})
	}
	if len(actions) > 0 {
		elements = append(elements, map[string]any{
			"tag":     "action",
			"actions": actions,
		})
	}

	return map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": map[string]any{
			"title":    map[string]string{"tag": "plain_text", "content": title},
			"template": "wathet",
		},
		"elements": elements,
	}
}

// buildResolvedUserQuestionCard 构造 ask_user 已处理卡片，去掉交互按钮并显示终态。
func buildResolvedUserQuestionCard(payload ResolvedUserQuestionCardPayload) map[string]any {
	title := strings.TrimSpace(payload.Title)
	if title == "" {
		title = "用户问题"
	}
	status := strings.TrimSpace(strings.ToLower(payload.Status))
	statusIcon := "✅"
	statusText := "已回答"
	headerColor := "green"
	switch status {
	case "skipped":
		statusIcon = "⏭️"
		statusText = "已跳过"
		headerColor = "yellow"
	case "timeout":
		statusIcon = "⏰"
		statusText = "已超时"
		headerColor = "red"
	}

	body := statusIcon + " **" + statusText + "**"
	if summary := strings.TrimSpace(payload.Summary); summary != "" {
		body += "\n\n" + summary
	}

	return map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": map[string]any{
			"title":    map[string]string{"tag": "plain_text", "content": title},
			"template": headerColor,
		},
		"elements": []map[string]any{
			{
				"tag":  "div",
				"text": map[string]any{"tag": "lark_md", "content": body},
			},
		},
	}
}

func fallbackStatusField(value string, fallback string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}
	return trimmed
}
