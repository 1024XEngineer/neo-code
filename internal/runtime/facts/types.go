package facts

// RuntimeFacts 描述运行期可验证的统一事实快照。
type RuntimeFacts struct {
	Todos        TodoFacts         `json:"todos"`
	Files        FileFacts         `json:"files"`
	Commands     CommandFacts      `json:"commands"`
	SubAgents    SubAgentFacts     `json:"subagents"`
	Verification VerificationFacts `json:"verification"`
	Errors       ErrorFacts        `json:"errors"`
	Progress     ProgressFacts     `json:"progress"`
}

// TodoFacts 描述 todo 领域事实。
type TodoFacts struct {
	CreatedIDs             []string `json:"created_ids,omitempty"`
	UpdatedIDs             []string `json:"updated_ids,omitempty"`
	CompletedIDs           []string `json:"completed_ids,omitempty"`
	FailedIDs              []string `json:"failed_ids,omitempty"`
	ConflictIDs            []string `json:"conflict_ids,omitempty"`
	OpenRequiredCount      int      `json:"open_required_count,omitempty"`
	CompletedRequiredCount int      `json:"completed_required_count,omitempty"`
	FailedRequiredCount    int      `json:"failed_required_count,omitempty"`
}

// FileFacts 描述文件相关事实。
type FileFacts struct {
	Written      []FileWriteFact        `json:"written,omitempty"`
	Exists       []FileExistFact        `json:"exists,omitempty"`
	ContentMatch []FileContentMatchFact `json:"content_match,omitempty"`
}

// FileWriteFact 描述一次写入事实。
type FileWriteFact struct {
	Path            string `json:"path"`
	Bytes           int    `json:"bytes"`
	WorkspaceWrite  bool   `json:"workspace_write"`
	ExpectedContent string `json:"expected_content,omitempty"`
}

// FileExistFact 描述一次文件存在性事实。
type FileExistFact struct {
	Path   string `json:"path"`
	Source string `json:"source,omitempty"`
}

// FileContentMatchFact 描述一次内容匹配事实。
type FileContentMatchFact struct {
	Path               string   `json:"path"`
	Scope              string   `json:"scope,omitempty"`
	ExpectedContains   []string `json:"expected_contains,omitempty"`
	VerificationPassed bool     `json:"verification_passed"`
}

// CommandFacts 描述命令执行事实。
type CommandFacts struct {
	Executed []CommandFact `json:"executed,omitempty"`
}

// CommandFact 描述单次命令执行事实。
type CommandFact struct {
	Tool      string `json:"tool"`
	Command   string `json:"command,omitempty"`
	ExitCode  int    `json:"exit_code"`
	Succeeded bool   `json:"succeeded"`
}

// SubAgentFacts 描述子代理生命周期事实。
type SubAgentFacts struct {
	Started   []SubAgentFact `json:"started,omitempty"`
	Completed []SubAgentFact `json:"completed,omitempty"`
	Failed    []SubAgentFact `json:"failed,omitempty"`
}

// SubAgentFact 描述单个子代理任务事实。
type SubAgentFact struct {
	TaskID     string   `json:"task_id"`
	Role       string   `json:"role,omitempty"`
	State      string   `json:"state,omitempty"`
	StopReason string   `json:"stop_reason,omitempty"`
	Summary    string   `json:"summary,omitempty"`
	Artifacts  []string `json:"artifacts,omitempty"`
}

// VerificationFacts 描述验证事实。
type VerificationFacts struct {
	Performed []VerificationFact `json:"performed,omitempty"`
	Passed    []VerificationFact `json:"passed,omitempty"`
	Failed    []VerificationFact `json:"failed,omitempty"`
}

// ErrorFacts 描述运行期工具错误事实，供终态决策识别不可恢复失败。
type ErrorFacts struct {
	ToolErrors []ToolErrorFact `json:"tool_errors,omitempty"`
}

// ToolErrorFact 描述单次工具错误事实。
type ToolErrorFact struct {
	Tool       string `json:"tool,omitempty"`
	ErrorClass string `json:"error_class,omitempty"`
	Content    string `json:"content,omitempty"`
}

// VerificationFact 描述一次验证尝试事实。
type VerificationFact struct {
	Tool   string `json:"tool,omitempty"`
	Scope  string `json:"scope,omitempty"`
	Reason string `json:"reason,omitempty"`
	Passed bool   `json:"passed"`
}

// ProgressFacts 描述事实层的推进度。
type ProgressFacts struct {
	ObservedFactCount int `json:"observed_fact_count"`
}
