package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	defaultNeoCodeDirName  = ".neocode"
	providerConfigFileName = "provider.yaml"
)

// migrationFile 表示迁移脚本支持处理的 provider.yaml 字段集合（含旧版嵌套块）。
type migrationFile struct {
	Name                  string                 `yaml:"name"`
	Driver                string                 `yaml:"driver"`
	APIKeyEnv             string                 `yaml:"api_key_env"`
	ModelSource           string                 `yaml:"model_source,omitempty"`
	BaseURL               string                 `yaml:"base_url,omitempty"`
	ChatEndpointPath      string                 `yaml:"chat_endpoint_path,omitempty"`
	DiscoveryEndpointPath string                 `yaml:"discovery_endpoint_path,omitempty"`
	Models                []map[string]any       `yaml:"models,omitempty"`
	OpenAICompatible      legacyProviderProtocol `yaml:"openai_compatible,omitempty"`
	Gemini                legacyProviderProtocol `yaml:"gemini,omitempty"`
	Anthropic             legacyProviderProtocol `yaml:"anthropic,omitempty"`
}

// legacyProviderProtocol 描述旧版 provider.yaml 里各 driver 嵌套块的公共可迁移字段。
type legacyProviderProtocol struct {
	BaseURL               string `yaml:"base_url,omitempty"`
	ChatEndpointPath      string `yaml:"chat_endpoint_path,omitempty"`
	DiscoveryEndpointPath string `yaml:"discovery_endpoint_path,omitempty"`
}

// hasValue 判断旧块是否含有可迁移信息。
func (l legacyProviderProtocol) hasValue() bool {
	return strings.TrimSpace(l.BaseURL) != "" ||
		strings.TrimSpace(l.ChatEndpointPath) != "" ||
		strings.TrimSpace(l.DiscoveryEndpointPath) != ""
}

// migrationResult 汇总单个 provider.yaml 的迁移结果。
type migrationResult struct {
	Path     string
	Changed  bool
	Backup   string
	Reason   string
	FileName string
}

// main 解析命令参数并执行 provider.yaml 迁移。
func main() {
	baseDirDefault := defaultBaseDir()
	baseDir := flag.String("base-dir", baseDirDefault, "NeoCode 配置根目录（默认 ~/.neocode）")
	target := flag.String("target", "", "指定待迁移文件或目录；为空时扫描 <base-dir>/providers")
	dryRun := flag.Bool("dry-run", false, "仅预览迁移结果，不写入文件")
	flag.Parse()

	results, err := runMigration(strings.TrimSpace(*baseDir), strings.TrimSpace(*target), *dryRun)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "迁移失败: %v\n", err)
		os.Exit(1)
	}

	changed := 0
	for _, result := range results {
		if result.Changed {
			changed++
			if *dryRun {
				fmt.Printf("[DRY-RUN] 将迁移: %s\n", result.Path)
			} else {
				fmt.Printf("已迁移: %s (备份: %s)\n", result.Path, result.Backup)
			}
			continue
		}
		if result.Reason != "" {
			fmt.Printf("跳过: %s (%s)\n", result.Path, result.Reason)
		}
	}

	fmt.Printf("完成: 总计 %d 个文件，发生迁移 %d 个\n", len(results), changed)
}

// runMigration 收集目标 provider.yaml 文件并逐个执行迁移。
func runMigration(baseDir string, target string, dryRun bool) ([]migrationResult, error) {
	files, err := collectProviderFiles(baseDir, target)
	if err != nil {
		return nil, err
	}

	results := make([]migrationResult, 0, len(files))
	for _, file := range files {
		result, err := migrateProviderFile(file, dryRun)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

// collectProviderFiles 解析 target 入参并返回需要处理的 provider.yaml 列表。
func collectProviderFiles(baseDir string, target string) ([]string, error) {
	if target != "" {
		info, err := os.Stat(target)
		if err != nil {
			return nil, fmt.Errorf("stat target: %w", err)
		}
		if info.IsDir() {
			return collectProviderFilesFromProvidersDir(target)
		}
		if filepath.Base(target) != providerConfigFileName {
			return nil, fmt.Errorf("target must be %s or a providers dir", providerConfigFileName)
		}
		return []string{target}, nil
	}

	providersDir := filepath.Join(baseDir, "providers")
	return collectProviderFilesFromProvidersDir(providersDir)
}

// collectProviderFilesFromProvidersDir 从 providers 目录中收集所有 provider.yaml 文件。
func collectProviderFilesFromProvidersDir(providersDir string) ([]string, error) {
	entries, err := os.ReadDir(providersDir)
	if err != nil {
		return nil, fmt.Errorf("read providers dir: %w", err)
	}

	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		filePath := filepath.Join(providersDir, entry.Name(), providerConfigFileName)
		if _, err := os.Stat(filePath); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("stat provider file %s: %w", filePath, err)
		}
		files = append(files, filePath)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no %s found under %s", providerConfigFileName, providersDir)
	}
	return files, nil
}

// migrateProviderFile 对单个 provider.yaml 执行迁移并在非 dry-run 模式下落盘。
func migrateProviderFile(path string, dryRun bool) (migrationResult, error) {
	result := migrationResult{Path: path, FileName: filepath.Base(path)}

	raw, err := os.ReadFile(path)
	if err != nil {
		return result, fmt.Errorf("read %s: %w", path, err)
	}

	migrated, changed, err := migrateProviderContent(raw)
	if err != nil {
		return result, fmt.Errorf("migrate %s: %w", path, err)
	}
	if !changed {
		result.Reason = "未检测到旧版嵌套字段"
		return result, nil
	}

	result.Changed = true
	if dryRun {
		return result, nil
	}

	backup := path + ".bak"
	if err := os.WriteFile(backup, raw, 0o644); err != nil {
		return result, fmt.Errorf("write backup %s: %w", backup, err)
	}
	if err := os.WriteFile(path, migrated, 0o644); err != nil {
		return result, fmt.Errorf("write migrated %s: %w", path, err)
	}
	result.Backup = backup
	return result, nil
}

// migrateProviderContent 将旧版嵌套字段收敛为当前扁平字段。
func migrateProviderContent(raw []byte) ([]byte, bool, error) {
	var cfg migrationFile
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, false, err
	}

	legacy := selectLegacyBlock(cfg)
	if !legacy.hasValue() {
		return raw, false, nil
	}

	if strings.TrimSpace(cfg.BaseURL) == "" {
		cfg.BaseURL = strings.TrimSpace(legacy.BaseURL)
	}
	if strings.TrimSpace(cfg.ChatEndpointPath) == "" {
		cfg.ChatEndpointPath = strings.TrimSpace(legacy.ChatEndpointPath)
	}
	if strings.TrimSpace(cfg.DiscoveryEndpointPath) == "" {
		cfg.DiscoveryEndpointPath = strings.TrimSpace(legacy.DiscoveryEndpointPath)
	}

	cfg.OpenAICompatible = legacyProviderProtocol{}
	cfg.Gemini = legacyProviderProtocol{}
	cfg.Anthropic = legacyProviderProtocol{}

	out, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}

// selectLegacyBlock 按 driver 优先选择对应旧版块，若 driver 未命中则回退到首个非空块。
func selectLegacyBlock(cfg migrationFile) legacyProviderProtocol {
	driver := normalizeDriver(cfg.Driver)
	switch driver {
	case "openaicompat":
		if cfg.OpenAICompatible.hasValue() {
			return cfg.OpenAICompatible
		}
	case "gemini":
		if cfg.Gemini.hasValue() {
			return cfg.Gemini
		}
	case "anthropic":
		if cfg.Anthropic.hasValue() {
			return cfg.Anthropic
		}
	}

	for _, candidate := range []legacyProviderProtocol{cfg.OpenAICompatible, cfg.Gemini, cfg.Anthropic} {
		if candidate.hasValue() {
			return candidate
		}
	}
	return legacyProviderProtocol{}
}

// normalizeDriver 统一 driver 字段，避免大小写和空白导致分支漂移。
func normalizeDriver(driver string) string {
	return strings.ToLower(strings.TrimSpace(driver))
}

// defaultBaseDir 返回当前用户主目录下默认的 NeoCode 配置路径。
func defaultBaseDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join("~", defaultNeoCodeDirName)
	}
	return filepath.Join(home, defaultNeoCodeDirName)
}
