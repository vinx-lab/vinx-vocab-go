package api

import (
	"context"
	"fmt"
	"net/http"
	"slices"

	"github.com/vinx-lab/vinx-vocab-go/internal/auth"
	"github.com/vinx-lab/vinx-vocab-go/internal/core"
	"github.com/vinx-lab/vinx-vocab-go/internal/httpx"
	"github.com/vinx-lab/vinx-vocab-go/internal/service"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// 单词单（对应旧 routes/sheets.ts）：预览、批量生成、列表、明细、删除、今日下一份（NextSheet 在 study.go 的 /today 里用）。

var sheetSourceKindValues = []string{"unfamiliar", "session", "unit", "book"}

// ===== 请求体 =====

// sheetSourceBody 选词来源（旧 discriminatedUnion("kind", [...])）。
type sheetSourceBody struct {
	Kind      httpx.Opt[string] `json:"kind"`
	SessionID httpx.Opt[string] `json:"sessionId"`
	UnitID    httpx.Opt[string] `json:"unitId"`
	BookID    httpx.Opt[string] `json:"bookId"`
}

// validateSheetSource 手动实现 zod discriminatedUnion 的行为：kind 不在枚举里（含缺省、null、类型错）
// 统一报 "Invalid discriminator value" 于 "<path>.kind"，覆盖解码阶段可能记录的类型错误；
// kind 合法时按对应分支校验专属字段（sessionId / unitId / bookId，z.string().min(1)）。
func validateSheetSource(v *httpx.V, path string, o httpx.Opt[sheetSourceBody]) *service.SheetSource {
	if !o.Set {
		return nil
	}
	if v.Has(path) {
		// 解码阶段已记录 "source" 级别的类型错误（如 Expected object, received string）
		return nil
	}
	if o.Null {
		v.Add(path, "Expected object, received null")
		return nil
	}
	b := o.Val
	kindPath := path + ".kind"
	kind := ""
	if b.Kind.Set && !b.Kind.Null {
		kind = b.Kind.Val
	}
	if !b.Kind.Set || b.Kind.Null || !slices.Contains(sheetSourceKindValues, kind) {
		v.Add(kindPath, "Invalid discriminator value. Expected 'unfamiliar' | 'session' | 'unit' | 'book'")
		return nil
	}
	switch kind {
	case "session":
		return &service.SheetSource{Kind: kind, SessionID: v.Str(path+".sessionId", b.SessionID, httpx.Min(1))}
	case "unit":
		return &service.SheetSource{Kind: kind, UnitID: v.Str(path+".unitId", b.UnitID, httpx.Min(1))}
	case "book":
		return &service.SheetSource{Kind: kind, BookID: v.Str(path+".bookId", b.BookID, httpx.Min(1))}
	default:
		return &service.SheetSource{Kind: kind}
	}
}

// sheetPreviewBody POST /sheets/preview。
type sheetPreviewBody struct {
	UserID  httpx.Opt[string]          `json:"userId"`
	Count   httpx.Opt[float64]         `json:"count"`
	Include httpx.Opt[[]string]        `json:"include"`
	Source  httpx.Opt[sheetSourceBody] `json:"source"`

	userID  string
	count   int
	include []string
	source  *service.SheetSource
}

func (b *sheetPreviewBody) Validate(v *httpx.V) {
	if s := v.OptStr("userId", b.UserID); s != nil {
		b.userID = *s
	}
	def := core.SheetDefault
	b.count = v.Int("count", b.Count, &def, httpx.Between(core.SheetMin, core.SheetMax))
	if b.Include.Set {
		if b.Include.Null {
			v.Add("include", "Expected array, received null")
		} else {
			ids := b.Include.Val
			v.ArrayLen("include", len(ids), -1, core.SheetMax, "", "")
			b.include = ids
		}
	}
	b.source = validateSheetSource(v, "source", b.Source)
}

// sheetCreateBody POST /sheets。
type sheetCreateBody struct {
	UserID   httpx.Opt[string]   `json:"userId"`
	WordIDs  httpx.Opt[[]string] `json:"wordIds"`
	Modes    httpx.Opt[[]string] `json:"modes"`
	Copies   httpx.Opt[float64]  `json:"copies"`
	PerSheet httpx.Opt[float64]  `json:"perSheet"`

	userID           string
	wordIDs          []string
	modes            []string
	copies, perSheet int
}

func (b *sheetCreateBody) Validate(v *httpx.V) {
	if s := v.OptStr("userId", b.UserID); s != nil {
		b.userID = *s
	}
	if !b.WordIDs.Set {
		v.Add("wordIds", "Required")
	} else if b.WordIDs.Null {
		v.Add("wordIds", "Expected array, received null")
	} else {
		ids := b.WordIDs.Val
		v.ArrayLen("wordIds", len(ids), 1, core.SheetMax, "至少选一个单词", "")
		for i, id := range ids {
			v.Str(fmt.Sprintf("wordIds.%d", i), httpx.Some(id), httpx.Min(1))
		}
		b.wordIDs = ids
	}
	b.modes = validateModes(v, "modes", b.Modes, []string{"recognition", "spelling"})
	one := 1
	b.copies = v.Int("copies", b.Copies, &one, httpx.Between(1, core.SheetCopiesMax))
	perMax := core.SheetPerMax
	b.perSheet = v.Int("perSheet", b.PerSheet, &perMax, httpx.Between(1, core.SheetPerMax))
}

// ===== 查询参数 =====

type sheetsListQuery struct {
	Page  httpx.Opt[string] `json:"page"`
	Limit httpx.Opt[string] `json:"limit"`

	page, limit int
}

func (q *sheetsListQuery) Validate(v *httpx.V) {
	q.page, q.limit = pageLimit(v, q.Page, q.Limit)
}

// ===== 权限 =====

// targetBodyUser 目标学生：请求体里的 userId（默认本人；给别人需能查看该学生，旧 target）。
func targetBodyUser(ctx context.Context, d *Deps, actor *service.Actor, userID string) (string, error) {
	t := userID
	if t == "" {
		t = actor.ID
	}
	if err := service.AssertCanViewUser(ctx, d.DB, actor, t); err != nil {
		return "", err
	}
	return t, nil
}

// assertSheetSource 来源的范围判定：错词来源只能用目标学生自己的学习组；单元 / 整本书须是操作者可见的词书（旧 assertSource）。
func assertSheetSource(ctx context.Context, d *Deps, actor *service.Actor, target string, source *service.SheetSource) error {
	if source == nil {
		return nil
	}
	switch source.Kind {
	case "session":
		owner, err := service.SheetSourceSessionOwner(ctx, d.DB, source.SessionID)
		if err != nil {
			return err
		}
		if owner != target {
			return httpx.Forbidden("只能用该学生自己的测试错词")
		}
		return service.AssertCanViewUser(ctx, d.DB, actor, owner)
	case "unit":
		filter, args, err := service.VisibleBookFilter(ctx, d.DB, actor, "b")
		if err != nil {
			return err
		}
		var id string
		err = d.DB.QueryRowContext(ctx, `SELECT u."id" FROM "Unit" u JOIN "Book" b ON b."id" = u."bookId" WHERE u."id" = ? AND `+filter,
			append([]any{source.UnitID}, args...)...).Scan(&id)
		if store.IsNoRows(err) {
			return httpx.NotFound("单元不存在")
		}
		return err
	case "book":
		filter, args, err := service.VisibleBookFilter(ctx, d.DB, actor, "b")
		if err != nil {
			return err
		}
		var id string
		err = d.DB.QueryRowContext(ctx, `SELECT b."id" FROM "Book" b WHERE b."id" = ? AND `+filter,
			append([]any{source.BookID}, args...)...).Scan(&id)
		if store.IsNoRows(err) {
			return httpx.NotFound("词书不存在")
		}
		return err
	}
	return nil
}

func registerSheets(r *Router, d *Deps) {
	study := auth.RequireCap(core.CapStudy)

	r.Post("/sheets/preview", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		body, err := httpx.Decode[sheetPreviewBody](req)
		if err != nil {
			return err
		}
		userID, err := targetBodyUser(ctx, d, actor, body.userID)
		if err != nil {
			return err
		}
		if err := assertSheetSource(ctx, d, actor, userID, body.source); err != nil {
			return err
		}
		out, err := service.PreviewSheet(ctx, d.DB, d.Cfg.Location, d.Now(), userID, body.count, body.include, body.source)
		if err != nil {
			return err
		}
		httpx.OK(w, out)
		return nil
	}, study)

	r.Get("/sheets/sources", func(w http.ResponseWriter, req *http.Request) error {
		userID, err := targetUser(req, d)
		if err != nil {
			return err
		}
		out, err := service.SheetSources(req.Context(), d.DB, d.Now(), userID)
		if err != nil {
			return err
		}
		httpx.OK(w, out)
		return nil
	}, study)

	r.Post("/sheets", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		body, err := httpx.Decode[sheetCreateBody](req)
		if err != nil {
			return err
		}
		userID, err := targetBodyUser(ctx, d, actor, body.userID)
		if err != nil {
			return err
		}
		out, err := service.CreateSheet(ctx, d.DB, d.Now(), userID, actor.ID, body.wordIDs, body.modes, body.copies, body.perSheet)
		if err != nil {
			return err
		}
		httpx.OK(w, out)
		return nil
	}, study)

	r.Get("/sheets", func(w http.ResponseWriter, req *http.Request) error {
		userID, err := targetUser(req, d)
		if err != nil {
			return err
		}
		q, err := httpx.DecodeQuery[sheetsListQuery](req)
		if err != nil {
			return err
		}
		out, err := service.ListSheets(req.Context(), d.DB, userID, q.page, q.limit)
		if err != nil {
			return err
		}
		httpx.OK(w, out)
		return nil
	}, study)

	r.Get("/sheets/{id}", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		detail, err := service.SheetDetail(ctx, d.DB, req.PathValue("id"))
		if err != nil {
			return err
		}
		if err := service.AssertCanViewUser(ctx, d.DB, actor, detail.UserID); err != nil {
			return err
		}
		httpx.OK(w, detail)
		return nil
	}, study)

	r.Delete("/sheets/{id}", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		id := req.PathValue("id")
		owner, err := service.SheetOwner(ctx, d.DB, id)
		if err != nil {
			return err
		}
		if owner.UserID != actor.ID && owner.CreatorID != actor.ID && !service.SeesAll(actor) {
			if err := service.AssertCanViewUser(ctx, d.DB, actor, owner.UserID); err != nil {
				return httpx.Forbidden("无权删除这张单词单")
			}
		}
		if err := service.DeleteSheet(ctx, d.DB, id); err != nil {
			return err
		}
		httpx.OK(w, nil)
		return nil
	}, study)
}
