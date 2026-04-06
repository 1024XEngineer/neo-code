# Config 模块

## 角色定位

Config 负责加载、校验、更新运行配置，并向 runtime/provider/tools 提供一致的配置快照。

## [CURRENT] 实现基线

- 当前核心实现为 `config.Manager`。
- 已有能力：`Load`、`Reload`、`Get`、`Save`、`Update`。
- provider 选择通过 `SelectedProvider` / `ResolvedSelectedProvider` 暴露。

## [PROPOSED] 目标态

- 抽象 `Registry` 契约，统一 Snapshot/Update/Watch 三类能力。
- 增加配置热更新监听，减少上层轮询。

## 上下游边界

- 上游：runtime、tui。
- 下游：loader 与持久化文件。
- 约束：API Key 只通过环境变量名引用，不写入明文配置。

## 联调注意事项

- 当前无稳定 `Watch` 接口，不应假设配置变更可事件推送。
