package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const logViewerPersistDir = "log-viewer"

// SessionLogEntry 描述会话维度的日志查看器持久化条目。
type SessionLogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Source    string    `json:"source"`
	Message   string    `json:"message"`
	Inline    string    `json:"inline_message,omitempty"`
}

// LoadSessionLogEntries 按会话 ID 读取日志查看器持久化数据。
func (s *Service) LoadSessionLogEntries(ctx context.Context, sessionID string) ([]SessionLogEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := s.sessionLogEntriesPath(sessionID)
	if err != nil || path == "" {
		return nil, err
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("runtime: read session log entries: %w", err)
	}
	entries := make([]SessionLogEntry, 0)
	if err := json.Unmarshal(payload, &entries); err != nil {
		return nil, fmt.Errorf("runtime: decode session log entries: %w", err)
	}
	return append([]SessionLogEntry(nil), entries...), nil
}

// SaveSessionLogEntries 将日志查看器条目写入会话维度持久化存储。
func (s *Service) SaveSessionLogEntries(ctx context.Context, sessionID string, entries []SessionLogEntry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := s.sessionLogEntriesPath(sessionID)
	if err != nil || path == "" {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("runtime: ensure session log directory: %w", err)
	}
	payload, err := json.Marshal(entries)
	if err != nil {
		return fmt.Errorf("runtime: encode session log entries: %w", err)
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		return fmt.Errorf("runtime: write session log entries: %w", err)
	}
	return nil
}

// sessionLogEntriesPath 生成会话日志文件路径，并确保命名稳定且避免会话 ID 冲突。
func (s *Service) sessionLogEntriesPath(sessionID string) (string, error) {
	normalizedSessionID := strings.TrimSpace(sessionID)
	if normalizedSessionID == "" {
		return "", nil
	}
	if s == nil || s.configManager == nil {
		return "", errors.New("runtime: config manager is not initialized")
	}
	baseDir := strings.TrimSpace(s.configManager.BaseDir())
	if baseDir == "" {
		return "", errors.New("runtime: config base directory is empty")
	}
	sum := sha256.Sum256([]byte(normalizedSessionID))
	fileName := fmt.Sprintf("%s_%s.json", sanitizeSessionLogPrefix(normalizedSessionID), hex.EncodeToString(sum[:8]))
	return filepath.Join(baseDir, logViewerPersistDir, fileName), nil
}

// sanitizeSessionLogPrefix 生成可读前缀，便于排查文件，同时不参与唯一性判定。
func sanitizeSessionLogPrefix(sessionID string) string {
	var b strings.Builder
	for _, r := range sessionID {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '_' || r == '-':
			b.WriteRune(r)
		default:
			if b.Len() > 0 {
				b.WriteByte('_')
			}
		}
		if b.Len() >= 24 {
			break
		}
	}
	prefix := strings.Trim(b.String(), "_")
	if prefix == "" {
		return "session"
	}
	return prefix
}
