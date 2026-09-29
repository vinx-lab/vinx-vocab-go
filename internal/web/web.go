// Package web 嵌入前端构建产物（internal/web/dist，由 make build-web 从 web/dist 复制而来）并提供 SPA 回退。
// dist 里只有占位页 placeholder.html 入库（go:embed 需要目录非空），构建产物不入库（见 .gitignore）。
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// Dist 前端产物文件系统（根目录即 dist/）。
func Dist() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}

// Handler 静态文件 + SPA 回退：
//   - 存在的文件原样返回（/assets/ 下的带哈希文件长期缓存）；
//   - 带扩展名但不存在的路径 → 404；
//   - 其余路径（前端路由）→ index.html（不缓存）。
func Handler(files fs.FS) http.Handler {
	fileServer := http.FileServerFS(files)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		p := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if p != "" && p != "index.html" {
			if st, err := fs.Stat(files, p); err == nil && !st.IsDir() {
				if strings.HasPrefix(p, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				fileServer.ServeHTTP(w, r)
				return
			}
			if path.Ext(p) != "" {
				http.NotFound(w, r)
				return
			}
		}
		serveIndex(w, r, files)
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request, files fs.FS) {
	b, err := fs.ReadFile(files, "index.html")
	if err != nil {
		// 没跑过 make build-web：dist 里只有入库的占位页
		b, err = fs.ReadFile(files, "placeholder.html")
	}
	if err != nil {
		http.Error(w, "index.html missing", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	if r.Method == http.MethodHead {
		return
	}
	w.Write(b)
}
