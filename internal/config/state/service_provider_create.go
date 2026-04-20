package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"neo-code/internal/config"
	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
)

var persistUserEnvVarForCreate = config.PersistUserEnvVar
var deleteUserEnvVarForCreate = config.DeleteUserEnvVar
var lookupUserEnvVarForCreate = config.LookupUserEnvVar
var saveCustomProviderWithModelsForCreate = config.SaveCustomProviderWithModels

const providerCreateRollbackReloadTimeout = 3 * time.Second
const providerCreateCrossProcessLockName = ".provider-create.lock"
const providerCreateCrossProcessLockLeaseName = ".lease"
const providerCreateCrossProcessLockRetryInterval = 50 * time.Millisecond
const providerCreateCrossProcessLockStaleThreshold = 30 * time.Second
const providerCreateCrossProcessLockHeartbeatInterval = 2 * time.Second

// CreateCustomProviderInput 定义新增自定义 Provider 所需的输入参数。
type CreateCustomProviderInput struct {
	Name                  string
	Driver                string
	BaseURL               string
	ChatAPIMode           string
	ChatEndpointPath      string
	APIKeyEnv             string
	APIKey                string
	ModelSource           string
	ManualModelsJSON      string
	DiscoveryEndpointPath string
}

type createCustomProviderNormalizedInput struct {
	Name                  string
	Driver                string
	BaseURL               string
	ChatAPIMode           string
	ChatEndpointPath      string
	APIKeyEnv             string
	APIKey                string
	ModelSource           string
	ManualModels          []providertypes.ModelDescriptor
	DiscoveryEndpointPath string
}

type providerConfigSnapshot struct {
	Exists  bool
	Content []byte
}

// CreateCustomProvider 负责创建/更新自定义 Provider，并统一处理环境变量写入与失败回滚。
func (s *Service) CreateCustomProvider(ctx context.Context, input CreateCustomProviderInput) (Selection, error) {
	if err := s.validate(); err != nil {
		return Selection{}, err
	}
	if err := ctx.Err(); err != nil {
		return Selection{}, err
	}

	normalized, err := normalizeCreateCustomProviderInput(input)
	if err != nil {
		return Selection{}, err
	}

	s.manager.LockProviderCreate()
	defer s.manager.UnlockProviderCreate()

	releaseCrossProcessLock, err := lockProviderCreateCrossProcess(ctx, s.manager.BaseDir())
	if err != nil {
		return Selection{}, err
	}
	defer releaseCrossProcessLock()

	cfgSnapshot := s.manager.Get()
	if err := validateCustomProviderCreateConflict(cfgSnapshot, normalized); err != nil {
		return Selection{}, err
	}

	previousProcessEnvValue, hadPreviousProcessEnv := os.LookupEnv(normalized.APIKeyEnv)
	previousUserEnvValue, hadPreviousUserEnv, err := lookupUserEnvVarForCreate(normalized.APIKeyEnv)
	if err != nil {
		return Selection{}, fmt.Errorf("selection: lookup user env: %w", err)
	}

	providerPath := filepath.Join(s.manager.BaseDir(), "providers", normalized.Name, "provider.yaml")
	providerSnapshot, err := loadProviderConfigSnapshot(providerPath)
	if err != nil {
		return Selection{}, fmt.Errorf("selection: snapshot provider config: %w", err)
	}

	providerSaveAttempted := false
	providerSaved := false
	userEnvPersisted := false
	processEnvApplied := false
	rollback := func(originalErr error) error {
		rolledErr := rollbackCreateCustomProvider(
			s.manager.BaseDir(),
			normalized.Name,
			normalized.APIKeyEnv,
			hadPreviousProcessEnv,
			previousProcessEnvValue,
			hadPreviousUserEnv,
			previousUserEnvValue,
			providerSaveAttempted,
			userEnvPersisted,
			processEnvApplied,
			providerSnapshot,
			originalErr,
		)
		if providerSaved {
			reloadCtx, cancel := context.WithTimeout(context.Background(), providerCreateRollbackReloadTimeout)
			defer cancel()
			if _, reloadErr := s.manager.Reload(reloadCtx); reloadErr != nil {
				return fmt.Errorf("%w (post-rollback reload failed: %v)", rolledErr, reloadErr)
			}
		}
		return rolledErr
	}

	providerSaveAttempted = true
	if err := saveCustomProviderWithModelsForCreate(s.manager.BaseDir(), config.SaveCustomProviderInput{
		Name:                  normalized.Name,
		Driver:                normalized.Driver,
		BaseURL:               normalized.BaseURL,
		ChatAPIMode:           normalized.ChatAPIMode,
		ChatEndpointPath:      normalized.ChatEndpointPath,
		APIKeyEnv:             normalized.APIKeyEnv,
		ModelSource:           normalized.ModelSource,
		Models:                normalized.ManualModels,
		DiscoveryEndpointPath: normalized.DiscoveryEndpointPath,
	}); err != nil {
		return Selection{}, rollback(fmt.Errorf("selection: save provider config: %w", err))
	}
	providerSaved = true

	if err := persistUserEnvVarForCreate(normalized.APIKeyEnv, normalized.APIKey); err != nil {
		return Selection{}, rollback(fmt.Errorf("selection: persist user env: %w", err))
	}
	userEnvPersisted = true

	if err := os.Setenv(normalized.APIKeyEnv, normalized.APIKey); err != nil {
		return Selection{}, rollback(fmt.Errorf("selection: apply process env: %w", err))
	}
	processEnvApplied = true

	if _, err := s.manager.Reload(ctx); err != nil {
		return Selection{}, rollback(fmt.Errorf("selection: reload config snapshot: %w", err))
	}

	selection, err := s.SelectProvider(ctx, normalized.Name)
	if err != nil {
		return Selection{}, rollback(fmt.Errorf("selection: select provider: %w", err))
	}

	return selection, nil
}

// normalizeCreateCustomProviderInput 统一裁剪新增 Provider 输入并执行基础字段校验。
func normalizeCreateCustomProviderInput(input CreateCustomProviderInput) (createCustomProviderNormalizedInput, error) {
	rawModelSource := strings.TrimSpace(input.ModelSource)
	normalizedModelSource := config.NormalizeModelSource(rawModelSource)
	if rawModelSource != "" && normalizedModelSource == "" {
		return createCustomProviderNormalizedInput{}, fmt.Errorf("selection: unsupported model source %q", input.ModelSource)
	}
	if normalizedModelSource == "" {
		normalizedModelSource = config.ModelSourceDiscover
	}

	normalized := createCustomProviderNormalizedInput{
		Name:                  strings.TrimSpace(input.Name),
		Driver:                strings.TrimSpace(input.Driver),
		BaseURL:               strings.TrimSpace(input.BaseURL),
		ChatAPIMode:           strings.TrimSpace(input.ChatAPIMode),
		ChatEndpointPath:      strings.TrimSpace(input.ChatEndpointPath),
		APIKeyEnv:             strings.TrimSpace(input.APIKeyEnv),
		APIKey:                strings.TrimSpace(input.APIKey),
		ModelSource:           normalizedModelSource,
		DiscoveryEndpointPath: strings.TrimSpace(input.DiscoveryEndpointPath),
	}

	if err := config.ValidateCustomProviderName(normalized.Name); err != nil {
		return createCustomProviderNormalizedInput{}, err
	}
	if normalized.Driver == "" {
		return createCustomProviderNormalizedInput{}, errors.New("selection: provider driver is empty")
	}
	if normalized.APIKey == "" {
		return createCustomProviderNormalizedInput{}, errors.New("selection: provider api key is empty")
	}
	if err := config.ValidateEnvVarName(normalized.APIKeyEnv); err != nil {
		return createCustomProviderNormalizedInput{}, err
	}
	if config.IsProtectedEnvVarName(normalized.APIKeyEnv) {
		return createCustomProviderNormalizedInput{}, fmt.Errorf("selection: env key %q is protected", normalized.APIKeyEnv)
	}
	switch normalized.ModelSource {
	case config.ModelSourceManual:
		manualModels, parseErr := parseManualModelsJSON(input.ManualModelsJSON)
		if parseErr != nil {
			return createCustomProviderNormalizedInput{}, parseErr
		}
		normalized.ManualModels = manualModels
	case config.ModelSourceDiscover:
		normalized.ManualModels = nil
	}

	normalizedProviderInput, err := config.NormalizeCustomProviderInput(config.SaveCustomProviderInput{
		Name:                  normalized.Name,
		Driver:                normalized.Driver,
		BaseURL:               normalized.BaseURL,
		ChatAPIMode:           normalized.ChatAPIMode,
		ChatEndpointPath:      normalized.ChatEndpointPath,
		APIKeyEnv:             normalized.APIKeyEnv,
		DiscoveryEndpointPath: normalized.DiscoveryEndpointPath,
		ModelSource:           normalized.ModelSource,
		Models:                normalized.ManualModels,
	})
	if err != nil {
		return createCustomProviderNormalizedInput{}, err
	}

	normalized.Name = normalizedProviderInput.Name
	normalized.Driver = normalizedProviderInput.Driver
	normalized.BaseURL = normalizedProviderInput.BaseURL
	normalized.ChatAPIMode = normalizedProviderInput.ChatAPIMode
	normalized.ChatEndpointPath = normalizedProviderInput.ChatEndpointPath
	normalized.APIKeyEnv = normalizedProviderInput.APIKeyEnv
	normalized.ModelSource = normalizedProviderInput.ModelSource
	normalized.DiscoveryEndpointPath = normalizedProviderInput.DiscoveryEndpointPath
	normalized.ManualModels = normalizedProviderInput.Models

	return normalized, nil
}

type manualModelJSON struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	ContextWindow   *int   `json:"context_window,omitempty"`
	MaxOutputTokens *int   `json:"max_output_tokens,omitempty"`
}

// parseManualModelsJSON 解析并校验手工模型 JSON，确保至少包含一个合法模型且 id/name 必填。
func parseManualModelsJSON(raw string) ([]providertypes.ModelDescriptor, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, errors.New("selection: manual model json is empty")
	}

	decoder := json.NewDecoder(strings.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	var models []manualModelJSON
	if err := decoder.Decode(&models); err != nil {
		return nil, fmt.Errorf("selection: parse manual model json: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, errors.New("selection: parse manual model json: unexpected trailing content")
	}
	if len(models) == 0 {
		return nil, errors.New("selection: manual model list is empty")
	}

	descriptors := make([]providertypes.ModelDescriptor, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	for index, model := range models {
		id := strings.TrimSpace(model.ID)
		name := strings.TrimSpace(model.Name)
		if id == "" {
			return nil, fmt.Errorf("selection: models[%d].id is required", index)
		}
		if name == "" {
			return nil, fmt.Errorf("selection: models[%d].name is required", index)
		}
		key := provider.NormalizeKey(id)
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("selection: models[%d].id %q is duplicated", index, id)
		}
		seen[key] = struct{}{}

		descriptor := providertypes.ModelDescriptor{
			ID:              id,
			Name:            name,
			ContextWindow:   config.ManualModelOptionalIntUnset,
			MaxOutputTokens: config.ManualModelOptionalIntUnset,
		}
		if model.ContextWindow != nil {
			if *model.ContextWindow <= 0 {
				return nil, fmt.Errorf("selection: models[%d].context_window must be greater than 0", index)
			}
			descriptor.ContextWindow = *model.ContextWindow
		}
		if model.MaxOutputTokens != nil {
			if *model.MaxOutputTokens <= 0 {
				return nil, fmt.Errorf("selection: models[%d].max_output_tokens must be greater than 0", index)
			}
			descriptor.MaxOutputTokens = *model.MaxOutputTokens
		}
		descriptors = append(descriptors, descriptor)
	}
	return providertypes.MergeModelDescriptors(descriptors), nil
}

// validateCustomProviderCreateConflict 校验新增 Provider 的名称与环境变量名是否与现有配置冲突。
func validateCustomProviderCreateConflict(cfg config.Config, input createCustomProviderNormalizedInput) error {
	existingProvider, err := cfg.ProviderByName(input.Name)
	if err == nil {
		if existingProvider.Source == config.ProviderSourceBuiltin {
			return fmt.Errorf("selection: provider %q duplicates builtin provider", input.Name)
		}
		return fmt.Errorf("selection: provider %q already exists", input.Name)
	}

	targetProviderName := provider.NormalizeKey(input.Name)
	targetEnvName := config.NormalizeEnvVarNameForCompare(input.APIKeyEnv)
	for _, providerCfg := range cfg.Providers {
		if provider.NormalizeKey(providerCfg.Name) == targetProviderName {
			continue
		}
		if config.NormalizeEnvVarNameForCompare(providerCfg.APIKeyEnv) == targetEnvName {
			return fmt.Errorf(
				"selection: env key %q duplicates provider %q",
				input.APIKeyEnv,
				strings.TrimSpace(providerCfg.Name),
			)
		}
	}
	return nil
}

// loadProviderConfigSnapshot 读取 provider.yaml 快照，用于失败回滚恢复原始状态。
func loadProviderConfigSnapshot(path string) (providerConfigSnapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return providerConfigSnapshot{}, nil
		}
		return providerConfigSnapshot{}, err
	}
	return providerConfigSnapshot{
		Exists:  true,
		Content: append([]byte(nil), data...),
	}, nil
}

// rollbackCreateCustomProvider 回滚新增 Provider 过程中的副作用，保证失败后状态一致。
func rollbackCreateCustomProvider(
	baseDir string,
	providerName string,
	apiKeyEnv string,
	hadPreviousProcessEnv bool,
	previousProcessEnvValue string,
	hadPreviousUserEnv bool,
	previousUserEnvValue string,
	providerSaveAttempted bool,
	userEnvPersisted bool,
	processEnvApplied bool,
	providerSnapshot providerConfigSnapshot,
	originalErr error,
) error {
	rollbackErrs := make([]error, 0, 3)

	if processEnvApplied {
		if hadPreviousProcessEnv {
			if err := os.Setenv(apiKeyEnv, previousProcessEnvValue); err != nil {
				rollbackErrs = append(rollbackErrs, fmt.Errorf("restore process env: %w", err))
			}
		} else if err := os.Unsetenv(apiKeyEnv); err != nil {
			rollbackErrs = append(rollbackErrs, fmt.Errorf("unset process env: %w", err))
		}
	}

	if userEnvPersisted {
		if hadPreviousUserEnv {
			if err := persistUserEnvVarForCreate(apiKeyEnv, previousUserEnvValue); err != nil {
				rollbackErrs = append(rollbackErrs, fmt.Errorf("restore user env: %w", err))
			}
		} else if err := deleteUserEnvVarForCreate(apiKeyEnv); err != nil {
			rollbackErrs = append(rollbackErrs, fmt.Errorf("delete user env: %w", err))
		}
	}

	if providerSaveAttempted {
		if err := restoreProviderConfigSnapshot(baseDir, providerName, providerSnapshot); err != nil {
			rollbackErrs = append(rollbackErrs, fmt.Errorf("restore provider config: %w", err))
		}
	}

	if len(rollbackErrs) == 0 {
		return originalErr
	}
	return fmt.Errorf("%w (rollback failed: %v)", originalErr, errors.Join(rollbackErrs...))
}

// restoreProviderConfigSnapshot 恢复 provider.yaml 快照；若原先不存在则删除新增目录。
func restoreProviderConfigSnapshot(baseDir string, providerName string, snapshot providerConfigSnapshot) error {
	providerDir := filepath.Join(baseDir, "providers", providerName)
	if !snapshot.Exists {
		return config.DeleteCustomProvider(baseDir, providerName)
	}
	if err := os.RemoveAll(providerDir); err != nil {
		return err
	}
	if err := os.MkdirAll(providerDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(providerDir, "provider.yaml"), snapshot.Content, 0o644)
}

// lockProviderCreateCrossProcess 使用基于目录的锁，串行化同一工作目录下跨进程的创建流程。
func lockProviderCreateCrossProcess(ctx context.Context, baseDir string) (func(), error) {
	lockPath := filepath.Join(baseDir, providerCreateCrossProcessLockName)
	for {
		if err := os.Mkdir(lockPath, 0o700); err == nil {
			if err := touchProviderCreateLockLease(lockPath, time.Now()); err != nil {
				_ = os.RemoveAll(lockPath)
				return nil, fmt.Errorf("selection: initialize create lock lease: %w", err)
			}
			stopHeartbeat := startProviderCreateLockHeartbeat(lockPath)
			return func() {
				stopHeartbeat()
				_ = os.RemoveAll(lockPath)
			}, nil
		} else if !os.IsExist(err) {
			return nil, fmt.Errorf("selection: acquire create lock: %w", err)
		}
		reclaimed, reclaimErr := tryReclaimStaleProviderCreateLock(lockPath, time.Now())
		if reclaimErr != nil {
			return nil, fmt.Errorf("selection: reclaim stale create lock: %w", reclaimErr)
		}
		if reclaimed {
			continue
		}

		timer := time.NewTimer(providerCreateCrossProcessLockRetryInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

// startProviderCreateLockHeartbeat 周期性刷新锁租约，避免长流程被误判为陈旧锁。
func startProviderCreateLockHeartbeat(lockPath string) func() {
	done := make(chan struct{})
	var once sync.Once

	go func() {
		ticker := time.NewTicker(providerCreateCrossProcessLockHeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				_ = touchProviderCreateLockLease(lockPath, time.Now())
			}
		}
	}()

	return func() {
		once.Do(func() {
			close(done)
		})
	}
}

// tryReclaimStaleProviderCreateLock 尝试回收超时未续租的锁目录。
func tryReclaimStaleProviderCreateLock(lockPath string, now time.Time) (bool, error) {
	leasePath := providerCreateLockLeasePath(lockPath)
	info, err := os.Stat(leasePath)
	if err != nil {
		if !os.IsNotExist(err) {
			return false, err
		}
		// 兼容旧锁：租约文件缺失时使用目录时间判定陈旧。
		info, err = os.Stat(lockPath)
		if err != nil {
			if os.IsNotExist(err) {
				return true, nil
			}
			return false, err
		}
	}

	if now.Sub(info.ModTime()) < providerCreateCrossProcessLockStaleThreshold {
		return false, nil
	}
	if err := os.RemoveAll(lockPath); err != nil && !os.IsNotExist(err) {
		return false, err
	}
	return true, nil
}

// touchProviderCreateLockLease 写入/刷新锁租约时间戳。
func touchProviderCreateLockLease(lockPath string, now time.Time) error {
	leasePath := providerCreateLockLeasePath(lockPath)
	if err := os.WriteFile(leasePath, []byte(now.UTC().Format(time.RFC3339Nano)), 0o600); err != nil {
		return err
	}
	return os.Chtimes(leasePath, now, now)
}

// providerCreateLockLeasePath 返回锁租约文件路径。
func providerCreateLockLeasePath(lockPath string) string {
	return filepath.Join(lockPath, providerCreateCrossProcessLockLeaseName)
}
