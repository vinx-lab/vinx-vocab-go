package core

import (
	"encoding/json"
	"math"
	"os"
	"testing"
	"time"
)

// 逐条翻译旧 apps/api/tests/lib/scheduler.test.ts，另加 ts-fsrs 对照数据回放（经 ApplyRating / CapDue）。

var (
	schedNow = mustTime("2026-09-17T02:00:00Z")
	both     = []string{"recognition", "spelling"}
)

const dayDur = 24 * time.Hour

func fa(mode string, correct bool, hint ...bool) FirstAttempt {
	return FirstAttempt{Mode: mode, Correct: correct, HintUsed: len(hint) > 0 && hint[0]}
}

func TestDeriveRating(t *testing.T) {
	cases := []struct {
		name     string
		kind     string
		attempts []FirstAttempt
		modes    []string
		want     Rating
	}{
		// learn：全对 Hard，有错 Again（当天看过答案只算弱证据）
		{"learn 全对", "learn", []FirstAttempt{fa("recognition", true), fa("spelling", true)}, both, RatingHard},
		{"learn 有错", "learn", []FirstAttempt{fa("recognition", true), fa("spelling", false)}, both, RatingAgain},
		// review：全对 Good，部分对或提示 Hard，全错 Again
		{"review 全对", "review", []FirstAttempt{fa("recognition", true), fa("spelling", true)}, both, RatingGood},
		{"review 部分对", "review", []FirstAttempt{fa("recognition", true), fa("spelling", false)}, both, RatingHard},
		{"review 提示", "review", []FirstAttempt{fa("recognition", true), fa("spelling", true, true)}, both, RatingHard},
		{"review 全错", "review", []FirstAttempt{fa("recognition", false), fa("spelling", false)}, both, RatingAgain},
		// test：全对 Good，否则 Again
		{"test 对", "test", []FirstAttempt{fa("recognition", true)}, []string{"recognition"}, RatingGood},
		{"test 错", "test", []FirstAttempt{fa("recognition", false)}, []string{"recognition"}, RatingAgain},
		// drill 不更新记忆；题型没答全不结算
		{"drill", "drill", []FirstAttempt{fa("recognition", true)}, []string{"recognition"}, 0},
		{"没答全", "review", []FirstAttempt{fa("recognition", true)}, both, 0},
		{"无题型", "review", nil, []string{}, 0},
		// sheet 与 test 同口径
		{"sheet 全对", "sheet", []FirstAttempt{fa("recognition", true), fa("spelling", true)}, both, RatingGood},
		{"sheet 有错", "sheet", []FirstAttempt{fa("recognition", true), fa("spelling", false)}, both, RatingAgain},
		{"sheet 缺题型", "sheet", []FirstAttempt{fa("recognition", true)}, both, 0},
	}
	for _, c := range cases {
		if got := DeriveRating(c.kind, c.attempts, c.modes); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestDeriveRatingDontKnow(t *testing.T) {
	// 「不会」按答错评分：点「不会」的题型记为错，各种组都走 Again
	dk := func(mode string, hint bool) FirstAttempt {
		return FirstAttempt{Mode: mode, Correct: JudgeAnswer(mode, "", true, "恐龙", "dinosaur"), HintUsed: hint}
	}
	if dk("recognition", false).Correct {
		t.Fatal("不会应判错")
	}
	checks := []struct {
		kind  string
		at    []FirstAttempt
		modes []string
	}{
		{"learn", []FirstAttempt{dk("recognition", false), fa("spelling", true)}, both},
		{"review", []FirstAttempt{dk("recognition", false), dk("spelling", true)}, both},
		{"test", []FirstAttempt{dk("recognition", false)}, []string{"recognition"}},
		{"sheet", []FirstAttempt{dk("spelling", false)}, []string{"spelling"}},
	}
	for _, c := range checks {
		if got := DeriveRating(c.kind, c.at, c.modes); got != RatingAgain {
			t.Errorf("%s: got %v want Again", c.kind, got)
		}
	}
}

func TestFSRSNewCardAndCapDue(t *testing.T) {
	// 新词 Again ≤ Hard < Good；capDue 把新学词封顶到下个学习日
	c := NewMemoryCard(schedNow)
	if c.State != 0 {
		t.Fatal("新卡状态应为 New")
	}
	hard := ApplyRating(c, RatingHard, schedNow)
	again := ApplyRating(c, RatingAgain, schedNow)
	good := ApplyRating(c, RatingGood, schedNow)
	if again.Due.After(hard.Due) || !good.Due.After(hard.Due) {
		t.Fatalf("到期顺序不对：again %v hard %v good %v", again.Due, hard.Due, good.Due)
	}
	if hard.Reps != 1 {
		t.Fatalf("reps = %d", hard.Reps)
	}
	tomorrow := schedNow.Add(dayDur)
	capped := CapDue(hard, tomorrow, schedNow)
	if !capped.Due.Equal(tomorrow) || capped.ScheduledDays != 1 || capped.Stability != hard.Stability {
		t.Fatalf("capDue = %+v", capped)
	}
	if got := CapDue(again, schedNow.Add(10*dayDur), schedNow); got != again {
		t.Fatal("到期早于封顶时不变")
	}
}

func TestFSRSIntervalsGrowAndLapses(t *testing.T) {
	// 连续答对间隔递增，遗忘计入 lapses
	c := ApplyRating(NewMemoryCard(schedNow), RatingHard, schedNow)
	at := c.Due
	intervals := []time.Duration{}
	for i := 0; i < 4; i++ {
		next := ApplyRating(c, RatingGood, at)
		intervals = append(intervals, next.Due.Sub(at))
		c = next
		at = next.Due
	}
	for i := 1; i < len(intervals); i++ {
		if intervals[i] <= intervals[i-1] {
			t.Fatalf("间隔未递增：%v", intervals)
		}
	}
	lapsed := ApplyRating(c, RatingAgain, at)
	if lapsed.Lapses != 1 || lapsed.Stability >= c.Stability {
		t.Fatalf("lapsed = %+v", lapsed)
	}
}

func TestFSRSClockBackwards(t *testing.T) {
	// 复习时刻早于上次复习（时钟回拨）不出错，并按上次复习时刻计算
	later := schedNow.Add(3 * dayDur)
	c := ApplyRating(ApplyRating(NewMemoryCard(schedNow), RatingHard, schedNow), RatingGood, later)
	got := ApplyRating(c, RatingGood, schedNow)
	if got.LastReview == nil || !got.LastReview.Equal(later) {
		t.Fatalf("lastReview = %v", got.LastReview)
	}
}

func TestMasteryLevel(t *testing.T) {
	for s, want := range map[float64]string{3: "learning", 7: "consolidating", 21: "mastered"} {
		if got := MasteryLevel(s); got != want {
			t.Errorf("MasteryLevel(%v) = %s", s, got)
		}
	}
}

// TestSchedulerFixtures 用 ts-fsrs 对照数据回放 ApplyRating 与 CapDue（与旧 lib/scheduler.ts 的封装逐步一致）。
func TestSchedulerFixtures(t *testing.T) {
	b, err := os.ReadFile("fsrs/testdata/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	type fcard struct {
		Due                                                            string
		Stability, Difficulty                                          float64
		ElapsedDays, ScheduledDays, LearningSteps, Reps, Lapses, State int
		LastReview                                                     *string
	}
	var d struct {
		Sequences []struct {
			Start   string
			Initial *fcard
			Steps   []struct {
				Now    string
				Rating int
				Card   fcard
				Cap    *string
				Capped *fcard
			}
		}
	}
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatal(err)
	}
	conv := func(f fcard) MemoryCard {
		m := MemoryCard{Due: mustTime(f.Due), Stability: f.Stability, Difficulty: f.Difficulty, ElapsedDays: f.ElapsedDays,
			ScheduledDays: f.ScheduledDays, LearningSteps: f.LearningSteps, Reps: f.Reps, Lapses: f.Lapses, State: f.State}
		if f.LastReview != nil {
			lr := mustTime(*f.LastReview)
			m.LastReview = &lr
		}
		return m
	}
	same := func(a, b MemoryCard) bool {
		lr := (a.LastReview == nil) == (b.LastReview == nil) && (a.LastReview == nil || a.LastReview.Equal(*b.LastReview))
		return a.Due.Equal(b.Due) && math.Abs(a.Stability-b.Stability) <= 1e-6 && math.Abs(a.Difficulty-b.Difficulty) <= 1e-6 &&
			a.ElapsedDays == b.ElapsedDays && a.ScheduledDays == b.ScheduledDays && a.LearningSteps == b.LearningSteps &&
			a.Reps == b.Reps && a.Lapses == b.Lapses && a.State == b.State && lr
	}
	steps, caps := 0, 0
	for i, q := range d.Sequences {
		c := NewMemoryCard(mustTime(q.Start))
		if q.Initial != nil {
			c = conv(*q.Initial)
		}
		for j, st := range q.Steps {
			now := mustTime(st.Now)
			c = ApplyRating(c, Rating(st.Rating), now)
			if want := conv(st.Card); !same(c, want) {
				t.Fatalf("seq %d step %d: got %+v want %+v", i, j, c, want)
			}
			steps++
			if st.Cap != nil {
				c = CapDue(c, mustTime(*st.Cap), now)
				if want := conv(*st.Capped); !same(c, want) {
					t.Fatalf("seq %d step %d capDue: got %+v want %+v", i, j, c, want)
				}
				caps++
			}
		}
	}
	if len(d.Sequences) < 200 {
		t.Fatalf("对照序列不足 200：%d", len(d.Sequences))
	}
	t.Logf("%d 条序列、%d 步、%d 次 capDue 全部一致", len(d.Sequences), steps, caps)
}
