package api

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"

	"github.com/vinx-lab/vinx-vocab-go/internal/auth"
	"github.com/vinx-lab/vinx-vocab-go/internal/core"
	"github.com/vinx-lab/vinx-vocab-go/internal/httpx"
	"github.com/vinx-lab/vinx-vocab-go/internal/service"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// 学习计划（对应旧 routes/plans.ts）。

var (
	planKindValues   = []string{"daily", "test"}
	planStatusValues = []string{"active", "paused", "archived"}
	planModeValues   = []string{"recognition", "spelling", "cloze"}
	planOrderValues  = []string{"sequential", "random"}
	planScopeValues  = []string{"all", "learned"}
)

// ===== 共用校验 =====

func validateDayKeyOpt(v *httpx.V, path string, o httpx.Opt[string]) httpx.Opt[string] {
	return v.NullStr(path, o, httpx.Refine(core.IsValidDayKey, "日期格式应为 YYYY-MM-DD"))
}

func dayKeyPtr(o httpx.Opt[string]) *string {
	if o.Set && !o.Null {
		s := o.Val
		return &s
	}
	return nil
}

// planTargetsBody 安排对象（旧 targets: { classIds, userIds }）。
type planTargetsBody struct {
	ClassIDs httpx.Opt[[]string] `json:"classIds"`
	UserIDs  httpx.Opt[[]string] `json:"userIds"`
}

func validateTargets(v *httpx.V, path string, o httpx.Opt[planTargetsBody]) service.PlanTargetsInput {
	if !o.Set {
		return service.PlanTargetsInput{ClassIDs: []string{}, UserIDs: []string{}}
	}
	if o.Null {
		v.Add(path, "Expected object, received null")
		return service.PlanTargetsInput{ClassIDs: []string{}, UserIDs: []string{}}
	}
	t := o.Val
	classIDs := []string{}
	if t.ClassIDs.Set {
		if t.ClassIDs.Null {
			v.Add(path+".classIds", "Expected array, received null")
		} else if t.ClassIDs.Val != nil {
			classIDs = t.ClassIDs.Val
		}
	}
	userIDs := []string{}
	if t.UserIDs.Set {
		if t.UserIDs.Null {
			v.Add(path+".userIds", "Expected array, received null")
		} else if t.UserIDs.Val != nil {
			userIDs = t.UserIDs.Val
		}
	}
	return service.PlanTargetsInput{ClassIDs: classIDs, UserIDs: userIDs}
}

// validateModes 题型数组：o 缺省时用 def（创建默认值）；提供时校验非空且元素为合法枚举。
func validateModes(v *httpx.V, path string, o httpx.Opt[[]string], def []string) []string {
	if !o.Set {
		return append([]string(nil), def...)
	}
	if o.Null {
		v.Add(path, "Expected array, received null")
		return nil
	}
	arr := o.Val
	v.ArrayLen(path, len(arr), 1, -1, "至少选择一种题型", "")
	out := make([]string, 0, len(arr))
	for i, m := range arr {
		out = append(out, v.Enum(fmt.Sprintf("%s.%d", path, i), httpx.Some(m), planModeValues, ""))
	}
	return out
}

// ===== POST /plans/preview =====

type planPreviewBody struct {
	UnitIDs httpx.Opt[[]string] `json:"unitIds"`

	unitIDs []string
}

func (b *planPreviewBody) Validate(v *httpx.V) {
	if !b.UnitIDs.Set {
		v.Add("unitIds", "Required")
		return
	}
	if b.UnitIDs.Null {
		v.Add("unitIds", "Expected array, received null")
		return
	}
	b.unitIDs = b.UnitIDs.Val
	v.ArrayLen("unitIds", len(b.unitIDs), -1, 200, "", "")
}

// ===== POST /plans/allowed-books =====

type planAllowedBooksBody struct {
	Targets httpx.Opt[planTargetsBody] `json:"targets"`

	targets service.PlanTargetsInput
}

func (b *planAllowedBooksBody) Validate(v *httpx.V) {
	b.targets = validateTargets(v, "targets", b.Targets)
}

// annotatePlan 单个计划视图的 spec 0008 标记。
func annotatePlan(ctx context.Context, q store.Querier, a *service.Actor, v *service.PlanView) error {
	views := []service.PlanView{*v}
	if err := service.AnnotatePlans(ctx, q, a, views); err != nil {
		return err
	}
	*v = views[0]
	return nil
}

// ===== POST /plans =====

type planCreateBody struct {
	Name         httpx.Opt[string]          `json:"name"`
	Kind         httpx.Opt[string]          `json:"kind"`
	Status       httpx.Opt[string]          `json:"status"`
	NewPerDay    httpx.Opt[float64]         `json:"newPerDay"`
	ReviewPerDay httpx.Opt[float64]         `json:"reviewPerDay"`
	Modes        httpx.Opt[[]string]        `json:"modes"`
	Order        httpx.Opt[string]          `json:"order"`
	TestSize     httpx.Opt[float64]         `json:"testSize"`
	TestScope    httpx.Opt[string]          `json:"testScope"`
	StartDate    httpx.Opt[string]          `json:"startDate"`
	EndDate      httpx.Opt[string]          `json:"endDate"`
	UnitIDs      httpx.Opt[[]string]        `json:"unitIds"`
	Targets      httpx.Opt[planTargetsBody] `json:"targets"`

	in service.PlanInput
}

func (b *planCreateBody) Validate(v *httpx.V) {
	b.in.Name = v.Str("name", b.Name, httpx.Trim(), httpx.Min(1, "请填写计划名称"), httpx.Max(60, "名称过长"))
	b.in.Kind = v.OptEnum("kind", b.Kind, planKindValues, "", "daily")
	b.in.Status = v.OptEnum("status", b.Status, planStatusValues, "", "active")
	ten := 10
	b.in.NewPerDay = v.Int("newPerDay", b.NewPerDay, &ten, httpx.Between(0, 200))
	fifty := 50
	b.in.ReviewPerDay = v.Int("reviewPerDay", b.ReviewPerDay, &fifty, httpx.Between(0, 500))
	b.in.Modes = validateModes(v, "modes", b.Modes, []string{"recognition", "spelling"})
	b.in.Order = v.OptEnum("order", b.Order, planOrderValues, "", "sequential")
	twenty := 20
	b.in.TestSize = v.Int("testSize", b.TestSize, &twenty, httpx.Between(1, 500))
	b.in.TestScope = v.OptEnum("testScope", b.TestScope, planScopeValues, "", "all")
	b.in.StartDate = dayKeyPtr(validateDayKeyOpt(v, "startDate", b.StartDate))
	b.in.EndDate = dayKeyPtr(validateDayKeyOpt(v, "endDate", b.EndDate))
	if !b.UnitIDs.Set {
		v.Add("unitIds", "Required")
	} else if b.UnitIDs.Null {
		v.Add("unitIds", "Expected array, received null")
	} else {
		b.in.UnitIDs = b.UnitIDs.Val
		v.ArrayLen("unitIds", len(b.in.UnitIDs), 1, 200, "请至少选择一个单元", "")
	}
	b.in.Targets = validateTargets(v, "targets", b.Targets)
}

// ===== PATCH /plans/:id =====

type planPatchBody struct {
	Name         httpx.Opt[string]          `json:"name"`
	Kind         httpx.Opt[string]          `json:"kind"`
	Status       httpx.Opt[string]          `json:"status"`
	NewPerDay    httpx.Opt[float64]         `json:"newPerDay"`
	ReviewPerDay httpx.Opt[float64]         `json:"reviewPerDay"`
	Modes        httpx.Opt[[]string]        `json:"modes"`
	Order        httpx.Opt[string]          `json:"order"`
	TestSize     httpx.Opt[float64]         `json:"testSize"`
	TestScope    httpx.Opt[string]          `json:"testScope"`
	StartDate    httpx.Opt[string]          `json:"startDate"`
	EndDate      httpx.Opt[string]          `json:"endDate"`
	UnitIDs      httpx.Opt[[]string]        `json:"unitIds"`
	Targets      httpx.Opt[planTargetsBody] `json:"targets"`

	patch service.PlanPatch
}

func (b *planPatchBody) Validate(v *httpx.V) {
	if b.Name.Set {
		name := v.Str("name", b.Name, httpx.Trim(), httpx.Min(1, "请填写计划名称"), httpx.Max(60, "名称过长"))
		b.patch.Name = &name
	}
	if b.Kind.Set {
		k := v.Enum("kind", b.Kind, planKindValues, "")
		b.patch.Kind = &k
	}
	if b.Status.Set {
		s := v.Enum("status", b.Status, planStatusValues, "")
		b.patch.Status = &s
	}
	if b.NewPerDay.Set {
		n := v.Int("newPerDay", b.NewPerDay, nil, httpx.Between(0, 200))
		b.patch.NewPerDay = &n
	}
	if b.ReviewPerDay.Set {
		n := v.Int("reviewPerDay", b.ReviewPerDay, nil, httpx.Between(0, 500))
		b.patch.ReviewPerDay = &n
	}
	if b.Modes.Set {
		b.patch.Modes = validateModes(v, "modes", b.Modes, nil)
	}
	if b.Order.Set {
		o := v.Enum("order", b.Order, planOrderValues, "")
		b.patch.Order = &o
	}
	if b.TestSize.Set {
		n := v.Int("testSize", b.TestSize, nil, httpx.Between(1, 500))
		b.patch.TestSize = &n
	}
	if b.TestScope.Set {
		s := v.Enum("testScope", b.TestScope, planScopeValues, "")
		b.patch.TestScope = &s
	}
	b.patch.StartDate = nullStrPatch(validateDayKeyOpt(v, "startDate", b.StartDate))
	b.patch.EndDate = nullStrPatch(validateDayKeyOpt(v, "endDate", b.EndDate))
	if b.UnitIDs.Set {
		if b.UnitIDs.Null {
			v.Add("unitIds", "Expected array, received null")
		} else {
			ids := b.UnitIDs.Val
			v.ArrayLen("unitIds", len(ids), 1, 200, "请至少选择一个单元", "")
			b.patch.UnitIDs = ids
		}
	}
	if b.Targets.Set {
		t := validateTargets(v, "targets", b.Targets)
		b.patch.Targets = &t
	}
}

// ===== GET /plans, /plans/:id/progress 的查询参数 =====

type plansListQuery struct {
	Scope   httpx.Opt[string] `json:"scope"`
	ClassID httpx.Opt[string] `json:"classId"`
	Status  httpx.Opt[string] `json:"status"`

	scope, classID, status string
}

func (q *plansListQuery) Validate(v *httpx.V) {
	q.scope = v.OptEnum("scope", q.Scope, []string{"all", "mine", "created"}, "", "all")
	if s := v.OptStr("classId", q.ClassID); s != nil {
		q.classID = *s
	}
	q.status = v.OptEnum("status", q.Status, planStatusValues, "", "")
}

// startEndAfterPatch 计算 PATCH 应用后的 startDate / endDate（未提供的字段沿用原值）。
func startEndAfterPatch(existingStart, existingEnd *string, patch service.PlanPatch) (start, end *string) {
	start, end = existingStart, existingEnd
	if patch.StartDate != nil {
		if patch.StartDate.Valid {
			s := patch.StartDate.String
			start = &s
		} else {
			start = nil
		}
	}
	if patch.EndDate != nil {
		if patch.EndDate.Valid {
			s := patch.EndDate.String
			end = &s
		} else {
			end = nil
		}
	}
	return
}

func registerPlans(r *Router, d *Deps) {
	r.Get("/plans", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		q, err := httpx.DecodeQuery[plansListQuery](req)
		if err != nil {
			return err
		}
		items, err := service.ListPlans(ctx, d.DB, actor, service.ListPlansQuery{Scope: q.scope, ClassID: q.classID, Status: q.status})
		if err != nil {
			return err
		}
		httpx.OK(w, httpx.List(items))
		return nil
	}, auth.RequireCap(core.CapPlans))

	r.Post("/plans/preview", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		body, err := httpx.Decode[planPreviewBody](req)
		if err != nil {
			return err
		}
		res, err := service.PreviewPlan(ctx, d.DB, actor, body.unitIDs)
		if err != nil {
			return err
		}
		httpx.OK(w, res)
		return nil
	}, auth.RequireCap(core.CapPlans))

	// 建计划页的单元选择范围（spec 0008）：安排对象（缺省 = 自己）的目标词书交集；目标都为空时不约束
	r.Post("/plans/allowed-books", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		body, err := httpx.Decode[planAllowedBooksBody](req)
		if err != nil {
			return err
		}
		targets, err := service.ResolveTargets(ctx, d.DB, actor, body.targets)
		if err != nil {
			return err
		}
		res, err := service.AllowedPlanBooksFor(ctx, d.DB, actor, targets)
		if err != nil {
			return err
		}
		httpx.OK(w, res)
		return nil
	}, auth.RequireCap(core.CapPlans))

	r.Post("/plans", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		body, err := httpx.Decode[planCreateBody](req)
		if err != nil {
			return err
		}
		if err := service.CheckPlanDates(body.in.StartDate, body.in.EndDate); err != nil {
			return err
		}
		if err := service.AssertUnitsVisible(ctx, d.DB, actor, body.in.UnitIDs); err != nil {
			return err
		}
		targets, err := service.ResolveTargets(ctx, d.DB, actor, body.in.Targets)
		if err != nil {
			return err
		}
		body.in.Targets = targets
		// spec 0008：班级不允许自主时不能自建；所选单元须落在安排对象的目标词书里
		if err := service.AssertSelfPlanAllowed(ctx, d.DB, actor, targets); err != nil {
			return err
		}
		if err := service.AssertPlanUnitsInTargets(ctx, d.DB, actor, body.in.UnitIDs, targets); err != nil {
			return err
		}
		var plan *service.PlanView
		err = d.DB.Tx(ctx, func(tx *sql.Tx) error {
			row, err := service.CreatePlan(ctx, tx, d.Now(), actor.ID, body.in)
			if err != nil {
				return err
			}
			myClassIDs, err := service.MemberClassIDs(ctx, tx, actor.ID)
			if err != nil {
				return err
			}
			if plan, err = service.SerializePlan(ctx, tx, row, actor, myClassIDs); err != nil {
				return err
			}
			return annotatePlan(ctx, tx, actor, plan)
		})
		if err != nil {
			return err
		}
		httpx.OK(w, plan)
		return nil
	}, auth.RequireCap(core.CapPlans))

	r.Get("/plans/{id}", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		id := req.PathValue("id")
		row, err := service.GetVisiblePlanRow(ctx, d.DB, actor, id)
		if err != nil {
			return err
		}
		if row == nil {
			return httpx.NotFound("计划不存在")
		}
		myClassIDs, err := service.MemberClassIDs(ctx, d.DB, actor.ID)
		if err != nil {
			return err
		}
		view, err := service.SerializePlan(ctx, d.DB, row, actor, myClassIDs)
		if err != nil {
			return err
		}
		if err := annotatePlan(ctx, d.DB, actor, view); err != nil {
			return err
		}
		httpx.OK(w, view)
		return nil
	}, auth.RequireCap(core.CapPlans))

	r.Patch("/plans/{id}", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		id := req.PathValue("id")
		existing, err := service.LoadEditablePlan(ctx, d.DB, actor, id)
		if err != nil {
			return err
		}
		body, err := httpx.Decode[planPatchBody](req)
		if err != nil {
			return err
		}
		start, end := startEndAfterPatch(existing.StartDate, existing.EndDate, body.patch)
		if err := service.CheckPlanDates(start, end); err != nil {
			return err
		}
		if body.patch.UnitIDs != nil {
			if err := service.AssertUnitsVisible(ctx, d.DB, actor, body.patch.UnitIDs); err != nil {
				return err
			}
		}
		if body.patch.Targets != nil {
			resolved, err := service.ResolveTargets(ctx, d.DB, actor, *body.patch.Targets)
			if err != nil {
				return err
			}
			body.patch.Targets = &resolved
		}
		// spec 0008：只在改了单元范围（有新增的单元）时检查；只改节奏、题型、日期、对象不检查，
		// 已有的超出目标的单元不受影响
		if body.patch.UnitIDs != nil {
			added, err := service.AddedPlanUnits(ctx, d.DB, id, body.patch.UnitIDs)
			if err != nil {
				return err
			}
			if len(added) > 0 {
				targets := body.patch.Targets
				if targets == nil {
					t, err := service.PlanTargetsOf(ctx, d.DB, id)
					if err != nil {
						return err
					}
					targets = &t
				}
				if existing.CreatorID == actor.ID {
					if err := service.AssertSelfPlanAllowed(ctx, d.DB, actor, *targets); err != nil {
						return err
					}
				}
				if err := service.AssertPlanUnitsInTargets(ctx, d.DB, actor, added, *targets); err != nil {
					return err
				}
			}
		}
		var plan *service.PlanView
		err = d.DB.Tx(ctx, func(tx *sql.Tx) error {
			row, err := service.UpdatePlan(ctx, tx, d.Now(), id, body.patch)
			if err != nil {
				return err
			}
			myClassIDs, err := service.MemberClassIDs(ctx, tx, actor.ID)
			if err != nil {
				return err
			}
			if plan, err = service.SerializePlan(ctx, tx, row, actor, myClassIDs); err != nil {
				return err
			}
			return annotatePlan(ctx, tx, actor, plan)
		})
		if err != nil {
			return err
		}
		httpx.OK(w, plan)
		return nil
	}, auth.RequireCap(core.CapPlans))

	r.Delete("/plans/{id}", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		id := req.PathValue("id")
		if _, err := service.LoadEditablePlan(ctx, d.DB, actor, id); err != nil {
			return err
		}
		if err := d.DB.Tx(ctx, func(tx *sql.Tx) error {
			return service.DeletePlan(ctx, tx, d.Cfg.Location, d.Now(), id)
		}); err != nil {
			return err
		}
		httpx.OK(w, nil)
		return nil
	}, auth.RequireCap(core.CapPlans))

	r.Get("/plans/{id}/progress", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		id := req.PathValue("id")
		res, err := service.PlanProgress(ctx, d.DB, d.Cfg.Location, d.Now(), actor, id)
		if err != nil {
			return err
		}
		if res == nil {
			return httpx.NotFound("计划不存在")
		}
		httpx.OK(w, res)
		return nil
	}, auth.RequireCap(core.CapPlans))
}
