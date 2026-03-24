# NeoCode

一个基于 Go 的终端对话工具，当前使用根目录 `config.yaml` 作为唯一业务配置文件，并使用结构化 code memory 做记忆召回。

现在支持：

1. TUI 对话与流式输出
2. `/switch` 切换聊天模型
3. `/provider` 切换模型提供商
4. 结构化长期记忆检索与写回
5. session memory 与短期上下文保留
6. 人设文件注入
7. 启动时校验环境变量 API Key
8. `/memory`、`/clear-memory confirm`、`/clear-context`
9. 工作区目录感知（`--workspace` / `NEOCODE_WORKSPACE` / 当前目录）

## 文档导航

如果你想快速理解项目设计、接口约定、安全机制或团队协作方式，可以优先阅读以下文档：

- [整体架构设计说明](./docs/structure.md)：快速了解项目分层思路、目录结构和模块职责。
- [架构详细说明文档](./docs/detailed_architecture_guide.md)：深入理解系统目标、核心架构、数据流和后续演进方向。
- [API 契约书与开发指南](./docs/API_Contract_Guide.md)：了解 `api/proto/chat.proto` 的契约设计和前后端协作边界。
- [GitHub Actions + Codecov 使用指南](./docs/github-actions-codecov-guide.md)：面向新成员的 CI、测试覆盖率与 PR 检查上手文档。
- [安全拦截器模块文档](./docs/Security_Interceptor.md)：说明工具执行前的安全校验模型与接口规范。
- [安全拦截器测试手册](./docs/Security_Interceptor_Test_Manual.md)：整理安全拦截相关测试点与手工验证步骤。
- [Repository Guidelines](./docs/AGENTS.md)：补充仓库协作约定、目录组织与开发命令说明。

## 配置方式

只需要维护根目录下的 `config.yaml`，并在系统环境变量中设置 API Key。

- 可以先参考 `config.example.yaml`
- 首次启动时如果 `config.yaml` 不存在，程序会自动创建默认配置
- API Key 配置方法见下方 `API Key 配置`
- `ai.api_key` 填写的是环境变量名；留空时会回退到 `AI_API_KEY`
- 如果 Key 校验失败，可使用 `/apikey <env_name>` 或 `/provider <name>` 调整配置
- 如果网络异常导致无法确认 Key 是否有效，可使用 `/retry`、`/continue`、`/apikey <env_name>`、`/provider <name>`、`/switch <model>` 或 `/exit`
- 支持的 provider：`modelscope`、`deepseek`、`openll`、`siliconflow`、`豆包大模型`、`openai`
- 切换 provider 时会自动切到该厂商默认模型，也可以继续使用 `/switch <model>` 自定义

## API Key 配置

程序会从 `config.yaml` 的 `ai.api_key` 指定的系统环境变量中读取真实 API Key；如果该字段为空，则默认读取 `AI_API_KEY`。`config.yaml` 不存储真实密钥。

例如：

```yaml
ai:
  provider: "openll"
  api_key: "MY_TEAM_API_KEY"
  model: "gpt-5.4"
```

这表示程序会读取系统环境变量 `MY_TEAM_API_KEY`。

Windows 永久生效：

```powershell
setx AI_API_KEY "your-api-key"
```

设置后请关闭并重新打开终端，再验证是否生效：

```powershell
echo $env:AI_API_KEY
```

Windows 当前终端临时生效：

```powershell
$env:AI_API_KEY="your-api-key"
```

CMD 当前终端临时生效：

```cmd
set AI_API_KEY=your-api-key
```

配置完成后启动项目：

```bash
go run ./cmd/tui
```

也可以显式指定工作区目录（工具读写和工作记忆都会基于该目录）：

```bash
go run ./cmd/tui --workspace ./
```

或者通过环境变量指定：

```bash
set NEOCODE_WORKSPACE=F:\\Qiniu\\test1
go run ./cmd/tui
```

工作区解析优先级：`--workspace` > `NEOCODE_WORKSPACE` > 启动时当前目录。

示例：

```yaml
app:
  name: "NeoCode"
  version: "1.0.0"

ai:
  provider: "openll"
  api_key: "AI_API_KEY"
  model: "gpt-5.4"

memory:
  top_k: 5
  min_match_score: 2.2
  max_prompt_chars: 1800
  max_items: 1000
  storage_path: "./data/memory_rules.json"
  persist_types:
    - "user_preference"
    - "project_rule"
    - "code_fact"
    - "fix_recipe"

history:
  short_term_turns: 6

persona:
  file_path: "./persona.txt"
```

说明：

- `ai.api_key`：API Key 对应的环境变量名；为空时回退到 `AI_API_KEY`
- `ai.provider`：当前模型提供商，支持 `modelscope`、`deepseek`、`openll`、`siliconflow`、`豆包大模型`、`openai`
- `ai.model`：当前 provider 使用的模型名；执行 `/provider <name>` 时会自动切到该厂商默认模型，也可通过 `/switch <model>` 覆盖
- `memory.storage_path`：长期结构化记忆文件
- `memory.persist_types`：允许持久化的结构化记忆类型
- `memory.min_match_score`：最低召回分数
- `memory.max_prompt_chars`：记忆注入 prompt 的总字符上限
- `history.short_term_turns`：保留最近多少轮上下文
- `persona.file_path`：启动时加载的人设文件

## Memory 设计

当前 memory 使用纯结构化规则召回，不使用 embedding 或向量相似度。系统会将长期结构化记忆写入 `memory.storage_path`，并在当前进程内维护 session memory。主要包括：

- `user_preference`：用户长期偏好
- `project_rule`：项目级约定、目录结构、常用命令、配置规则
- `code_fact`：明确的代码事实、文件职责、模块位置
- `fix_recipe`：排障经验、常见报错与修复方式
- `session_memory`：当前会话里仍有价值的临时 coding 信息

召回顺序会优先考虑长期记忆中的用户偏好、项目规则、代码事实、修复经验，再补充 session memory。

## 运行

```bash
go run ./cmd/tui
```

如果只想验证服务组装：

```bash
go run ./cmd/server
```

## 可用命令

- `/provider <name>`：切换当前模型提供商
- `/apikey <env_name>`：切换当前读取的 API Key 环境变量名并立即校验
- `/switch <model>`：切换当前聊天模型
- `/memory`：查看长期记忆和 session memory 状态，以及各类型统计
- `/pwd` 或 `/workspace`：查看当前工作区目录
- `/clear-memory confirm`：确认后清空长期结构化记忆
- `/clear-context`：清空当前短期上下文和 session memory
- `/help`：查看帮助
- `/exit`：退出程序

## 相关文件

- `config.yaml`：主配置文件
- `config.example.yaml`：配置模板
- `data/memory_rules.json`：长期结构化记忆文件
- `persona.txt`：人设内容

## 安全与本地文件

- `config.yaml` 中的 `ai.api_key` 仅保存环境变量名，不写入真实密钥
- `ai.api_key` 为空时默认读取系统环境变量 `AI_API_KEY`
- `config.yaml` 已在 `.gitignore` 中忽略，不应提交真实密钥
- `data/` 已在 `.gitignore` 中忽略，本地记忆不会默认入库
- `.env` 不再是主配置来源，如保留仅用于个人兼容场景

## 测试

- 运行测试：`go test ./...`
- 代码格式化：`go fmt ./...`
