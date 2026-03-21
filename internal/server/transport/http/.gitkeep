package http

import (
	"encoding/json"
	"net/http"

	"go-llm-demo/internal/server/domain"
)

type ChatHandler struct {
	chatSvc domain.ChatGateway
}

func (h *ChatHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Messages []domain.Message `json:"messages"`
		Model    string           `json:"model"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	stream, err := h.chatSvc.Send(r.Context(), &domain.ChatRequest{
		Messages: req.Messages,
		Model:    req.Model,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/x-ndjson")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	enc := json.NewEncoder(w)
	for chunk := range stream {
		if err := enc.Encode(map[string]string{"content": chunk}); err != nil {
			return
		}
		flusher.Flush()
	}
}
