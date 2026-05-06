package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"neo-code/internal/webassets"
)

type webCommandOptions struct {
	HTTPAddress  string
	LogLevel     string
	StaticDir    string
	OpenBrowser  bool
	SkipBuild    bool
	Workdir      string
	TokenFile    string
}

// newWebCommand 创建并返回根命令下的 web 子命令，负责构建前端并启动带 Web UI 的 Gateway。
func newWebCommand() *cobra.Command {
	options := &webCommandOptions{}

	cmd := &cobra.Command{
		Use:   "web",
		Short: "Start NeoCode with Web UI",
		Long:  "Build frontend assets (if needed) and start the gateway with an integrated web UI.\nOpen http://127.0.0.1:8080 in your browser to use the interactive coding agent.",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			options.Workdir = mustReadInheritedWorkdir(cmd)
			return runWebCommand(cmd.Context(), *options)
		},
	}

	cmd.Flags().StringVar(&options.HTTPAddress, "http-listen", "127.0.0.1:8080", "web UI listen address")
	cmd.Flags().StringVar(&options.LogLevel, "log-level", "info", "gateway log level: debug|info|warn|error")
	cmd.Flags().StringVar(&options.StaticDir, "static-dir", "", "frontend static files directory override")
	cmd.Flags().BoolVar(&options.OpenBrowser, "open-browser", true, "open browser automatically")
	cmd.Flags().BoolVar(&options.SkipBuild, "skip-build", false, "skip frontend build (error if dist/ missing)")
	cmd.Flags().StringVar(&options.TokenFile, "token-file", "", "gateway auth token file path")

	return cmd
}

// runWebCommand 执行 web 子命令：解析前端目录 → 构建前端（可选） → 启动 Gateway → 打开浏览器。
func runWebCommand(ctx context.Context, options webCommandOptions) error {
	logger := log.New(os.Stderr, "neocode-web: ", log.LstdFlags)

	// 如果未指定 workdir，默认使用当前工作目录
	if strings.TrimSpace(options.Workdir) == "" {
		if cwd, err := os.Getwd(); err == nil {
			options.Workdir = cwd
		}
	}

	// 1. 解析前端静态文件目录
	staticDir, err := resolveWebStaticDir(options.StaticDir)
	needBuild := err != nil

	// 检查源码是否比 dist 更新（仅在 dist 存在且未指定 --static-dir 时）
	if err == nil && options.StaticDir == "" {
		webDir := findWebSourceDir()
		if webDir != "" && isStaleFrontendBuild(webDir) {
			logger.Println("frontend source is newer than build output, rebuilding...")
			needBuild = true
		}
	}

	if needBuild {
		if options.SkipBuild {
			return fmt.Errorf("frontend needs rebuild and --skip-build is set")
		}
		webDir := findWebSourceDir()
		if webDir == "" {
			if err != nil {
				return fmt.Errorf("frontend not found: %w", err)
			}
			return fmt.Errorf("web source directory not found; run from project root or set --static-dir")
		}
		if buildErr := buildFrontend(webDir, logger); buildErr != nil {
			return fmt.Errorf("frontend build failed: %w", buildErr)
		}
		// 构建后重新解析
		staticDir, err = resolveWebStaticDir(options.StaticDir)
		if err != nil {
			return fmt.Errorf("frontend dist not found after build: %w", err)
		}
	}
	// 2. 确定静态文件来源：外部目录优先，找不到时回退到嵌入资源
	var staticFileFS fs.FS
	if staticDir == "" {
		if webassets.IsAvailable() {
			staticFileFS = webassets.FS
			logger.Println("serving web UI from embedded assets")
		} else {
			logger.Println("warning: no web UI assets found (external dist missing and embedded assets not compiled)")
		}
	} else {
		logger.Printf("serving web UI from %s", staticDir)
	}

	// 3. 启动 Gateway（复用共享启动逻辑，Web 模式跳过 IPC）
	gatewayOpts := gatewayCommandOptions{
		HTTPAddress: resolveWebListenAddress(options.HTTPAddress),
		LogLevel:    options.LogLevel,
		Workdir:     options.Workdir,
		TokenFile:   options.TokenFile,
		SkipIPC:     true,
	}

	// 网络服务器就绪后打开浏览器
	var onNetworkReady func(address string)
	if options.OpenBrowser {
		onNetworkReady = func(address string) {
			go waitForGatewayAndOpenBrowser(ctx, address, logger)
		}
	}

	return startGatewayServer(ctx, gatewayOpts, staticDir, staticFileFS, onNetworkReady)
}

// resolveWebStaticDir 按 --static-dir → <cwd>/web/dist → <exe_dir>/web/dist 顺序查找前端静态文件。
func resolveWebStaticDir(override string) (string, error) {
	if override != "" {
		return validateStaticDir(override)
	}

	// 相对于当前工作目录（适用于 go run ./cmd/neocode web）
	if dir, err := validateStaticDir(filepath.Join(".", "web", "dist")); err == nil {
		return dir, nil
	}

	// 相对于可执行文件（适用于安装的二进制）
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		if dir, err := validateStaticDir(filepath.Join(exeDir, "web", "dist")); err == nil {
			return dir, nil
		}
		if dir, err := validateStaticDir(filepath.Join(exeDir, "..", "web", "dist")); err == nil {
			return dir, nil
		}
	}

	return "", fmt.Errorf("web/dist not found; run from project root or build frontend with 'cd web && npm install && npm run build'")
}

// validateStaticDir 验证目录存在且包含 index.html。
func validateStaticDir(dir string) (string, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(filepath.Join(absDir, "index.html"))
	if err != nil {
		return "", fmt.Errorf("index.html not found in %s: %w", absDir, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("index.html in %s is a directory", absDir)
	}
	return absDir, nil
}

// findWebSourceDir 查找 web 源码目录（包含 package.json）。
func findWebSourceDir() string {
	candidates := []string{
		filepath.Join(".", "web"),
	}
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(exeDir, "web"),
			filepath.Join(exeDir, "..", "web"),
		)
	}
	for _, dir := range candidates {
		if _, err := os.Stat(filepath.Join(dir, "package.json")); err == nil {
			absDir, _ := filepath.Abs(dir)
			return absDir
		}
	}
	return ""
}

// isStaleFrontendBuild 检查前端源码是否比 dist 构建产物更新。
func isStaleFrontendBuild(webDir string) bool {
	distIndex := filepath.Join(webDir, "dist", "index.html")
	distInfo, err := os.Stat(distIndex)
	if err != nil {
		return true
	}
	distModTime := distInfo.ModTime()

	for _, f := range []string{
		filepath.Join(webDir, "package.json"),
		filepath.Join(webDir, "vite.config.ts"),
		filepath.Join(webDir, "tsconfig.json"),
	} {
		if info, statErr := os.Stat(f); statErr == nil && info.ModTime().After(distModTime) {
			return true
		}
	}

	stale := false
	filepath.Walk(filepath.Join(webDir, "src"), func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() {
			return nil
		}
		if info.ModTime().After(distModTime) {
			stale = true
			return filepath.SkipAll
		}
		return nil
	})
	return stale
}

// buildFrontend 在 webDir 中执行 npm install && npm run build。
func buildFrontend(webDir string, logger *log.Logger) error {
	npmBin, err := findNPMBinary()
	if err != nil {
		return err
	}

	logger.Printf("running npm install in %s ...", webDir)
	installCmd := exec.Command(npmBin, "install")
	installCmd.Dir = webDir
	installCmd.Stdout = os.Stderr
	installCmd.Stderr = os.Stderr
	if err := installCmd.Run(); err != nil {
		return fmt.Errorf("npm install failed: %w", err)
	}

	logger.Println("running npm run build ...")
	buildCmd := exec.Command(npmBin, "run", "build")
	buildCmd.Dir = webDir
	buildCmd.Stdout = os.Stderr
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		return fmt.Errorf("npm run build failed: %w", err)
	}

	// 验证构建产物
	distDir := filepath.Join(webDir, "dist")
	if _, err := os.Stat(filepath.Join(distDir, "index.html")); err != nil {
		return fmt.Errorf("build completed but dist/index.html not found: %w", err)
	}

	logger.Println("frontend build completed successfully")
	return nil
}

// findNPMBinary 查找系统中的 npm 可执行文件。
func findNPMBinary() (string, error) {
	name := "npm"
	if runtime.GOOS == "windows" {
		name = "npm.cmd"
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("npm not found on PATH; install Node.js to build the frontend, or use --static-dir to specify pre-built assets")
	}
	return path, nil
}

// waitForGatewayAndOpenBrowser 轮询 Gateway 健康检查，就绪后打开浏览器并附带认证 token。
func waitForGatewayAndOpenBrowser(ctx context.Context, address string, logger *log.Logger) {
	baseURL := fmt.Sprintf("http://%s", address)
	token := readGatewayToken()
	maxRetries := 30

	for i := 0; i < maxRetries; i++ {
		select {
		case <-ctx.Done():
			return
		default:
		}

		resp, err := http.Get(baseURL + "/healthz")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				browserURL := baseURL
				if token != "" {
					browserURL += "/?token=" + token
				}
				logger.Printf("gateway is ready, opening browser: %s", baseURL)
				if openErr := openBrowser(browserURL); openErr != nil {
					logger.Printf("failed to open browser: %v (open %s manually)", openErr, browserURL)
				}
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	logger.Printf("gateway health check timed out, open %s manually", baseURL)
}

// readGatewayToken 从 ~/.neocode/auth.json 读取认证 token。
func readGatewayToken() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	authPath := filepath.Join(homeDir, ".neocode", "auth.json")
	data, err := os.ReadFile(authPath)
	if err != nil {
		return ""
	}
	var auth struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(data, &auth); err != nil {
		return ""
	}
	return strings.TrimSpace(auth.Token)
}

// openBrowser 使用系统默认浏览器打开 URL。
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", strings.ReplaceAll(url, "&", "^&"))
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

// resolveWebListenAddress 解析 Web 监听地址。如果默认端口不可用，自动尝试后续端口。
func resolveWebListenAddress(preferred string) string {
	host, portStr, err := net.SplitHostPort(strings.TrimSpace(preferred))
	if err != nil {
		return preferred
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return preferred
	}

	// 尝试首选端口
	addr := fmt.Sprintf("%s:%d", host, port)
	if listener, err := net.Listen("tcp", addr); err == nil {
		listener.Close()
		return addr
	}

	// 首选不可用，依次尝试后续端口
	for offset := 1; offset <= 10; offset++ {
		candidate := fmt.Sprintf("%s:%d", host, port+offset)
		if listener, err := net.Listen("tcp", candidate); err == nil {
			listener.Close()
			return candidate
		}
	}

	// 全部不可用，返回首选让后续报错
	return preferred
}
