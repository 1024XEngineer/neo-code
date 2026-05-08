package ptyproxy

import (
	"strings"
	"sync"
	"sync/atomic"
)

// diagnoseToolArgs 定义诊断工具调用参数结构。
type diagnoseToolArgs struct {
	ErrorLog    string            `json:"error_log"`
	OSEnv       map[string]string `json:"os_env"`
	CommandText string            `json:"command_text"`
	ExitCode    int               `json:"exit_code"`
}

// diagnoseToolResult 定义诊断工具结构化返回。
type diagnoseToolResult struct {
	Confidence            float64  `json:"confidence"`
	RootCause             string   `json:"root_cause"`
	FixCommands           []string `json:"fix_commands"`
	InvestigationCommands []string `json:"investigation_commands"`
}

// diagnoseTrigger 描述一次诊断触发上下文。
type diagnoseTrigger struct {
	CommandText string
	ExitCode    int
	OutputText  string
}

// diagnoseJob 表示一次手动或自动诊断任务，供不同平台的 shell 代理复用。
type diagnoseJob struct {
	Trigger diagnoseTrigger
	IsAuto  bool
}

// diagnosisTriggerStore 保存最近一条完整命令的诊断上下文，供手动 diag 复用。
type diagnosisTriggerStore struct {
	mu      sync.Mutex
	trigger diagnoseTrigger
	ready   bool
}

// Remember 记录一条可用于诊断的命令窗口，空上下文会被忽略。
func (s *diagnosisTriggerStore) Remember(trigger diagnoseTrigger) {
	if s == nil || !hasDiagnoseTriggerContext(trigger) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trigger = trigger
	s.ready = true
}

// Last 返回最近记录的命令诊断上下文。
func (s *diagnosisTriggerStore) Last() (diagnoseTrigger, bool) {
	if s == nil {
		return diagnoseTrigger{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ready {
		return diagnoseTrigger{}, false
	}
	return s.trigger, true
}

// hasDiagnoseTriggerContext 判断触发上下文是否携带了可诊断的信息。
func hasDiagnoseTriggerContext(trigger diagnoseTrigger) bool {
	return strings.TrimSpace(trigger.CommandText) != "" || strings.TrimSpace(trigger.OutputText) != "" || trigger.ExitCode != 0
}

// autoRuntimeState 维护 shell 自动诊断运行时状态。
type autoRuntimeState struct {
	Enabled  atomic.Bool
	OSCReady atomic.Bool
}

// commandTracker 从宿主输入流中记录最近一次提交的命令文本。
type commandTracker struct {
	mu          sync.Mutex
	lineBuffer  []byte
	lastCommand string
	escapeState uint8
}

// Observe 记录输入字节并忽略常见控制序列，换行时提交当前命令。
func (t *commandTracker) Observe(payload []byte) {
	if t == nil || len(payload) == 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, b := range payload {
		switch t.escapeState {
		case 1:
			switch b {
			case '[':
				t.escapeState = 2
			case ']':
				t.escapeState = 3
			default:
				t.escapeState = 0
			}
			continue
		case 2:
			if b >= 0x40 && b <= 0x7e {
				t.escapeState = 0
			}
			continue
		case 3:
			if b == 0x07 {
				t.escapeState = 0
				continue
			}
			if b == 0x1b {
				t.escapeState = 4
				continue
			}
			continue
		case 4:
			if b == '\\' {
				t.escapeState = 0
				continue
			}
			t.escapeState = 3
			continue
		}

		switch b {
		case 0x1b:
			t.escapeState = 1
		case '\r', '\n':
			current := strings.TrimSpace(string(t.lineBuffer))
			if current != "" {
				t.lastCommand = current
			}
			t.lineBuffer = t.lineBuffer[:0]
		case 0x08, 0x7f:
			if len(t.lineBuffer) > 0 {
				t.lineBuffer = t.lineBuffer[:len(t.lineBuffer)-1]
			}
		default:
			if b >= 0x20 {
				t.lineBuffer = append(t.lineBuffer, b)
			}
		}
	}
}

// LastCommand 返回最近一次换行提交的命令文本。
func (t *commandTracker) LastCommand() string {
	if t == nil {
		return ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.TrimSpace(t.lastCommand)
}
