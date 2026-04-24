# 更新与版本检查

## 自动检查
- `neocode` 启动时会在后台静默检查最新稳定版本（默认 3 秒超时）。
- 为避免干扰 Bubble Tea TUI 交互，更新提示会在应用退出、终端屏幕恢复后输出。
- `url-dispatch`、`update`、`version` 子命令会跳过该静默检查，避免重复探测。

## 查询版本

查看当前版本并探测远端最新版本：

```bash
neocode version
```

包含预发布版本一起比较：

```bash
neocode version --prerelease
```

行为说明：
- 始终输出当前版本。
- 探测成功时输出“最新版本 + 比较结果”。
- 探测失败时输出失败原因，但命令仍返回成功退出码，方便脚本场景继续执行。

## 手动升级

升级到最新稳定版本：

```bash
neocode update
```

包含预发布版本：

```bash
neocode update --prerelease
```

更新命令在平台资产匹配失败时会输出可诊断信息，例如：
- `os`
- `arch`
- `expected-pattern`
- `available-assets-count`
- `candidate-assets`（最多展示前 10 个，单项最长 120 字符）

## 双产物安装建议

1. Full 模式：安装 `neocode`。
2. Gateway 模式：安装 `neocode-gateway`。

安装脚本支持 flavor：

```bash
bash ./scripts/install.sh --flavor full
bash ./scripts/install.sh --flavor gateway
```

```powershell
.\scripts\install.ps1 -Flavor full
.\scripts\install.ps1 -Flavor gateway
```

## 升级后验证（推荐）

1. `GET /healthz` 返回 200。
2. `/rpc` 未鉴权请求返回预期失败（`gateway_code=unauthorized`）。
3. 必要时执行一次最小 `gateway.run` 冒烟。

## 回滚步骤

1. 停止当前网关进程。
2. 回退到上一版已验证二进制。
3. 重新启动并执行“升级后验证（推荐）”步骤。

若回滚后仍异常，优先检查配置文件兼容性与 token 文件状态。

## 版本来源

- 发布构建会通过 `ldflags` 注入版本号到 `internal/version.Version`。
- 本地开发构建默认版本为 `dev`。
