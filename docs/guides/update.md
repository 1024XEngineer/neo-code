# 更新与回滚指南

## 1. 自动更新检测

1. `neocode` 启动时会后台检测新版本（默认 3 秒超时）。
2. 为避免干扰 TUI，提示在程序退出后展示。
3. `url-dispatch` 与 `update` 子命令默认跳过静默检测。

## 2. 手动升级

升级到最新稳定版本：

```bash
neocode update
```

包含预发布版本：

```bash
neocode update --prerelease
```

## 3. 双产物安装建议

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

## 4. 升级后验证（推荐）

1. `GET /healthz` 返回 200。
2. `/rpc` 未鉴权请求返回预期失败（`gateway_code=unauthorized`）。
3. 必要时执行一次最小 `gateway.run` 冒烟。

## 5. 回滚步骤

1. 停止当前网关进程。
2. 回退到上一版已验证二进制。
3. 重新启动并执行第 4 节验证步骤。

若回滚后仍异常，优先检查配置文件兼容性与 token 文件状态。
