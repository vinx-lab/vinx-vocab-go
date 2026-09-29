package api

import (
	"net/http"

	"github.com/vinx-lab/vinx-vocab-go/internal/httpx"
)

// InstanceHeader 本程序的健康检查响应头：启动时据此识别「同端口上已在运行的是不是本程序」。
const InstanceHeader = "X-Vinx-Vocab"

func registerHealth(r *Router, d *Deps) {
	r.Get("/health", func(w http.ResponseWriter, req *http.Request) error {
		w.Header().Set(InstanceHeader, d.Version)
		httpx.OK(w, map[string]string{"status": "ok"})
		return nil
	})
}
