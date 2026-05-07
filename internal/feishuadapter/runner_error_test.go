package feishuadapter

import "testing"

func TestTranslateRunnerError(t *testing.T) {
	cases := map[string]string{
		"runner_offline":                "本机 Runner 未连接，请在电脑上启动 `neocode runner`",
		"capability_denied":             "权限不足：当前能力令牌不允许此操作",
		"tool_execution_failed: failed": "工具执行失败：tool_execution_failed: failed",
		"timed out waiting for runner":  "本机 Runner 响应超时，请检查网络连接和 Runner 状态",
		"other":                         "",
	}
	for input, want := range cases {
		if got := translateRunnerError(input); got != want {
			t.Fatalf("translateRunnerError(%q) = %q, want %q", input, got, want)
		}
	}
}
