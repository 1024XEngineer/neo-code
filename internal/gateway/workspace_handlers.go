package gateway

import (
	"context"
	"strings"

	"neo-code/internal/gateway/protocol"
)

func init() {
	MustRegisterAction(FrameActionWorkspaceList, handleWorkspaceListFrame)
	MustRegisterAction(FrameActionWorkspaceCreate, handleWorkspaceCreateFrame)
	MustRegisterAction(FrameActionWorkspaceSwitch, handleWorkspaceSwitchFrame)
	MustRegisterAction(FrameActionWorkspaceRename, handleWorkspaceRenameFrame)
	MustRegisterAction(FrameActionWorkspaceDelete, handleWorkspaceDeleteFrame)
}

func requireMultiWorkspaceRuntime(port RuntimePort) (*MultiWorkspaceRuntime, *FrameError) {
	mw, ok := port.(*MultiWorkspaceRuntime)
	if !ok {
		return nil, NewFrameError(ErrorCodeUnsupportedAction, "multi-workspace runtime is not available")
	}
	return mw, nil
}

func handleWorkspaceListFrame(ctx context.Context, frame MessageFrame, runtimePort RuntimePort) MessageFrame {
	mw, err := requireMultiWorkspaceRuntime(runtimePort)
	if err != nil {
		return errorFrame(frame, err)
	}

	records := mw.ListWorkspaces()
	payload := make([]map[string]any, 0, len(records))
	for _, r := range records {
		payload = append(payload, map[string]any{
			"hash":       r.Hash,
			"path":       r.Path,
			"name":       r.Name,
			"created_at": r.CreatedAt,
			"updated_at": r.UpdatedAt,
		})
	}

	return MessageFrame{
		Type:      FrameTypeAck,
		Action:    FrameActionWorkspaceList,
		RequestID: frame.RequestID,
		Payload:   map[string]any{"workspaces": payload},
	}
}

func handleWorkspaceCreateFrame(ctx context.Context, frame MessageFrame, runtimePort RuntimePort) MessageFrame {
	mw, mwErr := requireMultiWorkspaceRuntime(runtimePort)
	if mwErr != nil {
		return errorFrame(frame, mwErr)
	}

	params, err := decodeWorkspaceCreatePayload(frame.Payload)
	if err != nil {
		return errorFrame(frame, err)
	}

	record, createErr := mw.CreateWorkspace(params.Path, params.Name)
	if createErr != nil {
		return errorFrame(frame, NewFrameError(ErrorCodeInternalError, createErr.Error()))
	}

	return MessageFrame{
		Type:      FrameTypeAck,
		Action:    FrameActionWorkspaceCreate,
		RequestID: frame.RequestID,
		Payload: map[string]any{
			"workspace": map[string]any{
				"hash":       record.Hash,
				"path":       record.Path,
				"name":       record.Name,
				"created_at": record.CreatedAt,
				"updated_at": record.UpdatedAt,
			},
		},
	}
}

func handleWorkspaceSwitchFrame(ctx context.Context, frame MessageFrame, runtimePort RuntimePort) MessageFrame {
	mw, mwErr := requireMultiWorkspaceRuntime(runtimePort)
	if mwErr != nil {
		return errorFrame(frame, mwErr)
	}

	params, err := decodeWorkspaceSwitchPayload(frame.Payload)
	if err != nil {
		return errorFrame(frame, err)
	}

	if switchErr := mw.SwitchWorkspace(ctx, params.Hash); switchErr != nil {
		return errorFrame(frame, NewFrameError(ErrorCodeInternalError, switchErr.Error()))
	}

	// 更新连接级工作区状态
	if wsState, ok := ConnectionWorkspaceStateFromContext(ctx); ok {
		wsState.SetWorkspaceHash(params.Hash)
	}

	// 清除旧工作区的 session 绑定，避免 fallback 到旧 session
	if relay, ok := StreamRelayFromContext(ctx); ok {
		if connID, connOK := ConnectionIDFromContext(ctx); connOK {
			relay.ClearConnectionBindings(connID)
		}
	}

	return MessageFrame{
		Type:      FrameTypeAck,
		Action:    FrameActionWorkspaceSwitch,
		RequestID: frame.RequestID,
		Payload:   map[string]any{"workspace_hash": params.Hash},
	}
}

func handleWorkspaceRenameFrame(ctx context.Context, frame MessageFrame, runtimePort RuntimePort) MessageFrame {
	mw, mwErr := requireMultiWorkspaceRuntime(runtimePort)
	if mwErr != nil {
		return errorFrame(frame, mwErr)
	}

	params, err := decodeWorkspaceRenamePayload(frame.Payload)
	if err != nil {
		return errorFrame(frame, err)
	}

	if renameErr := mw.RenameWorkspace(params.Hash, params.Name); renameErr != nil {
		return errorFrame(frame, NewFrameError(ErrorCodeInternalError, renameErr.Error()))
	}

	return MessageFrame{
		Type:      FrameTypeAck,
		Action:    FrameActionWorkspaceRename,
		RequestID: frame.RequestID,
		Payload:   map[string]any{"hash": params.Hash, "name": params.Name},
	}
}

func handleWorkspaceDeleteFrame(ctx context.Context, frame MessageFrame, runtimePort RuntimePort) MessageFrame {
	mw, mwErr := requireMultiWorkspaceRuntime(runtimePort)
	if mwErr != nil {
		return errorFrame(frame, mwErr)
	}

	params, err := decodeWorkspaceDeletePayload(frame.Payload)
	if err != nil {
		return errorFrame(frame, err)
	}

	if deleteErr := mw.DeleteWorkspace(params.Hash, params.RemoveData); deleteErr != nil {
		return errorFrame(frame, NewFrameError(ErrorCodeInternalError, deleteErr.Error()))
	}

	return MessageFrame{
		Type:      FrameTypeAck,
		Action:    FrameActionWorkspaceDelete,
		RequestID: frame.RequestID,
		Payload:   map[string]any{"hash": params.Hash},
	}
}

// ---- payload decode ----

type workspaceCreateParams struct {
	Path string
	Name string
}

type workspaceSwitchParams struct {
	Hash string
}

type workspaceRenameParams struct {
	Hash string
	Name string
}

type workspaceDeleteParams struct {
	Hash       string
	RemoveData bool
}

func decodeWorkspaceCreatePayload(payload any) (workspaceCreateParams, *FrameError) {
	switch typed := payload.(type) {
	case protocol.CreateWorkspaceParams:
		return workspaceCreateParams{Path: strings.TrimSpace(typed.Path), Name: strings.TrimSpace(typed.Name)}, nil
	case *protocol.CreateWorkspaceParams:
		if typed == nil {
			return workspaceCreateParams{}, NewMissingRequiredFieldError("payload.path")
		}
		return workspaceCreateParams{Path: strings.TrimSpace(typed.Path), Name: strings.TrimSpace(typed.Name)}, nil
	case map[string]any:
		path := readStringValue(typed, "path")
		if path == "" {
			return workspaceCreateParams{}, NewMissingRequiredFieldError("payload.path")
		}
		return workspaceCreateParams{Path: path, Name: readStringValue(typed, "name")}, nil
	default:
		return workspaceCreateParams{}, NewFrameError(ErrorCodeInvalidFrame, "invalid workspace.create payload")
	}
}

func decodeWorkspaceSwitchPayload(payload any) (workspaceSwitchParams, *FrameError) {
	switch typed := payload.(type) {
	case protocol.SwitchWorkspaceParams:
		return workspaceSwitchParams{Hash: strings.TrimSpace(typed.WorkspaceHash)}, nil
	case *protocol.SwitchWorkspaceParams:
		if typed == nil {
			return workspaceSwitchParams{}, NewMissingRequiredFieldError("payload.workspace_hash")
		}
		return workspaceSwitchParams{Hash: strings.TrimSpace(typed.WorkspaceHash)}, nil
	case map[string]any:
		hash := readStringValue(typed, "workspace_hash")
		if hash == "" {
			return workspaceSwitchParams{}, NewMissingRequiredFieldError("payload.workspace_hash")
		}
		return workspaceSwitchParams{Hash: hash}, nil
	default:
		return workspaceSwitchParams{}, NewFrameError(ErrorCodeInvalidFrame, "invalid workspace.switch payload")
	}
}

func decodeWorkspaceRenamePayload(payload any) (workspaceRenameParams, *FrameError) {
	switch typed := payload.(type) {
	case protocol.RenameWorkspaceParams:
		return workspaceRenameParams{Hash: strings.TrimSpace(typed.WorkspaceHash), Name: strings.TrimSpace(typed.Name)}, nil
	case *protocol.RenameWorkspaceParams:
		if typed == nil {
			return workspaceRenameParams{}, NewMissingRequiredFieldError("payload.workspace_hash")
		}
		return workspaceRenameParams{Hash: strings.TrimSpace(typed.WorkspaceHash), Name: strings.TrimSpace(typed.Name)}, nil
	case map[string]any:
		hash := readStringValue(typed, "workspace_hash")
		name := readStringValue(typed, "name")
		if hash == "" {
			return workspaceRenameParams{}, NewMissingRequiredFieldError("payload.workspace_hash")
		}
		if name == "" {
			return workspaceRenameParams{}, NewMissingRequiredFieldError("payload.name")
		}
		return workspaceRenameParams{Hash: hash, Name: name}, nil
	default:
		return workspaceRenameParams{}, NewFrameError(ErrorCodeInvalidFrame, "invalid workspace.rename payload")
	}
}

func decodeWorkspaceDeletePayload(payload any) (workspaceDeleteParams, *FrameError) {
	switch typed := payload.(type) {
	case protocol.DeleteWorkspaceParams:
		return workspaceDeleteParams{Hash: strings.TrimSpace(typed.WorkspaceHash), RemoveData: typed.RemoveData}, nil
	case *protocol.DeleteWorkspaceParams:
		if typed == nil {
			return workspaceDeleteParams{}, NewMissingRequiredFieldError("payload.workspace_hash")
		}
		return workspaceDeleteParams{Hash: strings.TrimSpace(typed.WorkspaceHash), RemoveData: typed.RemoveData}, nil
	case map[string]any:
		hash := readStringValue(typed, "workspace_hash")
		if hash == "" {
			return workspaceDeleteParams{}, NewMissingRequiredFieldError("payload.workspace_hash")
		}
		removeData := false
		if v, ok := typed["remove_data"].(bool); ok {
			removeData = v
		}
		return workspaceDeleteParams{Hash: hash, RemoveData: removeData}, nil
	default:
		return workspaceDeleteParams{}, NewFrameError(ErrorCodeInvalidFrame, "invalid workspace.delete payload")
	}
}

