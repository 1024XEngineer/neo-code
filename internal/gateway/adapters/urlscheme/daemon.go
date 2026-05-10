package urlscheme

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net"
	"net/http"
	"os"
	"os/exec"
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

const (
	daemonHTTPPageTitle      = "NeoCode Daemon"
	daemonHTTPPageFaviconURL = "data:image/svg+xml,%3Csvg%20xmlns='http://www.w3.org/2000/svg'%20viewBox='0%200%20120%20120'%3E%3Crect%20width='120'%20height='120'%20rx='22'%20fill='%230f172a'/%3E%3Cpath%20d='M36%2032h16l32%2056H68L36%2032z'%20fill='%2338bdf8'/%3E%3Cpath%20d='M84%2032H68L36%2088h16l32-56z'%20fill='%230ea5e9'/%3E%3C/svg%3E"
)

var (
	httpDaemonDispatchWakeFn = defaultHTTPDaemonDispatchWake
	httpDaemonGetHTTPClient  = defaultHTTPDaemonHTTPClient
	httpDaemonStartProcessFn = startHTTPDaemonProcess
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
	ListenAddress      string `json:"listen_address"`
	AutostartMode      string `json:"autostart_mode"`
	HostsWarning       string `json:"hosts_warning,omitempty"`
	DaemonStarted      bool   `json:"daemon_started"`
	DaemonStartWarning string `json:"daemon_start_warning,omitempty"`
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
		result.HostsWarning = buildHostsAliasWarning(hostsErr)
	}
	if daemonStartErr := ensureHTTPDaemonProcessStarted(executablePath, listenAddress); daemonStartErr != nil {
		result.DaemonStartWarning = daemonStartErr.Error()
	} else {
		result.DaemonStarted = true
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
			writeHTTPDaemonDispatchError(writer, gatewayListenAddress, err)
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
	content := `<h1 class="status error">` + escapedTitle + `</h1><p class="detail">` + escapedDetail + `</p>`
	_, _ = writer.Write([]byte(buildHTTPDaemonPageHTML(escapedTitle, content, "")))
}

// writeHTTPDaemonDispatchError 输出 wake 派发失败页面，并附带可执行补救指引。
func writeHTTPDaemonDispatchError(writer http.ResponseWriter, gatewayListenAddress string, err error) {
	title := "Dispatch failed"
	detail := strings.TrimSpace(err.Error())
	if detail == "" {
		detail = "unknown dispatch error"
	}
	hints := []string{
		"运行 `neocode daemon status` 检查 daemon 状态。",
	}
	startGatewayCommand := "neocode gateway"
	if trimmedGatewayListenAddress := strings.TrimSpace(gatewayListenAddress); trimmedGatewayListenAddress != "" {
		startGatewayCommand += " --listen " + trimmedGatewayListenAddress
	}
	hints = append(hints, "若 gateway 未运行，请手动执行 `"+startGatewayCommand+"`。")

	var dispatchErr *DispatchError
	if errors.As(err, &dispatchErr) {
		switch dispatchErr.Code {
		case ErrorCodeGatewayUnavailable:
			title = "Gateway unavailable"
			detail = "无法连接到 gateway，daemon 已尝试自动拉起，但当前不可达。"
			if strings.Contains(strings.ToLower(dispatchErr.Message), "timed out") {
				detail = "gateway 自动拉起后 10 秒内未就绪，无法继续派发。"
			}
		case ErrorCodeNotSupported:
			title = "Terminal launch is not supported"
			detail = strings.TrimSpace(dispatchErr.Message)
			if detail == "" {
				detail = "当前平台不支持自动拉起终端，请手动运行 neocode 续接会话。"
			}
		default:
			if message := strings.TrimSpace(dispatchErr.Message); message != "" {
				detail = message
			}
		}
	}
	if strings.TrimSpace(err.Error()) != "" {
		hints = append(hints, "原始错误: "+strings.TrimSpace(err.Error()))
	}
	writeHTTPDaemonError(writer, http.StatusInternalServerError, title, strings.Join(hintsWithLead(detail, hints), "\n"))
}

// writeHTTPDaemonSuccess 输出浏览器可读的成功页面，并提供可复用的 session 链接。
func writeHTTPDaemonSuccess(writer http.ResponseWriter, request *http.Request, result daemonWakeDispatchResult) {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.WriteHeader(http.StatusOK)
	action := html.EscapeString(strings.TrimSpace(result.Action))
	sessionID := html.EscapeString(strings.TrimSpace(result.SessionID))
	reusableURL := buildHTTPDaemonReusableURL(request, result.SessionID)
	escapedReusableURL := html.EscapeString(reusableURL)
	content := `<h1 class="status ok">Wake Accepted</h1>` +
		`<p class="detail">action=<strong>` + action + `</strong></p>` +
		`<p class="detail">session_id=<code>` + sessionID + `</code></p>` +
		`<p class="label">reusable_url</p>` +
		`<a class="link" id="reusable-link" href="` + escapedReusableURL + `">` + escapedReusableURL + `</a>` +
		`<div class="copy-row"><button class="copy-btn" type="button" id="copy-reusable-btn" data-copy-text="` + escapedReusableURL + `">复制链接</button>` +
		`<span class="copy-feedback" id="copy-feedback">点击后自动复制 reusable_url</span></div>` +
		`<p class="tip">后续若要续接同一会话，请使用带 session_id 的链接。</p>`
	_, _ = writer.Write([]byte(buildHTTPDaemonPageHTML("Wake Accepted", content, daemonCopyScript())))
}

// buildHTTPDaemonPageHTML 渲染统一的 daemon HTML 页面骨架与基础样式。
func buildHTTPDaemonPageHTML(title string, contentHTML string, script string) string {
	escapedTitle := html.EscapeString(strings.TrimSpace(title))
	if escapedTitle == "" {
		escapedTitle = daemonHTTPPageTitle
	}
	if strings.TrimSpace(contentHTML) == "" {
		contentHTML = `<p class="detail">empty content</p>`
	}
	return "<!doctype html><html lang=\"zh-CN\"><head><meta charset=\"utf-8\">" +
		"<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">" +
		"<title>" + escapedTitle + " - " + daemonHTTPPageTitle + "</title>" +
		"<link rel=\"icon\" href=\"" + daemonHTTPPageFaviconURL + "\">" +
		"<style>" + daemonHTTPPageStyle() + "</style></head><body><main class=\"card\">" + contentHTML +
		"</main>" + script + "</body></html>"
}

// daemonHTTPPageStyle 返回 daemon 页面使用的基础视觉样式。
func daemonHTTPPageStyle() string {
	return "body{margin:0;min-height:100vh;display:flex;align-items:center;justify-content:center;" +
		"background:radial-gradient(circle at top,#e0f2fe 0%,#f8fafc 45%,#f1f5f9 100%);" +
		"font-family:'Segoe UI',Tahoma,Helvetica,Arial,sans-serif;color:#0f172a;padding:24px;}" +
		".card{width:min(760px,100%);background:#ffffff;border:1px solid #dbeafe;border-radius:18px;" +
		"box-shadow:0 18px 40px rgba(15,23,42,.12);padding:28px;}" +
		".status{margin:0 0 12px;font-size:28px;line-height:1.2;}" +
		".status.ok{color:#0284c7;}.status.error{color:#b91c1c;}" +
		".detail{margin:8px 0;font-size:15px;line-height:1.6;white-space:pre-line;}" +
		".label{margin:18px 0 8px;font-weight:600;font-size:14px;color:#334155;}" +
		".link{display:block;word-break:break-all;color:#0f766e;text-decoration:none;background:#ecfeff;" +
		"padding:10px 12px;border-radius:10px;border:1px solid #bae6fd;}" +
		".copy-row{display:flex;flex-wrap:wrap;gap:10px;align-items:center;margin-top:12px;}" +
		".copy-btn{border:none;border-radius:10px;padding:10px 14px;background:#0ea5e9;color:#fff;cursor:pointer;" +
		"font-weight:600;}" +
		".copy-btn:hover{background:#0284c7;}" +
		".copy-feedback{font-size:13px;color:#475569;}" +
		".tip{margin-top:14px;font-size:14px;color:#334155;}"
}

// daemonCopyScript 返回成功页的链接复制脚本，优先使用 Clipboard API，
// 在非安全上下文（如 http://neocode）中回退到 execCommand。
func daemonCopyScript() string {
	return `<script>(function(){const btn=document.getElementById('copy-reusable-btn');` +
		`const feedback=document.getElementById('copy-feedback');if(!btn){return;}` +
		`function fallbackCopy(t){const ta=document.createElement('textarea');ta.value=t;` +
		`ta.style.position='fixed';ta.style.opacity='0';document.body.appendChild(ta);ta.select();` +
		`try{document.execCommand('copy');return true;}catch(_){return false;}finally{document.body.removeChild(ta);}}` +
		`btn.addEventListener('click',async function(){` +
		`const text=btn.getAttribute('data-copy-text')||'';if(!text){if(feedback){feedback.textContent='链接为空，无法复制';}return;}` +
		`let ok=false;try{if(navigator.clipboard&&navigator.clipboard.writeText){await navigator.clipboard.writeText(text);ok=true;}}catch(_){}` +
		`if(!ok){ok=fallbackCopy(text);}` +
		`if(feedback){feedback.textContent=ok?'已复制到剪贴板':'复制失败，请手动复制上方链接';}});})();</script>`
}

// hintsWithLead 拼装错误正文与补救建议列表。
func hintsWithLead(detail string, hints []string) []string {
	lines := []string{strings.TrimSpace(detail)}
	for _, hint := range hints {
		trimmedHint := strings.TrimSpace(hint)
		if trimmedHint == "" {
			continue
		}
		lines = append(lines, "- "+trimmedHint)
	}
	return lines
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

// ensureHTTPDaemonProcessStarted 尝试在 install 完成后立刻拉起 daemon，并返回可读告警。
func ensureHTTPDaemonProcessStarted(executablePath string, listenAddress string) error {
	normalizedListenAddress := normalizeHTTPDaemonListenAddress(listenAddress)
	probeCtx, cancel := context.WithTimeout(context.Background(), 1400*time.Millisecond)
	alreadyRunning := probeHTTPDaemonRunning(probeCtx, normalizedListenAddress)
	cancel()
	if alreadyRunning {
		return nil
	}

	startFn := httpDaemonStartProcessFn
	if startFn == nil {
		startFn = startHTTPDaemonProcess
	}
	if err := startFn(executablePath, normalizedListenAddress); err != nil {
		return fmt.Errorf("failed to start daemon in background: %w; run `%s daemon serve --listen %s` manually", err, executablePath, normalizedListenAddress)
	}

	readyCtx, readyCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer readyCancel()
	for {
		if probeHTTPDaemonRunning(readyCtx, normalizedListenAddress) {
			return nil
		}
		if readyCtx.Err() != nil {
			return fmt.Errorf("daemon background process started but not ready within 3s; run `%s daemon serve --listen %s` manually", executablePath, normalizedListenAddress)
		}
		time.Sleep(120 * time.Millisecond)
	}
}

// startHTTPDaemonProcess 以后台进程方式启动 daemon serve。
func startHTTPDaemonProcess(executablePath string, listenAddress string) error {
	command := exec.Command(executablePath, "daemon", "serve", "--listen", strings.TrimSpace(listenAddress))
	command.Stdin = nil
	command.Stdout = nil
	command.Stderr = nil
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}

// buildHostsAliasWarning 构建 hosts 自动写入失败时的手动修复提示。
func buildHostsAliasWarning(err error) string {
	base := fmt.Sprintf("failed to update hosts alias automatically: %v", err)
	if runtime.GOOS == "windows" {
		return base + "; please run as Administrator: echo 127.0.0.1 neocode >> C:\\Windows\\System32\\drivers\\etc\\hosts"
	}
	return base + "; please run with sudo: sudo echo '127.0.0.1 neocode' >> /etc/hosts"
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
