package service

import (
	"context"
	"sort"
	"time"

	"github.com/vinx-lab/vinx-vocab-go/internal/core"
	"github.com/vinx-lab/vinx-vocab-go/internal/httpx"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// 班级概览（旧 services/records.ts:classOverview；GET /classes/:id/overview）。

// ClassOverviewClass 概览头部的班级信息。
type ClassOverviewClass struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	InviteCode  string `json:"inviteCode"`
	MemberCount int    `json:"memberCount"`
}

// ClassStudentToday 学生今日状态。
type ClassStudentToday struct {
	// Status no-plan | done | in-progress | not-started
	Status       string `json:"status"`
	NewDone      int    `json:"newDone"`
	NewLeft      int    `json:"newLeft"`
	ReviewDone   int    `json:"reviewDone"`
	ReviewLeft   int    `json:"reviewLeft"`
	PendingTests int    `json:"pendingTests"`
	Answers      int    `json:"answers"`
	Minutes      int    `json:"minutes"`
}

// ClassStudentRow 概览里的一个学生。
type ClassStudentRow struct {
	UserID        string            `json:"userId"`
	Name          string            `json:"name"`
	Email         string            `json:"email"`
	JoinedAt      store.Time        `json:"joinedAt"`
	Streak        int               `json:"streak"`
	LastActiveDay *string           `json:"lastActiveDay"`
	LearnedWords  int               `json:"learnedWords"`
	Today         ClassStudentToday `json:"today"`
	Accuracy7d    core.Accuracy     `json:"accuracy7d"`
	Minutes7d     int               `json:"minutes7d"`
	ActiveDays7   int               `json:"activeDays7"`
	// Coverage 目标覆盖（spec 0003）；口径见 ClassOverviewView.CoverageMode（spec 0008）。没有目标为 null。
	Coverage *ClassStudentCoverage `json:"coverage"`
	// CoverageIncludesOwn 按学生自己的目标算时，目标里含自己追加的书（界面标「含自选」）。
	CoverageIncludesOwn bool `json:"coverageIncludesOwn"`
}

// ClassOverviewSummary 全班今日汇总。
type ClassOverviewSummary struct {
	DoneToday  int           `json:"doneToday"`
	InProgress int           `json:"inProgress"`
	NotStarted int           `json:"notStarted"`
	NoPlan     int           `json:"noPlan"`
	Accuracy7d core.Accuracy `json:"accuracy7d"`
}

// ClassHardWord 全班难词。
type ClassHardWord struct {
	core.HardWord
	Spelling   string `json:"spelling"`
	Definition string `json:"definition"`
}

// ClassActiveDay 近 14 天每日活跃。
type ClassActiveDay struct {
	Day            string   `json:"day"`
	ActiveStudents int      `json:"activeStudents"`
	Answers        int      `json:"answers"`
	Accuracy       *float64 `json:"accuracy"`
}

// ClassOverviewView 班级概览。
type ClassOverviewView struct {
	Class       ClassOverviewClass   `json:"class"`
	Day         string               `json:"day"`
	Summary     ClassOverviewSummary `json:"summary"`
	Students    []ClassStudentRow    `json:"students"`
	HardWords   []ClassHardWord      `json:"hardWords"`
	ActiveByDay []ClassActiveDay     `json:"activeByDay"`
	// CoverageMode 目标覆盖的口径（spec 0008）：class = 只按本班目标（班级不允许自主），
	// student = 按每个学生自己的有效目标（含自选，班级允许自主）。
	CoverageMode string `json:"coverageMode"`
}

type userAnswerFact struct {
	userID string
	core.AnswerFact
}

// ClassOverview 班级概览：成员今日状态、近 7 天正确率 / 时长 / 活跃天数、全班难词（近 14 天，前 15 个）、近 14 天活跃。
// 进行中检测（含单词单测试）的作答不计入（notActiveTestAnswer）。班级不存在 → NOT_FOUND「班级不存在」。
func ClassOverview(ctx context.Context, q store.Querier, loc *time.Location, now time.Time, classID string) (*ClassOverviewView, error) {
	now = msTime(now)
	day := core.DayKeyOf(now, loc)
	since7 := core.AddDays(day, -6)
	since14 := core.AddDays(day, -13)

	var v ClassOverviewView
	var allowSelf bool
	err := q.QueryRowContext(ctx, `SELECT "id","name","inviteCode","allowSelfPlan" FROM "Classroom" WHERE "id" = ?`, classID).Scan(&v.Class.ID, &v.Class.Name, &v.Class.InviteCode, &allowSelf)
	if store.IsNoRows(err) {
		return nil, httpx.NotFound("班级不存在")
	}
	if err != nil {
		return nil, err
	}

	type member struct {
		id, name, email string
		joinedAt        store.Time
	}
	mrows, err := q.QueryContext(ctx, `SELECT u."id",u."name",u."email",m."joinedAt" FROM "ClassMember" m JOIN "User" u ON u."id" = m."userId"
		WHERE m."classId" = ? ORDER BY m."joinedAt" ASC, m.rowid ASC`, classID)
	if err != nil {
		return nil, err
	}
	members := []member{}
	for mrows.Next() {
		var m member
		if err := mrows.Scan(&m.id, &m.name, &m.email, &m.joinedAt); err != nil {
			mrows.Close()
			return nil, err
		}
		members = append(members, m)
	}
	mrows.Close()
	if err := mrows.Err(); err != nil {
		return nil, err
	}
	userIDs := make([]string, len(members))
	for i, m := range members {
		userIDs[i] = m.id
	}
	v.Class.MemberCount = len(userIDs)

	answers14 := []userAnswerFact{}
	learned := map[string]int{}
	daysByUser := map[string][]string{}
	if len(userIDs) > 0 {
		ph, args := store.Placeholders(len(userIDs)), store.Args(userIDs)
		arows, err := q.QueryContext(ctx, `SELECT a."userId",a."wordId",a."mode",a."phase",a."attempt",a."correct",a."durationMs",a."dayKey",a."sessionId"
			FROM "Answer" a JOIN "StudySession" s ON s."id" = a."sessionId"
			WHERE a."userId" IN (`+ph+`) AND a."dayKey" >= ? AND `+notActiveTestAnswer+` ORDER BY a.rowid`, append(args, since14)...)
		if err != nil {
			return nil, err
		}
		for arows.Next() {
			var f userAnswerFact
			if err := arows.Scan(&f.userID, &f.WordID, &f.Mode, &f.Phase, &f.Attempt, &f.Correct, &f.DurationMs, &f.DayKey, &f.SessionID); err != nil {
				arows.Close()
				return nil, err
			}
			answers14 = append(answers14, f)
		}
		arows.Close()
		if err := arows.Err(); err != nil {
			return nil, err
		}

		lrows, err := q.QueryContext(ctx, `SELECT "userId", count(*) FROM "MemoryState" WHERE "userId" IN (`+ph+`) GROUP BY "userId"`, args...)
		if err != nil {
			return nil, err
		}
		for lrows.Next() {
			var id string
			var n int
			if err := lrows.Scan(&id, &n); err != nil {
				lrows.Close()
				return nil, err
			}
			learned[id] = n
		}
		lrows.Close()
		if err := lrows.Err(); err != nil {
			return nil, err
		}

		drows, err := q.QueryContext(ctx, `SELECT DISTINCT "userId","completedDay" FROM "StudySession"
			WHERE "userId" IN (`+ph+`) AND "status" = 'completed' AND "completedDay" IS NOT NULL`, args...)
		if err != nil {
			return nil, err
		}
		for drows.Next() {
			var id, d string
			if err := drows.Scan(&id, &d); err != nil {
				drows.Close()
				return nil, err
			}
			daysByUser[id] = append(daysByUser[id], d)
		}
		drows.Close()
		if err := drows.Err(); err != nil {
			return nil, err
		}
	}

	queues, err := ComputeTodayBatch(ctx, q, loc, now, userIDs)
	if err != nil {
		return nil, err
	}
	v.CoverageMode = CoverageModeStudent
	if !allowSelf {
		v.CoverageMode = CoverageModeClass
	}
	coverage, includesOwn, err := classCoverage(ctx, q, loc, now, classID, v.CoverageMode, userIDs)
	if err != nil {
		return nil, err
	}

	v.Students = make([]ClassStudentRow, 0, len(members))
	for _, m := range members {
		var mine7, todayAnswers []core.AnswerFact
		for _, a := range answers14 {
			if a.userID != m.id {
				continue
			}
			if a.DayKey >= since7 {
				mine7 = append(mine7, a.AnswerFact)
			}
			if a.DayKey == day {
				todayAnswers = append(todayAnswers, a.AnswerFact)
			}
		}
		days := daysByUser[m.id]
		queue := queues[m.id]
		hasWork, doneToday := false, len(queue.Plans) > 0
		for _, p := range queue.Plans {
			if p.Kind == "daily" || p.TestResult == nil {
				hasWork = true
			}
			if !p.DoneToday {
				doneToday = false
			}
		}
		status := "done"
		switch {
		case len(queue.Plans) == 0:
			status = "no-plan"
		case doneToday:
			status = "done"
		case len(todayAnswers) > 0:
			status = "in-progress"
		case hasWork:
			status = "not-started"
		}
		var last *string
		activeDays7 := 0
		if len(days) > 0 {
			sorted := append([]string(nil), days...)
			sort.Strings(sorted)
			l := sorted[len(sorted)-1]
			last = &l
		}
		for _, d := range days {
			if d >= since7 {
				activeDays7++
			}
		}
		v.Students = append(v.Students, ClassStudentRow{
			UserID: m.id, Name: m.name, Email: m.email, JoinedAt: m.joinedAt,
			Streak: core.ComputeStreak(days, day), LastActiveDay: last, LearnedWords: learned[m.id],
			Today: ClassStudentToday{
				Status: status, NewDone: queue.Totals.NewDoneToday, NewLeft: queue.Totals.NewLeft,
				ReviewDone: queue.Totals.ReviewDoneToday, ReviewLeft: queue.Totals.ReviewLeft, PendingTests: queue.Totals.PendingTests,
				Answers: len(todayAnswers), Minutes: core.ActiveMinutes(todayAnswers),
			},
			Accuracy7d: core.AccuracyOf(mine7), Minutes7d: core.ActiveMinutes(mine7), ActiveDays7: activeDays7,
			Coverage: coverage[m.id], CoverageIncludesOwn: includesOwn[m.id],
		})
	}

	all14 := make([]core.AnswerFact, len(answers14))
	all7 := []core.AnswerFact{}
	for i, a := range answers14 {
		all14[i] = a.AnswerFact
		if a.DayKey >= since7 {
			all7 = append(all7, a.AnswerFact)
		}
	}
	for _, s := range v.Students {
		switch s.Today.Status {
		case "done":
			v.Summary.DoneToday++
		case "in-progress":
			v.Summary.InProgress++
		case "not-started":
			v.Summary.NotStarted++
		case "no-plan":
			v.Summary.NoPlan++
		}
	}
	v.Summary.Accuracy7d = core.AccuracyOf(all7)
	v.Day = day

	hard := core.HardWords(all14, 15)
	words := map[string][2]string{}
	if len(hard) > 0 {
		ids := make([]string, len(hard))
		for i, h := range hard {
			ids[i] = h.WordID
		}
		wrows, err := q.QueryContext(ctx, `SELECT "id","spelling","definition" FROM "Word" WHERE "id" IN (`+store.Placeholders(len(ids))+`)`, store.Args(ids)...)
		if err != nil {
			return nil, err
		}
		for wrows.Next() {
			var id, sp, def string
			if err := wrows.Scan(&id, &sp, &def); err != nil {
				wrows.Close()
				return nil, err
			}
			words[id] = [2]string{sp, def}
		}
		wrows.Close()
		if err := wrows.Err(); err != nil {
			return nil, err
		}
	}
	v.HardWords = make([]ClassHardWord, 0, len(hard))
	for _, h := range hard {
		w := words[h.WordID]
		v.HardWords = append(v.HardWords, ClassHardWord{HardWord: h, Spelling: w[0], Definition: w[1]})
	}

	series := core.DailySeries(day, 14, all14, nil, nil)
	v.ActiveByDay = make([]ClassActiveDay, 0, len(series))
	for _, p := range series {
		active := map[string]bool{}
		for _, a := range answers14 {
			if a.DayKey == p.Day {
				active[a.userID] = true
			}
		}
		var acc *float64
		if p.FirstAttempts > 0 {
			r := float64(p.Correct) / float64(p.FirstAttempts)
			acc = &r
		}
		v.ActiveByDay = append(v.ActiveByDay, ClassActiveDay{Day: p.Day, ActiveStudents: len(active), Answers: p.Answers, Accuracy: acc})
	}
	return &v, nil
}
