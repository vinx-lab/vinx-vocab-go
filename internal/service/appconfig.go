package service

import (
	"context"

	"github.com/vinx-lab/vinx-vocab-go/internal/config"
	"github.com/vinx-lab/vinx-vocab-go/internal/core"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// AppConfig GET /config 的返回（旧 shared AppConfig）。
type AppConfig struct {
	Edition       core.Edition  `json:"edition"`
	Features      core.Features `json:"features"`
	AI            bool          `json:"ai"`
	Audio         bool          `json:"audio"`
	SignupEnabled bool          `json:"signupEnabled"`
	// EditionLocked 版本由 VINX_EDITION 指定，界面上不可切换（Go 版新增）。
	EditionLocked bool `json:"editionLocked"`
	// NeedsSetup 首次运行：未锁定、未选择版本且还没有账号时，前端先进入版本向导（Go 版新增，见 NeedsSetup）。
	NeedsSetup bool `json:"needsSetup"`
}

// SignupEnabled 个人版：只允许存在一个账号；班级版：开放注册（可用 SIGNUP_ENABLED 关闭）。
// 全新安装（库里还没有任何账号）时忽略 SIGNUP_ENABLED：否则照抄旧 .env 把它设成 false 的人，
// 全新安装会连第一个管理员都建不出来，陷入死路（K44）。
func SignupEnabled(ctx context.Context, q store.Querier, cfg *config.Config, ed EditionState) (bool, error) {
	n, err := CountUsers(ctx, q)
	if err != nil {
		return false, err
	}
	if n == 0 {
		return true, nil
	}
	if !cfg.SignupEnabled {
		return false, nil
	}
	if ed.Features().MultiUser {
		return true, nil
	}
	return false, nil
}

// AudioEnabled 真人发音是否可用（配置了发音源）。
func AudioEnabled(cfg *config.Config) bool { return cfg.AudioProviderURL != "" }
