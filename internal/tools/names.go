package tools

// Tool name constants are shared across tool implementations, context policies, and tests.
const (
	ToolNameBash                 = "bash"
	ToolNameWebFetch             = "webfetch"
	ToolNameFilesystemReadFile   = "filesystem_read_file"
	ToolNameFilesystemWriteFile  = "filesystem_write_file"
	ToolNameFilesystemGrep       = "filesystem_grep"
	ToolNameFilesystemGlob       = "filesystem_glob"
	ToolNameFilesystemEdit       = "filesystem_edit"
	ToolNameFilesystemMoveFile   = "filesystem_move_file"
	ToolNameFilesystemCopyFile   = "filesystem_copy_file"
	ToolNameFilesystemDeleteFile = "filesystem_delete_file"
	ToolNameFilesystemCreateDir  = "filesystem_create_dir"
	ToolNameFilesystemRemoveDir  = "filesystem_remove_dir"
	ToolNameTodoWrite            = "todo_write"
	ToolNameSpawnSubAgent        = "spawn_subagent"
	ToolNameMemoRemember         = "memo_remember"
	ToolNameMemoRecall           = "memo_recall"
	ToolNameMemoList             = "memo_list"
	ToolNameMemoRemove           = "memo_remove"
	ToolNameDiagnose             = "diagnose"
)
