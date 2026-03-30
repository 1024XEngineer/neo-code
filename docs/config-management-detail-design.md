# 配置管理模块详细设计
## 模块职责
`config` 模块主要负责四类事情：
- 加载和保存 YAML 配置文件
- 从环境变量解析真实密钥
- 管理 NeoCode 托管目录中的 `.env`
- 向运行中的系统提供并发安全的配置读写能力

## 核心类型
- `Config`：顶层应用配置，运行时包含 Provider 列表、当前选中的 Provider、当前模型、全局 `api_key_env_override`、工作目录、Shell 和循环限制等信息
- `ProviderConfig`：单个 Provider 的内建定义，包括 Base URL、默认模型、实例级模型列表和 API Key 环境变量名
- `Manager`：使用 `sync.RWMutex` 保护的配置访问器与修改器
- `Loader`：对 YAML 文件和托管 `.env` 文件的文件系统封装

## 环境变量策略
- YAML 不保存 Provider 元数据，但允许保存顶层 `api_key_env_override`，与 `selected_provider`、`current_model` 和通用运行配置一起持久化
- `Loader.LoadEnvironment` 会尝试加载当前工作目录下的 `.env` 和 NeoCode 托管目录中的 `.env`
- `ProviderConfig.ResolveAPIKey` 在真正发起请求前通过 `os.Getenv` 读取密钥

## 运行时更新
- TUI 只通过 `ConfigManager.Update` 修改配置
- TUI 可以切换当前 Provider、当前模型，以及设置全局 `api_key_env_override`，但不直接修改 Provider 元数据
- `base_url`、`api_key_env`、`driver` 和 `models` 由代码内建定义提供，不从 YAML 读写
- 修改模型时，只更新 `current_model`；当前 Provider 的 `model` 仍表示默认模型，`models` 负责描述该实例可选模型列表

## 默认值治理
- 默认 Provider 名称、URL、默认模型、模型列表和环境变量名统一由内建 Provider 定义提供
- 当前内建 Provider 包括 `openai`、`gemini`、`openll` 和 `qiniuyun`；其中 `qiniuyun` 复用 OpenAI-compatible driver，并默认读取 `QINIUYUN_API_KEY`
- `Loader` 在加载旧配置时会丢弃 `providers` / `provider_overrides`，重新回到“YAML 只保存选择状态和顶层 override”的最小结构
- Provider 的可选模型目录属于实例配置，进入运行时 `Config` 后再提供给 TUI 和 runtime，避免 TUI 或 driver 自己维护一套零散常量

## 安全约束
- 读操作统一走 `Get`，并返回拷贝后的配置快照
- 写操作统一走 `Update`，修改前后都要做校验
- 真实密钥不能出现在日志、状态栏、聊天流或错误提示中
