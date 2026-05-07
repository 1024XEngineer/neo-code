---
title: Web UI 使用指南
description: 通过浏览器使用 NeoCode 的完整功能，包括对话、会话管理、Provider 配置等。
---

# Web UI 使用指南

NeoCode 除了终端 TUI，还提供完整的浏览器 Web UI。两者共享同一个 Gateway 后端，功能一致。

## 启动

```bash
neocode web
```

启动后浏览器会自动打开 `http://127.0.0.1:8080`。如果端口被占用，会自动尝试 8081 ~ 8090。
如果当前目录或发布包内存在 `web/` 源码但还没有 `web/dist`，命令会自动执行 `npm install` 和 `npm run build`。标签发布版使用该能力时，用户机器必须预先安装 Node.js 和 npm。

### 常用参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `--http-listen` | `127.0.0.1:8080` | 监听地址（仅允许回环地址） |
| `--open-browser` | `true` | 启动后自动打开浏览器 |
| `--skip-build` | `false` | 跳过前端构建（dist/ 缺失时会报错；仅在你已准备好预构建资源时使用） |
| `--static-dir` | — | 指定前端静态文件目录 |
| `--log-level` | `info` | 日志级别：debug / info / warn / error |
| `--token-file` | — | 自定义认证 token 文件路径 |

示例：指定端口并跳过自动构建：

```bash
neocode web --http-listen 127.0.0.1:9090 --skip-build
```

## 访问方式

启动后浏览器自动打开的 URL 已包含认证 token（`?token=xxx`），无需手动输入。

如果需要在其他浏览器或设备上手动访问：

1. 从 `~/.neocode/auth.json` 读取 token
2. 打开 `http://127.0.0.1:8080/?token=<你的token>`

::: warning
Web UI 默认只监听 `127.0.0.1`（回环地址），仅本机可访问。不支持绑定到非回环地址。
:::

## 两种运行模式

Web UI 支持两种运行模式，根据启动方式自动选择：

### Electron 桌面应用

以 Electron 打包运行时，Gateway 地址和 token 通过 preload API 自动注入，无需手动配置。支持自动更新：新版本下载后会提示重启安装。

### 浏览器模式

通过 `neocode web` 或直接在浏览器中访问 Gateway 地址时使用。首次连接需要输入 Gateway URL 和 token，配置保存在 sessionStorage 中。
如果你使用标签发布版，首次运行 `neocode web` 可能先看到前端依赖安装与构建日志；构建完成后会继续启动 Web UI。若机器缺少 Node.js/npm，命令会直接提示安装依赖。

## 核心功能

### 对话界面

- 发送自然语言消息，Agent 流式响应并实时渲染
- 支持 Markdown 渲染和代码高亮
- 工具调用过程可视化（状态卡片展示执行进度）
- 可随时取消正在执行的任务（停止按钮）

### Build / Plan 模式

通过输入框旁的按钮切换：

- **Build**（默认）：Agent 直接执行任务，读写文件、运行命令
- **Plan**：Agent 只分析和规划，不执行修改

### 斜杠命令

在输入框中输入 `/` 打开命令菜单，支持键盘上下选择：

| 命令 | 说明 |
|------|------|
| `/help` | 查看所有可用命令 |
| `/compact` | 压缩当前会话上下文 |
| `/memo` | 查看持久化记忆索引 |
| `/remember <内容>` | 保存一条持久化记忆 |
| `/forget <关键词>` | 按关键词删除记忆 |
| `/skills` | 打开 Skill 选择器 |
| `/<skill-id>` | 切换指定 Skill 的启用状态 |

### 会话管理

- 侧边栏按时间分组显示会话（今天、昨天、近 7 天、更早）
- 支持新建（`Ctrl+N`）、切换、重命名、删除会话
- 按标题搜索会话

### 工作区管理

- 每个工作区对应一个文件系统目录
- 支持创建、切换、重命名、删除工作区
- Electron 模式下创建时可使用目录选择器

### Provider / Model 配置

- 侧边栏中打开 Provider 配置面板
- 切换当前活跃的 Provider 和 Model
- 添加自定义 Provider（支持 OpenAI Compatible、Gemini、Anthropic 驱动）
- 每个会话可单独设置不同的 Model

### MCP Server 配置

- 侧边栏中打开 MCP 配置面板
- 添加、编辑、删除 MCP Server
- 支持配置命令、参数、工作目录、环境变量
- 可单独启用或禁用每个 MCP Server

### Skill 管理

- 侧边栏中浏览所有可用 Skill
- 按会话粒度激活 / 停用 Skill
- 也可通过斜杠命令快速切换

### 权限确认

当 Agent 需要执行特权操作时，界面会弹出权限确认卡片，显示工具名称、操作目标和原因。三个选项：

| 选项 | 说明 |
|------|------|
| Reject | 拒绝本次请求 |
| Allow Once | 仅允许本次 |
| Allow for This Session | 本会话内同类请求全部放行 |

### 文件变更面板

实时追踪 Agent 对文件的修改，显示新增 / 修改 / 删除的文件及 diff 统计。文件写入时自动打开。

### 文件树面板

浏览工作区目录结构，通过顶部按钮切换显示。

### Checkpoint 回退

工具调用关联 Checkpoint，支持恢复到任意 Checkpoint 状态，也可撤销恢复操作。

### 状态栏

底部状态栏显示：

- 连接状态（已连接 / 连接中 / 错误）
- Token 用量统计
- Budget 指示器
- 当前工作目录（Electron 模式，点击可切换）
- 主题切换（浅色 / 深色）

## 认证机制

- 首次运行时自动生成 32 字节随机 token，存储在 `~/.neocode/auth.json`（权限 0600）
- WebSocket 连接通过 `?token=` 查询参数或 `Authorization: Bearer` 头认证
- 未认证的 WebSocket 连接有 3 秒宽限期，超时自动断开
