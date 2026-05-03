package cli

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"neo-code/internal/app"
	configstate "neo-code/internal/config/state"
	providertypes "neo-code/internal/provider/types"
)

// SelectionService 定义 CLI 选择类命令所需的最小能力集合。
type SelectionService interface {
	ListModels(ctx context.Context) ([]providertypes.ModelDescriptor, error)
	ListModelsSnapshot(ctx context.Context) ([]providertypes.ModelDescriptor, error)
	SetCurrentModel(ctx context.Context, modelID string) (configstate.Selection, error)
	SelectProvider(ctx context.Context, providerName string) (configstate.Selection, error)
	SelectProviderWithModel(ctx context.Context, providerName string, modelID string) (configstate.Selection, error)
	CreateCustomProvider(ctx context.Context, input configstate.CreateCustomProviderInput) (configstate.Selection, error)
	RemoveCustomProvider(ctx context.Context, name string) error
}

// selectionServiceResolver 负责在命令执行时解析并返回可复用的选择服务实例。
type selectionServiceResolver interface {
	Resolve(cmd *cobra.Command) (SelectionService, error)
}

// selectionServiceResolverFunc 允许以函数方式注入选择服务解析逻辑，方便命令层替换与测试。
type selectionServiceResolverFunc func(cmd *cobra.Command) (SelectionService, error)

// Resolve 调用函数式解析器获取 SelectionService。
func (f selectionServiceResolverFunc) Resolve(cmd *cobra.Command) (SelectionService, error) {
	if f == nil {
		return nil, fmt.Errorf("selection service resolver is nil")
	}
	return f(cmd)
}

// runtimeSelectionServiceResolver 按 workdir 维度缓存状态服务，避免同进程重复装配。
type runtimeSelectionServiceResolver struct {
	mu       sync.Mutex
	services map[string]SelectionService
}

// newRuntimeSelectionServiceResolver 创建默认 CLI 选择服务解析器。
func newRuntimeSelectionServiceResolver() selectionServiceResolver {
	return &runtimeSelectionServiceResolver{
		services: make(map[string]SelectionService),
	}
}

// Resolve 基于命令上下文与 --workdir 装配状态服务，并复用同路径缓存实例。
func (r *runtimeSelectionServiceResolver) Resolve(cmd *cobra.Command) (SelectionService, error) {
	if r == nil {
		return nil, fmt.Errorf("selection service resolver is nil")
	}

	workdir := strings.TrimSpace(mustReadInheritedWorkdir(cmd))
	cacheKey := workdir

	r.mu.Lock()
	if svc, ok := r.services[cacheKey]; ok {
		r.mu.Unlock()
		return svc, nil
	}
	r.mu.Unlock()

	ctx := context.Background()
	if cmd != nil && cmd.Context() != nil {
		ctx = cmd.Context()
	}
	shared, _, _, err := app.BuildSharedConfigDeps(ctx, app.BootstrapOptions{Workdir: workdir})
	if err != nil {
		return nil, err
	}
	if shared.ProviderSelection == nil {
		return nil, fmt.Errorf("selection service is unavailable")
	}

	r.mu.Lock()
	r.services[cacheKey] = shared.ProviderSelection
	r.mu.Unlock()
	return shared.ProviderSelection, nil
}
