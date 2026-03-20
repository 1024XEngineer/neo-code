# Repository Guidelines

## Project Structure & Module Organization
- `main.go` launches the CLI loop, gathers input, and routes messages through `services.Chat`.
- `ai/` houses provider implementations (`modelscope.go` and `provider.go`) that wrap ModelScope clients and message types.
- `services/` keeps request orchestration (currently `request.go`) and is the place for future adapters or middleware.
- Keep documentation, assets, or tooling scripts at the repository root alongside `go.mod` so dependency-modified imports remain straightforward.

## Build, Test, and Development Commands
- `go build ./...` compiles every package; fixes syntax or dependency issues before pushing.
- `go test ./...` executes all Go tests and reports coverage; rerun after touching logic in `ai/` or `services/`.
- `go run main.go` (from the repo root) runs the CLI for manual experimentation; supply any extra flags after `main.go`.
- `gofmt -w <file>` (or `go fmt ./...`) reformats Go files to keep tabs/spacing consistent before staging.

## Coding Style & Naming Conventions
- Follow idiomatic Go: short, lowercase package names (`ai`, `services`), exported identifiers with PascalCase, unexported with camelCase.
- Use tabs for indentation (Go default) and keep lines under ~120 characters when possible.
- Apply `gofmt`/`goimports` before committing; avoid hand formatting.
- Comment exported symbols with complete sentences to satisfy Go tooling and future readers.

## Testing Guidelines
- Name unit tests `*_test.go` and functions `TestXxx`; mimic the existing package structure when adding new suites.
- No test frameworks beyond the standard library are currently in use.
- Run `go test ./...` after any change that touches interface boundaries, especially inside `ai` providers or the `services` layer.

## Commit & Pull Request Guidelines
- Keep commits small and descriptive; prefer conventional prefixes such as `feat:`, `fix:`, `docs:`, or `refactor:` followed by a brief summary (e.g., `feat: add streaming helper`).
- Include a concise PR description, list linked issues if any, and note test commands that were run.
- Before asking for review, run `git status`, ensure gofmt has been applied, and double-check that no secret values leaked into tracked files (e.g., `.env`).

## Security & Configuration Tips
- Treat `.env` or similar files as local configuration; do not commit secrets. If shared values are needed, document them in README or this guide instead.
- Keep API keys out of source files; prefer environment variables passed at runtime.
