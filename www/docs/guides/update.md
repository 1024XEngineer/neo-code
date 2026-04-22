# 更新升级

## 自动检测

NeoCode 启动时会在后台静默检测最新版本（默认 3 秒超时）。更新提示会在应用退出后输出，不干扰 TUI 交互。

## 手动升级

升级到最新稳定版：

```bash
neocode update
```

包含预发布版本：

```bash
neocode update --prerelease
```

## 版本说明

- 发布构建通过 `ldflags` 注入版本号
- 本地开发构建默认版本为 `dev`
