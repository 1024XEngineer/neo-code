<script setup lang="ts">
defineProps<{
  locale?: 'zh' | 'en'
}>()
</script>

<template>
  <div class="quickstart-grid" v-if="locale === 'en'">
    <article class="quickstart-card">
      <p class="quickstart-kicker">Try Now</p>
      <h3>One-line install</h3>
      <p>Best for visitors who want the fastest path to a working local setup.</p>
      <p><strong>macOS / Linux</strong></p>
      <pre><code>curl -fsSL https://raw.githubusercontent.com/1024XEngineer/neo-code/main/scripts/install.sh | bash</code></pre>
      <p><strong>Windows PowerShell</strong></p>
      <pre><code>irm https://raw.githubusercontent.com/1024XEngineer/neo-code/main/scripts/install.ps1 | iex</code></pre>
    </article>

    <article class="quickstart-card">
      <p class="quickstart-kicker">From Source</p>
      <h3>Run from source</h3>
      <p>Best for contributors who want to inspect the code, debug behavior, or wire new features in directly.</p>
      <pre><code>git clone https://github.com/1024XEngineer/neo-code.git
cd neo-code
go run ./cmd/neocode</code></pre>
      <p><strong>Set API keys</strong></p>
      <pre><code>export OPENAI_API_KEY="your_key_here"
export GEMINI_API_KEY="your_key_here"
export AI_API_KEY="your_key_here"
export QINIU_API_KEY="your_key_here"</code></pre>
    </article>

    <article class="quickstart-card">
      <p class="quickstart-kicker">Next Step</p>
      <h3>More setup</h3>
      <p>Go deeper when you need workspace isolation, gateway mode, or the full install and upgrade guide.</p>
      <div class="quickstart-links">
        <p>Workspace isolation: <code>--workdir</code></p>
        <p>Gateway mode: <code>--runtime-mode gateway</code></p>
        <p><a href="/neo-code/en/docs/guides/configuration">Configuration guide</a></p>
        <p><a href="/neo-code/en/docs/">Docs</a></p>
      </div>
    </article>
  </div>

  <div class="quickstart-grid" v-else>
    <article class="quickstart-card">
      <p class="quickstart-kicker">Try Now</p>
      <h3>一键安装</h3>
      <p>适合想先试用 NeoCode 的访客，直接使用 README 中维护的安装脚本。</p>
      <p><strong>macOS / Linux</strong></p>
      <pre><code>curl -fsSL https://raw.githubusercontent.com/1024XEngineer/neo-code/main/scripts/install.sh | bash</code></pre>
      <p><strong>Windows PowerShell</strong></p>
      <pre><code>irm https://raw.githubusercontent.com/1024XEngineer/neo-code/main/scripts/install.ps1 | iex</code></pre>
    </article>

    <article class="quickstart-card">
      <p class="quickstart-kicker">From Source</p>
      <h3>从源码运行</h3>
      <p>适合准备阅读代码、调试功能或直接参与开发的使用方式。</p>
      <pre><code>git clone https://github.com/1024XEngineer/neo-code.git
cd neo-code
go run ./cmd/neocode</code></pre>
      <p><strong>配置 API Key</strong></p>
      <pre><code>export OPENAI_API_KEY="your_key_here"
export GEMINI_API_KEY="your_key_here"
export AI_API_KEY="your_key_here"
export QINIU_API_KEY="your_key_here"</code></pre>
    </article>

    <article class="quickstart-card">
      <p class="quickstart-kicker">Next Step</p>
      <h3>更多配置</h3>
      <p>当你需要工作区隔离、网关模式、升级说明或完整命令列表时，再继续深入。</p>
      <div class="quickstart-links">
        <p>工作区隔离：<code>--workdir</code></p>
        <p>网关模式：<code>--runtime-mode gateway</code></p>
        <p><a href="/neo-code/docs/guides/configuration">配置指南</a></p>
        <p><a href="/neo-code/docs/">文档</a></p>
      </div>
    </article>
  </div>
</template>
