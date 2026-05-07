---
title: 安装与首次运行
description: 从安装到第一条对话的完整路径，3 分钟跑起来。
---

# 安装与首次运行

## 1. 环境要求

- 操作系统：macOS、Linux 或 Windows
- 终端环境：系统默认终端、PowerShell 或其他兼容终端
- 至少一个可用的 API Key（OpenAI、Gemini、OpenLL、Qiniu 或 ModelScope）

## 2. 安装

### 一键安装（推荐）

macOS / Linux：

```bash
curl -fsSL https://raw.githubusercontent.com/1024XEngineer/neo-code/main/scripts/install.sh | bash
```

Windows PowerShell：

```powershell
irm https://raw.githubusercontent.com/1024XEngineer/neo-code/main/scripts/install.ps1 | iex
```

## 3. 设置 API Key

NeoCode 只从环境变量读取 API Key，不会写入配置文件。

macOS / Linux：

```bash
export OPENAI_API_KEY="your_key_here"
```

Windows PowerShell：

```powershell
$env:OPENAI_API_KEY = "your_key_here"
```

其他 Provider 的环境变量名：

| Provider | 环境变量 |
|---|---|
| OpenAI | `OPENAI_API_KEY` |
| Gemini | `GEMINI_API_KEY` |
| OpenLL | `AI_API_KEY` |
| Qiniu | `QINIU_API_KEY` |
| ModelScope | `MODELSCOPE_API_KEY` |

## 4. 启动

```bash
neocode
```

如果要直接打开指定项目：

```bash
neocode --workdir /path/to/your/project
```

启动后会进入终端界面，底部是输入框。直接输入自然语言即可开始对话；输入 `/` 可以打开本地控制命令建议。

除了终端界面，NeoCode 也提供浏览器 Web UI：

```bash
neocode web
```

标签发布版执行 `neocode web` 时，如果本地还没有 `web/dist`，会自动使用发布包内的 `web/` 源码执行 `npm install` 和 `npm run build`，然后启动 Web UI。该流程要求当前机器已安装 Node.js 和 npm。

## 5. 第一次对话

可以先让 NeoCode 读项目结构：

```text
请阅读当前项目目录结构，给出模块职责摘要，并指出我应该从哪些文件开始了解主流程。
```

再让它找测试入口：

```text
请根据刚才的项目结构，帮我找出测试入口和主要业务入口。
```

Agent 会自动调用文件读取和搜索工具。当它需要写入文件或执行命令时，界面会先弹出权限确认。

浏览器会自动打开 `http://127.0.0.1:8080`，功能与终端一致。详见 [Web UI 使用指南](./web-ui)。

## 6. 建议补一个 AGENTS.md

如果这是一个长期维护的项目，建议在仓库根目录放 `AGENTS.md`，告诉 NeoCode 项目规则：

```md
# Project Rules

- 修改 Go 代码后运行 `go test ./...`
- 中文文档继续使用中文
- 不要把 API Key 写入配置文件
```

详见 [AGENTS.md 项目规则](./agents-md)。

## 开发者：从源码构建

如果你要阅读代码、调试功能或参与开发，可以从源码构建运行。需要 Go 1.25+ 环境。

```bash
git clone https://github.com/1024XEngineer/neo-code.git
cd neo-code
go build ./...
go run ./cmd/neocode
```

如果你希望从源码仓库直接验证 Web UI，也可以运行 `go run ./cmd/neocode web`。当 `web/dist` 缺失时，命令会自动尝试构建前端；若构建机没有 Node.js/npm，会直接报出依赖缺失提示。

如果你只想稳定使用，优先使用一键安装方式。源码构建更适合阅读代码、调试功能或参与开发。

## 下一步

- 想用浏览器操作：[Web UI 使用指南](./web-ui)
- 想学会本地控制命令：[Slash 指令](./slash-commands)
- 想理解工作区和会话：[会话、上下文与工作区](./context-session-workspace)
- 想看更多任务写法：[使用示例](./examples)
- 安装有问题：[排障与常见问题](./troubleshooting)
