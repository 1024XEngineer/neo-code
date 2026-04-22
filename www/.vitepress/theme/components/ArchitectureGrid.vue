<script setup lang="ts">
defineProps<{
	locale?: 'zh' | 'en'
}>()

const layeringCode = 'TUI -> Gateway -> Runtime -> Provider / Tool Manager'
</script>

<template>
  <div class="info-grid">
    <div class="info-card architecture-block">
      <h3>{{ locale === 'en' ? 'Main execution path' : '主执行链路' }}</h3>
      <CodePanel
        :code="layeringCode"
        language="text"
        :copy-label="locale === 'en' ? 'Copy' : '复制'"
        :copied-label="locale === 'en' ? 'Copied' : '已复制'"
        :failed-label="locale === 'en' ? 'Failed' : '复制失败'"
      />
      <p v-if="locale === 'en'">
        NeoCode keeps the terminal UI, gateway relay, runtime orchestration, and tool/provider integration on one explicit path.
      </p>
      <p v-else>
        NeoCode 把终端 UI、网关中继、Runtime 编排与 Tool/Provider 能力固定在一条清晰主链路里。
      </p>
    </div>

    <div class="info-card">
      <h3>{{ locale === 'en' ? 'What each layer owns' : '各层负责什么' }}</h3>
      <div class="info-lines" v-if="locale === 'en'">
        <p><code>internal/tui</code>: renders runtime events and slash-command interactions</p>
        <p><code>internal/gateway</code>: relays IPC and network requests, auth, and streaming events</p>
        <p><code>internal/runtime</code>: owns the ReAct loop, tool orchestration, session flow, and stop conditions</p>
        <p><code>internal/provider</code> / <code>internal/tools</code>: isolate model protocol differences and executable tools</p>
      </div>
      <div class="info-lines" v-else>
        <p><code>internal/tui</code>：渲染 runtime 事件、Slash 命令和会话界面</p>
        <p><code>internal/gateway</code>：负责 IPC / 网络请求接入、鉴权与流式事件中继</p>
        <p><code>internal/runtime</code>：负责 ReAct 主循环、tool 编排、会话流转与停止条件</p>
        <p><code>internal/provider</code> / <code>internal/tools</code>：隔离模型协议差异并统一可执行工具能力</p>
      </div>
    </div>
  </div>
</template>
