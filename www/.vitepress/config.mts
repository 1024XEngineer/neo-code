import { defineConfig } from "vitepress";

const isVercel = process.env.VERCEL === "1";
const isCFPages = process.env.CF_PAGES === "1";
const isRootDeploy = isVercel || isCFPages;

const repoUrl = "https://github.com/1024XEngineer/neo-code";
const base = isRootDeploy ? "/" : "/neo-code/";

let siteUrl = "https://1024xengineer.github.io/neo-code/";
if (isVercel) {
  siteUrl = `https://${process.env.VERCEL_URL || "neocode-docs.vercel.app"}/`;
} else if (isCFPages) {
  siteUrl = "https://neocode-docs.pages.dev/";
}

export default defineConfig({
  title: "NeoCode",
  description: "终端里的本地 AI 编码助手用户指南",
  lang: "zh-CN",
  
  // 使用动态计算的 base
  base: base,
  
  cleanUrls: true,
  lastUpdated: true,
  head: [
    ["link", { rel: "preconnect", href: "https://fonts.googleapis.com" }],
    ["link", { rel: "preconnect", href: "https://fonts.gstatic.com", crossorigin: "" }],
    [
      "link",
      {
        rel: "stylesheet",
        href: "https://fonts.googleapis.com/css2?family=IBM+Plex+Sans:wght@400;500;600;700&family=IBM+Plex+Mono:wght@400;500;600;700&display=swap",
      },
    ],
    ["meta", { name: "theme-color", content: "#f7f7f4" }],
    ["meta", { property: "og:title", content: "NeoCode 用户指南" }],
    [
      "meta",
      {
        property: "og:description",
        content: "围绕安装、配置、日常使用、扩展能力和排障整理的 NeoCode 用户指南。",
      },
    ],
    ["meta", { property: "og:type", content: "website" }],
    ["meta", { property: "og:image", content: `${base}brand/neocode-mark.png` }],
    ["meta", { name: "twitter:card", content: "summary" }],
    // Favicon 路径随 base 变化
    ["link", { rel: "icon", href: `${base}brand/neocode-mark.png` }],
  ],
  markdown: {
    config(md) {
      md.linkify.set({ fuzzyLink: false });
    },
  },
  themeConfig: {
    logo: "/brand/neocode-mark.png", 
    siteTitle: "NeoCode",
    search: {
      provider: "local",
    },
    socialLinks: [{ icon: "github", link: repoUrl }],
  },
  locales: {
    root: {
      label: "简体中文",
      lang: "zh-CN",
      link: "/",
      themeConfig: {
        nav: [
          { text: "快速开始", link: "/guide/" },
          { text: "进阶与设计", link: "/reference/" },
        ],
        sidebar: {
          "/guide/": [
            {
              text: "快速开始",
              items: [
                { text: "总览", link: "/guide/" },
                { text: "安装与首次运行", link: "/guide/install" },
                { text: "使用示例", link: "/guide/examples" },
              ],
            },
            {
              text: "核心概念",
              items: [
                { text: "Slash 指令", link: "/guide/slash-commands" },
                { text: "AGENTS.md 项目规则", link: "/guide/agents-md" },
                { text: "会话、上下文与工作区", link: "/guide/context-session-workspace" },
                { text: "能力选择指南", link: "/guide/capability-choice" },
              ],
            },
            {
              text: "日常使用",
              items: [
                { text: "日常使用", link: "/guide/daily-use" },
                { text: "配置指南", link: "/guide/configuration" },
                { text: "工具与权限", link: "/guide/tools-permissions" },
              ],
            },
            {
              text: "扩展能力",
              items: [
                { text: "MCP 工具接入", link: "/guide/mcp" },
                { text: "Skills 使用", link: "/guide/skills" },
              ],
            },
            {
              text: "质量与协作",
              items: [
                { text: "排障与常见问题", link: "/guide/troubleshooting" },
                { text: "升级与版本检查", link: "/guide/update" },
              ],
            },
          ],
          "/reference/": [
            {
              text: "深入阅读",
              items: [
                { text: "文档导航", link: "/reference/" },
                { text: "Gateway 集成参考", link: "/reference/gateway" },
              ],
            },
          ],
          "/": [
            {
              text: "快速导航",
              items: [
                { text: "快速开始", link: "/guide/" },
                { text: "安装与首次运行", link: "/guide/install" },
                { text: "Slash 指令", link: "/guide/slash-commands" },
                { text: "日常使用", link: "/guide/daily-use" },
                { text: "排障", link: "/guide/troubleshooting" },
              ],
            },
          ],
        },
        outline: false,
        docFooter: {
          prev: "上一页",
          next: "下一页",
        },
        editLink: {
          pattern: `${repoUrl}/edit/main/www/:path`,
          text: "在 GitHub 上编辑此页",
        },
        footer: {
          message: "围绕安装、配置、日常使用、扩展能力和排障整理的 NeoCode 用户指南。",
          copyright: "MIT Licensed",
        },
        returnToTopLabel: "返回顶部",
        sidebarMenuLabel: "菜单",
        darkModeSwitchLabel: "主题",
        lightModeSwitchTitle: "切换到浅色模式",
        darkModeSwitchTitle: "切换到深色模式",
      },
    },
    en: {
      label: "English",
      lang: "en-US",
      link: "/en/",
      description:
        "A compact NeoCode docs entrypoint focused on current, verifiable behavior.",
      themeConfig: {
        nav: [
          { text: "Getting Started", link: "/en/guide/" },
          { text: "Reference", link: "/reference/" },
        ],
        sidebar: {
          "/en/guide/": [
            {
              text: "Getting Started",
              items: [
                { text: "Getting Started", link: "/en/guide/" },
                { text: "Install & First Run", link: "/en/guide/install" },
                { text: "Usage Examples", link: "/en/guide/examples" },
              ],
            },
            {
              text: "Core Concepts",
              items: [
                { text: "Slash Commands", link: "/en/guide/slash-commands" },
                { text: "AGENTS.md Project Rules", link: "/en/guide/agents-md" },
                { text: "Sessions, Context, Workspace", link: "/en/guide/context-session-workspace" },
                { text: "Capability Guide", link: "/en/guide/capability-choice" },
              ],
            },
            {
              text: "Daily Use",
              items: [
                { text: "Daily Use", link: "/en/guide/daily-use" },
                { text: "Configuration", link: "/en/guide/configuration" },
                { text: "Tools & Permissions", link: "/en/guide/tools-permissions" },
              ],
            },
            {
              text: "Extensions",
              items: [
                { text: "MCP Tools", link: "/en/guide/mcp" },
                { text: "Skills", link: "/en/guide/skills" },
              ],
            },
            {
              text: "Quality & Ops",
              items: [
                { text: "Troubleshooting", link: "/en/guide/troubleshooting" },
                { text: "Update & Version", link: "/en/guide/update" },
              ],
            },
          ],
          "/reference/": [
            {
              text: "Reference",
              items: [
                { text: "Documentation Index", link: "/reference/" },
                { text: "Gateway Reference", link: "/reference/gateway" },
              ],
            },
          ],
        },
        outline: false,
        docFooter: {
          prev: "Previous page",
          next: "Next page",
        },
        editLink: {
          pattern: `${repoUrl}/edit/main/www/:path`,
          text: "Edit this page on GitHub",
        },
        footer: {
          message:
            "A compact docs entrypoint built from NeoCode's current implementation.",
          copyright: "MIT Licensed",
        },
        returnToTopLabel: "Back to top",
        sidebarMenuLabel: "Menu",
        darkModeSwitchLabel: "Appearance",
        lightModeSwitchTitle: "Switch to light theme",
        darkModeSwitchTitle: "Switch to dark theme",
      },
    },
  },
  sitemap: {
    hostname: siteUrl,
  },

});
