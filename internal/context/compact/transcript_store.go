package compact

import (
	cryptorand "crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	goruntime "runtime"
	"strings"
	"time"

	"neo-code/internal/provider"
)

const (
	neocodeDataDirName          = ".neocode"
	compactProjectsDirName      = "projects"
	compactTranscriptsDirName   = ".transcripts"
	transcriptFallbackSessionID = "draft"
	transcriptFileExtension     = ".jsonl"
	transcriptTemporarySuffix   = ".tmp"
)

type transcriptLine struct {
	Index      int                 `json:"index"`
	Timestamp  string              `json:"timestamp"`
	Role       string              `json:"role"`
	Content    string              `json:"content"`
	ToolCalls  []provider.ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string              `json:"tool_call_id,omitempty"`
	IsError    bool                `json:"is_error,omitempty"`
}

// transcriptStore 只负责 compact 原始 transcript 的目录规划与安全落盘。
type transcriptStore struct {
	now         func() time.Time
	randomToken func() (string, error)
	userHomeDir func() (string, error)
	mkdirAll    func(path string, perm os.FileMode) error
	writeFile   func(name string, data []byte, perm os.FileMode) error
	rename      func(oldPath, newPath string) error
	remove      func(path string) error
}

// Save 按项目维度持久化当前 compact 前的 transcript，并返回 ID 与路径。
func (s transcriptStore) Save(messages []provider.Message, sessionID string, workdir string) (string, string, error) {
	home, err := s.userHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("compact: resolve user home: %w", err)
	}

	projectHash := hashProject(workdir)
	dir := transcriptDirectory(home, projectHash)
	if err := s.mkdirAll(dir, 0o755); err != nil {
		return "", "", fmt.Errorf("compact: create transcript dir: %w", err)
	}

	sessionID = sanitizeID(sessionID)
	if sessionID == "" {
		sessionID = transcriptFallbackSessionID
	}

	tokenFn := s.randomToken
	if tokenFn == nil {
		tokenFn = randomTranscriptToken
	}
	randomToken, err := tokenFn()
	if err != nil {
		return "", "", fmt.Errorf("compact: generate transcript token: %w", err)
	}

	transcriptID := fmt.Sprintf("transcript_%d_%s_%s", s.now().UnixNano(), randomToken, sessionID)
	transcriptPath := filepath.Join(dir, transcriptID+transcriptFileExtension)
	tmpPath := transcriptPath + transcriptTemporarySuffix

	now := s.now().UTC().Format(time.RFC3339Nano)
	var builder strings.Builder
	for i, message := range messages {
		line := transcriptLine{
			Index:      i,
			Timestamp:  now,
			Role:       message.Role,
			Content:    message.Content,
			ToolCalls:  append([]provider.ToolCall(nil), message.ToolCalls...),
			ToolCallID: message.ToolCallID,
			IsError:    message.IsError,
		}
		payload, err := json.Marshal(line)
		if err != nil {
			return "", "", fmt.Errorf("compact: marshal transcript line: %w", err)
		}
		builder.Write(payload)
		builder.WriteByte('\n')
	}

	if err := s.writeFile(tmpPath, []byte(builder.String()), transcriptFileMode()); err != nil {
		return "", "", fmt.Errorf("compact: write transcript: %w", err)
	}
	if err := s.rename(tmpPath, transcriptPath); err != nil {
		_ = s.remove(tmpPath)
		return "", "", fmt.Errorf("compact: commit transcript: %w", err)
	}

	return transcriptID, transcriptPath, nil
}

// transcriptFileMode 根据当前平台返回 transcript 文件权限。
func transcriptFileMode() os.FileMode {
	return transcriptFileModeForOS(goruntime.GOOS)
}

// transcriptFileModeForOS 根据给定平台名返回 transcript 文件权限，便于测试不同分支。
func transcriptFileModeForOS(goos string) os.FileMode {
	if goos == "windows" {
		return 0o644
	}
	return 0o600
}

// transcriptDirectory 统一构造 compact 原始 transcript 的持久化目录。
func transcriptDirectory(home string, projectHash string) string {
	return filepath.Join(home, neocodeDataDirName, compactProjectsDirName, projectHash, compactTranscriptsDirName)
}

// randomTranscriptToken 生成 transcript 文件名使用的随机令牌。
func randomTranscriptToken() (string, error) {
	entropy := make([]byte, 4)
	if _, err := cryptorand.Read(entropy); err != nil {
		return "", err
	}
	return hex.EncodeToString(entropy), nil
}

// hashProject 使用工作目录计算项目级 transcript 目录哈希。
func hashProject(workdir string) string {
	clean := strings.TrimSpace(filepath.Clean(workdir))
	if clean == "" {
		clean = "unknown"
	}
	sum := sha1.Sum([]byte(strings.ToLower(clean)))
	return hex.EncodeToString(sum[:8])
}

var nonIDChars = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

// sanitizeID 将 session id 收敛为适合落盘文件名的安全标识。
func sanitizeID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return nonIDChars.ReplaceAllString(value, "_")
}
