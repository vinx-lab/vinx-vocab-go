package core

import (
	"math/rand"
	"sort"
	"time"
)

// 今日队列（纯函数，对应旧 lib/today.ts）。不预建任务：每次请求根据生效计划、个人记忆状态和
// 今日已完成量实时计算。设计见 docs/product.md §4。

const (
	// LearnGroupSize 一次学新词的组大小。
	LearnGroupSize = 10
	// ReviewGroupSize 一次复习的组大小。
	ReviewGroupSize = 20
	// DrillGroupSize 一次错词强化的组大小。
	DrillGroupSize = 20
)

// TodayPlanInput 计划在今日计算里需要的字段（装配层从 Plan + PlanUnit + 计划范围词表 拼出）。
type TodayPlanInput struct {
	ID           string
	Name         string
	Kind         string // daily | test
	NewPerDay    int
	ReviewPerDay int
	Modes        []string
	TestSize     int
	CreatorID    string
	CreatorName  string
	CreatedAt    time.Time
	// ScopeWordIDs 计划范围内的词，已按单元顺序 + 单元内顺序排列、去重。
	ScopeWordIDs []string
}

// MemoryInput 今日计算需要的记忆状态字段。
type MemoryInput struct {
	WordID           string
	Due              time.Time
	IntroducedPlanID *string
	IntroducedDay    string
}

// ActiveSessionInput 进行中的学习组（今日计算需要的字段）。
type ActiveSessionInput struct {
	ID     string
	PlanID *string
	Kind   string
	Total  int
}

// TestResultInput 已完成的检测成绩（今日计算需要的字段）。
type TestResultInput struct {
	PlanID      string
	SessionID   string
	Correct     int
	Total       int
	CompletedAt time.Time
}

// TodayInput 计算一个学生今日队列所需的全部输入。
type TodayInput struct {
	UserID string
	Today  string
	DayEnd time.Time
	Plans  []TodayPlanInput
	// Memories 该学生已有记忆状态（不限于任一计划范围）。
	Memories []MemoryInput
	// ReviewedToday 今日在各计划复习组中已结算的去重词数（planId → 数量）。
	ReviewedToday  map[string]int
	ActiveSessions []ActiveSessionInput
	TestResults    []TestResultInput
}

// TodayTestResult 计划检测卡片里的最近成绩（首次交卷为正式成绩）。
type TodayTestResult struct {
	SessionID   string `json:"sessionId"`
	Correct     int    `json:"correct"`
	Total       int    `json:"total"`
	CompletedAt string `json:"completedAt"`
}

// TodayActiveSession 计划卡片里的进行中组摘要。
type TodayActiveSession struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Total int    `json:"total"`
}

// TodayPlanCard 一个计划的今日卡片（旧 TodayPlanCard）。
type TodayPlanCard struct {
	PlanID          string               `json:"planId"`
	Name            string               `json:"name"`
	Kind            string               `json:"kind"`
	Modes           []string             `json:"modes"`
	Source          string               `json:"source"` // self | assigned
	CreatorName     string               `json:"creatorName"`
	TotalWords      int                  `json:"totalWords"`
	LearnedWords    int                  `json:"learnedWords"`
	NewPerDay       int                  `json:"newPerDay"`
	NewDoneToday    int                  `json:"newDoneToday"`
	NewLeft         int                  `json:"newLeft"`
	NewAvailable    int                  `json:"newAvailable"`
	ReviewPerDay    int                  `json:"reviewPerDay"`
	ReviewDoneToday int                  `json:"reviewDoneToday"`
	DueCount        int                  `json:"dueCount"`
	ReviewLeft      int                  `json:"reviewLeft"`
	TestSize        int                  `json:"testSize"`
	TestResult      *TodayTestResult     `json:"testResult"`
	ActiveSessions  []TodayActiveSession `json:"activeSessions"`
	// DoneToday 今天这份计划要做的都做完了。
	DoneToday bool `json:"doneToday"`
}

// TodayTotals 今日队列的合计（旧 TodaySummary.totals）。
type TodayTotals struct {
	NewLeft         int `json:"newLeft"`
	ReviewLeft      int `json:"reviewLeft"`
	NewDoneToday    int `json:"newDoneToday"`
	ReviewDoneToday int `json:"reviewDoneToday"`
	PendingTests    int `json:"pendingTests"`
}

// TodaySummary 一个学生的今日队列（旧 TodaySummary）。
type TodaySummary struct {
	Day    string          `json:"day"`
	Plans  []TodayPlanCard `json:"plans"`
	Totals TodayTotals     `json:"totals"`
}

// PlanWindow 计划日期窗口判断需要的字段。
type PlanWindow struct {
	StartDate *string
	EndDate   *string
}

// IsPlanInWindow 计划在今天是否生效（日期窗口）。
func IsPlanInWindow(plan PlanWindow, today string) bool {
	if plan.StartDate != nil && today < *plan.StartDate {
		return false
	}
	if plan.EndDate != nil && today > *plan.EndDate {
		return false
	}
	return true
}

// claimPlan claimDueWords 需要的计划最小字段集。
type claimPlan struct {
	ID           string
	Kind         string
	CreatedAt    time.Time
	ScopeWordIDs []string
}

type claimMemory struct {
	Due time.Time
}

// claimDueWordsImpl 到期词认领：同一个到期词只归属一个「每日学习」计划（按计划创建时间先到先得），
// 检测计划不认领复习词。返回 planId → 到期词（最早到期在前）。
func claimDueWordsImpl(plans []claimPlan, memories map[string]claimMemory, dayEnd time.Time) map[string][]string {
	claimed := map[string]bool{}
	result := map[string][]string{}
	ordered := append([]claimPlan(nil), plans...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].CreatedAt.Before(ordered[j].CreatedAt) })
	for _, p := range ordered {
		if p.Kind != "daily" {
			result[p.ID] = []string{}
			continue
		}
		type dueWord struct {
			w   string
			due int64
		}
		mine := []dueWord{}
		for _, w := range p.ScopeWordIDs {
			m, ok := memories[w]
			if !ok || !m.Due.Before(dayEnd) || claimed[w] {
				continue
			}
			claimed[w] = true
			mine = append(mine, dueWord{w: w, due: m.Due.UnixMilli()})
		}
		sort.SliceStable(mine, func(i, j int) bool { return mine[i].due < mine[j].due })
		ws := make([]string, len(mine))
		for i, x := range mine {
			ws[i] = x.w
		}
		result[p.ID] = ws
	}
	return result
}

// ClaimDueWords 对外的到期词认领入口（旧 claimDueWords）。
func ClaimDueWords(plans []TodayPlanInput, memories []MemoryInput, dayEnd time.Time) map[string][]string {
	cp := make([]claimPlan, len(plans))
	for i, p := range plans {
		cp[i] = claimPlan{ID: p.ID, Kind: p.Kind, CreatedAt: p.CreatedAt, ScopeWordIDs: p.ScopeWordIDs}
	}
	cm := map[string]claimMemory{}
	for _, m := range memories {
		cm[m.WordID] = claimMemory{Due: m.Due}
	}
	return claimDueWordsImpl(cp, cm, dayEnd)
}

func sumInt[T any](xs []T, f func(T) int) int {
	s := 0
	for _, x := range xs {
		s += f(x)
	}
	return s
}

// BuildToday 计算一个学生的今日队列（旧 buildToday）。
func BuildToday(in TodayInput) TodaySummary {
	memoryByWord := map[string]MemoryInput{}
	for _, m := range in.Memories {
		memoryByWord[m.WordID] = m
	}
	plans := append([]TodayPlanInput(nil), in.Plans...)
	sort.SliceStable(plans, func(i, j int) bool { return plans[i].CreatedAt.Before(plans[j].CreatedAt) })
	dueByPlan := ClaimDueWords(plans, in.Memories, in.DayEnd)

	cards := make([]TodayPlanCard, len(plans))
	for i, p := range plans {
		scope := p.ScopeWordIDs
		learned := 0
		for _, wid := range scope {
			if _, ok := memoryByWord[wid]; ok {
				learned++
			}
		}
		dueCount := len(dueByPlan[p.ID])
		newDoneToday := 0
		for _, m := range in.Memories {
			if m.IntroducedPlanID != nil && *m.IntroducedPlanID == p.ID && m.IntroducedDay == in.Today {
				newDoneToday++
			}
		}
		newAvailable := len(scope) - learned
		newLeft := 0
		if p.Kind == "daily" {
			newLeft = min2(max2(p.NewPerDay-newDoneToday, 0), newAvailable)
			if newLeft < 0 {
				newLeft = 0
			}
		}
		reviewDoneToday := in.ReviewedToday[p.ID]
		reviewLeft := 0
		if p.Kind == "daily" {
			reviewLeft = max2(min2(dueCount, p.ReviewPerDay-reviewDoneToday), 0)
		}

		results := []TestResultInput{}
		for _, r := range in.TestResults {
			if r.PlanID == p.ID {
				results = append(results, r)
			}
		}
		sort.SliceStable(results, func(a, b int) bool { return results[a].CompletedAt.Before(results[b].CompletedAt) })
		var testResult *TodayTestResult
		if len(results) > 0 {
			r := results[0]
			testResult = &TodayTestResult{SessionID: r.SessionID, Correct: r.Correct, Total: r.Total, CompletedAt: FormatISOMs(r.CompletedAt)}
		}

		activeSessions := []TodayActiveSession{}
		for _, s := range in.ActiveSessions {
			if s.PlanID != nil && *s.PlanID == p.ID {
				activeSessions = append(activeSessions, TodayActiveSession{ID: s.ID, Kind: s.Kind, Total: s.Total})
			}
		}
		var doneToday bool
		if p.Kind == "daily" {
			doneToday = len(activeSessions) == 0 && newLeft == 0 && reviewLeft == 0
		} else {
			doneToday = len(activeSessions) == 0 && testResult != nil
		}

		source := "assigned"
		if p.CreatorID == in.UserID {
			source = "self"
		}

		cards[i] = TodayPlanCard{
			PlanID: p.ID, Name: p.Name, Kind: p.Kind, Modes: p.Modes, Source: source, CreatorName: p.CreatorName,
			TotalWords: len(scope), LearnedWords: learned, NewPerDay: p.NewPerDay, NewDoneToday: newDoneToday,
			NewLeft: newLeft, NewAvailable: newAvailable, ReviewPerDay: p.ReviewPerDay, ReviewDoneToday: reviewDoneToday,
			DueCount: dueCount, ReviewLeft: reviewLeft, TestSize: p.TestSize, TestResult: testResult,
			ActiveSessions: activeSessions, DoneToday: doneToday,
		}
	}

	totals := TodayTotals{
		NewLeft:         sumInt(cards, func(c TodayPlanCard) int { return c.NewLeft }),
		ReviewLeft:      sumInt(cards, func(c TodayPlanCard) int { return c.ReviewLeft }),
		NewDoneToday:    sumInt(cards, func(c TodayPlanCard) int { return c.NewDoneToday }),
		ReviewDoneToday: sumInt(cards, func(c TodayPlanCard) int { return c.ReviewDoneToday }),
	}
	for _, c := range cards {
		if c.Kind == "test" && c.TestResult == nil {
			totals.PendingTests++
		}
	}

	return TodaySummary{Day: in.Today, Plans: cards, Totals: totals}
}

func min2(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func max2(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// FormatISOMs 与 store.FormatTime 相同格式（today.go 不依赖 store，本地重复一份，两处输出必须一致）。
func FormatISOMs(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

// ---------------------------------------------------------------------------
// 选词
// ---------------------------------------------------------------------------

// Rng 随机数源（[0,1)），用于可复现测试。
type Rng func() float64

// DefaultRng 默认随机数源。
func DefaultRng() float64 { return rand.Float64() }

// shuffleStrings Fisher-Yates 洗牌，不修改入参。
func shuffleStrings(xs []string, rng Rng) []string {
	if rng == nil {
		rng = DefaultRng
	}
	out := append([]string(nil), xs...)
	for i := len(out) - 1; i > 0; i-- {
		j := int(rng() * float64(i+1))
		if j > i {
			j = i
		}
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// SelectNewWords 选新词：范围内尚未学过的词，按顺序或随机。
func SelectNewWords(scope []string, learned map[string]bool, count int, order string, rng Rng) []string {
	if count <= 0 {
		return []string{}
	}
	pool := make([]string, 0, len(scope))
	for _, w := range scope {
		if !learned[w] {
			pool = append(pool, w)
		}
	}
	if order == "random" {
		pool = shuffleStrings(pool, rng)
	}
	if len(pool) > count {
		pool = pool[:count]
	}
	return pool
}

// SelectDueWords 选复习词：范围内到期（due < dayEnd），最早到期优先。
func SelectDueWords(scope []string, memories map[string]time.Time, dayEnd time.Time, count int) []string {
	if count <= 0 {
		return []string{}
	}
	type item struct {
		w   string
		due time.Time
	}
	items := []item{}
	for _, w := range scope {
		due, ok := memories[w]
		if !ok || !due.Before(dayEnd) {
			continue
		}
		items = append(items, item{w: w, due: due})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].due.Before(items[j].due) })
	if len(items) > count {
		items = items[:count]
	}
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.w
	}
	return out
}

// SelectTestWords 选检测词：all = 范围内随机；learned = 仅已学词随机。
func SelectTestWords(scope []string, learned map[string]bool, testScope string, size int, rng Rng) []string {
	pool := []string{}
	if testScope == "learned" {
		for _, w := range scope {
			if learned[w] {
				pool = append(pool, w)
			}
		}
	} else {
		pool = append(pool, scope...)
	}
	pool = shuffleStrings(pool, rng)
	if size < 0 {
		size = 0
	}
	if len(pool) > size {
		pool = pool[:size]
	}
	return pool
}

// SelectDrillWords 错词强化选词：按近期首次作答错误次数降序，其次遗忘次数降序。
func SelectDrillWords(wrongCounts map[string]int, lapses map[string]int, count int) []string {
	seen := map[string]bool{}
	ids := []string{}
	for w := range wrongCounts {
		if !seen[w] {
			seen[w] = true
			ids = append(ids, w)
		}
	}
	for w, n := range lapses {
		if n > 0 && !seen[w] {
			seen[w] = true
			ids = append(ids, w)
		}
	}
	// 入参是 map，遍历顺序本身不确定；旧版按 Map 插入顺序稳定排序，Go 没有等价信息可还原，
	// 用词 id 升序兜底作为最终比较项，让同错次同遗忘次的词也有确定、可复现的顺序（不依赖 map 遍历随机性）。
	sort.SliceStable(ids, func(i, j int) bool {
		wi, wj := wrongCounts[ids[i]], wrongCounts[ids[j]]
		if wi != wj {
			return wi > wj
		}
		li, lj := lapses[ids[i]], lapses[ids[j]]
		if li != lj {
			return li > lj
		}
		return ids[i] < ids[j]
	})
	if len(ids) > count {
		ids = ids[:count]
	}
	return ids
}
