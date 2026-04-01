package context

// Metadata contains the non-message runtime state needed by context sources.
type Metadata struct {
	Workdir  string
	Shell    string
	Provider string
	Model    string
}

// GitState is the summarized git metadata exposed to the prompt builder.
type GitState struct {
	Available bool
	Branch    string
	Dirty     bool
}

// SystemState is the summarized runtime metadata exposed to the prompt builder.
type SystemState struct {
	Workdir  string
	Shell    string
	Provider string
	Model    string
	Git      GitState
}
