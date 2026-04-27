package gateway

import (
	"context"
	"testing"
)

func TestNewActionRegistryHasCoreHandlers(t *testing.T) {
	registry := NewActionRegistry()

	coreActions := []FrameAction{
		FrameActionAuthenticate,
		FrameActionPing,
		FrameActionBindStream,
		FrameActionWakeOpenURL,
		FrameActionRun,
		FrameActionCompact,
		FrameActionExecuteSystemTool,
		FrameActionActivateSessionSkill,
		FrameActionDeactivateSessionSkill,
		FrameActionListSessionSkills,
		FrameActionListAvailableSkills,
		FrameActionCancel,
		FrameActionListSessions,
		FrameActionLoadSession,
		FrameActionResolvePermission,
	}

	for _, action := range coreActions {
		if _, ok := registry.Lookup(action); !ok {
			t.Fatalf("expected core action %q to be registered", action)
		}
	}
}

func TestActionRegistryRegisterRejectsCoreOverride(t *testing.T) {
	registry := NewActionRegistry()
	err := registry.Register(FrameActionPing, func(context.Context, MessageFrame, RuntimePort) MessageFrame {
		return MessageFrame{Type: FrameTypeAck}
	})
	if err == nil {
		t.Fatal("expected override of core handler to fail")
	}
}

func TestActionRegistryRegisterAndLookupExtension(t *testing.T) {
	registry := NewActionRegistry()
	handler := func(_ context.Context, frame MessageFrame, _ RuntimePort) MessageFrame {
		return MessageFrame{
			Type:      FrameTypeAck,
			Action:    frame.Action,
			RequestID: frame.RequestID,
		}
	}

	if err := registry.Register(FrameAction("test.extension"), handler); err != nil {
		t.Fatalf("register extension handler: %v", err)
	}

	loaded, ok := registry.Lookup(FrameAction("test.extension"))
	if !ok {
		t.Fatal("expected extension handler to be discoverable")
	}
	if loaded == nil {
		t.Fatal("expected extension handler to be non-nil")
	}
}

func TestActionRegistryRegisterRejectsDuplicateExtension(t *testing.T) {
	registry := NewActionRegistry()
	handler := func(context.Context, MessageFrame, RuntimePort) MessageFrame {
		return MessageFrame{Type: FrameTypeAck}
	}

	if err := registry.Register(FrameAction("test.duplicate"), handler); err != nil {
		t.Fatalf("first register should succeed: %v", err)
	}
	if err := registry.Register(FrameAction("test.duplicate"), handler); err == nil {
		t.Fatal("expected duplicate extension registration to fail")
	}
}

func TestActionRegistryRegisterRejectsInvalidInputs(t *testing.T) {
	registry := NewActionRegistry()

	if err := registry.Register("", func(context.Context, MessageFrame, RuntimePort) MessageFrame {
		return MessageFrame{Type: FrameTypeAck}
	}); err == nil {
		t.Fatal("expected empty action to fail")
	}
	if err := registry.Register(FrameAction("test.nil"), nil); err == nil {
		t.Fatal("expected nil handler to fail")
	}
}

func TestActionRegistryMustRegisterPanics(t *testing.T) {
	registry := NewActionRegistry()

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic when must register fails")
		}
	}()
	registry.MustRegister(FrameActionPing, func(context.Context, MessageFrame, RuntimePort) MessageFrame {
		return MessageFrame{Type: FrameTypeAck}
	})
}
