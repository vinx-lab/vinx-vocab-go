// 学习计划装配与事务（对应旧 routes/plans.ts + services/learning.ts 的今日队列部分）。
//
// 学习流（开组、作答、结算，FSRS 排程）在 learning.go；本文件的 K22「删除计划前结算已作答进行中组」
// 调用其中的 CompleteSessionTx。
package service

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/vinx-lab/vinx-vocab-go/internal/core"
	"github.com/vinx-lab/vinx-vocab-go/internal/httpx"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// ===========================================================================
// 行与视图类型
// ===========================================================================

// planRow Plan 表一行 + 创建者姓名（旧 db.plan.findUnique({ include: { creator } })）。
type planRow struct {
	ID           string
	Name         string
	CreatorID    string
	CreatorName  string
	Kind         string
	Status       string
	NewPerDay    int
	ReviewPerDay int
	Modes        store.JSON[[]string]
	Order        string
	TestSize     int
	TestScope    string
	StartDate    *string
	EndDate      *string
	CreatedAt    store.Time
	UpdatedAt    store.Time
}

const planColumns = `p."id",p."name",p."creatorId",u."name",p."kind",p."status",p."newPerDay",p."reviewPerDay",p."modes",p."order",p."testSize",p."testScope",p."startDate",p."endDate",p."createdAt",p."updatedAt"`

const planFrom = `"Plan" p JOIN "User" u ON u."id" = p."creatorId"`

func scanPlanRow(row interface{ Scan(...any) error }) (*planRow, error) {
	var p planRow
	if err := row.Scan(&p.ID, &p.Name, &p.CreatorID, &p.CreatorName, &p.Kind, &p.Status, &p.NewPerDay, &p.ReviewPerDay, &p.Modes, &p.Order, &p.TestSize, &p.TestScope, &p.StartDate, &p.EndDate, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}
	return &p, nil
}

// GetPlanRow 按 id 取原始行（含创建者姓名）；不存在返回 (nil, nil)。
func GetPlanRow(ctx context.Context, q store.Querier, id string) (*planRow, error) {
	p, err := scanPlanRow(q.QueryRowContext(ctx, `SELECT `+planColumns+` FROM `+planFrom+` WHERE p."id" = ?`, id))
	if store.IsNoRows(err) {
		return nil, nil
	}
	return p, err
}

// GetVisiblePlanRow 按 id 取当前操作者可见的一行；不可见 / 不存在返回 (nil, nil)。
func GetVisiblePlanRow(ctx context.Context, q store.Querier, a *Actor, id string) (*planRow, error) {
	where, args, err := VisiblePlanWhere(ctx, q, a)
	if err != nil {
		return nil, err
	}
	p, err := scanPlanRow(q.QueryRowContext(ctx, `SELECT `+planColumns+` FROM `+planFrom+` WHERE p."id" = ? AND `+where, append([]any{id}, args...)...))
	if store.IsNoRows(err) {
		return nil, nil
	}
	return p, err
}

// PlanCreatorView 计划视图里的创建者。
type PlanCreatorView struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// PlanUnitView 计划视图里的单元。
type PlanUnitView struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	BookID   string `json:"bookId"`
	BookName string `json:"bookName"`
}

// PlanTargetView 计划视图里的一个安排对象。
type PlanTargetView struct {
	Type  string  `json:"type"` // class | user
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Email *string `json:"email,omitempty"`
}

// PlanView 计划详情（旧 serializePlan 的返回形状）。
type PlanView struct {
	ID           string           `json:"id"`
	Name         string           `json:"name"`
	Kind         string           `json:"kind"`
	Status       string           `json:"status"`
	NewPerDay    int              `json:"newPerDay"`
	ReviewPerDay int              `json:"reviewPerDay"`
	Modes        []string         `json:"modes"`
	Order        string           `json:"order"`
	TestSize     int              `json:"testSize"`
	TestScope    string           `json:"testScope"`
	StartDate    *string          `json:"startDate"`
	EndDate      *string          `json:"endDate"`
	Creator      PlanCreatorView  `json:"creator"`
	Units        []PlanUnitView   `json:"units"`
	Targets      []PlanTargetView `json:"targets"`
	WordCount    int              `json:"wordCount"`
	TargetsMe    bool             `json:"targetsMe"`
	IsSelfPlan   bool             `json:"isSelfPlan"`
	CanEdit      bool             `json:"canEdit"`
	CreatedAt    store.Time       `json:"createdAt"`
	UpdatedAt    store.Time       `json:"updatedAt"`
}

// planUnitIDs 计划的单元 id（按 sortOrder）。
func planUnitIDs(ctx context.Context, q store.Querier, planID string) ([]string, error) {
	return queryStrings(ctx, q, `SELECT "unitId" FROM "PlanUnit" WHERE "planId" = ? ORDER BY "sortOrder" ASC`, planID)
}

func planUnitViews(ctx context.Context, q store.Querier, planID string) ([]PlanUnitView, error) {
	rows, err := q.QueryContext(ctx, `SELECT un."id", un."name", bk."id", bk."name"
		FROM "PlanUnit" pu JOIN "Unit" un ON un."id" = pu."unitId" JOIN "Book" bk ON bk."id" = un."bookId"
		WHERE pu."planId" = ? ORDER BY pu."sortOrder" ASC`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PlanUnitView{}
	for rows.Next() {
		var u PlanUnitView
		if err := rows.Scan(&u.ID, &u.Name, &u.BookID, &u.BookName); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

type planTargetRaw struct {
	ID      string
	UserID  *string
	ClassID *string
}

func planTargetsRaw(ctx context.Context, q store.Querier, planID string) ([]planTargetRaw, error) {
	rows, err := q.QueryContext(ctx, `SELECT "id","userId","classId" FROM "PlanTarget" WHERE "planId" = ? ORDER BY "id" ASC`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []planTargetRaw{}
	for rows.Next() {
		var t planTargetRaw
		if err := rows.Scan(&t.ID, &t.UserID, &t.ClassID); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func planTargetViews(ctx context.Context, q store.Querier, planID string) ([]PlanTargetView, error) {
	raw, err := planTargetsRaw(ctx, q, planID)
	if err != nil {
		return nil, err
	}
	out := []PlanTargetView{}
	for _, t := range raw {
		if t.ClassID != nil {
			var name string
			if err := q.QueryRowContext(ctx, `SELECT "name" FROM "Classroom" WHERE "id" = ?`, *t.ClassID).Scan(&name); err != nil {
				return nil, err
			}
			out = append(out, PlanTargetView{Type: "class", ID: *t.ClassID, Name: name})
		} else if t.UserID != nil {
			var name, email string
			if err := q.QueryRowContext(ctx, `SELECT "name","email" FROM "User" WHERE "id" = ?`, *t.UserID).Scan(&name, &email); err != nil {
				return nil, err
			}
			out = append(out, PlanTargetView{Type: "user", ID: *t.UserID, Name: name, Email: &email})
		}
	}
	return out, nil
}

// SerializePlan 组装计划详情视图（旧 serializePlan）。
func SerializePlan(ctx context.Context, q store.Querier, row *planRow, a *Actor, myClassIDs []string) (*PlanView, error) {
	unitIDs, err := planUnitIDs(ctx, q, row.ID)
	if err != nil {
		return nil, err
	}
	units, err := planUnitViews(ctx, q, row.ID)
	if err != nil {
		return nil, err
	}
	targets, err := planTargetViews(ctx, q, row.ID)
	if err != nil {
		return nil, err
	}
	scope, err := ScopeWordIDs(ctx, q, unitIDs)
	if err != nil {
		return nil, err
	}
	myClasses := map[string]bool{}
	for _, c := range myClassIDs {
		myClasses[c] = true
	}
	targetsMe := false
	for _, t := range targets {
		if (t.Type == "user" && t.ID == a.ID) || (t.Type == "class" && myClasses[t.ID]) {
			targetsMe = true
			break
		}
	}
	isSelfPlan := row.CreatorID == a.ID && len(targets) == 1 && targets[0].Type == "user" && targets[0].ID == a.ID
	canEdit := SeesAll(a) || (row.CreatorID == a.ID && Can(a, core.CapPlans))
	modes := row.Modes.V
	if modes == nil {
		modes = []string{}
	}
	return &PlanView{
		ID: row.ID, Name: row.Name, Kind: row.Kind, Status: row.Status, NewPerDay: row.NewPerDay, ReviewPerDay: row.ReviewPerDay,
		Modes: modes, Order: row.Order, TestSize: row.TestSize, TestScope: row.TestScope, StartDate: row.StartDate, EndDate: row.EndDate,
		Creator: PlanCreatorView{ID: row.CreatorID, Name: row.CreatorName}, Units: units, Targets: targets, WordCount: len(scope),
		TargetsMe: targetsMe, IsSelfPlan: isSelfPlan, CanEdit: canEdit, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}, nil
}

// ===========================================================================
// 可见性、目标解析、校验
// ===========================================================================

// VisiblePlanWhere 计划可见性：创建者 / 安排给我（直接或班级）/ 我管理的班级或学生 / 管理员（旧 visiblePlanWhere）。
// 返回可拼进 WHERE 的 SQL 片段（已加括号）与参数；片段引用别名 "p"。
// 个人版（含管理员）：只看自己创建的与安排给自己的（SeesAll 为假，students.view 关闭）。
func VisiblePlanWhere(ctx context.Context, q store.Querier, a *Actor) (string, []any, error) {
	if SeesAll(a) {
		return "1=1", nil, nil
	}
	myClassIDs, err := MemberClassIDs(ctx, q, a.ID)
	if err != nil {
		return "", nil, err
	}
	ors := []string{`p."creatorId" = ?`}
	args := []any{a.ID}
	ors = append(ors, `EXISTS (SELECT 1 FROM "PlanTarget" pt WHERE pt."planId" = p."id" AND pt."userId" = ?)`)
	args = append(args, a.ID)
	if len(myClassIDs) > 0 {
		ors = append(ors, `EXISTS (SELECT 1 FROM "PlanTarget" pt WHERE pt."planId" = p."id" AND pt."classId" IN (`+store.Placeholders(len(myClassIDs))+`))`)
		args = append(args, store.Args(myClassIDs)...)
	}
	if Can(a, core.CapStudentsView) {
		managed, err := ManageableClassIDs(ctx, q, a)
		if err != nil {
			return "", nil, err
		}
		if len(managed) > 0 {
			ors = append(ors, `EXISTS (SELECT 1 FROM "PlanTarget" pt WHERE pt."planId" = p."id" AND pt."classId" IN (`+store.Placeholders(len(managed))+`))`)
			args = append(args, store.Args(managed)...)
			ors = append(ors, `EXISTS (SELECT 1 FROM "PlanTarget" pt JOIN "ClassMember" cm ON cm."userId" = pt."userId" WHERE pt."planId" = p."id" AND cm."classId" IN (`+store.Placeholders(len(managed))+`))`)
			args = append(args, store.Args(managed)...)
		}
	}
	return "(" + strings.Join(ors, " OR ") + ")", args, nil
}

func uniqueStrings(xs []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		if seen[x] {
			continue
		}
		seen[x] = true
		out = append(out, x)
	}
	return out
}

// PlanTargetsInput 计划安排对象（旧 targets: { classIds, userIds }）。
type PlanTargetsInput struct {
	ClassIDs []string
	UserIDs  []string
}

// ResolveTargets 校验并规整安排对象：空 = 自己；给他人安排需权限且在管理范围内（旧 resolveTargets）。
func ResolveTargets(ctx context.Context, q store.Querier, a *Actor, in PlanTargetsInput) (PlanTargetsInput, error) {
	classIDs := uniqueStrings(in.ClassIDs)
	userIDs := uniqueStrings(in.UserIDs)
	if len(classIDs) == 0 && len(userIDs) == 0 {
		return PlanTargetsInput{ClassIDs: []string{}, UserIDs: []string{a.ID}}, nil
	}
	others := make([]string, 0, len(userIDs))
	for _, u := range userIDs {
		if u != a.ID {
			others = append(others, u)
		}
	}
	if len(classIDs) > 0 || len(others) > 0 {
		if err := AssertCan(a, core.CapPlansAssign, "没有为他人安排计划的权限"); err != nil {
			return PlanTargetsInput{}, err
		}
		if !IsAdmin(a) {
			mine, err := ManageableClassIDs(ctx, q, a)
			if err != nil {
				return PlanTargetsInput{}, err
			}
			mineSet := map[string]bool{}
			for _, c := range mine {
				mineSet[c] = true
			}
			for _, c := range classIDs {
				if !mineSet[c] {
					return PlanTargetsInput{}, httpx.Forbidden("只能给自己的班级安排计划")
				}
			}
			for _, u := range others {
				ok, err := IsStudentOfTeacher(ctx, q, a.ID, u)
				if err != nil {
					return PlanTargetsInput{}, err
				}
				if !ok {
					return PlanTargetsInput{}, httpx.Forbidden("只能给本班学生安排计划")
				}
			}
		}
	}
	clsCount, err := countIn(ctx, q, `"Classroom"`, classIDs)
	if err != nil {
		return PlanTargetsInput{}, err
	}
	userCount, err := countIn(ctx, q, `"User"`, userIDs)
	if err != nil {
		return PlanTargetsInput{}, err
	}
	if clsCount != len(classIDs) || userCount != len(userIDs) {
		return PlanTargetsInput{}, httpx.Validation("安排对象不存在")
	}
	return PlanTargetsInput{ClassIDs: classIDs, UserIDs: userIDs}, nil
}

func countIn(ctx context.Context, q store.Querier, table string, ids []string) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	var n int
	err := q.QueryRowContext(ctx, `SELECT count(*) FROM `+table+` WHERE "id" IN (`+store.Placeholders(len(ids))+`)`, store.Args(ids)...).Scan(&n)
	return n, err
}

// AssertUnitsVisible 单元必须都在当前操作者可见的词书范围内（旧 assertUnitsVisible）。
func AssertUnitsVisible(ctx context.Context, q store.Querier, a *Actor, unitIDs []string) error {
	if len(unitIDs) == 0 {
		return nil
	}
	where, args, err := VisibleBookFilter(ctx, q, a, "b")
	if err != nil {
		return err
	}
	ids := uniqueStrings(unitIDs)
	var n int
	query := `SELECT count(*) FROM "Unit" u JOIN "Book" b ON b."id" = u."bookId" WHERE u."id" IN (` + store.Placeholders(len(ids)) + `) AND ` + where
	if err := q.QueryRowContext(ctx, query, append(store.Args(ids), args...)...).Scan(&n); err != nil {
		return err
	}
	if n != len(ids) {
		return httpx.Validation("包含不可用的单元")
	}
	return nil
}

// CheckPlanDates 结束日期不能早于开始日期（旧 checkDates）。
func CheckPlanDates(startDate, endDate *string) error {
	if startDate != nil && endDate != nil && *endDate < *startDate {
		return httpx.Validation("结束日期不能早于开始日期")
	}
	return nil
}

// LoadEditablePlan 可编辑的计划：不存在 → 404；非管理员需 CapPlans 且是创建者（旧 loadEditable）。
func LoadEditablePlan(ctx context.Context, q store.Querier, a *Actor, id string) (*planRow, error) {
	plan, err := GetPlanRow(ctx, q, id)
	if err != nil {
		return nil, err
	}
	if plan == nil {
		return nil, httpx.NotFound("计划不存在")
	}
	if !SeesAll(a) {
		if err := AssertCan(a, core.CapPlans, ""); err != nil {
			return nil, err
		}
		if plan.CreatorID != a.ID {
			return nil, httpx.Forbidden("只能修改自己创建的计划")
		}
	}
	return plan, nil
}

// ===========================================================================
// 列表
// ===========================================================================

// ListPlansQuery GET /plans 的查询参数。
type ListPlansQuery struct {
	Scope   string // all | mine | created
	ClassID string
	Status  string
}

// ListPlans 当前操作者可见的计划列表（旧 GET /plans）。
func ListPlans(ctx context.Context, q store.Querier, a *Actor, query ListPlansQuery) ([]PlanView, error) {
	myClassIDs, err := MemberClassIDs(ctx, q, a.ID)
	if err != nil {
		return nil, err
	}
	where, args, err := VisiblePlanWhere(ctx, q, a)
	if err != nil {
		return nil, err
	}
	and := []string{where}
	all := append([]any{}, args...)
	switch query.Scope {
	case "mine":
		cond := `EXISTS (SELECT 1 FROM "PlanTarget" pt WHERE pt."planId" = p."id" AND (pt."userId" = ?`
		condArgs := []any{a.ID}
		if len(myClassIDs) > 0 {
			cond += ` OR pt."classId" IN (` + store.Placeholders(len(myClassIDs)) + `)`
			condArgs = append(condArgs, store.Args(myClassIDs)...)
		}
		cond += `))`
		and = append(and, cond)
		all = append(all, condArgs...)
	case "created":
		and = append(and, `p."creatorId" = ?`)
		all = append(all, a.ID)
	}
	if query.ClassID != "" {
		and = append(and, `EXISTS (SELECT 1 FROM "PlanTarget" pt WHERE pt."planId" = p."id" AND pt."classId" = ?)`)
		all = append(all, query.ClassID)
	}
	if query.Status != "" {
		and = append(and, `p."status" = ?`)
		all = append(all, query.Status)
	}
	sqlQuery := `SELECT ` + planColumns + ` FROM ` + planFrom + ` WHERE ` + strings.Join(and, " AND ") + ` ORDER BY p."status" ASC, p."createdAt" DESC`
	rows, err := q.QueryContext(ctx, sqlQuery, all...)
	if err != nil {
		return nil, err
	}
	planRows := []*planRow{}
	for rows.Next() {
		p, err := scanPlanRow(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		planRows = append(planRows, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	items := make([]PlanView, 0, len(planRows))
	for _, p := range planRows {
		v, err := SerializePlan(ctx, q, p, a, myClassIDs)
		if err != nil {
			return nil, err
		}
		items = append(items, *v)
	}
	return items, nil
}

// PreviewUnitItem POST /plans/preview 的一个单元。
type PreviewUnitItem struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	WordCount int    `json:"wordCount"`
}

// PreviewPlanResult POST /plans/preview 的响应体。
type PreviewPlanResult struct {
	WordCount int               `json:"wordCount"`
	Units     []PreviewUnitItem `json:"units"`
}

// PreviewPlan 预览计划范围的词数（旧 POST /plans/preview）。
func PreviewPlan(ctx context.Context, q store.Querier, a *Actor, unitIDs []string) (*PreviewPlanResult, error) {
	if len(unitIDs) == 0 {
		return &PreviewPlanResult{WordCount: 0, Units: []PreviewUnitItem{}}, nil
	}
	if err := AssertUnitsVisible(ctx, q, a, unitIDs); err != nil {
		return nil, err
	}
	rows, err := q.QueryContext(ctx, `SELECT u."id", u."name", (SELECT count(*) FROM "UnitWord" uw WHERE uw."unitId" = u."id")
		FROM "Unit" u WHERE u."id" IN (`+store.Placeholders(len(unitIDs))+`)`, store.Args(unitIDs)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	units := []PreviewUnitItem{}
	for rows.Next() {
		var it PreviewUnitItem
		if err := rows.Scan(&it.ID, &it.Name, &it.WordCount); err != nil {
			return nil, err
		}
		units = append(units, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	scope, err := ScopeWordIDs(ctx, q, unitIDs)
	if err != nil {
		return nil, err
	}
	return &PreviewPlanResult{WordCount: len(scope), Units: units}, nil
}

// ===========================================================================
// 创建 / 更新 / 删除
// ===========================================================================

// PlanInput 建计划的输入（旧 planBase，默认值已由请求校验层套用）。
type PlanInput struct {
	Name         string
	Kind         string
	Status       string
	NewPerDay    int
	ReviewPerDay int
	Modes        []string
	Order        string
	TestSize     int
	TestScope    string
	StartDate    *string
	EndDate      *string
	UnitIDs      []string
	Targets      PlanTargetsInput
}

func insertPlanTargets(ctx context.Context, tx store.Querier, planID string, targets PlanTargetsInput) error {
	for _, classID := range targets.ClassIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO "PlanTarget" ("id","planId","classId") VALUES (?,?,?)`, store.NewID(), planID, classID); err != nil {
			return err
		}
	}
	for _, userID := range targets.UserIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO "PlanTarget" ("id","planId","userId") VALUES (?,?,?)`, store.NewID(), planID, userID); err != nil {
			return err
		}
	}
	return nil
}

// CreatePlan 建计划（必须在事务里调用：写 Plan + PlanUnit + PlanTarget）。
func CreatePlan(ctx context.Context, tx store.Querier, now time.Time, creatorID string, in PlanInput) (*planRow, error) {
	id := store.NewID()
	ts := store.NewTime(now)
	modes := store.NewJSON(in.Modes)
	_, err := tx.ExecContext(ctx, `INSERT INTO "Plan" ("id","name","creatorId","kind","status","newPerDay","reviewPerDay","modes","order","testSize","testScope","startDate","endDate","createdAt","updatedAt")
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, in.Name, creatorID, in.Kind, in.Status, in.NewPerDay, in.ReviewPerDay, modes, in.Order, in.TestSize, in.TestScope, in.StartDate, in.EndDate, ts, ts)
	if err != nil {
		return nil, err
	}
	unitIDs := uniqueStrings(in.UnitIDs)
	for i, unitID := range unitIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO "PlanUnit" ("planId","unitId","sortOrder") VALUES (?,?,?)`, id, unitID, i); err != nil {
			return nil, err
		}
	}
	if err := insertPlanTargets(ctx, tx, id, in.Targets); err != nil {
		return nil, err
	}
	return GetPlanRow(ctx, tx, id)
}

// PlanPatch 计划部分更新（旧 planUpdate = planBase.partial()）；nil 指针 / nil 切片表示不改。
type PlanPatch struct {
	Name         *string
	Kind         *string
	Status       *string
	NewPerDay    *int
	ReviewPerDay *int
	Modes        []string // nil = 不改
	Order        *string
	TestSize     *int
	TestScope    *string
	// StartDate / EndDate：nil = 不改；非 nil 且 Valid=false = 清空为 null；非 nil 且 Valid=true = 设为值。
	StartDate *sql.NullString
	EndDate   *sql.NullString
	UnitIDs   []string          // nil = 不改（区别于长度为 0，请求校验已保证非空）
	Targets   *PlanTargetsInput // nil = 不改
}

// UpdatePlan 更新计划（必须在事务里调用）。
func UpdatePlan(ctx context.Context, tx store.Querier, now time.Time, id string, p PlanPatch) (*planRow, error) {
	if p.UnitIDs != nil {
		if _, err := tx.ExecContext(ctx, `DELETE FROM "PlanUnit" WHERE "planId" = ?`, id); err != nil {
			return nil, err
		}
		for i, unitID := range uniqueStrings(p.UnitIDs) {
			if _, err := tx.ExecContext(ctx, `INSERT INTO "PlanUnit" ("planId","unitId","sortOrder") VALUES (?,?,?)`, id, unitID, i); err != nil {
				return nil, err
			}
		}
	}
	if p.Targets != nil {
		if _, err := tx.ExecContext(ctx, `DELETE FROM "PlanTarget" WHERE "planId" = ?`, id); err != nil {
			return nil, err
		}
		if err := insertPlanTargets(ctx, tx, id, *p.Targets); err != nil {
			return nil, err
		}
	}
	// 旧版 tx.plan.update({ data: fields }) 里 fields 不含 unitIds/targets（单独处理）；
	// fields 为空对象时（PATCH {} 或只改 unitIds/targets）Prisma 不会刷新 @updatedAt（已对 oracle 实测确认），
	// 所以这里只有存在标量字段时才拼 updatedAt 和执行这条 UPDATE。
	var sets []string
	var args []any
	if p.Name != nil {
		sets = append(sets, `"name" = ?`)
		args = append(args, *p.Name)
	}
	if p.Kind != nil {
		sets = append(sets, `"kind" = ?`)
		args = append(args, *p.Kind)
	}
	if p.Status != nil {
		sets = append(sets, `"status" = ?`)
		args = append(args, *p.Status)
	}
	if p.NewPerDay != nil {
		sets = append(sets, `"newPerDay" = ?`)
		args = append(args, *p.NewPerDay)
	}
	if p.ReviewPerDay != nil {
		sets = append(sets, `"reviewPerDay" = ?`)
		args = append(args, *p.ReviewPerDay)
	}
	if p.Modes != nil {
		sets = append(sets, `"modes" = ?`)
		args = append(args, store.NewJSON(p.Modes))
	}
	if p.Order != nil {
		sets = append(sets, `"order" = ?`)
		args = append(args, *p.Order)
	}
	if p.TestSize != nil {
		sets = append(sets, `"testSize" = ?`)
		args = append(args, *p.TestSize)
	}
	if p.TestScope != nil {
		sets = append(sets, `"testScope" = ?`)
		args = append(args, *p.TestScope)
	}
	if p.StartDate != nil {
		sets = append(sets, `"startDate" = ?`)
		if p.StartDate.Valid {
			args = append(args, p.StartDate.String)
		} else {
			args = append(args, nil)
		}
	}
	if p.EndDate != nil {
		sets = append(sets, `"endDate" = ?`)
		if p.EndDate.Valid {
			args = append(args, p.EndDate.String)
		} else {
			args = append(args, nil)
		}
	}
	if len(sets) > 0 {
		sets = append(sets, `"updatedAt" = ?`)
		args = append(args, store.NewTime(now))
		args = append(args, id)
		if _, err := tx.ExecContext(ctx, `UPDATE "Plan" SET `+strings.Join(sets, ", ")+` WHERE "id" = ?`, args...); err != nil {
			return nil, err
		}
	}
	return GetPlanRow(ctx, tx, id)
}

// sessionResultSummary 结算结果（今日队列读取 correctFirst / totalFirst）。
type sessionResultSummary = SessionResult

// SettleActiveSessionsForPlan 结算某计划下已作答的进行中组（K22：删除计划前保留学习记录）。
// 与旧 DELETE /plans/:id 相同：每个已作答的进行中组按组主人走完整结算（CompleteSessionTx：FSRS 排程、
// MemoryState / ReviewLog 更新，completedDay 为结算当天）；未作答的组不动，交给调用方随计划删除。
// 必须在事务里调用。
func SettleActiveSessionsForPlan(ctx context.Context, tx store.Querier, loc *time.Location, now time.Time, planID string) error {
	rows, err := tx.QueryContext(ctx, `SELECT s."id", s."userId" FROM "StudySession" s
		WHERE s."planId" = ? AND s."status" = 'active' AND EXISTS (SELECT 1 FROM "Answer" a WHERE a."sessionId" = s."id")
		ORDER BY s.rowid`, planID)
	if err != nil {
		return err
	}
	type active struct{ id, userID string }
	list := []active{}
	for rows.Next() {
		var a active
		if err := rows.Scan(&a.id, &a.userID); err != nil {
			rows.Close()
			return err
		}
		list = append(list, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, a := range list {
		if _, err := CompleteSessionTx(ctx, tx, loc, now, a.userID, a.id); err != nil {
			return err
		}
	}
	return nil
}

// DeletePlan 删除计划（K22）：先结算已作答的进行中组，删除未作答的进行中组，最后删计划
// （已完成的组通过外键 ON DELETE SET NULL 保留，planId 置空）。必须在事务里调用。
func DeletePlan(ctx context.Context, tx store.Querier, loc *time.Location, now time.Time, id string) error {
	if err := SettleActiveSessionsForPlan(ctx, tx, loc, now, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM "StudySession" WHERE "planId" = ? AND "status" = 'active'`, id); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM "Plan" WHERE "id" = ?`, id)
	return err
}
