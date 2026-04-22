<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{
	locale?: 'zh' | 'en'
}>()

const isEnglish = computed(() => props.locale === 'en')

const installUnix = 'curl -fsSL https://raw.githubusercontent.com/1024XEngineer/neo-code/main/scripts/install.sh | bash'
const installWindows = 'irm https://raw.githubusercontent.com/1024XEngineer/neo-code/main/scripts/install.ps1 | iex'
const fromSource = `git clone https://github.com/1024XEngineer/neo-code.git
cd neo-code
go run ./cmd/neocode`
const envUnix = `export OPENAI_API_KEY="your_key_here"
export GEMINI_API_KEY="your_key_here"
export AI_API_KEY="your_key_here"
export QINIU_API_KEY="your_key_here"`
const envWindows = `$env:OPENAI_API_KEY = "your_key_here"
$env:GEMINI_API_KEY = "your_key_here"
$env:AI_API_KEY = "your_key_here"
$env:QINIU_API_KEY = "your_key_here"`
</script>

<template>
  <div class="quickstart-grid" v-if="isEnglish">
    <article class="quickstart-card">
      <p class="quickstart-kicker">Try Now</p>
      <h3>Install with one command</h3>
      <p>Use the same install scripts maintained in the repository README.</p>
      <CodePanel language="bash" label="macOS / Linux" :code="installUnix" />
      <CodePanel language="powershell" label="Windows PowerShell" :code="installWindows" />
    </article>

    <article class="quickstart-card">
      <p class="quickstart-kicker">From Source</p>
      <h3>Run the current codebase</h3>
      <p>Best when you want to inspect behavior, debug issues, or contribute changes.</p>
      <CodePanel language="bash" label="Clone and run" :code="fromSource" />
    </article>

    <article class="quickstart-card">
      <p class="quickstart-kicker">First Run</p>
      <h3>Set provider credentials</h3>
      <p>NeoCode reads API keys from environment variables instead of storing them in config files.</p>
      <CodePanel language="bash" label="Shell" :code="envUnix" />
      <CodePanel language="powershell" label="PowerShell" :code="envWindows" />
      <div class="quickstart-links">
        <p>Workspace isolation: <code>--workdir</code></p>
        <p>Gateway mode: <code>--runtime-mode gateway</code></p>
        <p><a href="/neo-code/guide/quick-start">Chinese quick start</a></p>
        <p><a href="/neo-code/en/docs/">English docs index</a></p>
      </div>
    </article>
  </div>

  <div class="quickstart-grid" v-else>
    <article class="quickstart-card">
      <p class="quickstart-kicker">Step 1</p>
      <h3>安装 NeoCode</h3>
      <p>直接使用仓库里维护的安装脚本，适合先把本地环境跑起来。</p>
      <CodePanel language="bash" label="macOS / Linux" :code="installUnix" />
      <CodePanel language="powershell" label="Windows PowerShell" :code="installWindows" />
    </article>

    <article class="quickstart-card">
      <p class="quickstart-kicker">Step 2</p>
      <h3>从源码运行</h3>
      <p>准备调试、看源码或参与开发时，直接运行当前仓库即可。</p>
      <CodePanel language="bash" label="Clone and run" :code="fromSource" />
      <div class="quickstart-links">
        <p>会话工作区：<code>--workdir /path/to/workspace</code></p>
        <p>网关进程：<code>go run ./cmd/neocode gateway</code></p>
      </div>
    </article>

    <article class="quickstart-card">
      <p class="quickstart-kicker">Step 3</p>
      <h3>配置 API Key</h3>
      <p>当前内置 provider 包括 <code>openai</code>、<code>gemini</code>、<code>openll</code> 和 <code>qiniu</code>。</p>
      <CodePanel language="bash" label="Shell" :code="envUnix" />
      <CodePanel language="powershell" label="PowerShell" :code="envWindows" />
      <div class="quickstart-links">
        <p>工作区隔离：<code>--workdir</code></p>
        <p>网关模式：<code>--runtime-mode gateway</code></p>
        <p><a href="/neo-code/guide/quick-start">继续看首次上手</a></p>
        <p><a href="/neo-code/guide/gateway">查看 Gateway 用法</a></p>
      </div>
    </article>
  </div>
</template>
