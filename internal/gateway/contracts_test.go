package gateway

import "context"

// runtimePortCompileStub 用于编译期验证 RuntimePort 契约完整性。
type runtimePortCompileStub struct{}

func (s *runtimePortCompileStub) Run(_ context.Context, _ RunInput) error {
	return nil
}

func (s *runtimePortCompileStub) Compact(_ context.Context, _ CompactInput) (CompactResult, error) {
	return CompactResult{}, nil
}

func (s *runtimePortCompileStub) ResolvePermission(_ context.Context, _ PermissionResolutionInput) error {
	return nil
}

func (s *runtimePortCompileStub) CancelActiveRun() bool {
	return false
}

func (s *runtimePortCompileStub) Events() <-chan RuntimeEvent {
	return nil
}

func (s *runtimePortCompileStub) ListSessions(_ context.Context) ([]SessionSummary, error) {
	return nil, nil
}

func (s *runtimePortCompileStub) LoadSession(_ context.Context, _ string) (Session, error) {
	return Session{}, nil
}

func (s *runtimePortCompileStub) SetSessionWorkdir(_ context.Context, _, _ string) (Session, error) {
	return Session{}, nil
}

var _ RuntimePort = (*runtimePortCompileStub)(nil)
