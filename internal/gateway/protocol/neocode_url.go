package protocol

import "strings"

const (
	// WakeActionReview 表示 review 唤醒动作。
	WakeActionReview = "review"
	// WakeActionRun 表示 run 唤醒动作。
	WakeActionRun = "run"
)

var supportedWakeActionSet = map[string]struct{}{
	WakeActionReview: {},
	WakeActionRun:    {},
}

// WakeIntent 表示标准化后的外部唤醒意图。
type WakeIntent struct {
	Action    string            `json:"action"`
	SessionID string            `json:"session_id,omitempty"`
	Workdir   string            `json:"workdir,omitempty"`
	Params    map[string]string `json:"params,omitempty"`
	RawURL    string            `json:"raw_url"`
}

// IsSupportedWakeAction 判断动作是否属于网关允许的唤醒动作集合。
func IsSupportedWakeAction(action string) bool {
	_, exists := supportedWakeActionSet[strings.ToLower(strings.TrimSpace(action))]
	return exists
}
