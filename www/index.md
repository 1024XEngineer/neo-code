---
layout: home

hero:
  name: NeoCode
  text: 在终端里运行的 AI 编码助手
  tagline: 安装即用，无需云端，代码留在本地。
  image:
    src: /brand/neocode-mark.png
    alt: NeoCode
  actions:
    - theme: brand
      text: 开始使用
      link: /guide/
    - theme: alt
      text: 首次上手
      link: /guide/quick-start
    - theme: alt
      text: GitHub
      link: https://github.com/1024XEngineer/neo-code

features:
  - title: 完全本地
    details: 模型调用走本地配置，不经过任何第三方中转服务器，数据留在你的机器上。
  - title: 终端原生
    details: TUI 界面，无需浏览器，和你的 shell 工作流无缝集成，直接在终端中对话和操作。
  - title: 多模型支持
    details: 内置 OpenAI、Gemini、OpenLL、Qiniu、ModelScope 等多个模型服务，通过配置随时切换，无需改代码。
  - title: 安全优先
    details: 工具权限审批、Bash 语义治理、文件系统沙箱、Gateway Origin 校验，多层安全机制保障操作可控。
  - title: 可扩展能力层
    details: Skills 提示层、MCP stdio 接入、子代理编排、验收验证器，不改变主执行链路即可扩展能力。
---

<section class="home-section compact">
  <p class="eyebrow">Quick Start</p>
  <h2>先安装，再开始用</h2>
  <QuickStartCards locale="zh" />
</section>

<section class="home-section">
  <p class="eyebrow">Guide</p>
  <h2>常用入口</h2>
  <div class="doc-grid">
    <a class="doc-card" href="/neo-code/guide/why-neocode">
      <strong>为什么选择 NeoCode</strong>
      <span>本地运行、安全优先、可扩展能力层的差异化优势。</span>
    </a>
    <a class="doc-card" href="/neo-code/guide/install">
      <strong>安装与运行</strong>
      <span>安装脚本、源码运行和环境要求。</span>
    </a>
    <a class="doc-card" href="/neo-code/guide/quick-start">
      <strong>首次上手</strong>
      <span>第一次提问、常用 Slash 命令和 Provider / Model 切换。</span>
    </a>
    <a class="doc-card" href="/neo-code/guide/install">
      <strong>配置环境变量</strong>
      <span>设置 <code>OPENAI_API_KEY</code>、<code>GEMINI_API_KEY</code> 等 API Key 环境变量。</span>
    </a>
    <a class="doc-card" href="/neo-code/guide/configuration">
      <strong>配置</strong>
      <span><code>config.yaml</code>、custom provider 和环境变量。</span>
    </a>
    <a class="doc-card" href="/neo-code/guide/workspace-session">
      <strong>工作区与会话</strong>
      <span><code>--workdir</code>、<code>/cwd</code>、会话切换和上下文压缩。</span>
    </a>
    <a class="doc-card" href="/neo-code/guide/memo-skills">
      <strong>记忆与 Skills</strong>
      <span><code>/memo</code>、<code>/remember</code>、<code>/forget</code> 和 Skills。</span>
    </a>
    <a class="doc-card" href="/neo-code/guide/gateway">
      <strong>Gateway 使用</strong>
      <span><code>neocode gateway</code>、<code>url-dispatch</code> 和网络访问面。</span>
    </a>
  </div>
</section>
