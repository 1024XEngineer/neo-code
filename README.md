# NeoCode

基于 Go + Bubble Tea 的本地 AI Coding Agent，主链路为：

`用户输入(TUI) -> Gateway -> Runtime -> Tools -> 结果回传 -> UI 展示`

## 产物形态

本项目提供双产物发布：

1. `neocode`：默认完整客户端入口（含 `gateway` 子命令）。
2. `neocode-gateway`：Gateway-Only 服务端入口（不含 TUI 主入口语义）。

## 快速开始

### 1) 从源码运行

```bash
git clone https://github.com/1024XEngineer/neo-code.git
cd neo-code
go run ./cmd/neocode
```

### 2) 启动网关（两种等价方式）

```bash
go run ./cmd/neocode gateway --http-listen 127.0.0.1:8080
```

```bash
go run ./cmd/neocode-gateway --http-listen 127.0.0.1:8080
```

### 3) URL 唤醒分发

```bash
go run ./cmd/neocode url-dispatch --url "neocode://review?path=README.md"
```

当网关不可达时，`url-dispatch` 会按固定发现顺序尝试自动拉起：

1. `NEOCODE_GATEWAY_BIN` 显式路径
2. `PATH` 中 `neocode-gateway`
3. 回退当前可执行 `neocode gateway`

## 安装脚本

### Linux / macOS

```bash
curl -fsSL https://raw.githubusercontent.com/1024XEngineer/neo-code/main/scripts/install.sh | bash
```

可选 flavor：

```bash
bash ./scripts/install.sh --flavor full
bash ./scripts/install.sh --flavor gateway
```

Dry-run（仅输出资产 URL / checksum URL）：

```bash
bash ./scripts/install.sh --flavor gateway --dry-run
```

### Windows PowerShell

```powershell
irm https://raw.githubusercontent.com/1024XEngineer/neo-code/main/scripts/install.ps1 | iex
```

可选 flavor 与 dry-run：

```powershell
.\scripts\install.ps1 -Flavor full
.\scripts\install.ps1 -Flavor gateway
.\scripts\install.ps1 -Flavor gateway -DryRun
```

## 部署拓扑建议

1. 本地内嵌（默认）：`neocode` 进程内通过 `gateway` 子命令管理网关。
2. 独立网关服务：使用 `neocode-gateway` 作为可审计、可独立运维的网关进程。

默认监听保持回环地址（`127.0.0.1`）；对外暴露必须显式配置并补齐鉴权与 ACL。

## 升级与回滚（最小流程）

1. 升级后先验证 `GET /healthz`。
2. 再验证 `/rpc` 最小请求（含未鉴权失败路径）。
3. 如异常，回滚到上一个已验证版本的二进制与配置。

## 文档索引

- [Gateway 详细设计 RFC](docs/gateway-detailed-design.md)
- [Gateway 第三方接入协作指南](docs/guides/gateway-integration-guide.md)
- [Gateway RPC API（XGO 风格）](docs/gateway-rpc-api.md)
- [Gateway 错误字典](docs/gateway-error-catalog.md)
- [Gateway 兼容性策略](docs/gateway-compatibility.md)
- [配置指南](docs/guides/configuration.md)
- [更新指南](docs/guides/update.md)

## 开发与验证

```bash
go build ./...
go test ./...
gofmt -w ./cmd ./internal
```

## License

MIT
