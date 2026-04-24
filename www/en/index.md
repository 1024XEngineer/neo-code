---
layout: home

hero:
  name: NeoCode
  text: An AI coding agent that runs in your terminal
  tagline: Install and run locally. No cloud relay. Your code stays on your machine.
  image:
    src: /brand/neocode-mark.png
    alt: NeoCode
  actions:
    - theme: brand
      text: Docs Index
      link: /en/docs/
    - theme: alt
      text: 中文用户指南
      link: /guide/
    - theme: alt
      text: GitHub
      link: https://github.com/1024XEngineer/neo-code

features:
  - title: Fully local
    details: Model calls go through your local configuration. No third-party relay, no data leaving your machine.
  - title: Terminal-native
    details: TUI interface. No browser needed. Works directly inside your shell workflow.
  - title: Multi-model support
    details: Built-in support for OpenAI, Gemini, OpenLL, Qiniu and more. Switch between models without changing code.
---

<section class="home-section compact">
  <p class="eyebrow">Quick Start</p>
  <h2>Run NeoCode, then choose the guide depth you need</h2>
  <QuickStartCards locale="en" />
</section>

<section class="home-section">
  <p class="eyebrow">Entry Points</p>
  <h2>Where to go next</h2>
  <div class="doc-grid">
    <a class="doc-card" href="/neo-code/en/docs/">
      <strong>English docs index</strong>
      <span>A short overview with links into the Chinese guide and repository docs.</span>
    </a>
    <a class="doc-card" href="/neo-code/guide/install">
      <strong>Set credentials</strong>
      <span>Configure <code>OPENAI_API_KEY</code>, <code>GEMINI_API_KEY</code>, and other provider API keys.</span>
    </a>
    <a class="doc-card" href="/neo-code/guide/">
      <strong>Chinese guide</strong>
      <span>The primary user guide covering install, quick start, configuration, sessions, memo, skills, and Gateway usage.</span>
    </a>
    <a class="doc-card" href="/neo-code/reference/">
      <strong>Reference index</strong>
      <span>Curated links to runtime, gateway, configuration, and skills design notes.</span>
    </a>
  </div>
</section>
