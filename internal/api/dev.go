package api

import (
	"net/http"

	"github.com/vinx-lab/vinx-vocab-go/internal/httpx"
	"github.com/vinx-lab/vinx-vocab-go/internal/service"
)

// 开发模式（serve --dev，spec 0002）：免密切换账号。不加 --dev 时这些路由不注册，请求与未知路径一样返回 404。
// 签发的令牌与正常登录相同，后端其余代码不区分令牌来源；写请求照常经过 Origin 检查。

const (
	devScopeTab     = "tab"
	devScopeBrowser = "browser"
)

type impersonateBody struct {
	UserID httpx.Opt[string] `json:"userId"`
	Scope  httpx.Opt[string] `json:"scope"`

	userID, scope string
}

func (b *impersonateBody) Validate(v *httpx.V) {
	b.userID = v.Str("userId", b.UserID, httpx.Min(1, "userId 不能为空"))
	b.scope = v.Enum("scope", b.Scope, []string{devScopeTab, devScopeBrowser}, "scope 只能是 tab 或 browser")
}

// impersonateResult scope=tab 时带 token（前端存进 sessionStorage，用 Bearer 头发送）；scope=browser 时不带。
type impersonateResult struct {
	User  service.UserView `json:"user"`
	Token string           `json:"token,omitempty"`
}

func registerDev(r *Router, d *Deps) {
	if !d.Cfg.Dev {
		return
	}

	// 全部账号（无需登录）：管理员 → 老师 → 学生，同角色按邮箱
	r.Get("/dev/users", func(w http.ResponseWriter, req *http.Request) error {
		users, err := service.ListDevUsers(req.Context(), d.DB)
		if err != nil {
			return err
		}
		httpx.OK(w, users)
		return nil
	})

	// 免密切换：browser 与正常登录一样写 Cookie；tab 不写 Cookie，只返回令牌
	r.Post("/dev/impersonate", func(w http.ResponseWriter, req *http.Request) error {
		body, err := httpx.Decode[impersonateBody](req)
		if err != nil {
			return err
		}
		user, err := service.FindUserByID(req.Context(), d.DB, body.userID)
		if err != nil {
			return err
		}
		if user == nil {
			return httpx.NotFound("用户不存在")
		}
		out := impersonateResult{User: service.ToUserView(user)}
		if body.scope == devScopeBrowser {
			if err := d.Auth.SignIn(w, req, user); err != nil {
				return err
			}
		} else {
			if out.Token, err = d.Auth.Sign(user.ID, user.Email, user.Name, user.Role); err != nil {
				return err
			}
		}
		httpx.OK(w, out)
		return nil
	})
}
