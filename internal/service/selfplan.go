package service

import (
	"context"
	"strings"

	"github.com/vinx-lab/vinx-vocab-go/internal/core"
	"github.com/vinx-lab/vinx-vocab-go/internal/httpx"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// 班级自主开关与「先定目标词书，再建计划」（spec 0008）：
//   - 学生的有效自主（SelfPlanAllowed：不在班里为真，任一班不允许为假；个人版永远为真）；
//   - 建计划、改单元时检查计划落在目标词书里（core.PlanBooksAllowed）；
//   - 不允许自主时，学生自建的、只安排给自己的计划「算出来的暂停」（LoadPlansForLearners 过滤，不改 status）；
//   - 计划列表、今日页的「不在目标词书内」「班级未开放自主安排，暂停中」标记。

// ------------------------------------------------------------------
// 请求的版本（只给拿不到 Actor 的学习流使用）
// ------------------------------------------------------------------

type editionKey struct{}

// WithEdition 把本次请求的版本挂到 ctx 上（路由在身份解析后与 Actor.Edition 一起设置）。
// 今日队列、开组等只拿到 userID 的服务函数据此判断班级的自主开关是否生效：个人版没有班级，相当于永远允许。
func WithEdition(ctx context.Context, ed core.Edition) context.Context {
	return context.WithValue(ctx, editionKey{}, ed)
}

// classesApplyIn ctx 上的版本是否看班级成员关系（没有挂版本时按班级版处理，与 Actor.Edition 为空串一致）。
func classesApplyIn(ctx context.Context) bool {
	ed, _ := ctx.Value(editionKey{}).(core.Edition)
	return ed != core.EditionPersonal
}

// ------------------------------------------------------------------
// 有效自主
// ------------------------------------------------------------------

// SelfPlanAllowedBatch 一批用户的有效自主；useClasses 为假（个人版）时都为真。
func SelfPlanAllowedBatch(ctx context.Context, q store.Querier, useClasses bool, userIDs []string) (map[string]bool, error) {
	out := make(map[string]bool, len(userIDs))
	for _, id := range userIDs {
		out[id] = true
	}
	if !useClasses || len(userIDs) == 0 {
		return out, nil
	}
	rows, err := q.QueryContext(ctx, `SELECT m."userId", c."allowSelfPlan" FROM "ClassMember" m JOIN "Classroom" c ON c."id" = m."classId"
		WHERE m."userId" IN (`+store.Placeholders(len(userIDs))+`)`, store.Args(userIDs)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byUser := map[string][]bool{}
	for rows.Next() {
		var uid string
		var allow bool
		if err := rows.Scan(&uid, &allow); err != nil {
			return nil, err
		}
		byUser[uid] = append(byUser[uid], allow)
	}
	for uid, list := range byUser {
		out[uid] = core.SelfPlanAllowed(list)
	}
	return out, rows.Err()
}

// SelfPlanAllowedFor 某用户能否自主安排计划。
func SelfPlanAllowedFor(ctx context.Context, q store.Querier, useClasses bool, userID string) (bool, error) {
	m, err := SelfPlanAllowedBatch(ctx, q, useClasses, []string{userID})
	if err != nil {
		return false, err
	}
	return m[userID], nil
}

// isSelfPlanOf 计划是 userID 自建的、只安排给自己的。
func isSelfPlanOf(creatorID, userID string, targets []planTargetOwner) bool {
	return creatorID == userID && len(targets) == 1 && targets[0].ClassID == nil && targets[0].UserID != nil && *targets[0].UserID == userID
}

// isSelfTargets 规整后的安排对象只有操作者自己。
func isSelfTargets(a *Actor, t PlanTargetsInput) bool {
	return len(t.ClassIDs) == 0 && len(t.UserIDs) == 1 && t.UserIDs[0] == a.ID
}

// MsgSelfPlanClosed 学生在不允许自主的班里自建计划。
const MsgSelfPlanClosed = "班级未开放自主安排计划"

// AssertSelfPlanAllowed 安排对象只有自己时，班级不允许自主 → 403「班级未开放自主安排计划」。
func AssertSelfPlanAllowed(ctx context.Context, q store.Querier, a *Actor, targets PlanTargetsInput) error {
	if !isSelfTargets(a, targets) {
		return nil
	}
	ok, err := SelfPlanAllowedFor(ctx, q, TargetUsesClasses(a), a.ID)
	if err != nil {
		return err
	}
	if !ok {
		return httpx.Forbidden(MsgSelfPlanClosed)
	}
	return nil
}

// selfPlanPausedFor 计划是 userID 自建的生效中计划，但因班级不允许自主而暂停（开组时给出明确提示）。
func selfPlanPausedFor(ctx context.Context, q store.Querier, userID, planID string) (bool, error) {
	var creatorID, status string
	err := q.QueryRowContext(ctx, `SELECT "creatorId","status" FROM "Plan" WHERE "id" = ?`, planID).Scan(&creatorID, &status)
	if store.IsNoRows(err) {
		return false, nil
	}
	if err != nil || status != "active" || creatorID != userID {
		return false, err
	}
	raw, err := planTargetsRaw(ctx, q, planID)
	if err != nil {
		return false, err
	}
	targets := make([]planTargetOwner, len(raw))
	for i, t := range raw {
		targets[i] = planTargetOwner{UserID: t.UserID, ClassID: t.ClassID}
	}
	if !isSelfPlanOf(creatorID, userID, targets) {
		return false, nil
	}
	ok, err := SelfPlanAllowedFor(ctx, q, classesApplyIn(ctx), userID)
	return !ok, err
}

// ------------------------------------------------------------------
// 计划必须落在目标里
// ------------------------------------------------------------------

// planTargetSets 每个安排对象的目标词书：班级 → 班级目标；学生（含自己）→ 其有效目标。
func planTargetSets(ctx context.Context, q store.Querier, a *Actor, targets PlanTargetsInput) ([][]string, error) {
	sets := [][]string{}
	for _, cid := range targets.ClassIDs {
		books, err := ClassTargetBooks(ctx, q, cid)
		if err != nil {
			return nil, err
		}
		ids := make([]string, len(books))
		for i, b := range books {
			ids[i] = b.ID
		}
		sets = append(sets, ids)
	}
	if len(targets.UserIDs) > 0 {
		m, err := effectiveTargetsBatch(ctx, q, TargetUsesClasses(a), targets.UserIDs)
		if err != nil {
			return nil, err
		}
		for _, uid := range targets.UserIDs {
			sets = append(sets, m[uid].TargetBookIDs())
		}
	}
	return sets, nil
}

// unitBookIDs 单元所在的词书（按单元顺序去重）。
func unitBookIDs(ctx context.Context, q store.Querier, unitIDs []string) ([]string, error) {
	ids := uniqueStrings(unitIDs)
	if len(ids) == 0 {
		return []string{}, nil
	}
	rows, err := q.QueryContext(ctx, `SELECT "id","bookId" FROM "Unit" WHERE "id" IN (`+store.Placeholders(len(ids))+`)`, store.Args(ids)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byUnit := map[string]string{}
	for rows.Next() {
		var u, b string
		if err := rows.Scan(&u, &b); err != nil {
			return nil, err
		}
		byUnit[u] = b
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []string{}
	for _, u := range ids {
		if b, ok := byUnit[u]; ok {
			out = append(out, b)
		}
	}
	return uniqueStrings(out), nil
}

// AssertPlanUnitsInTargets 所选单元的词书都在约束集合里（各对象目标的交集，空目标不约束），
// 否则 400「所选单元不在目标词书内：《书名》」。targets 须已规整（ResolveTargets）。
func AssertPlanUnitsInTargets(ctx context.Context, q store.Querier, a *Actor, unitIDs []string, targets PlanTargetsInput) error {
	if len(unitIDs) == 0 {
		return nil
	}
	sets, err := planTargetSets(ctx, q, a, targets)
	if err != nil {
		return err
	}
	books, err := unitBookIDs(ctx, q, unitIDs)
	if err != nil {
		return err
	}
	missing := core.PlanBooksAllowed(books, sets)
	if len(missing) == 0 {
		return nil
	}
	names, err := bookNames(ctx, q, missing)
	if err != nil {
		return err
	}
	parts := make([]string, len(missing))
	for i, id := range missing {
		parts[i] = "《" + names[id] + "》"
	}
	return httpx.Validation("所选单元不在目标词书内：" + strings.Join(parts, "、"))
}

func bookNames(ctx context.Context, q store.Querier, ids []string) (map[string]string, error) {
	out := map[string]string{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := q.QueryContext(ctx, `SELECT "id","name" FROM "Book" WHERE "id" IN (`+store.Placeholders(len(ids))+`)`, store.Args(ids)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = name
	}
	return out, rows.Err()
}

// PlanTargetsOf 计划现有的安排对象（PATCH 没改对象时用来算约束）。
func PlanTargetsOf(ctx context.Context, q store.Querier, planID string) (PlanTargetsInput, error) {
	raw, err := planTargetsRaw(ctx, q, planID)
	if err != nil {
		return PlanTargetsInput{}, err
	}
	out := PlanTargetsInput{ClassIDs: []string{}, UserIDs: []string{}}
	for _, t := range raw {
		if t.ClassID != nil {
			out.ClassIDs = append(out.ClassIDs, *t.ClassID)
		} else if t.UserID != nil {
			out.UserIDs = append(out.UserIDs, *t.UserID)
		}
	}
	return out, nil
}

// AddedPlanUnits 编辑后新增的单元（不在原计划里的）。只删单元、调顺序不算改了单元范围。
func AddedPlanUnits(ctx context.Context, q store.Querier, planID string, unitIDs []string) ([]string, error) {
	existing, err := planUnitIDs(ctx, q, planID)
	if err != nil {
		return nil, err
	}
	had := map[string]bool{}
	for _, u := range existing {
		had[u] = true
	}
	added := []string{}
	for _, u := range uniqueStrings(unitIDs) {
		if !had[u] {
			added = append(added, u)
		}
	}
	return added, nil
}

// AllowedBooksView POST /plans/allowed-books：建计划页的单元选择只列 books；constrained 为假时不约束
// （安排对象的目标都为空），列全部可见词书并提示先设目标。
type AllowedBooksView struct {
	Constrained bool         `json:"constrained"`
	Books       []TargetBook `json:"books"`
	// SelfPlanAllowed 安排对象只有自己时，能否自建（否则恒为真）。
	SelfPlanAllowed bool `json:"selfPlanAllowed"`
}

// AllowedPlanBooksFor 安排对象（已规整）对应的约束集合。
func AllowedPlanBooksFor(ctx context.Context, q store.Querier, a *Actor, targets PlanTargetsInput) (*AllowedBooksView, error) {
	sets, err := planTargetSets(ctx, q, a, targets)
	if err != nil {
		return nil, err
	}
	ids, constrained := core.AllowedPlanBooks(sets)
	names, err := bookNames(ctx, q, ids)
	if err != nil {
		return nil, err
	}
	books := make([]TargetBook, 0, len(ids))
	for _, id := range ids {
		books = append(books, TargetBook{ID: id, Name: names[id]})
	}
	allowed := true
	if isSelfTargets(a, targets) {
		if allowed, err = SelfPlanAllowedFor(ctx, q, TargetUsesClasses(a), a.ID); err != nil {
			return nil, err
		}
	}
	return &AllowedBooksView{Constrained: constrained, Books: books, SelfPlanAllowed: allowed}, nil
}

// ------------------------------------------------------------------
// 计划列表与今日页的标记
// ------------------------------------------------------------------

// AnnotatePlans 给计划视图补上 selfPlanPaused 与 outsideTarget：
//   - selfPlanPaused：计划是某学生自建、只安排给自己的，而他的有效自主为否（老师看学生的自建计划也按学生本人算）；
//   - outsideTarget：计划安排给我（TargetsMe），我的有效目标非空，且有单元所在的书不在里面。
func AnnotatePlans(ctx context.Context, q store.Querier, a *Actor, views []PlanView) error {
	useClasses := TargetUsesClasses(a)
	owners := []string{}
	for _, v := range views {
		if len(v.Targets) == 1 && v.Targets[0].Type == "user" && v.Targets[0].ID == v.Creator.ID {
			owners = append(owners, v.Creator.ID)
		}
	}
	allowed, err := SelfPlanAllowedBatch(ctx, q, useClasses, uniqueStrings(owners))
	if err != nil {
		return err
	}
	var mine map[string]bool
	for i := range views {
		v := &views[i]
		if len(v.Targets) == 1 && v.Targets[0].Type == "user" && v.Targets[0].ID == v.Creator.ID {
			v.SelfPlanPaused = !allowed[v.Creator.ID]
		}
		if !v.TargetsMe {
			continue
		}
		if mine == nil {
			t, err := EffectiveTargets(ctx, q, useClasses, a.ID)
			if err != nil {
				return err
			}
			mine = map[string]bool{}
			for _, b := range t.Books {
				mine[b.ID] = true
			}
		}
		if len(mine) == 0 {
			continue
		}
		for _, u := range v.Units {
			if !mine[u.BookID] {
				v.OutsideTarget = true
				break
			}
		}
	}
	return nil
}

// TodayPlanFlags 今日页的补充信息（spec 0008）：今日计划里不在我的目标词书内的计划，以及因班级不允许自主而暂停的自建计划数。
func TodayPlanFlags(ctx context.Context, q store.Querier, a *Actor, planIDs []string) (outside []string, paused int, err error) {
	outside = []string{}
	useClasses := TargetUsesClasses(a)
	if len(planIDs) > 0 {
		t, err := EffectiveTargets(ctx, q, useClasses, a.ID)
		if err != nil {
			return nil, 0, err
		}
		if len(t.Books) > 0 {
			mine := map[string]bool{}
			for _, b := range t.Books {
				mine[b.ID] = true
			}
			ids := uniqueStrings(planIDs)
			rows, err := q.QueryContext(ctx, `SELECT DISTINCT pu."planId", u."bookId" FROM "PlanUnit" pu JOIN "Unit" u ON u."id" = pu."unitId"
				WHERE pu."planId" IN (`+store.Placeholders(len(ids))+`)`, store.Args(ids)...)
			if err != nil {
				return nil, 0, err
			}
			bad := map[string]bool{}
			for rows.Next() {
				var pid, bid string
				if err := rows.Scan(&pid, &bid); err != nil {
					rows.Close()
					return nil, 0, err
				}
				if !mine[bid] {
					bad[pid] = true
				}
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return nil, 0, err
			}
			for _, id := range ids {
				if bad[id] {
					outside = append(outside, id)
				}
			}
		}
	}
	ok, err := SelfPlanAllowedFor(ctx, q, useClasses, a.ID)
	if err != nil || ok {
		return outside, 0, err
	}
	err = q.QueryRowContext(ctx, `SELECT count(*) FROM "Plan" p WHERE p."creatorId" = ? AND p."status" = 'active'
		AND (SELECT count(*) FROM "PlanTarget" t WHERE t."planId" = p."id") = 1
		AND EXISTS (SELECT 1 FROM "PlanTarget" t WHERE t."planId" = p."id" AND t."userId" = ?)`, a.ID, a.ID).Scan(&paused)
	return outside, paused, err
}
