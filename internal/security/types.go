package security

import (
	"errors"
	"fmt"
	"strings"
)

// Decision is the deterministic outcome returned by the permission engine.
type Decision string

const (
	DecisionAllow Decision = "allow"
	DecisionDeny  Decision = "deny"
	DecisionAsk   Decision = "ask"
)

// Validate reports whether the decision is supported.
func (d Decision) Validate() error {
	switch d {
	case DecisionAllow, DecisionDeny, DecisionAsk:
		return nil
	default:
		return fmt.Errorf("security: invalid decision %q", d)
	}
}

// ActionType is the top-level normalized security action category.
type ActionType string

const (
	ActionTypeBash  ActionType = "bash"
	ActionTypeRead  ActionType = "read"
	ActionTypeWrite ActionType = "write"
	ActionTypeMCP   ActionType = "mcp"
)

// Validate reports whether the action type is supported.
func (a ActionType) Validate() error {
	switch a {
	case ActionTypeBash, ActionTypeRead, ActionTypeWrite, ActionTypeMCP:
		return nil
	default:
		return fmt.Errorf("security: invalid action type %q", a)
	}
}

// TargetType describes the resource type an action intends to touch.
type TargetType string

const (
	TargetTypePath      TargetType = "path"
	TargetTypeDirectory TargetType = "directory"
	TargetTypeCommand   TargetType = "command"
	TargetTypeURL       TargetType = "url"
	TargetTypeMCP       TargetType = "mcp"
)

// ActionPayload is the normalized structured context used by policy and sandbox.
type ActionPayload struct {
	ToolName          string
	Resource          string
	Operation         string
	SessionID         string
	Workdir           string
	TargetType        TargetType
	Target            string
	SandboxTargetType TargetType
	SandboxTarget     string
}

// Action is the unified security input for one tool execution request.
type Action struct {
	Type    ActionType
	Payload ActionPayload
}

// Validate reports whether the action is usable by the permission engine.
func (a Action) Validate() error {
	if err := a.Type.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(a.Payload.ToolName) == "" {
		return errors.New("security: action payload tool_name is empty")
	}
	if strings.TrimSpace(a.Payload.Resource) == "" {
		return errors.New("security: action payload resource is empty")
	}
	return nil
}

// Rule is one deterministic permission rule. Empty Type, Resource, or TargetPrefix act
// as wildcards.
type Rule struct {
	ID           string
	Type         ActionType
	Resource     string
	TargetPrefix string
	Decision     Decision
	Reason       string
}

// Validate reports whether the rule is well formed.
func (r Rule) Validate() error {
	if err := r.Decision.Validate(); err != nil {
		return err
	}
	if r.Type != "" {
		if err := r.Type.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// CheckResult is the permission engine response for one action.
type CheckResult struct {
	Decision Decision
	Action   Action
	Rule     *Rule
	Reason   string
}
