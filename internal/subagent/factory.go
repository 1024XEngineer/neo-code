package subagent

import "time"

// EngineBuilder 定义基于角色策略构建执行引擎的工厂函数。
type EngineBuilder func(role Role, policy RolePolicy) Engine

// WorkerFactory 是默认的 WorkerRuntime 工厂实现。
type WorkerFactory struct {
	builder EngineBuilder
}

// NewWorkerFactory 创建 WorkerFactory；当 builder 为空时使用默认引擎。
func NewWorkerFactory(builder EngineBuilder) *WorkerFactory {
	return &WorkerFactory{builder: builder}
}

// Create 基于角色创建对应策略与执行引擎的 WorkerRuntime。
func (f *WorkerFactory) Create(role Role) (WorkerRuntime, error) {
	policy, err := DefaultRolePolicy(role)
	if err != nil {
		return nil, err
	}
	// 兜底保证默认预算始终可用。
	policy.DefaultBudget = policy.DefaultBudget.normalize(Budget{
		MaxSteps: defaultPolicyMaxSteps,
		Timeout:  defaultPolicyTimeout * time.Second,
	})

	var engine Engine
	if f != nil && f.builder != nil {
		engine = f.builder(role, policy)
	}
	return NewWorker(role, policy, engine)
}

