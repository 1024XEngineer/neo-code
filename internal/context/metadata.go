package context

// Metadata 描述构建上下文时需要注入的非消息元信息。
type Metadata struct {
	Workdir  string
	Shell    string
	Provider string
	Model    string
}

// GitState 是注入到 prompt 的 Git 摘要信息。
type GitState struct {
	Available bool
	Branch    string
	Dirty     bool
}

// SystemState 是注入到 prompt 的运行时环境摘要。
type SystemState struct {
	Workdir  string
	Shell    string
	Provider string
	Model    string
	Git      GitState
}
