package ptyproxy

import "strings"

const maxAltScreenLeftover = maxOSCLeftover

// altScreenState 维护备用屏幕识别与自动诊断抑制窗口状态。
type altScreenState struct {
	guardEnabled              bool
	inAltScreen               bool
	suppressNextAutoAfterExit bool
	leftover                  []byte
}

// newAltScreenState 创建全屏抑制状态机实例。
func newAltScreenState(guardEnabled bool) *altScreenState {
	return &altScreenState{guardEnabled: guardEnabled}
}

// Observe 扫描 PTY 原始字节流，识别备用屏幕切换序列并更新状态。
func (s *altScreenState) Observe(payload []byte) {
	if s == nil || !s.guardEnabled || len(payload) == 0 {
		return
	}
	buffer := append(append([]byte(nil), s.leftover...), payload...)
	s.leftover = nil

	for index := 0; index < len(buffer); {
		if hasPrefixAt(buffer, index, tmuxDCSOpen) {
			end, ok := findESCBackslash(buffer, index+len(tmuxDCSOpen))
			if !ok {
				s.leftover = keepAltScreenLeftover(buffer[index:])
				return
			}
			s.observeTmuxPassthrough(buffer[index+len(tmuxDCSOpen) : end])
			index = end + 2
			continue
		}

		if buffer[index] == oscEscape && index+1 < len(buffer) && buffer[index+1] == '[' {
			next, complete := s.consumeCSISequence(buffer, index)
			if !complete {
				s.leftover = keepAltScreenLeftover(buffer[index:])
				return
			}
			index = next
			continue
		}
		index++
	}
	s.leftover = trailingAltScreenPrefix(buffer)
}

// ShouldSuppressAutoTrigger 判断当前是否应屏蔽自动诊断，并按需消耗一次性退出保护窗口。
func (s *altScreenState) ShouldSuppressAutoTrigger(consume bool) bool {
	if s == nil || !s.guardEnabled {
		return false
	}
	if s.inAltScreen {
		return true
	}
	if s.suppressNextAutoAfterExit {
		if consume {
			s.suppressNextAutoAfterExit = false
		}
		return true
	}
	return false
}

// observeTmuxPassthrough 处理 tmux DCS passthrough 中的备用屏幕控制序列。
func (s *altScreenState) observeTmuxPassthrough(inner []byte) {
	if s == nil || len(inner) == 0 {
		return
	}
	decoded := decodeTmuxEscapedSequence(inner)
	s.scanCSIWithoutLeftover(decoded)
}

// decodeTmuxEscapedSequence 还原 tmux passthrough 中双 ESC 转义的原始序列。
func decodeTmuxEscapedSequence(inner []byte) []byte {
	if len(inner) == 0 {
		return nil
	}
	decoded := make([]byte, 0, len(inner))
	for index := 0; index < len(inner); index++ {
		current := inner[index]
		if current == oscEscape && index+1 < len(inner) && inner[index+1] == oscEscape {
			decoded = append(decoded, oscEscape)
			index++
			continue
		}
		decoded = append(decoded, current)
	}
	return decoded
}

// scanCSIWithoutLeftover 解析完整缓冲区中的 CSI 序列，不维护跨包残留状态。
func (s *altScreenState) scanCSIWithoutLeftover(buffer []byte) {
	for index := 0; index < len(buffer); {
		if buffer[index] == oscEscape && index+1 < len(buffer) && buffer[index+1] == '[' {
			next, complete := s.consumeCSISequence(buffer, index)
			if !complete {
				return
			}
			index = next
			continue
		}
		index++
	}
}

// consumeCSISequence 解析一段 CSI 序列并据此更新备用屏幕状态。
func (s *altScreenState) consumeCSISequence(buffer []byte, start int) (int, bool) {
	cursor := start + 2
	for cursor < len(buffer) {
		current := buffer[cursor]
		if current >= 0x40 && current <= 0x7e {
			s.updateFromCSI(buffer[start+2:cursor], current)
			return cursor + 1, true
		}
		// CSI 参数区与中间区允许范围为 0x20~0x3f。
		if current < 0x20 || current > 0x3f {
			return start + 1, true
		}
		cursor++
	}
	return start, false
}

// updateFromCSI 提取 `CSI ? 47/1047/1049 h/l` 并推进状态机。
func (s *altScreenState) updateFromCSI(params []byte, final byte) {
	if s == nil {
		return
	}
	if final != 'h' && final != 'l' {
		return
	}
	if len(params) == 0 || params[0] != '?' {
		return
	}
	if !containsAltScreenMode(string(params[1:])) {
		return
	}

	if final == 'h' {
		s.inAltScreen = true
		// 新一轮全屏会话开始后，旧的一次性保护窗口失效。
		s.suppressNextAutoAfterExit = false
		return
	}

	wasInAltScreen := s.inAltScreen
	s.inAltScreen = false
	if wasInAltScreen {
		s.suppressNextAutoAfterExit = true
	}
}

// containsAltScreenMode 判断参数中是否包含备用屏幕模式编号。
func containsAltScreenMode(raw string) bool {
	if strings.TrimSpace(raw) == "" {
		return false
	}
	parts := strings.Split(raw, ";")
	for _, part := range parts {
		switch strings.TrimSpace(part) {
		case "47", "1047", "1049":
			return true
		}
	}
	return false
}

// keepAltScreenLeftover 控制备用屏幕序列残留缓冲上限，避免异常流导致内存增长。
func keepAltScreenLeftover(raw []byte) []byte {
	if len(raw) <= maxAltScreenLeftover {
		return append([]byte(nil), raw...)
	}
	return append([]byte(nil), raw[len(raw)-maxAltScreenLeftover:]...)
}

// trailingAltScreenPrefix 保留跨 PTY read 被切开的备用屏幕控制序列前缀。
func trailingAltScreenPrefix(buffer []byte) []byte {
	maxPrefixLength := len(tmuxDCSOpen) - 1
	if len(buffer) < maxPrefixLength {
		maxPrefixLength = len(buffer)
	}
	for length := maxPrefixLength; length > 0; length-- {
		suffix := buffer[len(buffer)-length:]
		if isPartialAltScreenPrefix(suffix) {
			return keepAltScreenLeftover(suffix)
		}
	}
	return nil
}

// isPartialAltScreenPrefix 判断尾部字节是否可能是下一次 read 才补齐的控制序列前缀。
func isPartialAltScreenPrefix(candidate []byte) bool {
	if len(candidate) == 0 {
		return false
	}
	return strings.HasPrefix(tmuxDCSOpen, string(candidate)) || strings.HasPrefix("\x1b[", string(candidate))
}
