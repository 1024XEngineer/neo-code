package services

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tuistate "neo-code/internal/tui/state"
)

const (
	// RuntimeEventRunContext 是与 runtime 对接时预留的 run context 事件名。
	RuntimeEventRunContext = "run_context"
	// RuntimeEventToolStatus 是与 runtime 对接时预留的 tool status 事件名。
	RuntimeEventToolStatus = "tool_status"
	// RuntimeEventUsage 是与 runtime 对接时预留的 usage 事件名。
	RuntimeEventUsage = "usage"
)

// ToolStateVM 定义 tool 状态在服务层桥接的视图模型。
type ToolStateVM = tuistate.ToolState

// ContextWindowVM 定义上下文窗口在服务层桥接的视图模型。
type ContextWindowVM = tuistate.ContextWindowState

// TokenUsageVM 定义 token 使用统计在服务层桥接的视图模型。
type TokenUsageVM = tuistate.TokenUsageState

// RuntimeRunContextPayload 是 TUI 对 runtime run_context 的预留载荷结构。
type RuntimeRunContextPayload struct {
	Provider string
	Model    string
	Workdir  string
	Mode     string
}

// RuntimeToolStatusPayload 是 TUI 对 runtime tool_status 的预留载荷结构。
type RuntimeToolStatusPayload struct {
	ToolCallID string
	ToolName   string
	Status     string
	Message    string
	DurationMS int64
}

// RuntimeUsageSnapshot 表示 token 使用统计快照。
type RuntimeUsageSnapshot struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}

// RuntimeUsagePayload 是 TUI 对 runtime usage 的预留载荷结构。
type RuntimeUsagePayload struct {
	Delta   RuntimeUsageSnapshot
	Run     RuntimeUsageSnapshot
	Session RuntimeUsageSnapshot
}

// RuntimeSessionContextSnapshot 是 runtime 查询接口 session context 的预留结构。
type RuntimeSessionContextSnapshot struct {
	SessionID string
	Provider  string
	Model     string
	Workdir   string
	Mode      string
}

// RuntimeToolStateSnapshot 是 runtime 查询接口 tool states 的预留结构。
type RuntimeToolStateSnapshot struct {
	ToolCallID string
	ToolName   string
	Status     string
	Message    string
	DurationMS int64
	UpdatedAt  time.Time
}

// RuntimeRunContextSnapshot 是 runtime 查询接口 run context 的预留结构。
type RuntimeRunContextSnapshot struct {
	RunID     string
	SessionID string
	Provider  string
	Model     string
	Workdir   string
	Mode      string
}

// RuntimeRunSnapshot 是 runtime 查询接口 run snapshot 的预留结构。
type RuntimeRunSnapshot struct {
	RunID        string
	SessionID    string
	Context      RuntimeRunContextSnapshot
	ToolStates   []RuntimeToolStateSnapshot
	Usage        RuntimeUsageSnapshot
	SessionUsage RuntimeUsageSnapshot
}

// ParseRunContextPayload 解析 runtime run_context 事件载荷。
func ParseRunContextPayload(payload any) (RuntimeRunContextPayload, bool) {
	var out RuntimeRunContextPayload
	switch typed := payload.(type) {
	case RuntimeRunContextPayload:
		out = typed
	case *RuntimeRunContextPayload:
		if typed == nil {
			return RuntimeRunContextPayload{}, false
		}
		out = *typed
	case map[string]any:
		out = RuntimeRunContextPayload{
			Provider: readMapString(typed, "Provider"),
			Model:    readMapString(typed, "Model"),
			Workdir:  readMapString(typed, "Workdir"),
			Mode:     readMapString(typed, "Mode"),
		}
	default:
		return RuntimeRunContextPayload{}, false
	}
	out.Provider = strings.TrimSpace(out.Provider)
	out.Model = strings.TrimSpace(out.Model)
	out.Workdir = strings.TrimSpace(out.Workdir)
	out.Mode = strings.TrimSpace(out.Mode)
	if out.Provider == "" && out.Model == "" && out.Workdir == "" && out.Mode == "" {
		return RuntimeRunContextPayload{}, false
	}
	return out, true
}

// ParseToolStatusPayload 解析 runtime tool_status 事件载荷。
func ParseToolStatusPayload(payload any) (RuntimeToolStatusPayload, bool) {
	var out RuntimeToolStatusPayload
	switch typed := payload.(type) {
	case RuntimeToolStatusPayload:
		out = typed
	case *RuntimeToolStatusPayload:
		if typed == nil {
			return RuntimeToolStatusPayload{}, false
		}
		out = *typed
	case map[string]any:
		out = RuntimeToolStatusPayload{
			ToolCallID: readMapString(typed, "ToolCallID"),
			ToolName:   readMapString(typed, "ToolName"),
			Status:     readMapString(typed, "Status"),
			Message:    readMapString(typed, "Message"),
			DurationMS: readMapInt64(typed, "DurationMS"),
		}
	default:
		return RuntimeToolStatusPayload{}, false
	}
	out.ToolCallID = strings.TrimSpace(out.ToolCallID)
	out.ToolName = strings.TrimSpace(out.ToolName)
	out.Status = strings.TrimSpace(out.Status)
	out.Message = strings.TrimSpace(out.Message)
	if out.ToolCallID == "" && out.ToolName == "" {
		return RuntimeToolStatusPayload{}, false
	}
	return out, true
}

// ParseUsagePayload 解析 runtime usage 事件载荷。
func ParseUsagePayload(payload any) (RuntimeUsagePayload, bool) {
	var out RuntimeUsagePayload
	switch typed := payload.(type) {
	case RuntimeUsagePayload:
		out = typed
	case *RuntimeUsagePayload:
		if typed == nil {
			return RuntimeUsagePayload{}, false
		}
		out = *typed
	case map[string]any:
		out = RuntimeUsagePayload{
			Delta:   readUsageFromAny(typed["Delta"]),
			Run:     readUsageFromAny(typed["Run"]),
			Session: readUsageFromAny(typed["Session"]),
		}
	default:
		return RuntimeUsagePayload{}, false
	}
	if out.Delta == (RuntimeUsageSnapshot{}) && out.Run == (RuntimeUsageSnapshot{}) && out.Session == (RuntimeUsageSnapshot{}) {
		return RuntimeUsagePayload{}, false
	}
	return out, true
}

// ParseSessionContextSnapshot 解析 runtime 查询接口返回的 session context。
func ParseSessionContextSnapshot(snapshot any) (RuntimeSessionContextSnapshot, bool) {
	var out RuntimeSessionContextSnapshot
	switch typed := snapshot.(type) {
	case RuntimeSessionContextSnapshot:
		out = typed
	case *RuntimeSessionContextSnapshot:
		if typed == nil {
			return RuntimeSessionContextSnapshot{}, false
		}
		out = *typed
	case map[string]any:
		out = RuntimeSessionContextSnapshot{
			SessionID: readMapString(typed, "SessionID"),
			Provider:  readMapString(typed, "Provider"),
			Model:     readMapString(typed, "Model"),
			Workdir:   readMapString(typed, "Workdir"),
			Mode:      readMapString(typed, "Mode"),
		}
	default:
		return RuntimeSessionContextSnapshot{}, false
	}
	out.SessionID = strings.TrimSpace(out.SessionID)
	out.Provider = strings.TrimSpace(out.Provider)
	out.Model = strings.TrimSpace(out.Model)
	out.Workdir = strings.TrimSpace(out.Workdir)
	out.Mode = strings.TrimSpace(out.Mode)
	if out.SessionID == "" && out.Provider == "" && out.Workdir == "" {
		return RuntimeSessionContextSnapshot{}, false
	}
	return out, true
}

// ParseUsageSnapshot 解析 runtime 查询接口返回的 usage 快照。
func ParseUsageSnapshot(snapshot any) (RuntimeUsageSnapshot, bool) {
	usage := readUsageFromAny(snapshot)
	if usage == (RuntimeUsageSnapshot{}) {
		return RuntimeUsageSnapshot{}, false
	}
	return usage, true
}

// ParseRunSnapshot 解析 runtime 查询接口返回的 run snapshot。
func ParseRunSnapshot(snapshot any) (RuntimeRunSnapshot, bool) {
	var out RuntimeRunSnapshot
	switch typed := snapshot.(type) {
	case RuntimeRunSnapshot:
		out = typed
	case *RuntimeRunSnapshot:
		if typed == nil {
			return RuntimeRunSnapshot{}, false
		}
		out = *typed
	case map[string]any:
		out = RuntimeRunSnapshot{
			RunID:        readMapString(typed, "RunID"),
			SessionID:    readMapString(typed, "SessionID"),
			Context:      parseRunContextSnapshotFromAny(typed["Context"]),
			ToolStates:   parseToolStatesFromAny(typed["ToolStates"]),
			Usage:        readUsageFromAny(typed["Usage"]),
			SessionUsage: readUsageFromAny(typed["SessionUsage"]),
		}
	default:
		return RuntimeRunSnapshot{}, false
	}
	out.RunID = strings.TrimSpace(out.RunID)
	out.SessionID = strings.TrimSpace(out.SessionID)
	if out.RunID == "" && out.SessionID == "" {
		return RuntimeRunSnapshot{}, false
	}
	return out, true
}

// MapRunContextPayload 将 run_context 载荷映射为桥接视图模型。
func MapRunContextPayload(runID string, sessionID string, payload RuntimeRunContextPayload) ContextWindowVM {
	return tuistate.ContextWindowState{
		RunID:     strings.TrimSpace(runID),
		SessionID: strings.TrimSpace(sessionID),
		Provider:  strings.TrimSpace(payload.Provider),
		Model:     strings.TrimSpace(payload.Model),
		Workdir:   strings.TrimSpace(payload.Workdir),
		Mode:      strings.TrimSpace(payload.Mode),
	}
}

// MapSessionContextSnapshot 将 session context 查询结果映射为桥接视图模型。
func MapSessionContextSnapshot(snapshot RuntimeSessionContextSnapshot) ContextWindowVM {
	return tuistate.ContextWindowState{
		SessionID: strings.TrimSpace(snapshot.SessionID),
		Provider:  strings.TrimSpace(snapshot.Provider),
		Model:     strings.TrimSpace(snapshot.Model),
		Workdir:   strings.TrimSpace(snapshot.Workdir),
		Mode:      strings.TrimSpace(snapshot.Mode),
	}
}

// MapToolStatusPayload 将 tool_status 载荷映射为桥接视图模型。
func MapToolStatusPayload(payload RuntimeToolStatusPayload) ToolStateVM {
	return tuistate.ToolState{
		ToolCallID: strings.TrimSpace(payload.ToolCallID),
		ToolName:   strings.TrimSpace(payload.ToolName),
		Status:     mapToolLifecycleStatus(payload.Status),
		Message:    strings.TrimSpace(payload.Message),
		DurationMS: payload.DurationMS,
		UpdatedAt:  time.Now(),
	}
}

// MergeToolStates 按 tool_call_id + tool_name 去重合并状态，处理重复与乱序事件。
func MergeToolStates(existing []ToolStateVM, incoming ToolStateVM, limit int) []ToolStateVM {
	if limit <= 0 {
		limit = 16
	}

	incomingCallID := strings.TrimSpace(incoming.ToolCallID)
	incomingName := strings.TrimSpace(incoming.ToolName)
	updated := append([]ToolStateVM(nil), existing...)
	for i := range updated {
		if strings.EqualFold(strings.TrimSpace(updated[i].ToolCallID), incomingCallID) &&
			strings.EqualFold(strings.TrimSpace(updated[i].ToolName), incomingName) {
			updated[i] = incoming
			return updated
		}
	}

	updated = append(updated, incoming)
	if len(updated) > limit {
		return append([]ToolStateVM(nil), updated[len(updated)-limit:]...)
	}
	return updated
}

// MapUsagePayload 将 usage 载荷映射为桥接视图模型。
func MapUsagePayload(payload RuntimeUsagePayload) TokenUsageVM {
	return tuistate.TokenUsageState{
		RunInputTokens:      payload.Run.InputTokens,
		RunOutputTokens:     payload.Run.OutputTokens,
		RunTotalTokens:      payload.Run.TotalTokens,
		SessionInputTokens:  payload.Session.InputTokens,
		SessionOutputTokens: payload.Session.OutputTokens,
		SessionTotalTokens:  payload.Session.TotalTokens,
	}
}

// MapUsageSnapshot 将 usage 快照映射为 TokenUsageVM（保留当前 run 统计不变）。
func MapUsageSnapshot(snapshot RuntimeUsageSnapshot, current TokenUsageVM) TokenUsageVM {
	current.SessionInputTokens = snapshot.InputTokens
	current.SessionOutputTokens = snapshot.OutputTokens
	current.SessionTotalTokens = snapshot.TotalTokens
	return current
}

// MapRunSnapshot 将 run snapshot 映射为桥接视图数据。
func MapRunSnapshot(snapshot RuntimeRunSnapshot) (ContextWindowVM, []ToolStateVM, TokenUsageVM) {
	context := tuistate.ContextWindowState{
		RunID:     strings.TrimSpace(snapshot.RunID),
		SessionID: strings.TrimSpace(snapshot.SessionID),
		Provider:  strings.TrimSpace(snapshot.Context.Provider),
		Model:     strings.TrimSpace(snapshot.Context.Model),
		Workdir:   strings.TrimSpace(snapshot.Context.Workdir),
		Mode:      strings.TrimSpace(snapshot.Context.Mode),
	}

	tools := make([]ToolStateVM, 0, len(snapshot.ToolStates))
	for _, item := range snapshot.ToolStates {
		tools = append(tools, tuistate.ToolState{
			ToolCallID: strings.TrimSpace(item.ToolCallID),
			ToolName:   strings.TrimSpace(item.ToolName),
			Status:     mapToolLifecycleStatus(item.Status),
			Message:    strings.TrimSpace(item.Message),
			DurationMS: item.DurationMS,
			UpdatedAt:  item.UpdatedAt,
		})
	}

	usage := tuistate.TokenUsageState{
		RunInputTokens:      snapshot.Usage.InputTokens,
		RunOutputTokens:     snapshot.Usage.OutputTokens,
		RunTotalTokens:      snapshot.Usage.TotalTokens,
		SessionInputTokens:  snapshot.SessionUsage.InputTokens,
		SessionOutputTokens: snapshot.SessionUsage.OutputTokens,
		SessionTotalTokens:  snapshot.SessionUsage.TotalTokens,
	}

	return context, tools, usage
}

func mapToolLifecycleStatus(status string) tuistate.ToolLifecycleStatus {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case string(tuistate.ToolLifecyclePlanned):
		return tuistate.ToolLifecyclePlanned
	case string(tuistate.ToolLifecycleRunning):
		return tuistate.ToolLifecycleRunning
	case string(tuistate.ToolLifecycleSucceeded):
		return tuistate.ToolLifecycleSucceeded
	case string(tuistate.ToolLifecycleFailed):
		return tuistate.ToolLifecycleFailed
	default:
		return tuistate.ToolLifecycleRunning
	}
}

func readUsageFromAny(value any) RuntimeUsageSnapshot {
	switch typed := value.(type) {
	case RuntimeUsageSnapshot:
		return typed
	case *RuntimeUsageSnapshot:
		if typed == nil {
			return RuntimeUsageSnapshot{}
		}
		return *typed
	case map[string]any:
		return RuntimeUsageSnapshot{
			InputTokens:  readMapInt(typed, "InputTokens"),
			OutputTokens: readMapInt(typed, "OutputTokens"),
			TotalTokens:  readMapInt(typed, "TotalTokens"),
		}
	default:
		return RuntimeUsageSnapshot{}
	}
}

func parseRunContextSnapshotFromAny(value any) RuntimeRunContextSnapshot {
	switch typed := value.(type) {
	case RuntimeRunContextSnapshot:
		return typed
	case *RuntimeRunContextSnapshot:
		if typed == nil {
			return RuntimeRunContextSnapshot{}
		}
		return *typed
	case map[string]any:
		return RuntimeRunContextSnapshot{
			RunID:     readMapString(typed, "RunID"),
			SessionID: readMapString(typed, "SessionID"),
			Provider:  readMapString(typed, "Provider"),
			Model:     readMapString(typed, "Model"),
			Workdir:   readMapString(typed, "Workdir"),
			Mode:      readMapString(typed, "Mode"),
		}
	default:
		return RuntimeRunContextSnapshot{}
	}
}

func parseToolStatesFromAny(value any) []RuntimeToolStateSnapshot {
	switch typed := value.(type) {
	case []RuntimeToolStateSnapshot:
		return append([]RuntimeToolStateSnapshot(nil), typed...)
	case []any:
		out := make([]RuntimeToolStateSnapshot, 0, len(typed))
		for _, item := range typed {
			if parsed, ok := parseToolStateFromAny(item); ok {
				out = append(out, parsed)
			}
		}
		return out
	default:
		return nil
	}
}

func parseToolStateFromAny(value any) (RuntimeToolStateSnapshot, bool) {
	switch typed := value.(type) {
	case RuntimeToolStateSnapshot:
		return typed, true
	case *RuntimeToolStateSnapshot:
		if typed == nil {
			return RuntimeToolStateSnapshot{}, false
		}
		return *typed, true
	case map[string]any:
		return RuntimeToolStateSnapshot{
			ToolCallID: readMapString(typed, "ToolCallID"),
			ToolName:   readMapString(typed, "ToolName"),
			Status:     readMapString(typed, "Status"),
			Message:    readMapString(typed, "Message"),
			DurationMS: readMapInt64(typed, "DurationMS"),
			UpdatedAt:  readMapTime(typed, "UpdatedAt"),
		}, true
	default:
		return RuntimeToolStateSnapshot{}, false
	}
}

func readMapString(m map[string]any, key string) string {
	value, ok := m[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", typed))
	}
}

func readMapInt(m map[string]any, key string) int {
	value, ok := m[key]
	if !ok || value == nil {
		return 0
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case int32:
		return int(typed)
	case float64:
		return int(typed)
	case float32:
		return int(typed)
	case string:
		number, err := strconv.Atoi(strings.TrimSpace(typed))
		if err != nil {
			return 0
		}
		return number
	default:
		return 0
	}
}

func readMapInt64(m map[string]any, key string) int64 {
	value, ok := m[key]
	if !ok || value == nil {
		return 0
	}
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case int32:
		return int64(typed)
	case float64:
		return int64(typed)
	case float32:
		return int64(typed)
	case string:
		number, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		if err != nil {
			return 0
		}
		return number
	default:
		return 0
	}
}

func readMapTime(m map[string]any, key string) time.Time {
	value, ok := m[key]
	if !ok || value == nil {
		return time.Time{}
	}
	switch typed := value.(type) {
	case time.Time:
		return typed
	default:
		return time.Time{}
	}
}
