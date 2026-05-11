---
title: 配置指南
description: 配好 API Key、模型、Shell、工作区、自定义 Provider 和 MCP 工具。
---

# 配置指南

NeoCode 的配置目标很简单：把常用选择保存下来，把密钥留在环境变量里。第一次使用只需要配置 Provider、模型和 Shell；其他能力按需打开。

配置文件位置：

```text
~/.neocode/config.yaml
```

## 最小配置

```yaml
selected_provider: openai
current_model: gpt-5.4
shell: bash
```

Windows 用户通常使用：

```yaml
shell: powershell
```

## API Key

NeoCode 只从环境变量读取 API Key，不会把明文密钥写进配置文件。

| Provider | 环境变量 |
|---|---|
| OpenAI | `OPENAI_API_KEY` |
| Gemini | `GEMINI_API_KEY` |
| OpenLL | `AI_API_KEY` |
| Qiniu | `QINIU_API_KEY` |
| ModelScope | `MODELSCOPE_API_KEY` |

macOS / Linux：

```bash
export OPENAI_API_KEY="your_key_here"
```

Windows PowerShell：

```powershell
$env:OPENAI_API_KEY = "your_key_here"
```

如果想长期保存环境变量，请用你所在系统或 Shell 的标准方式保存，不要把真实 Key 写进 `config.yaml`。

### ModelScope API Key 获取

ModelScope（魔搭）通过 TUI 半引导流程完成 API Key 配置：

1. 在 TUI 中执行 `/provider`，选择 `modelscope`
2. 按 Enter 依次打开引导页 → 登录页，完成魔搭社区登录
3. 进入 Token 页（<https://www.modelscope.cn/my/access/token>），创建并复制 API Key
4. 回到 TUI，在引导面板中粘贴 token，按 Enter 提交校验
5. **绑定阿里云账号**：访问[账号设置](https://www.modelscope.cn/my/settings/account)完成阿里云账号绑定。此步骤为必须，未绑定时 API 调用将返回认证失败

如果 token 校验时检测到认证或权限错误，TUI 会自动打开账号绑定页引导你完成。

## 切换 Provider 和模型

推荐在 NeoCode 界面里切换，选择会自动保存：

```text
/provider
/model
```

也可以直接修改配置：

```yaml
selected_provider: gemini
current_model: gemini-2.5-pro
```

如果模型列表为空，优先检查当前 Provider 对应的 API Key 是否已在启动 NeoCode 的同一个终端里设置。

## 工作区

工作区建议通过启动参数指定，不写进主配置：

```bash
neocode -w /path/to/project
```

## Shell 和工具超时

Shell 决定 Agent 执行命令时使用的环境：

```yaml
shell: powershell    # Windows
shell: bash          # macOS / Linux
```

如果你的项目测试或构建经常比较慢，可以调高工具执行超时：

```yaml
tool_timeout_sec: 30
```

## 自定义 Provider

如果你的模型服务不在内置列表里，可以添加 OpenAI 兼容 Provider。

更推荐先在界面里交互添加：

```text
/provider add
```

也可以创建配置文件：

```text
~/.neocode/providers/company/provider.yaml
```

示例：

```yaml
name: company
driver: openaicompat
api_key_env: COMPANY_API_KEY
model_source: discover
base_url: https://llm.example.com/v1
chat_api_mode: chat_completions
chat_endpoint_path: /chat/completions
discovery_endpoint_path: /models
```

如果服务不支持自动获取模型列表，改用手动列表：

```yaml
name: company
driver: openaicompat
api_key_env: COMPANY_API_KEY
model_source: manual
base_url: https://llm.example.com/v1
chat_endpoint_path: /chat/completions
models:
  - id: company-coder
    name: Company Coder
    context_window: 128000
```

自定义 Provider 里同样只写环境变量名，真实 Key 放在系统环境变量 `COMPANY_API_KEY` 中。

## MCP 工具

如果你有外部工具想让 Agent 调用，例如文档搜索、Issue 查询或内部平台操作，可以通过 MCP 接入。

最小示例：

```yaml
tools:
  mcp:
    servers:
      - id: docs
        enabled: true
        source: stdio
        stdio:
          command: node
          args:
            - ./mcp-server.js
          workdir: ./mcp
        env:
          - name: MCP_TOKEN
            value_env: MCP_TOKEN
```

配置完成后，启动 NeoCode 并询问：

```text
请列出你当前可用的工具。
```

更完整的 MCP 配置、工具暴露和排障步骤见 [MCP 工具接入](./mcp)。

## 常见问题

### API Key 未设置

看到类似下面的错误：

```text
environment variable OPENAI_API_KEY is empty
```

说明当前 Provider 对应的环境变量没有在当前终端会话里生效。设置环境变量后，重新启动 NeoCode。

### 配置文件报未知字段

NeoCode 会严格检查 `config.yaml`。如果你从旧版本文档复制过配置，先保留这些常用字段：`selected_provider`、`current_model`、`shell`、`tool_timeout_sec`、`tools`。

其他不确定的字段建议先删掉，再用 `/provider`、`/model` 或 `/provider add` 重新配置。

## 下一步

- 想看日常命令：[日常使用](./daily-use)
- 想了解权限确认：[工具与权限](./tools-permissions)
- 想接入外部工具：[MCP 工具接入](./mcp)
- 遇到报错：[排障与常见问题](./troubleshooting)
