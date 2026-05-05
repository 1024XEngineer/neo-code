package ptyproxy

import (
	"fmt"
	"strings"
)

const shellInitScript = `# >>> neocode shell integration >>>
if [ -n "${NEOCODE_SHELL_INIT_LOADED:-}" ]; then
	return 0 2>/dev/null || exit 0
fi
export NEOCODE_SHELL_INIT_LOADED=1

__neocode_emit_osc133() {
	local _payload="$1"
	if [ -n "${TMUX:-}" ]; then
		printf '\033Ptmux;\033\033]133;%s\007\033\\' "$_payload"
	else
		printf '\033]133;%s\007' "$_payload"
	fi
}

if [ -n "${ZSH_VERSION:-}" ]; then
	autoload -Uz add-zsh-hook
	__neocode_preexec() {
		__neocode_emit_osc133 "C"
	}
	__neocode_precmd() {
		local _exit="$?"
		__neocode_emit_osc133 "D;${_exit}"
		__neocode_emit_osc133 "A"
	}
	add-zsh-hook preexec __neocode_preexec
	add-zsh-hook precmd __neocode_precmd
elif [ -n "${BASH_VERSION:-}" ]; then
	__neocode_last_cmd=""
	__neocode_preexec() {
		local _current="$BASH_COMMAND"
		if [ "${_current}" = "__neocode_precmd" ]; then
			return
		fi
		if [ "${_current}" = "${__neocode_last_cmd}" ]; then
			return
		fi
		__neocode_last_cmd="${_current}"
		__neocode_emit_osc133 "C"
	}
	trap '__neocode_preexec' DEBUG

	__neocode_precmd() {
		local _exit="$?"
		__neocode_emit_osc133 "D;${_exit}"
		__neocode_emit_osc133 "A"
	}
	if [ -n "${PROMPT_COMMAND:-}" ]; then
		PROMPT_COMMAND="__neocode_precmd;${PROMPT_COMMAND}"
	else
		PROMPT_COMMAND="__neocode_precmd"
	fi
fi
# <<< neocode shell integration <<<
`

// BuildShellInitScript 返回可直接 source 的 shell integration 脚本内容。
func BuildShellInitScript(shellOption string) string {
	trimmed := strings.TrimSpace(shellOption)
	if trimmed == "" {
		return shellInitScript
	}
	return fmt.Sprintf("# target shell: %s\n%s", trimmed, shellInitScript)
}
