package gateway

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// ActionRegistry stores core and extension request handlers.
type ActionRegistry struct {
	mu sync.RWMutex

	core     map[FrameAction]requestFrameHandler
	extended map[FrameAction]requestFrameHandler
}

var defaultRegistry = NewActionRegistry()

// NewActionRegistry creates a registry with all core handlers preloaded.
func NewActionRegistry() *ActionRegistry {
	registry := &ActionRegistry{
		core:     make(map[FrameAction]requestFrameHandler),
		extended: make(map[FrameAction]requestFrameHandler),
	}
	registry.initCore()
	return registry
}

// initCore registers built-in handlers that cannot be overridden.
func (r *ActionRegistry) initCore() {
	if r == nil {
		return
	}
	r.core[FrameActionAuthenticate] = func(ctx context.Context, frame MessageFrame, _ RuntimePort) MessageFrame {
		return handleAuthenticateFrame(ctx, frame)
	}
	r.core[FrameActionPing] = func(ctx context.Context, frame MessageFrame, _ RuntimePort) MessageFrame {
		return handlePingFrame(ctx, frame)
	}
	r.core[FrameActionBindStream] = func(ctx context.Context, frame MessageFrame, _ RuntimePort) MessageFrame {
		return handleBindStreamFrame(ctx, frame)
	}
	r.core[FrameActionWakeOpenURL] = func(ctx context.Context, frame MessageFrame, runtimePort RuntimePort) MessageFrame {
		return handleWakeOpenURLFrame(ctx, frame, runtimePort)
	}
	r.core[FrameActionRun] = handleRunFrame
	r.core[FrameActionCompact] = handleCompactFrame
	r.core[FrameActionExecuteSystemTool] = handleExecuteSystemToolFrame
	r.core[FrameActionActivateSessionSkill] = handleActivateSessionSkillFrame
	r.core[FrameActionDeactivateSessionSkill] = handleDeactivateSessionSkillFrame
	r.core[FrameActionListSessionSkills] = handleListSessionSkillsFrame
	r.core[FrameActionListAvailableSkills] = handleListAvailableSkillsFrame
	r.core[FrameActionCancel] = handleCancelFrame
	r.core[FrameActionListSessions] = handleListSessionsFrame
	r.core[FrameActionLoadSession] = handleLoadSessionFrame
	r.core[FrameActionListSessionTodos] = handleListSessionTodosFrame
	r.core[FrameActionGetRuntimeSnapshot] = handleGetRuntimeSnapshotFrame
	r.core[FrameActionResolvePermission] = handleResolvePermissionFrame
	r.core[FrameActionDeleteSession] = handleDeleteSessionFrame
	r.core[FrameActionRenameSession] = handleRenameSessionFrame
	r.core[FrameActionListFiles] = handleListFilesFrame
	r.core[FrameActionListModels] = handleListModelsFrame
	r.core[FrameActionSetSessionModel] = handleSetSessionModelFrame
	r.core[FrameActionGetSessionModel] = handleGetSessionModelFrame
	r.core[FrameActionListProviders] = handleListProvidersFrame
	r.core[FrameActionCreateCustomProvider] = handleCreateCustomProviderFrame
	r.core[FrameActionDeleteCustomProvider] = handleDeleteCustomProviderFrame
	r.core[FrameActionSelectProviderModel] = handleSelectProviderModelFrame
	r.core[FrameActionListMCPServers] = handleListMCPServersFrame
	r.core[FrameActionUpsertMCPServer] = handleUpsertMCPServerFrame
	r.core[FrameActionSetMCPServerEnabled] = handleSetMCPServerEnabledFrame
	r.core[FrameActionDeleteMCPServer] = handleDeleteMCPServerFrame
	r.core[FrameActionListCheckpoints] = handleListCheckpointsFrame
	r.core[FrameActionRestoreCheckpoint] = handleRestoreCheckpointFrame
	r.core[FrameActionUndoRestore] = handleUndoRestoreFrame
	r.core[FrameActionCheckpointDiff] = handleCheckpointDiffFrame
}

// Lookup returns the handler for an action.
func (r *ActionRegistry) Lookup(action FrameAction) (requestFrameHandler, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	if handler, ok := r.core[action]; ok {
		return handler, true
	}
	handler, ok := r.extended[action]
	return handler, ok
}

// Register adds an extension handler and protects core handlers from override.
func (r *ActionRegistry) Register(action FrameAction, handler requestFrameHandler) error {
	if r == nil {
		return fmt.Errorf("action registry is nil")
	}
	if strings.TrimSpace(string(action)) == "" {
		return fmt.Errorf("action is empty")
	}
	if handler == nil {
		return fmt.Errorf("handler is nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.core[action]; exists {
		return fmt.Errorf("cannot override core handler: %s", action)
	}
	if _, exists := r.extended[action]; exists {
		return fmt.Errorf("action already registered: %s", action)
	}
	r.extended[action] = handler
	return nil
}

// MustRegister adds an extension handler and panics on registration failure.
func (r *ActionRegistry) MustRegister(action FrameAction, handler requestFrameHandler) {
	if err := r.Register(action, handler); err != nil {
		panic(err)
	}
}

// RegisterAction registers an extension handler on the global default registry.
func RegisterAction(action FrameAction, handler requestFrameHandler) error {
	return defaultRegistry.Register(action, handler)
}

// MustRegisterAction registers an extension handler and panics on failure.
func MustRegisterAction(action FrameAction, handler requestFrameHandler) {
	defaultRegistry.MustRegister(action, handler)
}
