package runtime

import (
	"testing"

	"neo-code/internal/subagent"
)

type fakeSubAgentFactory struct{}

func (fakeSubAgentFactory) Create(role subagent.Role) (subagent.WorkerRuntime, error) {
	return subagent.NewWorkerFactory(nil).Create(role)
}

func TestServiceSubAgentFactoryRegistration(t *testing.T) {
	t.Parallel()

	svc := NewWithFactory(nil, nil, nil, nil, nil)
	if svc.SubAgentFactory() == nil {
		t.Fatalf("expected default sub-agent factory")
	}

	custom := fakeSubAgentFactory{}
	svc.SetSubAgentFactory(custom)
	if svc.SubAgentFactory() == nil {
		t.Fatalf("expected custom sub-agent factory")
	}

	svc.SetSubAgentFactory(nil)
	if svc.SubAgentFactory() == nil {
		t.Fatalf("expected reset to default sub-agent factory")
	}
}

func TestServiceSubAgentFactoryIsolationAcrossInstances(t *testing.T) {
	t.Parallel()

	svcA := NewWithFactory(nil, nil, nil, nil, nil)
	svcB := NewWithFactory(nil, nil, nil, nil, nil)

	custom := fakeSubAgentFactory{}
	svcA.SetSubAgentFactory(custom)

	if svcA.SubAgentFactory() == nil {
		t.Fatalf("expected service A factory to be set")
	}
	if svcB.SubAgentFactory() == nil {
		t.Fatalf("expected service B default factory")
	}

	if svcA.SubAgentFactory() == svcB.SubAgentFactory() {
		t.Fatalf("expected per-service factory isolation")
	}
}
