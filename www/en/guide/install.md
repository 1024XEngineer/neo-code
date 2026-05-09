---
title: Install & First Run
description: From installation to your first conversation in 3 minutes.
---

# Install & First Run

## 1. Requirements

- macOS, Linux, or Windows
- A terminal such as PowerShell or your system default terminal
- At least one API key: OpenAI, Gemini, OpenLL, Qiniu, or ModelScope

## 2. Install

macOS / Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/1024XEngineer/neo-code/main/scripts/install.sh | bash
```

Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/1024XEngineer/neo-code/main/scripts/install.ps1 | iex
```

## 3. Set API key

NeoCode reads API keys from environment variables and never writes them to config files.

macOS / Linux:

```bash
export OPENAI_API_KEY="your_key_here"
```

Windows PowerShell:

```powershell
$env:OPENAI_API_KEY = "your_key_here"
```

| Provider | Environment variable |
|---|---|
| OpenAI | `OPENAI_API_KEY` |
| Gemini | `GEMINI_API_KEY` |
| OpenLL | `AI_API_KEY` |
| Qiniu | `QINIU_API_KEY` |
| ModelScope | `MODELSCOPE_API_KEY` |

## 4. Launch

```bash
neocode
```

Open a specific project:

```bash
neocode --workdir /path/to/your/project
```

Type natural language in the input box to chat. Type `/` to see local control commands.

NeoCode also provides a browser-based Web UI:

```bash
neocode web
```

Tagged release builds start the Web UI from assets already embedded in the `neocode` binary, so the target machine does not need Node.js or npm. When running from source, a missing `web/dist` still triggers a local frontend build.

## 5. First conversation

Try:

```text
Read the current project directory structure, summarize module responsibilities, and point out which files I should read first.
```

Then:

```text
Based on that structure, find the test entry point and the main business entry point.
```

The agent uses file reading and search tools automatically. NeoCode asks before file writes or risky commands.

## 6. Add AGENTS.md for long-term projects

For maintained projects, add `AGENTS.md` at the repository root:

```md
# Project Rules

- Run `go test ./...` after changing Go code
- Keep Chinese docs in Chinese
- Do not write API keys to config files
```

See [AGENTS.md Project Rules](./agents-md).

## Run from source

Use source builds for development or debugging. Go 1.25+ is required.

```bash
git clone https://github.com/1024XEngineer/neo-code.git
cd neo-code
go build ./...
go run ./cmd/neocode
```

## Next steps

- Local commands: [Slash Commands](./slash-commands)
- Workspace and sessions: [Sessions, Context, and Workspace](./context-session-workspace)
- More prompts: [Usage Examples](./examples)
- Installation issues: [Troubleshooting](./troubleshooting)
