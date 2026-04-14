package gateway

import (
	"encoding/json"
	"errors"
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
	case FrameActionPing:
		return nil
	case FrameActionRun:
		return validateRunFrame(frame)
	case FrameActionCompact, FrameActionLoadSession:
		if strings.TrimSpace(frame.SessionID) == "" {
			return NewMissingRequiredFieldError("session_id")
		}
	case FrameActionResolvePermission:
		return validateResolvePermissionFrame(frame)
	case FrameActionCancel, FrameActionListSessions:
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
	case FrameActionPing,
		FrameActionRun,
		FrameActionCompact,
		FrameActionCancel,
		FrameActionListSessions,
		FrameActionLoadSession,
		FrameActionResolvePermission:
		return true
	default:
		return false
	}
}
