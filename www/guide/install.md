---
title: 安装与运行
description: 安装脚本、源码运行、环境要求以及第一次启动前的准备项。
---

# 安装与运行

## 环境要求

- Go `1.25+`
- 至少一个可用的 Provider API Key，例如 OpenAI、Gemini、OpenLL、Qiniu 或 ModelScope

## 一键安装

### macOS / Linux

```bash
curl -fsSL https://raw.githubusercontent.com/1024XEngineer/neo-code/main/scripts/install.sh | bash
```

### Windows PowerShell

```powershell
irm https://raw.githubusercontent.com/1024XEngineer/neo-code/main/scripts/install.ps1 | iex
```

安装脚本会自动从 GitHub Releases 下载最新稳定版二进制文件：

- macOS / Linux：下载并解压到 `/usr/local/bin/`（可能需要 `sudo` 权限）
- Windows：下载并解压到 `%LOCALAPPDATA%\NeoCode`，同时更新用户 `PATH`

安装完成后，在终端直接运行：

```bash
neocode
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
export MODELSCOPE_API_KEY="your_key_here"
```

### Windows PowerShell

```powershell
$env:OPENAI_API_KEY = "your_key_here"
$env:GEMINI_API_KEY = "your_key_here"
$env:AI_API_KEY = "your_key_here"
$env:QINIU_API_KEY = "your_key_here"
$env:MODELSCOPE_API_KEY = "your_key_here"
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

## 验证安装

安装完成后，可以运行以下命令确认版本：

```bash
neocode version
```

如果看到版本号输出（如 `1.2.3` 或 `dev`），说明安装成功。

## 常用启动参数

```bash
neocode                    # 默认启动，进入 TUI
neocode --workdir /path    # 指定工作区
neocode --help             # 查看所有启动参数
neocode version            # 查看版本
neocode update             # 升级到最新版
neocode gateway            # 启动 Gateway
```

接下来建议阅读 [首次上手](./quick-start)。
