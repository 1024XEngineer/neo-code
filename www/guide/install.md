---
title: 安装与运行
description: 安装脚本、源码运行、环境要求以及第一次启动前的准备项。
---

# 安装与运行

## 环境要求

- Go `1.25+`
- 至少一个可用的 Provider API Key，例如 OpenAI、Gemini、OpenLL 或 Qiniu

## 一键安装

### macOS / Linux

```bash
curl -fsSL https://raw.githubusercontent.com/1024XEngineer/neo-code/main/scripts/install.sh | bash
```

### Windows PowerShell

```powershell
irm https://raw.githubusercontent.com/1024XEngineer/neo-code/main/scripts/install.ps1 | iex
```

## 从源码运行

```bash
git clone https://github.com/1024XEngineer/neo-code.git
cd neo-code
go run ./cmd/neocode
```

如果你只想启动 Gateway：

```bash
go run ./cmd/neocode gateway
```

指定网络访问面监听地址时，可以显式传入 `--http-listen`：

```bash
go run ./cmd/neocode gateway --http-listen 127.0.0.1:8080
```

## 第一次启动前要准备什么

NeoCode 不会把 API Key 写进 `config.yaml`，而是直接读取环境变量。

### Shell

```bash
export OPENAI_API_KEY="your_key_here"
export GEMINI_API_KEY="your_key_here"
export AI_API_KEY="your_key_here"
export QINIU_API_KEY="your_key_here"
```

### Windows PowerShell

```powershell
$env:OPENAI_API_KEY = "your_key_here"
$env:GEMINI_API_KEY = "your_key_here"
$env:AI_API_KEY = "your_key_here"
$env:QINIU_API_KEY = "your_key_here"
```

## 工作区启动

只影响当前进程、不回写配置文件：

```bash
go run ./cmd/neocode --workdir /path/to/workspace
```

这个参数会影响当前运行使用的工作区根目录，也会影响工具访问范围和 session 隔离。

## Gateway 自动拉起

当前实现里：

- `neocode` 默认优先通过本地 Gateway 转发 runtime 请求与事件流
- 启动时会先探测本地网关
- 如果本地网关未运行，会自动尝试后台拉起并等待就绪
- 如果拉起后仍不可达或握手失败，会直接报错退出

接下来建议阅读 [首次上手](./quick-start)。
