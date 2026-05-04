package ptyproxy

import (
	"strconv"
	"strings"
)

const (
	oscBEL         = byte('\a')
	oscEscape      = byte(0x1b)
	oscCloseByte   = byte('\\')
	oscPrefix      = "\x1b]133;"
	tmuxDCSOpen    = "\x1bPtmux;"
	maxOSCLeftover = 4096
)

// ShellEventType 表示代理捕获到的 Shell Integration 事件类型。
type ShellEventType string

const (
	// ShellEventPromptReady 对应 OSC 133;A，表示 prompt 已就绪。
	ShellEventPromptReady ShellEventType = "prompt_ready"
	// ShellEventCommandStart 对应 OSC 133;C，表示命令开始执行。
	ShellEventCommandStart ShellEventType = "command_start"
	// ShellEventCommandDone 对应 OSC 133;D;<exit_code>，表示命令结束。
	ShellEventCommandDone ShellEventType = "command_done"
)

// ShellEvent 描述从输出字节流中提取的结构化 shell 事件。
type ShellEvent struct {
	Type     ShellEventType
	ExitCode int
}

// OSC133Parser 负责从 PTY 字节流中剥离 OSC 133 并产出结构化事件。
type OSC133Parser struct {
	leftover []byte
}

// Feed 解析新输入分片，返回去除控制序列后的可显示字节与提取出的事件。
func (p *OSC133Parser) Feed(chunk []byte) ([]byte, []ShellEvent) {
	if len(chunk) == 0 {
		return nil, nil
	}
	buffer := append(append([]byte(nil), p.leftover...), chunk...)
	p.leftover = nil

	output := make([]byte, 0, len(buffer))
	events := make([]ShellEvent, 0, 4)

	for index := 0; index < len(buffer); {
		// 尝试剥离 tmux passthrough 包裹。
		if hasPrefixAt(buffer, index, tmuxDCSOpen) {
			end, ok := findESCBackslash(buffer, index+len(tmuxDCSOpen))
			if !ok {
				p.leftover = keepOSCLeftover(buffer[index:])
				break
			}
			inner := buffer[index+len(tmuxDCSOpen) : end]
			// tmux 透传会将 ESC 转义为双 ESC，这里先做一次解包还原。
			if len(inner) > 1 && inner[0] == oscEscape && hasPrefixAt(inner, 1, oscPrefix) {
				inner = inner[1:]
			}
			if hasPrefixAt(inner, 0, oscPrefix) {
				payload, payloadOK := parseOSCPayload(inner)
				if payloadOK {
					if event, matched := parseOSC133Event(payload); matched {
						events = append(events, event)
					}
					index = end + 2
					continue
				}
			}
		}

		// 直接解析 OSC 133。
		if hasPrefixAt(buffer, index, oscPrefix) {
			payload, next, ok := parseOSCFromBuffer(buffer, index)
			if !ok {
				p.leftover = keepOSCLeftover(buffer[index:])
				break
			}
			if event, matched := parseOSC133Event(payload); matched {
				events = append(events, event)
			}
			index = next
			continue
		}

		output = append(output, buffer[index])
		index++
	}

	return output, events
}

// parseOSCFromBuffer 从完整缓冲区中读取一个 OSC payload。
func parseOSCFromBuffer(buffer []byte, start int) (string, int, bool) {
	cursor := start + len(oscPrefix)
	for cursor < len(buffer) {
		if buffer[cursor] == oscBEL {
			return string(buffer[start+len(oscPrefix) : cursor]), cursor + 1, true
		}
		if cursor+1 < len(buffer) && buffer[cursor] == oscEscape && buffer[cursor+1] == oscCloseByte {
			return string(buffer[start+len(oscPrefix) : cursor]), cursor + 2, true
		}
		cursor++
	}
	return "", start, false
}

// parseOSCPayload 解析独立 payload 片段（用于 tmux 内嵌场景）。
func parseOSCPayload(raw []byte) (string, bool) {
	payload, _, ok := parseOSCFromBuffer(raw, 0)
	return payload, ok
}

// parseOSC133Event 将 OSC payload 转换为结构化 shell 事件。
func parseOSC133Event(payload string) (ShellEvent, bool) {
	trimmed := strings.TrimSpace(payload)
	if trimmed == "" {
		return ShellEvent{}, false
	}
	switch {
	case strings.HasPrefix(trimmed, "A"):
		return ShellEvent{Type: ShellEventPromptReady}, true
	case strings.HasPrefix(trimmed, "C"):
		return ShellEvent{Type: ShellEventCommandStart}, true
	case strings.HasPrefix(trimmed, "D"):
		parts := strings.Split(trimmed, ";")
		exitCode := 0
		if len(parts) >= 2 {
			if parsed, err := strconv.Atoi(strings.TrimSpace(parts[1])); err == nil {
				exitCode = parsed
			}
		}
		return ShellEvent{
			Type:     ShellEventCommandDone,
			ExitCode: exitCode,
		}, true
	default:
		return ShellEvent{}, false
	}
}

// findESCBackslash 在缓冲区中查找 ESC\ 结束序列。
func findESCBackslash(buffer []byte, start int) (int, bool) {
	for index := start; index+1 < len(buffer); index++ {
		if buffer[index] == oscEscape && buffer[index+1] == oscCloseByte {
			return index, true
		}
	}
	return 0, false
}

// hasPrefixAt 判断给定下标处是否命中指定前缀。
func hasPrefixAt(buffer []byte, start int, prefix string) bool {
	if start < 0 || start+len(prefix) > len(buffer) {
		return false
	}
	return string(buffer[start:start+len(prefix)]) == prefix
}

// keepOSCLeftover 控制未完成片段缓存上限，防止异常流导致内存无限增长。
func keepOSCLeftover(raw []byte) []byte {
	if len(raw) <= maxOSCLeftover {
		return append([]byte(nil), raw...)
	}
	return append([]byte(nil), raw[len(raw)-maxOSCLeftover:]...)
}
