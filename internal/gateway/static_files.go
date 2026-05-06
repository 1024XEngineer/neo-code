package gateway

import (
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
)

// knownAPIPrefixes 定义属于 Gateway API 的路径前缀，静态文件中间件不会拦截这些路径。
var knownAPIPrefixes = map[string]bool{
	"/healthz":      true,
	"/version":      true,
	"/rpc":          true,
	"/ws":           true,
	"/sse":          true,
	"/metrics":      true,
	"/metrics.json": true,
}

// WithStaticFileHandler 返回一个 http.Handler，将 API 请求转发给 apiHandler，
// 其余请求从 staticDir 提供静态文件。对于 SPA 路由，不存在的路径会回退到 index.html。
func WithStaticFileHandler(apiHandler http.Handler, staticDir string, logger *log.Logger) http.Handler {
	if staticDir == "" {
		return apiHandler
	}
	return WithFSStaticFileHandler(apiHandler, os.DirFS(staticDir), logger)
}

// WithFSStaticFileHandler 返回一个 http.Handler，将 API 请求转发给 apiHandler，
// 其余请求从 fsys 提供静态文件。对于 SPA 路由，不存在的路径会回退到 index.html。
func WithFSStaticFileHandler(apiHandler http.Handler, fsys fs.FS, logger *log.Logger) http.Handler {
	if fsys == nil {
		return apiHandler
	}
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		cleanPath := path.Clean("/" + request.URL.Path)

		// API 路径直接转发
		if isAPIPath(cleanPath) {
			apiHandler.ServeHTTP(writer, request)
			return
		}

		// 根路径 → index.html
		relPath := strings.TrimPrefix(cleanPath, "/")
		if relPath == "" {
			relPath = "index.html"
		}

		// 尝试从 fs.FS 中打开文件
		file, err := fsys.Open(relPath)
		if err == nil {
			stat, statErr := file.Stat()
			if statErr == nil && !stat.IsDir() {
				setCacheHeaders(writer, relPath)
				serveFileContent(writer, request, relPath, stat.ModTime(), file)
				_ = file.Close()
				return
			}
			_ = file.Close()
		}

		// SPA fallback：文件不存在时返回 index.html
		indexFile, err := fsys.Open("index.html")
		if err == nil {
			stat, statErr := indexFile.Stat()
			if statErr == nil {
				writer.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
				serveFileContent(writer, request, "index.html", stat.ModTime(), indexFile)
				_ = indexFile.Close()
				return
			}
			_ = indexFile.Close()
		}

		if logger != nil {
			logger.Printf("static files: index.html not found")
		}
		http.NotFound(writer, request)
	})
}

// serveFileContent 将 fs.File 内容写入 HTTP 响应。如果文件支持 io.ReadSeeker，
// 使用 http.ServeContent 以支持 Range 请求和 If-Modified-Since；否则回退到 io.Copy。
func serveFileContent(writer http.ResponseWriter, request *http.Request, name string, modTime time.Time, file fs.File) {
	if rs, ok := file.(io.ReadSeeker); ok {
		http.ServeContent(writer, request, name, modTime, rs)
		return
	}
	_, _ = io.Copy(writer, file)
}

// isAPIPath 判断请求路径是否属于 Gateway API。
func isAPIPath(cleanPath string) bool {
	if knownAPIPrefixes[cleanPath] {
		return true
	}
	for prefix := range knownAPIPrefixes {
		if strings.HasPrefix(cleanPath, prefix+"/") {
			return true
		}
	}
	return false
}

// setCacheHeaders 根据文件名设置缓存策略。
// Vite hashed assets（如 assets/index-BzA30N4.js）使用 immutable 缓存，
// 其他文件使用 no-cache。
func setCacheHeaders(writer http.ResponseWriter, relPath string) {
	base := path.Base(relPath)
	if strings.Contains(base, "-") && !strings.HasSuffix(base, ".html") {
		writer.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		writer.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	}
}
