# 配置管理模块详细设计
## 模块职责
`config` 模块主要负责四类事情：
- 加载和保存 YAML 配置文件
- 从环境变量解析真实密钥
- 管理 NeoCode 托管目录中的配置与 `.env`
- 向运行中的系统提供并发安全的配置读写能力

## 核心类型
- `Config`：顶层应用配置，包含 Provider 列表、当前选中 Provider、当前模型、工作目录、Shell 和循环限制等信息
- `ProviderConfig`：单个 Provider 的配置项，包括 Base URL、默认模型和 API Key 环境变量名
- `Manager`：使用 `sync.RWMutex` 保护的配置访问器与修改器
- `Loader`：对 YAML 文件和托管 `.env` 文件的文件系统封装

## 环境变量策略
- YAML 只保存 `api_key_env`，不保存真实密钥。
- `Loader.LoadEnvironment` 会尝试加载当前工作目录下的 `.env` 和 NeoCode 托管目录中的 `.env`。
- `ProviderConfig.ResolveAPIKey` 在真正发起请求前通过 `os.Getenv` 读取密钥。

## 运行时更新
- TUI 只能通过 `ConfigManager.Update` 修改配置。
- 修改 Base URL 时只更新当前选中 Provider，并立即持久化。
- 修改 API Key 时写入托管 `.env`，然后重新加载环境变量并刷新配置快照。
- 修改模型时，同时更新 `current_model` 和当前 Provider 的 `model` 字段，保持状态一致。

## 默认值治理
- 默认 Provider 名称、URL、模型和环境变量名统一定义在 `internal/config/model.go` 中。
- 内建模型目录也收口在 `config` 包中，避免 TUI 自己维护一套零散的临时常量。

## 安全约束
- 读操作统一走 `Get`，并返回拷贝后的配置快照。
- 写操作统一走 `Update`，修改前后都要做校验。
- 真实密钥不能出现在日志、状态栏、聊天流或错误提示中。
