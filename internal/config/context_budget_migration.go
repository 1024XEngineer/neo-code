package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ContextBudgetMigrationResult 汇总 config.yaml schema 升级的执行结果。
type ContextBudgetMigrationResult struct {
	Path    string
	Changed bool
	Backup  string
	Reason  string
	Notes   []string
}

const (
	// ContextBudgetMigrationNoteEnabledDeprecated 提示旧 enabled 开关已废弃。
	ContextBudgetMigrationNoteEnabledDeprecated = "旧 context.auto_compact.enabled 已废弃，新预算门禁不可关闭"
)

// DefaultConfigPath 返回当前用户环境下的默认主配置文件路径。
func DefaultConfigPath() string {
	return filepath.Join(defaultBaseDir(), configName)
}

// UpgradeConfigSchema 执行配置 schema 升级并返回迁移结果。
func UpgradeConfigSchema(path string) (ContextBudgetMigrationResult, error) {
	return MigrateContextBudgetConfigFile(path, false)
}

// MigrateContextBudgetConfigFile 将 config.yaml 中的旧 schema 迁移到当前实现。
func MigrateContextBudgetConfigFile(path string, dryRun bool) (ContextBudgetMigrationResult, error) {
	if path == "" {
		path = DefaultConfigPath()
	}
	if filepath.Base(path) != configName {
		return ContextBudgetMigrationResult{}, fmt.Errorf("config: migration target must be %s", configName)
	}

	result := ContextBudgetMigrationResult{Path: path}
	raw, err := os.ReadFile(path)
	if err != nil {
		return result, fmt.Errorf("config: read migration target %s: %w", path, err)
	}

	migrated, changed, notes, err := MigrateContextBudgetConfigContent(raw)
	if err != nil {
		return result, fmt.Errorf("config: migrate %s: %w", path, err)
	}
	result.Notes = append(result.Notes, notes...)
	if !changed {
		result.Reason = "未检测到需要升级的配置字段"
		return result, nil
	}

	result.Changed = true
	if dryRun {
		return result, nil
	}

	backup := path + ".bak"
	if err := writeFileAtomically(backup, raw, 0o644); err != nil {
		return result, fmt.Errorf("config: write migration backup %s: %w", backup, err)
	}
	if err := writeFileAtomically(path, migrated, 0o644); err != nil {
		return result, fmt.Errorf("config: write migrated config %s: %w", path, err)
	}
	result.Backup = backup
	return result, nil
}

// MigrateContextBudgetConfigContent 将旧 YAML schema 迁移为当前 schema，并返回迁移说明。
func MigrateContextBudgetConfigContent(raw []byte) ([]byte, bool, []string, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return raw, false, nil, nil
	}

	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, false, nil, err
	}
	if doc == nil {
		doc = make(map[string]any)
	}

	changed := false
	if _, exists := doc["generate_start_timeout_sec"]; !exists {
		doc["generate_start_timeout_sec"] = DefaultGenerateStartTimeoutSec
		changed = true
	}

	var notes []string
	contextValue, hasContext := doc["context"]
	if hasContext {
		contextMap, ok := migrationStringMap(contextValue)
		if !ok {
			return nil, false, nil, errors.New("context must be a mapping")
		}

		autoValue, hasAutoCompact := contextMap["auto_compact"]
		if hasAutoCompact {
			if _, hasBudget := contextMap["budget"]; hasBudget {
				return nil, false, nil, errors.New("context.auto_compact and context.budget cannot both exist")
			}

			autoMap, ok := migrationStringMap(autoValue)
			if !ok {
				return nil, false, nil, errors.New("context.auto_compact must be a mapping")
			}
			budgetMap := make(map[string]any)
			migrationMoveField(autoMap, budgetMap, "input_token_threshold", "prompt_budget")
			migrationMoveField(autoMap, budgetMap, "reserve_tokens", "reserve_tokens")
			migrationMoveField(autoMap, budgetMap, "fallback_input_token_threshold", "fallback_prompt_budget")
			notes = collectContextBudgetMigrationNotes(autoMap)

			delete(contextMap, "auto_compact")
			contextMap["budget"] = budgetMap
			doc["context"] = contextMap
			changed = true
		}
	}

	verificationChanged, err := migrateVerificationConfig(doc)
	if err != nil {
		return nil, false, nil, err
	}
	if verificationChanged {
		changed = true
	}
	legacyRuntimeChanged := migrateLegacyRuntimeConfigFields(doc)
	if legacyRuntimeChanged {
		changed = true
	}
	legacyMemoChanged := migrateLegacyMemoConfigFields(doc)
	if legacyMemoChanged {
		changed = true
	}

	if !changed {
		return raw, false, nil, nil
	}

	out, err := yaml.Marshal(doc)
	if err != nil {
		return nil, false, nil, err
	}
	return out, true, notes, nil
}

// migrateLegacyRuntimeConfigFields 清理 runtime 下已废弃且会导致严格解析失败的历史字段。
func migrateLegacyRuntimeConfigFields(doc map[string]any) bool {
	runtimeValue, ok := doc["runtime"]
	if !ok {
		return false
	}
	runtimeMap, ok := migrationStringMap(runtimeValue)
	if !ok {
		return false
	}

	changed := false
	for _, key := range []string{"max_no_progress_streak"} {
		if _, exists := runtimeMap[key]; exists {
			delete(runtimeMap, key)
			changed = true
		}
	}
	if !changed {
		return false
	}
	doc["runtime"] = runtimeMap
	return true
}

// migrateLegacyMemoConfigFields 清理 memo 下已移除且可安全丢弃的历史字段。
func migrateLegacyMemoConfigFields(doc map[string]any) bool {
	memoValue, ok := doc["memo"]
	if !ok {
		return false
	}
	memoMap, ok := migrationStringMap(memoValue)
	if !ok {
		return false
	}

	changed := false
	for _, key := range []string{"extract_recent_messages"} {
		if _, exists := memoMap[key]; exists {
			delete(memoMap, key)
			changed = true
		}
	}
	if !changed {
		return false
	}
	if len(memoMap) == 0 {
		delete(doc, "memo")
	} else {
		doc["memo"] = memoMap
	}
	return true
}

// migrateVerificationConfig 清理已废弃的 verification 字段，并将安全的旧 command string 收敛成 argv。
func migrateVerificationConfig(doc map[string]any) (bool, error) {
	runtimeValue, ok := doc["runtime"]
	if !ok {
		return false, nil
	}
	runtimeMap, ok := migrationStringMap(runtimeValue)
	if !ok {
		return false, nil
	}
	verificationValue, ok := runtimeMap["verification"]
	if !ok {
		return false, nil
	}
	verificationMap, ok := migrationStringMap(verificationValue)
	if !ok {
		return false, nil
	}

	changed := false
	for _, key := range []string{"enabled", "default_task_policy", "final_intercept", "max_retries", "hooks", "max_no_progress"} {
		if _, exists := verificationMap[key]; exists {
			delete(verificationMap, key)
			changed = true
		}
	}

	verifiersValue, ok := verificationMap["verifiers"]
	if ok {
		verifiersMap, ok := migrationStringMap(verifiersValue)
		if ok {
			for name, rawVerifier := range verifiersMap {
				verifierMap, ok := migrationStringMap(rawVerifier)
				if !ok {
					continue
				}
				for _, key := range []string{"enabled", "required", "fail_open", "fail_closed"} {
					if _, exists := verifierMap[key]; exists {
						delete(verifierMap, key)
						changed = true
					}
				}
				commandChanged, err := migrateVerifierCommandField(verifierMap)
				if err != nil {
					return false, err
				}
				if commandChanged {
					changed = true
				}
				verifiersMap[name] = verifierMap
			}
			verificationMap["verifiers"] = verifiersMap
		}
	}

	runtimeMap["verification"] = verificationMap
	doc["runtime"] = runtimeMap
	return changed, nil
}

// migrateVerifierCommandField 将简单的旧 command string 迁移为 argv；含 shell 语义时直接报错。
func migrateVerifierCommandField(verifierMap map[string]any) (bool, error) {
	value, ok := verifierMap["command"]
	if !ok {
		return false, nil
	}
	command, ok := value.(string)
	if !ok {
		return false, nil
	}
	fields, err := parseLegacyVerificationCommand(command)
	if err != nil {
		return false, err
	}
	if len(fields) == 0 {
		delete(verifierMap, "command")
		return true, nil
	}

	args := make([]any, 0, len(fields))
	for _, field := range fields {
		args = append(args, field)
	}
	verifierMap["command"] = args
	return true, nil
}

// parseLegacyVerificationCommand 仅接受不含 shell 语义的简单空白分隔命令。
func parseLegacyVerificationCommand(command string) ([]string, error) {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return nil, nil
	}
	if containsUnsafeLegacyVerifierCommandSyntax(trimmed) {
		return nil, errors.New("runtime.verification.verifiers.command uses unsupported shell syntax; rewrite it as argv")
	}
	return strings.Fields(trimmed), nil
}

// containsUnsafeLegacyVerifierCommandSyntax 判断旧命令是否包含无法安全自动迁移的 shell 结构。
func containsUnsafeLegacyVerifierCommandSyntax(command string) bool {
	unsafeTokens := []string{"'", "\"", "`", "|", "&&", "||", ";", ">", "<", "$(", "\n", "\r"}
	for _, token := range unsafeTokens {
		if strings.Contains(command, token) {
			return true
		}
	}
	return false
}

// collectContextBudgetMigrationNotes 汇总迁移过程中需要提示给用户的行为变化说明。
func collectContextBudgetMigrationNotes(autoCompact map[string]any) []string {
	if value, ok := autoCompact["enabled"]; ok && migrationExplicitFalse(value) {
		return []string{ContextBudgetMigrationNoteEnabledDeprecated}
	}
	return nil
}

// migrationExplicitFalse 判断迁移字段是否显式配置为 false。
func migrationExplicitFalse(value any) bool {
	switch typed := value.(type) {
	case bool:
		return !typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "false")
	default:
		return false
	}
}

// migrationMoveField 在两个 YAML map 之间迁移字段名，不修改字段值。
func migrationMoveField(src map[string]any, dst map[string]any, oldName string, newName string) {
	if value, ok := src[oldName]; ok {
		dst[newName] = value
	}
}

// migrationStringMap 将 YAML map 统一转为 map[string]any。
func migrationStringMap(value any) (map[string]any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		return typed, true
	case map[any]any:
		result := make(map[string]any, len(typed))
		for key, value := range typed {
			keyString, ok := key.(string)
			if !ok {
				return nil, false
			}
			result[keyString] = value
		}
		return result, true
	default:
		return nil, false
	}
}
