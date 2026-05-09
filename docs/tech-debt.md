# NeoCode 已知局限与技术债

### 16.1 架构风险

| 风险 | 严重度 | 描述 | 缓解措施 |
|------|--------|------|----------|
| **Gateway 单点故障** | 中 | 所有客户端依赖 Gateway 作为唯一入口；Gateway 进程异常时整个系统不可用 | 客户端内置自动拉起（auto-spawn）；本地 loopback 部署下 Gateway 与 CLI 同生命周期；网络模式下建议部署多个 Gateway 实例（当前未实现） |
| **模型行为不可预测** | 高 | 底层模型升级或切换时，Agent 的行为可能发生微妙变化（推理深度、工具选择偏好、错误处理风格），且这种变化难以通过自动化测试捕获 | Provider 层契约极简（2 方法），限制厂商差异扩散；100% 覆盖率的框架层测试确保框架逻辑不受影响；实际模型行为通过验收测试（`runtime/acceptance/`）做抽样验证 |
| **SQLite 写并发瓶颈** | 低 | 同会话的所有写操作（追加消息、更新状态、Compact 替换）串行执行；当需要跨多个会话做批量分析时单 writer 限制成为瓶颈 | 同会话并发写已通过 `sessionLock` 串行化，不同会话可并行；当前用户场景（单用户、顺序交互）下不构成实际瓶颈；若未来需要批量跨会话操作，可通过读写分离（读可并发）缓解 |
| **上下文窗口天花板** | 中 | 即使有 Compact，模型原生的 context window 有硬限制（如 Claude 200K、GPT-4 128K）；对于超长会话，最终仍会达到无法继续的临界点 | Compact 两级策略（Micro + Full）最大化利用现有窗口；`max_turns` 限制防止无限循环；长期来看需借助模型厂商的 context window 增长 |
| **TOCTOU 路径竞态** | 低 | 文件系统操作在 Security Engine 校验通过后、实际读写前，目标路径的状态可能被外部进程改变（symlink 替换攻击） | 当前在校验时 resolve symlink，但存在微小的时间窗口；现代 OS 的 `O_NOFOLLOW` 等标志可进一步缓解；实际攻击面极小（本地单用户场景） |

### 16.2 已知局限

| 局限 | 影响 | 讨论 |
|------|------|------|
| **单机单用户模型** | 不支持多用户共享同一 NeoCode 实例 | 这是刻意的设计选择（见 ADR-004）。多用户场景可通过每个用户运行自己的 Gateway 实例 + 共享 Runner 来解决，不需要多租户 |
| **无分布式追踪** | SessionID/RunID 仅在 NeoCode 内部可追踪，无法与外部的 APM（如 Datadog、Jaeger）关联 | 当前通过标准化日志格式（SessionID+RunID 前缀）做手动关联，未引入分布式追踪 SDK。如果未来部署复杂度提升，可在 Gateway 层注入 OpenTelemetry context |
| **纯 Go 生态** | 工具和 Skill 的执行受限于 Go 生态；无法直接调用 Python/Node.js 库 | MCP 协议（stdio 子进程）提供了语言无关的扩展通道。Python/Node.js 工具可通过 MCP server 接入 |
| **Web UI 嵌入分发** | Web 端 patch 只能随二进制更新，不支持独立热更新 | 这是单二进制部署的代价。对于需要频繁更新 UI 的场景，可将 Web 端独立部署（当前已支持 `neocode-gateway` 独立二进制 + 反向代理静态资源） |
| **无插件市场/发现机制** | Skills 和 MCP server 的获取依赖用户手动配置，没有中心化的发现和安装渠道 | 当前的 Skills 设计优先保证离线可用性和零信任（文件即 Skill）。发现机制可在此基础上叠加 |

### 16.3 技术债清单

| 技术债 | 位置 | 影响 | 建议处理时机 |
|--------|------|------|-------------|
| **底层传输层 IPC 残留** | `internal/gateway/transport/` — Unix domain socket / Named pipe | 客户端连接路径复杂（需判断平台选 socket 类型），迁移到全 HTTP 后可消除 | 短期——已在迁移计划中 |
| **`runtime/run.go` 单文件过长** | ReAct 主循环逻辑集中在 `run.go` (~400 行) 和 `runtime.go` (~540 行) | 新成员理解核心循环需要较长时间；修改风险集中在少数大文件中 | 中期——可按阶段拆分（pre-processing / loop body / termination） |
| **Compact 策略配置分散** | MicroCompact 配置在 `MicroCompactConfig`，Full Compact 在 `CompactConfig`，部分阈值在 `RuntimeConfig` | 调整上下文管理策略需要理解三处配置 | 中期——收敛为统一的 `CompactPolicy` 结构体 |
| **Gateway Bootstrap 单文件** | `bootstrap.go` 超过 1600 行，包含帧路由、认证、session CRUD、RPC 处理 | 单体文件难以定位和维护 | 中期——拆分为 `session_handler.go`、`rpc_handler.go`、`auth_handler.go` |
| **Acceptance 测试耗时长** | `runtime/acceptance/` 的端到端测试依赖真实模型 API | CI 成本高、不稳定（网络波动导致 flaky） | 长期——增加录制/回放（VCR）模式，CI 中默认使用录制的 fixture |

---

