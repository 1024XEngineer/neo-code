#!/usr/bin/env bash
set -euo pipefail

warns=0
warn() {
  echo "[contract-drift][WARN] $1"
  warns=$((warns + 1))
}

DOCS_PROVIDER="docs/architecture/provider/interface.go"
IMPL_PROVIDER="internal/provider/provider.go"
IMPL_RUNTIME="internal/runtime/runtime.go"

if grep -Fq "Generate(ctx context.Context, req GenerateRequest" "$DOCS_PROVIDER" && \
   grep -Fq "Chat(ctx context.Context, req ChatRequest" "$IMPL_PROVIDER"; then
  warn "provider contract drift: docs use Generate/GenerateRequest, implementation still Chat/ChatRequest"
fi

if grep -Fq "provider.ChatRequest" "$IMPL_RUNTIME"; then
  warn "runtime still depends on provider.ChatRequest; docs contract has moved to provider.GenerateRequest"
fi

if [[ ! -f "internal/gateway/interface.go" ]]; then
  warn "no internal gateway contract file found; cannot auto-compare MessageFrame payload typing"
fi

if [[ ! -f "internal/cli/interface.go" ]]; then
  warn "no internal cli contract file found; cannot auto-compare Invocation validation constraints"
fi

echo "[contract-drift] completed with $warns warning(s)"
exit 0