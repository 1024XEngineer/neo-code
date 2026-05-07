package streaming

import (
	"fmt"
	"sort"
	"strings"

	providertypes "neo-code/internal/provider/types"
)

// Accumulator 负责按 provider 流式事件累积 assistant 文本与工具调用。
type Accumulator struct {
	content     strings.Builder
	thinking    strings.Builder
	toolCalls   map[int]*providertypes.ToolCall
	messageDone bool
}

// NewAccumulator 创建一个空的流式消息累积器。
func NewAccumulator() *Accumulator {
	return &Accumulator{
		toolCalls: make(map[int]*providertypes.ToolCall),
	}
}

// MessageDone 返回当前流是否已经收到 message_done 事件。
func (a *Accumulator) MessageDone() bool {
	return a != nil && a.messageDone
}

// BuildMessage 将当前累积状态收敛为最终 assistant 消息。
func (a *Accumulator) BuildMessage() (providertypes.Message, error) {
	if a == nil {
		return providertypes.Message{Role: providertypes.RoleAssistant}, nil
	}

	ordered := make([]int, 0, len(a.toolCalls))
	for index := range a.toolCalls {
		ordered = append(ordered, index)
	}
	sort.Ints(ordered)

	message := providertypes.Message{
		Role:  providertypes.RoleAssistant,
		Parts: []providertypes.ContentPart{providertypes.NewTextPart(a.content.String())},
	}
	for _, index := range ordered {
		call := a.toolCalls[index]
		if call == nil {
			continue
		}
		if strings.TrimSpace(call.ID) == "" {
			return providertypes.Message{}, fmt.Errorf("runtime: provider emitted tool call %d without id", index)
		}
		if strings.TrimSpace(call.Name) == "" {
			return providertypes.Message{}, fmt.Errorf("runtime: provider emitted tool call %d without name", index)
		}
		message.ToolCalls = append(message.ToolCalls, *call)
	}
	return message, nil
}

// AccumulateText 负责累积文本增量。
func (a *Accumulator) AccumulateText(text string) {
	if a == nil {
		return
	}
	a.content.WriteString(text)
}

// AccumulateThinking 负责累积 thinking/reasoning 文本增量（不混入 assistant 正文）。
func (a *Accumulator) AccumulateThinking(text string) {
	if a == nil {
		return
	}
	a.thinking.WriteString(text)
}

// ThinkingContent 返回累积的 thinking 文本。
func (a *Accumulator) ThinkingContent() string {
	if a == nil {
		return ""
	}
	return a.thinking.String()
}

// ensureToolCall 返回指定索引的工具调用占位对象。
func (a *Accumulator) ensureToolCall(index int) *providertypes.ToolCall {
	if a == nil {
		return nil
	}
	call, exists := a.toolCalls[index]
	if !exists {
		call = &providertypes.ToolCall{}
		a.toolCalls[index] = call
	}
	return call
}

// AccumulateToolCallStart 负责合并工具调用的起始元数据。
func (a *Accumulator) AccumulateToolCallStart(index int, id string, name string) {
	call := a.ensureToolCall(index)
	if call == nil {
		return
	}
	if strings.TrimSpace(id) != "" {
		call.ID = id
	}
	if strings.TrimSpace(name) != "" {
		call.Name = name
	}
}

// AccumulateToolCallDelta 负责累积工具调用参数增量。
func (a *Accumulator) AccumulateToolCallDelta(index int, id string, argumentsDelta string) {
	call := a.ensureToolCall(index)
	if call == nil {
		return
	}
	if strings.TrimSpace(id) != "" {
		call.ID = id
	}
	call.Arguments += argumentsDelta
}

// MarkMessageDone 标记当前流已经收到最终完成事件。
func (a *Accumulator) MarkMessageDone() {
	if a == nil {
		return
	}
	a.messageDone = true
}
