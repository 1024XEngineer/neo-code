# 快速开始

## 安装

### macOS / Linux

```bash
curl -fsSL https://raw.githubusercontent.com/1024XEngineer/neo-code/main/scripts/install.sh | bash
```

### Windows PowerShell

```powershell
irm https://raw.githubusercontent.com/1024XEngineer/neo-code/main/scripts/install.ps1 | iex
```

### 从源码运行

适合想阅读代码、调试功能或参与开发的方式：

```bash
git clone https://github.com/1024XEngineer/neo-code.git
cd neo-code
go run ./cmd/neocode
```

## 配置 API Key

NeoCode 从系统环境变量读取 API Key，不会写入配置文件。

```bash
# 选择你要使用的 provider，设置对应的环境变量
export OPENAI_API_KEY="sk-..."
export GEMINI_API_KEY="AI..."
```

完整的 provider 与环境变量对应关系见[配置指南](./guides/configuration)。

## 启动

```bash
neocode
```

默认使用上次保存的 provider 和模型。首次启动会进入交互式选择。

## 指定工作目录

```bash
neocode --workdir /path/to/your/project
```

工作目录只对当前进程生效，不会写入配置文件。

## 下一步

- [配置 provider 和模型](./guides/configuration)
- [切换模型](./guides/providers)
- [配置 MCP 工具](./guides/mcp)
