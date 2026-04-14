package gateway

// FrameType 表示网关协议帧类型。
type FrameType string

const (
	// FrameTypeRequest 表示客户端发往网关的请求帧。
	FrameTypeRequest FrameType = "request"
	// FrameTypeEvent 表示网关推送给客户端的事件帧。
	FrameTypeEvent FrameType = "event"
	// FrameTypeError 表示网关推送给客户端的错误帧。
	FrameTypeError FrameType = "error"
	// FrameTypeAck 表示网关对请求的接收确认帧。
	FrameTypeAck FrameType = "ack"
)

// FrameAction 表示请求动作类型。
type FrameAction string

const (
	// FrameActionPing 表示探活动作，用于验证网关可用性。
	FrameActionPing FrameAction = "ping"
	// FrameActionRun 表示发起一次运行。
	FrameActionRun FrameAction = "run"
	// FrameActionCompact 表示触发一次手动压缩。
	FrameActionCompact FrameAction = "compact"
	// FrameActionCancel 表示取消当前活跃运行。
	FrameActionCancel FrameAction = "cancel"
	// FrameActionListSessions 表示获取会话摘要列表。
	FrameActionListSessions FrameAction = "list_sessions"
	// FrameActionLoadSession 表示加载指定会话详情。
	FrameActionLoadSession FrameAction = "load_session"
	// FrameActionResolvePermission 表示提交一次权限审批决策。
	FrameActionResolvePermission FrameAction = "resolve_permission"
)

// InputPartType 表示多模态输入分片类型。
type InputPartType string

const (
	// InputPartTypeText 表示文本分片。
	InputPartTypeText InputPartType = "text"
	// InputPartTypeImage 表示图片分片。
	InputPartTypeImage InputPartType = "image"
)

// Media 表示非文本输入的媒体描述。
type Media struct {
	// URI 是媒体资源地址。
	URI string `json:"uri"`
	// MimeType 是媒体 MIME 类型。
	MimeType string `json:"mime_type"`
	// FileName 是媒体文件名。
	FileName string `json:"file_name,omitempty"`
}

// InputPart 表示网关协议中的多模态输入分片。
type InputPart struct {
	// Type 表示分片类型，如 text / image。
	Type InputPartType `json:"type"`
	// Text 是文本分片内容，仅 text 类型使用。
	Text string `json:"text,omitempty"`
	// Media 是非文本分片媒体信息，仅 image 等类型使用。
	Media *Media `json:"media,omitempty"`
}

// FrameError 表示协议帧中的错误信息。
type FrameError struct {
	// Code 是稳定错误码，供客户端做分支判断。
	Code string `json:"code"`
	// Message 是面向用户或日志的错误描述。
	Message string `json:"message"`
}

// MessageFrame 是网关与客户端之间的统一通信帧。
type MessageFrame struct {
	// Type 是帧类型。
	Type FrameType `json:"type"`
	// Action 是请求动作，非 request 帧可为空。
	Action FrameAction `json:"action,omitempty"`
	// RequestID 是客户端请求幂等标识。
	RequestID string `json:"request_id,omitempty"`
	// RunID 是运行标识。
	RunID string `json:"run_id,omitempty"`
	// SessionID 是会话标识。
	SessionID string `json:"session_id,omitempty"`
	// InputText 是文本输入内容。
	InputText string `json:"input_text,omitempty"`
	// InputParts 是多模态输入分片。
	InputParts []InputPart `json:"input_parts,omitempty"`
	// Workdir 是本次请求的工作目录覆盖值。
	Workdir string `json:"workdir,omitempty"`
	// Payload 是动作扩展负载或事件负载。
	Payload any `json:"payload,omitempty"`
	// Error 是错误帧负载。
	Error *FrameError `json:"error,omitempty"`
}
