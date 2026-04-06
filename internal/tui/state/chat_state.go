package state

import "time"

// ActivityEntry 表示 Activity 面板中的单条事件记录。
type ActivityEntry struct {
	Time    time.Time
	Kind    string
	Title   string
	Detail  string
	IsError bool
}

// CommandMenuMeta 表示命令建议菜单的标题等元信息。
type CommandMenuMeta struct {
	Title string
}
