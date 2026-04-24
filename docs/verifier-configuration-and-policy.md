# Verifier Configuration And Policy

## 配置来源
- 全局：`~/.neocode/config.yaml` 的 `runtime.verification`。
- 仓库级扩展预留：`.neocode/verification.yaml`（本阶段先保留接口与策略位）。

## 优先级
- 仓库级 > 全局级 > 内建默认值（策略已按该优先级设计）。

## 命令来源
- 所有命令型 verifier 从 `runtime.verification.verifiers.<name>.command` 读取。
- verifier 内禁止硬编码项目命令。

## 启停规则
- verifier 支持独立 `enabled/required`。
- 未配置 command 时：
  - required=true -> 返回显式 soft_block/fail
  - required=false -> skip（显式结果，不 silent pass）

## required / optional 行为
- required verifier 失败会阻断 final。
- optional verifier 缺省可跳过，但仍有事件与结果记录。

## non-interactive policy
- verifier 命令走独立 `execution_policy`，不走普通 ask 权限链路。
- 默认白名单命令（go/git/test/lint/typecheck 等）。
- 明确拒绝高风险命令（如 `rm`、`sudo`）。

