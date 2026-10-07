package api

import (
	"context"
	"net/http"

	"github.com/vinx-lab/vinx-vocab-go/internal/httpx"
	"github.com/vinx-lab/vinx-vocab-go/internal/service"
)

// appConfig 当前运行时配置（GET /config，以及选择 / 切换版本后的返回值）。
func (d *Deps) appConfig(ctx context.Context) (*service.AppConfig, error) {
	ed, err := d.Edition(ctx)
	if err != nil {
		return nil, err
	}
	signup, err := service.SignupEnabled(ctx, d.DB, d.Cfg, ed)
	if err != nil {
		return nil, err
	}
	needsSetup, err := service.NeedsSetup(ctx, d.DB, ed)
	if err != nil {
		return nil, err
	}
	return &service.AppConfig{
		Edition:       ed.Edition,
		Features:      ed.Features(),
		AI:            d.AIEnabled(ctx),
		Audio:         service.AudioEnabled(d.Cfg),
		SignupEnabled: signup,
		EditionLocked: ed.Locked,
		NeedsSetup:    needsSetup,
		Dev:           d.Cfg.Dev,
		Version:       d.Version,
	}, nil
}

// 运行时配置：版本、可用功能、AI / 发音能力、版本是否锁定、是否需要首次运行向导。前端启动时读取一次。无需登录。
func registerConfig(r *Router, d *Deps) {
	r.Get("/config", func(w http.ResponseWriter, req *http.Request) error {
		cfg, err := d.appConfig(req.Context())
		if err != nil {
			return err
		}
		httpx.OK(w, cfg)
		return nil
	})
}
