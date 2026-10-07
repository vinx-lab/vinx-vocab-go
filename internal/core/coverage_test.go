package core

import (
	"reflect"
	"testing"
	"time"
)

func evAt(d, h int) time.Time { return time.Date(2026, 10, d, h, 0, 0, 0, time.UTC) }

func evDay(d int) string { return time.Date(2026, 10, d, 0, 0, 0, 0, time.UTC).Format("2006-01-02") }

func TestBuildEvidence(t *testing.T) {
	sh := "sheet1"
	sess := func(id, kind string, d, h int) EvidenceSession {
		return EvidenceSession{ID: id, Kind: kind, Day: evDay(d), At: evAt(d, h)}
	}
	sheet := func(id string, d, h int, self bool) EvidenceSession {
		s := sess(id, "sheet", d, h)
		s.SheetID = &sh
		s.SelfGraded = self
		return s
	}
	ans := func(session, word, mode, phase string, attempt int, correct bool) EvidenceAnswer {
		return EvidenceAnswer{SessionID: session, WordID: word, Mode: mode, Phase: phase, Attempt: attempt, Correct: correct}
	}
	type got struct {
		session string
		correct bool
	}
	cases := []struct {
		name     string
		sessions []EvidenceSession
		answers  []EvidenceAnswer
		want     map[string][]got
	}{
		{"检测计划连考两组：都是证据（不再有重测）",
			[]EvidenceSession{sess("t1", "test", 1, 8), sess("t2", "test", 2, 8)},
			[]EvidenceAnswer{ans("t1", "a", "recognition", "test", 1, false), ans("t2", "a", "recognition", "test", 1, true), ans("t2", "b", "recognition", "test", 1, true)},
			map[string][]got{"a": {{"t1", false}, {"t2", true}}, "b": {{"t2", true}}}},
		{"同一学习日只取完成最早的一组",
			[]EvidenceSession{sess("t2", "test", 1, 10), sess("t1", "test", 1, 8)},
			[]EvidenceAnswer{ans("t2", "a", "recognition", "test", 1, true), ans("t1", "a", "recognition", "test", 1, false)},
			map[string][]got{"a": {{"t1", false}}}},
		{"同一张单词单再测不算，换一天也不算",
			[]EvidenceSession{sheet("w1", 1, 8, false), sheet("w2", 3, 8, false)},
			[]EvidenceAnswer{ans("w1", "a", "recognition", "test", 1, false), ans("w2", "a", "recognition", "test", 1, true)},
			map[string][]got{"a": {{"w1", false}}}},
		{"巩固阶段、第二次作答不算",
			[]EvidenceSession{sess("r1", "review", 1, 8)},
			[]EvidenceAnswer{ans("r1", "a", "recognition", "practice", 1, false), ans("r1", "a", "recognition", "consolidate", 1, true), ans("r1", "a", "recognition", "practice", 2, true)},
			map[string][]got{"a": {{"r1", false}}}},
		{"多个题型有一题错即错",
			[]EvidenceSession{sess("l1", "learn", 1, 8)},
			[]EvidenceAnswer{ans("l1", "a", "recognition", "practice", 1, true), ans("l1", "a", "spelling", "practice", 1, false)},
			map[string][]got{"a": {{"l1", false}}}},
		{"检测类组的练习阶段、练习类组的检测阶段都不算",
			[]EvidenceSession{sess("t1", "test", 1, 8), sess("d1", "drill", 2, 8)},
			[]EvidenceAnswer{ans("t1", "a", "recognition", "practice", 1, true), ans("d1", "a", "recognition", "test", 1, true)},
			map[string][]got{}},
		{"不在已完成组里的作答忽略；错词强化是证据",
			[]EvidenceSession{sess("d1", "drill", 1, 8)},
			[]EvidenceAnswer{ans("x", "a", "recognition", "test", 1, true), ans("d1", "b", "spelling", "practice", 1, true)},
			map[string][]got{"b": {{"d1", true}}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ev := BuildEvidence(c.sessions, c.answers)
			out := map[string][]got{}
			for w, list := range ev {
				for _, e := range list {
					out[w] = append(out[w], got{e.SessionID, e.Correct})
				}
			}
			if !reflect.DeepEqual(out, c.want) {
				t.Fatalf("got %v want %v", out, c.want)
			}
		})
	}
	// 评分要用的首次作答随证据带出
	ev := BuildEvidence([]EvidenceSession{sess("l1", "learn", 1, 8)}, []EvidenceAnswer{
		ans("l1", "a", "recognition", "practice", 1, true), {SessionID: "l1", WordID: "a", Mode: "spelling", Phase: "practice", Attempt: 1, Correct: true, HintUsed: true},
	})
	if want := []FirstAttempt{{Mode: "recognition", Correct: true}, {Mode: "spelling", Correct: true, HintUsed: true}}; !reflect.DeepEqual(ev["a"][0].Attempts, want) {
		t.Fatalf("attempts = %+v", ev["a"][0].Attempts)
	}
}

func TestWordStage(t *testing.T) {
	now := evAt(10, 12)
	dayEnd := evAt(11, 0)
	ptr := func(t time.Time) *time.Time { return &t }
	e := func(d int, correct bool) Evidence {
		return Evidence{SessionID: "s", Day: evDay(d), At: evAt(d, 8), Correct: correct}
	}
	mem := func(st float64, due time.Time, last *time.Time) *StageMemory {
		return &StageMemory{Stability: st, Due: due, LastReview: last}
	}
	future := evAt(20, 0)
	cases := []struct {
		name string
		ev   []Evidence
		mem  *StageMemory
		want WordStageInfo
	}{
		{"没有证据没有记忆：未接触", nil, nil, WordStageInfo{Stage: StageUntested}},
		{"答错、没有记忆：没记住", []Evidence{e(1, false)}, nil, WordStageInfo{Stage: StageMissed}},
		{"答对、没有记忆：刚记住", []Evidence{e(1, true)}, nil, WordStageInfo{Stage: StageFresh}},
		{"最近一次答错，记忆再稳也是没记住", []Evidence{e(1, true), e(9, false)}, mem(40, future, ptr(evAt(9, 8))), WordStageInfo{Stage: StageMissed}},
		{"错后答对，按稳定度：巩固中", []Evidence{e(1, false), e(5, true)}, mem(10, future, ptr(evAt(5, 8))), WordStageInfo{Stage: StageConsolidating}},
		{"已掌握", []Evidence{e(1, true)}, mem(21, future, ptr(evAt(1, 8))), WordStageInfo{Stage: StageMastered}},
		{"稳定度不到 7 天：刚记住", []Evidence{e(9, true)}, mem(6.9, future, ptr(evAt(9, 8))), WordStageInfo{Stage: StageFresh}},
		{"没有证据但有记忆（导入的旧数据）：按稳定度", nil, mem(30, future, ptr(evAt(1, 8))), WordStageInfo{Stage: StageMastered}},
		{"今天到期：待复查，不降档", []Evidence{e(1, true)}, mem(30, evAt(10, 20), ptr(evAt(1, 8))), WordStageInfo{Stage: StageMastered, Due: true}},
		{"超期不久：只是待复查", []Evidence{e(1, true)}, mem(2, evAt(3, 0), ptr(evAt(1, 8))), WordStageInfo{Stage: StageFresh, Due: true}},
		{"超期很久：可能忘了", []Evidence{e(1, true)}, mem(2, evAt(3, 0), ptr(time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC))), WordStageInfo{Stage: StageFresh, Due: true, Forgetting: true}},
		{"自批标记取最近一条证据", []Evidence{e(1, true), {SessionID: "w", At: evAt(2, 8), Correct: true, SelfGraded: true}}, nil, WordStageInfo{Stage: StageFresh, SelfGraded: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := WordStage(c.ev, c.mem, now, dayEnd); got != c.want {
				t.Fatalf("got %+v want %+v", got, c.want)
			}
		})
	}
}

func TestRetrievability(t *testing.T) {
	last := evAt(1, 8)
	m := StageMemory{Stability: 10, LastReview: &last}
	if r := Retrievability(m, evAt(1, 20)); r != 1 {
		t.Fatalf("当天 = %v", r)
	}
	// 隔了稳定度那么多天，回忆概率约 90%
	if r := Retrievability(m, evAt(11, 8)); r < 0.89 || r > 0.91 {
		t.Fatalf("隔 10 天 = %v", r)
	}
	if r := Retrievability(StageMemory{Stability: 10}, evAt(11, 8)); r != 0 {
		t.Fatalf("没有复习时间 = %v", r)
	}
}

func TestStageMapping(t *testing.T) {
	want := map[string]CoverageStatus{StageUntested: CoverageUntested, "": CoverageUntested, StageMissed: CoverageLearning,
		StageFresh: CoverageKnown, StageConsolidating: CoverageKnown, StageMastered: CoverageKnown}
	for st, w := range want {
		if got := CoverageStatusOf(st); got != w {
			t.Errorf("%q → %s want %s", st, got, w)
		}
	}
	if StageLevel(StageFresh) != "learning" || StageLevel(StageMissed) != "missed" || StageLevel(StageMastered) != "mastered" {
		t.Fatal("StageLevel")
	}
}

func TestMemoryActionFor(t *testing.T) {
	cases := []struct {
		kind   string
		hasMem bool
		rating Rating
		want   MemoryAction
	}{
		{"learn", false, RatingHard, MemoryCreateLearn},
		{"learn", false, RatingAgain, MemoryCreateLearn},
		{"learn", true, RatingHard, MemoryNone},
		{"review", true, RatingAgain, MemoryUpdate},
		{"drill", true, RatingGood, MemoryUpdate},
		{"test", true, RatingAgain, MemoryUpdate},
		{"test", false, RatingGood, MemoryCreate},
		{"sheet", false, RatingGood, MemoryCreate},
		{"drill", false, RatingGood, MemoryCreate},
		{"test", false, RatingAgain, MemoryNone},
		{"drill", false, RatingHard, MemoryNone},
		{"review", true, 0, MemoryNone},
	}
	for _, c := range cases {
		if got := MemoryActionFor(c.kind, c.hasMem, c.rating); got != c.want {
			t.Errorf("%s mem=%v rating=%v: got %v want %v", c.kind, c.hasMem, c.rating, got, c.want)
		}
	}
}

func TestCountSelfGraded(t *testing.T) {
	books := []CoverageBook{
		{BookID: "b1", WordIDs: []string{"a", "b", "c", "a"}},
		{BookID: "b2", WordIDs: []string{"a", "d", "e"}},
	}
	st := func(stage string, self bool) WordStageInfo { return WordStageInfo{Stage: stage, SelfGraded: self} }
	cases := []struct {
		name   string
		stages map[string]WordStageInfo
		want   int
	}{
		{"没有自批", map[string]WordStageInfo{"a": st(StageFresh, false)}, 0},
		{"跨书重复只算一次", map[string]WordStageInfo{"a": st(StageFresh, true)}, 1},
		{"各档都算", map[string]WordStageInfo{"a": st(StageMastered, true), "b": st(StageMissed, true)}, 2},
		{"未接触的和不在目标里的不算", map[string]WordStageInfo{"d": st(StageUntested, true), "x": st(StageFresh, true), "c": st(StageFresh, true)}, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := CountSelfGraded(c.stages, books); got != c.want {
				t.Fatalf("got %d want %d", got, c.want)
			}
		})
	}
}

func TestSummarizeCoverage(t *testing.T) {
	stages := map[string]WordStageInfo{
		"a": {Stage: StageMastered, Due: true}, "b": {Stage: StageMissed}, "c": {Stage: StageFresh}, "x": {Stage: StageMissed},
	}
	// b1 书内重复只算一次；b2 的 a 与 b1 重叠，合计只算一次
	books := []CoverageBook{
		{BookID: "b1", Name: "七上", WordIDs: []string{"a", "b", "d", "a"}},
		{BookID: "b2", Name: "中考", WordIDs: []string{"a", "c", "e"}},
		{BookID: "b3", Name: "空书", WordIDs: nil},
	}
	total, per := SummarizeCoverage(stages, books)
	wantTotal := CoverageCounts{Target: 5, Tested: 3, Known: 2, Learning: 1, Untested: 2, Fresh: 1, Mastered: 1, Due: 1}
	if total != wantTotal {
		t.Fatalf("total = %+v", total)
	}
	want := []CoverageBookCounts{
		{BookID: "b1", Name: "七上", CoverageCounts: CoverageCounts{Target: 3, Tested: 2, Known: 1, Learning: 1, Untested: 1, Mastered: 1, Due: 1}},
		{BookID: "b2", Name: "中考", CoverageCounts: CoverageCounts{Target: 3, Tested: 2, Known: 2, Untested: 1, Fresh: 1, Mastered: 1, Due: 1}},
		{BookID: "b3", Name: "空书", CoverageCounts: CoverageCounts{}},
	}
	if !reflect.DeepEqual(per, want) {
		t.Fatalf("per = %+v", per)
	}
	if total, per := SummarizeCoverage(nil, nil); total != (CoverageCounts{}) || per == nil || len(per) != 0 {
		t.Fatalf("空 = %+v %v", total, per)
	}
}

func TestCoverageWordOrder(t *testing.T) {
	books := []CoverageBook{
		{BookID: "b1", WordIDs: []string{"a", "b", "a"}},
		{BookID: "b2", WordIDs: []string{"c", "a", "d"}},
	}
	if got := CoverageWordOrder(books); !reflect.DeepEqual(got, []string{"a", "b", "c", "d"}) {
		t.Fatalf("order = %v", got)
	}
}
