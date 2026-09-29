package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/vinx-lab/vinx-vocab-go/internal/core"
	"github.com/vinx-lab/vinx-vocab-go/internal/httpx"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// 学习流服务层测试（对应旧 tests/learning.flow.test.ts 里直接调用服务函数的部分），
// 注入固定时钟，覆盖上海时区 00:00 前后的学习日边界、K19 / K20 / K22、并发开组与作答幂等。

var shanghai = mustLoc("Asia/Shanghai")

func mustLoc(name string) *time.Location {
	l, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return l
}

// sh 上海时间。
func sh(y int, m time.Month, d, hh, mm, ss int) time.Time {
	return time.Date(y, m, d, hh, mm, ss, 0, shanghai)
}

type learnFixture struct {
	db      *store.DB
	userID  string
	unitID  string
	wordIDs []string
}

func insertTestUser(t *testing.T, db *store.DB, id, role string) {
	t.Helper()
	now := store.NewTime(time.Now())
	if _, err := db.Exec(`INSERT INTO "User" ("id","email","passwordHash","name","role","createdAt","updatedAt") VALUES (?,?,?,?,?,?,?)`,
		id, id+"@x.test", "x", "用户"+id, role, now, now); err != nil {
		t.Fatal(err)
	}
}

// newLearnFixture 一个学生 + 一本 n 个词的书（每个词都有例句）。
func newLearnFixture(t *testing.T, n int) *learnFixture {
	t.Helper()
	db := openTempDB(t)
	f := &learnFixture{db: db, userID: "stu"}
	insertTestUser(t, db, f.userID, core.RoleStudent)
	if _, err := db.Exec(`INSERT INTO "Book" ("id","name","isSystem") VALUES ('b1','书',1)`); err != nil {
		t.Fatal(err)
	}
	f.unitID = "u1"
	if _, err := db.Exec(`INSERT INTO "Unit" ("id","bookId","name") VALUES ('u1','b1','U1')`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("w%02d", i)
		sp := fmt.Sprintf("word%c", 'a'+i)
		if _, err := db.Exec(`INSERT INTO "Word" ("id","spelling","partOfSpeech","definition","example","exampleCn") VALUES (?,?,?,?,?,?)`,
			id, sp, "n.", fmt.Sprintf("释义%d", i), fmt.Sprintf("I like the %s here.", sp), "例句"); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO "UnitWord" ("unitId","wordId","sortOrder") VALUES ('u1',?,?)`, id, i); err != nil {
			t.Fatal(err)
		}
		f.wordIDs = append(f.wordIDs, id)
	}
	return f
}

func (f *learnFixture) plan(t *testing.T, now time.Time, in PlanInput) string {
	t.Helper()
	if in.Kind == "" {
		in.Kind = "daily"
	}
	if in.Modes == nil {
		in.Modes = []string{"recognition", "spelling"}
	}
	in.Status, in.Order, in.TestScope = "active", "sequential", orDefault(in.TestScope, "all")
	if in.NewPerDay == 0 {
		in.NewPerDay = 12
	}
	if in.ReviewPerDay == 0 {
		in.ReviewPerDay = 50
	}
	if in.TestSize == 0 {
		in.TestSize = 20
	}
	in.Name = "计划"
	in.UnitIDs = []string{f.unitID}
	in.Targets = PlanTargetsInput{UserIDs: []string{f.userID}}
	var id string
	if err := f.db.Tx(context.Background(), func(tx *sql.Tx) error {
		p, err := CreatePlan(context.Background(), tx, now, f.userID, in)
		if p != nil {
			id = p.ID
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return id
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func (f *learnFixture) start(t *testing.T, now time.Time, kind, planID string) *StartResult {
	t.Helper()
	var pid *string
	if planID != "" {
		pid = &planID
	}
	res, err := StartSession(context.Background(), f.db, shanghai, now, core.SeededRng(7), f.userID, StartInput{Kind: kind, PlanID: pid})
	if err != nil {
		t.Fatalf("StartSession(%s): %v", kind, err)
	}
	return res
}

func snapOf(t *testing.T, s *SessionRow) *Snapshot {
	t.Helper()
	snap, err := ParseSnapshot(s.SnapshotText)
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

// answerAll 按快照作答：wrong(wordId, mode) 为真时答错。
func (f *learnFixture) answerAll(t *testing.T, now time.Time, s *SessionRow, phase string, wrong func(string, string) bool) {
	t.Helper()
	snap := snapOf(t, s)
	for i := range snap.Items {
		it := &snap.Items[i]
		for _, mode := range snap.ItemModes(it) {
			ans := it.Answer
			if mode == "recognition" {
				ans = it.Definition
			}
			if wrong != nil && wrong(it.WordID, mode) {
				ans = "__wrong__"
			}
			if _, err := RecordAnswer(context.Background(), f.db, shanghai, now, f.userID, s.ID, AnswerInput{
				WordID: it.WordID, Mode: mode, Phase: phase, Attempt: 1, Answer: ans, DurationMs: 3000,
			}); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func (f *learnFixture) complete(t *testing.T, now time.Time, id string) SessionResult {
	t.Helper()
	out, err := CompleteSession(context.Background(), f.db, shanghai, now, f.userID, id)
	if err != nil {
		t.Fatal(err)
	}
	var r SessionResult
	if err := json.Unmarshal(out.Result, &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func (f *learnFixture) sessionDays(t *testing.T, id string) (day, completedDay string) {
	t.Helper()
	var cd sql.NullString
	if err := f.db.QueryRow(`SELECT "dayKey","completedDay" FROM "StudySession" WHERE "id" = ?`, id).Scan(&day, &cd); err != nil {
		t.Fatal(err)
	}
	return day, cd.String
}

func codeOf(err error) string {
	if e, ok := err.(*httpx.Error); ok {
		return e.Code + " " + e.Message
	}
	if err == nil {
		return ""
	}
	return err.Error()
}

// TestLearnReviewAcrossMidnight 学新词 → 跨上海 00:00 → 复习：学习日、封顶到期、每日最多更新一次（K19）。
func TestLearnReviewAcrossMidnight(t *testing.T) {
	ctx := context.Background()
	f := newLearnFixture(t, 15)
	t0 := sh(2026, 9, 28, 23, 50, 0) // 学习日 2026-09-28
	planID := f.plan(t, t0, PlanInput{NewPerDay: 12, ReviewPerDay: 30})

	today, err := ComputeToday(ctx, f.db, shanghai, t0, f.userID)
	if err != nil {
		t.Fatal(err)
	}
	if today.Day != "2026-09-28" || today.Plans[0].NewLeft != 12 || today.Plans[0].Source != "self" {
		t.Fatalf("today = %+v", today)
	}

	// 第一组：23:50 开组、作答、结算
	s1 := f.start(t, t0, "learn", planID)
	if s1.Resumed || s1.Session.WordCount != 10 || s1.Session.DayKey != "2026-09-28" {
		t.Fatalf("s1 = %+v", s1.Session)
	}
	if again := f.start(t, t0, "learn", planID); !again.Resumed || again.Session.ID != s1.Session.ID {
		t.Fatal("同一计划同一类型应续做同一组")
	}
	snap1 := snapOf(t, s1.Session)
	for _, it := range snap1.Items {
		if len(it.Options) != 4 || !contains(it.Options, it.Definition) || it.Cloze != nil {
			t.Fatalf("选项或挖空不对：%+v", it)
		}
	}
	firstWord := snap1.Items[0].WordID
	f.answerAll(t, t0, s1.Session, "practice", func(w, m string) bool { return w == firstWord && m == "spelling" })
	r1 := f.complete(t, t0.Add(time.Minute), s1.Session.ID)
	if r1.Words != 10 || r1.Settled != 10 || r1.NewLearned != 10 || r1.TotalFirst != 20 || r1.CorrectFirst != 19 ||
		r1.Ratings["hard"] != 9 || r1.Ratings["again"] != 1 || len(r1.WrongWordIDs) != 1 || r1.WrongWordIDs[0] != firstWord || r1.DurationMs != 20*3000 {
		t.Fatalf("r1 = %+v", r1)
	}
	if d, cd := f.sessionDays(t, s1.Session.ID); d != "2026-09-28" || cd != "2026-09-28" {
		t.Fatalf("days %s %s", d, cd)
	}
	tomorrow := sh(2026, 9, 29, 0, 0, 0)
	var badDue int
	if err := f.db.QueryRow(`SELECT count(*) FROM "MemoryState" WHERE "userId" = ? AND ("due" > ? OR "introducedDay" <> '2026-09-28' OR "introducedPlanId" <> ?)`,
		f.userID, tomorrow, planID).Scan(&badDue); err != nil || badDue != 0 {
		t.Fatalf("新学词应封顶到下个学习日 00:00：bad=%d err=%v", badDue, err)
	}

	// 第二组：23:59 开组作答，00:01（次日）结算 → dayKey 仍是开组日，completedDay / ReviewLog 是结算日
	t1 := sh(2026, 9, 28, 23, 59, 0)
	s2 := f.start(t, t1, "learn", planID)
	if s2.Session.WordCount != 2 {
		t.Fatalf("额度内只剩 2 词，得到 %d", s2.Session.WordCount)
	}
	f.answerAll(t, t1, s2.Session, "practice", nil)
	t2 := sh(2026, 9, 29, 0, 1, 0)
	r2 := f.complete(t, t2, s2.Session.ID)
	if r2.NewLearned != 2 || r2.Ratings["hard"] != 2 {
		t.Fatalf("r2 = %+v", r2)
	}
	if d, cd := f.sessionDays(t, s2.Session.ID); d != "2026-09-28" || cd != "2026-09-29" {
		t.Fatalf("跨午夜结算：dayKey %s completedDay %s", d, cd)
	}
	var answerDay, logDay, introDay string
	var due store.Time
	w2 := snapOf(t, s2.Session).Items[0].WordID
	if err := f.db.QueryRow(`SELECT "dayKey" FROM "Answer" WHERE "sessionId" = ? LIMIT 1`, s2.Session.ID).Scan(&answerDay); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(`SELECT "dayKey" FROM "ReviewLog" WHERE "sessionId" = ? LIMIT 1`, s2.Session.ID).Scan(&logDay); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(`SELECT "introducedDay","due" FROM "MemoryState" WHERE "wordId" = ?`, w2).Scan(&introDay, &due); err != nil {
		t.Fatal(err)
	}
	if answerDay != "2026-09-28" || logDay != "2026-09-29" || introDay != "2026-09-28" {
		t.Fatalf("answer %s log %s intro %s", answerDay, logDay, introDay)
	}
	if !due.Time.Equal(sh(2026, 9, 30, 0, 0, 0)) {
		t.Fatalf("00:01 结算的新词封顶到 09-30 00:00，得到 %s", due.Time)
	}

	// 00:05（2026-09-29）：今日队列按新学习日计算
	t3 := sh(2026, 9, 29, 0, 5, 0)
	q, err := ComputeToday(ctx, f.db, shanghai, t3, f.userID)
	if err != nil {
		t.Fatal(err)
	}
	card := q.Plans[0]
	// 00:01 结算的 2 个新词封顶到 09-30 00:00 = 今日结束，不算今日到期
	if q.Day != "2026-09-29" || card.DueCount != 10 || card.ReviewLeft != 10 || card.NewDoneToday != 0 || card.NewLeft != 3 || card.LearnedWords != 12 {
		t.Fatalf("09-29 today = %+v", card)
	}
	// 23:59:59.999 前一刻仍是 09-28
	if q0, _ := ComputeToday(ctx, f.db, shanghai, sh(2026, 9, 28, 23, 59, 59).Add(999*time.Millisecond), f.userID); q0.Day != "2026-09-28" {
		t.Fatalf("前一刻学习日 %s", q0.Day)
	}

	// 复习 10 个到期词：全对 Good，间隔拉长
	rs := f.start(t, t3, "review", planID)
	if rs.Session.WordCount != 10 {
		t.Fatalf("复习组 %d 词", rs.Session.WordCount)
	}
	f.answerAll(t, t3, rs.Session, "practice", nil)
	rr := f.complete(t, t3, rs.Session.ID)
	if rr.Settled != 10 || rr.Ratings["good"] != 10 || len(rr.Ratings) != 1 {
		t.Fatalf("复习结果 %+v", rr)
	}
	for _, it := range snapOf(t, rs.Session).Items {
		var d store.Time
		f.db.QueryRow(`SELECT "due" FROM "MemoryState" WHERE "wordId" = ?`, it.WordID).Scan(&d)
		if !d.Time.After(t3.Add(24 * time.Hour)) {
			t.Fatalf("复习后间隔应拉长：%s due %s", it.WordID, d.Time)
		}
	}
	q2, _ := ComputeToday(ctx, f.db, shanghai, t3, f.userID)
	if q2.Plans[0].ReviewLeft != 0 || q2.Plans[0].ReviewDoneToday != 10 {
		t.Fatalf("复习后 today = %+v", q2.Plans[0])
	}

	// 额度用完：再学返回 NO_DATA 以外的剩余 3 词；学完后 NO_DATA
	s4 := f.start(t, t3, "learn", planID)
	f.answerAll(t, t3, s4.Session, "practice", nil)
	f.complete(t, t3, s4.Session.ID)
	if _, err := StartSession(ctx, f.db, shanghai, t3, nil, f.userID, StartInput{Kind: "learn", PlanID: &planID}); codeOf(err) != "NO_DATA 今天的新词已经学完了" {
		t.Fatalf("err = %v", err)
	}

	// 同一学习日（09-29）做检测：所有词今天都已更新过记忆（含 00:01 结算、开组在前一天的 2 个词），
	// 检测照常出成绩但不再推动记忆（K19）
	testPlan := f.plan(t, t3, PlanInput{Kind: "test", Modes: []string{"recognition"}, TestSize: 15})
	ts := f.start(t, t3, "test", testPlan)
	f.answerAll(t, t3, ts.Session, "test", nil)
	tr := f.complete(t, t3, ts.Session.ID)
	if tr.Settled != 15 || len(tr.Ratings) != 0 || tr.CorrectFirst != 15 {
		t.Fatalf("同日检测 %+v", tr)
	}

	// 记录：连续学习天数、每日统计
	sum, err := UserSummary(ctx, f.db, shanghai, t3, f.userID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Streak != 2 || sum.LearnedWords != 15 || sum.Today.NewWords != 3 || sum.Today.ReviewedWords != 10 {
		t.Fatalf("summary = %+v", sum)
	}
	daily, err := UserDaily(ctx, f.db, shanghai, t3, f.userID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if daily[0].Day != "2026-09-28" || daily[0].NewWords != 12 || daily[1].NewWords != 3 || daily[1].ReviewedWords != 10 {
		t.Fatalf("daily = %+v", daily)
	}
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// TestConcurrentStartSession 同一学生同一计划同一类型并发开组只产生一个进行中的组（K12）。
func TestConcurrentStartSession(t *testing.T) {
	f := newLearnFixture(t, 12)
	now := sh(2026, 9, 28, 10, 0, 0)
	planID := f.plan(t, now, PlanInput{})
	const n = 8
	var wg sync.WaitGroup
	ids := make([]string, n)
	resumed := make([]bool, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res, err := StartSession(context.Background(), f.db, shanghai, now, nil, f.userID, StartInput{Kind: "learn", PlanID: &planID})
			if err != nil {
				errs[i] = err
				return
			}
			ids[i], resumed[i] = res.Session.ID, res.Resumed
		}(i)
	}
	wg.Wait()
	fresh := 0
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("第 %d 个请求出错：%v", i, errs[i])
		}
		if ids[i] != ids[0] {
			t.Fatalf("返回了不同的组：%v", ids)
		}
		if !resumed[i] {
			fresh++
		}
	}
	var active int
	f.db.QueryRow(`SELECT count(*) FROM "StudySession" WHERE "status" = 'active'`).Scan(&active)
	if fresh != 1 || active != 1 {
		t.Fatalf("新建 %d 个、进行中 %d 个，应各为 1", fresh, active)
	}
}

// TestRecordAnswerRules 作答校验、幂等（同一 sessionId+wordId+mode+phase+attempt 只记一次）与已结束的组。
func TestRecordAnswerRules(t *testing.T) {
	ctx := context.Background()
	f := newLearnFixture(t, 3)
	now := sh(2026, 9, 28, 10, 0, 0)
	planID := f.plan(t, now, PlanInput{Modes: []string{"recognition", "spelling", "cloze"}})
	s := f.start(t, now, "learn", planID)
	it := snapOf(t, s.Session).Items[0]
	if !contains(it.Modes, "cloze") || it.Cloze == nil || *it.Cloze != "I like the ____ here." {
		t.Fatalf("挖空题面 %+v", it)
	}
	rec := func(in AnswerInput) (*AnswerResult, error) {
		return RecordAnswer(ctx, f.db, shanghai, now, f.userID, s.Session.ID, in)
	}
	for _, c := range []struct {
		in   AnswerInput
		want string
	}{
		{AnswerInput{WordID: "nope", Mode: "recognition", Phase: "practice", Attempt: 1}, "VALIDATION 该词不在本组中"},
		{AnswerInput{WordID: it.WordID, Mode: "recognition", Phase: "test", Attempt: 1}, "VALIDATION 作答阶段不匹配"},
	} {
		if _, err := rec(c.in); codeOf(err) != c.want {
			t.Fatalf("got %v want %s", err, c.want)
		}
	}
	if _, err := RecordAnswer(ctx, f.db, shanghai, now, "other", s.Session.ID, AnswerInput{WordID: it.WordID}); codeOf(err) != "NOT_FOUND 学习组不存在" {
		t.Fatalf("别人的组：%v", err)
	}
	r, err := rec(AnswerInput{WordID: it.WordID, Mode: "spelling", Phase: "practice", Attempt: 1, Answer: "zzz", DurationMs: 20 * 60_000})
	if err != nil || *r.Correct || *r.Expected != it.Answer {
		t.Fatalf("r = %+v err %v", r, err)
	}
	// 重试（哪怕这次答对）按已存结果返回，不产生重复记录
	r, err = rec(AnswerInput{WordID: it.WordID, Mode: "spelling", Phase: "practice", Attempt: 1, Answer: it.Answer})
	if err != nil || *r.Correct {
		t.Fatalf("重试 r = %+v err %v", r, err)
	}
	// 「不会」：记错、不存内容
	r, _ = rec(AnswerInput{WordID: it.WordID, Mode: "recognition", Phase: "practice", Attempt: 1, Answer: it.Definition, DontKnow: true})
	if *r.Correct || *r.Expected != it.Definition {
		t.Fatalf("不会 r = %+v", r)
	}
	var n, dur int
	var ua string
	f.db.QueryRow(`SELECT count(*) FROM "Answer" WHERE "sessionId" = ?`, s.Session.ID).Scan(&n)
	f.db.QueryRow(`SELECT "durationMs" FROM "Answer" WHERE "mode" = 'spelling'`).Scan(&dur)
	f.db.QueryRow(`SELECT "userAnswer" FROM "Answer" WHERE "dontKnow" = 1`).Scan(&ua)
	if n != 2 || dur != 10*60_000 || ua != "" {
		t.Fatalf("n=%d dur=%d ua=%q", n, dur, ua)
	}
	// 已作答不能放弃
	if err := DiscardSession(ctx, f.db, f.userID, s.Session.ID); codeOf(err) != "INVALID_ACTION 已经开始作答，请点“结束本组”保存进度" {
		t.Fatalf("discard: %v", err)
	}
	f.complete(t, now, s.Session.ID)
	if _, err := rec(AnswerInput{WordID: it.WordID, Mode: "cloze", Phase: "practice", Attempt: 1, Answer: "x"}); codeOf(err) != "INVALID_STATUS 这一组已经结束" {
		t.Fatalf("结束后作答：%v", err)
	}
	// 进度：结束后静默忽略
	if err := SaveProgress(ctx, f.db, f.userID, s.Session.ID, json.RawMessage(`{"a":1}`)); err != nil {
		t.Fatal(err)
	}
}

// TestTestRetakeAndLearnedOnly 检测只更新已学词、首次交卷为正式成绩，重考不更新记忆（K10 / K20）。
func TestTestRetakeAndLearnedOnly(t *testing.T) {
	ctx := context.Background()
	f := newLearnFixture(t, 6)
	day1 := sh(2026, 9, 28, 10, 0, 0)
	daily := f.plan(t, day1, PlanInput{NewPerDay: 3})
	s := f.start(t, day1, "learn", daily)
	f.answerAll(t, day1, s.Session, "practice", nil)
	f.complete(t, day1, s.Session.ID)

	day2 := sh(2026, 9, 29, 10, 0, 0)
	testPlan := f.plan(t, day1, PlanInput{Kind: "test", Modes: []string{"recognition"}, TestSize: 6, TestScope: "all"})
	ts := f.start(t, day2, "test", testPlan)
	if ts.Session.WordCount != 6 {
		t.Fatalf("检测 %d 词", ts.Session.WordCount)
	}
	r, err := RecordAnswer(ctx, f.db, shanghai, day2, f.userID, ts.Session.ID, AnswerInput{WordID: snapOf(t, ts.Session).Items[0].WordID, Mode: "recognition", Phase: "practice", Attempt: 1})
	if codeOf(err) != "VALIDATION 作答阶段不匹配" {
		t.Fatalf("检测用 practice：%v %v", r, err)
	}
	f.answerAll(t, day2, ts.Session, "test", nil)
	res := f.complete(t, day2, ts.Session.ID)
	// 6 个词全对都结算，但只有已学的 3 个更新记忆
	if res.Settled != 6 || res.Ratings["good"] != 3 || res.NewLearned != 0 {
		t.Fatalf("检测结果 %+v", res)
	}
	day3 := sh(2026, 9, 30, 10, 0, 0)
	retake := f.start(t, day3, "test", testPlan)
	f.answerAll(t, day3, retake.Session, "test", func(string, string) bool { return true })
	res2 := f.complete(t, day3, retake.Session.ID)
	if len(res2.Ratings) != 0 || res2.CorrectFirst != 0 {
		t.Fatalf("重考不更新记忆：%+v", res2)
	}
	var logs int
	f.db.QueryRow(`SELECT count(*) FROM "ReviewLog" WHERE "sessionId" = ?`, retake.Session.ID).Scan(&logs)
	if logs != 0 {
		t.Fatalf("重考产生了 %d 条复习记录", logs)
	}
	q, _ := ComputeToday(ctx, f.db, shanghai, day3, f.userID)
	for _, c := range q.Plans {
		if c.PlanID == testPlan && (c.TestResult == nil || c.TestResult.SessionID != ts.Session.ID || c.TestResult.Correct != 6) {
			t.Fatalf("正式成绩应为首次交卷：%+v", c.TestResult)
		}
	}
}

// TestDeletePlanSettlesWithFSRS K22：删除计划前，已作答的进行中组按完整流程结算（记忆推进、completedDay 为结算日、
// 时长含全部作答），未作答的组随计划删除。
func TestDeletePlanSettlesWithFSRS(t *testing.T) {
	ctx := context.Background()
	f := newLearnFixture(t, 4)
	insertTestUser(t, f.db, "stu2", core.RoleStudent)
	start := sh(2026, 9, 28, 23, 58, 0)
	planID := f.plan(t, start, PlanInput{NewPerDay: 4})
	s := f.start(t, start, "learn", planID)
	snap := snapOf(t, s.Session)
	// 只答第一个词的两个题型 + 一次巩固
	it := snap.Items[0]
	for _, in := range []AnswerInput{
		{WordID: it.WordID, Mode: "recognition", Phase: "practice", Attempt: 1, Answer: it.Definition, DurationMs: 4000},
		{WordID: it.WordID, Mode: "spelling", Phase: "practice", Attempt: 1, Answer: "x", DurationMs: 90_000},
		{WordID: it.WordID, Mode: "spelling", Phase: "consolidate", Attempt: 1, Answer: it.Answer, DurationMs: 2000},
	} {
		if _, err := RecordAnswer(ctx, f.db, shanghai, start, f.userID, s.Session.ID, in); err != nil {
			t.Fatal(err)
		}
	}
	// 另一个未作答的进行中组（复习类不会有词，这里直接插一行）
	if _, err := f.db.Exec(`INSERT INTO "StudySession" ("id","userId","planId","kind","dayKey","snapshot") VALUES ('empty','stu2',?, 'learn','2026-09-28','{"planName":"x","modes":[],"items":[]}')`, planID); err != nil {
		t.Fatal(err)
	}

	del := sh(2026, 9, 29, 0, 2, 0) // 跨过午夜删除
	if err := f.db.Tx(ctx, func(tx *sql.Tx) error { return DeletePlan(ctx, tx, shanghai, del, planID) }); err != nil {
		t.Fatal(err)
	}
	row, err := GetSession(ctx, f.db, s.Session.ID)
	if err != nil || row == nil {
		t.Fatalf("已作答的组应保留：%v", err)
	}
	if row.Status != "completed" || row.PlanID != nil || row.CompletedDay == nil || *row.CompletedDay != "2026-09-29" ||
		!row.CompletedAt.Time.Equal(del.UTC()) {
		t.Fatalf("结算后的组 %+v", row)
	}
	var r SessionResult
	json.Unmarshal([]byte(*row.Result), &r)
	if r.Words != 4 || r.Settled != 1 || r.Unsettled != 3 || r.NewLearned != 1 || r.Ratings["again"] != 1 ||
		r.TotalFirst != 2 || r.CorrectFirst != 1 || r.DurationMs != 4000+60_000+2000 {
		t.Fatalf("result %+v", r)
	}
	var mems, logs, empty int
	var logDay string
	f.db.QueryRow(`SELECT count(*) FROM "MemoryState" WHERE "userId" = ? AND "wordId" = ?`, f.userID, it.WordID).Scan(&mems)
	f.db.QueryRow(`SELECT count(*), max("dayKey") FROM "ReviewLog" WHERE "sessionId" = ?`, s.Session.ID).Scan(&logs, &logDay)
	f.db.QueryRow(`SELECT count(*) FROM "StudySession" WHERE "id" = 'empty'`).Scan(&empty)
	if mems != 1 || logs != 1 || logDay != "2026-09-29" || empty != 0 {
		t.Fatalf("mems=%d logs=%d logDay=%s empty=%d", mems, logs, logDay, empty)
	}
	h, err := UserWordHistory(ctx, f.db, f.userID, it.WordID)
	// introducedPlanId 不是外键，计划删除后保留原值（与旧库一致）
	if err != nil || h.Memory == nil || len(h.ReviewLogs) != 1 || h.Memory.IntroducedPlanID == nil || *h.Memory.IntroducedPlanID != planID {
		t.Fatalf("单词历史 %+v err %v", h, err)
	}
}

// TestStartSessionWhenPlanGone K22：计划暂停后续做，先保存已作答部分再提示；未作答的组直接删除。
func TestStartSessionWhenPlanGone(t *testing.T) {
	ctx := context.Background()
	f := newLearnFixture(t, 4)
	now := sh(2026, 9, 28, 9, 0, 0)
	planID := f.plan(t, now, PlanInput{})
	s := f.start(t, now, "learn", planID)
	f.answerAll(t, now, s.Session, "practice", nil)
	if _, err := f.db.Exec(`UPDATE "Plan" SET "status" = 'paused' WHERE "id" = ?`, planID); err != nil {
		t.Fatal(err)
	}
	_, err := StartSession(ctx, f.db, shanghai, now, nil, f.userID, StartInput{Kind: "learn", PlanID: &planID})
	if codeOf(err) != "NOT_FOUND 计划已暂停、结束或不再安排给你，之前的作答已保存" {
		t.Fatalf("err = %v", err)
	}
	row, _ := GetSession(ctx, f.db, s.Session.ID)
	if row.Status != "completed" {
		t.Fatalf("已作答的组应被结算：%s", row.Status)
	}
	if _, err := StartSession(ctx, f.db, shanghai, now, nil, f.userID, StartInput{Kind: "learn", PlanID: &planID}); codeOf(err) != "NOT_FOUND 计划不存在、未生效或不属于你" {
		t.Fatalf("err = %v", err)
	}
}

// TestDrillDoesNotTouchMemory 错词强化：只用近期错词与遗忘词，不更新记忆。
func TestDrillDoesNotTouchMemory(t *testing.T) {
	ctx := context.Background()
	f := newLearnFixture(t, 5)
	now := sh(2026, 9, 28, 9, 0, 0)
	planID := f.plan(t, now, PlanInput{NewPerDay: 5})
	if _, err := StartSession(ctx, f.db, shanghai, now, nil, f.userID, StartInput{Kind: "drill"}); codeOf(err) != "NO_DATA 最近没有错词，继续保持！" {
		t.Fatalf("err = %v", err)
	}
	s := f.start(t, now, "learn", planID)
	wrongWord := snapOf(t, s.Session).Items[2].WordID
	f.answerAll(t, now, s.Session, "practice", func(w, _ string) bool { return w == wrongWord })
	f.complete(t, now, s.Session.ID)
	ids, err := DrillCandidates(ctx, f.db, shanghai, now, f.userID)
	if err != nil || len(ids) != 1 || ids[0] != wrongWord {
		t.Fatalf("drill = %v %v", ids, err)
	}
	var before string
	f.db.QueryRow(`SELECT group_concat("due") FROM "MemoryState"`).Scan(&before)
	d := f.start(t, now, "drill", "ignored-plan")
	if d.Session.PlanID != nil {
		t.Fatal("错词强化不属于任何计划")
	}
	f.answerAll(t, now, d.Session, "practice", nil)
	r := f.complete(t, now, d.Session.ID)
	var after string
	f.db.QueryRow(`SELECT group_concat("due") FROM "MemoryState"`).Scan(&after)
	if len(r.Ratings) != 0 || r.Settled != 1 || r.Unsettled != 0 || before != after {
		t.Fatalf("r = %+v", r)
	}
}
