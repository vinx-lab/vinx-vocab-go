package core

import (
	"testing"
	"time"
)

// 逐条翻译自旧 apps/api/tests/lib/unfamiliar.test.ts。

var unfamiliarNow = time.Date(2026, 9, 23, 4, 0, 0, 0, time.UTC)

func unfamiliarDays(n int) time.Time { return unfamiliarNow.Add(time.Duration(n) * 24 * time.Hour) }

func unfamiliarCandidate(wordID string, mods ...func(*UnfamiliarCandidate)) UnfamiliarCandidate {
	c := UnfamiliarCandidate{WordID: wordID, Stability: 30, Lapses: 0, Due: unfamiliarDays(30), Wrong14: 0}
	for _, m := range mods {
		m(&c)
	}
	return c
}

func withWrong14(n int) func(*UnfamiliarCandidate) {
	return func(c *UnfamiliarCandidate) { c.Wrong14 = n }
}
func withLapses(n int) func(*UnfamiliarCandidate) {
	return func(c *UnfamiliarCandidate) { c.Lapses = n }
}
func withStability(s float64) func(*UnfamiliarCandidate) {
	return func(c *UnfamiliarCandidate) { c.Stability = s }
}
func withDue(t time.Time) func(*UnfamiliarCandidate) {
	return func(c *UnfamiliarCandidate) { c.Due = t }
}

func reasonsEqual(t *testing.T, got, want []UnfamiliarReason) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("reasons = %+v, want %+v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("reasons[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestScoreUnfamiliar(t *testing.T) {
	t.Run("按规则累加：错 ×3、遗忘 ×2、学习中 +2、快到期 +1", func(t *testing.T) {
		p := ScoreUnfamiliar(unfamiliarCandidate("a", withWrong14(2), withLapses(1), withStability(3), withDue(unfamiliarDays(2))), unfamiliarNow)
		if p == nil {
			t.Fatal("want non-nil")
		}
		if want := 2*3 + 1*2 + 2 + 1; p.Score != want {
			t.Errorf("score = %d, want %d", p.Score, want)
		}
		reasonsEqual(t, p.Reasons, []UnfamiliarReason{{Kind: "wrong", Count: 2}, {Kind: "lapse", Count: 1}, {Kind: "learning"}, {Kind: "dueSoon"}})
	})

	t.Run("巩固中（7 ≤ 稳定性 < 21）+1", func(t *testing.T) {
		p := ScoreUnfamiliar(unfamiliarCandidate("a", withStability(10)), unfamiliarNow)
		if p == nil || p.Score != 1 {
			t.Fatalf("got %+v", p)
		}
		reasonsEqual(t, p.Reasons, []UnfamiliarReason{{Kind: "consolidating"}})
	})

	t.Run("已掌握且近 14 天没错、没遗忘过 → 排除", func(t *testing.T) {
		if p := ScoreUnfamiliar(unfamiliarCandidate("a", withStability(25), withDue(unfamiliarDays(1))), unfamiliarNow); p != nil {
			t.Fatalf("want nil, got %+v", p)
		}
	})

	t.Run("已掌握但遗忘过 → 保留", func(t *testing.T) {
		p := ScoreUnfamiliar(unfamiliarCandidate("a", withStability(25), withLapses(1)), unfamiliarNow)
		if p == nil || p.Score != 2 {
			t.Fatalf("got %+v", p)
		}
	})

	t.Run("3 天内到期的边界：恰好 3 天算，3 天多一点不算", func(t *testing.T) {
		p1 := ScoreUnfamiliar(unfamiliarCandidate("a", withStability(10), withDue(unfamiliarDays(3))), unfamiliarNow)
		if p1 == nil || p1.Score != 2 {
			t.Fatalf("got %+v", p1)
		}
		p2 := ScoreUnfamiliar(unfamiliarCandidate("a", withStability(10), withDue(unfamiliarDays(3).Add(time.Millisecond))), unfamiliarNow)
		if p2 == nil || p2.Score != 1 {
			t.Fatalf("got %+v", p2)
		}
	})
}

func pickIDs(picks []UnfamiliarPick) []string {
	out := make([]string, len(picks))
	for i, p := range picks {
		out[i] = p.WordID
	}
	return out
}

func TestSelectUnfamiliarWords(t *testing.T) {
	t.Run("按分数降序，同分先到期的在前", func(t *testing.T) {
		picks := SelectUnfamiliarWords([]UnfamiliarCandidate{
			unfamiliarCandidate("low", withStability(10)),
			unfamiliarCandidate("high", withWrong14(3)),
			unfamiliarCandidate("tieLate", withStability(10), withDue(unfamiliarDays(10))),
			unfamiliarCandidate("tieEarly", withStability(10), withDue(unfamiliarDays(5))),
		}, SelectOptions{Now: unfamiliarNow, Count: 10})
		want := []string{"high", "tieEarly", "tieLate", "low"}
		got := pickIDs(picks)
		if len(got) != len(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("got %v, want %v", got, want)
			}
		}
	})

	t.Run("排除集合里的词不出现", func(t *testing.T) {
		picks := SelectUnfamiliarWords([]UnfamiliarCandidate{
			unfamiliarCandidate("a", withWrong14(1)),
			unfamiliarCandidate("b", withWrong14(1)),
		}, SelectOptions{Now: unfamiliarNow, Count: 10, Exclude: map[string]bool{"a": true}})
		if got := pickIDs(picks); len(got) != 1 || got[0] != "b" {
			t.Fatalf("got %v", got)
		}
	})

	t.Run("截断到 count", func(t *testing.T) {
		many := make([]UnfamiliarCandidate, 50)
		for i := range many {
			many[i] = unfamiliarCandidate("w"+itoa(i), withWrong14(1))
		}
		picks := SelectUnfamiliarWords(many, SelectOptions{Now: unfamiliarNow, Count: 12})
		if len(picks) != 12 {
			t.Fatalf("len = %d, want 12", len(picks))
		}
	})

	t.Run("include 的词排最前、带 retest 原因，其余按分数补齐；include 不受 exclude 影响", func(t *testing.T) {
		picks := SelectUnfamiliarWords([]UnfamiliarCandidate{
			unfamiliarCandidate("a", withWrong14(5)),
			unfamiliarCandidate("b", withWrong14(1)),
			unfamiliarCandidate("x", withStability(3)),
		}, SelectOptions{Now: unfamiliarNow, Count: 3, Include: []string{"x", "ghost"}, Exclude: map[string]bool{"x": true}})
		want := []string{"x", "ghost", "a"}
		got := pickIDs(picks)
		if len(got) != len(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("got %v, want %v", got, want)
			}
		}
		if picks[0].Reasons[0] != (UnfamiliarReason{Kind: "retest"}) {
			t.Fatalf("reasons[0] = %+v", picks[0].Reasons)
		}
		found := false
		for _, r := range picks[0].Reasons {
			if r == (UnfamiliarReason{Kind: "learning"}) {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected learning reason in %+v", picks[0].Reasons)
		}
		if picks[1].WordID != "ghost" || picks[1].Score != 0 {
			t.Fatalf("got %+v", picks[1])
		}
		reasonsEqual(t, picks[1].Reasons, []UnfamiliarReason{{Kind: "retest"}})
	})

	t.Run("include 超过 count 时全部保留，但不超过上限", func(t *testing.T) {
		include := make([]string, SheetMax+5)
		for i := range include {
			include[i] = "i" + itoa(i)
		}
		picks := SelectUnfamiliarWords(nil, SelectOptions{Now: unfamiliarNow, Count: 10, Include: include})
		if len(picks) != SheetMax {
			t.Fatalf("len = %d, want %d", len(picks), SheetMax)
		}
	})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func TestSelectFromPool(t *testing.T) {
	t.Run("错词来源：池里的词全部保留（含已掌握、没学过的），已学的按分数降序，没学过的按池顺序排后面", func(t *testing.T) {
		picks := SelectFromPool([]string{"m", "new1", "low", "high", "new2"}, []UnfamiliarCandidate{
			unfamiliarCandidate("m", withStability(25)),
			unfamiliarCandidate("low", withStability(10)),
			unfamiliarCandidate("high", withWrong14(2)),
		}, PoolOptions{Now: unfamiliarNow, Count: 10, KeepFamiliar: true, Tag: &UnfamiliarReason{Kind: "sessionWrong"}})
		want := []string{"high", "low", "m", "new1", "new2"}
		got := pickIDs(picks)
		if len(got) != len(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("got %v, want %v", got, want)
			}
		}
		for _, p := range picks {
			if p.Reasons[0].Kind != "sessionWrong" {
				t.Fatalf("reasons[0] = %+v", p.Reasons)
			}
		}
		var new1 *UnfamiliarPick
		for i := range picks {
			if picks[i].WordID == "new1" {
				new1 = &picks[i]
			}
		}
		if new1 == nil {
			t.Fatal("new1 missing")
		}
		found := false
		for _, r := range new1.Reasons {
			if r == (UnfamiliarReason{Kind: "unlearned"}) {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected unlearned reason in %+v", new1.Reasons)
		}
	})

	t.Run("单元来源：排除已掌握且干净的词，不熟的在前，没学过的在后", func(t *testing.T) {
		picks := SelectFromPool([]string{"new1", "m", "low", "high"}, []UnfamiliarCandidate{
			unfamiliarCandidate("m", withStability(25)),
			unfamiliarCandidate("low", withStability(10)),
			unfamiliarCandidate("high", withLapses(1), withStability(3)),
		}, PoolOptions{Now: unfamiliarNow, Count: 10, KeepFamiliar: false})
		want := []string{"high", "low", "new1"}
		got := pickIDs(picks)
		if len(got) != len(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("got %v, want %v", got, want)
			}
		}
		if picks[2].WordID != "new1" || picks[2].Score != 0 {
			t.Fatalf("got %+v", picks[2])
		}
		reasonsEqual(t, picks[2].Reasons, []UnfamiliarReason{{Kind: "unlearned"}})
	})

	t.Run("池内重复只算一次；截断到 count，且不超过总上限", func(t *testing.T) {
		got := pickIDs(SelectFromPool([]string{"a", "a", "b", "c"}, nil, PoolOptions{Now: unfamiliarNow, Count: 2, KeepFamiliar: false}))
		if len(got) != 2 || got[0] != "a" || got[1] != "b" {
			t.Fatalf("got %v", got)
		}
		pool := make([]string, SheetMax+5)
		for i := range pool {
			pool[i] = "p" + itoa(i)
		}
		picks := SelectFromPool(pool, nil, PoolOptions{Now: unfamiliarNow, Count: SheetMax + 5, KeepFamiliar: false})
		if len(picks) != SheetMax {
			t.Fatalf("len = %d, want %d", len(picks), SheetMax)
		}
	})
}
