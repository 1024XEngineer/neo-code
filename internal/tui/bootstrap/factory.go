package bootstrap

import (
	agentruntime "neo-code/internal/runtime"
)

// ServiceFactory 定义 runtime、provider 与工作区切换服务的可切换装配策略。
type ServiceFactory interface {
	// BuildRuntime 根据 mode 返回实际注入到 TUI 的 runtime 实现。
	BuildRuntime(mode Mode, current agentruntime.Runtime) (agentruntime.Runtime, error)
	// BuildProvider 根据 mode 返回实际注入到 TUI 的 provider service 实现。
	BuildProvider(mode Mode, current ProviderService) (ProviderService, error)
	// BuildWorkspaceSwitcher 根据 mode 返回实际注入到 TUI 的工作区切换服务。
	BuildWorkspaceSwitcher(mode Mode, current WorkspaceSwitcher) (WorkspaceSwitcher, error)
}

type passthroughFactory struct{}

// BuildRuntime 默认直接透传已有 runtime，不做替换。
func (passthroughFactory) BuildRuntime(mode Mode, current agentruntime.Runtime) (agentruntime.Runtime, error) {
	return current, nil
}

// BuildProvider 默认直接透传已有 provider service，不做替换。
func (passthroughFactory) BuildProvider(mode Mode, current ProviderService) (ProviderService, error) {
	return current, nil
}

// BuildWorkspaceSwitcher 默认直接透传已有工作区切换服务，不做替换。
func (passthroughFactory) BuildWorkspaceSwitcher(mode Mode, current WorkspaceSwitcher) (WorkspaceSwitcher, error) {
	return current, nil
}
