package service

import (
	"context"
	"reflect"
	"testing"
	"time"
)

// memRow MemoryState 的全部可比较列（不含 id、updatedAt）。
type memRow struct {
	Due, LastReview, IntroducedAt, IntroducedDay string
	IntroducedPlanID                             *string
	Stability, Difficulty                        float64
	ElapsedDays, ScheduledDays, Steps            int
	Reps, Lapses, State                          int
}

func memRows(t *testing.T, f *learnFixture) map[string]memRow {
	t.Helper()
	rows, err := f.db.Query(`SELECT "wordId","due",coalesce("lastReview",''),"introducedAt","introducedDay","introducedPlanId","stability","difficulty",
		"elapsedDays","scheduledDays","learningSteps","reps","lapses","state" FROM "MemoryState" WHERE "userId" = ?`, f.userID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]memRow{}
	for rows.Next() {
		var w string
		var m memRow
		if err := rows.Scan(&w, &m.Due, &m.LastReview, &m.IntroducedAt, &m.IntroducedDay, &m.IntroducedPlanID, &m.Stability, &m.Difficulty,
			&m.ElapsedDays, &m.ScheduledDays, &m.Steps, &m.Reps, &m.Lapses, &m.State); err != nil {
			t.Fatal(err)
		}
		out[w] = m
	}
	return out
}

type logRowT struct {
	Word, Session, ReviewedAt, Day, Due string
	Rating, StateBefore                 int
	Stability                           float64
}

func reviewLogs(t *testing.T, f *learnFixture, sessionID string) []logRowT {
	t.Helper()
	rows, err := f.db.Query(`SELECT "wordId","sessionId","reviewedAt","dayKey","dueAfter","rating","stateBefore","stabilityAfter" FROM "ReviewLog"
		WHERE "sessionId" = ? ORDER BY "wordId"`, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := []logRowT{}
	for rows.Next() {
		var r logRowT
		if err := rows.Scan(&r.Word, &r.Session, &r.ReviewedAt, &r.Day, &r.Due, &r.Rating, &r.StateBefore, &r.Stability); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

// TestBackfillMatchesLiveSettlement 补算的结果与「当时就按新规则结算」一致：先按新规则结算一组检测，记下记忆与复习记录，
// 再把库退回「旧规则没动记忆」的样子，补算后应完全复原；演练不写库；再跑一次没有可补的；与已有记录交错的词跳过。
func TestBackfillMatchesLiveSettlement(t *testing.T) {
	ctx := context.Background()
	f := newLearnFixture(t, 6)
	day1 := sh(2026, 9, 28, 10, 0, 0)
	daily := f.plan(t, day1, PlanInput{NewPerDay: 3})
	s := f.start(t, day1, "learn", daily)
	f.answerAll(t, day1, s.Session, "practice", nil)
	f.complete(t, day1, s.Session.ID)
	before := memRows(t, f)

	day2 := sh(2026, 9, 29, 10, 0, 0)
	testPlan := f.plan(t, day1, PlanInput{Kind: "test", Modes: []string{"recognition"}, TestSize: 6, TestScope: "all"})
	ts := f.start(t, day2, "test", testPlan)
	missed := ""
	for _, it := range snapOf(t, ts.Session).Items {
		if _, ok := before[it.WordID]; !ok {
			missed = it.WordID
			break
		}
	}
	f.answerAll(t, day2, ts.Session, "test", func(w, _ string) bool { return w == missed })
	f.complete(t, day2.Add(time.Minute), ts.Session.ID)
	wantMem := memRows(t, f)
	wantLogs := reviewLogs(t, f, ts.Session.ID)
	if len(wantMem) != 5 || len(wantLogs) != 5 {
		t.Fatalf("前提：结算后记忆 %d、复习记录 %d", len(wantMem), len(wantLogs))
	}

	// 退回旧规则下的样子：这一组没有复习记录，记忆停在新学之后
	revert := func() {
		t.Helper()
		f.db.Exec(`DELETE FROM "ReviewLog" WHERE "sessionId" = ?`, ts.Session.ID)
		f.db.Exec(`DELETE FROM "MemoryState" WHERE "userId" = ? AND "introducedPlanId" IS NULL`, f.userID)
		for w, m := range before {
			if _, err := f.db.Exec(`UPDATE "MemoryState" SET "due"=?,"lastReview"=?,"stability"=?,"difficulty"=?,"elapsedDays"=?,"scheduledDays"=?,"learningSteps"=?,"reps"=?,"lapses"=?,"state"=?
				WHERE "userId" = ? AND "wordId" = ?`, m.Due, m.LastReview, m.Stability, m.Difficulty, m.ElapsedDays, m.ScheduledDays, m.Steps, m.Reps, m.Lapses, m.State, f.userID, w); err != nil {
				t.Fatal(err)
			}
		}
		if got := memRows(t, f); !reflect.DeepEqual(got, before) {
			t.Fatalf("退回失败")
		}
	}
	revert()

	// 演练：报告对，但不写库
	dry, err := BackfillMemory(ctx, f.db, shanghai, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(dry.Users) != 1 || dry.Users[0].Words != 6 || dry.Users[0].Events != 5 || dry.Users[0].Created != 2 || dry.Users[0].Updated != 3 || dry.Users[0].NoMem != 1 || dry.Users[0].Skipped != 0 {
		t.Fatalf("演练 = %+v", dry.Users)
	}
	if got := memRows(t, f); !reflect.DeepEqual(got, before) {
		t.Fatal("演练改了记忆")
	}

	rep, err := BackfillMemory(ctx, f.db, shanghai, true)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Applied || rep.Users[0].Events != 5 {
		t.Fatalf("执行 = %+v", rep.Users)
	}
	if got := memRows(t, f); !reflect.DeepEqual(got, wantMem) {
		t.Fatalf("补算后的记忆与按新规则结算不一致：\ngot  %+v\nwant %+v", got, wantMem)
	}
	if got := reviewLogs(t, f, ts.Session.ID); !reflect.DeepEqual(got, wantLogs) {
		t.Fatalf("补写的复习记录不一致：\ngot  %+v\nwant %+v", got, wantLogs)
	}
	var tagged int
	f.db.QueryRow(`SELECT count(*) FROM "ReviewLog" WHERE "source" = ?`, BackfillSource).Scan(&tagged)
	if tagged != 5 {
		t.Fatalf("带补算标记的复习记录 %d", tagged)
	}

	// 幂等：再跑没有可补的（答错的生词仍只算「涉及」，不写）
	again, err := BackfillMemory(ctx, f.db, shanghai, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Users) != 1 || again.Users[0].Events != 0 || again.Users[0].Words != 1 || again.Users[0].NoMem != 1 {
		t.Fatalf("再跑 = %+v", again.Users)
	}

	// 检测里答错的生词，之后在每日计划里学了：那次答错不需要补，也不算交错
	day3 := sh(2026, 9, 30, 10, 0, 0)
	l2 := f.start(t, day3, "learn", daily)
	f.answerAll(t, day3, l2.Session, "practice", nil)
	f.complete(t, day3, l2.Session.ID)
	if mems := memRows(t, f); len(mems) != 6 {
		t.Fatalf("学完后记忆 %d", len(mems))
	}
	if r, err := BackfillMemory(ctx, f.db, shanghai, true); err != nil || len(r.Users) != 0 {
		t.Fatalf("学过以后再跑 = %+v %v", r, err)
	}
	f.db.Exec(`DELETE FROM "ReviewLog" WHERE "sessionId" = ?`, l2.Session.ID)
	f.db.Exec(`DELETE FROM "MemoryState" WHERE "userId" = ? AND "introducedDay" = '2026-09-30'`, f.userID)

	// 与已有记录交错：退回后把一个已学词的最后复习时间改到检测之后 → 跳过，不动
	revert()
	var learned string
	for w := range before {
		learned = w
		break
	}
	f.db.Exec(`UPDATE "MemoryState" SET "lastReview" = ? WHERE "userId" = ? AND "wordId" = ?`, "2026-09-30T02:00:00.000Z", f.userID, learned)
	rep, err = BackfillMemory(ctx, f.db, shanghai, true)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Users[0].Skipped != 1 || rep.Users[0].Updated != 2 {
		t.Fatalf("交错 = %+v", rep.Users)
	}
	var n int
	f.db.QueryRow(`SELECT count(*) FROM "ReviewLog" WHERE "wordId" = ? AND "sessionId" = ?`, learned, ts.Session.ID).Scan(&n)
	if n != 0 {
		t.Fatal("交错的词不应补写")
	}
}
