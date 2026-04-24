import { defineConfig } from "vitepress";

// --- 环境变量检测逻辑 ---
const isVercel = process.env.VERCEL === "1";
const isCFPages = process.env.CF_PAGES === "1"; 

const isRootDeploy = isVercel || isCFPages;

const repoUrl = "https://github.com/1024XEngineer/neo-code";
const docsBase = `${repoUrl}/blob/main/docs`;

// 核心修改 1：动态基础路径
const base = isRootDeploy ? "/" : "/neo-code/";

// 👇👇👇 新增这段调试日志 👇👇👇
console.log("====== VitePress Build Info ======");
console.log("VERCEL Env:", process.env.VERCEL);
console.log("CF_PAGES Env:", process.env.CF_PAGES);
console.log("Final Base Path:", base);
console.log("==================================");

// 核心修改 2：动态站点 URL
let siteUrl = "https://1024xengineer.github.io/neo-code/";
if (isVercel) {
  siteUrl = `https://${process.env.VERCEL_URL || 'neocode-docs.vercel.app'}/`;
} else if (isCFPages) {
  siteUrl = "https://neocode-docs.pages.dev/"; 
}

const brandImageUrl = `${siteUrl}brand/neocode-mark.png`;

export default defineConfig({
  title: "NeoCode",
  description: "基于 Go + Bubble Tea 的本地 Coding Agent 用户指南",
  lang: "zh-CN",
  
  // 使用动态计算的 base
  base: base,
  
  cleanUrls: true,
  lastUpdated: true,
  head: [
    ["meta", { name: "theme-color", content: "#090B1A" }],
    ["meta", { property: "og:title", content: "NeoCode 用户指南" }],
    [
      "meta",
      {
        property: "og:description",
        content: "围绕真实命令、配置与 Gateway 使用场景整理的 NeoCode 用户指导网站。",
      },
    ],
    ["meta", { property: "og:type", content: "website" }],
    [
      "meta",
      {
        property: "og:image",
        content: brandImageUrl,
      },
    ],
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
          { text: "开始使用", link: "/guide/" },
          { text: "配置", link: "/guide/configuration" },
          { text: "命令与会话", link: "/guide/quick-start" },
          { text: "Gateway", link: "/guide/gateway" },
          { text: "深入阅读", link: "/reference/" },
          { text: "GitHub", link: repoUrl },
        ],
        sidebar: {
          "/guide/": [
            {
              text: "开始使用",
              items: [
                { text: "总览", link: "/guide/" },
                { text: "NeoCode 是什么", link: "/guide/getting-started" },
                { text: "安装与运行", link: "/guide/install" },
                { text: "首次上手", link: "/guide/quick-start" },
              ],
            },
            {
              text: "日常使用",
              items: [
                { text: "配置入口", link: "/guide/configuration" },
                { text: "工作区与会话", link: "/guide/workspace-session" },
                { text: "记忆与 Skills", link: "/guide/memo-skills" },
                { text: "Gateway 与 URL Dispatch", link: "/guide/gateway" },
                { text: "升级与版本检查", link: "/guide/update" },
              ],
            },
          ],
          "/reference/": [
            {
              text: "深入阅读",
              items: [
                { text: "文档导航", link: "/reference/" },
                { text: "旧入口兼容页", link: "/docs/" },
              ],
            },
          ],
          "/docs/": [
            {
              text: "文档入口",
              items: [
                { text: "总览", link: "/docs/" },
                { text: "开始使用", link: "/guide/" },
                { text: "深入阅读", link: "/reference/" },
              ],
            },
          ],
          "/": [
            {
              text: "快速导航",
              items: [
                { text: "开始使用", link: "/guide/" },
                { text: "安装与运行", link: "/guide/install" },
                { text: "首次上手", link: "/guide/quick-start" },
                { text: "配置入口", link: "/guide/configuration" },
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
          message: "围绕真实命令、配置与主链路整理的 NeoCode 用户指南。",
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
          { text: "Home", link: "/en/" },
          { text: "Docs", link: "/en/docs/" },
          { text: "GitHub", link: repoUrl },
        ],
        sidebar: {
          "/en/docs/": [
            {
              text: "Overview",
              items: [
                { text: "Docs Index", link: "/en/docs/" },
                { text: "Chinese Guide", link: "/guide/" },
                { text: "Architecture Notes", link: "/reference/" },
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
  vite: {
    define: {
      __NEOCODE_DOCS_BASE__: JSON.stringify(docsBase),
    },
  },
});
