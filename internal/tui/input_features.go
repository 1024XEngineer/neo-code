package tui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"

	tea "github.com/charmbracelet/bubbletea"

	"neo-code/internal/config"
)

const (
	workspaceCommandPrefix = "&"
	workspaceCommandUsage  = "& <command>"
	fileReferencePrefix    = "@"
	fileMenuTitle          = "Files"
	shellMenuTitle         = "Shell"
	maxWorkspaceFiles      = 4000
	maxFileSuggestions     = 6
)

type workspaceCommandResultMsg struct {
	command string
	output  string
	err     error
}

type tokenSelector int

const (
	tokenSelectorFirst tokenSelector = iota
	tokenSelectorLast
)

var workspaceCommandExecutor = defaultWorkspaceCommandExecutor

var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

func isWorkspaceCommandInput(input string) bool {
	return strings.HasPrefix(strings.TrimSpace(input), workspaceCommandPrefix)
}

func extractWorkspaceCommand(input string) (string, error) {
	trimmed := strings.TrimSpace(input)
	if !strings.HasPrefix(trimmed, workspaceCommandPrefix) {
		return "", fmt.Errorf("usage: %s", workspaceCommandUsage)
	}
	command := strings.TrimSpace(strings.TrimPrefix(trimmed, workspaceCommandPrefix))
	if command == "" {
		return "", fmt.Errorf("usage: %s", workspaceCommandUsage)
	}
	return command, nil
}

func runWorkspaceCommand(configManager *config.Manager, workdir string, raw string) tea.Cmd {
	return func() tea.Msg {
		command, output, err := executeWorkspaceCommand(context.Background(), configManager, workdir, raw)
		return workspaceCommandResultMsg{
			command: command,
			output:  output,
			err:     err,
		}
	}
}

func executeWorkspaceCommand(ctx context.Context, configManager *config.Manager, workdir string, raw string) (string, string, error) {
	command, err := extractWorkspaceCommand(raw)
	if err != nil {
		return "", "", err
	}

	cfg := configManager.Get()
	output, execErr := workspaceCommandExecutor(ctx, cfg, workdir, command)
	return command, output, execErr
}

func defaultWorkspaceCommandExecutor(ctx context.Context, cfg config.Config, workdir string, command string) (string, error) {
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

	args := shellArgs(cfg.Shell, command)
	cmd := exec.CommandContext(runCtx, args[0], args[1:]...)
	cmd.Dir = targetWorkdir
	output, err := cmd.CombinedOutput()
	text := sanitizeWorkspaceOutput(output)

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

func shellArgs(shell string, command string) []string {
	switch strings.ToLower(strings.TrimSpace(shell)) {
	case "powershell", "pwsh":
		return []string{"powershell", "-NoProfile", "-Command", powershellUTF8Command(command)}
	case "bash":
		return []string{"bash", "-lc", command}
	case "sh":
		return []string{"sh", "-lc", command}
	default:
		return []string{"powershell", "-NoProfile", "-Command", powershellUTF8Command(command)}
	}
}

func powershellUTF8Command(command string) string {
	utf8Setup := "[Console]::InputEncoding=[System.Text.Encoding]::UTF8; [Console]::OutputEncoding=[System.Text.Encoding]::UTF8; $OutputEncoding=[System.Text.Encoding]::UTF8; chcp 65001 > $null"
	return utf8Setup + "; " + command
}

func formatWorkspaceCommandResult(command string, output string, err error) string {
	header := "Command"
	if err != nil {
		header = "Command Failed"
	}

	body := strings.TrimSpace(output)
	if body == "" && err != nil {
		body = err.Error()
	}
	if body == "" {
		body = "(no output)"
	}

	body = strings.ReplaceAll(body, "```", "` ` `")
	return fmt.Sprintf("%s: & %s\n```text\n%s\n```", header, command, body)
}

func sanitizeWorkspaceOutput(raw []byte) string {
	text := decodeWorkspaceOutput(raw)
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

func decodeWorkspaceOutput(raw []byte) string {
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

func (a *App) refreshFileCandidates() error {
	candidates, err := collectWorkspaceFiles(a.state.CurrentWorkdir, maxWorkspaceFiles)
	if err != nil {
		return err
	}
	a.fileCandidates = candidates
	if workdir := strings.TrimSpace(a.state.CurrentWorkdir); workdir != "" {
		if absolute, absErr := filepath.Abs(workdir); absErr == nil {
			a.fileBrowser.CurrentDirectory = absolute
		}
	}
	a.refreshCommandMenu()
	return nil
}

func collectWorkspaceFiles(root string, limit int) ([]string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	var (
		candidates []string
		limitErr   = errors.New("file limit reached")
	)

	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		name := d.Name()
		if d.IsDir() {
			switch name {
			case ".git", ".gocache", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		candidates = append(candidates, filepath.ToSlash(rel))
		if limit > 0 && len(candidates) >= limit {
			return limitErr
		}
		return nil
	})
	if err != nil && !errors.Is(err, limitErr) {
		return nil, err
	}

	sort.Strings(candidates)
	return candidates, nil
}

func (a App) resolveFileReferenceSuggestions(input string) (start int, end int, query string, suggestions []string, ok bool) {
	start, end, token, ok := currentReferenceToken(input)
	if !ok {
		return 0, 0, "", nil, false
	}

	query = strings.ToLower(strings.TrimPrefix(token, fileReferencePrefix))
	suggestions = collectFileSuggestionMatches(query, a.fileCandidates, maxFileSuggestions)
	return start, end, query, suggestions, true
}

func collectFileSuggestionMatches(query string, candidates []string, limit int) []string {
	if len(candidates) == 0 || limit <= 0 {
		return nil
	}
	prefixMatches := make([]string, 0, maxFileSuggestions)
	containsMatches := make([]string, 0, maxFileSuggestions)
	for _, candidate := range candidates {
		lower := strings.ToLower(candidate)
		switch {
		case query == "" || strings.HasPrefix(lower, query):
			prefixMatches = append(prefixMatches, candidate)
		case strings.Contains(lower, query):
			containsMatches = append(containsMatches, candidate)
		}
		if len(prefixMatches)+len(containsMatches) >= maxFileSuggestions {
			break
		}
	}

	out := append(prefixMatches, containsMatches...)
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func tokenRange(input string, selector tokenSelector) (start int, end int, token string, ok bool) {
	if strings.TrimSpace(input) == "" {
		return 0, 0, "", false
	}

	switch selector {
	case tokenSelectorFirst:
		start = 0
		for start < len(input) {
			switch input[start] {
			case ' ', '\t', '\r', '\n':
				start++
			default:
				goto parse
			}
		}
		return 0, 0, "", false
	case tokenSelectorLast:
		end = len(input)
		start = strings.LastIndexAny(input, " \t\r\n")
		if start < 0 {
			start = 0
		} else {
			start++
		}
		if start >= end {
			return 0, 0, "", false
		}
		token = input[start:end]
		return start, end, token, true
	default:
		return 0, 0, "", false
	}

parse:
	end = start
	for end < len(input) {
		switch input[end] {
		case ' ', '\t', '\r', '\n':
			token = input[start:end]
			return start, end, token, true
		default:
			end++
		}
	}
	token = input[start:end]
	return start, end, token, true
}

func currentReferenceToken(input string) (start int, end int, token string, ok bool) {
	start, end, token, ok = tokenRange(input, tokenSelectorLast)
	if !ok {
		return 0, 0, "", false
	}
	if !strings.HasPrefix(token, fileReferencePrefix) {
		return 0, 0, "", false
	}
	return start, end, token, true
}

func (a *App) applyFileReference(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("file path is empty")
	}

	resolved := filepath.ToSlash(path)
	if workdir := strings.TrimSpace(a.state.CurrentWorkdir); workdir != "" {
		base, errBase := filepath.Abs(workdir)
		target, errTarget := filepath.Abs(path)
		if errBase == nil && errTarget == nil {
			if rel, errRel := filepath.Rel(base, target); errRel == nil && !strings.HasPrefix(rel, "..") {
				resolved = filepath.ToSlash(rel)
			} else {
				resolved = filepath.ToSlash(target)
			}
		}
	}
	resolved = strings.TrimPrefix(resolved, "./")
	reference := fileReferencePrefix + resolved

	current := a.input.Value()
	if start, end, _, ok := currentReferenceToken(current); ok {
		current = current[:start] + reference + current[end:]
	} else if strings.TrimSpace(current) == "" {
		current = reference
	} else {
		separator := " "
		if strings.HasSuffix(current, " ") || strings.HasSuffix(current, "\t") {
			separator = ""
		}
		current = current + separator + reference
	}

	a.input.SetValue(current)
	a.state.InputText = current
	a.normalizeComposerHeight()
	a.applyComponentLayout(false)
	a.refreshCommandMenu()
	a.state.StatusText = fmt.Sprintf("[System] Added file reference %s.", reference)
	return nil
}
