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

// ContextBudgetMigrationResult 汇总 config.yaml 预算配置迁移的执行结果。
type ContextBudgetMigrationResult struct {
	Path    string
	Changed bool
	Backup  string
	Reason  string
	Notes   []string
}

const (
	// ContextBudgetMigrationNoteEnabledDeprecated 标记旧开关被废弃且预算门禁不可关闭。
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

// MigrateContextBudgetConfigFile 将 config.yaml 中的 context.auto_compact 迁移到 context.budget。
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

// MigrateContextBudgetConfigContent 将旧预算 YAML 块替换为当前预算 YAML 块，并返回迁移说明。
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

	if !changed {
		return raw, false, nil, nil
	}

	out, err := yaml.Marshal(doc)
	if err != nil {
		return nil, false, nil, err
	}
	return out, true, notes, nil
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
