package core

import (
	"reflect"
	"testing"
)

// 逐条翻译旧 apps/api/tests/lib/stats.test.ts。

func ans(over func(*AnswerFact)) AnswerFact {
	a := AnswerFact{WordID: "w", Mode: "recognition", Phase: "practice", Attempt: 1, Correct: true, DurationMs: 5000, DayKey: "2026-09-17", SessionID: "s"}
	if over != nil {
		over(&a)
	}
	return a
}

func TestIsFirstAttempt(t *testing.T) {
	if !IsFirstAttempt("practice", 1) || !IsFirstAttempt("test", 1) || IsFirstAttempt("practice", 2) || IsFirstAttempt("consolidate", 1) {
		t.Fatal("首次作答只含练习/检测阶段第 1 次")
	}
}

func TestAccuracy(t *testing.T) {
	r := AccuracyOf([]AnswerFact{ans(func(a *AnswerFact) { a.Correct = false }), ans(nil), ans(func(a *AnswerFact) { a.Phase = "consolidate" })})
	if r.Correct != 1 || r.Total != 2 || r.Rate == nil || *r.Rate != 0.5 {
		t.Fatalf("r = %+v", r)
	}
	if AccuracyOf(nil).Rate != nil {
		t.Fatal("无数据为 null")
	}
}

func TestActiveMinutes(t *testing.T) {
	if got := ActiveMinutes([]AnswerFact{ans(func(a *AnswerFact) { a.DurationMs = 30 * 60_000 }), ans(func(a *AnswerFact) { a.DurationMs = 60_000 })}); got != 2 {
		t.Fatalf("got %d", got)
	}
}

func TestDailySeries(t *testing.T) {
	s := DailySeries("2026-09-17", 3, []AnswerFact{
		ans(nil), ans(func(a *AnswerFact) { a.Correct = false }), ans(func(a *AnswerFact) { a.Phase = "consolidate" }),
		ans(func(a *AnswerFact) { a.DayKey = "2026-09-15"; a.DurationMs = 120_000 }), ans(func(a *AnswerFact) { a.DayKey = "2026-01-01" }),
	}, []string{"2026-09-17", "2026-09-17"}, []string{"2026-09-16"})
	days := []string{s[0].Day, s[1].Day, s[2].Day}
	if !reflect.DeepEqual(days, []string{"2026-09-15", "2026-09-16", "2026-09-17"}) {
		t.Fatalf("days %v", days)
	}
	if s[2].NewWords != 2 || s[2].Answers != 3 || s[2].FirstAttempts != 2 || s[2].Correct != 1 {
		t.Fatalf("s[2] = %+v", s[2])
	}
	if s[1].ReviewedWords != 1 || s[1].Answers != 0 {
		t.Fatalf("s[1] = %+v", s[1])
	}
	if s[0].Minutes != 1 {
		t.Fatalf("s[0] = %+v", s[0])
	}
}

func TestMasteryDistribution(t *testing.T) {
	if got := MasteryDistribution([]float64{1, 8, 30, 25}); got != (MasteryDist{Learning: 1, Consolidating: 1, Mastered: 2}) {
		t.Fatalf("got %+v", got)
	}
}

func TestHardWords(t *testing.T) {
	w := func(id string, correct bool, phase ...string) AnswerFact {
		return ans(func(a *AnswerFact) {
			a.WordID = id
			a.Correct = correct
			if len(phase) > 0 {
				a.Phase = phase[0]
			}
		})
	}
	r := HardWords([]AnswerFact{w("a", false), w("a", true), w("b", false), w("b", false), w("c", true), w("c", false, "consolidate")}, 10)
	got := [][3]any{}
	for _, x := range r {
		got = append(got, [3]any{x.WordID, x.Wrong, x.Total})
	}
	if !reflect.DeepEqual(got, [][3]any{{"b", 2, 2}, {"a", 1, 2}}) {
		t.Fatalf("got %v", got)
	}
}
