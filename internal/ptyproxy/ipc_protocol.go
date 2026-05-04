package ptyproxy

import (
	"encoding/json"
	"strings"
)

const (
	diagCommandDiagnose   = "diagnose"
	diagCommandAutoOn     = "auto_on"
	diagCommandAutoOff    = "auto_off"
	diagCommandAutoStatus = "auto_status"
)

type diagIPCRequest struct {
	Cmd     string          `json:"cmd"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type diagIPCResponse struct {
	OK          bool   `json:"ok"`
	Message     string `json:"message,omitempty"`
	AutoEnabled bool   `json:"auto_enabled,omitempty"`
}

// normalizeDiagIPCCommand 归一化 IPC 指令名，避免大小写或空白导致分支漂移。
func normalizeDiagIPCCommand(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}
