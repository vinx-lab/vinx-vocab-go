package service

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/vinx-lab/vinx-vocab-go/internal/core"
	"github.com/vinx-lab/vinx-vocab-go/internal/httpx"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// 学习记录与统计装配（对应旧 services/records.ts；口径见 core/stats.go 与 docs/product.md §6）。
// 进行中检测（含单词单测试）的作答不进入任何统计，避免交卷前从正确率变化推断对错（notActiveTestAnswer）。

// answerFacts 该学生 dayKey >= since 的作答事实（排除进行中检测）。
func answerFacts(ctx context.Context, q store.Querier, userID, since string) ([]core.AnswerFact, error) {
	rows, err := q.QueryContext(ctx, `SELECT a."wordId",a."mode",a."phase",a."attempt",a."correct",a."durationMs",a."dayKey",a."sessionId"
		FROM "Answer" a JOIN "StudySession" s ON s."id" = a."sessionId"
		WHERE a."userId" = ? AND a."dayKey" >= ? AND `+notActiveTestAnswer+` ORDER BY a.rowid`, userID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []core.AnswerFact{}
	for rows.Next() {
		var a core.AnswerFact
		if err := rows.Scan(&a.WordID, &a.Mode, &a.Phase, &a.Attempt, &a.Correct, &a.DurationMs, &a.DayKey, &a.SessionID); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ActiveDays 有完成组的学习日（去重）；since 非空时只取 >= since。
func ActiveDays(ctx context.Context, q store.Querier, userID, since string) ([]string, error) {
	query := `SELECT DISTINCT "completedDay" FROM "StudySession" WHERE "userId" = ? AND "status" = 'completed' AND "completedDay" IS NOT NULL`
	args := []any{userID}
	if since != "" {
		query += ` AND "completedDay" >= ?`
		args = append(args, since)
	}
	return queryStrings(ctx, q, query, args...)
}

// TodayStats /records/summary 的 today 字段（/today 的 stats）。
type TodayStats struct {
	NewWords      int `json:"newWords"`
	ReviewedWords int `json:"reviewedWords"`
	Answers       int `json:"answers"`
	Minutes       int `json:"minutes"`
	NewLeft       int `json:"newLeft"`
	ReviewLeft    int `json:"reviewLeft"`
	PendingTests  int `json:"pendingTests"`
}

// UserSummaryView 旧 userSummary 的返回值。
type UserSummaryView struct {
	Day           string           `json:"day"`
	Streak        int              `json:"streak"`
	ActiveDays30  int              `json:"activeDays30"`
	LastActiveDay *string          `json:"lastActiveDay"`
	LearnedWords  int              `json:"learnedWords"`
	Mastery       core.MasteryDist `json:"mastery"`
	DueToday      int              `json:"dueToday"`
	Today         TodayStats       `json:"today"`
	Accuracy7d    core.Accuracy    `json:"accuracy7d"`
	Accuracy30d   core.Accuracy    `json:"accuracy30d"`
	Minutes7d     int              `json:"minutes7d"`
}

// UserSummary 学习概况（旧 userSummary）；queue 为 nil 时现算今日队列。
func UserSummary(ctx context.Context, q store.Querier, loc *time.Location, now time.Time, userID string, queue *core.TodaySummary) (*UserSummaryView, error) {
	now = msTime(now)
	day, _, dayEnd := TodayRange(loc, now)
	since30 := core.AddDays(day, -29)
	since7 := core.AddDays(day, -6)

	rows, err := q.QueryContext(ctx, `SELECT "stability","due","introducedDay" FROM "MemoryState" WHERE "userId" = ?`, userID)
	if err != nil {
		return nil, err
	}
	stabilities := []float64{}
	dueToday, newToday := 0, 0
	for rows.Next() {
		var st float64
		var due store.Time
		var intro string
		if err := rows.Scan(&st, &due, &intro); err != nil {
			rows.Close()
			return nil, err
		}
		stabilities = append(stabilities, st)
		if due.Time.Before(dayEnd) {
			dueToday++
		}
		if intro == day {
			newToday++
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	answers30, err := answerFacts(ctx, q, userID, since30)
	if err != nil {
		return nil, err
	}
	stages, err := userWordStages(ctx, q, loc, now, userID)
	if err != nil {
		return nil, err
	}
	days, err := ActiveDays(ctx, q, userID, "")
	if err != nil {
		return nil, err
	}
	if queue == nil {
		t, err := ComputeToday(ctx, q, loc, now, userID)
		if err != nil {
			return nil, err
		}
		queue = &t
	}
	answers7, answersToday := []core.AnswerFact{}, []core.AnswerFact{}
	for _, a := range answers30 {
		if a.DayKey >= since7 {
			answers7 = append(answers7, a)
		}
		if a.DayKey == day {
			answersToday = append(answersToday, a)
		}
	}
	var reviewed int
	if err := q.QueryRowContext(ctx, `SELECT count(DISTINCT "wordId") FROM "ReviewLog" WHERE "userId" = ? AND "dayKey" = ? AND "stateBefore" <> 0`, userID, day).Scan(&reviewed); err != nil {
		return nil, err
	}
	activeDays30 := 0
	for _, d := range days {
		if d >= since30 {
			activeDays30++
		}
	}
	var last *string
	if len(days) > 0 {
		sorted := append([]string(nil), days...)
		sort.Strings(sorted)
		l := sorted[len(sorted)-1]
		last = &l
	}
	return &UserSummaryView{
		Day: day, Streak: core.ComputeStreak(days, day), ActiveDays30: activeDays30, LastActiveDay: last,
		LearnedWords: len(stabilities), Mastery: core.StageDistribution(stages), DueToday: dueToday,
		Today: TodayStats{
			NewWords: newToday, ReviewedWords: reviewed, Answers: len(answersToday), Minutes: core.ActiveMinutes(answersToday),
			NewLeft: queue.Totals.NewLeft, ReviewLeft: queue.Totals.ReviewLeft, PendingTests: queue.Totals.PendingTests,
		},
		Accuracy7d: core.AccuracyOf(answers7), Accuracy30d: core.AccuracyOf(answers30), Minutes7d: core.ActiveMinutes(answers7),
	}, nil
}

// UserDaily 最近 days 天的每日统计（旧 userDaily）。
func UserDaily(ctx context.Context, q store.Querier, loc *time.Location, now time.Time, userID string, days int) ([]core.DailyPoint, error) {
	day, _, _ := TodayRange(loc, msTime(now))
	since := core.AddDays(day, -(days - 1))
	answers, err := answerFacts(ctx, q, userID, since)
	if err != nil {
		return nil, err
	}
	introduced, err := queryStrings(ctx, q, `SELECT "introducedDay" FROM "MemoryState" WHERE "userId" = ? AND "introducedDay" >= ?`, userID, since)
	if err != nil {
		return nil, err
	}
	reviews, err := queryStrings(ctx, q, `SELECT "dayKey" FROM "ReviewLog" WHERE "userId" = ? AND "dayKey" >= ? AND "stateBefore" <> 0`, userID, since)
	if err != nil {
		return nil, err
	}
	return core.DailySeries(day, days, answers, introduced, reviews), nil
}

// SessionListItem /records/sessions 的一项。
type SessionListItem struct {
	ID          string          `json:"id"`
	Kind        string          `json:"kind"`
	Status      string          `json:"status"`
	PlanID      *string         `json:"planId"`
	PlanName    *string         `json:"planName"`
	Words       int             `json:"words"`
	Answers     int             `json:"answers"`
	Result      json.RawMessage `json:"result"`
	DayKey      string          `json:"dayKey"`
	StartedAt   store.Time      `json:"startedAt"`
	CompletedAt store.NullTime  `json:"completedAt"`
	// 默写单批改组才有（spec 0006）：格式与自批标记。
	Format     *string `json:"format,omitempty"`
	SelfGraded *bool   `json:"selfGraded,omitempty"`
}

func rawOrNull(s *string) json.RawMessage {
	if s == nil {
		return json.RawMessage("null")
	}
	return json.RawMessage(*s)
}

// UserSessions 学习组列表（旧 userSessions），按开始时间倒序。kind 为空不过滤。
func UserSessions(ctx context.Context, q store.Querier, userID string, page, limit int, kind string) (httpx.Paginated[SessionListItem], error) {
	where := `"userId" = ?`
	args := []any{userID}
	if kind != "" {
		where += ` AND "kind" = ?`
		args = append(args, kind)
	}
	var total int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM "StudySession" WHERE `+where, args...).Scan(&total); err != nil {
		return httpx.Paginated[SessionListItem]{}, err
	}
	rows, err := q.QueryContext(ctx, `SELECT `+sessionCols+`, (SELECT count(*) FROM "Answer" a WHERE a."sessionId" = "StudySession"."id")
		FROM "StudySession" WHERE `+where+` ORDER BY "startedAt" DESC, rowid DESC LIMIT ? OFFSET ?`, append(args, limit, (page-1)*limit)...)
	if err != nil {
		return httpx.Paginated[SessionListItem]{}, err
	}
	defer rows.Close()
	items := []SessionListItem{}
	for rows.Next() {
		var s SessionRow
		var n int
		if err := rows.Scan(&s.ID, &s.UserID, &s.PlanID, &s.SheetID, &s.Kind, &s.WordCount, &s.Status, &s.DayKey, &s.SnapshotText, &s.Progress, &s.Result, &s.StartedAt, &s.CompletedAt, &s.CompletedDay, &n); err != nil {
			return httpx.Paginated[SessionListItem]{}, err
		}
		snap, err := ParseSnapshot(s.SnapshotText)
		if err != nil {
			return httpx.Paginated[SessionListItem]{}, err
		}
		items = append(items, SessionListItem{
			ID: s.ID, Kind: s.Kind, Status: s.Status, PlanID: s.PlanID, PlanName: snap.PlanName, Words: len(snap.Items),
			Answers: n, Result: rawOrNull(s.Result), DayKey: s.DayKey, StartedAt: s.StartedAt, CompletedAt: s.CompletedAt,
			Format: snap.Format, SelfGraded: snap.SelfGraded,
		})
	}
	if err := rows.Err(); err != nil {
		return httpx.Paginated[SessionListItem]{}, err
	}
	return httpx.Page(items, total, page, limit), nil
}

// SessionAnswer 学习组里的一次作答（Answer 全部列）。
type SessionAnswer struct {
	ID         string     `json:"id"`
	SessionID  string     `json:"sessionId"`
	UserID     string     `json:"userId"`
	WordID     string     `json:"wordId"`
	Mode       string     `json:"mode"`
	Phase      string     `json:"phase"`
	Attempt    int        `json:"attempt"`
	Correct    bool       `json:"correct"`
	UserAnswer *string    `json:"userAnswer"`
	HintUsed   bool       `json:"hintUsed"`
	DontKnow   bool       `json:"dontKnow"`
	DurationMs int        `json:"durationMs"`
	DayKey     string     `json:"dayKey"`
	CreatedAt  store.Time `json:"createdAt"`
}

// SessionAnswers 某组的全部作答（按作答时间）。
func SessionAnswers(ctx context.Context, q store.Querier, sessionID string) ([]SessionAnswer, error) {
	rows, err := q.QueryContext(ctx, `SELECT "id","sessionId","userId","wordId","mode","phase","attempt","correct","userAnswer","hintUsed","dontKnow","durationMs","dayKey","createdAt"
		FROM "Answer" WHERE "sessionId" = ? ORDER BY "createdAt", rowid`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SessionAnswer{}
	for rows.Next() {
		var a SessionAnswer
		if err := rows.Scan(&a.ID, &a.SessionID, &a.UserID, &a.WordID, &a.Mode, &a.Phase, &a.Attempt, &a.Correct, &a.UserAnswer, &a.HintUsed, &a.DontKnow, &a.DurationMs, &a.DayKey, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ReviewBrief 学习组详情里每个词的复习记录摘要。
type ReviewBrief struct {
	WordID         string     `json:"wordId"`
	Rating         int        `json:"rating"`
	DueAfter       store.Time `json:"dueAfter"`
	StabilityAfter float64    `json:"stabilityAfter"`
}

// SessionDetailView /records/sessions/:id。
type SessionDetailView struct {
	ID          string              `json:"id"`
	User        SessionDetailUser   `json:"user"`
	Kind        string              `json:"kind"`
	Status      string              `json:"status"`
	PlanID      *string             `json:"planId"`
	PlanName    *string             `json:"planName"`
	Modes       []string            `json:"modes"`
	Result      json.RawMessage     `json:"result"`
	StartedAt   store.Time          `json:"startedAt"`
	CompletedAt store.NullTime      `json:"completedAt"`
	Words       []SessionDetailWord `json:"words"`
	// 默写单批改组才有（spec 0006）：格式、批改人、自批标记、句子题的对错。
	Format     *string                 `json:"format,omitempty"`
	GradedBy   *SnapshotUser           `json:"gradedBy,omitempty"`
	SelfGraded *bool                   `json:"selfGraded,omitempty"`
	Sentences  []SessionDetailSentence `json:"sentences,omitempty"`
}

// SessionDetailSentence 默写单批改组里的一道句子题。
type SessionDetailSentence struct {
	Index      int     `json:"index"`
	SentenceID string  `json:"sentenceId"`
	Type       string  `json:"type"`
	En         string  `json:"en"`
	Cn         string  `json:"cn"`
	Prompt     string  `json:"prompt"`
	Correct    bool    `json:"correct"`
	UserAnswer *string `json:"userAnswer"`
}

// SessionDetailUser 组主人。
type SessionDetailUser struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// SessionDetailWord 详情里的一个词。
type SessionDetailWord struct {
	WordID       string                `json:"wordId"`
	Spelling     string                `json:"spelling"`
	Definition   string                `json:"definition"`
	Phonetic     *string               `json:"phonetic"`
	PartOfSpeech *string               `json:"partOfSpeech"`
	Answers      []SessionDetailAnswer `json:"answers"`
	Review       *ReviewBrief          `json:"review"`
}

// SessionDetailAnswer 详情里的一次作答。
type SessionDetailAnswer struct {
	Mode       string  `json:"mode"`
	Phase      string  `json:"phase"`
	Attempt    int     `json:"attempt"`
	Correct    bool    `json:"correct"`
	UserAnswer *string `json:"userAnswer"`
	HintUsed   bool    `json:"hintUsed"`
	DontKnow   bool    `json:"dontKnow"`
	DurationMs int     `json:"durationMs"`
}

// SessionRecord 学习组详情（旧 GET /records/sessions/:id）：组不存在 → 404；无权查看 → 403；进行中的检测 → 400。
func SessionRecord(ctx context.Context, q store.Querier, a *Actor, sessionID string) (*SessionDetailView, error) {
	s, err := GetSession(ctx, q, sessionID)
	if err != nil {
		return nil, err
	}
	if s == nil {
		return nil, httpx.NotFound("学习组不存在")
	}
	if err := AssertCanViewUser(ctx, q, a, s.UserID); err != nil {
		return nil, err
	}
	if core.IsTestKind(s.Kind) && s.Status == "active" {
		return nil, httpx.NewError(httpx.CodeInvalidStatus, "检测进行中，交卷后才能查看详情", nil)
	}
	var user SessionDetailUser
	if err := q.QueryRowContext(ctx, `SELECT "id","name" FROM "User" WHERE "id" = ?`, s.UserID).Scan(&user.ID, &user.Name); err != nil {
		return nil, err
	}
	answers, err := SessionAnswers(ctx, q, sessionID)
	if err != nil {
		return nil, err
	}
	rows, err := q.QueryContext(ctx, `SELECT "wordId","rating","dueAfter","stabilityAfter" FROM "ReviewLog" WHERE "sessionId" = ?`, sessionID)
	if err != nil {
		return nil, err
	}
	logs := map[string]*ReviewBrief{}
	for rows.Next() {
		var r ReviewBrief
		if err := rows.Scan(&r.WordID, &r.Rating, &r.DueAfter, &r.StabilityAfter); err != nil {
			rows.Close()
			return nil, err
		}
		logs[r.WordID] = &r
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	snap, err := ParseSnapshot(s.SnapshotText)
	if err != nil {
		return nil, err
	}
	words := make([]SessionDetailWord, 0, len(snap.Items))
	for _, it := range snap.Items {
		as := []SessionDetailAnswer{}
		for _, x := range answers {
			if x.WordID == it.WordID {
				as = append(as, SessionDetailAnswer{Mode: x.Mode, Phase: x.Phase, Attempt: x.Attempt, Correct: x.Correct,
					UserAnswer: x.UserAnswer, HintUsed: x.HintUsed, DontKnow: x.DontKnow, DurationMs: x.DurationMs})
			}
		}
		words = append(words, SessionDetailWord{WordID: it.WordID, Spelling: it.Spelling, Definition: it.Definition,
			Phonetic: it.Phonetic, PartOfSpeech: it.PartOfSpeech, Answers: as, Review: logs[it.WordID]})
	}
	view := &SessionDetailView{
		ID: s.ID, User: user, Kind: s.Kind, Status: s.Status, PlanID: s.PlanID, PlanName: snap.PlanName, Modes: snap.Modes,
		Result: rawOrNull(s.Result), StartedAt: s.StartedAt, CompletedAt: s.CompletedAt, Words: words,
		Format: snap.Format, GradedBy: snap.GradedBy, SelfGraded: snap.SelfGraded,
	}
	if len(snap.Sentences) > 0 {
		if view.Sentences, err = sessionSentenceDetails(ctx, q, s.ID, snap.Sentences); err != nil {
			return nil, err
		}
	}
	return view, nil
}

// WordListItem /records/words 的一项。
type WordListItem struct {
	WordID       string     `json:"wordId"`
	Spelling     string     `json:"spelling"`
	Phonetic     *string    `json:"phonetic"`
	PartOfSpeech *string    `json:"partOfSpeech"`
	Definition   string     `json:"definition"`
	Due          store.Time `json:"due"`
	IsDue        bool       `json:"isDue"`
	Stability    float64    `json:"stability"`
	Difficulty   float64    `json:"difficulty"`
	Level        string     `json:"level"`
	LevelLabel   string     `json:"levelLabel"`
	// Forgetting 可能忘了（spec 0009）：已到期且回忆概率低于 70%。
	Forgetting    bool           `json:"forgetting"`
	Reps          int            `json:"reps"`
	Lapses        int            `json:"lapses"`
	LastReview    store.NullTime `json:"lastReview"`
	IntroducedDay string         `json:"introducedDay"`
}

// WordFilters /records/words 的 filter 取值（spec 0009 新增 missed：最近一次答错）。
var WordFilters = []string{"all", "due", "difficult", "mastered", "consolidating", "learning", "missed"}

// userWordStages 一个用户有记忆的词的状态（spec 0009）。
func userWordStages(ctx context.Context, q store.Querier, loc *time.Location, now time.Time, userID string) (map[string]core.WordStageInfo, error) {
	ev, err := loadEvidence(ctx, q, []string{userID})
	if err != nil {
		return nil, err
	}
	mem, err := loadStageMemories(ctx, q, []string{userID})
	if err != nil {
		return nil, err
	}
	now = msTime(now)
	_, _, dayEnd := TodayRange(loc, now)
	out := map[string]core.WordStageInfo{}
	for w, m := range mem[userID] {
		out[w] = core.WordStage(ev[userID][w], m, now, dayEnd)
	}
	return out, nil
}

// UserWords 学过的词（旧 userWords）：按筛选与关键词分页。分层筛选按词状态（spec 0009），在内存里过滤后分页。
func UserWords(ctx context.Context, q store.Querier, loc *time.Location, now time.Time, userID, filter, search string, page, limit int) (httpx.Paginated[WordListItem], error) {
	_, _, dayEnd := TodayRange(loc, msTime(now))
	where := `m."userId" = ?`
	args := []any{userID}
	if search != "" {
		where += ` AND (VINX_ICONTAINS(w."spelling", ?) = 1 OR instr(w."definition", ?) > 0)`
		args = append(args, search, search)
	}
	switch filter {
	case "due":
		where += ` AND m."due" < ?`
		args = append(args, dayEnd)
	case "difficult":
		where += ` AND (m."lapses" >= 2 OR m."difficulty" >= 7)`
	}
	// 同分决胜：旧版只按主排序键，PG 按 (userId, wordId) 唯一索引取行后排序，小批量同分时即 wordId 升序；
	// 大批量同分时 PG 的次序由排序算法决定（未定义），无法复刻（见 A8 迁移演练）。
	order := `m."introducedAt" DESC, m."wordId"`
	switch filter {
	case "difficult":
		order = `m."lapses" DESC, m."difficulty" DESC, m."wordId"`
	case "due":
		order = `m."due" ASC, m."wordId"`
	}
	stages, err := userWordStages(ctx, q, loc, now, userID)
	if err != nil {
		return httpx.Paginated[WordListItem]{}, err
	}
	rows, err := q.QueryContext(ctx, `SELECT m."wordId", w."spelling", w."phonetic", w."partOfSpeech", w."definition", m."due", m."stability", m."difficulty",
		m."reps", m."lapses", m."lastReview", m."introducedDay" FROM "MemoryState" m JOIN "Word" w ON w."id" = m."wordId" WHERE `+where+` ORDER BY `+order, args...)
	if err != nil {
		return httpx.Paginated[WordListItem]{}, err
	}
	defer rows.Close()
	all := []WordListItem{}
	for rows.Next() {
		var it WordListItem
		if err := rows.Scan(&it.WordID, &it.Spelling, &it.Phonetic, &it.PartOfSpeech, &it.Definition, &it.Due, &it.Stability, &it.Difficulty,
			&it.Reps, &it.Lapses, &it.LastReview, &it.IntroducedDay); err != nil {
			return httpx.Paginated[WordListItem]{}, err
		}
		st := stages[it.WordID]
		it.Level = core.StageLevel(st.Stage)
		if it.Level == "" || it.Level == core.StageUntested {
			it.Level = core.MasteryLevel(it.Stability)
		}
		switch filter {
		case "mastered", "consolidating", "learning", "missed":
			if it.Level != filter {
				continue
			}
		}
		it.IsDue = it.Due.Time.Before(dayEnd)
		it.Forgetting = st.Forgetting
		it.LevelLabel = core.MasteryLabel[it.Level]
		it.Stability = core.Round1(it.Stability)
		it.Difficulty = core.Round1(it.Difficulty)
		all = append(all, it)
	}
	if err := rows.Err(); err != nil {
		return httpx.Paginated[WordListItem]{}, err
	}
	from := min((page-1)*limit, len(all))
	to := min(from+limit, len(all))
	return httpx.Page(all[from:to], len(all), page, limit), nil
}

// MemoryView MemoryState 全部列 + 分层（/records/words/:wordId 的 memory）。
type MemoryView struct {
	ID               string         `json:"id"`
	UserID           string         `json:"userId"`
	WordID           string         `json:"wordId"`
	Due              store.Time     `json:"due"`
	Stability        float64        `json:"stability"`
	Difficulty       float64        `json:"difficulty"`
	ElapsedDays      int            `json:"elapsedDays"`
	ScheduledDays    int            `json:"scheduledDays"`
	LearningSteps    int            `json:"learningSteps"`
	Reps             int            `json:"reps"`
	Lapses           int            `json:"lapses"`
	State            int            `json:"state"`
	LastReview       store.NullTime `json:"lastReview"`
	IntroducedAt     store.Time     `json:"introducedAt"`
	IntroducedPlanID *string        `json:"introducedPlanId"`
	IntroducedDay    string         `json:"introducedDay"`
	UpdatedAt        store.Time     `json:"updatedAt"`
	Level            string         `json:"level"`
	LevelLabel       string         `json:"levelLabel"`
}

// ReviewLogRow ReviewLog 全部列。
type ReviewLogRow struct {
	ID              string     `json:"id"`
	UserID          string     `json:"userId"`
	WordID          string     `json:"wordId"`
	SessionID       *string    `json:"sessionId"`
	Rating          int        `json:"rating"`
	StateBefore     int        `json:"stateBefore"`
	StabilityAfter  float64    `json:"stabilityAfter"`
	DifficultyAfter float64    `json:"difficultyAfter"`
	DueAfter        store.Time `json:"dueAfter"`
	ReviewedAt      store.Time `json:"reviewedAt"`
	DayKey          string     `json:"dayKey"`
	// Source 来源：null 为线上结算，"backfill-0009" 为历史补算（spec 0009）。
	Source *string `json:"source"`
}

// WordHistoryAnswer 单词历史里的一次作答。
type WordHistoryAnswer struct {
	ID         string     `json:"id"`
	SessionID  string     `json:"sessionId"`
	Mode       string     `json:"mode"`
	Phase      string     `json:"phase"`
	Attempt    int        `json:"attempt"`
	Correct    bool       `json:"correct"`
	UserAnswer *string    `json:"userAnswer"`
	HintUsed   bool       `json:"hintUsed"`
	DontKnow   bool       `json:"dontKnow"`
	DayKey     string     `json:"dayKey"`
	CreatedAt  store.Time `json:"createdAt"`
}

// WordHistoryView /records/words/:wordId。
type WordHistoryView struct {
	Word       *WordRow            `json:"word"`
	Memory     *MemoryView         `json:"memory"`
	ReviewLogs []ReviewLogRow      `json:"reviewLogs"`
	Answers    []WordHistoryAnswer `json:"answers"`
}

// UserWordHistory 单词学习历史（旧 userWordHistory）。
func UserWordHistory(ctx context.Context, q store.Querier, loc *time.Location, now time.Time, userID, wordID string) (*WordHistoryView, error) {
	word, err := GetWordRow(ctx, q, wordID)
	if err != nil {
		return nil, err
	}
	var memory *MemoryView
	var m MemoryView
	err = q.QueryRowContext(ctx, `SELECT "id","userId","wordId","due","stability","difficulty","elapsedDays","scheduledDays","learningSteps","reps","lapses","state","lastReview","introducedAt","introducedPlanId","introducedDay","updatedAt"
		FROM "MemoryState" WHERE "userId" = ? AND "wordId" = ?`, userID, wordID).
		Scan(&m.ID, &m.UserID, &m.WordID, &m.Due, &m.Stability, &m.Difficulty, &m.ElapsedDays, &m.ScheduledDays, &m.LearningSteps, &m.Reps, &m.Lapses, &m.State, &m.LastReview, &m.IntroducedAt, &m.IntroducedPlanID, &m.IntroducedDay, &m.UpdatedAt)
	if err == nil {
		stages, err := userWordStages(ctx, q, loc, now, userID)
		if err != nil {
			return nil, err
		}
		m.Level = core.StageLevel(stages[wordID].Stage)
		if m.Level == "" || m.Level == core.StageUntested {
			m.Level = core.MasteryLevel(m.Stability)
		}
		m.LevelLabel = core.MasteryLabel[m.Level]
		m.Stability = core.Round1(m.Stability)
		m.Difficulty = core.Round1(m.Difficulty)
		memory = &m
	} else if !store.IsNoRows(err) {
		return nil, err
	}
	rows, err := q.QueryContext(ctx, `SELECT "id","userId","wordId","sessionId","rating","stateBefore","stabilityAfter","difficultyAfter","dueAfter","reviewedAt","dayKey","source"
		FROM "ReviewLog" WHERE "userId" = ? AND "wordId" = ? ORDER BY "reviewedAt", rowid`, userID, wordID)
	if err != nil {
		return nil, err
	}
	logs := []ReviewLogRow{}
	for rows.Next() {
		var r ReviewLogRow
		if err := rows.Scan(&r.ID, &r.UserID, &r.WordID, &r.SessionID, &r.Rating, &r.StateBefore, &r.StabilityAfter, &r.DifficultyAfter, &r.DueAfter, &r.ReviewedAt, &r.DayKey, &r.Source); err != nil {
			rows.Close()
			return nil, err
		}
		logs = append(logs, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	arows, err := q.QueryContext(ctx, `SELECT a."id",a."sessionId",a."mode",a."phase",a."attempt",a."correct",a."userAnswer",a."hintUsed",a."dontKnow",a."dayKey",a."createdAt"
		FROM "Answer" a JOIN "StudySession" s ON s."id" = a."sessionId"
		WHERE a."userId" = ? AND a."wordId" = ? AND `+notActiveTestAnswer+` ORDER BY a."createdAt", a.rowid`, userID, wordID)
	if err != nil {
		return nil, err
	}
	answers := []WordHistoryAnswer{}
	for arows.Next() {
		var a WordHistoryAnswer
		if err := arows.Scan(&a.ID, &a.SessionID, &a.Mode, &a.Phase, &a.Attempt, &a.Correct, &a.UserAnswer, &a.HintUsed, &a.DontKnow, &a.DayKey, &a.CreatedAt); err != nil {
			arows.Close()
			return nil, err
		}
		answers = append(answers, a)
	}
	arows.Close()
	if err := arows.Err(); err != nil {
		return nil, err
	}
	if word == nil {
		return nil, httpx.NotFound("单词不存在")
	}
	return &WordHistoryView{Word: word, Memory: memory, ReviewLogs: logs, Answers: answers}, nil
}

// NextSheetView 今日页显示的下一份单词单（旧 NextSheet）。
type NextSheetView struct {
	ID              string  `json:"id"`
	Seq             int     `json:"seq"`
	WordCount       int     `json:"wordCount"`
	ActiveSessionID *string `json:"activeSessionId"`
	Remaining       int     `json:"remaining"`
	// Format selftest | dictation（spec 0006：默写单在今日页显示「待批改」）；ItemCount 默写单的题数（自测表 = 词数）。
	Format    string `json:"format"`
	ItemCount int    `json:"itemCount"`
}

// NextSheet 旧 services/sheets.ts nextSheet + lib/sheet-batch.ts pickNextSheet：
// 有进行中测试（含已测单子的重测，K30 同一时间只能测一份）的优先，否则取编号最小的未测单子；remaining 数其余未测的。
func NextSheet(ctx context.Context, q store.Querier, userID string) (*NextSheetView, error) {
	rows, err := q.QueryContext(ctx, `SELECT w."id", w."seq", w."wordIds",
			EXISTS (SELECT 1 FROM "StudySession" s WHERE s."sheetId" = w."id" AND s."status" = 'completed'),
			(SELECT s."id" FROM "StudySession" s WHERE s."sheetId" = w."id" AND s."status" = 'active' LIMIT 1),
			w."format", w."items"
		FROM "WordSheet" w WHERE w."userId" = ?`, userID)
	if err != nil {
		return nil, err
	}
	type row struct {
		id       string
		seq      int
		words    int
		tested   bool
		activeID *string
		format   string
		items    int
	}
	list := []row{}
	for rows.Next() {
		var r row
		var ids store.JSON[[]string]
		var items store.JSON[[]core.DictationItem]
		if err := rows.Scan(&r.id, &r.seq, &ids, &r.tested, &r.activeID, &r.format, &items); err != nil {
			rows.Close()
			return nil, err
		}
		r.words = len(ids.V)
		r.items = r.words
		if r.format == core.SheetFormatDictation {
			r.items = len(items.V)
		}
		// 旧查询条件：从未测完，或有进行中的测试
		if !r.tested || r.activeID != nil {
			list = append(list, r)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].seq < list[j].seq })
	next := -1
	for i, r := range list {
		if r.activeID != nil {
			next = i
			break
		}
	}
	if next < 0 {
		for i, r := range list {
			if !r.tested {
				next = i
				break
			}
		}
	}
	if next < 0 {
		return nil, nil
	}
	remaining := 0
	for i, r := range list {
		if i != next && !r.tested {
			remaining++
		}
	}
	n := list[next]
	return &NextSheetView{ID: n.id, Seq: n.seq, WordCount: n.words, ActiveSessionID: n.activeID, Remaining: remaining, Format: n.format, ItemCount: n.items}, nil
}

// GradedSheetToday 今日已批改的默写单（spec 0006：今日页显示「默写单 #N · 已批改」）。
type GradedSheetToday struct {
	ID         string     `json:"id"`
	Seq        int        `json:"seq"`
	ItemCount  int        `json:"itemCount"`
	SessionID  string     `json:"sessionId"`
	Correct    int        `json:"correct"`
	Total      int        `json:"total"`
	SelfGraded bool       `json:"selfGraded"`
	GradedAt   store.Time `json:"gradedAt"`
}

// SheetsGradedToday 今天（学习日 day）批改提交的默写单，按批改时间先后。批改后的默写单不再出现在 NextSheet 里，
// 今日页用这个列表显示「已批改」。
func SheetsGradedToday(ctx context.Context, q store.Querier, userID, day string) ([]GradedSheetToday, error) {
	rows, err := q.QueryContext(ctx, `SELECT w."id", w."seq", w."items", s."id", s."status", s."result", s."snapshot", s."completedAt"
		FROM "StudySession" s JOIN "WordSheet" w ON w."id" = s."sheetId"
		WHERE w."userId" = ? AND w."format" = ? AND s."status" = 'completed' AND s."completedDay" = ?
		ORDER BY s."completedAt", s.rowid`, userID, core.SheetFormatDictation, day)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GradedSheetToday{}
	for rows.Next() {
		var g GradedSheetToday
		var items store.JSON[[]core.DictationItem]
		var b sheetSessionBrief
		if err := rows.Scan(&g.ID, &g.Seq, &items, &b.id, &b.status, &b.result, &b.snapshot, &g.GradedAt); err != nil {
			return nil, err
		}
		g.ItemCount = len(items.V)
		g.SessionID = b.id
		res, err := sheetFirstResult(&b)
		if err != nil {
			return nil, err
		}
		g.Correct, g.Total = res.Correct, res.Total
		self, err := snapshotSelfGraded(b.snapshot)
		if err != nil {
			return nil, err
		}
		g.SelfGraded = self != nil && *self
		out = append(out, g)
	}
	return out, rows.Err()
}
