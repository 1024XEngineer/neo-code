# NeoCode

使用 `.env` 配置聊天模型、向量模型和本地记忆存储。

1. 复制 `.env.example` 为 `.env`
2. 按需填写 `AI_PROVIDER`、`AI_API_KEY`、`AI_BASE_URL`、`AI_MODEL`
3. 按需填写 `EMBEDDING_PROVIDER`、`EMBEDDING_API_KEY`、`EMBEDDING_BASE_URL`、`EMBEDDING_MODEL`
4. 可选配置 `MEMORY_FILE_PATH`、`MEMORY_TOP_K`、`MEMORY_MIN_SCORE`、`MEMORY_MAX_ITEMS`、`SHORT_TERM_HISTORY_TURNS`、`PERSONA_FILE_PATH`
5. 运行程序，系统会把记忆保存到本地 JSON 文件，并在后续对话中自动召回相似上下文
6. 支持 `/memory` 查看长期记忆状态，`/clear-memory` 清空长期记忆，`/clear-context` 清空当前短期上下文
7. 如果配置了 `PERSONA_FILE_PATH`，程序启动时会读取对应人设文件，并作为 `system` 提示词注入每次会话

示例人设文件可参考 `persona.txt.example`
