package subagent

import "strings"

// TaskType 描述子代理任务输出契约类型。
type TaskType string

const (
	// TaskTypeReview 表示只读审查任务，输出 report/findings。
	TaskTypeReview TaskType = "review"
	// TaskTypeEdit 表示编辑任务，输出 summary/findings/patches。
	TaskTypeEdit TaskType = "edit"
	// TaskTypeVerify 表示验证任务，输出 status/logs/findings。
	TaskTypeVerify TaskType = "verify"
)

// Valid 判断 task_type 是否受支持。
func (t TaskType) Valid() bool {
	switch TaskType(strings.ToLower(strings.TrimSpace(string(t)))) {
	case TaskTypeReview, TaskTypeEdit, TaskTypeVerify:
		return true
	default:
		return false
	}
}

// RequiredSectionsForTaskType 返回 task_type 对应的最小输出契约字段集合。
func RequiredSectionsForTaskType(taskType TaskType) []string {
	switch TaskType(strings.ToLower(strings.TrimSpace(string(taskType)))) {
	case TaskTypeReview:
		return []string{"report", "findings"}
	case TaskTypeVerify:
		return []string{"status", "logs", "findings"}
	case TaskTypeEdit:
		return []string{"summary", "findings", "patches"}
	default:
		return []string{"summary", "findings", "patches"}
	}
}
