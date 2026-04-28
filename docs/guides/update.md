# 更新与回滚指南

## 1. 自动更新检测

1. `neocode` 启动时会后台检测新版本（默认 3 秒超时）。
2. 为避免干扰 TUI，提示在程序退出后展示。
3. `update` 与 `version` 子命令默认跳过静默检测（避免同次命令重复探测）。

## 2. 版本查询

查看当前版本并探测远端最新稳定版本：

```bash
neocode version
```

探测时包含预发布版本：

```bash
neocode version --prerelease
```

输出语义说明：

1. `Current version`：当前本地二进制版本。
2. `Latest stable version` / `Latest version (including prerelease)`：远端语义上的最新版本。
3. 若远端最新版本对当前平台不可安装，但存在更低可安装版本：
   - 会提示可执行升级（目标为当前平台可安装的最高版本）。
   - 同时提示远端存在“更新但当前平台暂不可安装”的状态，避免误判为“已是最新”。
4. 版本探测失败时命令仍返回成功退出码（0），并输出 `check failed` 诊断信息，便于脚本集成。

## 3. 手动升级

升级到最新稳定版本：

```bash
neocode update
```

包含预发布版本：

```bash
neocode update --prerelease
```

## 4. 双产物安装建议

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

## 5. 升级后验证（推荐）

1. `GET /healthz` 返回 200。
2. `/rpc` 未鉴权请求返回预期失败（`gateway_code=unauthorized`）。
3. 必要时执行一次最小 `gateway.run` 冒烟。

## 6. 回滚步骤

1. 停止当前网关进程。
2. 回退到上一版已验证二进制。
3. 重新启动并执行第 4 节验证步骤。

若回滚后仍异常，优先检查配置文件兼容性与 token 文件状态。
