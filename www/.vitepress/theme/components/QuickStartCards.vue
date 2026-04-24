<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{
	locale?: 'zh' | 'en'
}>()

const installUnix = 'curl -fsSL https://raw.githubusercontent.com/1024XEngineer/neo-code/main/scripts/install.sh | bash'
const installWindows = 'irm https://raw.githubusercontent.com/1024XEngineer/neo-code/main/scripts/install.ps1 | iex'
const fromSource = `git clone https://github.com/1024XEngineer/neo-code.git
cd neo-code
go run ./cmd/neocode`

type QuickStartCard = {
	key: string
	kicker: string
	title: string
	description: string
	panels: Array<{
		language: 'bash' | 'powershell'
		label: string
		code: string
	}>
	links: Array<{
		text: string
		code: string
	}>
}

const quickStartCards: Record<'zh' | 'en', QuickStartCard[]> = {
	zh: [
		{
			key: 'install',
			kicker: '安装脚本',
			title: '安装 NeoCode',
			description: '直接使用仓库里维护的安装脚本，适合先把本地环境跑起来。',
			panels: [
				{ language: 'bash', label: 'macOS / Linux', code: installUnix },
				{ language: 'powershell', label: 'Windows PowerShell', code: installWindows },
			],
			links: [{ text: '安装完成后运行：', code: 'neocode' }],
		},
		{
			key: 'source',
			kicker: '源码运行',
			title: '从源码运行',
			description: '准备调试、看源码或参与开发时，直接运行当前仓库即可。',
			panels: [{ language: 'bash', label: 'Clone and run', code: fromSource }],
			links: [
				{ text: '网关进程：', code: 'go run ./cmd/neocode gateway' },
				{ text: '会话工作区：', code: '--workdir /path/to/workspace' },
			],
		},
	],
	en: [
		{
			key: 'install',
			kicker: 'Install',
			title: 'Install with one command',
			description: 'Use the same install scripts maintained in the repository README.',
			panels: [
				{ language: 'bash', label: 'macOS / Linux', code: installUnix },
				{ language: 'powershell', label: 'Windows PowerShell', code: installWindows },
			],
			links: [{ text: 'Then run: ', code: 'neocode' }],
		},
		{
			key: 'source',
			kicker: 'Source',
			title: 'Run the current codebase',
			description: 'Best when you want to inspect behavior, debug issues, or contribute changes.',
			panels: [{ language: 'bash', label: 'Clone and run', code: fromSource }],
			links: [
				{ text: 'Gateway command: ', code: 'go run ./cmd/neocode gateway' },
				{ text: 'Workspace isolation: ', code: '--workdir /path/to/workspace' },
			],
		},
	],
}

const currentLocale = computed<'zh' | 'en'>(() => (props.locale === 'en' ? 'en' : 'zh'))
const currentCards = computed(() => quickStartCards[currentLocale.value])
</script>

<template>
	<div class="quickstart-grid">
		<article v-for="card in currentCards" :key="card.key" class="quickstart-card">
			<p class="quickstart-kicker">{{ card.kicker }}</p>
			<h3>{{ card.title }}</h3>
			<p>{{ card.description }}</p>
			<CodePanel
				v-for="panel in card.panels"
				:key="`${card.key}-${panel.label}`"
				:language="panel.language"
				:label="panel.label"
				:code="panel.code"
			/>
			<div class="quickstart-links">
				<p v-for="link in card.links" :key="`${card.key}-${link.code}`">
					{{ link.text }}<code>{{ link.code }}</code>
				</p>
			</div>
		</article>
	</div>
</template>
