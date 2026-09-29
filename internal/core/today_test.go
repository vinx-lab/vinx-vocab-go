package core

import (
	"testing"
	"time"
)

// 逐条翻译旧 apps/api/tests/lib/today.test.ts。

var (
	todayDay = "2026-09-17"
	dayEnd   = mustTime("2026-09-17T16:00:00Z")
	past     = mustTime("2026-09-16T00:00:00Z")
	future   = mustTime("2026-09-20T00:00:00Z")
)

// seededRng 旧 apps/api/src/lib/questions.ts:seededRng 的等价实现（mulberry32），只在本文件的测试里用，
// 让"随机"用例可复现（对应旧 tests/lib/today.test.ts 里的 seededRng(5)）。
// 不导出、不放进 core 的公共 API：questions.ts 的完整移植（shuffle/buildCloze 等）属于 A4，
// 避免抢占那个文件名或产生合并冲突。
func seededRng(seed uint32) func() float64 {
	a := seed
	return func() float64 {
		a += 0x6d2b79f5
		t := a
		t = (t ^ (t >> 15)) * (t | 1)
		t ^= t + (t^(t>>7))*(t|61)
		return float64(t^(t>>14)) / 4294967296
	}
}

func mustTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func strp(s string) *string { return &s }

func testPlan(id string, over func(*TodayPlanInput)) TodayPlanInput {
	p := TodayPlanInput{
		ID: id, Name: id, Kind: "daily", NewPerDay: 3, ReviewPerDay: 5, Modes: []string{"recognition"}, TestSize: 10,
		CreatorID: "teacher", CreatorName: "老师", CreatedAt: mustTime("2026-09-01T00:00:00Z"), ScopeWordIDs: nil,
	}
	if over != nil {
		over(&p)
	}
	return p
}

func testInput(over func(*TodayInput)) TodayInput {
	in := TodayInput{
		UserID: "u1", Today: todayDay, DayEnd: dayEnd, Plans: nil, Memories: nil,
		ReviewedToday: map[string]int{}, ActiveSessions: nil, TestResults: nil,
	}
	if over != nil {
		over(&in)
	}
	return in
}

func TestBuildTodayNewQuota(t *testing.T) {
	// 新词额度 = min(每日新词 − 今日已学, 可学)，缺席不累计
	r := BuildToday(testInput(func(in *TodayInput) {
		in.Plans = []TodayPlanInput{testPlan("p", func(p *TodayPlanInput) { p.ScopeWordIDs = []string{"a", "b", "c", "d", "e"} })}
		in.Memories = []MemoryInput{
			{WordID: "a", Due: future, IntroducedPlanID: strp("p"), IntroducedDay: todayDay},
			{WordID: "b", Due: future, IntroducedPlanID: strp("p"), IntroducedDay: "2026-09-10"},
		}
	}))
	c := r.Plans[0]
	if c.LearnedWords != 2 || c.NewDoneToday != 1 || c.NewLeft != 2 || c.NewAvailable != 3 || c.Source != "assigned" {
		t.Fatalf("got %+v", c)
	}
}

func TestBuildTodayDueDedupeAcrossPlans(t *testing.T) {
	// 到期词跨计划去重：归属先创建的计划；复习剩余受每日上限约束
	r := BuildToday(testInput(func(in *TodayInput) {
		in.Plans = []TodayPlanInput{
			testPlan("late", func(p *TodayPlanInput) {
				p.CreatedAt = mustTime("2026-09-10T00:00:00Z")
				p.ScopeWordIDs = []string{"x", "y"}
				p.CreatorID = "u1"
			}),
			testPlan("early", func(p *TodayPlanInput) {
				p.CreatedAt = mustTime("2026-09-01T00:00:00Z")
				p.ScopeWordIDs = []string{"x"}
				p.ReviewPerDay = 5
			}),
		}
		in.Memories = []MemoryInput{
			{WordID: "x", Due: past, IntroducedPlanID: strp("early"), IntroducedDay: "2026-09-10"},
			{WordID: "y", Due: past, IntroducedPlanID: strp("late"), IntroducedDay: "2026-09-10"},
		}
		in.ReviewedToday = map[string]int{"late": 0}
	}))
	var early, late TodayPlanCard
	for _, p := range r.Plans {
		if p.PlanID == "early" {
			early = p
		}
		if p.PlanID == "late" {
			late = p
		}
	}
	if r.Plans[0].PlanID != "early" {
		t.Fatalf("排序错误: %+v", r.Plans)
	}
	if early.DueCount != 1 || late.DueCount != 1 {
		t.Fatalf("dueCount: early=%d late=%d", early.DueCount, late.DueCount)
	}
	if late.Source != "self" {
		t.Fatalf("late.Source = %s", late.Source)
	}
	if r.Totals.ReviewLeft != 2 {
		t.Fatalf("totals.reviewLeft = %d", r.Totals.ReviewLeft)
	}
}

func TestBuildTodayReviewCapAndDoneToday(t *testing.T) {
	// 复习已达上限 → reviewLeft 0；无进行中组且无剩余 → doneToday
	r := BuildToday(testInput(func(in *TodayInput) {
		in.Plans = []TodayPlanInput{testPlan("p", func(p *TodayPlanInput) {
			p.ReviewPerDay = 2
			p.NewPerDay = 0
			p.ScopeWordIDs = []string{"a", "b", "c"}
		})}
		mems := []MemoryInput{}
		for _, w := range []string{"a", "b", "c"} {
			mems = append(mems, MemoryInput{WordID: w, Due: past, IntroducedPlanID: strp("p"), IntroducedDay: "2026-09-01"})
		}
		in.Memories = mems
		in.ReviewedToday = map[string]int{"p": 2}
	}))
	if r.Plans[0].ReviewLeft != 0 || !r.Plans[0].DoneToday {
		t.Fatalf("got %+v", r.Plans[0])
	}
}

func TestBuildTodayActiveSessionBlocksDoneToday(t *testing.T) {
	// 进行中的组使 doneToday 为 false
	r := BuildToday(testInput(func(in *TodayInput) {
		in.Plans = []TodayPlanInput{testPlan("p", func(p *TodayPlanInput) { p.NewPerDay = 0; p.ScopeWordIDs = []string{} })}
		in.ActiveSessions = []ActiveSessionInput{{ID: "s", PlanID: strp("p"), Kind: "learn", Total: 3}}
	}))
	if len(r.Plans[0].ActiveSessions) != 1 || r.Plans[0].DoneToday {
		t.Fatalf("got %+v", r.Plans[0])
	}
}

func TestBuildTodayTestPlan(t *testing.T) {
	// 检测计划：未完成计入 pendingTests，完成后显示最近成绩
	pending := BuildToday(testInput(func(in *TodayInput) {
		in.Plans = []TodayPlanInput{testPlan("t", func(p *TodayPlanInput) { p.Kind = "test"; p.ScopeWordIDs = []string{"a"} })}
	}))
	if pending.Totals.PendingTests != 1 || pending.Plans[0].NewLeft != 0 {
		t.Fatalf("got %+v", pending)
	}
	done := BuildToday(testInput(func(in *TodayInput) {
		in.Plans = []TodayPlanInput{testPlan("t", func(p *TodayPlanInput) { p.Kind = "test"; p.ScopeWordIDs = []string{"a"} })}
		in.TestResults = []TestResultInput{
			{PlanID: "t", SessionID: "s1", Correct: 1, Total: 2, CompletedAt: mustTime("2026-09-10T00:00:00Z")},
			{PlanID: "t", SessionID: "s2", Correct: 2, Total: 2, CompletedAt: mustTime("2026-09-12T00:00:00Z")},
		}
	}))
	// 首次交卷为正式成绩
	if done.Plans[0].TestResult == nil || done.Plans[0].TestResult.SessionID != "s1" || !done.Plans[0].DoneToday {
		t.Fatalf("got %+v", done.Plans[0])
	}
}

func TestClaimDueWordsTestPlanDoesNotClaim(t *testing.T) {
	// 检测计划不认领到期词，不会吞掉每日计划的复习（审查 #2）
	r := BuildToday(testInput(func(in *TodayInput) {
		in.Plans = []TodayPlanInput{
			testPlan("test", func(p *TodayPlanInput) {
				p.Kind = "test"
				p.CreatedAt = mustTime("2026-09-01T00:00:00Z")
				p.ScopeWordIDs = []string{"a", "b"}
			}),
			testPlan("daily", func(p *TodayPlanInput) {
				p.CreatedAt = mustTime("2026-09-05T00:00:00Z")
				p.ScopeWordIDs = []string{"a", "b"}
				p.NewPerDay = 0
			}),
		}
		mems := []MemoryInput{}
		for _, w := range []string{"a", "b"} {
			mems = append(mems, MemoryInput{WordID: w, Due: past, IntroducedPlanID: strp("daily"), IntroducedDay: "2026-09-10"})
		}
		in.Memories = mems
	}))
	var daily, test TodayPlanCard
	for _, p := range r.Plans {
		if p.PlanID == "daily" {
			daily = p
		}
		if p.PlanID == "test" {
			test = p
		}
	}
	if daily.DueCount != 2 || daily.ReviewLeft != 2 || daily.DoneToday {
		t.Fatalf("daily = %+v", daily)
	}
	if test.DueCount != 0 {
		t.Fatalf("test.DueCount = %d", test.DueCount)
	}
}

func TestClaimDueWordsOrderAndDedupe(t *testing.T) {
	// claimDueWords 同词只归一个计划，按到期先后排序
	memories := []MemoryInput{
		{WordID: "x", Due: mustTime("2026-09-15T00:00:00Z")},
		{WordID: "y", Due: mustTime("2026-09-10T00:00:00Z")},
		{WordID: "z", Due: future},
	}
	claim := ClaimDueWords([]TodayPlanInput{
		testPlan("p2", func(p *TodayPlanInput) {
			p.CreatedAt = mustTime("2026-09-02T00:00:00Z")
			p.ScopeWordIDs = []string{"x", "y"}
		}),
		testPlan("p1", func(p *TodayPlanInput) {
			p.CreatedAt = mustTime("2026-09-01T00:00:00Z")
			p.ScopeWordIDs = []string{"x", "z"}
		}),
	}, memories, dayEnd)
	if len(claim["p1"]) != 1 || claim["p1"][0] != "x" {
		t.Fatalf("p1 = %v", claim["p1"])
	}
	if len(claim["p2"]) != 1 || claim["p2"][0] != "y" {
		t.Fatalf("p2 = %v", claim["p2"])
	}
}

func TestIsPlanInWindow(t *testing.T) {
	cases := []struct {
		start, end *string
		want       bool
	}{
		{nil, nil, true},
		{strp("2026-09-18"), nil, false},
		{nil, strp("2026-09-16"), false},
		{strp(todayDay), strp(todayDay), true},
	}
	for _, c := range cases {
		got := IsPlanInWindow(PlanWindow{StartDate: c.start, EndDate: c.end}, todayDay)
		if got != c.want {
			t.Fatalf("IsPlanInWindow(%v,%v) = %v, want %v", c.start, c.end, got, c.want)
		}
	}
}

func strSliceEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSelectNewWords(t *testing.T) {
	learned := map[string]bool{"b": true}
	got := SelectNewWords([]string{"a", "b", "c", "d"}, learned, 2, "sequential", nil)
	if !strSliceEq(got, []string{"a", "c"}) {
		t.Fatalf("got %v", got)
	}
	got2 := SelectNewWords([]string{"a"}, map[string]bool{}, 0, "sequential", nil)
	if len(got2) != 0 {
		t.Fatalf("got2 %v", got2)
	}
	got3 := SelectNewWords([]string{"a", "b", "c"}, map[string]bool{}, 3, "random", seededRng(5))
	set := map[string]bool{}
	for _, w := range got3 {
		set[w] = true
	}
	if len(got3) != 3 || !set["a"] || !set["b"] || !set["c"] {
		t.Fatalf("got3 %v", got3)
	}
}

func TestSelectDueWords(t *testing.T) {
	memories := map[string]time.Time{
		"a": mustTime("2026-09-15T00:00:00Z"),
		"b": mustTime("2026-09-10T00:00:00Z"),
		"c": future,
	}
	got := SelectDueWords([]string{"a", "b", "c", "d"}, memories, dayEnd, 10)
	if !strSliceEq(got, []string{"b", "a"}) {
		t.Fatalf("got %v", got)
	}
	got2 := SelectDueWords([]string{"a", "b"}, memories, dayEnd, 1)
	if !strSliceEq(got2, []string{"b"}) {
		t.Fatalf("got2 %v", got2)
	}
}

func TestSelectTestWords(t *testing.T) {
	got := SelectTestWords([]string{"a", "b", "c"}, map[string]bool{"c": true}, "learned", 5, nil)
	if !strSliceEq(got, []string{"c"}) {
		t.Fatalf("got %v", got)
	}
	got2 := SelectTestWords([]string{"a", "b", "c"}, map[string]bool{}, "all", 2, nil)
	if len(got2) != 2 {
		t.Fatalf("got2 %v", got2)
	}
}

func TestSelectDrillWords(t *testing.T) {
	wrong := map[string]int{"a": 1, "b": 3}
	lapses := map[string]int{"c": 2, "a": 5, "d": 0}
	got := SelectDrillWords(wrong, lapses, 10)
	if !strSliceEq(got, []string{"b", "a", "c"}) {
		t.Fatalf("got %v", got)
	}
}
