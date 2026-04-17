package state

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"neo-code/internal/config"
	"neo-code/internal/provider"
)

var persistUserEnvVarForCreate = config.PersistUserEnvVar
var deleteUserEnvVarForCreate = config.DeleteUserEnvVar
var lookupUserEnvVarForCreate = config.LookupUserEnvVar
var saveCustomProviderForCreate = config.SaveCustomProvider

const providerCreateRollbackReloadTimeout = 3 * time.Second
const providerCreateCrossProcessLockName = ".provider-create.lock"
const providerCreateCrossProcessLockRetryInterval = 50 * time.Millisecond

// CreateCustomProviderInput 定义新增自定义 Provider 所需的输入参数。
type CreateCustomProviderInput struct {
	Name           string
	Driver         string
	BaseURL        string
	APIKeyEnv      string
	APIKey         string
	APIStyle       string
	DeploymentMode string
	APIVersion     string
}

type createCustomProviderNormalizedInput struct {
	Name           string
	Driver         string
	BaseURL        string
	APIKeyEnv      string
	APIKey         string
	APIStyle       string
	DeploymentMode string
	APIVersion     string
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
			providerSaved,
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

	providerSaved = true
	if err := saveCustomProviderForCreate(
		s.manager.BaseDir(),
		normalized.Name,
		normalized.Driver,
		normalized.BaseURL,
		normalized.APIKeyEnv,
		normalized.APIStyle,
		normalized.DeploymentMode,
		normalized.APIVersion,
	); err != nil {
		return Selection{}, rollback(fmt.Errorf("selection: save provider config: %w", err))
	}

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
	normalized := createCustomProviderNormalizedInput{
		Name:           strings.TrimSpace(input.Name),
		Driver:         strings.TrimSpace(input.Driver),
		BaseURL:        strings.TrimSpace(input.BaseURL),
		APIKeyEnv:      strings.TrimSpace(input.APIKeyEnv),
		APIKey:         strings.TrimSpace(input.APIKey),
		APIStyle:       strings.TrimSpace(input.APIStyle),
		DeploymentMode: strings.TrimSpace(input.DeploymentMode),
		APIVersion:     strings.TrimSpace(input.APIVersion),
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

	return normalized, nil
}

// validateCustomProviderCreateConflict 校验新增 Provider 的名称与环境变量名是否与现有配置冲突。
func validateCustomProviderCreateConflict(cfg config.Config, input createCustomProviderNormalizedInput) error {
	existingProvider, err := cfg.ProviderByName(input.Name)
	if err == nil && existingProvider.Source == config.ProviderSourceBuiltin {
		return fmt.Errorf("selection: provider %q duplicates builtin provider", input.Name)
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
	providerSaved bool,
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

	if providerSaved {
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
			return func() {
				_ = os.Remove(lockPath)
			}, nil
		} else if !os.IsExist(err) {
			return nil, fmt.Errorf("selection: acquire create lock: %w", err)
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
