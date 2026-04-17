# 更新与升级

## 自动检测

- `neocode` 启动时会在后台静默检测最新版本（默认 3 秒超时）。
- 为避免干扰 Bubble Tea TUI 交互，更新提示会在应用退出、终端屏幕恢复后输出。
- `url-dispatch` 与 `update` 子命令会跳过该检测流程。

## 手动升级

使用以下命令升级到最新稳定版：

```bash
neocode update
```

如需包含预发布版本：

```bash
neocode update --prerelease
```

## 版本来源

- 发布构建会通过 `ldflags` 注入版本号到 `internal/version.Version`。
- 本地开发构建默认版本为 `dev`。
