---
layout: home

hero:
  name: NeoCode
  text: An AI coding agent that runs in your terminal
  tagline: Install and run locally. No cloud relay. Your code stays on your machine.
  image:
    src: /brand/neocode-mark.svg
    alt: NeoCode
  actions:
    - theme: brand
      text: Quick Start
      link: /en/docs/quick-start
    - theme: alt
      text: GitHub
      link: https://github.com/1024XEngineer/neo-code
    - theme: alt
      text: 中文
      link: /

features:
  - title: Fully local
    details: Model calls go through your local configuration. No third-party relay, no data leaving your machine.
  - title: Terminal-native
    details: TUI interface. No browser needed. Works directly inside your shell workflow.
  - title: Multi-model support
    details: One config file manages OpenAI, Gemini, Ollama, and more. Switch between models without changing code.
---

<section class="home-section quickstart">
  <p class="eyebrow">Quick Start</p>
  <h2>Pick an entry point and get started</h2>

  <p>Use the install script for the fastest path to a working setup. Run from source if you want to explore the code.</p>
  <QuickStartCards locale="en" />
</section>
