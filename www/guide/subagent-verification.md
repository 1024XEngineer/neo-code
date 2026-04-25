---
title: 子代理与验证器
description: 介绍 spawn_subagent 工具的三种角色、验收验证器的配置与内置验证器列表。
---

# 子代理与验证器

NeoCode 提供两种"任务质量保障"机制：子代理用于拆分和并行化复杂任务，验证器用于在任务完成前自动检查结果质量。

## 子代理

NeoCode 内置 `spawn_subagent` 工具，允许主 Agent 在受控预算内启动独立子代理执行子任务。子代理的运行结果会回灌到主会话上下文中。

### 三种角色

| 角色 | 用途 | 典型场景 |
|------|------|----------|
| `researcher` | 检索与分析 | 搜索代码、查找文档、收集信息 |
| `coder` | 实现与修复 | 编写代码、修复 bug、重构 |
| `reviewer` | 审查与验收 | 代码审查、结果验证、一致性检查 |

### 参数

`spawn_subagent` 支持以下参数：

| 参数 | 说明 |
|------|------|
| `mode` | 当前仅支持 `inline`（同步内联模式） |
| `role` | 角色类型：`researcher` / `coder` / `reviewer` |
| `id` | 子代理标识，用于结果回灌时区分来源 |
| `prompt` | 子代理的任务描述 |
| `expected_output` | 期望输出的简要描述 |
| `max_steps` | 最大推理步数（默认 6） |
| `timeout_sec` | 超时时间（默认 30 秒） |
| `allowed_tools` | 限制子代理可使用的工具列表 |
| `allowed_paths` | 限制子代理可访问的路径列表 |

### 执行约束

子代理在独立预算内运行，不影响主会话的 token 预算和步数计数：

- 超过 `max_steps` 或 `timeout_sec` 后自动终止
- `allowed_tools` 和 `allowed_paths` 限制子代理的能力边界
- 子代理的工具调用仍经过统一权限审批

### 使用示例

在对话中，你可以这样引导 Agent 使用子代理：

```text
请用 researcher 角色搜索 internal/runtime 下所有与 compact 相关的函数签名
请用 coder 角色在 internal/tools/memo 下新增一个 memo_update 工具
请用 reviewer 角色审查刚才的修改是否满足测试覆盖率要求
```

Agent 会根据任务需要自行决定是否调用 `spawn_subagent`，你也可以在 Skill 中通过 `tool_hints` 引导 Agent 优先使用子代理。

## 验证器

验证器（Verifier）是任务完成前的自动检查机制。当 Agent 认为任务已完成时，Runtime 会触发验证器对结果进行校验，根据校验结果决定是否允许收尾。

### 内置验证器

| 验证器 | 默认启用 | 检查内容 |
|--------|----------|----------|
| `todo_convergence` | 是（Required） | Todo 列表是否收敛（所有 required 项完成） |
| `file_exists` | 是 | 指定文件是否存在 |
| `content_match` | 是 | 文件内容是否匹配预期 |
| `git_diff` | 是 | Git 变更是否符合预期（默认执行 `git diff --name-only`） |
| `command_success` | 否 | 指定命令是否成功执行 |
| `build` | 否 | 构建是否通过 |
| `test` | 否 | 测试是否通过 |
| `lint` | 否 | Lint 检查是否通过 |
| `typecheck` | 否 | 类型检查是否通过 |

### 验证结果状态

每个验证器返回以下状态之一：

- **pass**：验证通过
- **soft_block**：当前不能收尾，但仍可继续推进
- **hard_block**：当前不能收尾且需要外部条件
- **fail**：验证明确失败

### 验证器配置

验证器行为通过 `config.yaml` 的 `verification` 字段控制：

```yaml
verification:
  enabled: true
  default_task_policy: unknown
  final_intercept: true
  max_no_progress: 3
  max_retries: 2
  verifiers:
    todo_convergence:
      enabled: true
      required: true
      timeout_sec: 5
      fail_closed: true
    build:
      enabled: false
      required: false
      timeout_sec: 300
      fail_closed: true
    test:
      enabled: false
      required: false
      timeout_sec: 300
      fail_closed: true
  execution_policy:
    mode: non_interactive
    allowed_commands:
      - go
      - git
      - npm
      - pnpm
      - make
      - cargo
      - python
      - pytest
      - ruff
      - eslint
      - tsc
      - golangci-lint
    denied_commands:
      - rm
      - sudo
      - curl
      - wget
```

### 关键配置字段

| 字段 | 说明 |
|------|------|
| `verification.enabled` | 是否启用验证引擎（默认 `true`） |
| `verification.final_intercept` | 是否在任务收尾前拦截并触发验证（默认 `true`） |
| `verification.max_no_progress` | 验证无进展时的最大重试次数（默认 `3`） |
| `verification.max_retries` | 验证失败后的最大重试次数（默认 `2`） |
| `verifiers.<name>.enabled` | 是否启用该验证器 |
| `verifiers.<name>.required` | 该验证器是否为硬性要求（required 验证器失败会阻止收尾） |
| `verifiers.<name>.timeout_sec` | 该验证器的执行超时 |
| `verifiers.<name>.fail_closed` | 验证器执行异常时是否按失败处理（`true` = 失败，`false` = 忽略） |
| `execution_policy.allowed_commands` | 验证器可执行的命令白名单 |
| `execution_policy.denied_commands` | 验证器禁止执行的命令黑名单 |

### 启用 build / test / lint 验证器

默认情况下 `build`、`test`、`lint` 验证器是关闭的。如果你的项目有对应的构建和测试工具链，建议在 `config.yaml` 中启用：

```yaml
verification:
  verifiers:
    build:
      enabled: true
      command: go build ./...
      timeout_sec: 300
    test:
      enabled: true
      command: go test ./...
      timeout_sec: 300
    lint:
      enabled: true
      command: golangci-lint run ./...
      timeout_sec: 120
```

## 继续阅读

- 工具与权限机制：看 [工具与权限](./tools-permissions)
- 配置验证器参数：看 [配置入口](./configuration)
- 验证器设计细节：看 [验证器引擎设计](https://github.com/1024XEngineer/neo-code/blob/main/docs/verifier-engine-design.md)
