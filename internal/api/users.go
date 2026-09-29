package api

import (
	"net/http"

	"github.com/vinx-lab/vinx-vocab-go/internal/auth"
	"github.com/vinx-lab/vinx-vocab-go/internal/core"
	"github.com/vinx-lab/vinx-vocab-go/internal/httpx"
	"github.com/vinx-lab/vinx-vocab-go/internal/service"
)

// 用户管理（班级版，对应旧 routes/users.ts）：教师可建学生账号，管理员可建任意角色并改角色。
// 经 r.Feature(core.FeatureMultiUser) 注册：个人版（含运行时降级）按请求 404。

type usersQuery struct {
	Page  httpx.Opt[string] `json:"page"`
	Limit httpx.Opt[string] `json:"limit"`
	Q     httpx.Opt[string] `json:"q"`

	page, limit int
	q           string
}

func (q *usersQuery) Validate(v *httpx.V) {
	one, fifty := 1, 50
	q.page = v.CoerceInt("page", q.Page, &one, httpx.IntRange{Min: &one})
	q.limit = v.CoerceInt("limit", q.Limit, &fifty, httpx.Between(1, 500))
	if s := v.OptStr("q", q.Q, httpx.Trim()); s != nil {
		q.q = *s
	}
}

type createUserBody struct {
	Email    httpx.Opt[string] `json:"email"`
	Name     httpx.Opt[string] `json:"name"`
	Password httpx.Opt[string] `json:"password"`
	Role     httpx.Opt[string] `json:"role"`

	email, name, password, role string
}

func (b *createUserBody) Validate(v *httpx.V) {
	b.email = v.Str("email", b.Email, httpx.Trim(), httpx.Min(2, "账号至少 2 位"), httpx.Max(100), httpx.Lower())
	b.name = v.Str("name", b.Name, httpx.Trim(), httpx.Min(1, "姓名不能为空"), httpx.Max(50))
	b.password = v.Str("password", b.Password, httpx.Min(1, "密码不能为空"))
	b.role = v.OptEnum("role", b.Role, core.Roles, "", core.RoleStudent)
}

type roleBody struct {
	Role httpx.Opt[string] `json:"role"`
	role string
}

func (b *roleBody) Validate(v *httpx.V) {
	b.role = v.Enum("role", b.Role, core.Roles, "")
}

func registerUsers(r *Router, d *Deps) {
	ur := r.Feature(core.FeatureMultiUser)
	guard := auth.RequireCap(core.CapUsers)

	// 老师只看自己创建的账号与本班学生；管理员看全部。新建的在前。
	ur.Get("/users", func(w http.ResponseWriter, req *http.Request) error {
		q, err := httpx.DecodeQuery[usersQuery](req)
		if err != nil {
			return err
		}
		page, err := service.ListManagedUsers(req.Context(), d.DB, auth.ActorFrom(req.Context()), q.q, q.page, q.limit)
		if err != nil {
			return err
		}
		httpx.OK(w, page)
		return nil
	}, guard)

	ur.Post("/users", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		body, err := httpx.Decode[createUserBody](req)
		if err != nil {
			return err
		}
		if body.role != core.RoleStudent && !service.IsAdmin(actor) {
			return httpx.Forbidden("只有管理员可以创建教师或管理员账号")
		}
		if msg := core.ValidatePassword(body.password); msg != "" {
			return httpx.Validation(msg)
		}
		existing, err := service.FindUserByEmail(ctx, d.DB, body.email)
		if err != nil {
			return err
		}
		if existing != nil {
			return httpx.NewError(httpx.CodeEmailExists, "账号已存在", nil)
		}
		hash, err := auth.HashPassword(body.password)
		if err != nil {
			return err
		}
		creator := actor.ID
		u, err := service.CreateUser(ctx, d.DB, d.Now(), service.NewUser{Email: body.email, Name: body.name, Role: body.role, PasswordHash: hash, CreatedByID: &creator})
		if err != nil {
			return err
		}
		httpx.OK(w, map[string]string{"id": u.ID, "email": u.Email, "name": u.Name, "role": u.Role})
		return nil
	}, guard)

	// 改角色（仅管理员，且不能把自己降级导致无人可管）
	ur.Patch("/users/{id}", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		body, err := httpx.Decode[roleBody](req)
		if err != nil {
			return err
		}
		if err := service.ChangeUserRole(ctx, d.DB, d.Now(), actor, req.PathValue("id"), body.role); err != nil {
			return err
		}
		httpx.OK(w, map[string]string{"id": req.PathValue("id"), "role": body.role})
		return nil
	}, auth.RequireCap(core.CapSystem))

	// 重置密码：管理员任意账号；老师仅限自己创建的学生账号
	ur.Post("/users/{id}/reset-password", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		body, err := httpx.Decode[newPasswordBody](req)
		if err != nil {
			return err
		}
		if msg := core.ValidatePassword(body.newPassword); msg != "" {
			return httpx.Validation(msg)
		}
		target, err := service.FindUserByID(ctx, d.DB, req.PathValue("id"))
		if err != nil {
			return err
		}
		if target == nil {
			return httpx.NotFound("用户不存在")
		}
		if !service.IsAdmin(actor) && (target.CreatedByID == nil || *target.CreatedByID != actor.ID || target.Role != core.RoleStudent) {
			return httpx.Forbidden("只能重置自己创建的学生账号的密码")
		}
		hash, err := auth.HashPassword(body.newPassword)
		if err != nil {
			return err
		}
		if err := service.UpdatePassword(ctx, d.DB, d.Now(), target.ID, hash); err != nil {
			return err
		}
		httpx.OK(w, nil)
		return nil
	}, guard)
}
