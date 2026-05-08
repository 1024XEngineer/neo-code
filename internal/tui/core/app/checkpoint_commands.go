package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	tuiservices "neo-code/internal/tui/services"
)

const maxCheckpointPatchPreviewChars = 4000

type checkpointCommandRuntime interface {
	ListCheckpoints(ctx context.Context, input tuiservices.CheckpointListInput) ([]tuiservices.CheckpointEntry, error)
	RestoreCheckpoint(ctx context.Context, input tuiservices.CheckpointRestoreInput) (tuiservices.CheckpointRestoreResult, error)
	UndoRestoreCheckpoint(ctx context.Context, sessionID string) (tuiservices.CheckpointRestoreResult, error)
	CheckpointDiff(ctx context.Context, sessionID string, checkpointID string) (tuiservices.CheckpointDiffResult, error)
}

func (a *App) handleCheckpointCommand(rest string) tea.Cmd {
	sessionID := strings.TrimSpace(a.state.ActiveSessionID)
	if sessionID == "" {
		a.applyInlineCommandError("checkpoint command requires an active session; send one message first or switch session via /session")
		return nil
	}
	runtime, ok := a.runtime.(checkpointCommandRuntime)
	if !ok {
		a.applyInlineCommandError("checkpoint command is unavailable in current runtime mode")
		return nil
	}

	action, argument := splitFirstWord(strings.TrimSpace(rest))
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "", "list":
		if strings.TrimSpace(argument) != "" {
			a.applyInlineCommandError(fmt.Sprintf("usage: %s", slashUsageCheckpoint))
			return nil
		}
		return a.runCheckpointCommand(func(ctx context.Context) (string, error) {
			entries, err := runtime.ListCheckpoints(ctx, tuiservices.CheckpointListInput{
				SessionID:      sessionID,
				Limit:          20,
				RestorableOnly: true,
			})
			if err != nil {
				return "", normalizeCheckpointCommandError(err)
			}
			return formatCheckpointList(entries), nil
		})
	case "restore":
		checkpointID, tail := splitFirstWord(strings.TrimSpace(argument))
		checkpointID = strings.TrimSpace(checkpointID)
		if checkpointID == "" || isCommandPlaceholder(checkpointID) || strings.TrimSpace(tail) != "" {
			a.applyInlineCommandError(fmt.Sprintf("usage: %s", slashUsageCheckpointRestore))
			return nil
		}
		return a.runCheckpointCommand(func(ctx context.Context) (string, error) {
			result, err := runtime.RestoreCheckpoint(ctx, tuiservices.CheckpointRestoreInput{
				SessionID:    sessionID,
				CheckpointID: checkpointID,
			})
			if err != nil {
				return "", normalizeCheckpointCommandError(err)
			}
			id := fallbackText(strings.TrimSpace(result.CheckpointID), checkpointID)
			if result.HasConflict {
				return fmt.Sprintf("Checkpoint restored with conflicts: %s", id), nil
			}
			return fmt.Sprintf("Checkpoint restored: %s", id), nil
		})
	case "undo":
		if strings.TrimSpace(argument) != "" {
			a.applyInlineCommandError(fmt.Sprintf("usage: %s", slashUsageCheckpointUndo))
			return nil
		}
		return a.runCheckpointCommand(func(ctx context.Context) (string, error) {
			result, err := runtime.UndoRestoreCheckpoint(ctx, sessionID)
			if err != nil {
				return "", normalizeCheckpointCommandError(err)
			}
			id := fallbackText(strings.TrimSpace(result.CheckpointID), "guard checkpoint")
			return fmt.Sprintf("Checkpoint restore undo applied: %s", id), nil
		})
	case "diff":
		checkpointID, tail := splitFirstWord(strings.TrimSpace(argument))
		checkpointID = strings.TrimSpace(checkpointID)
		if checkpointID == "" || isCommandPlaceholder(checkpointID) || strings.TrimSpace(tail) != "" {
			a.applyInlineCommandError(fmt.Sprintf("usage: %s", slashUsageCheckpointDiff))
			return nil
		}
		return a.runCheckpointCommand(func(ctx context.Context) (string, error) {
			diff, err := runtime.CheckpointDiff(ctx, sessionID, checkpointID)
			if err != nil {
				return "", normalizeCheckpointCommandError(err)
			}
			return formatCheckpointDiff(diff), nil
		})
	default:
		a.applyInlineCommandError("usage: /checkpoint | /checkpoint restore <id> | /checkpoint undo | /checkpoint diff <id>")
		return nil
	}
}

func (a *App) runCheckpointCommand(run func(context.Context) (string, error)) tea.Cmd {
	return tuiservices.RunLocalCommandCmd(
		run,
		func(notice string, err error) tea.Msg {
			return localCommandResultMsg{Notice: notice, Err: err}
		},
	)
}

func normalizeCheckpointCommandError(err error) error {
	if err == nil {
		return nil
	}
	if isGatewayUnsupportedActionError(err) {
		return errors.New("gateway does not support checkpoint commands; please upgrade gateway and client to the latest version")
	}
	return err
}

func formatCheckpointList(entries []tuiservices.CheckpointEntry) string {
	if len(entries) == 0 {
		return "No restorable checkpoints in current session."
	}
	rows := make([]string, 0, len(entries)+1)
	rows = append(rows, "Restorable checkpoints:")
	for _, entry := range entries {
		id := fallbackText(strings.TrimSpace(entry.CheckpointID), "(unknown)")
		reason := fallbackText(strings.TrimSpace(entry.Reason), "-")
		status := fallbackText(strings.TrimSpace(entry.Status), "-")
		createdAt := "-"
		if entry.CreatedAtMS > 0 {
			createdAt = time.UnixMilli(entry.CreatedAtMS).Local().Format(time.RFC3339)
		}
		rows = append(rows, fmt.Sprintf("- %s | reason=%s | status=%s | created=%s", id, reason, status, createdAt))
	}
	return strings.Join(rows, "\n")
}

func formatCheckpointDiff(result tuiservices.CheckpointDiffResult) string {
	checkpointID := fallbackText(strings.TrimSpace(result.CheckpointID), "(unknown)")
	rows := []string{
		fmt.Sprintf(
			"Checkpoint diff: %s (added=%d modified=%d deleted=%d)",
			checkpointID,
			len(result.Files.Added),
			len(result.Files.Modified),
			len(result.Files.Deleted),
		),
	}
	patch := strings.TrimSpace(result.Patch)
	if patch == "" {
		return strings.Join(rows, "\n")
	}
	runes := []rune(patch)
	if len(runes) > maxCheckpointPatchPreviewChars {
		patch = string(runes[:maxCheckpointPatchPreviewChars]) + "\n...(truncated)"
	}
	rows = append(rows, patch)
	return strings.Join(rows, "\n")
}

func isCommandPlaceholder(value string) bool {
	trimmed := strings.TrimSpace(value)
	return strings.HasPrefix(trimmed, "<") && strings.HasSuffix(trimmed, ">")
}
