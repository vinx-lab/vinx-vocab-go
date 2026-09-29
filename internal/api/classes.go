package api

import (
	"fmt"
	"net/http"
	"regexp"

	"github.com/vinx-lab/vinx-vocab-go/internal/auth"
	"github.com/vinx-lab/vinx-vocab-go/internal/core"
	"github.com/vinx-lab/vinx-vocab-go/internal/httpx"
	"github.com/vinx-lab/vinx-vocab-go/internal/school"
	"github.com/vinx-lab/vinx-vocab-go/internal/service"
)

// 班级版：班级、成员、邀请码、批量建号、班级概览、学生侧入班 / 退班（对应旧 school/classes.routes.ts）。
// 全部经 r.Feature(core.FeatureClasses) 注册：个人版（含运行时降级）按请求 404「接口不存在」。

// classBody 旧 classSchema（建班）；classPatchBody 为 classSchema.partial()（修改）。
type classBody struct {
	Name     httpx.Opt[string] `json:"name"`
	Archived httpx.Opt[bool]   `json:"archived"`

	name     *string
	archived *bool
}

type classPatchBody classBody

func validateClass(v *httpx.V, b *classBody, partial bool) {
	checks := []httpx.StrCheck{httpx.Trim(), httpx.Min(1, "班级名称不能为空"), httpx.Max(40, "名称过长")}
	if partial {
		b.name = v.OptStr("name", b.Name, checks...)
	} else {
		s := v.Str("name", b.Name, checks...)
		b.name = &s
	}
	if b.Archived.Set && !v.Has("archived") {
		a := v.OptBool("archived", b.Archived, false)
		if !b.Archived.Null {
			b.archived = &a
		}
	}
}

func (b *classBody) Validate(v *httpx.V)      { validateClass(v, b, false) }
func (b *classPatchBody) Validate(v *httpx.V) { validateClass(v, (*classBody)(b), true) }

type accountBody struct {
	Account httpx.Opt[string] `json:"account"`
	account string
}

func (b *accountBody) Validate(v *httpx.V) {
	b.account = v.Str("account", b.Account, httpx.Trim(), httpx.Min(1, "请输入账号"), httpx.Lower())
}

var batchPrefixRe = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,19}$`)

type batchBody struct {
	Names    httpx.Opt[[]string] `json:"names"`
	Prefix   httpx.Opt[string]   `json:"prefix"`
	Password httpx.Opt[string]   `json:"password"`

	names            []string
	prefix, password string
}

func (b *batchBody) Validate(v *httpx.V) {
	if !v.Has("names") {
		switch {
		case !b.Names.Set:
			v.Add("names", "Required")
		case b.Names.Null:
			v.Add("names", "Expected array, received null")
		default:
			b.names = make([]string, 0, len(b.Names.Val))
			for i, n := range b.Names.Val {
				b.names = append(b.names, v.Str(fmt.Sprintf("names.%d", i), httpx.Some(n), httpx.Trim(), httpx.Min(1), httpx.Max(30)))
			}
			v.ArrayLen("names", len(b.Names.Val), 1, 100, "请至少填写一个姓名", "一次最多 100 人")
		}
	}
	b.prefix = v.Str("prefix", b.Prefix, httpx.Trim(), httpx.Lower(), httpx.Regex(batchPrefixRe, "账号前缀需以字母开头，2–20 位字母数字"))
	b.password = v.Str("password", b.Password, httpx.Min(6, "初始密码至少 6 位"))
}

type newPasswordBody struct {
	NewPassword httpx.Opt[string] `json:"newPassword"`
	newPassword string
}

func (b *newPasswordBody) Validate(v *httpx.V) {
	b.newPassword = v.Str("newPassword", b.NewPassword, httpx.Min(1, "新密码不能为空"))
}

type joinBody struct {
	InviteCode httpx.Opt[string] `json:"inviteCode"`
	inviteCode string
}

func (b *joinBody) Validate(v *httpx.V) {
	b.inviteCode = v.Str("inviteCode", b.InviteCode, httpx.Trim(), httpx.Min(4, "请输入邀请码"))
}

func registerClasses(r *Router, d *Deps) {
	cr := r.Feature(core.FeatureClasses)
	guard := auth.RequireCap(core.CapClasses)

	// manage 当前操作者可管理该班级（不存在 404，不是班主任 403）。
	manage := func(req *http.Request) (*service.Actor, string, error) {
		actor := auth.ActorFrom(req.Context())
		id := req.PathValue("id")
		return actor, id, service.AssertCanManageClass(req.Context(), d.DB, actor, id)
	}

	cr.Get("/classes", func(w http.ResponseWriter, req *http.Request) error {
		items, err := school.ListClasses(req.Context(), d.DB, auth.ActorFrom(req.Context()))
		if err != nil {
			return err
		}
		httpx.OK(w, httpx.List(items))
		return nil
	}, guard)

	cr.Post("/classes", func(w http.ResponseWriter, req *http.Request) error {
		actor := auth.ActorFrom(req.Context())
		body, err := httpx.Decode[classBody](req)
		if err != nil {
			return err
		}
		cls, err := school.CreateClass(req.Context(), d.DB, d.Now(), actor.ID, *body.name)
		if err != nil {
			return err
		}
		httpx.OK(w, cls)
		return nil
	}, guard)

	cr.Get("/classes/{id}", func(w http.ResponseWriter, req *http.Request) error {
		actor, id, err := manage(req)
		if err != nil {
			return err
		}
		detail, err := school.GetClassDetail(req.Context(), d.DB, actor, id)
		if err != nil {
			return err
		}
		httpx.OK(w, detail)
		return nil
	}, guard)

	cr.Patch("/classes/{id}", func(w http.ResponseWriter, req *http.Request) error {
		_, id, err := manage(req)
		if err != nil {
			return err
		}
		body, err := httpx.Decode[classPatchBody](req)
		if err != nil {
			return err
		}
		cls, err := school.UpdateClass(req.Context(), d.DB, d.Now(), id, school.ClassPatch{Name: body.name, Archived: body.archived})
		if err != nil {
			return err
		}
		httpx.OK(w, cls)
		return nil
	}, guard)

	cr.Delete("/classes/{id}", func(w http.ResponseWriter, req *http.Request) error {
		_, id, err := manage(req)
		if err != nil {
			return err
		}
		if err := school.DeleteClass(req.Context(), d.DB, id); err != nil {
			return err
		}
		httpx.OK(w, nil)
		return nil
	}, guard)

	cr.Post("/classes/{id}/invite-code", func(w http.ResponseWriter, req *http.Request) error {
		_, id, err := manage(req)
		if err != nil {
			return err
		}
		cls, err := school.RotateInviteCode(req.Context(), d.DB, d.Now(), id)
		if err != nil {
			return err
		}
		httpx.OK(w, cls)
		return nil
	}, guard)

	cr.Get("/classes/{id}/overview", func(w http.ResponseWriter, req *http.Request) error {
		_, id, err := manage(req)
		if err != nil {
			return err
		}
		v, err := service.ClassOverview(req.Context(), d.DB, d.Cfg.Location, d.Now(), id)
		if err != nil {
			return err
		}
		httpx.OK(w, v)
		return nil
	}, guard)

	// 按账号添加已有学生：老师只能添加自己批量创建的账号（其他学生需凭邀请码自愿加入）；管理员不限
	cr.Post("/classes/{id}/members", func(w http.ResponseWriter, req *http.Request) error {
		actor, id, err := manage(req)
		if err != nil {
			return err
		}
		body, err := httpx.Decode[accountBody](req)
		if err != nil {
			return err
		}
		m, err := school.AddMemberByAccount(req.Context(), d.DB, d.Now(), actor, id, body.account)
		if err != nil {
			return err
		}
		httpx.OK(w, m)
		return nil
	}, guard)

	// 批量创建学生账号并入班（账号 = 前缀 + 两位序号）
	cr.Post("/classes/{id}/members/batch", func(w http.ResponseWriter, req *http.Request) error {
		actor := auth.ActorFrom(req.Context())
		if err := service.AssertCan(actor, core.CapUsers, ""); err != nil {
			return err
		}
		_, id, err := manage(req)
		if err != nil {
			return err
		}
		body, err := httpx.Decode[batchBody](req)
		if err != nil {
			return err
		}
		if msg := core.ValidatePassword(body.password); msg != "" {
			return httpx.Validation(msg)
		}
		hash, err := auth.HashPassword(body.password)
		if err != nil {
			return err
		}
		created, err := school.BatchCreateMembers(req.Context(), d.DB, d.Now(), actor.ID, id, body.prefix, hash, body.names)
		if err != nil {
			return err
		}
		httpx.OK(w, map[string]any{"created": created})
		return nil
	}, guard)

	cr.Delete("/classes/{id}/members/{userId}", func(w http.ResponseWriter, req *http.Request) error {
		_, id, err := manage(req)
		if err != nil {
			return err
		}
		if err := school.RemoveMember(req.Context(), d.DB, id, req.PathValue("userId")); err != nil {
			return err
		}
		httpx.OK(w, nil)
		return nil
	}, guard)

	cr.Post("/classes/{id}/members/{userId}/reset-password", func(w http.ResponseWriter, req *http.Request) error {
		actor, id, err := manage(req)
		if err != nil {
			return err
		}
		body, err := httpx.Decode[newPasswordBody](req)
		if err != nil {
			return err
		}
		if msg := core.ValidatePassword(body.newPassword); msg != "" {
			return httpx.Validation(msg)
		}
		userID := req.PathValue("userId")
		// 老师只能重置自己创建的学生账号；自行注册的学生由本人或管理员处理，防止账号被接管（K21）
		if err := school.CheckMemberPasswordReset(req.Context(), d.DB, actor, id, userID); err != nil {
			return err
		}
		hash, err := auth.HashPassword(body.newPassword)
		if err != nil {
			return err
		}
		if err := service.UpdatePassword(req.Context(), d.DB, d.Now(), userID, hash); err != nil {
			return err
		}
		httpx.OK(w, nil)
		return nil
	}, auth.RequireCap(core.CapUsers))

	// ===== 学生侧 =====

	cr.Post("/classes/join", func(w http.ResponseWriter, req *http.Request) error {
		actor := auth.ActorFrom(req.Context())
		body, err := httpx.Decode[joinBody](req)
		if err != nil {
			return err
		}
		if service.Can(actor, core.CapClasses) {
			return httpx.Forbidden("教师和管理员账号不能加入班级")
		}
		cls, err := school.JoinByCode(req.Context(), d.DB, d.Now(), actor.ID, body.inviteCode)
		if err != nil {
			return err
		}
		httpx.OK(w, map[string]string{"id": cls.ID, "name": cls.Name})
		return nil
	}, auth.RequireCap(core.CapStudy))

	cr.Get("/me/classes", func(w http.ResponseWriter, req *http.Request) error {
		items, err := school.MyClasses(req.Context(), d.DB, auth.ActorFrom(req.Context()).ID)
		if err != nil {
			return err
		}
		httpx.OK(w, httpx.List(items))
		return nil
	}, auth.RequireAuth)

	cr.Delete("/me/classes/{id}", func(w http.ResponseWriter, req *http.Request) error {
		actor := auth.ActorFrom(req.Context())
		if err := school.RemoveMember(req.Context(), d.DB, req.PathValue("id"), actor.ID); err != nil {
			return err
		}
		httpx.OK(w, nil)
		return nil
	}, auth.RequireAuth)
}
