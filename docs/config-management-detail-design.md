# 配置管理模块详细设计

## 模块职责

`internal/config` 只负责以下几件事：

- 读取和保存 `~/.neocode/config.yaml`
- 提供静态默认值
- 组装运行时 provider 集合
  - builtin provider 来自代码内置定义
  - custom provider 来自 `~/.neocode/providers/<name>/provider.yaml`
- 提供并发安全的配置快照读写能力
- 在真正发起请求前，根据环境变量解析 API Key

`internal/config` 不负责以下事情：

- 不自动加载 `.env`
- 不兼容或清洗旧版 YAML 字段
- 不判断 `selected_provider/current_model` 是否已经可直接运行
- 不承担模型发现、目录缓存或运行时选择修正

最后一项由 `internal/config/state` 负责：它基于当前运行时支持情况和 catalog 结果修正选择状态，并将变更持久化。

## 核心类型

- `config.StaticDefaults()`
  - 返回 config 层负责的静态默认值骨架
  - 不包含 provider 装配，也不保证选择状态可运行
- `config.Loader`
  - 负责 `config.yaml` 与 custom provider 文件的文件系统读写
- `config.Manager`
  - 提供线程安全的快照读取、更新与保存
- `config.Config`
  - 顶层运行时配置快照
- `config.Config.ValidateSnapshot()`
  - 只校验配置快照本身是否结构完整
  - 不等价于“当前配置已经 runtime-ready”

## 配置来源

### 1. 静态默认值

静态默认值由 `config.StaticDefaults()` 提供，覆盖：

- `workdir`
- `shell`
- `tool_timeout_sec`
- `generate_start_timeout_sec`
- `context`
- `tools`

这些默认值只表达“缺省配置长什么样”，不代表完整可运行配置。

### 2. builtin provider

builtin provider 由 `internal/config/builtin_providers.go` 提供。这里集中维护：

- provider 名称
- driver
- base URL
- 默认模型
- API Key 对应的环境变量名

### 3. custom provider

custom provider 来自：

```text
~/.neocode/providers/<provider-name>/provider.yaml
```

当前只接受明确受支持的字段；未知字段会直接报错，不做“旧格式自动迁移”。
`provider.yaml` 只支持平铺字段：`name/driver/base_url/api_key_env/model_source/chat_api_mode/chat_endpoint_path/discovery_endpoint_path/generate_max_retries/generate_idle_timeout_sec/models`。

`generate_start_timeout_sec` 已上移到根 `config.yaml` 顶层，启动 preflight 会自动将缺失字段补写为默认值 `90`。

## 加载流程

启动时配置加载流程如下：

1. 构造 `StaticDefaults`
2. 读取 `config.yaml`
3. 严格解析 YAML
   - 使用 `KnownFields(true)`
   - 未知字段直接失败
4. 扫描 custom provider 目录
5. 组装 builtin/custom provider 集合
6. 应用静态默认值
7. 执行 `ValidateSnapshot()`
8. 交给 `config/state.Service.EnsureSelection()` 修正选择状态

这里有两个明确边界：

- `ValidateSnapshot()` 只验证“配置快照是否完整”
- `EnsureSelection()` 才处理“当前选择是否还能运行”

## 持久化策略

`config.yaml` 只持久化用户可编辑的最小运行时状态，不持久化 provider 元数据。

当前持久化内容包括：

- `selected_provider`
- `current_model`
- `shell`
- `tool_timeout_sec`
- `generate_start_timeout_sec`
- `context`
- `tools`

当前不写回 `config.yaml` 的内容包括：

- `providers`
- `base_url`
- `api_key_env`
- `models`
- `workdir`

`workdir` 只来自启动默认值或 CLI 覆盖，不进入 YAML。

## 安全约束

- API Key 只从环境变量读取
- YAML 中不保存明文密钥
- 配置访问统一走 `Manager.Get()` / `Manager.Update()`
- `Get()` 返回克隆快照，避免共享可变状态
- `Update()` 在保存前重新执行默认值补齐与快照校验

## 设计结论

`config` 层的目标不是“替运行时兜底一切”，而是提供一份边界清晰、结构稳定、可验证的配置快照。

因此这里坚持三条规则：

- 不在 `config` 中堆兼容旧字段的逻辑
- 不把选择修正混进快照校验
- 不把 provider/catalog/runtime 语义倒灌回 `config`

## custom provider `models` 校验约束

`~/.neocode/providers/<provider-name>/provider.yaml` 中的 `models` 现在只表达“用户声明需要出现的模型 ID / 展示名”，不再承担元数据补齐职责。

当前约束是：

- `models[].id` 必须非空。
- `models[].name` 必须非空。
- `models` 中不允许出现 `context_window`、`max_output_tokens`、`description`、`capability_hints`。
- discovery 缓存只保存规范化后的白名单字段：`id/name/description/context_window/max_output_tokens/capability_hints`。
- builtin provider 不再走 `/models` discovery，模型清单改为仓库内静态维护。
