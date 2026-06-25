package keymap

import "testing"

func TestMatchInputKey(t *testing.T) {
	tests := []struct {
		key    string
		action Action
	}{
		{"enter", ActionSend},
		{"shift+enter", ActionNewline},
		{"esc", ActionEscape},
		{"ctrl+c", ActionCtrlC},
		{"ctrl+p", ActionOpenPalette},
		{"ctrl+l", ActionLogViewer},
		// ctrl+d 不在 MatchInputKey 映射（由 app 层按输入框空否决定）。
		{"ctrl+d", ActionNone},
		{"a", ActionNone},
		{"j", ActionNone},
	}
	for _, tt := range tests {
		got := MatchInputKey(tt.key)
		if got != tt.action {
			t.Errorf("MatchInputKey(%q) = %v, want %v", tt.key, got, tt.action)
		}
	}
}

func TestMatchNormalKey(t *testing.T) {
	tests := []struct {
		key    string
		action Action
	}{
		{"i", ActionEnterInput},
		{"j", ActionScrollDown},
		{"k", ActionScrollUp},
		{"ctrl+d", ActionHalfPageDown},
		{"ctrl+u", ActionHalfPageUp},
		{"ctrl+f", ActionFullPageDown},
		{"ctrl+b", ActionFullPageUp},
		{"g", ActionScrollTop},
		{"G", ActionScrollBottom},
		{"/", ActionSearchForward},
		{"n", ActionSearchNext},
		{"N", ActionSearchPrev},
		{":", ActionExCommand},
		{"q", ActionQuit},
		{" ", ActionLeader},
		{"ctrl+c", ActionCtrlC},
		{"enter", ActionNone},
		{"a", ActionNone},
	}
	for _, tt := range tests {
		got := MatchNormalKey(tt.key)
		if got != tt.action {
			t.Errorf("MatchNormalKey(%q) = %v, want %v", tt.key, got, tt.action)
		}
	}
}

func TestMatchLeaderKey(t *testing.T) {
	tests := []struct {
		key    string
		action Action
	}{
		{"p", ActionLeaderPalette},
		{"n", ActionLeaderNewSession},
		{"s", ActionLeaderSwitchSession},
		{"h", ActionLeaderHelp},
		{"m", ActionLeaderModelPicker},
		{"f", ActionLeaderFullAccess},
		{"l", ActionLeaderLog},
		{"c", ActionLeaderCancelRun},
		{"r", ActionLeaderRetry},
		{" ", ActionLeaderLastSession},
		{"space", ActionLeaderLastSession},
		{"q", ActionLeaderQuit},
		{"a", ActionNone},
		{"j", ActionNone},
	}
	for _, tt := range tests {
		got := MatchLeaderKey(tt.key)
		if got != tt.action {
			t.Errorf("MatchLeaderKey(%q) = %v, want %v", tt.key, got, tt.action)
		}
	}
}

func TestIsLeaderSuffix(t *testing.T) {
	if !IsLeaderSuffix("p") {
		t.Error("IsLeaderSuffix(\"p\") = false, want true")
	}
	if !IsLeaderSuffix("q") {
		t.Error("IsLeaderSuffix(\"q\") = false, want true")
	}
	if !IsLeaderSuffix("r") {
		t.Error("IsLeaderSuffix(\"r\") = false, want true")
	}
	if !IsLeaderSuffix(" ") {
		t.Error("IsLeaderSuffix(\" \") = false, want true")
	}
	if IsLeaderSuffix("a") {
		t.Error("IsLeaderSuffix(\"a\") = true, want false")
	}
}

func TestFullHelpContainsAllGroups(t *testing.T) {
	groups := FullHelp()
	if len(groups) < 4 {
		t.Errorf("FullHelp() returned %d groups, want at least 4", len(groups))
	}
	titles := make(map[string]bool)
	for _, g := range groups {
		titles[g.Title] = true
	}
	for _, want := range []string{"Input Mode", "Normal Mode (Esc)", "Leader (Space)", "Navigation"} {
		if !titles[want] {
			t.Errorf("FullHelp() missing group %q", want)
		}
	}
}

// TestHelpEntriesConsistent 校验帮助文案不出现与规划冲突的描述（如 g g 双键）。
func TestHelpEntriesConsistent(t *testing.T) {
	for _, group := range FullHelp() {
		for _, entry := range group.Entries {
			// 不应出现 "g g" 双键描述（已改为 g 单键）。
			if entry.Key == "g g" {
				t.Errorf("help entry still references \"g g\": %+v", entry)
			}
		}
	}
}
