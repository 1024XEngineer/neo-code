package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrateContextBudgetConfigContentMovesAutoCompactToBudget(t *testing.T) {
	t.Parallel()

	input := []byte(strings.TrimSpace(`
selected_provider: openai
context:
  compact:
    manual_strategy: keep_recent
  auto_compact:
    input_token_threshold: 120000
    reserve_tokens: 13000
    fallback_input_token_threshold: 100000
`) + "\n")

	out, changed, notes, err := MigrateContextBudgetConfigContent(input)
	if err != nil {
		t.Fatalf("MigrateContextBudgetConfigContent() error = %v", err)
	}
	if !changed {
		t.Fatal("expected migration change")
	}
	if len(notes) != 0 {
		t.Fatalf("expected no migration notes, got %v", notes)
	}
	text := string(out)
	if strings.Contains(text, "auto_compact:") {
		t.Fatalf("expected auto_compact removed, got:\n%s", text)
	}
	for _, want := range []string{
		"generate_start_timeout_sec: 90",
		"budget:",
		"prompt_budget: 120000",
		"reserve_tokens: 13000",
		"fallback_prompt_budget: 100000",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected migrated YAML to contain %q, got:\n%s", want, text)
		}
	}
}

func TestMigrateContextBudgetConfigContentAddsGenerateStartTimeoutWhenMissing(t *testing.T) {
	t.Parallel()

	input := []byte(strings.TrimSpace(`
selected_provider: openai
current_model: gpt-5.4
shell: powershell
`) + "\n")

	out, changed, notes, err := MigrateContextBudgetConfigContent(input)
	if err != nil {
		t.Fatalf("MigrateContextBudgetConfigContent() error = %v", err)
	}
	if !changed {
		t.Fatal("expected migration change")
	}
	if len(notes) != 0 {
		t.Fatalf("expected no migration notes, got %v", notes)
	}
	if !strings.Contains(string(out), "generate_start_timeout_sec: 90") {
		t.Fatalf("expected generate_start_timeout_sec to be added, got:\n%s", out)
	}
}

func TestMigrateContextBudgetConfigContentKeepsExistingGenerateStartTimeout(t *testing.T) {
	t.Parallel()

	input := []byte(strings.TrimSpace(`
selected_provider: openai
current_model: gpt-5.4
shell: powershell
generate_start_timeout_sec: 120
`) + "\n")

	out, changed, notes, err := MigrateContextBudgetConfigContent(input)
	if err != nil {
		t.Fatalf("MigrateContextBudgetConfigContent() error = %v", err)
	}
	if changed {
		t.Fatalf("expected no migration change, got:\n%s", out)
	}
	if len(notes) != 0 {
		t.Fatalf("expected no migration notes, got %v", notes)
	}
}

func TestMigrateContextBudgetConfigContentRejectsMixedBudgetBlocks(t *testing.T) {
	t.Parallel()

	input := []byte(strings.TrimSpace(`
context:
  budget:
    prompt_budget: 100000
  auto_compact:
    input_token_threshold: 120000
`) + "\n")

	_, _, _, err := MigrateContextBudgetConfigContent(input)
	if err == nil || !strings.Contains(err.Error(), "cannot both exist") {
		t.Fatalf("expected mixed block error, got %v", err)
	}
}

func TestMigrateContextBudgetConfigContentAddsNoteWhenEnabledExplicitlyFalse(t *testing.T) {
	t.Parallel()

	input := []byte(strings.TrimSpace(`
context:
  auto_compact:
    enabled: false
    input_token_threshold: 120000
`) + "\n")

	_, changed, notes, err := MigrateContextBudgetConfigContent(input)
	if err != nil {
		t.Fatalf("MigrateContextBudgetConfigContent() error = %v", err)
	}
	if !changed {
		t.Fatal("expected migration change")
	}
	if len(notes) != 1 || notes[0] != ContextBudgetMigrationNoteEnabledDeprecated {
		t.Fatalf("expected notes [%q], got %v", ContextBudgetMigrationNoteEnabledDeprecated, notes)
	}
}

func TestMigrateContextBudgetConfigContentNoNoteWhenEnabledTrueOrMissing(t *testing.T) {
	t.Parallel()

	cases := []string{
		strings.TrimSpace(`
context:
  auto_compact:
    enabled: true
    reserve_tokens: 13000
`) + "\n",
		strings.TrimSpace(`
context:
  auto_compact:
    reserve_tokens: 13000
`) + "\n",
	}

	for _, input := range cases {
		_, changed, notes, err := MigrateContextBudgetConfigContent([]byte(input))
		if err != nil {
			t.Fatalf("MigrateContextBudgetConfigContent() error = %v", err)
		}
		if !changed {
			t.Fatal("expected migration change")
		}
		if len(notes) != 0 {
			t.Fatalf("expected no notes, got %v", notes)
		}
	}
}

func TestMigrateContextBudgetConfigFileCreatesBackup(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, configName)
	original := strings.TrimSpace(`
context:
  auto_compact:
    input_token_threshold: 120000
`) + "\n"
	if err := os.WriteFile(target, []byte(original), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}

	result, err := MigrateContextBudgetConfigFile(target, false)
	if err != nil {
		t.Fatalf("MigrateContextBudgetConfigFile() error = %v", err)
	}
	if !result.Changed {
		t.Fatal("expected changed result")
	}
	if len(result.Notes) != 0 {
		t.Fatalf("expected no notes, got %v", result.Notes)
	}
	if result.Backup == "" {
		t.Fatal("expected backup path")
	}
	backup, err := os.ReadFile(result.Backup)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(backup) != original {
		t.Fatalf("expected backup to keep original content, got:\n%s", backup)
	}
}

func TestMigrateContextBudgetConfigFileCreatesBackupWhenAddingGenerateStartTimeout(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, configName)
	original := "selected_provider: openai\nshell: powershell\n"
	if err := os.WriteFile(target, []byte(original), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}

	result, err := MigrateContextBudgetConfigFile(target, false)
	if err != nil {
		t.Fatalf("MigrateContextBudgetConfigFile() error = %v", err)
	}
	if !result.Changed {
		t.Fatal("expected changed result")
	}
	if result.Backup == "" {
		t.Fatal("expected backup path")
	}

	migrated, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read migrated: %v", err)
	}
	if !strings.Contains(string(migrated), "generate_start_timeout_sec: 90") {
		t.Fatalf("expected generate_start_timeout_sec to be persisted, got:\n%s", migrated)
	}

	backup, err := os.ReadFile(result.Backup)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(backup) != original {
		t.Fatalf("expected backup to keep original content, got:\n%s", backup)
	}
}

func TestUpgradeConfigSchemaReturnsNotes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, configName)
	original := strings.TrimSpace(`
context:
  auto_compact:
    enabled: false
    reserve_tokens: 13000
`) + "\n"
	if err := os.WriteFile(target, []byte(original), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}

	result, err := UpgradeConfigSchema(target)
	if err != nil {
		t.Fatalf("UpgradeConfigSchema() error = %v", err)
	}
	if !result.Changed {
		t.Fatal("expected changed result")
	}
	if len(result.Notes) != 1 || result.Notes[0] != ContextBudgetMigrationNoteEnabledDeprecated {
		t.Fatalf("expected note %q, got %v", ContextBudgetMigrationNoteEnabledDeprecated, result.Notes)
	}
}

func TestMigrateContextBudgetConfigFileKeepsOriginalWhenBackupWriteFails(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, configName)
	original := strings.TrimSpace(`
context:
  auto_compact:
    input_token_threshold: 120000
`) + "\n"
	if err := os.WriteFile(target, []byte(original), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}

	restore := stubAtomicWriteOps(t)
	defer restore()
	atomicCreateTemp = func(dir string, pattern string) (*os.File, error) {
		return nil, errors.New("create temp failed")
	}

	_, err := MigrateContextBudgetConfigFile(target, false)
	if err == nil || !strings.Contains(err.Error(), "write migration backup") {
		t.Fatalf("expected backup write error, got %v", err)
	}
	raw, readErr := os.ReadFile(target)
	if readErr != nil {
		t.Fatalf("read target: %v", readErr)
	}
	if string(raw) != original {
		t.Fatalf("expected original config to stay unchanged, got:\n%s", raw)
	}
}

func TestMigrateContextBudgetConfigFileKeepsOriginalWhenTargetReplaceFails(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, configName)
	original := strings.TrimSpace(`
context:
  auto_compact:
    input_token_threshold: 120000
`) + "\n"
	if err := os.WriteFile(target, []byte(original), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}

	restore := stubAtomicWriteOps(t)
	defer restore()
	renameCount := 0
	atomicRename = func(oldpath string, newpath string) error {
		renameCount++
		if renameCount == 2 {
			return errors.New("rename target failed")
		}
		return os.Rename(oldpath, newpath)
	}

	_, err := MigrateContextBudgetConfigFile(target, false)
	if err == nil || !strings.Contains(err.Error(), "write migrated config") {
		t.Fatalf("expected migrated config write error, got %v", err)
	}
	if renameCount < 2 {
		t.Fatalf("expected second rename to fail, got renameCount=%d", renameCount)
	}

	raw, readErr := os.ReadFile(target)
	if readErr != nil {
		t.Fatalf("read target: %v", readErr)
	}
	if string(raw) != original {
		t.Fatalf("expected original config to stay unchanged, got:\n%s", raw)
	}

	backupRaw, backupErr := os.ReadFile(target + ".bak")
	if backupErr != nil {
		t.Fatalf("read backup: %v", backupErr)
	}
	if string(backupRaw) != original {
		t.Fatalf("expected backup to keep original content, got:\n%s", backupRaw)
	}
}

func stubAtomicWriteOps(t *testing.T) func() {
	t.Helper()
	prevCreateTemp := atomicCreateTemp
	prevRename := atomicRename
	return func() {
		atomicCreateTemp = prevCreateTemp
		atomicRename = prevRename
	}
}
