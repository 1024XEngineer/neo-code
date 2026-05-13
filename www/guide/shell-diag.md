---
title: 终端代理与诊断
description: 使用 neocode shell 进入代理终端，通过 neocode diag 触发或自动诊断终端错误。
---

# 终端代理与诊断

NeoCode 提供终端代理与诊断功能，让 Agent 能够实时感知你在终端中的操作，并在出现错误时自动或手动触发诊断。

使用流程为：先通过 `neocode shell` 启动代理终端会话，然后在代理终端中正常工作。当遇到错误时，可以用 `neocode diag` 触发诊断，或开启自动诊断让 Agent 主动分析错误。

## 启动代理 Shell

在终端中启动代理 Shell 会话：

```bash
neocode shell
```

启动后，你会进入一个由 NeoCode 代理的终端环境。该会话会向 NeoCode Gateway 注册为 shell 角色，从而使后续的诊断命令能够找到目标会话。

> **注意**：所有诊断命令（`neocode diag` 及其子命令）都依赖当前已有一个活跃的 `neocode shell` 会话。请先在一个终端窗口启动 `neocode shell`，然后在另一个终端窗口执行诊断命令。

## Shell Integration

如果你使用的 Shell 支持 integration，可以通过 `--init` 输出初始化脚本：

```bash
# 输出 bash 初始化脚本
neocode shell --init bash

# 输出 zsh 初始化脚本
neocode shell --init zsh
```

你也可以将输出直接写入 shell 配置文件，使每次启动代理 Shell 时自动加载：

```bash
neocode shell --init bash >> ~/.bashrc
```

## 触发一次诊断

当终端中出现报错时，在另一个终端窗口执行诊断命令：

```bash
# 触发一次手动诊断（两种写法等价）
neocode diag
neocode diag diagnose
```

你也可以通过管道直接将错误日志传入诊断：

```bash
cat error.log | neocode diag --session <session-id>
```

或使用 `--error-log` 参数直接指定错误内容：

```bash
neocode diag --error-log "command not found: xxx"
```

诊断结果会以流式输出显示在终端中。

## 交互式诊断沙盒（IDM）

交互式诊断模式提供一个沙盒环境，你可以在其中与 Agent 进行多轮对话来定位问题：

```bash
neocode diag -i
```

在 IDM 中，Agent 可以读取你的终端上下文，并在对话中给出诊断建议。退出 IDM 的方式：

- 输入 `exit` 退出
- 在空闲状态下按 `Ctrl+C` 退出

## 自动诊断开关

你可以设置在终端中出现错误时，Agent 是否自动触发诊断：

| 命令 | 作用 |
|------|------|
| `neocode diag auto on` | 开启自动诊断 |
| `neocode diag auto off` | 关闭自动诊断 |
| `neocode diag auto status` | 查询当前自动诊断状态 |

```bash
# 开启自动诊断
neocode diag auto on

# 查询状态
neocode diag auto status
```

如果知道目标 Shell 会话 ID，可以通过 `--session` 参数指定：

```bash
neocode diag auto on --session <session-id>
```

## 常见问题

### 运行 `neocode diag` 时报错 "no neocode shell session found"

**现象**

- 执行 `neocode diag` 或 `neocode diag -i` 时提示找不到 shell 会话

**可能原因**

- 尚未启动 `neocode shell`
- 代理 Shell 会话已退出
- `NEOCODE_SHELL_SESSION` 环境变量在另一个终端中不可见

**怎么处理**

1. 在一个终端窗口中先执行 `neocode shell`，进入代理终端。
2. 在另一个终端窗口中执行诊断命令。
3. 确保代理 Shell 会话仍在运行，没有退出。

### `neocode shell` 无法在当前平台启动

**现象**

- 提示平台不支持或启动失败

**可能原因**

- 当前平台不支持 PTY 代理（`neocode shell` 当前仅支持 Unix-like 系统）

**怎么处理**

1. 确认当前操作系统：`uname -s`。
2. 如果你在 Windows 上，当前版本暂不支持 `neocode shell`，Windows 支持计划在未来版本中提供。

### 诊断无响应或超时

**现象**

- `neocode diag` 执行后长时间无输出

**可能原因**

- Gateway 未运行或无法连接
- 网络问题导致 RPC 调用超时

**怎么处理**

1. 确认 `neocode shell` 会话仍在运行。
2. 检查 Gateway 连接是否正常。
3. 重试命令。

## 下一步

- 了解 NeoCode 的日常使用方式：[日常使用](./daily-use)
- 遇到其他问题：[排障与常见问题](./troubleshooting)
- 查看配置选项：[配置指南](./configuration)
