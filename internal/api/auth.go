package api

import (
	"database/sql"
	"net/http"
	"strings"

	"github.com/vinx-lab/vinx-vocab-go/internal/auth"
	"github.com/vinx-lab/vinx-vocab-go/internal/core"
	"github.com/vinx-lab/vinx-vocab-go/internal/httpx"
	"github.com/vinx-lab/vinx-vocab-go/internal/school"
	"github.com/vinx-lab/vinx-vocab-go/internal/service"
)

// ===== 入参 =====

type signupBody struct {
	Email      httpx.Opt[string] `json:"email"`
	Password   httpx.Opt[string] `json:"password"`
	Name       httpx.Opt[string] `json:"name"`
	InviteCode httpx.Opt[string] `json:"inviteCode"`

	email, password  string
	name, inviteCode *string
}

func (b *signupBody) Validate(v *httpx.V) {
	b.email = v.Str("email", b.Email, httpx.Email("邮箱格式不正确"), httpx.Lower())
	b.password = v.Str("password", b.Password, httpx.Min(1, "密码不能为空"))
	b.name = v.OptStr("name", b.Name, httpx.Min(1), httpx.Max(50))
	b.inviteCode = v.OptStr("inviteCode", b.InviteCode, httpx.Max(20))
}

// 登录账号：邮箱或老师批量创建的学号账号（非邮箱格式）
type loginBody struct {
	Email    httpx.Opt[string] `json:"email"`
	Password httpx.Opt[string] `json:"password"`

	email, password string
}

func (b *loginBody) Validate(v *httpx.V) {
	b.email = v.Str("email", b.Email, httpx.Trim(), httpx.Min(1, "账号不能为空"), httpx.Lower())
	b.password = v.Str("password", b.Password, httpx.Min(1, "密码不能为空"))
}

type changePasswordBody struct {
	OldPassword     httpx.Opt[string] `json:"oldPassword"`
	NewPassword     httpx.Opt[string] `json:"newPassword"`
	ConfirmPassword httpx.Opt[string] `json:"confirmPassword"`

	oldPassword, newPassword, confirmPassword string
}

func (b *changePasswordBody) Validate(v *httpx.V) {
	b.oldPassword = v.Str("oldPassword", b.OldPassword, httpx.Min(1, "旧密码不能为空"))
	b.newPassword = v.Str("newPassword", b.NewPassword, httpx.Min(1, "新密码不能为空"))
	b.confirmPassword = v.Str("confirmPassword", b.ConfirmPassword, httpx.Min(1, "确认密码不能为空"))
}

// 各字段都可单独提交：外观切换只发 theme
type profileBody struct {
	Name         httpx.Opt[string] `json:"name"`
	CurrentGrade httpx.Opt[string] `json:"currentGrade"`
	Theme        httpx.Opt[string] `json:"theme"`

	name         *string
	currentGrade httpx.Opt[string]
	theme        string
}

func (b *profileBody) Validate(v *httpx.V) {
	b.name = v.OptStr("name", b.Name, httpx.Min(1), httpx.Max(50, "姓名过长"))
	b.currentGrade = v.NullStr("currentGrade", b.CurrentGrade, httpx.Max(20))
	b.theme = v.OptEnum("theme", b.Theme, core.ThemePrefs, "外观只能是 system / light / dark", "")
}

// loginResult 登录 / 注册成功返回 { user }。
type loginResult struct {
	User service.UserView `json:"user"`
}

func registerAuth(r *Router, d *Deps) {
	// 注册（公开，201）
	r.Post("/auth/signup", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		body, err := httpx.Decode[signupBody](req)
		if err != nil {
			return err
		}
		if msg := core.ValidatePassword(body.password); msg != "" {
			return httpx.Validation(msg)
		}
		ed, err := d.Edition(ctx)
		if err != nil {
			return err
		}
		features := ed.Features()
		ok, err := service.SignupEnabled(ctx, d.DB, d.Cfg, ed)
		if err != nil {
			return err
		}
		if !ok {
			if features.MultiUser {
				return httpx.Forbidden("当前未开放注册")
			}
			return httpx.Forbidden("个人版只允许一个账号，请直接登录")
		}
		existing, err := service.FindUserByEmail(ctx, d.DB, body.email)
		if err != nil {
			return err
		}
		if existing != nil {
			return httpx.NewError(httpx.CodeEmailExists, "该邮箱已注册", nil)
		}
		// 个人版：第一个账号就是使用者本人，直接给管理员角色（自己管自己的词书）
		role := core.RoleAdmin
		if features.MultiUser {
			role = core.RoleStudent
		}
		var joinClass *school.ClassRef
		if features.Classes && body.inviteCode != nil && httpx.JSTrim(*body.inviteCode) != "" {
			if joinClass, err = school.FindClassByInviteCode(ctx, d.DB, *body.inviteCode); err != nil {
				return err
			}
		}
		name := strings.SplitN(body.email, "@", 2)[0]
		if body.name != nil {
			name = *body.name
		}
		hash, err := auth.HashPassword(body.password)
		if err != nil {
			return err
		}
		var user *service.UserRow
		err = d.DB.Tx(ctx, func(tx *sql.Tx) error {
			// 事务内（写锁）重新数账号：个人版并发注册只放行一个；库里还没有任何账号时（全新安装，
			// 无 seed），第一个账号在班级版下也成为管理员，否则没人能建老师账号（Go 版新增，旧版靠 seed 建管理员）。
			n, err := service.CountUsers(ctx, tx)
			if err != nil {
				return err
			}
			if !features.MultiUser && n > 0 {
				return httpx.Forbidden("个人版只允许一个账号，请直接登录")
			}
			role := role
			if n == 0 {
				role = core.RoleAdmin
			}
			if user, err = service.CreateUser(ctx, tx, d.Now(), service.NewUser{Email: body.email, Name: name, Role: role, PasswordHash: hash}); err != nil {
				return err
			}
			if joinClass != nil {
				return school.AddMember(ctx, tx, d.Now(), joinClass.ID, user.ID)
			}
			return nil
		})
		if err != nil {
			return err
		}
		if err := d.Auth.SignIn(w, req, user); err != nil {
			return err
		}
		httpx.Created(w, loginResult{User: service.ToUserView(user)})
		return nil
	})

	// 登录（公开）：种 HttpOnly Cookie
	r.Post("/auth/login", func(w http.ResponseWriter, req *http.Request) error {
		body, err := httpx.Decode[loginBody](req)
		if err != nil {
			return err
		}
		user, err := service.FindUserByEmail(req.Context(), d.DB, body.email)
		if err != nil {
			return err
		}
		if user == nil || !auth.VerifyPassword(body.password, user.PasswordHash) {
			return httpx.Unauthorized("账号或密码错误")
		}
		if err := d.Auth.SignIn(w, req, user); err != nil {
			return err
		}
		httpx.OK(w, loginResult{User: service.ToUserView(user)})
		return nil
	})

	// 登出：清 Cookie（JWT 无状态，客户端删除 Cookie 后即失效）
	r.Post("/auth/logout", func(w http.ResponseWriter, req *http.Request) error {
		d.Auth.ClearCookie(w)
		httpx.OK(w, nil)
		return nil
	}, auth.RequireAuth)

	// 当前用户
	r.Get("/auth/me", func(w http.ResponseWriter, req *http.Request) error {
		user, err := service.FindUserByID(req.Context(), d.DB, auth.ActorFrom(req.Context()).ID)
		if err != nil {
			return err
		}
		if user == nil {
			return httpx.NotFound("用户不存在")
		}
		httpx.OK(w, service.ToUserView(user))
		return nil
	}, auth.RequireAuth)

	// 修改密码
	r.Post("/auth/change-password", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		body, err := httpx.Decode[changePasswordBody](req)
		if err != nil {
			return err
		}
		if body.newPassword != body.confirmPassword {
			return httpx.Validation("两次输入的新密码不一致")
		}
		if msg := core.ValidatePassword(body.newPassword); msg != "" {
			return httpx.Validation(msg)
		}
		user, err := service.FindUserByID(ctx, d.DB, auth.ActorFrom(ctx).ID)
		if err != nil {
			return err
		}
		if user == nil {
			return httpx.NotFound("用户不存在")
		}
		if !auth.VerifyPassword(body.oldPassword, user.PasswordHash) {
			return httpx.Validation("旧密码不正确")
		}
		hash, err := auth.HashPassword(body.newPassword)
		if err != nil {
			return err
		}
		if err := service.UpdatePassword(ctx, d.DB, d.Now(), user.ID, hash); err != nil {
			return err
		}
		httpx.OK(w, nil)
		return nil
	}, auth.RequireAuth)

	// 更新个人资料（学生可改 currentGrade；外观偏好 theme 跟账号走）
	r.Put("/auth/profile", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		body, err := httpx.Decode[profileBody](req)
		if err != nil {
			return err
		}
		userID := auth.ActorFrom(ctx).ID
		user, err := service.FindUserByID(ctx, d.DB, userID)
		if err != nil {
			return err
		}
		if user == nil {
			return httpx.NotFound("用户不存在")
		}
		patch := service.ProfilePatch{Name: body.name}
		if body.currentGrade.Set {
			patch.CurrentGrade = &sql.NullString{String: body.currentGrade.Val, Valid: !body.currentGrade.Null}
		}
		if body.theme != "" {
			patch.Theme = &body.theme
		}
		if err := service.UpdateProfile(ctx, d.DB, d.Now(), userID, patch); err != nil {
			return err
		}
		httpx.OK(w, nil)
		return nil
	}, auth.RequireAuth)
}
