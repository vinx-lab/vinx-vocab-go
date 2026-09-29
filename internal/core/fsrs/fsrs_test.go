package fsrs

import (
	"encoding/json"
	"math"
	"os"
	"testing"
	"time"
)

// FixtureCard 对照数据里的卡片（字段名与旧 MemoryCard 一致）。
type FixtureCard struct {
	Due           string  `json:"due"`
	Stability     float64 `json:"stability"`
	Difficulty    float64 `json:"difficulty"`
	ElapsedDays   int     `json:"elapsedDays"`
	ScheduledDays int     `json:"scheduledDays"`
	LearningSteps int     `json:"learningSteps"`
	Reps          int     `json:"reps"`
	Lapses        int     `json:"lapses"`
	State         int     `json:"state"`
	LastReview    *string `json:"lastReview"`
}

type fixtureStep struct {
	Now    string       `json:"now"`
	Rating int          `json:"rating"`
	Card   FixtureCard  `json:"card"`
	Cap    *string      `json:"cap"`
	Capped *FixtureCard `json:"capped"`
}

type fixtureSeq struct {
	Start   string       `json:"start"`
	Initial *FixtureCard `json:"initial"`
	Steps   []fixtureStep
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func toCard(t *testing.T, f FixtureCard) Card {
	c := Card{Due: mustTime(t, f.Due), Stability: f.Stability, Difficulty: f.Difficulty, ElapsedDays: f.ElapsedDays,
		ScheduledDays: f.ScheduledDays, LearningSteps: f.LearningSteps, Reps: f.Reps, Lapses: f.Lapses, State: State(f.State)}
	if f.LastReview != nil {
		lr := mustTime(t, *f.LastReview)
		c.LastReview = &lr
	}
	return c
}

func loadFixtures(t *testing.T) []fixtureSeq {
	t.Helper()
	b, err := os.ReadFile("testdata/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var d struct {
		TsFsrs    string       `json:"tsFsrs"`
		Sequences []fixtureSeq `json:"sequences"`
	}
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatal(err)
	}
	if d.TsFsrs != "5.4.2" || len(d.Sequences) < 200 {
		t.Fatalf("对照数据不符合要求：ts-fsrs %s，%d 条序列", d.TsFsrs, len(d.Sequences))
	}
	return d.Sequences
}

func compareCard(t *testing.T, where string, got Card, want Card) {
	t.Helper()
	if !got.Due.Equal(want.Due) {
		t.Errorf("%s due = %s, want %s", where, got.Due.UTC().Format(time.RFC3339Nano), want.Due.UTC().Format(time.RFC3339Nano))
	}
	if math.Abs(got.Stability-want.Stability) > 1e-6 || math.Abs(got.Difficulty-want.Difficulty) > 1e-6 {
		t.Errorf("%s s/d = %v/%v, want %v/%v", where, got.Stability, got.Difficulty, want.Stability, want.Difficulty)
	}
	if got.ElapsedDays != want.ElapsedDays || got.ScheduledDays != want.ScheduledDays || got.LearningSteps != want.LearningSteps ||
		got.Reps != want.Reps || got.Lapses != want.Lapses || got.State != want.State {
		t.Errorf("%s ints = %+v, want %+v", where, got, want)
	}
	if (got.LastReview == nil) != (want.LastReview == nil) || (got.LastReview != nil && !got.LastReview.Equal(*want.LastReview)) {
		t.Errorf("%s lastReview = %v, want %v", where, got.LastReview, want.LastReview)
	}
}

// TestNextMatchesTsFsrs 逐步比对 ts-fsrs 5.4.2 的结果（浮点 ≤ 1e-6，时间完全相等）。
// 每一步都从 Go 自己上一步的结果继续（不回灌 JS 的值），误差会累积，所以这也检验了不会漂移。
func TestNextMatchesTsFsrs(t *testing.T) {
	s := New(Params{RequestRetention: 0.9, MaximumInterval: 365, W: DefaultW})
	steps, exact := 0, 0
	for i, q := range loadFixtures(t) {
		var c Card
		if q.Initial != nil {
			c = toCard(t, *q.Initial)
		} else {
			c = Card{Due: mustTime(t, q.Start)}
		}
		for j, st := range q.Steps {
			now := mustTime(t, st.Now)
			// 旧 applyRating：早于上次复习时取上次复习时刻
			at := now
			if c.LastReview != nil && now.Before(*c.LastReview) {
				at = *c.LastReview
			}
			c = s.Next(c, at, Rating(st.Rating))
			want := toCard(t, st.Card)
			compareCard(t, "seq "+itoa(i)+" step "+itoa(j), c, want)
			if c.Stability == want.Stability && c.Difficulty == want.Difficulty {
				exact++
			}
			steps++
			if st.Capped != nil {
				// capDue 的行为由 core 测试覆盖；这里直接采用封顶后的到期时间继续
				c.Due = mustTime(t, st.Capped.Due)
				c.ScheduledDays = st.Capped.ScheduledDays
			}
		}
	}
	t.Logf("%d 步，稳定度与难度逐位相等 %d 步", steps, exact)
}

func TestJSRound(t *testing.T) {
	cases := map[float64]float64{2.5: 3, -2.5: -2, 0.49999999999999994: 0, -0.5: -0, 1.4999: 1, -1.5: -1, 3: 3}
	for in, want := range cases {
		if got := jsRound(in); got != want {
			t.Errorf("jsRound(%v) = %v, want %v", in, got, want)
		}
	}
}

func TestDateDiffInDaysUsesUTCDates(t *testing.T) {
	a := time.Date(2026, 9, 27, 23, 59, 0, 0, time.UTC)
	b := time.Date(2026, 9, 28, 0, 1, 0, 0, time.UTC)
	if got := dateDiffInDays(a, b); got != 1 {
		t.Fatalf("跨 UTC 零点 = %d, want 1", got)
	}
	if got := dateDiffInDays(b, b.Add(23*time.Hour)); got != 0 {
		t.Fatalf("同一 UTC 日 = %d, want 0", got)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}
