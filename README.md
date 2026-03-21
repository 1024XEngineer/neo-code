# NeoCode

一个基于 Go 的终端对话工具，当前使用根目录 `config.yaml` 作为唯一业务配置文件，并使用结构化 code memory 做记忆召回。

现在支持：

1. TUI 对话与流式输出
2. `/switch` 切换聊天模型
3. 结构化长期记忆检索与写回
4. session memory 与短期上下文保留
5. 人设文件注入
6. 启动时校验环境变量 API Key
7. `/memory`、`/clear-memory confirm`、`/clear-context`

## 配置方式

只需要维护根目录下的 `config.yaml`，并在系统环境变量中设置 `AI_API_KEY`。

- 可以先参考 `config.example.yaml`
- 首次启动时如果 `config.yaml` 不存在，程序会自动创建默认配置
- API Key 配置方法见下方 `API Key 配置`
- 如果 Key 校验失败，请更新环境变量后重新启动
- 如果网络异常导致无法确认 Key 是否有效，可使用 `/retry`、`/continue`、`/models`、`/switch <model>` 或 `/exit`

## API Key 配置

程序现在只从系统环境变量 `AI_API_KEY` 读取 API Key，不再从 `config.yaml` 读取真实密钥。

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

示例：

```yaml
app:
  name: "NeoCode"
  version: "1.0.0"

ai:
  provider: "modelscope"
  api_key: ""
  model: "Qwen/Qwen3-Coder-480B-A35B-Instruct"

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

models:
  chat:
    default_model: "Qwen/Qwen3-Coder-480B-A35B-Instruct"
    models:
      - name: "Qwen/Qwen3-Coder-480B-A35B-Instruct"
        url: "https://api-inference.modelscope.cn/v1/chat/completions"
```

说明：

- `ai.api_key`：已废弃，占位保留，不再读取真实 Key
- `memory.storage_path`：长期结构化记忆文件
- `memory.persist_types`：允许持久化的结构化记忆类型
- `memory.min_match_score`：最低召回分数
- `memory.max_prompt_chars`：记忆注入 prompt 的总字符上限
- `history.short_term_turns`：保留最近多少轮上下文
- `persona.file_path`：启动时加载的人设文件
- `models.chat.models`：聊天模型与接口地址映射

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

- `/models`：查看支持的模型
- `/switch <model>`：切换当前聊天模型
- `/memory`：查看长期记忆和 session memory 状态，以及各类型统计
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

- API Key 必须放在系统环境变量 `AI_API_KEY` 中，不再写入 `config.yaml`
- `config.yaml` 已在 `.gitignore` 中忽略，不应提交真实密钥
- `data/` 已在 `.gitignore` 中忽略，本地记忆不会默认入库
- `.env` 不再是主配置来源，如保留仅用于个人兼容场景

## 测试

- 运行测试：`go test ./...`
- 代码格式化：`go fmt ./...`
