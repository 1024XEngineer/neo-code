#!/usr/bin/env sh

set -eu

usage() {
	cat <<'USAGE'
在仓库内直接创建 GitHub Issue。

用法:
  ./scripts/create_issue.sh --type <proposal|architecture|implementation> --title <标题> [选项]

选项:
  --repo <owner/repo>       目标仓库，默认自动检测当前仓库
  --body-file <path>        指定 issue 正文文件
  --labels <a,b,c>          逗号分隔的标签列表（可选）
  --type <type>             issue 类型：proposal|architecture|implementation
  --title <title>           issue 标题（不含类型前缀）
  -h, --help                显示帮助

示例:
  ./scripts/create_issue.sh --type proposal --title "新增会话恢复策略"
  ./scripts/create_issue.sh --type implementation --title "修复 streaming 中断持久化" --labels "bug,priority-high"
USAGE
}

require_cmd() {
	if ! command -v "$1" >/dev/null 2>&1; then
		echo "缺少命令: $1" >&2
		exit 1
	fi
}

default_repo() {
	gh repo view --json nameWithOwner -q .nameWithOwner 2>/dev/null || true
}

title_prefix() {
	case "$1" in
	proposal) echo "【提案】" ;;
	architecture) echo "【架构】" ;;
	implementation) echo "【实现】" ;;
	*) return 1 ;;
	esac
}

# trim_label 用于去除标签参数的首尾空白字符，避免传递无效标签值。
trim_label() {
	printf '%s' "$1" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//'
}

create_body_file() {
	type="$1"
	out="$2"

	case "$type" in
	proposal)
		cat >"$out" <<'BODY'
### 目标问题（Why）
- 当前痛点：
- 触发场景：

### 设计方案（How）
- 核心设计：
- 关键机制：
- 边界与非目标：

### 落地清单（What）
- [ ]
- [ ]

### 验收标准（Done）
- [ ]
- [ ]

### 风险与回滚
- 风险：
- 回滚方案：
BODY
		;;
	architecture)
		cat >"$out" <<'BODY'
### 目标问题（Why）
- 当前痛点：
- 影响范围：

### 现状与边界
- TUI：
- Runtime：
- Provider/Tools：
- Session/Context：

### 核心设计（How）
- 核心设计：
- 数据流/事件流：
- 关键取舍：

### 落地清单（What）
- [ ]
- [ ]

### 验收标准（Done）
- [ ]
- [ ]

### 风险与回滚
- 风险：
- 回滚方案：
BODY
		;;
	implementation)
		cat >"$out" <<'BODY'
### 关联 RFC / 架构
- 提案/架构 issue：
- 当前问题：

### 实现设计（How）
- 关键改动点：
- 影响模块：
- 边界与非目标：

### 任务拆解
- [ ]
- [ ]

### 测试与验证（Done）
- [ ] 正常路径
- [ ] 边界条件
- [ ] 异常分支

### 风险与回滚
- 风险：
- 回滚方案：
BODY
		;;
	*)
		echo "不支持的类型: $type" >&2
		exit 1
		;;
	esac
}

REPO=""
BODY_FILE=""
LABELS=""
TYPE=""
TITLE=""

while [ "$#" -gt 0 ]; do
	case "$1" in
	--repo)
		REPO="${2:-}"
		shift 2
		;;
	--body-file)
		BODY_FILE="${2:-}"
		shift 2
		;;
	--labels)
		LABELS="${2:-}"
		shift 2
		;;
	--type)
		TYPE="${2:-}"
		shift 2
		;;
	--title)
		TITLE="${2:-}"
		shift 2
		;;
	-h|--help)
		usage
		exit 0
		;;
	*)
		echo "未知参数: $1" >&2
		usage
		exit 1
		;;
	esac
done

require_cmd gh

if [ -z "$TYPE" ] || [ -z "$TITLE" ]; then
	echo "--type 和 --title 为必填参数" >&2
	usage
	exit 1
fi

if [ -z "$REPO" ]; then
	REPO="$(default_repo)"
fi
if [ -z "$REPO" ]; then
	echo "无法自动识别仓库，请通过 --repo 显式传入 owner/repo" >&2
	exit 1
fi

PREFIX="$(title_prefix "$TYPE" || true)"
if [ -z "$PREFIX" ]; then
	echo "--type 仅支持: proposal | architecture | implementation" >&2
	exit 1
fi

FINAL_TITLE="$PREFIX $TITLE"
TEMP_BODY=""
if [ -n "$BODY_FILE" ]; then
	if [ ! -f "$BODY_FILE" ]; then
		echo "--body-file 指向的文件不存在: $BODY_FILE" >&2
		exit 1
	fi
else
	TEMP_BODY="$(mktemp -t neocode-issue-body-XXXXXX.md)"
	BODY_FILE="$TEMP_BODY"
	create_body_file "$TYPE" "$BODY_FILE"
fi

cleanup() {
	if [ -n "$TEMP_BODY" ] && [ -f "$TEMP_BODY" ]; then
		rm -f "$TEMP_BODY"
	fi
}
trap cleanup EXIT INT TERM

set -- issue create --repo "$REPO" --title "$FINAL_TITLE" --body-file "$BODY_FILE"
if [ -n "$LABELS" ]; then
	OLD_IFS=$IFS
	IFS=','
	for label in $LABELS; do
		trimmed_label="$(trim_label "$label")"
		if [ -n "$trimmed_label" ]; then
			set -- "$@" --label "$trimmed_label"
		fi
	done
	IFS=$OLD_IFS
fi

ISSUE_URL="$(gh "$@")"
echo "Issue created: $ISSUE_URL"
