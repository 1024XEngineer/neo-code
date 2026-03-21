# NeoCode 安全拦截器 (Security Interceptor) 模块文档

## 1. 接口提供与调用规格说明

本模块为 Agent 工具执行层（Executor）提供统一的底层安全校验接口。所有具有潜在副作用的工具（如文件读写、终端执行、网络请求）在正式执行前，**必须**同步调用此接口获取权限批文。

### 1.1 核心状态枚举 (`Action`)

拦截器会返回以下三种明确的决策动作：

- `ActionDeny` ("deny"): **黑名单拦截**。底层必须拒绝执行，并向大模型返回权限受限的错误信息。
- `ActionAllow` ("allow"): **白名单放行**。底层可静默执行该操作，无需打断当前 Agent 工作流。
- `ActionAsk` ("ask"): **黄名单询问**。底层必须挂起当前执行流，通过 TUI 界面向用户弹窗请求授权（Y/N）。

### 1.2 对外暴露接口 (`Check`)

```Go
// Check 方法签发权限批文
func (sm *SecurityManager) Check(toolType string, target string) Action
```

**参数规范：**

- `toolType` (string): 必须为以下四种标准工具类型之一：
  - `"Read"`: 读取本地文件内容。
  - `"Write"`: 覆盖或修改本地文件。
  - `"Bash"`: 执行 Shell 终端命令。
  - `"WebFetch"`: 发起外部网络请求。
- `target` (string): 操作的目标载体。
  - 对于 Read/Write：传入相对或绝对文件路径（如 `src/main.go`）。
  - 对于 Bash：传入完整的命令字符串（如 `rm -rf /`）。
  - 对于 WebFetch：传入目标域名或 URL（如 `github.com`）。

**Agent 侧调用示例：**

```Go
// 假设已在系统启动时初始化了 securityManager
action := securityManager.Check("Bash", "rm -rf /")

switch action {
case security.ActionDeny:
    return "Error: 触发系统安全黑名单，请求被拦截。"
case security.ActionAsk:
    // 触发 TUI 弹窗询问逻辑
    if !tui.Confirm("AI 试图执行删库命令，是否允许？") {
        return "Error: 用户拒绝执行。"
    }
    fallthrough
case security.ActionAllow:
    // 执行实际的 bash 命令
    return executeCommand("rm -rf /")
}
```

------

## 2. 模块概述与目录情况

### 2.1 架构概述

本模块采用**“三态漏斗模型”**与**“职责分离”**的设计理念。通过解析外部的 YAML 配置文件，将安全策略映射至内存，实现高吞吐量的内存级正则校验。配置分为黑、白、黄三个独立名单，彻底解耦安全规则与业务逻辑。

### 2.2 目录结构

```Plaintext
NeoCode/
├── security/                   # [外部配置层] 供用户自定义的安全规则文件
│   ├── blacklist.yaml          # 绝对禁区规则
│   ├── whitelist.yaml          # 信任放行规则
│   └── yellowlist.yaml         # 需人工确认规则
├── internal/
│   ├── pkg/
│       ├── security/           # [核心逻辑层] 拦截器引擎大本营
│           ├── config.go       # YAML 解析与数据结构定义
│           ├── checker.go      # 漏斗判决引擎与双核匹配逻辑
│           ├── checker_test.go # 表格驱动的自动化单元测试
```

------

## 3. 测试用例与运行结果

本模块采用 Go 原生的表格驱动测试（Table-Driven Tests）保证引擎的可靠性，覆盖了正向匹配、越权阻断、降级兜底等核心场景。

### 3.1 核心测试数据 (Mock Data)

```Go
BlackList: { Target: ".git/**", R: "deny", W: "deny" }, { Command: "rm -rf *", X: "deny" }
WhiteList: { Target: "src/*.go", R: "allow" }, { Command: "go version", X: "allow" }
YellowList: { Target: "src/*.go", W: "ask" }, { Command: "go build *", X: "ask" }
```

### 3.2 自动化测试结果

在 `internal/pkg/security/` 目录下执行 `go test -v` 的运行报告：

```Plaintext
=== RUN   TestSecurityManager_Check
=== RUN   TestSecurityManager_Check/试图读取git源码 (Target: ".git/config")
=== RUN   TestSecurityManager_Check/试图执行删库跑路 (Command: "rm -rf /")
=== RUN   TestSecurityManager_Check/正常读取业务代码 (Target: "src/main.go")
=== RUN   TestSecurityManager_Check/执行安全的诊断命令 (Command: "go version")
=== RUN   TestSecurityManager_Check/试图修改业务代码 (Target: "src/main.go")
=== RUN   TestSecurityManager_Check/执行耗时的编译命令 (Command: "go build main.go")
=== RUN   TestSecurityManager_Check/未知的网络请求 (兜底策略测试)
=== RUN   TestSecurityManager_Check/未知的高危命令 (兜底策略测试)
--- PASS: TestSecurityManager_Check (0.00s)
    --- PASS: TestSecurityManager_Check/试图读取git源码 (0.00s)
    --- PASS: TestSecurityManager_Check/试图执行删库跑路 (0.00s)
    --- PASS: TestSecurityManager_Check/正常读取业务代码 (0.00s)
    --- PASS: TestSecurityManager_Check/执行安全的诊断命令 (0.00s)
    --- PASS: TestSecurityManager_Check/试图修改业务代码 (0.00s)
    --- PASS: TestSecurityManager_Check/执行耗时的编译命令 (0.00s)
    --- PASS: TestSecurityManager_Check/未知的网络请求 (0.00s)
    --- PASS: TestSecurityManager_Check/未知的高危命令 (0.00s)
PASS
ok      NeoCode/internal/pkg/security   0.003s
```

------

## 4. 具体实现逻辑

### 4.1 优先级漏斗判决 (Funnel Priority)

`Check` 方法严格按照以下优先级链向下路由：

1. **最高级 (Blacklist)**：一旦命中黑名单，强制返回 `deny`。
2. **第二级 (Whitelist)**：黑名单未命中，且命中白名单，返回 `allow`。
3. **第三级 (Yellowlist)**：前两者均未命中，且命中黄名单，返回 `ask`。
4. **默认兜底 (Fallback)**：所有名单均未命中时，采用**“默认拒绝，显式放行”**的零信任原则，强制降级返回 `ask`，交由人类决策。

### 4.2 避免规则短路 (Anti-Shadowing)

引擎在遍历规则列表时，不仅校验 `Target/Command` 是否正则匹配，还会校验该规则是否实际挂载了请求对应的权限动作（R/W/X/N）。若路径匹配但权限字段为空，引擎将跳过该规则继续向下遍历，防止高危请求意外穿透至下层名单。

### 4.3 双核匹配引擎 (Dual-Core Matcher)

为了解决路径操作与纯文本命令在通配符语义（Globbing Semantics）上的领域冲突，`isMatch` 内部采用了双引擎分流降级策略：

- **核心一：跨层级文件引擎 (doublestar)**
  - 针对 `Read` / `Write` 工具。
  - 引入 `github.com/bmatcuk/doublestar` 处理文件系统语义，严格遵循 `/` 为目录分隔符的规则，完美支持 `**` 跨级目录匹配。
- **核心二：纯文本正则引擎 (regexp)**
  - 针对 `Bash` / `WebFetch` 工具。
  - 由于 Shell 命令中 `/` 不具备目录边界意义（如 `rm -rf /`），工具将其视为纯文本。
  - 通过 `regexp.QuoteMeta` 自动转义输入中的特殊符号防止正则注入，随后将用户配置的 `*` 和 `**` 安全地替换为正则的 `.*`，实现无视 `/` 的万能通配拦截。