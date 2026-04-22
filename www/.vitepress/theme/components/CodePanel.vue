<script setup lang="ts">
import { computed, ref } from 'vue'

const props = withDefaults(
	defineProps<{
		code: string
		language?: string
		label?: string
		copyLabel?: string
		copiedLabel?: string
		failedLabel?: string
	}>(),
	{
		language: 'text',
		label: '',
		copyLabel: '复制',
		copiedLabel: '已复制',
		failedLabel: '复制失败'
	}
)

const copyState = ref<'idle' | 'success' | 'error'>('idle')
let resetTimer: number | undefined

const buttonText = computed(() => {
	if (copyState.value === 'success') {
		return props.copiedLabel
	}
	if (copyState.value === 'error') {
		return props.failedLabel
	}
	return props.copyLabel
})

// copyCode 负责复制代码文本，并在按钮上反馈当前结果。
async function copyCode() {
	clearResetTimer()
	try {
		if (typeof navigator !== 'undefined' && navigator.clipboard?.writeText) {
			await navigator.clipboard.writeText(props.code)
		} else {
			fallbackCopy(props.code)
		}
		copyState.value = 'success'
	} catch {
		try {
			fallbackCopy(props.code)
			copyState.value = 'success'
		} catch {
			copyState.value = 'error'
		}
	}
	scheduleReset()
}

// fallbackCopy 在 Clipboard API 不可用时退回到 document.execCommand。
function fallbackCopy(text: string) {
	const textarea = document.createElement('textarea')
	textarea.value = text
	textarea.setAttribute('readonly', 'true')
	textarea.style.position = 'fixed'
	textarea.style.opacity = '0'
	document.body.appendChild(textarea)
	textarea.select()
	document.execCommand('copy')
	document.body.removeChild(textarea)
}

// scheduleReset 用于在提示短暂展示后恢复默认文案。
function scheduleReset() {
	resetTimer = window.setTimeout(() => {
		copyState.value = 'idle'
		resetTimer = undefined
	}, 1800)
}

// clearResetTimer 在重复点击时取消上一次的恢复计时。
function clearResetTimer() {
	if (resetTimer !== undefined) {
		window.clearTimeout(resetTimer)
		resetTimer = undefined
	}
}
</script>

<template>
  <div class="code-panel">
    <div class="code-panel__toolbar">
      <span class="code-panel__label" v-if="label">{{ label }}</span>
      <button
        class="code-panel__copy"
        :class="`is-${copyState}`"
        type="button"
        @click="copyCode"
      >
        {{ buttonText }}
      </button>
    </div>
    <pre :class="`language-${language}`"><code>{{ code }}</code></pre>
  </div>
</template>
