package runtime

import (
	"context"
	"errors"
	"strings"
	"time"

	approvalflow "neo-code/internal/runtime/approval"
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

// ResolvePlanApproval 向等待中的计划审批流程提交用户决议，不持有会话锁以避免与运行循环死锁。
func (s *Service) ResolvePlanApproval(ctx context.Context, input ResolvePlanApprovalInput) error {
	if s == nil {
		return errors.New("runtime: service is nil")
	}
	requestID := strings.TrimSpace(input.RequestID)
	if requestID == "" {
		return errors.New("runtime: plan approval request id is empty")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	decision := approvalflow.DecisionReject
	if input.Approved {
		decision = approvalflow.DecisionAllowOnce
	}
	return s.approvalBroker.Resolve(requestID, decision)
}
