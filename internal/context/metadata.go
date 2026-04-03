package context

// Metadata contains the non-message runtime state needed by context sources.
// Metadata 描述消息之外的运行时元信息，由 runtime 传入 context 模块。
type Metadata struct {
	// Workdir 是当前工作目录，通常用于规则发现和 git 状态采集。
	Workdir string
	// Shell 是当前会话使用的 shell 名称（如 bash/powershell）。
	Shell string
	// Provider 是当前选中的模型服务提供方标识。
	Provider string
	// Model 是当前实际使用的模型名称。
	Model string
}

// GitState is the summarized git metadata exposed to the prompt builder.
// GitState 是 git 状态的最小摘要，避免把冗余仓库信息注入提示词。
type GitState struct {
	// Available 表示当前目录可成功读取 git 元数据。
	Available bool
	// Branch 是当前分支名。
	Branch string
	// Dirty 表示工作区是否存在未提交改动。
	Dirty bool
}

// SystemState is the summarized runtime metadata exposed to the prompt builder.
// SystemState 是供提示词渲染使用的系统态快照。
type SystemState struct {
	Workdir  string
	Shell    string
	Provider string
	Model    string
	Git      GitState
}
