package runtime

import (
	"context"
	"errors"
	"strings"
	"time"
)

// ApproveCurrentPlan 显式批准当前完整计划 revision，并安排下一轮做一次完整计划对齐。
func (s *Service) ApproveCurrentPlan(ctx context.Context, input ApproveCurrentPlanInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil {
		return errors.New("runtime: service is nil")
	}
	sessionID := strings.TrimSpace(input.SessionID)
	releaseLock := s.bindSessionLock(sessionID)
	defer releaseLock()

	session, err := s.sessionStore.LoadSession(ctx, sessionID)
	if err != nil {
		return err
	}
	if err := approveCurrentPlan(&session, input.PlanID, input.Revision); err != nil {
		return err
	}
	session.UpdatedAt = time.Now()
	return s.sessionStore.UpdateSessionState(ctx, sessionStateInputFromSession(session))
}
