#!/usr/bin/env sh

set -eu

ROOT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
SRC_DIR="$ROOT_DIR/.agents/skills"

if [ ! -d "$SRC_DIR" ]; then
	echo "技能目录不存在: $SRC_DIR" >&2
	exit 1
fi

DEFAULT_TARGETS="$ROOT_DIR/.codex/skills:$ROOT_DIR/.claude/skills:$ROOT_DIR/.cursor/skills:$ROOT_DIR/.windsurf/skills"
TARGETS="${SKILL_INSTALL_TARGETS:-$DEFAULT_TARGETS}"

copied=0
old_ifs=$IFS
IFS=':'
for target in $TARGETS; do
	if [ -z "$target" ]; then
		continue
	fi
	mkdir -p "$target"
	cp -R "$SRC_DIR"/. "$target"/
	echo "installed -> $target"
	copied=$((copied + 1))
done
IFS=$old_ifs

if [ "$copied" -eq 0 ]; then
	echo "未安装任何技能目录，请检查 SKILL_INSTALL_TARGETS" >&2
	exit 1
fi

echo "skills installed: $copied target(s)"
