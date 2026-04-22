import { defineConfig } from 'vitepress'

const repoUrl = 'https://github.com/1024XEngineer/neo-code'

export default defineConfig({
  title: 'NeoCode',
  description: '基于 Go + Bubble Tea 的本地 Coding Agent',
  lang: 'zh-CN',
  base: '/neo-code/',
  cleanUrls: true,
  lastUpdated: true,
  head: [
    ['meta', { name: 'theme-color', content: '#0f766e' }],
    ['meta', { property: 'og:title', content: 'NeoCode' }],
    [
      'meta',
      {
        property: 'og:description',
        content: '一个围绕 ReAct 主链路构建的本地 Go + Bubble Tea Coding Agent。'
      }
    ],
    ['meta', { property: 'og:type', content: 'website' }],
    ['link', { rel: 'icon', href: '/neo-code/brand/neocode-mark.svg' }]
  ],
  themeConfig: {
    logo: '/brand/neocode-mark.svg',
    siteTitle: 'NeoCode',
    search: {
      provider: 'local'
    },
    socialLinks: [
      { icon: 'github', link: repoUrl }
    ]
  },
  locales: {
    root: {
      label: '简体中文',
      lang: 'zh-CN',
      link: '/',
      themeConfig: {
        nav: [
          { text: '文档', link: '/docs/quick-start' },
          { text: 'GitHub', link: repoUrl }
        ],
        sidebar: {
          '/docs/': [
            { text: '快速开始', link: '/docs/quick-start' },
            {
              text: '使用指南',
              items: [
                { text: '配置', link: '/docs/guides/configuration' },
                { text: '切换模型', link: '/docs/guides/providers' },
                { text: 'MCP 配置', link: '/docs/guides/mcp' },
                { text: '更新升级', link: '/docs/guides/update' }
              ]
            },
            {
              text: '参考',
              items: [
                { text: '架构概览', link: '/docs/reference/architecture' }
              ]
            }
          ]
        },
        footer: {
          message: '围绕可验证主链路构建的本地 Coding Agent。',
          copyright: 'MIT Licensed'
        },
        outline: {
          label: '本页目录'
        },
        returnToTopLabel: '返回顶部',
        sidebarMenuLabel: '菜单',
        darkModeSwitchLabel: '主题',
        lightModeSwitchTitle: '切换到浅色模式',
        darkModeSwitchTitle: '切换到深色模式'
      }
    },
    en: {
      label: 'English',
      lang: 'en-US',
      link: '/en/',
      description: 'A local Go + Bubble Tea coding agent built around a verifiable ReAct loop.',
      themeConfig: {
        nav: [
          { text: 'Docs', link: '/en/docs/quick-start' },
          { text: 'GitHub', link: repoUrl }
        ],
        sidebar: {
          '/en/docs/': [
            { text: 'Quick Start', link: '/en/docs/quick-start' },
            {
              text: 'Guides',
              items: [
                { text: 'Configuration', link: '/en/docs/guides/configuration' },
                { text: 'Switching Models', link: '/en/docs/guides/providers' },
                { text: 'MCP Configuration', link: '/en/docs/guides/mcp' },
                { text: 'Updating', link: '/en/docs/guides/update' }
              ]
            },
            {
              text: 'Reference',
              items: [
                { text: 'Architecture', link: '/en/docs/reference/architecture' }
              ]
            }
          ]
        },
        footer: {
          message: 'A local coding agent shaped around a verifiable execution loop.',
          copyright: 'MIT Licensed'
        },
        outline: {
          label: 'On this page'
        },
        returnToTopLabel: 'Back to top',
        sidebarMenuLabel: 'Menu',
        darkModeSwitchLabel: 'Appearance',
        lightModeSwitchTitle: 'Switch to light theme',
        darkModeSwitchTitle: 'Switch to dark theme'
      }
    }
  },
  markdown: {
    config(md) {
      md.linkify.set({ fuzzyLink: false })
    }
  }
})
