package api

import (
	"fmt"
	"net/http"

	"github.com/vinx-lab/vinx-vocab-go/internal/auth"
	"github.com/vinx-lab/vinx-vocab-go/internal/core"
	"github.com/vinx-lab/vinx-vocab-go/internal/httpx"
	"github.com/vinx-lab/vinx-vocab-go/internal/service"
)

// 目标词书与覆盖进度（spec 0003）：我的目标词书、覆盖进度、按状态的词表。
// 班级目标词书（GET/PUT /classes/:id/target-books）是班级版专属，在 classes.go 里注册。

// targetBooksMax 一次设置的目标词书上限。
const targetBooksMax = 100

// targetBooksBody PUT /classes/:id/target-books 与 PUT /me/target-books：{ bookIds: string[] }，顺序即 sortOrder。
type targetBooksBody struct {
	BookIDs httpx.Opt[[]string] `json:"bookIds"`
	bookIDs []string
}

func (b *targetBooksBody) Validate(v *httpx.V) {
	switch {
	case v.Has("bookIds"):
	case !b.BookIDs.Set:
		v.Add("bookIds", "Required")
	case b.BookIDs.Null:
		v.Add("bookIds", "Expected array, received null")
	default:
		ids := b.BookIDs.Val
		v.ArrayLen("bookIds", len(ids), -1, targetBooksMax, "", "")
		b.bookIDs = make([]string, 0, len(ids))
		for i, id := range ids {
			b.bookIDs = append(b.bookIDs, v.Str(fmt.Sprintf("bookIds.%d", i), httpx.Some(id), httpx.Min(1)))
		}
	}
}

// coverageWordsQuery GET /records/coverage/words。
type coverageWordsQuery struct {
	Page   httpx.Opt[string] `json:"page"`
	Limit  httpx.Opt[string] `json:"limit"`
	BookID httpx.Opt[string] `json:"bookId"`
	Status httpx.Opt[string] `json:"status"`

	page, limit    int
	bookID, status string
}

func (q *coverageWordsQuery) Validate(v *httpx.V) {
	q.page, q.limit = pageLimit(v, q.Page, q.Limit)
	if s := v.OptStr("bookId", q.BookID); s != nil {
		q.bookID = *s
	}
	q.status = v.OptEnum("status", q.Status, core.CoverageStatuses, "", "")
}

func registerCoverage(r *Router, d *Deps) {
	study := auth.RequireCap(core.CapStudy)
	records := auth.RequireCap(core.CapStudy, core.CapStudentsView)

	myTargets := func(req *http.Request) (*service.TargetBooksView, error) {
		actor := auth.ActorFrom(req.Context())
		return service.EffectiveTargets(req.Context(), d.DB, service.TargetUsesClasses(actor), actor.ID)
	}

	r.Get("/me/target-books", func(w http.ResponseWriter, req *http.Request) error {
		out, err := myTargets(req)
		if err != nil {
			return err
		}
		httpx.OK(w, out)
		return nil
	}, study)

	// 有班级时也能保存（退出所有班级后生效），返回的是当前生效的目标（source 仍为 class）
	r.Put("/me/target-books", func(w http.ResponseWriter, req *http.Request) error {
		body, err := httpx.Decode[targetBooksBody](req)
		if err != nil {
			return err
		}
		if err := service.SetOwnTargetBooks(req.Context(), d.DB, auth.ActorFrom(req.Context()), body.bookIDs); err != nil {
			return err
		}
		out, err := myTargets(req)
		if err != nil {
			return err
		}
		httpx.OK(w, out)
		return nil
	}, study)

	r.Get("/records/coverage", func(w http.ResponseWriter, req *http.Request) error {
		userID, err := targetUser(req, d)
		if err != nil {
			return err
		}
		out, err := service.UserCoverage(req.Context(), d.DB, service.TargetUsesClasses(auth.ActorFrom(req.Context())), userID)
		if err != nil {
			return err
		}
		httpx.OK(w, out)
		return nil
	}, records)

	r.Get("/records/coverage/words", func(w http.ResponseWriter, req *http.Request) error {
		userID, err := targetUser(req, d)
		if err != nil {
			return err
		}
		q, err := httpx.DecodeQuery[coverageWordsQuery](req)
		if err != nil {
			return err
		}
		out, err := service.CoverageWords(req.Context(), d.DB, service.TargetUsesClasses(auth.ActorFrom(req.Context())), userID, q.bookID, q.status, q.page, q.limit)
		if err != nil {
			return err
		}
		httpx.OK(w, out)
		return nil
	}, records)
}
