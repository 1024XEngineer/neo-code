package askuser

// Request 表示一次 ask_user 的完整请求负载。
type Request struct {
	RequestID   string   `json:"request_id,omitempty"`
	QuestionID  string   `json:"question_id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Kind        string   `json:"kind"`
	Options     []Option `json:"options,omitempty"`
	Required    bool     `json:"required"`
	AllowSkip   bool     `json:"allow_skip"`
	MaxChoices  int      `json:"max_choices,omitempty"`
	TimeoutSec  int      `json:"timeout_sec,omitempty"`
}

// Option 表示 ask_user 选项。
type Option struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// Result 表示 ask_user 的响应结果。
type Result struct {
	Status     string   `json:"status"`
	QuestionID string   `json:"question_id,omitempty"`
	Values     []string `json:"values,omitempty"`
	Message    string   `json:"message,omitempty"`
}

// ResolveInput 表示一次来自网关的 ask_user 回答。
type ResolveInput struct {
	SubjectID string   `json:"subject_id,omitempty"`
	RequestID string   `json:"request_id"`
	Status    string   `json:"status"`
	Values    []string `json:"values,omitempty"`
	Message   string   `json:"message,omitempty"`
}

const (
	StatusAnswered = "answered"
	StatusSkipped  = "skipped"
	StatusTimeout  = "timeout"
)
