package api

import (
	"fmt"
	"net/http"
	"path"
	"runtime/debug"
	"slices"
	"strings"

	"github.com/vinx-lab/vinx-vocab-go/internal/auth"
	"github.com/vinx-lab/vinx-vocab-go/internal/core"
	"github.com/vinx-lab/vinx-vocab-go/internal/httpx"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
	"github.com/vinx-lab/vinx-vocab-go/internal/web"
)

// modules 所有模块的注册函数。后续任务新增 internal/api/<area>.go 后在这里加一行。
var modules = []func(r *Router, d *Deps){
	registerHealth,
	registerConfig,
	registerAuth,
	registerBooks,
	registerPlans,
	registerStudy,
	registerAI,
	registerSettings,
	registerEdition,
	registerClasses,
	registerUsers,
	registerSheets,
	registerDev,
	registerCoverage,
	registerTexts,
}

// Handler 整个程序的 HTTP 入口：/api/ 下是 API（去掉前缀后与旧版路径一一对应），其余交给嵌入的前端。
func Handler(d *Deps) http.Handler {
	api := NewRouter(d)
	for _, register := range modules {
		register(api, d)
	}
	spa := web.Handler(web.Dist())
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
			r2 := r.Clone(r.Context())
			r2.URL.Path = strings.TrimPrefix(r.URL.Path, "/api")
			r2.URL.RawPath = ""
			api.ServeHTTP(w, r2)
			return
		}
		spa.ServeHTTP(w, r)
	})
}

// Router API 路由：按方法分表（方法不匹配时与 Fastify 一样返回 404 包络，而不是 405），
// 每条路由依次经过：请求体解析（Prepare，先于匹配）→ 功能开关 → Origin 检查 → 身份解析 → 守卫 → 处理函数。
type Router struct {
	d       *Deps
	muxes   map[string]*http.ServeMux
	feature core.Feature
}

// NewRouter 空路由表。
func NewRouter(d *Deps) *Router {
	return &Router{d: d, muxes: map[string]*http.ServeMux{}}
}

// Feature 返回一个视图：经它注册的路由只在当前版本开启该功能时存在，否则按请求返回 404「接口不存在」。
// 版本在每个请求时判定（运行时切换版本后立即生效），不在启动时决定是否注册。
func (r *Router) Feature(f core.Feature) *Router {
	return &Router{d: r.d, muxes: r.muxes, feature: f}
}

func (r *Router) Get(p string, h httpx.HandlerFunc, mws ...httpx.Middleware) {
	r.Handle(http.MethodGet, p, h, mws...)
}
func (r *Router) Post(p string, h httpx.HandlerFunc, mws ...httpx.Middleware) {
	r.Handle(http.MethodPost, p, h, mws...)
}
func (r *Router) Put(p string, h httpx.HandlerFunc, mws ...httpx.Middleware) {
	r.Handle(http.MethodPut, p, h, mws...)
}
func (r *Router) Patch(p string, h httpx.HandlerFunc, mws ...httpx.Middleware) {
	r.Handle(http.MethodPatch, p, h, mws...)
}
func (r *Router) Delete(p string, h httpx.HandlerFunc, mws ...httpx.Middleware) {
	r.Handle(http.MethodDelete, p, h, mws...)
}

// Handle 注册路由。路径用 Go 1.22 ServeMux 语法（/books/{id}），处理函数里用 r.PathValue("id") 取参数。
// mws 按书写顺序从外到内执行（常见：auth.RequireCap(core.CapBooksEdit)）。
func (r *Router) Handle(method, pattern string, h httpx.HandlerFunc, mws ...httpx.Middleware) {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	feature := r.feature
	d := r.d
	final := func(w http.ResponseWriter, req *http.Request) {
		// 处理函数 panic（如事务里误用 d.DB、未挂守卫却解引用 actor）→ 500 包络并记日志，而不是断开连接
		defer func() {
			if p := recover(); p != nil {
				if p == http.ErrAbortHandler {
					panic(p)
				}
				httpx.Fail(w, req, fmt.Errorf("panic: %v\n%s", p, debug.Stack()))
			}
		}()
		req = req.WithContext(store.WithTxGuard(req.Context()))
		if err := d.routeGuards(req, method, feature); err != nil {
			httpx.Fail(w, req, err)
			return
		}
		req = d.Auth.Authenticate(req)
		// 登录者带上本次请求的版本：个人版下依赖版本功能的能力关闭、数据范围只限本人（service.Can / SeesAll）
		if actor := auth.ActorFrom(req.Context()); actor != nil {
			ed, err := d.Edition(req.Context())
			if err != nil {
				httpx.Fail(w, req, err)
				return
			}
			actor.Edition = ed.Edition
		}
		if err := h(w, req); err != nil {
			httpx.Fail(w, req, err)
		}
	}
	mux := r.muxes[method]
	if mux == nil {
		mux = http.NewServeMux()
		mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) { httpx.Fail(w, req, httpx.ErrRouteNotFound()) })
		r.muxes[method] = mux
	}
	mux.HandleFunc(pattern, final)
}

// routeGuards 功能开关与 Origin 检查。
func (d *Deps) routeGuards(r *http.Request, method string, feature core.Feature) error {
	if feature != "" {
		ed, err := d.Edition(r.Context())
		if err != nil {
			return err
		}
		if !ed.Features().Has(feature) {
			return httpx.ErrRouteNotFound()
		}
	}
	return d.checkOrigin(r, method)
}

// checkOrigin CSRF 纵深防线（主防线是 SameSite=Lax Cookie）：写请求带了 Origin 头时，
// 只放行同主机或 ALLOWED_ORIGINS 里的来源（含 "*" 时不限）。没有 Origin 头（curl、服务端调用）放行。
func (d *Deps) checkOrigin(r *http.Request, method string) error {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		return nil
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return nil
	}
	allowed := d.Cfg.AllowedOrigins
	if slices.Contains(allowed, "*") || slices.Contains(allowed, origin) {
		return nil
	}
	if host := originHost(origin); host != "" && strings.EqualFold(host, r.Host) {
		return nil
	}
	return httpx.Forbidden("非法请求来源")
}

func originHost(origin string) string {
	if i := strings.Index(origin, "://"); i >= 0 {
		return strings.TrimSuffix(origin[i+3:], "/")
	}
	return ""
}

// ServeHTTP 分发 API 请求（路径已去掉 /api 前缀）。
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	req = httpx.Prepare(w, req)
	if req == nil {
		return
	}
	method := req.Method
	if method == http.MethodHead {
		method = http.MethodGet
	}
	mux := r.muxes[method]
	// 不规整的路径（// 或 ..）ServeMux 会重定向；Fastify 直接 404
	if mux == nil || req.URL.Path == "" || path.Clean(req.URL.Path) != req.URL.Path {
		httpx.Fail(w, req, httpx.ErrRouteNotFound())
		return
	}
	if req.Method == http.MethodHead {
		req.Method = http.MethodGet
		mux.ServeHTTP(headWriter{w}, req)
		return
	}
	mux.ServeHTTP(w, req)
}

// headWriter HEAD 请求：执行 GET 处理函数但不写响应体。
type headWriter struct{ http.ResponseWriter }

func (h headWriter) Write(b []byte) (int, error) { return len(b), nil }
