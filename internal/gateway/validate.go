package gateway

import (
	"encoding/json"
	"errors"
	"neo-code/internal/config"
	"neo-code/internal/gateway/protocol"
	providertypes "neo-code/internal/provider/types"
	"strings"
)

// ValidateFrame 校验网关协议帧是否满足基础契约约束。
func ValidateFrame(frame MessageFrame) *FrameError {
	if !isValidFrameType(frame.Type) {
		return NewFrameError(ErrorCodeInvalidFrame, "invalid frame type")
	}

	if strings.TrimSpace(string(frame.Action)) != "" && !isValidFrameAction(frame.Action) {
		return NewFrameError(ErrorCodeInvalidAction, "invalid action")
	}

	if frame.Type == FrameTypeRequest {
		return validateRequestFrame(frame)
	}

	return nil
}

// validateRequestFrame 校验 request 帧的动作及动作所需字段。
func validateRequestFrame(frame MessageFrame) *FrameError {
	if strings.TrimSpace(string(frame.Action)) == "" {
		return NewMissingRequiredFieldError("action")
	}

	switch frame.Action {
	case FrameActionAuthenticate,
		FrameActionBindStream,
		FrameActionWakeOpenURL,
		FrameActionExecuteSystemTool,
		FrameActionActivateSessionSkill,
		FrameActionDeactivateSessionSkill,
		FrameActionRenameSession,
		FrameActionSetSessionModel,
		FrameActionCreateCustomProvider,
		FrameActionDeleteCustomProvider,
		FrameActionSelectProviderModel,
		FrameActionUpsertMCPServer,
		FrameActionSetMCPServerEnabled,
		FrameActionDeleteMCPServer:
		if frame.Payload == nil {
			return NewMissingRequiredFieldError("payload")
		}
		return nil
	case FrameActionPing,
		FrameActionCancel,
		FrameActionListSessions,
		FrameActionCreateSession,
		FrameActionListAvailableSkills,
		FrameActionListModels,
		FrameActionListProviders,
		FrameActionListMCPServers:
		return nil
	case FrameActionRun:
		return validateRunFrame(frame)
	case FrameActionCompact,
		FrameActionLoadSession,
		FrameActionListSessionSkills,
		FrameActionListSessionTodos,
		FrameActionGetRuntimeSnapshot,
		FrameActionDeleteSession,
		FrameActionGetSessionModel,
		FrameActionListCheckpoints,
		FrameActionUndoRestore:
		if strings.TrimSpace(frame.SessionID) == "" {
			return NewMissingRequiredFieldError("session_id")
		}
	case FrameActionListFiles:
		// listFiles 不强制 session_id，workdir 可独立使用
	case FrameActionWorkspaceList:
		return nil
	case FrameActionWorkspaceCreate,
		FrameActionWorkspaceSwitch,
		FrameActionWorkspaceRename,
		FrameActionWorkspaceDelete:
		if frame.Payload == nil {
			return NewMissingRequiredFieldError("payload")
		}
		return nil
	case FrameActionResolvePermission:
		return validateResolvePermissionFrame(frame)
	case FrameActionRestoreCheckpoint,
		FrameActionCheckpointDiff:
		if frame.Payload == nil {
			return NewMissingRequiredFieldError("payload")
		}
		if strings.TrimSpace(frame.SessionID) == "" {
			return NewMissingRequiredFieldError("session_id")
		}
		return nil
	default:
		return NewFrameError(ErrorCodeInvalidAction, "invalid action")
	}

	if len(frame.InputParts) > 0 {
		return validateInputParts(frame.InputParts)
	}

	return nil
}

// validateRunFrame 校验 run 动作的输入字段是否完整且合法。
func validateRunFrame(frame MessageFrame) *FrameError {
	hasText := strings.TrimSpace(frame.InputText) != ""
	hasParts := len(frame.InputParts) > 0
	if !hasText && !hasParts {
		return NewMissingRequiredFieldError("input_text_or_input_parts")
	}

	if hasParts {
		return validateInputParts(frame.InputParts)
	}

	return nil
}

// validateResolvePermissionFrame 校验 resolve_permission 动作所需字段。
func validateResolvePermissionFrame(frame MessageFrame) *FrameError {
	if frame.Payload == nil {
		return NewMissingRequiredFieldError("payload")
	}

	input, err := decodePermissionResolutionInput(frame.Payload)
	if err != nil {
		return NewFrameError(ErrorCodeInvalidAction, "invalid resolve_permission payload")
	}
	if strings.TrimSpace(input.RequestID) == "" {
		return NewMissingRequiredFieldError("payload.request_id")
	}
	if !isValidPermissionResolutionDecision(input.Decision) {
		return NewFrameError(ErrorCodeInvalidAction, "invalid resolve_permission decision")
	}

	return nil
}

// decodePermissionResolutionInput 将 payload 解析为权限审批决策输入。
func decodePermissionResolutionInput(payload any) (PermissionResolutionInput, error) {
	if direct, ok := payload.(PermissionResolutionInput); ok {
		return direct, nil
	}
	if ptr, ok := payload.(*PermissionResolutionInput); ok {
		if ptr == nil {
			return PermissionResolutionInput{}, errors.New("permission payload is nil")
		}
		return *ptr, nil
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return PermissionResolutionInput{}, err
	}

	var input PermissionResolutionInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return PermissionResolutionInput{}, err
	}
	return input, nil
}

// decodeRenameSessionPayload 解析 renameSession 的负载参数。
func decodeRenameSessionPayload(payload any) (renameSessionParams, *FrameError) {
	switch typed := payload.(type) {
	case protocol.RenameSessionParams:
		return renameSessionParams{SessionID: strings.TrimSpace(typed.SessionID), Title: strings.TrimSpace(typed.Title)}, nil
	case *protocol.RenameSessionParams:
		if typed == nil {
			return renameSessionParams{}, NewMissingRequiredFieldError("payload")
		}
		return renameSessionParams{SessionID: strings.TrimSpace(typed.SessionID), Title: strings.TrimSpace(typed.Title)}, nil
	case map[string]any:
		sessionID := readStringValue(typed, "session_id")
		title := readStringValue(typed, "title")
		return renameSessionParams{SessionID: sessionID, Title: title}, nil
	default:
		raw, marshalErr := json.Marshal(payload)
		if marshalErr != nil {
			return renameSessionParams{}, NewFrameError(ErrorCodeInvalidFrame, "invalid rename_session payload")
		}
		var decoded protocol.RenameSessionParams
		if unmarshalErr := json.Unmarshal(raw, &decoded); unmarshalErr != nil {
			return renameSessionParams{}, NewFrameError(ErrorCodeInvalidFrame, "invalid rename_session payload")
		}
		return renameSessionParams{SessionID: strings.TrimSpace(decoded.SessionID), Title: strings.TrimSpace(decoded.Title)}, nil
	}
}

// decodeListFilesPayload 解析 listFiles 的负载参数。
func decodeListFilesPayload(payload any) (listFilesParams, *FrameError) {
	switch typed := payload.(type) {
	case protocol.ListFilesParams:
		return listFilesParams{SessionID: strings.TrimSpace(typed.SessionID), Workdir: strings.TrimSpace(typed.Workdir), Path: strings.TrimSpace(typed.Path)}, nil
	case *protocol.ListFilesParams:
		if typed == nil {
			return listFilesParams{}, nil
		}
		return listFilesParams{SessionID: strings.TrimSpace(typed.SessionID), Workdir: strings.TrimSpace(typed.Workdir), Path: strings.TrimSpace(typed.Path)}, nil
	case map[string]any:
		sessionID := readStringValue(typed, "session_id")
		workdir := readStringValue(typed, "workdir")
		path := readStringValue(typed, "path")
		return listFilesParams{SessionID: sessionID, Workdir: workdir, Path: path}, nil
	default:
		raw, marshalErr := json.Marshal(payload)
		if marshalErr != nil {
			return listFilesParams{}, NewFrameError(ErrorCodeInvalidFrame, "invalid list_files payload")
		}
		var decoded protocol.ListFilesParams
		if unmarshalErr := json.Unmarshal(raw, &decoded); unmarshalErr != nil {
			return listFilesParams{}, NewFrameError(ErrorCodeInvalidFrame, "invalid list_files payload")
		}
		return listFilesParams{SessionID: strings.TrimSpace(decoded.SessionID), Workdir: strings.TrimSpace(decoded.Workdir), Path: strings.TrimSpace(decoded.Path)}, nil
	}
}

// decodeSetSessionModelPayload 解析 setSessionModel 的负载参数。
func decodeSetSessionModelPayload(payload any) (setSessionModelParams, *FrameError) {
	switch typed := payload.(type) {
	case protocol.SetSessionModelParams:
		return setSessionModelParams{SessionID: strings.TrimSpace(typed.SessionID), ProviderID: strings.TrimSpace(typed.ProviderID), ModelID: strings.TrimSpace(typed.ModelID)}, nil
	case *protocol.SetSessionModelParams:
		if typed == nil {
			return setSessionModelParams{}, NewMissingRequiredFieldError("payload")
		}
		return setSessionModelParams{SessionID: strings.TrimSpace(typed.SessionID), ProviderID: strings.TrimSpace(typed.ProviderID), ModelID: strings.TrimSpace(typed.ModelID)}, nil
	case map[string]any:
		sessionID := readStringValue(typed, "session_id")
		providerID := readStringValue(typed, "provider_id")
		modelID := readStringValue(typed, "model_id")
		return setSessionModelParams{SessionID: sessionID, ProviderID: providerID, ModelID: modelID}, nil
	default:
		raw, marshalErr := json.Marshal(payload)
		if marshalErr != nil {
			return setSessionModelParams{}, NewFrameError(ErrorCodeInvalidFrame, "invalid set_session_model payload")
		}
		var decoded protocol.SetSessionModelParams
		if unmarshalErr := json.Unmarshal(raw, &decoded); unmarshalErr != nil {
			return setSessionModelParams{}, NewFrameError(ErrorCodeInvalidFrame, "invalid set_session_model payload")
		}
		return setSessionModelParams{SessionID: strings.TrimSpace(decoded.SessionID), ProviderID: strings.TrimSpace(decoded.ProviderID), ModelID: strings.TrimSpace(decoded.ModelID)}, nil
	}
}

// decodeCreateProviderPayload 解析 createCustomProvider 的负载参数。
func decodeCreateProviderPayload(payload any) (CreateProviderInput, *FrameError) {
	var params protocol.CreateCustomProviderParams
	if err := decodePayload(payload, &params); err != nil {
		return CreateProviderInput{}, NewFrameError(ErrorCodeInvalidFrame, "invalid create_custom_provider payload")
	}
	return CreateProviderInput{
		Name:                  strings.TrimSpace(params.Name),
		Driver:                strings.TrimSpace(params.Driver),
		BaseURL:               strings.TrimSpace(params.BaseURL),
		ChatAPIMode:           strings.TrimSpace(params.ChatAPIMode),
		ChatEndpointPath:      strings.TrimSpace(params.ChatEndpointPath),
		APIKeyEnv:             strings.TrimSpace(params.APIKeyEnv),
		APIKey:                strings.TrimSpace(params.APIKey),
		ModelSource:           strings.TrimSpace(params.ModelSource),
		DiscoveryEndpointPath: strings.TrimSpace(params.DiscoveryEndpointPath),
		Models:                convertProtocolModelDescriptors(params.Models),
	}, nil
}

// decodeDeleteProviderPayload 解析 deleteCustomProvider 的负载参数。
func decodeDeleteProviderPayload(payload any) (DeleteProviderInput, *FrameError) {
	var params protocol.DeleteCustomProviderParams
	if err := decodePayload(payload, &params); err != nil {
		return DeleteProviderInput{}, NewFrameError(ErrorCodeInvalidFrame, "invalid delete_custom_provider payload")
	}
	return DeleteProviderInput{ProviderID: strings.TrimSpace(params.ProviderID)}, nil
}

// decodeSelectProviderModelPayload 解析 selectProviderModel 的负载参数。
func decodeSelectProviderModelPayload(payload any) (SelectProviderModelInput, *FrameError) {
	var params protocol.SelectProviderModelParams
	if err := decodePayload(payload, &params); err != nil {
		return SelectProviderModelInput{}, NewFrameError(ErrorCodeInvalidFrame, "invalid select_provider_model payload")
	}
	return SelectProviderModelInput{
		ProviderID: strings.TrimSpace(params.ProviderID),
		ModelID:    strings.TrimSpace(params.ModelID),
	}, nil
}

// decodeUpsertMCPServerPayload 解析 upsertMCPServer 的负载参数。
func decodeUpsertMCPServerPayload(payload any) (UpsertMCPServerInput, *FrameError) {
	var params protocol.UpsertMCPServerParams
	if err := decodePayload(payload, &params); err != nil {
		return UpsertMCPServerInput{}, NewFrameError(ErrorCodeInvalidFrame, "invalid upsert_mcp_server payload")
	}
	return UpsertMCPServerInput{Server: convertProtocolMCPServer(params.Server)}, nil
}

// decodeSetMCPServerEnabledPayload 解析 setMCPServerEnabled 的负载参数。
func decodeSetMCPServerEnabledPayload(payload any) (SetMCPServerEnabledInput, *FrameError) {
	var params protocol.SetMCPServerEnabledParams
	if err := decodePayload(payload, &params); err != nil {
		return SetMCPServerEnabledInput{}, NewFrameError(ErrorCodeInvalidFrame, "invalid set_mcp_server_enabled payload")
	}
	return SetMCPServerEnabledInput{ID: strings.TrimSpace(params.ID), Enabled: params.Enabled}, nil
}

// decodeDeleteMCPServerPayload 解析 deleteMCPServer 的负载参数。
func decodeDeleteMCPServerPayload(payload any) (DeleteMCPServerInput, *FrameError) {
	var params protocol.DeleteMCPServerParams
	if err := decodePayload(payload, &params); err != nil {
		return DeleteMCPServerInput{}, NewFrameError(ErrorCodeInvalidFrame, "invalid delete_mcp_server payload")
	}
	return DeleteMCPServerInput{ID: strings.TrimSpace(params.ID)}, nil
}

// decodePayload 通过 JSON 编解码把 map/struct 负载收敛到目标类型。
func decodePayload(payload any, target any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, target)
}

// convertProtocolModelDescriptors 将协议层模型描述转换为 provider 通用模型描述。
func convertProtocolModelDescriptors(models []protocol.ProviderModelDescriptor) []providertypes.ModelDescriptor {
	if len(models) == 0 {
		return nil
	}
	converted := make([]providertypes.ModelDescriptor, 0, len(models))
	for _, model := range models {
		converted = append(converted, providertypes.ModelDescriptor{
			ID:              strings.TrimSpace(model.ID),
			Name:            strings.TrimSpace(model.Name),
			Description:     strings.TrimSpace(model.Description),
			ContextWindow:   model.ContextWindow,
			MaxOutputTokens: model.MaxOutputTokens,
			CapabilityHints: providertypes.ModelCapabilityHints{
				ToolCalling: providertypes.ModelCapabilityState(strings.TrimSpace(model.CapabilityHints.ToolCalling)),
				ImageInput:  providertypes.ModelCapabilityState(strings.TrimSpace(model.CapabilityHints.ImageInput)),
			},
		})
	}
	return converted
}

// convertProtocolMCPServer 将协议层 MCP 配置转换为 config 层结构。
func convertProtocolMCPServer(server protocol.MCPServerParams) config.MCPServerConfig {
	env := make([]config.MCPEnvVarConfig, 0, len(server.Env))
	for _, item := range server.Env {
		env = append(env, config.MCPEnvVarConfig{
			Name:     strings.TrimSpace(item.Name),
			Value:    strings.TrimSpace(item.Value),
			ValueEnv: strings.TrimSpace(item.ValueEnv),
		})
	}
	return config.MCPServerConfig{
		ID:      strings.TrimSpace(server.ID),
		Enabled: server.Enabled,
		Source:  strings.TrimSpace(server.Source),
		Version: strings.TrimSpace(server.Version),
		Stdio: config.MCPStdioConfig{
			Command:           strings.TrimSpace(server.Stdio.Command),
			Args:              append([]string(nil), server.Stdio.Args...),
			Workdir:           strings.TrimSpace(server.Stdio.Workdir),
			StartTimeoutSec:   server.Stdio.StartTimeoutSec,
			CallTimeoutSec:    server.Stdio.CallTimeoutSec,
			RestartBackoffSec: server.Stdio.RestartBackoffSec,
		},
		Env: env,
	}
}

// isValidPermissionResolutionDecision 判断审批决策是否属于受支持集合。
func isValidPermissionResolutionDecision(decision PermissionResolutionDecision) bool {
	switch decision {
	case PermissionResolutionAllowOnce, PermissionResolutionAllowSession, PermissionResolutionReject:
		return true
	default:
		return false
	}
}

// validateInputParts 校验多模态输入分片数组。
func validateInputParts(parts []InputPart) *FrameError {
	for index := range parts {
		if err := validateInputPart(parts[index], index); err != nil {
			return err
		}
	}
	return nil
}

// validateInputPart 校验单个多模态输入分片。
func validateInputPart(part InputPart, index int) *FrameError {
	switch part.Type {
	case InputPartTypeText:
		if strings.TrimSpace(part.Text) == "" {
			return NewFrameError(ErrorCodeInvalidMultimodalPayload, "input_parts[text] requires non-empty text")
		}
	case InputPartTypeImage:
		if part.Media == nil {
			return NewFrameError(ErrorCodeInvalidMultimodalPayload, "input_parts[image] requires media")
		}
		if strings.TrimSpace(part.Media.URI) == "" {
			return NewFrameError(ErrorCodeInvalidMultimodalPayload, "input_parts[image] requires media.uri")
		}
		if strings.TrimSpace(part.Media.MimeType) == "" {
			return NewFrameError(ErrorCodeInvalidMultimodalPayload, "input_parts[image] requires media.mime_type")
		}
	default:
		_ = index
		return NewFrameError(ErrorCodeInvalidMultimodalPayload, "input_parts contains unsupported type")
	}

	return nil
}

// isValidFrameType 判断帧类型是否属于协议定义集合。
func isValidFrameType(frameType FrameType) bool {
	switch frameType {
	case FrameTypeRequest, FrameTypeEvent, FrameTypeError, FrameTypeAck:
		return true
	default:
		return false
	}
}

// isValidFrameAction 判断动作是否属于协议定义集合。
func isValidFrameAction(action FrameAction) bool {
	switch action {
	case FrameActionAuthenticate,
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
		FrameActionCreateSession,
		FrameActionLoadSession,
		FrameActionListSessionTodos,
		FrameActionGetRuntimeSnapshot,
		FrameActionResolvePermission,
		FrameActionDeleteSession,
		FrameActionRenameSession,
		FrameActionListFiles,
		FrameActionListModels,
		FrameActionSetSessionModel,
		FrameActionGetSessionModel,
		FrameActionListProviders,
		FrameActionCreateCustomProvider,
		FrameActionDeleteCustomProvider,
		FrameActionSelectProviderModel,
		FrameActionListMCPServers,
		FrameActionUpsertMCPServer,
		FrameActionSetMCPServerEnabled,
		FrameActionDeleteMCPServer,
		FrameActionListCheckpoints,
		FrameActionRestoreCheckpoint,
		FrameActionUndoRestore,
		FrameActionCheckpointDiff,
		FrameActionWorkspaceList,
		FrameActionWorkspaceCreate,
		FrameActionWorkspaceSwitch,
		FrameActionWorkspaceRename,
		FrameActionWorkspaceDelete:
		return true
	default:
		return false
	}
}
