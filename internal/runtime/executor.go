package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"neocode/internal/provider"
	"neocode/internal/tools"
)

func (s *Service) executeLoop(ctx context.Context, input UserInput) error {
	binding, ok := s.currentBinding()
	if !ok {
		return fmt.Errorf("no active provider configured")
	}

	for turn := 0; turn < s.maxTurns; turn++ {
		session, ok := s.sessions.Get(input.SessionID)
		if !ok {
			return fmt.Errorf("session %q not found", input.SessionID)
		}

		response, err := s.chat(ctx, binding, provider.ChatRequest{
			Model:    binding.Model,
			Messages: s.prompts.Build(session),
			Tools:    s.registry.ListSchemas(),
		}, input.SessionID)
		if err != nil {
			return err
		}

		assistantMessage := response.Message
		if assistantMessage.Role == "" {
			assistantMessage.Role = provider.RoleAssistant
		}
		if _, err := s.sessions.Append(input.SessionID, assistantMessage); err != nil {
			return err
		}
		s.publish(Event{
			Type:      EventAgentMessage,
			SessionID: input.SessionID,
			Payload:   assistantMessage,
			At:        time.Now(),
		})

		if len(assistantMessage.ToolCalls) == 0 {
			if strings.TrimSpace(assistantMessage.Content) == "" {
				return fmt.Errorf("provider returned an empty assistant response")
			}
			s.publish(Event{
				Type:      EventCompleted,
				SessionID: input.SessionID,
				Payload:   assistantMessage,
				At:        time.Now(),
			})
			return nil
		}

		for _, call := range assistantMessage.ToolCalls {
			s.setStatus(fmt.Sprintf("Running %s...", call.Name), true)
			s.publish(Event{
				Type:      EventToolStarted,
				SessionID: input.SessionID,
				Payload:   call,
				At:        time.Now(),
			})

			result, err := s.registry.Execute(ctx, tools.Invocation{
				ID:        call.ID,
				Name:      call.Name,
				Arguments: json.RawMessage(call.Arguments),
				SessionID: input.SessionID,
				Workdir:   s.workdir,
			})

			toolMessage := provider.Message{
				Role:       provider.RoleTool,
				Content:    result.Content,
				ToolCallID: result.ToolCallID,
			}
			if result.IsError && !strings.HasPrefix(toolMessage.Content, "tool error:") {
				toolMessage.Content = "tool error: " + toolMessage.Content
			}

			if _, appendErr := s.sessions.Append(input.SessionID, toolMessage); appendErr != nil {
				return appendErr
			}

			s.publish(Event{
				Type:      EventToolFinished,
				SessionID: input.SessionID,
				Payload: map[string]any{
					"call":   call,
					"result": result,
				},
				At: time.Now(),
			})

			if err != nil {
				s.publish(Event{
					Type:      EventError,
					SessionID: input.SessionID,
					Payload:   err.Error(),
					At:        time.Now(),
				})
			}
		}
	}

	return fmt.Errorf("agent stopped after reaching max turns (%d)", s.maxTurns)
}

func (s *Service) chat(
	ctx context.Context,
	binding ProviderBinding,
	req provider.ChatRequest,
	sessionID string,
) (provider.ChatResponse, error) {
	if streamingProvider, ok := binding.Client.(provider.StreamingProvider); ok {
		req.Stream = true
		return streamingProvider.ChatStream(ctx, req, func(delta string) error {
			if delta == "" {
				return nil
			}
			s.publish(Event{
				Type:      EventAgentChunk,
				SessionID: sessionID,
				Payload:   delta,
				At:        time.Now(),
			})
			return nil
		})
	}

	response, err := binding.Client.Chat(ctx, req)
	if err != nil {
		return response, err
	}

	if len(response.Message.ToolCalls) == 0 && strings.TrimSpace(response.Message.Content) != "" {
		for _, chunk := range chunkText(response.Message.Content, 48) {
			s.publish(Event{
				Type:      EventAgentChunk,
				SessionID: sessionID,
				Payload:   chunk,
				At:        time.Now(),
			})
		}
	}

	return response, nil
}

func chunkText(value string, chunkSize int) []string {
	if value == "" {
		return nil
	}
	if chunkSize <= 0 {
		chunkSize = 48
	}
	if utf8.RuneCountInString(value) <= chunkSize {
		return []string{value}
	}

	runes := []rune(value)
	chunks := make([]string, 0, (len(runes)/chunkSize)+1)
	for start := 0; start < len(runes); start += chunkSize {
		end := start + chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[start:end]))
	}

	return chunks
}
