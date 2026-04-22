# Quick Start

## Install

### macOS / Linux

```bash
curl -fsSL https://raw.githubusercontent.com/1024XEngineer/neo-code/main/scripts/install.sh | bash
```

### Windows PowerShell

```powershell
irm https://raw.githubusercontent.com/1024XEngineer/neo-code/main/scripts/install.ps1 | iex
```

### Run from source

For contributors who want to inspect the code or work on the codebase:

```bash
git clone https://github.com/1024XEngineer/neo-code.git
cd neo-code
go run ./cmd/neocode
```

## Set API keys

NeoCode reads API keys from environment variables. Keys are never written to config files.

```bash
# Set the key for the provider you want to use
export OPENAI_API_KEY="sk-..."
export GEMINI_API_KEY="AI..."
```

See the [configuration guide](./guides/configuration) for a full list of providers and their environment variables.

## Start

```bash
neocode
```

On first run, NeoCode will prompt you to select a provider and model. Your choice is saved to `~/.neocode/config.yaml`.

## Set a working directory

```bash
neocode --workdir /path/to/your/project
```

The working directory applies to the current process only and is not saved to config.

## Next steps

- [Configure providers and models](./guides/configuration)
- [Switch models](./guides/providers)
- [Set up MCP tools](./guides/mcp)
