---
title: NeoCode 是什么
description: NeoCode 的定位、适用场景、主链路和当前真实能力概览。
---

# NeoCode 是什么

NeoCode 是一个基于 Go 和 Bubble Tea 的本地 Coding Agent，运行在终端里，围绕 ReAct 风格的执行闭环工作：

`用户输入 -> Agent 推理 -> 调用工具 -> 获取结果 -> 继续推理 -> UI 展示`

## 适合谁

NeoCode 更适合这些场景：

- 你希望在本地终端里完成代码阅读、修改、调试和自动化操作，而不是切换多个浏览器标签页。
- 你希望 Provider 差异收敛在内部层，不把模型厂商细节扩散到运行时和 UI。
- 你需要会话持久化、上下文压缩、工作区隔离，以及一条可验证的执行链路。

## 当前真实能力

以当前仓库实现为准，NeoCode 已经具备这些入口：

- Bubble Tea TUI 交互界面
- 本地 Gateway 进程与 URL Scheme 派发入口
- Provider / Model 选择
- 会话持久化与恢复
- 上下文压缩与自动压缩配置
- 记忆查看、写入、删除
- Skills 发现、激活、停用和会话恢复

## 模块怎么分工

你不需要先理解所有内部实现，但知道这几个层次会更容易判断问题在哪一层：

- `internal/tui`：终端界面、Slash 命令和运行状态展示
- `internal/gateway`：IPC / 网络接入、鉴权、ACL 和流式中继
- `internal/runtime`：ReAct 主循环、tool result 回灌、停止条件、事件派发
- `internal/provider`：模型协议差异、请求组装和流式响应解析
- `internal/tools`：工具契约、参数校验、执行与结果收敛

## 接下来读什么

- 想先跑起来：看 [安装与运行](./install)
- 想知道第一次怎么提问：看 [首次上手](./quick-start)
- 想理解配置文件和 provider：看 [配置入口](./configuration)
