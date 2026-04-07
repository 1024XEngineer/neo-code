package infra

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"

	"neo-code/internal/config"
)

var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// DefaultWorkspaceCommandExecutor 在受控超时下执行工作区命令并返回清洗后的输出。
func DefaultWorkspaceCommandExecutor(ctx context.Context, cfg config.Config, workdir string, command string) (string, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return "", errors.New("command is empty")
	}
	targetWorkdir := strings.TrimSpace(workdir)
	if targetWorkdir == "" {
		targetWorkdir = cfg.Workdir
	}

	timeoutSec := cfg.ToolTimeoutSec
	if timeoutSec <= 0 {
		timeoutSec = config.DefaultToolTimeoutSec
	}

	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	args := ShellArgs(cfg.Shell, command)
	cmd := exec.CommandContext(runCtx, args[0], args[1:]...)
	cmd.Dir = targetWorkdir
	output, err := cmd.CombinedOutput()
	text := SanitizeWorkspaceOutput(output)

	if runCtx.Err() == context.DeadlineExceeded {
		return text, fmt.Errorf("command timed out after %ds", timeoutSec)
	}
	if err != nil {
		return text, err
	}
	if text == "" {
		return "(no output)", nil
	}
	return text, nil
}

// ShellArgs 根据 shell 类型构造可执行参数。
func ShellArgs(shell string, command string) []string {
	switch strings.ToLower(strings.TrimSpace(shell)) {
	case "powershell", "pwsh":
		return []string{"powershell", "-NoProfile", "-Command", PowerShellUTF8Command(command)}
	case "bash":
		return []string{"bash", "-lc", command}
	case "sh":
		return []string{"sh", "-lc", command}
	default:
		return []string{"powershell", "-NoProfile", "-Command", PowerShellUTF8Command(command)}
	}
}

// PowerShellUTF8Command 为 PowerShell 命令前置 UTF-8 控制台编码设置。
func PowerShellUTF8Command(command string) string {
	utf8Setup := "[Console]::InputEncoding=[System.Text.Encoding]::UTF8; [Console]::OutputEncoding=[System.Text.Encoding]::UTF8; $OutputEncoding=[System.Text.Encoding]::UTF8; chcp 65001 > $null"
	return utf8Setup + "; " + command
}

// SanitizeWorkspaceOutput 对原始命令输出做编码恢复、ANSI 去除与控制字符清洗。
func SanitizeWorkspaceOutput(raw []byte) string {
	text := DecodeWorkspaceOutput(raw)
	text = strings.ToValidUTF8(text, "?")
	text = ansiEscapePattern.ReplaceAllString(text, "")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return r
		case r < 0x20:
			return -1
		default:
			return r
		}
	}, text)
	return strings.TrimSpace(text)
}

// DecodeWorkspaceOutput 尝试在 UTF-8 / UTF-16 场景下恢复可读文本。
func DecodeWorkspaceOutput(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}

	switch {
	case bytes.HasPrefix(raw, []byte{0xFF, 0xFE}):
		return decodeUTF16(raw[2:], true)
	case bytes.HasPrefix(raw, []byte{0xFE, 0xFF}):
		return decodeUTF16(raw[2:], false)
	}

	if len(raw)%2 == 0 {
		le := decodeUTF16(raw, true)
		be := decodeUTF16(raw, false)
		rawText := string(raw)
		rawScore := decodedTextScore(rawText)
		leScore := decodedTextScore(le)
		beScore := decodedTextScore(be)

		bestText := rawText
		bestScore := rawScore
		if leScore > bestScore {
			bestText = le
			bestScore = leScore
		}
		if beScore > bestScore {
			bestText = be
		}
		return bestText
	}

	return string(raw)
}

// decodedTextScore 基于可打印字符比例评估解码结果质量。
func decodedTextScore(text string) int {
	if text == "" {
		return 0
	}

	score := 0
	for _, r := range text {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			score += 1
		case r == unicode.ReplacementChar:
			score -= 6
		case unicode.IsPrint(r):
			score += 2
		default:
			score -= 3
		}
	}
	return score
}

// decodeUTF16 按大小端将字节流解析为 UTF-16 文本。
func decodeUTF16(raw []byte, littleEndian bool) string {
	if len(raw) < 2 {
		return string(raw)
	}
	if len(raw)%2 != 0 {
		raw = raw[:len(raw)-1]
	}

	words := make([]uint16, 0, len(raw)/2)
	for i := 0; i < len(raw); i += 2 {
		if littleEndian {
			words = append(words, uint16(raw[i])|uint16(raw[i+1])<<8)
		} else {
			words = append(words, uint16(raw[i])<<8|uint16(raw[i+1]))
		}
	}
	return string(utf16.Decode(words))
}
