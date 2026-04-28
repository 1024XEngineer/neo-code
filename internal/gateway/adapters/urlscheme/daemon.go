package urlscheme

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"neo-code/internal/gateway/protocol"
)

const (
	// DefaultHTTPDaemonListenAddress 是 HTTP daemon 的默认监听地址。
	DefaultHTTPDaemonListenAddress = "127.0.0.1:18921"
	// DaemonHostsAlias 是 daemon 方案要求的本地域名别名。
	DaemonHostsAlias = "neocode"
)

const (
	daemonAutostartModeWindowsRun  = "windows_run"
	daemonAutostartModeLaunchAgent = "launchagent"
	daemonAutostartModeSystemdUser = "systemd_user"
	daemonAutostartModeDesktop     = "desktop_autostart"
)

var (
	httpDaemonDispatchWakeFn = defaultHTTPDaemonDispatchWake
	httpDaemonGetHTTPClient  = defaultHTTPDaemonHTTPClient
)

// HTTPDaemonServeOptions 定义 daemon serve 的启动参数。
type HTTPDaemonServeOptions struct {
	ListenAddress        string
	GatewayListenAddress string
}

// HTTPDaemonInstallOptions 定义 daemon install 的安装参数。
type HTTPDaemonInstallOptions struct {
	ExecutablePath string
	ListenAddress  string
}

// HTTPDaemonInstallResult 返回 daemon install 的结果摘要。
type HTTPDaemonInstallResult struct {
	ListenAddress string `json:"listen_address"`
	AutostartMode string `json:"autostart_mode"`
	HostsWarning  string `json:"hosts_warning,omitempty"`
}

// HTTPDaemonStatusOptions 定义 daemon status 的查询参数。
type HTTPDaemonStatusOptions struct {
	ListenAddress string
}

// HTTPDaemonStatus 返回 daemon status 的状态快照。
type HTTPDaemonStatus struct {
	ListenAddress        string `json:"listen_address"`
	Running              bool   `json:"running"`
	AutostartConfigured  bool   `json:"autostart_configured"`
	AutostartMode        string `json:"autostart_mode,omitempty"`
	HostsAliasConfigured bool   `json:"hosts_alias_configured"`
}

type daemonAutostartState struct {
	Configured bool
	Mode       string
}

type daemonWakeDispatchRequest struct {
	Intent        protocol.WakeIntent
	ListenAddress string
}

type daemonWakeDispatchResult struct {
	SessionID string
	Action    string
}

// ServeHTTPDaemon 启动本地 HTTP daemon，并将 /run /review 请求派发到 wake.openUrl。
func ServeHTTPDaemon(ctx context.Context, options HTTPDaemonServeOptions) error {
	listenAddress := normalizeHTTPDaemonListenAddress(options.ListenAddress)
	gatewayListenAddress := strings.TrimSpace(options.GatewayListenAddress)
	dispatchWakeFn := httpDaemonDispatchWakeFn
	if dispatchWakeFn == nil {
		dispatchWakeFn = defaultHTTPDaemonDispatchWake
	}

	handler := newHTTPDaemonHandler(dispatchWakeFn, gatewayListenAddress)
	server := &http.Server{
		Addr:              listenAddress,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	err := server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		<-shutdownDone
		return nil
	}
	return err
}

// InstallHTTPDaemon 在用户目录安装 daemon 自启动，并 best-effort 写入 hosts 别名。
func InstallHTTPDaemon(options HTTPDaemonInstallOptions) (HTTPDaemonInstallResult, error) {
	executablePath, err := normalizeURLSchemeExecutablePath(options.ExecutablePath)
	if err != nil {
		return HTTPDaemonInstallResult{}, newDispatchError(ErrorCodeInternal, fmt.Sprintf("invalid executable path: %v", err))
	}
	listenAddress := normalizeHTTPDaemonListenAddress(options.ListenAddress)

	mode, err := installDaemonAutostart(executablePath, listenAddress)
	if err != nil {
		return HTTPDaemonInstallResult{}, err
	}

	result := HTTPDaemonInstallResult{
		ListenAddress: listenAddress,
		AutostartMode: mode,
	}
	if hostsErr := ensureDaemonHostsAlias(); hostsErr != nil {
		result.HostsWarning = hostsErr.Error()
	}
	return result, nil
}

// UninstallHTTPDaemon 移除 daemon 自启动配置。
func UninstallHTTPDaemon() error {
	return uninstallDaemonAutostart()
}

// GetHTTPDaemonStatus 返回 daemon 的运行状态与安装状态。
func GetHTTPDaemonStatus(ctx context.Context, options HTTPDaemonStatusOptions) (HTTPDaemonStatus, error) {
	listenAddress := normalizeHTTPDaemonListenAddress(options.ListenAddress)
	autostart, err := daemonAutostartStatus()
	if err != nil {
		return HTTPDaemonStatus{}, err
	}

	running := probeHTTPDaemonRunning(ctx, listenAddress)
	hostsConfigured := isDaemonHostsAliasConfigured()
	return HTTPDaemonStatus{
		ListenAddress:        listenAddress,
		Running:              running,
		AutostartConfigured:  autostart.Configured,
		AutostartMode:        autostart.Mode,
		HostsAliasConfigured: hostsConfigured,
	}, nil
}

// defaultHTTPDaemonDispatchWake 使用共享 Dispatcher 直接派发 WakeIntent。
func defaultHTTPDaemonDispatchWake(ctx context.Context, request daemonWakeDispatchRequest) (daemonWakeDispatchResult, error) {
	dispatcher := NewDispatcher()
	result, err := dispatcher.DispatchWakeIntent(ctx, WakeDispatchRequest{
		Intent:        request.Intent,
		ListenAddress: request.ListenAddress,
	})
	if err != nil {
		return daemonWakeDispatchResult{}, err
	}
	return daemonWakeDispatchResult{
		SessionID: strings.TrimSpace(result.Response.SessionID),
		Action:    strings.TrimSpace(request.Intent.Action),
	}, nil
}

// newHTTPDaemonHandler 构建 daemon 的 HTTP 路由与参数校验逻辑。
func newHTTPDaemonHandler(
	dispatchWakeFn func(context.Context, daemonWakeDispatchRequest) (daemonWakeDispatchResult, error),
	gatewayListenAddress string,
) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !isAllowedHTTPDaemonHost(request.Host) {
			writeHTTPDaemonError(writer, http.StatusForbidden, "forbidden host", request.Host)
			return
		}

		switch request.URL.Path {
		case "/healthz":
			if request.Method != http.MethodGet {
				writeHTTPDaemonError(writer, http.StatusMethodNotAllowed, "method not allowed", "")
				return
			}
			writeHTTPDaemonHealthz(writer)
			return
		case "/run", "/review":
		default:
			writeHTTPDaemonError(writer, http.StatusNotFound, "not found", request.URL.Path)
			return
		}

		if request.Method != http.MethodGet {
			writeHTTPDaemonError(writer, http.StatusMethodNotAllowed, "method not allowed", "")
			return
		}

		intent, err := buildHTTPDaemonWakeIntent(request)
		if err != nil {
			writeHTTPDaemonError(writer, http.StatusBadRequest, "invalid request", err.Error())
			return
		}

		result, err := dispatchWakeFn(request.Context(), daemonWakeDispatchRequest{
			Intent:        intent,
			ListenAddress: gatewayListenAddress,
		})
		if err != nil {
			writeHTTPDaemonError(writer, http.StatusInternalServerError, "dispatch failed", err.Error())
			return
		}
		writeHTTPDaemonSuccess(writer, request, result)
	})
}

// buildHTTPDaemonWakeIntent 将 HTTP 请求映射为 WakeIntent。
func buildHTTPDaemonWakeIntent(request *http.Request) (protocol.WakeIntent, error) {
	action := strings.Trim(strings.ToLower(strings.TrimSpace(request.URL.Path)), "/")
	if !protocol.IsSupportedWakeAction(action) {
		return protocol.WakeIntent{}, fmt.Errorf("unsupported action: %s", action)
	}

	query := request.URL.Query()
	params := flattenHTTPDaemonQuery(query)
	sessionID := popWakeQueryParam(params, "session_id", "session")
	workdir := popWakeQueryParam(params, "workdir")
	switch action {
	case protocol.WakeActionRun:
		if strings.TrimSpace(sessionID) == "" && strings.TrimSpace(params["prompt"]) == "" {
			return protocol.WakeIntent{}, errors.New("missing required query: prompt")
		}
	case protocol.WakeActionReview:
		if strings.TrimSpace(sessionID) == "" {
			if strings.TrimSpace(params["path"]) == "" {
				return protocol.WakeIntent{}, errors.New("missing required query: path")
			}
			if strings.TrimSpace(workdir) == "" {
				return protocol.WakeIntent{}, errors.New("missing required query: workdir or session_id")
			}
		}
	}
	if len(params) == 0 {
		params = nil
	}

	return protocol.WakeIntent{
		Action:    action,
		SessionID: strings.TrimSpace(sessionID),
		Workdir:   strings.TrimSpace(workdir),
		Params:    params,
		RawURL:    request.URL.String(),
	}, nil
}

// flattenHTTPDaemonQuery 将 query 参数压平为 key->value 映射（保留最后一个值）。
func flattenHTTPDaemonQuery(query map[string][]string) map[string]string {
	params := make(map[string]string, len(query))
	for key, values := range query {
		normalizedKey := strings.TrimSpace(key)
		if normalizedKey == "" {
			continue
		}
		if len(values) == 0 {
			params[normalizedKey] = ""
			continue
		}
		params[normalizedKey] = strings.TrimSpace(values[len(values)-1])
	}
	return params
}

// popWakeQueryParam 从参数表中按顺序读取并删除首个命中的键，避免下游重复处理保留字段。
func popWakeQueryParam(params map[string]string, keys ...string) string {
	if len(params) == 0 || len(keys) == 0 {
		return ""
	}
	for _, key := range keys {
		normalizedKey := strings.TrimSpace(key)
		if normalizedKey == "" {
			continue
		}
		value, exists := params[normalizedKey]
		if !exists {
			continue
		}
		delete(params, normalizedKey)
		return strings.TrimSpace(value)
	}
	return ""
}

// normalizeHTTPDaemonListenAddress 对监听地址执行默认化与去空白处理。
func normalizeHTTPDaemonListenAddress(listenAddress string) string {
	normalized := strings.TrimSpace(listenAddress)
	if normalized == "" {
		return DefaultHTTPDaemonListenAddress
	}
	return normalized
}

// isAllowedHTTPDaemonHost 校验 daemon 入口 Host 白名单。
func isAllowedHTTPDaemonHost(host string) bool {
	normalized := normalizeHTTPDaemonHost(host)
	switch normalized {
	case "neocode", "localhost", "127.0.0.1":
		return true
	default:
		return false
	}
}

// normalizeHTTPDaemonHost 从 Host 头中提取归一化主机名。
func normalizeHTTPDaemonHost(host string) string {
	normalized := strings.TrimSpace(host)
	if normalized == "" {
		return ""
	}
	if parsedHost, _, err := net.SplitHostPort(normalized); err == nil {
		normalized = parsedHost
	}
	normalized = strings.TrimSpace(strings.Trim(normalized, "[]"))
	return strings.ToLower(normalized)
}

// writeHTTPDaemonHealthz 输出健康检查响应。
func writeHTTPDaemonHealthz(writer http.ResponseWriter) {
	writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write([]byte("ok\n"))
}

// writeHTTPDaemonError 输出浏览器可读的错误页面。
func writeHTTPDaemonError(writer http.ResponseWriter, statusCode int, title string, detail string) {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.WriteHeader(statusCode)
	escapedTitle := html.EscapeString(strings.TrimSpace(title))
	escapedDetail := html.EscapeString(strings.TrimSpace(detail))
	_, _ = writer.Write([]byte(
		"<html><body><h3>" + escapedTitle + "</h3><p>" + escapedDetail + "</p></body></html>",
	))
}

// writeHTTPDaemonSuccess 输出浏览器可读的成功页面，并提供可复用的 session 链接。
func writeHTTPDaemonSuccess(writer http.ResponseWriter, request *http.Request, result daemonWakeDispatchResult) {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.WriteHeader(http.StatusOK)
	action := html.EscapeString(strings.TrimSpace(result.Action))
	sessionID := html.EscapeString(strings.TrimSpace(result.SessionID))
	reusableURL := buildHTTPDaemonReusableURL(request, result.SessionID)
	escapedReusableURL := html.EscapeString(reusableURL)
	_, _ = writer.Write([]byte(
		"<html><body><h3>OK</h3><p>action=" + action + "</p><p>session_id=" + sessionID +
			"</p><p>reusable_url=<a href=\"" + escapedReusableURL + "\">" + escapedReusableURL +
			"</a></p><p>tip=后续若要续接同一会话，请使用带 session_id 的链接。</p></body></html>",
	))
}

// buildHTTPDaemonReusableURL 基于当前请求地址生成包含 session_id 的可复用链接。
func buildHTTPDaemonReusableURL(request *http.Request, sessionID string) string {
	if request == nil || request.URL == nil {
		return ""
	}
	normalizedSessionID := strings.TrimSpace(sessionID)
	if normalizedSessionID == "" {
		return ""
	}

	reusableURL := *request.URL
	query := reusableURL.Query()
	query.Set("session_id", normalizedSessionID)
	reusableURL.RawQuery = query.Encode()
	if reusableURL.IsAbs() {
		return reusableURL.String()
	}

	requestURI := reusableURL.RequestURI()
	if strings.TrimSpace(requestURI) == "" {
		requestURI = reusableURL.String()
	}
	host := strings.TrimSpace(request.Host)
	if host == "" {
		return requestURI
	}
	scheme := "http"
	if request.TLS != nil {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s%s", scheme, host, requestURI)
}

// defaultHTTPDaemonHTTPClient 构建用于 status 探活的 HTTP 客户端。
func defaultHTTPDaemonHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout}
}

// probeHTTPDaemonRunning 探测 daemon /healthz 是否可达。
func probeHTTPDaemonRunning(ctx context.Context, listenAddress string) bool {
	clientFactory := httpDaemonGetHTTPClient
	if clientFactory == nil {
		clientFactory = defaultHTTPDaemonHTTPClient
	}
	client := clientFactory(1200 * time.Millisecond)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+listenAddress+"/healthz", http.NoBody)
	if err != nil {
		return false
	}
	response, err := client.Do(request)
	if err != nil {
		return false
	}
	defer func() { _ = response.Body.Close() }()
	return response.StatusCode == http.StatusOK
}

// ensureDaemonHostsAlias 以 best-effort 方式确保 hosts 文件存在 neocode 别名。
func ensureDaemonHostsAlias() error {
	hostsPath := daemonHostsFilePath()
	content, err := os.ReadFile(hostsPath)
	if err != nil {
		return fmt.Errorf("update hosts alias failed: %w", err)
	}
	if hasHostsAlias(content, DaemonHostsAlias) {
		return nil
	}

	text := string(content)
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	text += "127.0.0.1 " + DaemonHostsAlias + "\n"
	if err := os.WriteFile(hostsPath, []byte(text), 0o644); err != nil {
		return fmt.Errorf("update hosts alias failed: %w", err)
	}
	return nil
}

// isDaemonHostsAliasConfigured 检查 hosts 中是否已包含 neocode 别名。
func isDaemonHostsAliasConfigured() bool {
	content, err := os.ReadFile(daemonHostsFilePath())
	if err != nil {
		return false
	}
	return hasHostsAlias(content, DaemonHostsAlias)
}

// hasHostsAlias 判断 hosts 文本是否包含指定别名。
func hasHostsAlias(content []byte, alias string) bool {
	normalizedAlias := strings.ToLower(strings.TrimSpace(alias))
	if normalizedAlias == "" {
		return false
	}

	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if commentIndex := strings.Index(trimmed, "#"); commentIndex >= 0 {
			trimmed = strings.TrimSpace(trimmed[:commentIndex])
		}
		if trimmed == "" {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) < 2 {
			continue
		}
		for _, item := range fields[1:] {
			if strings.EqualFold(strings.TrimSpace(item), normalizedAlias) {
				return true
			}
		}
	}
	return false
}

// daemonHostsFilePath 返回当前系统 hosts 文件路径。
func daemonHostsFilePath() string {
	if runtime.GOOS == "windows" {
		systemRoot := strings.TrimSpace(os.Getenv("SystemRoot"))
		if systemRoot == "" {
			systemRoot = `C:\Windows`
		}
		return filepath.Join(systemRoot, "System32", "drivers", "etc", "hosts")
	}
	return "/etc/hosts"
}
