package protocol

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
)

// WakeStartupInput 表示由外部唤醒链路传递给 TUI 启动阶段的一次性自动提交输入。
type WakeStartupInput struct {
	Text    string `json:"text"`
	Workdir string `json:"workdir,omitempty"`
}

// EncodeWakeStartupInput 将启动输入编码为 URL-safe base64 字符串，供命令行参数透传。
func EncodeWakeStartupInput(input WakeStartupInput) (string, error) {
	normalized := WakeStartupInput{
		Text:    strings.TrimSpace(input.Text),
		Workdir: strings.TrimSpace(input.Workdir),
	}
	if normalized.Text == "" {
		return "", errors.New("wake startup input text is empty")
	}
	payload, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

// DecodeWakeStartupInput 从 URL-safe base64 字符串恢复启动输入，并执行最小字段校验。
func DecodeWakeStartupInput(encoded string) (WakeStartupInput, error) {
	trimmed := strings.TrimSpace(encoded)
	if trimmed == "" {
		return WakeStartupInput{}, errors.New("wake startup input payload is empty")
	}
	raw, err := base64.RawURLEncoding.DecodeString(trimmed)
	if err != nil {
		return WakeStartupInput{}, err
	}
	var input WakeStartupInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return WakeStartupInput{}, err
	}
	input.Text = strings.TrimSpace(input.Text)
	input.Workdir = strings.TrimSpace(input.Workdir)
	if input.Text == "" {
		return WakeStartupInput{}, errors.New("wake startup input text is empty")
	}
	return input, nil
}
