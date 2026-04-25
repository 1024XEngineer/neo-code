---
title: 升级与版本检查
description: NeoCode 当前的静默版本检查与 update 子命令说明。
---

# 升级与版本检查

## 自动检测

当前实现里，`neocode` 启动时会在后台静默检测最新版本：

- 默认超时为 3 秒
- 为了不打断 Bubble Tea TUI，提示会在应用退出、终端屏幕恢复后输出
- `url-dispatch` 和 `update` 子命令会跳过该检测

## 查看版本

查看当前版本并检查最新稳定版：

```bash
neocode version
```

包含预发布版本：

```bash
neocode version --prerelease
```

当远端"语义最新版本"在当前平台不可安装时，`version` 会同时给出"可安装的最高版本"升级提示。

## 手动升级

升级到最新稳定版：

```bash
neocode update
```

包含预发布版本：

```bash
neocode update --prerelease
```

## 版本来源

- 发布构建通过 `ldflags` 注入版本号到 `internal/version.Version`
- 本地开发构建默认版本为 `dev`

如果你是在源码模式下运行，看见 `dev` 是符合当前实现的。
