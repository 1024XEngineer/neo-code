package runtime

import (
	"runtime"
	"sync"

	"neo-code/internal/subagent"
)

type subAgentFactoryRegistry struct {
	mu      sync.RWMutex
	factory map[*Service]subagent.Factory
	tracked map[*Service]struct{}
}

var globalSubAgentFactories = &subAgentFactoryRegistry{
	factory: make(map[*Service]subagent.Factory),
	tracked: make(map[*Service]struct{}),
}

// ensureTracked 为 Service 注册 GC 回调，避免全局注册表持有泄漏。
func (r *subAgentFactoryRegistry) ensureTracked(s *Service) {
	if s == nil {
		return
	}

	r.mu.Lock()
	if _, ok := r.tracked[s]; ok {
		r.mu.Unlock()
		return
	}
	r.tracked[s] = struct{}{}
	r.mu.Unlock()

	runtime.SetFinalizer(s, func(service *Service) {
		globalSubAgentFactories.mu.Lock()
		delete(globalSubAgentFactories.factory, service)
		delete(globalSubAgentFactories.tracked, service)
		globalSubAgentFactories.mu.Unlock()
	})
}

// set 保存 Service 级工厂实例。
func (r *subAgentFactoryRegistry) set(s *Service, f subagent.Factory) {
	if s == nil {
		return
	}
	r.ensureTracked(s)

	r.mu.Lock()
	r.factory[s] = f
	r.mu.Unlock()
}

// get 读取 Service 级工厂实例。
func (r *subAgentFactoryRegistry) get(s *Service) (subagent.Factory, bool) {
	if s == nil {
		return nil, false
	}
	r.ensureTracked(s)

	r.mu.RLock()
	factory, ok := r.factory[s]
	r.mu.RUnlock()
	return factory, ok
}

// defaultSubAgentFactory 返回默认的子代理工厂实例。
func defaultSubAgentFactory() subagent.Factory {
	return subagent.NewWorkerFactory(nil)
}

// SetSubAgentFactory 设置子代理运行时工厂；传入 nil 时回退到默认工厂。
func (s *Service) SetSubAgentFactory(factory subagent.Factory) {
	if s == nil {
		return
	}
	if factory == nil {
		globalSubAgentFactories.set(s, defaultSubAgentFactory())
		return
	}
	globalSubAgentFactories.set(s, factory)
}

// SubAgentFactory 返回当前 runtime 持有的子代理运行时工厂。
func (s *Service) SubAgentFactory() subagent.Factory {
	if s == nil {
		return defaultSubAgentFactory()
	}
	if factory, ok := globalSubAgentFactories.get(s); ok && factory != nil {
		return factory
	}

	defaultFactory := defaultSubAgentFactory()
	globalSubAgentFactories.set(s, defaultFactory)
	return defaultFactory
}
