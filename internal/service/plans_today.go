package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/vinx-lab/vinx-vocab-go/internal/core"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// 今日队列的装配层（旧 services/learning.ts 里 loadPlansForLearners / computeTodayBatch 部分）。
// 学习流（开组、作答、结算）在 learning.go；这里是今日队列的只读计算，供 /today、开组与
// 学习计划详情页的「进度」标签页（GET /plans/:id/progress）共用。

// LearnerPlan 对某学生生效的计划（旧 LearnerPlan = TodayPlanInput & { order, testScope }）。
type LearnerPlan struct {
	core.TodayPlanInput
	Order     string
	TestScope string
}

type planTargetOwner struct {
	UserID  *string
	ClassID *string
}

type membership struct {
	ClassID       string
	TeacherID     string
	AllowSelfPlan bool
}

// LoadPlansForLearners 批量加载多个学生当前可学的计划（含范围词表，同一计划的范围只展开一次）。
//
// 计划对某学生生效的条件：status=active、在日期窗口内，且
//   - 通过班级安排：学生当前仍是该班成员；
//   - 直接安排给学生：本人创建，或创建者是管理员，或学生当前仍在创建者所带的班级里（退班后老师的直接安排随之失效）。
//
// 学生自建、只安排给自己的计划，在他的有效自主为否（所在任一班级不允许自主安排）时不生效——
// 算出来的暂停（spec 0008 §5），不改 status；个人版（ctx 上的版本，见 WithEdition）不看班级，永远允许。
//
// planID 非空时只考虑该计划（旧 loadPlansForLearners(userIds, day, planId?)）。
func LoadPlansForLearners(ctx context.Context, q store.Querier, userIDs []string, day string, planID string) (map[string][]LearnerPlan, error) {
	result := make(map[string][]LearnerPlan, len(userIDs))
	for _, u := range userIDs {
		result[u] = []LearnerPlan{}
	}
	if len(userIDs) == 0 {
		return result, nil
	}

	memRows, err := q.QueryContext(ctx, `SELECT m."userId", m."classId", c."teacherId", c."allowSelfPlan" FROM "ClassMember" m JOIN "Classroom" c ON c."id" = m."classId" WHERE m."userId" IN (`+store.Placeholders(len(userIDs))+`)`, store.Args(userIDs)...)
	if err != nil {
		return nil, err
	}
	membershipsByUser := map[string][]membership{}
	classIDSet := map[string]bool{}
	for memRows.Next() {
		var userID, classID, teacherID string
		var allow bool
		if err := memRows.Scan(&userID, &classID, &teacherID, &allow); err != nil {
			memRows.Close()
			return nil, err
		}
		membershipsByUser[userID] = append(membershipsByUser[userID], membership{ClassID: classID, TeacherID: teacherID, AllowSelfPlan: allow})
		classIDSet[classID] = true
	}
	if err := memRows.Err(); err != nil {
		memRows.Close()
		return nil, err
	}
	memRows.Close()
	classIDs := make([]string, 0, len(classIDSet))
	for c := range classIDSet {
		classIDs = append(classIDs, c)
	}

	cond := `pt."userId" IN (` + store.Placeholders(len(userIDs)) + `)`
	condArgs := append([]any{}, store.Args(userIDs)...)
	if len(classIDs) > 0 {
		cond += ` OR pt."classId" IN (` + store.Placeholders(len(classIDs)) + `)`
		condArgs = append(condArgs, store.Args(classIDs)...)
	}
	query := `SELECT p."id",p."name",p."kind",p."newPerDay",p."reviewPerDay",p."modes",p."testSize",p."creatorId",u."name",u."role",p."createdAt",p."order",p."testScope",p."startDate",p."endDate"
		FROM "Plan" p JOIN "User" u ON u."id" = p."creatorId"
		WHERE p."status" = 'active'`
	args := []any{}
	if planID != "" {
		query += ` AND p."id" = ?`
		args = append(args, planID)
	}
	query += ` AND EXISTS (SELECT 1 FROM "PlanTarget" pt WHERE pt."planId" = p."id" AND (` + cond + `)) ORDER BY p."createdAt" ASC`
	args = append(args, condArgs...)

	planRows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	type rawPlan struct {
		id, name, kind, creatorID, creatorName, creatorRole, order, testScope string
		newPerDay, reviewPerDay, testSize                                     int
		modes                                                                 store.JSON[[]string]
		createdAt                                                             store.Time
		startDate, endDate                                                    *string
	}
	plans := []rawPlan{}
	for planRows.Next() {
		var p rawPlan
		if err := planRows.Scan(&p.id, &p.name, &p.kind, &p.newPerDay, &p.reviewPerDay, &p.modes, &p.testSize, &p.creatorID, &p.creatorName, &p.creatorRole, &p.createdAt, &p.order, &p.testScope, &p.startDate, &p.endDate); err != nil {
			planRows.Close()
			return nil, err
		}
		plans = append(plans, p)
	}
	if err := planRows.Err(); err != nil {
		planRows.Close()
		return nil, err
	}
	planRows.Close()
	if len(plans) == 0 {
		return result, nil
	}

	planIDs := make([]string, len(plans))
	for i, p := range plans {
		planIDs[i] = p.id
	}
	targetRows, err := q.QueryContext(ctx, `SELECT "planId","userId","classId" FROM "PlanTarget" WHERE "planId" IN (`+store.Placeholders(len(planIDs))+`)`, store.Args(planIDs)...)
	if err != nil {
		return nil, err
	}
	targetsByPlan := map[string][]planTargetOwner{}
	for targetRows.Next() {
		var pid string
		var t planTargetOwner
		if err := targetRows.Scan(&pid, &t.UserID, &t.ClassID); err != nil {
			targetRows.Close()
			return nil, err
		}
		targetsByPlan[pid] = append(targetsByPlan[pid], t)
	}
	if err := targetRows.Err(); err != nil {
		targetRows.Close()
		return nil, err
	}
	targetRows.Close()

	unitRows, err := q.QueryContext(ctx, `SELECT "planId","unitId" FROM "PlanUnit" WHERE "planId" IN (`+store.Placeholders(len(planIDs))+`) ORDER BY "planId" ASC, "sortOrder" ASC`, store.Args(planIDs)...)
	if err != nil {
		return nil, err
	}
	unitsByPlan := map[string][]string{}
	for unitRows.Next() {
		var pid, unitID string
		if err := unitRows.Scan(&pid, &unitID); err != nil {
			unitRows.Close()
			return nil, err
		}
		unitsByPlan[pid] = append(unitsByPlan[pid], unitID)
	}
	if err := unitRows.Err(); err != nil {
		unitRows.Close()
		return nil, err
	}
	unitRows.Close()

	useClasses := classesApplyIn(ctx)
	selfAllowed := make(map[string]bool, len(userIDs))
	for _, u := range userIDs {
		allows := []bool{}
		for _, m := range membershipsByUser[u] {
			allows = append(allows, m.AllowSelfPlan)
		}
		selfAllowed[u] = !useClasses || core.SelfPlanAllowed(allows)
	}

	for _, p := range plans {
		if !core.IsPlanInWindow(core.PlanWindow{StartDate: p.startDate, EndDate: p.endDate}, day) {
			continue
		}
		scope, err := ScopeWordIDs(ctx, q, unitsByPlan[p.id])
		if err != nil {
			return nil, err
		}
		modes := p.modes.V
		if modes == nil {
			modes = []string{}
		}
		base := LearnerPlan{
			TodayPlanInput: core.TodayPlanInput{
				ID: p.id, Name: p.name, Kind: p.kind, NewPerDay: p.newPerDay, ReviewPerDay: p.reviewPerDay, Modes: modes,
				TestSize: p.testSize, CreatorID: p.creatorID, CreatorName: p.creatorName, CreatedAt: p.createdAt.Time, ScopeWordIDs: scope,
			},
			Order: p.order, TestScope: p.testScope,
		}
		targets := targetsByPlan[p.id]
		for _, userID := range userIDs {
			if !selfAllowed[userID] && isSelfPlanOf(p.creatorID, userID, targets) {
				continue // 算出来的暂停
			}
			mine := membershipsByUser[userID]
			viaClass := false
			for _, t := range targets {
				if t.ClassID != nil {
					for _, m := range mine {
						if m.ClassID == *t.ClassID {
							viaClass = true
							break
						}
					}
				}
				if viaClass {
					break
				}
			}
			direct := false
			for _, t := range targets {
				if t.UserID != nil && *t.UserID == userID {
					if p.creatorID == userID || p.creatorRole == core.RoleAdmin {
						direct = true
					} else {
						for _, m := range mine {
							if m.TeacherID == p.creatorID {
								direct = true
								break
							}
						}
					}
					break
				}
			}
			if viaClass || direct {
				result[userID] = append(result[userID], base)
			}
		}
	}
	return result, nil
}

// classMembersUserIDs 若干班级的成员 id（去重）。
func classMembersUserIDs(ctx context.Context, q store.Querier, classIDs []string) ([]string, error) {
	if len(classIDs) == 0 {
		return []string{}, nil
	}
	return queryStrings(ctx, q, `SELECT DISTINCT "userId" FROM "ClassMember" WHERE "classId" IN (`+store.Placeholders(len(classIDs))+`)`, store.Args(classIDs)...)
}

// ComputeTodayBatch 批量计算多个学生的今日队列（班级概览、计划进度用；查询次数与学生数无关）。
func ComputeTodayBatch(ctx context.Context, q store.Querier, loc *time.Location, now time.Time, userIDs []string) (map[string]core.TodaySummary, error) {
	out := map[string]core.TodaySummary{}
	if len(userIDs) == 0 {
		return out, nil
	}
	day := core.DayKeyOf(now, loc)
	_, dayEnd := core.DayRange(day, loc)

	plansByUser, err := LoadPlansForLearners(ctx, q, userIDs, day, "")
	if err != nil {
		return nil, err
	}
	planIDSet := map[string]bool{}
	for _, list := range plansByUser {
		for _, p := range list {
			planIDSet[p.ID] = true
		}
	}
	planIDs := make([]string, 0, len(planIDSet))
	for id := range planIDSet {
		planIDs = append(planIDs, id)
	}

	memRows, err := q.QueryContext(ctx, `SELECT "userId","wordId","due","introducedPlanId","introducedDay" FROM "MemoryState" WHERE "userId" IN (`+store.Placeholders(len(userIDs))+`)`, store.Args(userIDs)...)
	if err != nil {
		return nil, err
	}
	memoriesByUser := map[string][]core.MemoryInput{}
	for memRows.Next() {
		var userID string
		var m core.MemoryInput
		var due store.Time
		if err := memRows.Scan(&userID, &m.WordID, &due, &m.IntroducedPlanID, &m.IntroducedDay); err != nil {
			memRows.Close()
			return nil, err
		}
		m.Due = due.Time
		memoriesByUser[userID] = append(memoriesByUser[userID], m)
	}
	if err := memRows.Err(); err != nil {
		memRows.Close()
		return nil, err
	}
	memRows.Close()

	reviewRows, err := q.QueryContext(ctx, `SELECT r."userId", r."wordId", s."planId" FROM "ReviewLog" r JOIN "StudySession" s ON s."id" = r."sessionId"
		WHERE r."dayKey" = ? AND s."kind" = 'review' AND r."userId" IN (`+store.Placeholders(len(userIDs))+`)`, append([]any{day}, store.Args(userIDs)...)...)
	if err != nil {
		return nil, err
	}
	reviewedToday := map[string]map[string]int{}
	seenReview := map[string]bool{}
	for reviewRows.Next() {
		var userID, wordID string
		var planID *string
		if err := reviewRows.Scan(&userID, &wordID, &planID); err != nil {
			reviewRows.Close()
			return nil, err
		}
		if planID == nil {
			continue
		}
		key := userID + "\x00" + *planID + "\x00" + wordID
		if seenReview[key] {
			continue
		}
		seenReview[key] = true
		if reviewedToday[userID] == nil {
			reviewedToday[userID] = map[string]int{}
		}
		reviewedToday[userID][*planID]++
	}
	if err := reviewRows.Err(); err != nil {
		reviewRows.Close()
		return nil, err
	}
	reviewRows.Close()

	activeRows, err := q.QueryContext(ctx, `SELECT "id","userId","planId","kind","wordCount" FROM "StudySession" WHERE "userId" IN (`+store.Placeholders(len(userIDs))+`) AND "status" = 'active'`, store.Args(userIDs)...)
	if err != nil {
		return nil, err
	}
	activeByUser := map[string][]core.ActiveSessionInput{}
	for activeRows.Next() {
		var userID string
		var s core.ActiveSessionInput
		if err := activeRows.Scan(&s.ID, &userID, &s.PlanID, &s.Kind, &s.Total); err != nil {
			activeRows.Close()
			return nil, err
		}
		activeByUser[userID] = append(activeByUser[userID], s)
	}
	if err := activeRows.Err(); err != nil {
		activeRows.Close()
		return nil, err
	}
	activeRows.Close()

	testByUser := map[string][]core.TestResultInput{}
	if len(planIDs) > 0 {
		testRows, err := q.QueryContext(ctx, `SELECT "id","userId","planId","result","completedAt" FROM "StudySession"
			WHERE "userId" IN (`+store.Placeholders(len(userIDs))+`) AND "status" = 'completed' AND "kind" = 'test' AND "planId" IN (`+store.Placeholders(len(planIDs))+`)`,
			append(store.Args(userIDs), store.Args(planIDs)...)...)
		if err != nil {
			return nil, err
		}
		for testRows.Next() {
			var userID string
			var planID *string
			var resultText *string
			var completedAt store.NullTime
			var sessionID string
			if err := testRows.Scan(&sessionID, &userID, &planID, &resultText, &completedAt); err != nil {
				testRows.Close()
				return nil, err
			}
			if planID == nil {
				continue
			}
			r, err := parseSessionResultSummary(resultText)
			if err != nil {
				testRows.Close()
				return nil, err
			}
			completed := completedAt.Time
			testByUser[userID] = append(testByUser[userID], core.TestResultInput{
				PlanID: *planID, SessionID: sessionID, Correct: r.CorrectFirst, Total: r.TotalFirst, CompletedAt: completed,
			})
		}
		if err := testRows.Err(); err != nil {
			testRows.Close()
			return nil, err
		}
		testRows.Close()
	}

	for _, userID := range userIDs {
		lp := plansByUser[userID]
		plans := make([]core.TodayPlanInput, len(lp))
		for i, p := range lp {
			plans[i] = p.TodayPlanInput
		}
		out[userID] = core.BuildToday(core.TodayInput{
			UserID: userID, Today: day, DayEnd: dayEnd, Plans: plans, Memories: memoriesByUser[userID],
			ReviewedToday: reviewedToday[userID], ActiveSessions: activeByUser[userID], TestResults: testByUser[userID],
		})
	}
	return out, nil
}

func parseSessionResultSummary(text *string) (sessionResultSummary, error) {
	var r sessionResultSummary
	if text == nil || *text == "" {
		return r, nil
	}
	if err := json.Unmarshal([]byte(*text), &r); err != nil {
		return r, err
	}
	return r, nil
}
