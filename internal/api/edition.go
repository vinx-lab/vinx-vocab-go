package api

import (
	"net/http"

	"github.com/vinx-lab/vinx-vocab-go/internal/auth"
	"github.com/vinx-lab/vinx-vocab-go/internal/core"
	"github.com/vinx-lab/vinx-vocab-go/internal/httpx"
	"github.com/vinx-lab/vinx-vocab-go/internal/service"
)

// 版本选择与切换（Go 版新增，spec 0001 §2「版本」；旧版只能用 VINX_EDITION 在启动时指定）。
//   - POST /setup/edition：首次运行向导，仅 needsSetup 时可用，无需登录；
//   - PUT /settings/edition：管理员（system 能力）双向切换；设了 VINX_EDITION 时 403。
// 两者都返回切换后的 /config 数据。功能开关按请求判定，下一个请求即生效。

var editionValues = []string{core.EditionPersonal, core.EditionSchool}

type editionBody struct {
	Edition httpx.Opt[string] `json:"edition"`

	edition string
}

func (b *editionBody) Validate(v *httpx.V) {
	b.edition = v.Enum("edition", b.Edition, editionValues, "")
}

func registerEdition(r *Router, d *Deps) {
	r.Post("/setup/edition", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		body, err := httpx.Decode[editionBody](req)
		if err != nil {
			return err
		}
		if err := service.SetupEdition(ctx, d.DB, d.Editions, d.Now(), body.edition); err != nil {
			return err
		}
		cfg, err := d.appConfig(ctx)
		if err != nil {
			return err
		}
		httpx.OK(w, cfg)
		return nil
	})

	r.Put("/settings/edition", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		body, err := httpx.Decode[editionBody](req)
		if err != nil {
			return err
		}
		if err := service.SwitchEdition(ctx, d.DB, d.Editions, d.Now(), body.edition, actor.ID); err != nil {
			return err
		}
		cfg, err := d.appConfig(ctx)
		if err != nil {
			return err
		}
		httpx.OK(w, cfg)
		return nil
	}, auth.RequireCap(core.CapSystem))
}
