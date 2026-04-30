#!/usr/bin/env bash
set -euo pipefail

ARCH_DIR="docs/architecture"
errors=0

fail() {
  echo "[contract-lint][ERROR] $1"
  errors=$((errors + 1))
}

# 1) BOM 检查
while IFS= read -r -d '' file; do
  sig="$(head -c 3 "$file" | od -An -t x1 | tr -d ' \n')"
  if [[ "$sig" == "efbbbf" ]]; then
    fail "UTF-8 BOM detected: $file"
  fi
done < <(find "$ARCH_DIR" -type f -print0)

# 2) interface.go 头部约束
notice='// 说明：本文件为架构契约定义，仅用于文档与校验，不参与生产编译。'
while IFS= read -r -d '' file; do
  l1="$(sed -n '1p' "$file")"
  l2="$(sed -n '2p' "$file")"
  l3="$(sed -n '3p' "$file")"
  [[ "$l1" == "//go:build ignore" ]] || fail "missing line1 build tag in $file"
  [[ "$l2" == "// +build ignore" ]] || fail "missing line2 build tag in $file"
  [[ "$l3" == "$notice" ]] || fail "missing build-tag explanation in $file"
done < <(find "$ARCH_DIR" -type f -name 'interface.go' -print0)

# 3) Gateway payload 类型安全与映射
GW_IF="$ARCH_DIR/gateway/interface.go"
GW_MD="$ARCH_DIR/gateway/README.md"
for needle in \
  "type FramePayload interface" \
  "PayloadKind PayloadKind" \
  "Payload FramePayload" \
  "type RunRequestPayload struct" \
  "type CompactRequestPayload struct" \
  "type CancelRequestPayload struct" \
  "type ListSessionsRequestPayload struct" \
  "type LoadSessionRequestPayload struct" \
  "type SetSessionWorkdirRequestPayload struct" \
  "type RuntimeEventPayload struct" \
  "type AckPayload struct"; do
  grep -Fq "$needle" "$GW_IF" || fail "gateway/interface.go missing: $needle"
done

grep -Fq "FrameType + FrameAction -> Payload" "$GW_MD" || fail "gateway/README.md missing payload mapping matrix"

# 4) CLI Invocation 约束
CLI_IF="$ARCH_DIR/cli/interface.go"
grep -Fq "Argv 不能为 nil" "$CLI_IF" || fail "cli/interface.go missing Argv constraint"
grep -Fq "Workdir 必须为绝对路径" "$CLI_IF" || fail "cli/interface.go missing Workdir absolute path constraint"

if (( errors > 0 )); then
  echo "[contract-lint] failed with $errors error(s)."
  exit 1
fi

echo "[contract-lint] passed"