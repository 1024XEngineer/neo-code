package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"neo-code/internal/config"
	"neo-code/internal/tools/mcp"
)

var newMCPStdioClient = mcp.NewStdIOClient
var registerMCPStdioServer = defaultRegisterMCPStdioServer

// BuildMCPRegistry 按配置构建并初始化 MCP registry；若无启用 server 则返回 nil。
func BuildMCPRegistry(cfg config.Config) (*mcp.Registry, error) {
	if len(cfg.Tools.MCP.Servers) == 0 {
		return nil, nil
	}

	registry := mcp.NewRegistry()
	enabledCount := 0
	registeredServerIDs := make([]string, 0, len(cfg.Tools.MCP.Servers))
	for index := range cfg.Tools.MCP.Servers {
		server := cfg.Tools.MCP.Servers[index]
		if !server.Enabled {
			continue
		}
		enabledCount++

		switch strings.ToLower(strings.TrimSpace(server.Source)) {
		case "", "stdio":
			if err := registerMCPStdioServer(registry, cfg, server); err != nil {
				rollbackMCPServers(registry, append(registeredServerIDs, strings.TrimSpace(server.ID)))
				return nil, fmt.Errorf("app: register mcp server %q: %w", strings.TrimSpace(server.ID), err)
			}
			registeredServerIDs = append(registeredServerIDs, strings.TrimSpace(server.ID))
		default:
			rollbackMCPServers(registry, registeredServerIDs)
			return nil, fmt.Errorf("app: unsupported mcp source %q", server.Source)
		}
	}

	if enabledCount == 0 {
		return nil, nil
	}
	return registry, nil
}

// rollbackMCPServers 在批量注册失败时回滚已注册 server，避免残留子进程或脏状态。
func rollbackMCPServers(registry *mcp.Registry, serverIDs []string) {
	if registry == nil || len(serverIDs) == 0 {
		return
	}
	for index := len(serverIDs) - 1; index >= 0; index-- {
		_ = registry.UnregisterServer(serverIDs[index])
	}
}

// defaultRegisterMCPStdioServer 创建 stdio client 并完成 server 注册与 tools 快照初始化。
func defaultRegisterMCPStdioServer(registry *mcp.Registry, cfg config.Config, server config.MCPServerConfig) error {
	env, err := resolveMCPServerEnv(server)
	if err != nil {
		return err
	}

	workdir := resolveMCPServerWorkdir(cfg.Workdir, server.Stdio.Workdir)
	client, err := newMCPStdioClient(mcp.StdioClientConfig{
		Command:        strings.TrimSpace(server.Stdio.Command),
		Args:           append([]string(nil), server.Stdio.Args...),
		Env:            env,
		Workdir:        workdir,
		StartTimeout:   durationFromSeconds(server.Stdio.StartTimeoutSec),
		CallTimeout:    durationFromSeconds(server.Stdio.CallTimeoutSec),
		RestartBackoff: durationFromSeconds(server.Stdio.RestartBackoffSec),
	})
	if err != nil {
		return err
	}

	serverID := strings.TrimSpace(server.ID)
	source := strings.ToLower(strings.TrimSpace(server.Source))
	if source == "" {
		source = "stdio"
	}
	if err := registry.RegisterServer(serverID, source, strings.TrimSpace(server.Version), client); err != nil {
		return err
	}

	refreshCtx, cancel := context.WithTimeout(context.Background(), initialMCPRefreshTimeout(cfg))
	defer cancel()
	if err := registry.RefreshServerTools(refreshCtx, serverID); err != nil {
		_ = registry.UnregisterServer(serverID)
		return err
	}
	return nil
}

// resolveMCPServerEnv 将配置中的 env 绑定解析为子进程环境变量。
func resolveMCPServerEnv(server config.MCPServerConfig) ([]string, error) {
	if len(server.Env) == 0 {
		return nil, nil
	}
	result := make([]string, 0, len(server.Env))
	for index, item := range server.Env {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			return nil, fmt.Errorf("env[%d].name is empty", index)
		}

		value := strings.TrimSpace(item.Value)
		valueEnv := strings.TrimSpace(item.ValueEnv)
		switch {
		case value != "" && valueEnv != "":
			return nil, fmt.Errorf("env[%d] must set either value or value_env", index)
		case value != "":
			result = append(result, name+"="+value)
		case valueEnv != "":
			resolved := strings.TrimSpace(os.Getenv(valueEnv))
			if resolved == "" {
				return nil, fmt.Errorf("env[%d] value_env %q is empty", index, valueEnv)
			}
			result = append(result, name+"="+resolved)
		default:
			return nil, fmt.Errorf("env[%d] must set one of value/value_env", index)
		}
	}
	return result, nil
}

// resolveMCPServerWorkdir 解析 MCP server 子进程工作目录，支持相对路径。
func resolveMCPServerWorkdir(baseWorkdir string, override string) string {
	trimmedOverride := strings.TrimSpace(override)
	if trimmedOverride == "" {
		return strings.TrimSpace(baseWorkdir)
	}
	if filepath.IsAbs(trimmedOverride) {
		return filepath.Clean(trimmedOverride)
	}

	trimmedBase := strings.TrimSpace(baseWorkdir)
	if trimmedBase == "" {
		return filepath.Clean(trimmedOverride)
	}
	return filepath.Clean(filepath.Join(trimmedBase, trimmedOverride))
}

// initialMCPRefreshTimeout 计算启动阶段首轮 tools 刷新的超时时间。
func initialMCPRefreshTimeout(cfg config.Config) time.Duration {
	timeout := time.Duration(cfg.ToolTimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	if timeout < 5*time.Second {
		timeout = 5 * time.Second
	}
	return timeout
}

// durationFromSeconds 将秒级配置转换为 duration；非正值返回 0 以启用 client 默认值。
func durationFromSeconds(seconds int) time.Duration {
	if seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}
