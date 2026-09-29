package service

import (
	"context"
	"time"

	"github.com/vinx-lab/vinx-vocab-go/internal/core"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// PlanProgressToday 计划进度里某学生的今日完成情况（旧 GET /plans/:id/progress 的 items[].today）。
type PlanProgressToday struct {
	NewDone    int                   `json:"newDone"`
	NewLeft    int                   `json:"newLeft"`
	ReviewDone int                   `json:"reviewDone"`
	ReviewLeft int                   `json:"reviewLeft"`
	DoneToday  bool                  `json:"doneToday"`
	TestResult *core.TodayTestResult `json:"testResult"`
}

// PlanProgressItem 计划进度里的一个学生。
type PlanProgressItem struct {
	UserID       string             `json:"userId"`
	Name         string             `json:"name"`
	Email        string             `json:"email"`
	LearnedWords int                `json:"learnedWords"`
	TotalWords   int                `json:"totalWords"`
	Today        *PlanProgressToday `json:"today"`
}

// PlanProgressResult 旧 GET /plans/:id/progress 的响应体。
type PlanProgressResult struct {
	Day        string             `json:"day"`
	TotalWords int                `json:"totalWords"`
	Items      []PlanProgressItem `json:"items"`
	Total      int                `json:"total"`
}

// PlanProgress 计划进度：每个安排对象的学生范围内学习进度与今日完成情况（旧 GET /plans/:id/progress）。
// 计划不可见 / 不存在返回 (nil, nil)（路由统一转 404「计划不存在」）。
func PlanProgress(ctx context.Context, q store.Querier, loc *time.Location, now time.Time, a *Actor, planID string) (*PlanProgressResult, error) {
	plan, err := GetVisiblePlanRow(ctx, q, a, planID)
	if err != nil || plan == nil {
		return nil, err
	}
	isManager := SeesAll(a) || plan.CreatorID == a.ID || Can(a, core.CapStudentsView)

	targets, err := planTargetsRaw(ctx, q, planID)
	if err != nil {
		return nil, err
	}
	classIDSet := map[string]bool{}
	for _, t := range targets {
		if t.ClassID != nil {
			classIDSet[*t.ClassID] = true
		}
	}
	classIDs := make([]string, 0, len(classIDSet))
	for c := range classIDSet {
		classIDs = append(classIDs, c)
	}
	members, err := classMembersUserIDs(ctx, q, classIDs)
	if err != nil {
		return nil, err
	}
	candidateSet := map[string]bool{}
	candidates := []string{}
	add := func(u string) {
		if u != "" && !candidateSet[u] {
			candidateSet[u] = true
			candidates = append(candidates, u)
		}
	}
	for _, t := range targets {
		if t.UserID != nil {
			add(*t.UserID)
		}
	}
	for _, u := range members {
		add(u)
	}

	day := core.DayKeyOf(now, loc)
	effective, err := LoadPlansForLearners(ctx, q, candidates, day, planID)
	if err != nil {
		return nil, err
	}
	learnerIDs := []string{}
	for _, u := range candidates {
		if len(effective[u]) > 0 || plan.Status != "active" {
			learnerIDs = append(learnerIDs, u)
		}
	}
	if !isManager {
		filtered := learnerIDs[:0:0]
		for _, u := range learnerIDs {
			if u == a.ID {
				filtered = append(filtered, u)
			}
		}
		learnerIDs = filtered
	} else if !SeesAll(a) {
		// 老师（含创建者）：仅限自己 + 仍在自己班级里的学生
		managed, err := ManageableClassIDs(ctx, q, a)
		if err != nil {
			return nil, err
		}
		allowed := map[string]bool{}
		if len(managed) > 0 {
			allowedUsers, err := classMembersUserIDs(ctx, q, managed)
			if err != nil {
				return nil, err
			}
			for _, u := range allowedUsers {
				allowed[u] = true
			}
		}
		filtered := learnerIDs[:0:0]
		for _, u := range learnerIDs {
			if u == a.ID || allowed[u] {
				filtered = append(filtered, u)
			}
		}
		learnerIDs = filtered
	}

	unitIDs, err := planUnitIDs(ctx, q, planID)
	if err != nil {
		return nil, err
	}
	scope, err := ScopeWordIDs(ctx, q, unitIDs)
	if err != nil {
		return nil, err
	}

	users, err := usersByIDs(ctx, q, learnerIDs)
	if err != nil {
		return nil, err
	}
	learnedCounts, err := memoryStateCounts(ctx, q, learnerIDs, scope)
	if err != nil {
		return nil, err
	}
	queues, err := ComputeTodayBatch(ctx, q, loc, now, learnerIDs)
	if err != nil {
		return nil, err
	}

	items := make([]PlanProgressItem, 0, len(users))
	for _, u := range users {
		var todayView *PlanProgressToday
		if summary, ok := queues[u.ID]; ok {
			for _, c := range summary.Plans {
				if c.PlanID == planID {
					todayView = &PlanProgressToday{NewDone: c.NewDoneToday, NewLeft: c.NewLeft, ReviewDone: c.ReviewDoneToday, ReviewLeft: c.ReviewLeft, DoneToday: c.DoneToday, TestResult: c.TestResult}
					break
				}
			}
		}
		items = append(items, PlanProgressItem{
			UserID: u.ID, Name: u.Name, Email: u.Email, LearnedWords: learnedCounts[u.ID], TotalWords: len(scope), Today: todayView,
		})
	}
	return &PlanProgressResult{Day: day, TotalWords: len(scope), Items: items, Total: len(items)}, nil
}

type userBasic struct {
	ID, Name, Email string
}

func usersByIDs(ctx context.Context, q store.Querier, ids []string) ([]userBasic, error) {
	if len(ids) == 0 {
		return []userBasic{}, nil
	}
	// 旧版 db.user.findMany 不带 orderBy：PostgreSQL 按物理堆顺序返回，未加索引提示时接近插入顺序，
	// 但语言层面不保证。这里按 id（cuid，前缀是创建时间的 base36）升序作为确定性的插入顺序近似，
	// 迁移演练中比按 name 排序更贴近旧版的实际结果。
	rows, err := q.QueryContext(ctx, `SELECT "id","name","email" FROM "User" WHERE "id" IN (`+store.Placeholders(len(ids))+`) ORDER BY "id" ASC`, store.Args(ids)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []userBasic{}
	for rows.Next() {
		var u userBasic
		if err := rows.Scan(&u.ID, &u.Name, &u.Email); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func memoryStateCounts(ctx context.Context, q store.Querier, userIDs, wordIDs []string) (map[string]int, error) {
	out := map[string]int{}
	if len(userIDs) == 0 || len(wordIDs) == 0 {
		return out, nil
	}
	rows, err := q.QueryContext(ctx, `SELECT "userId", count(*) FROM "MemoryState" WHERE "userId" IN (`+store.Placeholders(len(userIDs))+`) AND "wordId" IN (`+store.Placeholders(len(wordIDs))+`) GROUP BY "userId"`,
		append(store.Args(userIDs), store.Args(wordIDs)...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var userID string
		var n int
		if err := rows.Scan(&userID, &n); err != nil {
			return nil, err
		}
		out[userID] = n
	}
	return out, rows.Err()
}
