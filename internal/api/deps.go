// Package api HTTP 路由与处理函数（对应旧 apps/api/src/routes 与 school/*.routes.ts）。
//
// 约定（详见 docs/architecture.md）：
//   - 每个模块一个文件 <area>.go，提供 registerXxx(r *Router, d *Deps)，并在 router.go 的 modules 列表加一行；
//   - 路由路径与旧版一致（不带 /api 前缀），整体挂在 /api/ 下；
//   - 处理函数签名 func(w, r) error：成功时自己调用 httpx.OK / httpx.Created，失败直接 return 错误；
//   - 只做参数校验与权限，业务规则放 internal/core，装配与事务放 internal/service。
package api

import (
	"context"
	"time"

	"github.com/vinx-lab/vinx-vocab-go/internal/auth"
	"github.com/vinx-lab/vinx-vocab-go/internal/config"
	"github.com/vinx-lab/vinx-vocab-go/internal/core"
	coreai "github.com/vinx-lab/vinx-vocab-go/internal/core/ai"
	"github.com/vinx-lab/vinx-vocab-go/internal/service"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// Deps 处理函数共享的依赖。字段在启动时装配好，处理函数只读。
type Deps struct {
	DB       *store.DB
	Cfg      *config.Config
	Auth     *auth.Auth
	Editions *service.Editions
	// Clock 当前时间（测试注入固定时钟，覆盖跨午夜等边界）。
	Clock func() time.Time
	// Version 程序版本（/api/health 的 X-Vinx-Vocab 头，用于识别本机已在运行的实例）。
	Version string
	// AIEnabled /config 的 ai 字段：页面保存的配置优先，其次环境变量（A7）。
	AIEnabled func(ctx context.Context) bool
	// AIConfig 当前生效的 AI 配置缓存（数据库优先，其次环境变量），保存/恢复设置后调用 Invalidate。
	AIConfig *service.AIConfigCache
	// AIJobs AI 生成后台任务的内存存放（进程内，重启丢失）。
	AIJobs *coreai.JobStore
	// Rand 学习组出题 / 随机选词的随机源（[0,1)）；测试注入 core.SeededRng 得到确定结果。
	Rand core.Rng
}

// NewDeps 按配置装配默认依赖；clock 为 nil 时用 time.Now。
func NewDeps(db *store.DB, cfg *config.Config, clock func() time.Time) *Deps {
	if clock == nil {
		clock = time.Now
	}
	d := &Deps{DB: db, Cfg: cfg, Clock: clock, Version: "dev", Rand: core.DefaultRng}
	d.Auth = auth.New(cfg.JWTSecret, cfg.JWTExpiresIn, cfg.CookieSecure, clock)
	d.Editions = &service.Editions{DB: db, Cfg: cfg}
	d.AIConfig = service.NewAIConfigCache()
	d.AIJobs = coreai.NewJobStore(coreai.JobStoreOptions{})
	d.AIEnabled = func(ctx context.Context) bool { return d.AIConfig.AIEnabled(ctx, db, cfg) }
	return d
}

// Now 当前时间（经 Clock）。
func (d *Deps) Now() time.Time { return d.Clock() }

// Edition 当前请求的版本（按请求判定，支持运行时切换）。
func (d *Deps) Edition(ctx context.Context) (service.EditionState, error) {
	return d.Editions.Current(ctx)
}
