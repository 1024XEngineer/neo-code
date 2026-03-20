package security

import (
	"testing"
)

// 测试函数必须以 Test 开头，并且接收一个指向 testing.T 的指针
func TestSecurityManager_Check(t *testing.T) {
	// 1. 准备测试数据 (Mock Data)
	// 我们不需要真正去读硬盘上的 YAML 文件，直接在内存里“捏造”一个装满规则的安检门
	sm := &SecurityManager{
		BlackList: Config{
			Rules: []Rule{
				{Target: ".git/**", R: "deny", W: "deny"},
				{Command: "rm -rf *", X: "deny"},
			},
		},
		WhiteList: Config{
			Rules: []Rule{
				{Target: "src/**/*.go", R: "allow"},
				{Command: "go version", X: "allow"},
			},
		},
		YellowList: Config{
			Rules: []Rule{
				{Target: "src/**/*.go", W: "ask"},
				{Command: "go build *", X: "ask"},
			},
		},
	}

	// 2. 表格驱动测试 (Table-Driven Tests) - Go 语言极其经典的测试套路！
	// 我们定义一个匿名结构体的切片，把所有的测试用例像表格一样列出来
	tests := []struct {
		name     string // 测试用例的名字
		toolType string // 模拟 AI 调用的工具
		target   string // 模拟 AI 想要操作的目标路径或命令
		want     Action // 我们期望拦截器返回的结果
	}{
		// 🔴 黑名单拦截测试
		{"试图读取git源码", "Read", ".git/config", ActionDeny},
		{"试图执行删库跑路", "Bash", "rm -rf /", ActionDeny},

		// 🟢 白名单放行测试
		{"正常读取业务代码", "Read", "src/main.go", ActionAllow},
		{"执行安全的诊断命令", "Bash", "go version", ActionAllow},

		// 🟡 黄名单询问测试
		{"试图修改业务代码", "Write", "src/main.go", ActionAsk},
		{"执行耗时的编译命令", "Bash", "go build main.go", ActionAsk},

		// 🛡️ 兜底策略测试 (配置文件里没写的，统统算黄名单)
		{"未知的网络请求", "WebFetch", "api.unknown.com", ActionAsk},
		{"未知的高危命令", "Bash", "format c:", ActionAsk},
	}

	// 3. 循环遍历“表格”，逐一执行测试
	for _, tt := range tests {
		// t.Run 会启动一个子测试，这样如果报错了，能精准告诉你是哪个用例挂了
		t.Run(tt.name, func(t *testing.T) {
			// 实际调用我们刚才写的 Check 引擎
			got := sm.Check(tt.toolType, tt.target)

			// 对比实际结果 (got) 和 期望结果 (want)
			if got != tt.want {
				// t.Errorf 就相当于 C++ 里的 EXPECT_EQ 失败，并且支持类似 printf 的格式化输出
				t.Errorf("Check(工具: %q, 目标: %q) = %v, 但期望的结果是 %v", tt.toolType, tt.target, got, tt.want)
			}
		})
	}
}
