# NeoCode 运维指南

### 12.3 安装与分发

| 渠道 | 说明 |
|------|------|
| **Shell 脚本** | `curl -fsSL <url>/install.sh \| bash`（macOS/Linux），支持 `--flavor full\|gateway` |
| **PowerShell** | `irm <url>/install.ps1 \| iex`（Windows） |
| **自更新** | `neocode update` 命令通过 `go-selfupdate` 拉取最新 GitHub Release，校验 checksum 后原地替换二进制 |
| **Electron 桌面端** | 通过 `electron-builder` 打包为 `.dmg`（macOS）/ `.exe` installer（Windows）/ `.AppImage`（Linux） |
| **手动下载** | GitHub Releases 页面下载对应平台的 `.tar.gz` / `.zip` |


### 14.5 告警建议

基于 Gateway 暴露的 Prometheus 指标，推荐配置以下告警规则：

| 告警 | 条件 | 严重度 |
|------|------|--------|
| 认证失败率异常 | `rate(gateway_auth_failures_total[5m]) > 0.1` | Warning |
| ACL 拒绝突增 | `rate(gateway_acl_denied_total[5m]) > 5` | Warning |
| 流连接数接近上限 | `gateway_connections_active > max * 0.8` | Warning |
| 流连接异常剔除 | `rate(gateway_stream_dropped_total[5m]) > 0` | Critical |
| Gateway 不可达 | `gateway.ping` 无响应或 `/healthz` 非 200 | Critical |

### 14.6 运维诊断工具

| 工具 | 用途 |
|------|------|
| `neocode diag` | Shell 诊断代理：自动获取终端最近一次命令的异常输出，调用 LLM 分析原因并给出建议 |
| `neocode daemon status` | 查看 HTTP Daemon 运行状态与自启动安装状态 |
| `neocode gateway --http-listen <addr>` | 显式指定 Gateway HTTP 监听地址，供调试时暴露到非 loopback 接口 |
| Session Log Viewer | Runtime 内部将关键会话事件写入 `log-viewer/` 目录，供离线排查 |

