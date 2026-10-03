package core

import (
	"reflect"
	"testing"
	"time"
)

func TestFormalTestSessions(t *testing.T) {
	p1, p2, sh := "p1", "p2", "sh1"
	at := func(h int) time.Time { return time.Date(2026, 10, 1, h, 0, 0, 0, time.UTC) }
	// b：同一计划重测；e：同一张单子重测；f、g：计划已删除，无法判断重测，各自算正式；
	// h：不同类型的 key 不相互影响；i：非检测类组不在结果里
	sessions := []TestSessionRef{
		{ID: "a", Kind: "test", GroupKey: &p1, CompletedAt: at(1)},
		{ID: "b", Kind: "test", GroupKey: &p1, CompletedAt: at(2)},
		{ID: "c", Kind: "test", GroupKey: &p2, CompletedAt: at(3)},
		{ID: "d", Kind: "sheet", GroupKey: &sh, CompletedAt: at(4)},
		{ID: "e", Kind: "sheet", GroupKey: &sh, CompletedAt: at(5)},
		{ID: "f", Kind: "test", GroupKey: nil, CompletedAt: at(6)},
		{ID: "g", Kind: "test", GroupKey: nil, CompletedAt: at(7)},
		{ID: "h", Kind: "sheet", GroupKey: &p1, CompletedAt: at(8)},
		{ID: "i", Kind: "review", GroupKey: nil, CompletedAt: at(9)},
	}
	got := FormalTestSessions(sessions)
	want := map[string]bool{"a": true, "c": true, "d": true, "f": true, "g": true, "h": true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("formal = %v", got)
	}
	// 输入顺序不按完成时间时，仍以先交卷的为正式
	rev := []TestSessionRef{sessions[1], sessions[0]}
	if got := FormalTestSessions(rev); !reflect.DeepEqual(got, map[string]bool{"a": true}) {
		t.Fatalf("乱序 = %v", got)
	}
}

func TestWordCoverage(t *testing.T) {
	at := func(d int) time.Time { return time.Date(2026, 10, d, 8, 0, 0, 0, time.UTC) }
	ptr := func(t time.Time) *time.Time { return &t }
	ans := func(session string, d int, correct bool) CoverageAnswer {
		return CoverageAnswer{SessionID: session, Kind: "test", Phase: "test", Attempt: 1, Correct: correct, At: at(d)}
	}
	cases := []struct {
		name    string
		answers []CoverageAnswer
		memory  *CoverageMemory
		want    CoverageStatus
	}{
		{"没有作答", nil, nil, CoverageUntested},
		{"没有作答但记忆已掌握也算未测", nil, &CoverageMemory{Stability: 30, LastReview: ptr(at(1))}, CoverageUntested},
		{"只答对", []CoverageAnswer{ans("s1", 1, true)}, nil, CoverageKnown},
		{"只答错", []CoverageAnswer{ans("s1", 1, false)}, nil, CoverageLearning},
		{"错后答对", []CoverageAnswer{ans("s1", 1, false), ans("s2", 2, true)}, nil, CoverageKnown},
		{"对后答错", []CoverageAnswer{ans("s1", 1, true), ans("s2", 2, false)}, nil, CoverageLearning},
		{"按时间而不是输入顺序取最近一次", []CoverageAnswer{ans("s2", 2, false), ans("s1", 1, true)}, nil, CoverageLearning},
		{"错后记忆达到已掌握", []CoverageAnswer{ans("s1", 1, false)}, &CoverageMemory{Stability: 21, LastReview: ptr(at(3))}, CoverageKnown},
		{"错后记忆巩固中仍要学", []CoverageAnswer{ans("s1", 1, false)}, &CoverageMemory{Stability: 20.9, LastReview: ptr(at(3))}, CoverageLearning},
		{"已掌握但复习早于答错仍要学", []CoverageAnswer{ans("s1", 2, false)}, &CoverageMemory{Stability: 40, LastReview: ptr(at(1))}, CoverageLearning},
		{"已掌握但没有复习时间仍要学", []CoverageAnswer{ans("s1", 2, false)}, &CoverageMemory{Stability: 40}, CoverageLearning},
		{"已掌握后又答错退回要学", []CoverageAnswer{ans("s1", 1, false), ans("s2", 5, false)}, &CoverageMemory{Stability: 30, LastReview: ptr(at(3))}, CoverageLearning},
		{"同一组多题型有一题错即错", []CoverageAnswer{
			{SessionID: "s1", Kind: "test", Phase: "test", Attempt: 1, Correct: true, At: at(1)},
			{SessionID: "s1", Kind: "test", Phase: "test", Attempt: 1, Correct: false, At: at(1)},
		}, nil, CoverageLearning},
		{"同一组内的重测（attempt>1）不计", []CoverageAnswer{
			ans("s1", 1, false),
			{SessionID: "s1", Kind: "test", Phase: "test", Attempt: 2, Correct: true, At: at(1)},
		}, nil, CoverageLearning},
		{"重测组（重新测同一计划）不计", []CoverageAnswer{
			ans("s1", 1, false),
			{SessionID: "s2", Kind: "test", Phase: "test", Attempt: 1, Correct: true, At: at(2), Retake: true},
		}, nil, CoverageLearning},
		{"只有重测组算未测", []CoverageAnswer{{SessionID: "s2", Kind: "sheet", Phase: "test", Attempt: 1, Correct: true, At: at(2), Retake: true}}, nil, CoverageUntested},
		{"练习不算正式测试", []CoverageAnswer{
			{SessionID: "l1", Kind: "learn", Phase: "practice", Attempt: 1, Correct: true, At: at(1)},
			{SessionID: "r1", Kind: "review", Phase: "practice", Attempt: 1, Correct: false, At: at(2)},
			{SessionID: "d1", Kind: "drill", Phase: "practice", Attempt: 1, Correct: false, At: at(3)},
		}, nil, CoverageUntested},
		{"单词单测试与检测同口径", []CoverageAnswer{{SessionID: "w1", Kind: "sheet", Phase: "test", Attempt: 1, Correct: true, At: at(1)}}, nil, CoverageKnown},
		{"检测里非 test 阶段不计", []CoverageAnswer{{SessionID: "s1", Kind: "test", Phase: "practice", Attempt: 1, Correct: true, At: at(1)}}, nil, CoverageUntested},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := WordCoverage(c.answers, c.memory); got != c.want {
				t.Fatalf("got %s want %s", got, c.want)
			}
		})
	}
}

func TestWordCoverageDetailSelfGraded(t *testing.T) {
	at := func(d int) time.Time { return time.Date(2026, 10, d, 8, 0, 0, 0, time.UTC) }
	ptr := func(t time.Time) *time.Time { return &t }
	ans := func(session string, d int, correct, self bool) CoverageAnswer {
		return CoverageAnswer{SessionID: session, Kind: "sheet", Phase: "test", Attempt: 1, Correct: correct, At: at(d), SelfGraded: self}
	}
	cases := []struct {
		name       string
		answers    []CoverageAnswer
		memory     *CoverageMemory
		wantStatus CoverageStatus
		wantSelf   bool
	}{
		{"未测不算自批", nil, nil, CoverageUntested, false},
		{"自批答对", []CoverageAnswer{ans("s1", 1, true, true)}, nil, CoverageKnown, true},
		{"自批答错", []CoverageAnswer{ans("s1", 1, false, true)}, nil, CoverageLearning, true},
		{"老师批改", []CoverageAnswer{ans("s1", 1, true, false)}, nil, CoverageKnown, false},
		{"自批后老师复核：取最近一次", []CoverageAnswer{ans("s1", 1, true, true), ans("s2", 2, false, false)}, nil, CoverageLearning, false},
		{"老师批改后又自批：取最近一次", []CoverageAnswer{ans("s1", 1, true, false), ans("s2", 2, true, true)}, nil, CoverageKnown, true},
		{"自批答错后记忆达到已掌握：仍按那次自批", []CoverageAnswer{ans("s1", 1, false, true)}, &CoverageMemory{Stability: 30, LastReview: ptr(at(3))}, CoverageKnown, true},
		{"自批的重测组不计", []CoverageAnswer{ans("s1", 1, true, false), {SessionID: "s2", Kind: "sheet", Phase: "test", Attempt: 1, Correct: true, At: at(2), Retake: true, SelfGraded: true}}, nil, CoverageKnown, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st, self := WordCoverageDetail(c.answers, c.memory)
			if st != c.wantStatus || self != c.wantSelf {
				t.Fatalf("got %s %v want %s %v", st, self, c.wantStatus, c.wantSelf)
			}
		})
	}
}

func TestCountSelfGraded(t *testing.T) {
	books := []CoverageBook{
		{BookID: "b1", WordIDs: []string{"a", "b", "c", "a"}},
		{BookID: "b2", WordIDs: []string{"a", "d", "e"}},
	}
	status := map[string]CoverageStatus{"a": CoverageKnown, "b": CoverageLearning, "c": CoverageKnown, "d": CoverageUntested}
	cases := []struct {
		name string
		self map[string]bool
		want int
	}{
		{"没有自批", nil, 0},
		{"跨书重复只算一次", map[string]bool{"a": true}, 1},
		{"会了和要学都算", map[string]bool{"a": true, "b": true}, 2},
		{"未测的和不在目标里的不算", map[string]bool{"d": true, "e": true, "x": true, "c": true}, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := CountSelfGraded(status, c.self, books); got != c.want {
				t.Fatalf("got %d want %d", got, c.want)
			}
		})
	}
}

func TestSummarizeCoverage(t *testing.T) {
	status := map[string]CoverageStatus{"a": CoverageKnown, "b": CoverageLearning, "c": CoverageKnown, "x": CoverageLearning}
	// b1 书内重复只算一次；b2 的 a 与 b1 重叠，合计只算一次
	books := []CoverageBook{
		{BookID: "b1", Name: "七上", WordIDs: []string{"a", "b", "d", "a"}},
		{BookID: "b2", Name: "中考", WordIDs: []string{"a", "c", "e"}},
		{BookID: "b3", Name: "空书", WordIDs: nil},
	}
	total, per := SummarizeCoverage(status, books)
	wantTotal := CoverageCounts{Target: 5, Tested: 3, Known: 2, Learning: 1, Untested: 2}
	if total != wantTotal {
		t.Fatalf("total = %+v", total)
	}
	want := []CoverageBookCounts{
		{BookID: "b1", Name: "七上", CoverageCounts: CoverageCounts{Target: 3, Tested: 2, Known: 1, Learning: 1, Untested: 1}},
		{BookID: "b2", Name: "中考", CoverageCounts: CoverageCounts{Target: 3, Tested: 2, Known: 2, Learning: 0, Untested: 1}},
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
