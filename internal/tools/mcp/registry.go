package mcp

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// ServerStatus 描述 MCP server 在 registry 中的生命周期状态。
type ServerStatus string

const (
	// ServerStatusConnecting 表示 server 已注册但仍在连接或初始化阶段。
	ServerStatusConnecting ServerStatus = "connecting"
	// ServerStatusReady 表示 server 已可用，支持正常工具调用。
	ServerStatusReady ServerStatus = "ready"
	// ServerStatusDegraded 表示 server 可部分服务，但存在健康或调用异常。
	ServerStatusDegraded ServerStatus = "degraded"
	// ServerStatusOffline 表示 server 当前不可用。
	ServerStatusOffline ServerStatus = "offline"
)

// ToolDescriptor 描述 MCP tool 的稳定元信息与输入 schema。
type ToolDescriptor struct {
	Name        string
	Description string
	InputSchema map[string]any
}

// ServerSnapshot 描述 registry 对外暴露的 server 只读快照。
type ServerSnapshot struct {
	ServerID  string
	Source    string
	Version   string
	Status    ServerStatus
	UpdatedAt time.Time
	Tools     []ToolDescriptor
}

// CallResult 收敛 MCP tool 调用后的统一结果语义。
type CallResult struct {
	Content  string
	IsError  bool
	Metadata map[string]any
}

// ServerClient 描述 registry 与具体 MCP server 交互所需的最小能力。
type ServerClient interface {
	ListTools(ctx context.Context) ([]ToolDescriptor, error)
	CallTool(ctx context.Context, toolName string, arguments []byte) (CallResult, error)
	HealthCheck(ctx context.Context) error
}

// closeableServerClient 描述支持主动关闭资源的 MCP client 扩展能力。
type closeableServerClient interface {
	Close() error
}

type serverEntry struct {
	snapshot ServerSnapshot
	client   ServerClient
}

// Registry 维护 MCP server 注册、快照读取和工具调用分发。
type Registry struct {
	mu      sync.RWMutex
	servers map[string]*serverEntry
}

// NewRegistry 创建线程安全的 MCP registry 实例。
func NewRegistry() *Registry {
	return &Registry{
		servers: make(map[string]*serverEntry),
	}
}

// RegisterServer 注册一个 MCP server，并初始化其生命周期状态。
func (r *Registry) RegisterServer(serverID string, source string, version string, client ServerClient) error {
	if r == nil {
		return errors.New("mcp: registry is nil")
	}
	normalizedID := normalizeServerID(serverID)
	if normalizedID == "" {
		return errors.New("mcp: server id is empty")
	}
	if client == nil {
		return errors.New("mcp: server client is nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.servers[normalizedID]; exists {
		return fmt.Errorf("mcp: server %q already exists", normalizedID)
	}
	r.servers[normalizedID] = &serverEntry{
		snapshot: ServerSnapshot{
			ServerID:  normalizedID,
			Source:    strings.TrimSpace(source),
			Version:   strings.TrimSpace(version),
			Status:    ServerStatusConnecting,
			UpdatedAt: time.Now(),
		},
		client: client,
	}
	return nil
}

// UnregisterServer 注销一个 MCP server，返回是否实际删除。
func (r *Registry) UnregisterServer(serverID string) bool {
	if r == nil {
		return false
	}
	normalizedID := normalizeServerID(serverID)
	if normalizedID == "" {
		return false
	}

	r.mu.Lock()
	entry, exists := r.servers[normalizedID]
	if !exists {
		r.mu.Unlock()
		return false
	}
	delete(r.servers, normalizedID)
	r.mu.Unlock()
	closeServerClient(entry.client)
	return true
}

// Close 会卸载并关闭注册表中所有的 MCP client，通常用于在系统退出时清理所有的 stdio 子进程。
func (r *Registry) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	servers := r.servers
	r.servers = make(map[string]*serverEntry)
	r.mu.Unlock()

	for _, entry := range servers {
		closeServerClient(entry.client)
	}
	return nil
}

// SetServerStatus 更新指定 server 的生命周期状态。
func (r *Registry) SetServerStatus(serverID string, status ServerStatus) error {
	if r == nil {
		return errors.New("mcp: registry is nil")
	}
	if !isValidStatus(status) {
		return fmt.Errorf("mcp: unsupported server status %q", status)
	}
	normalizedID := normalizeServerID(serverID)
	if normalizedID == "" {
		return errors.New("mcp: server id is empty")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.servers[normalizedID]
	if !ok {
		return fmt.Errorf("mcp: server %q not found", normalizedID)
	}
	entry.snapshot.Status = status
	entry.snapshot.UpdatedAt = time.Now()
	return nil
}

// RefreshServerTools 从 server 拉取工具清单并刷新快照。
func (r *Registry) RefreshServerTools(ctx context.Context, serverID string) error {
	if r == nil {
		return errors.New("mcp: registry is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	normalizedID := normalizeServerID(serverID)
	if normalizedID == "" {
		return errors.New("mcp: server id is empty")
	}

	r.mu.RLock()
	entry, ok := r.servers[normalizedID]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("mcp: server %q not found", normalizedID)
	}

	tools, err := entry.client.ListTools(ctx)
	if err != nil {
		_ = r.SetServerStatus(normalizedID, ServerStatusDegraded)
		return fmt.Errorf("mcp: list tools for server %q: %w", normalizedID, err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	current, exists := r.servers[normalizedID]
	if !exists {
		return fmt.Errorf("mcp: server %q not found", normalizedID)
	}
	current.snapshot.Tools = cloneToolDescriptors(tools)
	current.snapshot.Status = ServerStatusReady
	current.snapshot.UpdatedAt = time.Now()
	return nil
}

// HealthCheck 触发指定 server 的健康探测并同步状态。
func (r *Registry) HealthCheck(ctx context.Context, serverID string) error {
	if r == nil {
		return errors.New("mcp: registry is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	normalizedID := normalizeServerID(serverID)
	if normalizedID == "" {
		return errors.New("mcp: server id is empty")
	}

	r.mu.RLock()
	entry, ok := r.servers[normalizedID]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("mcp: server %q not found", normalizedID)
	}

	if err := entry.client.HealthCheck(ctx); err != nil {
		_ = r.SetServerStatus(normalizedID, ServerStatusOffline)
		return fmt.Errorf("mcp: health check failed for server %q: %w", normalizedID, err)
	}
	return r.SetServerStatus(normalizedID, ServerStatusReady)
}

// Call 通过 registry 分发指定 server/tool 的调用请求。
func (r *Registry) Call(ctx context.Context, serverID string, toolName string, arguments []byte) (CallResult, error) {
	if r == nil {
		return CallResult{}, errors.New("mcp: registry is nil")
	}
	if err := ctx.Err(); err != nil {
		return CallResult{}, err
	}
	normalizedID := normalizeServerID(serverID)
	if normalizedID == "" {
		return CallResult{}, errors.New("mcp: server id is empty")
	}
	trimmedToolName := strings.TrimSpace(toolName)
	if trimmedToolName == "" {
		return CallResult{}, errors.New("mcp: tool name is empty")
	}

	r.mu.RLock()
	entry, ok := r.servers[normalizedID]
	r.mu.RUnlock()
	if !ok {
		return CallResult{}, fmt.Errorf("mcp: server %q not found", normalizedID)
	}

	result, err := entry.client.CallTool(ctx, trimmedToolName, arguments)
	if err != nil {
		_ = r.SetServerStatus(normalizedID, ServerStatusDegraded)
		return CallResult{}, fmt.Errorf("mcp: call %s on %s failed: %w", trimmedToolName, normalizedID, err)
	}
	return result, nil
}

// Snapshot 返回当前 registry 的不可变 server 快照集合。
func (r *Registry) Snapshot() []ServerSnapshot {
	if r == nil {
		return nil
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.servers) == 0 {
		return nil
	}

	keys := make([]string, 0, len(r.servers))
	for serverID := range r.servers {
		keys = append(keys, serverID)
	}
	sort.Strings(keys)

	result := make([]ServerSnapshot, 0, len(keys))
	for _, serverID := range keys {
		entry := r.servers[serverID]
		snapshot := entry.snapshot
		snapshot.Tools = cloneToolDescriptors(snapshot.Tools)
		result = append(result, snapshot)
	}
	return result
}

// normalizeServerID 统一规范化 server id 以保证匹配稳定性。
func normalizeServerID(serverID string) string {
	return strings.ToLower(strings.TrimSpace(serverID))
}

// isValidStatus 校验 server 状态是否属于已定义集合。
func isValidStatus(status ServerStatus) bool {
	switch status {
	case ServerStatusConnecting, ServerStatusReady, ServerStatusDegraded, ServerStatusOffline:
		return true
	default:
		return false
	}
}

// cloneToolDescriptors 深拷贝工具描述，避免快照被外部引用污染。
func cloneToolDescriptors(input []ToolDescriptor) []ToolDescriptor {
	if len(input) == 0 {
		return nil
	}

	result := make([]ToolDescriptor, 0, len(input))
	for _, descriptor := range input {
		cloned := ToolDescriptor{
			Name:        strings.TrimSpace(descriptor.Name),
			Description: strings.TrimSpace(descriptor.Description),
			InputSchema: cloneSchema(descriptor.InputSchema),
		}
		result = append(result, cloned)
	}
	return result
}

// cloneSchema 深拷贝 schema 顶层 map，满足当前工具定义的只读需求。
func cloneSchema(schema map[string]any) map[string]any {
	if len(schema) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(schema))
	for key, value := range schema {
		cloned[key] = cloneAny(value)
	}
	return cloned
}

// cloneAny 递归复制 schema/metadata 中的 map 与 slice，避免跨层共享引用。
func cloneAny(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		cloned := make(map[string]any, len(typed))
		for key, item := range typed {
			cloned[key] = cloneAny(item)
		}
		return cloned
	case []any:
		cloned := make([]any, len(typed))
		for i, item := range typed {
			cloned[i] = cloneAny(item)
		}
		return cloned
	default:
		return value
	}
}

// closeServerClient 在 server 注销时尽力释放 client 持有的底层资源。
func closeServerClient(client ServerClient) {
	if client == nil {
		return
	}
	closeableClient, ok := client.(closeableServerClient)
	if !ok {
		return
	}
	_ = closeableClient.Close()
}
