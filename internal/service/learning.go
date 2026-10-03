package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/vinx-lab/vinx-vocab-go/internal/core"
	"github.com/vinx-lab/vinx-vocab-go/internal/httpx"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// 学习引擎服务层（对应旧 services/learning.ts）：今日队列、开组、作答、结算。
// 业务规则全部在 internal/core（today / scheduler / questions / answercheck），本文件只做数据装配与事务。
//
// 调用约定：
//   - 时间一律由调用方传入（api 层用 d.Now()，测试注入固定时钟），函数内先规整到毫秒（与旧版 JS Date 精度一致）；
//   - 需要随机的函数接收 core.Rng（nil 用默认随机源），测试注入 core.SeededRng 得到确定结果；
//   - 单词单（kind = "sheet"）与计划学习组走同一套开组 / 作答 / 结算流程（StartSession 按 kind 分派）。

// Modes 学习组题型全集（顺序即规整后的顺序）。
var Modes = []string{"recognition", "spelling", "cloze"}

// LearnClock 学习日时区 + 当前时刻。
type LearnClock struct {
	Loc *time.Location
	Now time.Time
}

// msTime 规整到毫秒 UTC（旧版 Date 精度）。
func msTime(t time.Time) time.Time { return t.UTC().Truncate(time.Millisecond) }

// TodayRange 旧 today(now)：学习日与 [start, end)。
func TodayRange(loc *time.Location, now time.Time) (day string, start, end time.Time) {
	day = core.DayKeyOf(now, loc)
	start, end = core.DayRange(day, loc)
	return
}

// LoadLearnerPlans 旧 loadLearnerPlans：某学生当前可学的计划（planID 非空时只看该计划）。
func LoadLearnerPlans(ctx context.Context, q store.Querier, userID, day, planID string) ([]LearnerPlan, error) {
	m, err := LoadPlansForLearners(ctx, q, []string{userID}, day, planID)
	if err != nil {
		return nil, err
	}
	return m[userID], nil
}

// ComputeToday 旧 computeToday：一个学生的今日队列。
func ComputeToday(ctx context.Context, q store.Querier, loc *time.Location, now time.Time, userID string) (core.TodaySummary, error) {
	m, err := ComputeTodayBatch(ctx, q, loc, msTime(now), []string{userID})
	if err != nil {
		return core.TodaySummary{}, err
	}
	return m[userID], nil
}

// notActiveTestAnswer 进行中检测（含单词单测试）的作答：交卷前不进入任何计数，避免推断对错。
// 用于 Answer a JOIN StudySession s 的查询条件（旧 NOT_ACTIVE_TEST_ANSWER）。
const notActiveTestAnswer = `NOT (s."kind" IN ('test','sheet') AND s."status" = 'active')`

// DrillCandidates 错词强化可练词（近 14 天首答错 + 遗忘过），最多 core.DrillGroupSize 个。
func DrillCandidates(ctx context.Context, q store.Querier, loc *time.Location, now time.Time, userID string) ([]string, error) {
	day, _, _ := TodayRange(loc, now)
	since := core.AddDays(day, -13)
	rows, err := q.QueryContext(ctx, `SELECT a."wordId", count(*) FROM "Answer" a JOIN "StudySession" s ON s."id" = a."sessionId"
		WHERE a."userId" = ? AND a."correct" = 0 AND a."attempt" = 1 AND a."phase" IN ('practice','test') AND a."dayKey" >= ? AND `+notActiveTestAnswer+`
		GROUP BY a."wordId"`, userID, since)
	if err != nil {
		return nil, err
	}
	wrong := map[string]int{}
	for rows.Next() {
		var w string
		var n int
		if err := rows.Scan(&w, &n); err != nil {
			rows.Close()
			return nil, err
		}
		wrong[w] = n
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	lrows, err := q.QueryContext(ctx, `SELECT "wordId","lapses" FROM "MemoryState" WHERE "userId" = ? AND "lapses" > 0`, userID)
	if err != nil {
		return nil, err
	}
	lapses := map[string]int{}
	for lrows.Next() {
		var w string
		var n int
		if err := lrows.Scan(&w, &n); err != nil {
			lrows.Close()
			return nil, err
		}
		lapses[w] = n
	}
	lrows.Close()
	if err := lrows.Err(); err != nil {
		return nil, err
	}
	return core.SelectDrillWords(wrong, lapses, core.DrillGroupSize), nil
}

// ------------------------------------------------------------------
// 会话快照
// ------------------------------------------------------------------

// SnapshotItem 学习组快照里的一个词（字段顺序与旧版一致）。
type SnapshotItem struct {
	WordID       string   `json:"wordId"`
	Spelling     string   `json:"spelling"`
	Answer       string   `json:"answer"`
	Phonetic     *string  `json:"phonetic"`
	PartOfSpeech *string  `json:"partOfSpeech"`
	Definition   string   `json:"definition"`
	Example      *string  `json:"example"`
	ExampleCn    *string  `json:"exampleCn"`
	Type         string   `json:"type"`
	IsNew        bool     `json:"isNew"`
	Options      []string `json:"options"`
	// Modes 该词实际可出的题型（挖空需要例句，缺例句的词自动跳过该题型）。旧数据可能没有（nil），用组的 modes。
	Modes []string `json:"modes"`
	// Cloze / ClozeCn 挖空题面（例句中目标词替换为 ____）与中文翻译。
	Cloze   *string `json:"cloze"`
	ClozeCn *string `json:"clozeCn"`
}

// ItemModes 该词的题型（旧 item.modes ?? snap.modes）。
func (s *Snapshot) ItemModes(i *SnapshotItem) []string {
	if i.Modes != nil {
		return i.Modes
	}
	return s.Modes
}

// Snapshot 学习组快照（StudySession.snapshot）。
type Snapshot struct {
	PlanName *string        `json:"planName"`
	Modes    []string       `json:"modes"`
	Items    []SnapshotItem `json:"items"`
	// 默写单批改组（spec 0006）才有以下字段：格式、批改人、是否自批、句子题。
	Format     *string            `json:"format,omitempty"`
	GradedBy   *SnapshotUser      `json:"gradedBy,omitempty"`
	SelfGraded *bool              `json:"selfGraded,omitempty"`
	Sentences  []SnapshotSentence `json:"sentences,omitempty"`
	// raw 每个词的原始 JSON（接口原样返回，保持旧数据里缺省字段的形态）。
	raw []json.RawMessage
}

// RawItem 第 i 个词的原始 JSON。
func (s *Snapshot) RawItem(i int) json.RawMessage {
	if i < len(s.raw) {
		return s.raw[i]
	}
	b, _ := json.Marshal(s.Items[i])
	return b
}

// SnapshotUser 快照里的批改人。
type SnapshotUser struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// SnapshotSentence 默写单快照里的一道句子题（Index 为题目在默写单 items 里的下标）。
type SnapshotSentence struct {
	Index      int    `json:"index"`
	SentenceID string `json:"sentenceId"`
	Type       string `json:"type"`
	En         string `json:"en"`
	Cn         string `json:"cn"`
	Prompt     string `json:"prompt"`
}

// ParseSnapshot 解析快照 JSON 文本。
func ParseSnapshot(text string) (*Snapshot, error) {
	var s Snapshot
	if err := json.Unmarshal([]byte(text), &s); err != nil {
		return nil, fmt.Errorf("解析学习组快照: %w", err)
	}
	var raw struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal([]byte(text), &raw); err == nil {
		s.raw = raw.Items
	}
	if s.Modes == nil {
		s.Modes = []string{}
	}
	return &s, nil
}

func (s *Snapshot) find(wordID string) *SnapshotItem {
	for i := range s.Items {
		if s.Items[i].WordID == wordID {
			return &s.Items[i]
		}
	}
	return nil
}

// SnapshotOptions 生成快照的参数。
type SnapshotOptions struct {
	PlanName        *string
	Modes           []string
	IsNew           bool
	DistractorScope []string
}

type snapWord struct {
	core.QuestionWord
	Phonetic, Example, ExampleCn *string
	Type                         string
}

func loadSnapWords(ctx context.Context, q store.Querier, query string, args ...any) ([]snapWord, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []snapWord{}
	for rows.Next() {
		var w snapWord
		if err := rows.Scan(&w.ID, &w.Spelling, &w.Type, &w.Phonetic, &w.PartOfSpeech, &w.Definition, &w.Example, &w.ExampleCn); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

const snapWordCols = `"id","spelling","type","phonetic","partOfSpeech","definition","example","exampleCn"`

func wordsByIDs(ctx context.Context, q store.Querier, ids []string) ([]snapWord, error) {
	out := []snapWord{}
	for i := 0; i < len(ids); i += 500 {
		chunk := ids[i:min(i+500, len(ids))]
		ws, err := loadSnapWords(ctx, q, `SELECT `+snapWordCols+` FROM "Word" WHERE "id" IN (`+store.Placeholders(len(chunk))+`)`, store.Args(chunk)...)
		if err != nil {
			return nil, err
		}
		out = append(out, ws...)
	}
	return out, nil
}

// BuildSnapshot 生成学习组快照（旧 buildSnapshot）：按 wordIds 顺序取词、出认义选项、挖空题面。
func BuildSnapshot(ctx context.Context, q store.Querier, rng core.Rng, wordIDs []string, opts SnapshotOptions) (*Snapshot, error) {
	if rng == nil {
		rng = core.DefaultRng
	}
	words, err := wordsByIDs(ctx, q, wordIDs)
	if err != nil {
		return nil, err
	}
	byID := map[string]snapWord{}
	for _, w := range words {
		byID[w.ID] = w
	}
	ordered := []snapWord{}
	for _, id := range wordIDs {
		if w, ok := byID[id]; ok {
			ordered = append(ordered, w)
		}
	}
	hasMode := func(m string) bool { return slices.Contains(opts.Modes, m) }

	pool := make([]core.QuestionWord, 0, len(ordered))
	for _, w := range ordered {
		pool = append(pool, w.QuestionWord)
	}
	if hasMode("recognition") {
		rest := []string{}
		for _, id := range opts.DistractorScope {
			if _, ok := byID[id]; !ok {
				rest = append(rest, id)
			}
		}
		extraIDs := core.Shuffle(rest, rng)
		if len(extraIDs) > 60 {
			extraIDs = extraIDs[:60]
		}
		extra, err := wordsByIDs(ctx, q, extraIDs)
		if err != nil {
			return nil, err
		}
		if len(ordered)+len(extra) < 8 {
			// 范围太小：从全库随机补干扰项
			var count int
			if err := q.QueryRowContext(ctx, `SELECT count(*) FROM "Word"`).Scan(&count); err != nil {
				return nil, err
			}
			skip := int(rng() * float64(max(0, count-40)))
			more, err := loadSnapWords(ctx, q, `SELECT `+snapWordCols+` FROM "Word" ORDER BY rowid LIMIT 40 OFFSET ?`, max(0, skip))
			if err != nil {
				return nil, err
			}
			extra = append(extra, more...)
		}
		for _, w := range extra {
			pool = append(pool, w.QuestionWord)
		}
	}

	modes := append([]string{}, opts.Modes...)
	snap := &Snapshot{PlanName: opts.PlanName, Modes: modes, Items: make([]SnapshotItem, 0, len(ordered))}
	for _, w := range ordered {
		var cloze *string
		if hasMode("cloze") {
			cloze = core.BuildCloze(w.Example, w.Spelling)
		}
		options := []string{}
		if hasMode("recognition") {
			options = core.BuildChoiceOptions(w.QuestionWord, pool, rng, 4)
		}
		itemModes := []string{}
		for _, m := range opts.Modes {
			if m != "cloze" || cloze != nil {
				itemModes = append(itemModes, m)
			}
		}
		var clozeCn *string
		if cloze != nil {
			clozeCn = w.ExampleCn
		}
		snap.Items = append(snap.Items, SnapshotItem{
			WordID: w.ID, Spelling: w.Spelling, Answer: core.SpellingTarget(w.Spelling), Phonetic: w.Phonetic,
			PartOfSpeech: w.PartOfSpeech, Definition: w.Definition, Example: w.Example, ExampleCn: w.ExampleCn,
			Type: w.Type, IsNew: opts.IsNew, Options: options, Modes: itemModes, Cloze: cloze, ClozeCn: clozeCn,
		})
	}
	return snap, nil
}

// ------------------------------------------------------------------
// 学习组行
// ------------------------------------------------------------------

// SessionRow StudySession 表的一行。
type SessionRow struct {
	ID           string
	UserID       string
	PlanID       *string
	SheetID      *string
	Kind         string
	WordCount    int
	Status       string
	DayKey       string
	SnapshotText string
	Progress     *string
	Result       *string
	StartedAt    store.Time
	CompletedAt  store.NullTime
	CompletedDay *string
}

const sessionCols = `"id","userId","planId","sheetId","kind","wordCount","status","dayKey","snapshot","progress","result","startedAt","completedAt","completedDay"`

func scanSession(row interface{ Scan(...any) error }) (*SessionRow, error) {
	var s SessionRow
	if err := row.Scan(&s.ID, &s.UserID, &s.PlanID, &s.SheetID, &s.Kind, &s.WordCount, &s.Status, &s.DayKey, &s.SnapshotText, &s.Progress, &s.Result, &s.StartedAt, &s.CompletedAt, &s.CompletedDay); err != nil {
		return nil, err
	}
	return &s, nil
}

// GetSession 按 id 取学习组；不存在返回 (nil, nil)。
func GetSession(ctx context.Context, q store.Querier, id string) (*SessionRow, error) {
	s, err := scanSession(q.QueryRowContext(ctx, `SELECT `+sessionCols+` FROM "StudySession" WHERE "id" = ?`, id))
	if store.IsNoRows(err) {
		return nil, nil
	}
	return s, err
}

// findActiveSession 同一学生、同一计划（nil 为无计划）、同一类型的进行中组。
func findActiveSession(ctx context.Context, q store.Querier, userID string, planID *string, kind string) (*SessionRow, error) {
	query := `SELECT ` + sessionCols + ` FROM "StudySession" WHERE "userId" = ? AND "kind" = ? AND "status" = 'active' AND `
	args := []any{userID, kind}
	if planID == nil {
		query += `"planId" IS NULL`
	} else {
		query += `"planId" = ?`
		args = append(args, *planID)
	}
	s, err := scanSession(q.QueryRowContext(ctx, query+` LIMIT 1`, args...))
	if store.IsNoRows(err) {
		return nil, nil
	}
	return s, err
}

func countAnswers(ctx context.Context, q store.Querier, sessionID string) (int, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT count(*) FROM "Answer" WHERE "sessionId" = ?`, sessionID).Scan(&n)
	return n, err
}

// ------------------------------------------------------------------
// 开组
// ------------------------------------------------------------------

// StartInput 开组参数（旧 StartInput）。PlanID / SheetID 为 nil 表示缺省或 null。
type StartInput struct {
	Kind    string
	PlanID  *string
	SheetID *string
}

// StartResult 开组结果（旧 { session, resumed }）。
type StartResult struct {
	Session *SessionRow
	Resumed bool
}

// NormalizeModes 按固定顺序保留合法题型；为空时只有认义。
func NormalizeModes(modes []string) []string {
	out := []string{}
	for _, m := range Modes {
		if slices.Contains(modes, m) {
			out = append(out, m)
		}
	}
	if len(out) == 0 {
		return []string{"recognition"}
	}
	return out
}

// wordsInOtherActiveSessions 其他进行中组已占用的词（避免两组同时学 / 复习同一个词）。
// 单词单测试不占词（K30）：它挑的正是到期易错词，占住会让当天复习卡死；重复更新记忆已由 K19 挡住。
func wordsInOtherActiveSessions(ctx context.Context, q store.Querier, userID string, exceptPlanID *string, kind string) (map[string]bool, error) {
	// 与 Prisma NOT: { planId, kind } 相同的三值逻辑：planId 为 NULL 的组只有 kind 不同时才计入
	rows, err := q.QueryContext(ctx, `SELECT "snapshot" FROM "StudySession" WHERE "userId" = ? AND "status" = 'active' AND "kind" <> 'sheet'
		AND NOT ("planId" = ? AND "kind" = ?)`, userID, exceptPlanID, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	busy := map[string]bool{}
	for rows.Next() {
		var text string
		if err := rows.Scan(&text); err != nil {
			return nil, err
		}
		snap, err := ParseSnapshot(text)
		if err != nil {
			return nil, err
		}
		for _, it := range snap.Items {
			busy[it.WordID] = true
		}
	}
	return busy, rows.Err()
}

type memDue struct {
	wordID string
	due    time.Time
}

// userMemoryDue 该学生在 scope 内的记忆（wordId, due），按 scope 过滤。
func userMemoryDue(ctx context.Context, q store.Querier, userID string, scope []string) ([]memDue, error) {
	in := make(map[string]bool, len(scope))
	for _, w := range scope {
		in[w] = true
	}
	rows, err := q.QueryContext(ctx, `SELECT "wordId","due" FROM "MemoryState" WHERE "userId" = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []memDue{}
	for rows.Next() {
		var m memDue
		var due store.Time
		if err := rows.Scan(&m.wordID, &due); err != nil {
			return nil, err
		}
		if in[m.wordID] {
			m.due = due.Time
			out = append(out, m)
		}
	}
	return out, rows.Err()
}

// StartSession 开始（或续做）一组（旧 startSession）。同一学生同一计划同一类型已有进行中组时直接返回它（K12）；
// 若计划已暂停、结束或不再安排给该学生，先保存已作答部分再提示（K22）。
// db 不能是事务：并发开组靠部分唯一索引 StudySession_active_uniq 兜底，冲突后返回已存在的组。
func StartSession(ctx context.Context, db *store.DB, loc *time.Location, now time.Time, rng core.Rng, userID string, in StartInput) (*StartResult, error) {
	now = msTime(now)
	if in.Kind == "sheet" {
		return startSheetSession(ctx, db, loc, now, rng, userID, in.SheetID)
	}
	var planID *string
	if in.Kind != "drill" {
		planID = in.PlanID
	}
	day, _, dayEnd := TodayRange(loc, now)
	var plan *LearnerPlan
	if planID != nil && *planID != "" {
		plans, err := LoadLearnerPlans(ctx, db, userID, day, *planID)
		if err != nil {
			return nil, err
		}
		if len(plans) > 0 {
			plan = &plans[0]
		}
	}

	existing, err := findActiveSession(ctx, db, userID, planID, in.Kind)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		if in.Kind == "drill" || plan != nil {
			return &StartResult{Session: existing, Resumed: true}, nil
		}
		n, err := countAnswers(ctx, db, existing.ID)
		if err != nil {
			return nil, err
		}
		if n > 0 {
			if _, err := CompleteSession(ctx, db, loc, now, userID, existing.ID); err != nil {
				return nil, err
			}
		} else if _, err := db.ExecContext(ctx, `DELETE FROM "StudySession" WHERE "id" = ?`, existing.ID); err != nil {
			return nil, err
		}
		return nil, httpx.NotFound("计划已暂停、结束或不再安排给你，之前的作答已保存")
	}

	wordIDs := []string{}
	modes := []string{"recognition", "spelling"}
	var planName *string
	distractorScope := []string{}

	if in.Kind == "drill" {
		if wordIDs, err = DrillCandidates(ctx, db, loc, now, userID); err != nil {
			return nil, err
		}
		if len(wordIDs) == 0 {
			return nil, httpx.NewError(httpx.CodeNoData, "最近没有错词，继续保持！", nil)
		}
		name := "错词强化"
		planName = &name
	} else {
		if planID == nil || *planID == "" {
			return nil, httpx.Validation("缺少计划")
		}
		if plan == nil {
			return nil, httpx.NotFound("计划不存在、未生效或不属于你")
		}
		name := plan.Name
		planName = &name
		modes = NormalizeModes(plan.Modes)
		distractorScope = plan.ScopeWordIDs

		summary, err := ComputeToday(ctx, db, loc, now, userID)
		if err != nil {
			return nil, err
		}
		busy, err := wordsInOtherActiveSessions(ctx, db, userID, planID, in.Kind)
		if err != nil {
			return nil, err
		}
		var card *core.TodayPlanCard
		for i := range summary.Plans {
			if summary.Plans[i].PlanID == *planID {
				card = &summary.Plans[i]
				break
			}
		}
		memories, err := userMemoryDue(ctx, db, userID, plan.ScopeWordIDs)
		if err != nil {
			return nil, err
		}
		learned := map[string]bool{}
		dueByWord := map[string]time.Time{}
		memInputs := make([]core.MemoryInput, 0, len(memories))
		for _, m := range memories {
			learned[m.wordID] = true
			dueByWord[m.wordID] = m.due
			memInputs = append(memInputs, core.MemoryInput{WordID: m.wordID, Due: m.due})
		}

		switch in.Kind {
		case "learn":
			if plan.Kind != "daily" {
				return nil, httpx.NewError(httpx.CodeInvalidAction, "检测计划不能学新词", nil)
			}
			n := 0
			if card != nil {
				n = card.NewLeft
			}
			n = min(n, core.LearnGroupSize)
			scope := []string{}
			for _, w := range plan.ScopeWordIDs {
				if !busy[w] {
					scope = append(scope, w)
				}
			}
			order := "sequential"
			if plan.Order == "random" {
				order = "random"
			}
			wordIDs = core.SelectNewWords(scope, learned, n, order, rng)
			if len(wordIDs) == 0 {
				return nil, httpx.NewError(httpx.CodeNoData, "今天的新词已经学完了", nil)
			}
		case "review":
			if plan.Kind != "daily" {
				return nil, httpx.NewError(httpx.CodeInvalidAction, "检测计划没有复习", nil)
			}
			n := 0
			if card != nil {
				n = card.ReviewLeft
			}
			n = min(n, core.ReviewGroupSize)
			// 只复习本计划认领到的到期词，且不与其他进行中组重复
			plansToday, err := LoadLearnerPlans(ctx, db, userID, day, "")
			if err != nil {
				return nil, err
			}
			inputs := make([]core.TodayPlanInput, len(plansToday))
			for i, p := range plansToday {
				inputs[i] = p.TodayPlanInput
			}
			claimed := core.ClaimDueWords(inputs, memInputs, dayEnd)[*planID]
			free := []string{}
			for _, w := range claimed {
				if !busy[w] {
					free = append(free, w)
				}
			}
			wordIDs = core.SelectDueWords(free, dueByWord, dayEnd, n)
			if len(wordIDs) == 0 {
				return nil, httpx.NewError(httpx.CodeNoData, "今天没有需要复习的词", nil)
			}
		case "test":
			if plan.Kind != "test" {
				return nil, httpx.NewError(httpx.CodeInvalidAction, "该计划不是检测", nil)
			}
			scope := "all"
			if plan.TestScope == "learned" {
				scope = "learned"
			}
			wordIDs = core.SelectTestWords(plan.ScopeWordIDs, learned, scope, plan.TestSize, rng)
			if len(wordIDs) == 0 {
				msg := "检测范围内没有单词"
				if plan.TestScope == "learned" {
					msg = "范围内还没有学过的词"
				}
				return nil, httpx.NewError(httpx.CodeNoData, msg, nil)
			}
		default:
			return nil, httpx.Validation("未知的学习类型")
		}
	}

	snap, err := BuildSnapshot(ctx, db, rng, wordIDs, SnapshotOptions{PlanName: planName, Modes: modes, IsNew: in.Kind == "learn", DistractorScope: distractorScope})
	if err != nil {
		return nil, err
	}
	s, err := insertSession(ctx, db, now, userID, planID, nil, in.Kind, day, snap)
	if err != nil {
		// 并发：部分唯一索引命中，返回已存在的进行中组
		if store.IsUniqueViolation(err) {
			again, err2 := findActiveSession(ctx, db, userID, planID, in.Kind)
			if err2 != nil {
				return nil, err2
			}
			if again != nil {
				return &StartResult{Session: again, Resumed: true}, nil
			}
		}
		return nil, err
	}
	return &StartResult{Session: s}, nil
}

func insertSession(ctx context.Context, q store.Querier, now time.Time, userID string, planID, sheetID *string, kind, day string, snap *Snapshot) (*SessionRow, error) {
	text, err := json.Marshal(snap)
	if err != nil {
		return nil, err
	}
	id := store.NewID()
	if _, err := q.ExecContext(ctx, `INSERT INTO "StudySession" ("id","userId","planId","sheetId","kind","wordCount","status","dayKey","snapshot","startedAt")
		VALUES (?,?,?,?,?,?,'active',?,?,?)`, id, userID, planID, sheetID, kind, len(snap.Items), day, string(text), store.NewTime(now)); err != nil {
		return nil, err
	}
	return GetSession(ctx, q, id)
}

// startSheetSession 单词单测试：只允许目标学生本人；同一时间只有一张在测（K30，沿用 StudySession_active_uniq，不改索引）。
func startSheetSession(ctx context.Context, db *store.DB, loc *time.Location, now time.Time, rng core.Rng, userID string, sheetID *string) (*StartResult, error) {
	if sheetID == nil || *sheetID == "" {
		return nil, httpx.Validation("缺少单词单")
	}
	var sheet struct {
		id, userID string
		seq        int
		wordIDs    store.JSON[[]string]
		modes      store.JSON[[]string]
		format     string
	}
	err := db.QueryRowContext(ctx, `SELECT "id","userId","seq","wordIds","modes","format" FROM "WordSheet" WHERE "id" = ?`, *sheetID).
		Scan(&sheet.id, &sheet.userID, &sheet.seq, &sheet.wordIDs, &sheet.modes, &sheet.format)
	if store.IsNoRows(err) || (err == nil && sheet.userID != userID) {
		return nil, httpx.NotFound("单词单不存在")
	}
	if err != nil {
		return nil, err
	}
	if sheet.format == core.SheetFormatDictation {
		return nil, httpx.NewError(httpx.CodeInvalidAction, "默写单不能在线测试，请打印后批改", nil)
	}
	active, err := findActiveSession(ctx, db, userID, nil, "sheet")
	if err != nil {
		return nil, err
	}
	if active != nil {
		if active.SheetID != nil && *active.SheetID == sheet.id {
			return &StartResult{Session: active, Resumed: true}, nil
		}
		seq := "?"
		if active.SheetID != nil {
			var n int
			if err := db.QueryRowContext(ctx, `SELECT "seq" FROM "WordSheet" WHERE "id" = ?`, *active.SheetID).Scan(&n); err == nil {
				seq = fmt.Sprint(n)
			} else if !store.IsNoRows(err) {
				return nil, err
			}
		}
		return nil, httpx.NewError(httpx.CodeInvalidAction, "先完成单词单 #"+seq+" 的测试", nil)
	}
	name := fmt.Sprintf("单词单 #%d", sheet.seq)
	snap, err := BuildSnapshot(ctx, db, rng, sheet.wordIDs.V, SnapshotOptions{
		PlanName: &name, Modes: NormalizeModes(sheet.modes.V), IsNew: false, DistractorScope: sheet.wordIDs.V,
	})
	if err != nil {
		return nil, err
	}
	day, _, _ := TodayRange(loc, now)
	sid := sheet.id
	s, err := insertSession(ctx, db, now, userID, nil, &sid, "sheet", day, snap)
	if err != nil {
		if store.IsUniqueViolation(err) {
			again, err2 := findActiveSession(ctx, db, userID, nil, "sheet")
			if err2 != nil {
				return nil, err2
			}
			if again != nil && again.SheetID != nil && *again.SheetID == sheet.id {
				return &StartResult{Session: again, Resumed: true}, nil
			}
			return nil, httpx.NewError(httpx.CodeInvalidAction, "已有单词单正在测试", nil)
		}
		return nil, err
	}
	return &StartResult{Session: s}, nil
}

// ------------------------------------------------------------------
// 作答
// ------------------------------------------------------------------

// AnswerInput 一次作答（旧 AnswerInput）。
type AnswerInput struct {
	WordID   string
	Mode     string
	Phase    string // practice | consolidate | test
	Attempt  int
	Answer   string
	HintUsed bool
	// DontKnow 点了「不会」：不判定答案，直接记为答错并单独标记（K39）。
	DontKnow   bool
	DurationMs int
}

// AnswerResult 作答结果：检测类组只有 Recorded（不即时反馈）。
type AnswerResult struct {
	Recorded bool    `json:"recorded"`
	Correct  *bool   `json:"correct,omitempty"`
	Expected *string `json:"expected,omitempty"`
}

// sliceUTF16 JS s.slice(0, n)（按 UTF-16 码元；不拆开代理对）。
func sliceUTF16(s string, n int) string {
	units := 0
	for i, r := range s {
		w := 1
		if r >= 0x10000 {
			w = 2
		}
		if units+w > n {
			return s[:i]
		}
		units += w
	}
	return s
}

func expectedAnswer(item *SnapshotItem, mode string) string {
	if mode == "recognition" {
		return item.Definition
	}
	return item.Answer
}

// RecordAnswer 记录一次作答（旧 recordAnswer）。同一 (sessionId, wordId, mode, phase, attempt) 重复提交
// （网络重试）不再插入，按已存结果返回。
func RecordAnswer(ctx context.Context, q store.Querier, loc *time.Location, now time.Time, userID, sessionID string, in AnswerInput) (*AnswerResult, error) {
	now = msTime(now)
	s, err := GetSession(ctx, q, sessionID)
	if err != nil {
		return nil, err
	}
	if s == nil || s.UserID != userID {
		return nil, httpx.NotFound("学习组不存在")
	}
	if s.Status != "active" {
		return nil, httpx.NewError(httpx.CodeInvalidStatus, "这一组已经结束", nil)
	}
	snap, err := ParseSnapshot(s.SnapshotText)
	if err != nil {
		return nil, err
	}
	item := snap.find(in.WordID)
	if item == nil {
		return nil, httpx.Validation("该词不在本组中")
	}
	if !slices.Contains(snap.ItemModes(item), in.Mode) {
		return nil, httpx.Validation("本组不包含该题型")
	}
	testKind := core.IsTestKind(s.Kind)
	expectedPhases := []string{"practice", "consolidate"}
	if testKind {
		expectedPhases = []string{"test"}
	}
	if !slices.Contains(expectedPhases, in.Phase) {
		return nil, httpx.Validation("作答阶段不匹配")
	}

	correct := core.JudgeAnswer(in.Mode, in.Answer, in.DontKnow, item.Definition, item.Spelling)
	userAnswer := sliceUTF16(in.Answer, 200)
	if in.DontKnow {
		userAnswer = "" // 「不会」不保存填写内容，记录里只显示「不会」
	}
	duration := max(0, min(in.DurationMs, 10*60_000))
	day, _, _ := TodayRange(loc, now)
	_, err = q.ExecContext(ctx, `INSERT INTO "Answer" ("id","sessionId","userId","wordId","mode","phase","attempt","correct","userAnswer","hintUsed","dontKnow","durationMs","dayKey","createdAt")
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		store.NewID(), sessionID, userID, in.WordID, in.Mode, in.Phase, in.Attempt, correct, userAnswer, in.HintUsed, in.DontKnow, duration, day, store.NewTime(now))
	if err != nil {
		if !store.IsUniqueViolation(err) {
			return nil, err
		}
		// 网络重试：同一题同一次作答已记录，按已存结果返回
		var prev bool
		perr := q.QueryRowContext(ctx, `SELECT "correct" FROM "Answer" WHERE "sessionId" = ? AND "wordId" = ? AND "mode" = ? AND "phase" = ? AND "attempt" = ?`,
			sessionID, in.WordID, in.Mode, in.Phase, in.Attempt).Scan(&prev)
		if perr == nil {
			correct = prev
		} else if !store.IsNoRows(perr) {
			return nil, perr
		}
	}
	if testKind {
		return &AnswerResult{Recorded: true}, nil
	}
	exp := expectedAnswer(item, in.Mode)
	return &AnswerResult{Recorded: true, Correct: &correct, Expected: &exp}, nil
}

// SaveProgress 保存前端进度（旧 saveProgress）；组已结束时静默忽略。progress 为 JSON 对象文本。
func SaveProgress(ctx context.Context, q store.Querier, userID, sessionID string, progress json.RawMessage) error {
	var owner, status string
	err := q.QueryRowContext(ctx, `SELECT "userId","status" FROM "StudySession" WHERE "id" = ?`, sessionID).Scan(&owner, &status)
	if store.IsNoRows(err) || (err == nil && owner != userID) {
		return httpx.NotFound("学习组不存在")
	}
	if err != nil {
		return err
	}
	if status != "active" {
		return nil
	}
	_, err = q.ExecContext(ctx, `UPDATE "StudySession" SET "progress" = ? WHERE "id" = ?`, string(progress), sessionID)
	return err
}

// DiscardSession 放弃一组（旧 discardSession）：只允许还没有任何作答的组，避免抹掉学习事实。
func DiscardSession(ctx context.Context, q store.Querier, userID, sessionID string) error {
	s, err := GetSession(ctx, q, sessionID)
	if err != nil {
		return err
	}
	if s == nil || s.UserID != userID {
		return httpx.NotFound("学习组不存在")
	}
	if s.Status != "active" {
		return httpx.NewError(httpx.CodeInvalidStatus, "这一组已经结束", nil)
	}
	n, err := countAnswers(ctx, q, sessionID)
	if err != nil {
		return err
	}
	if n > 0 {
		return httpx.NewError(httpx.CodeInvalidAction, "已经开始作答，请点“结束本组”保存进度", nil)
	}
	_, err = q.ExecContext(ctx, `DELETE FROM "StudySession" WHERE "id" = ?`, sessionID)
	return err
}

// ------------------------------------------------------------------
// 结算
// ------------------------------------------------------------------

// SessionResult 结算结果（旧 SessionResult，存在 StudySession.result）。
type SessionResult struct {
	Words        int            `json:"words"`
	Settled      int            `json:"settled"`
	Unsettled    int            `json:"unsettled"`
	CorrectFirst int            `json:"correctFirst"`
	TotalFirst   int            `json:"totalFirst"`
	Accuracy     *float64       `json:"accuracy"`
	DurationMs   int            `json:"durationMs"`
	NewLearned   int            `json:"newLearned"`
	Ratings      map[string]int `json:"ratings"`
	WrongWordIDs []string       `json:"wrongWordIds"`
	// Sentences 默写单的句子题成绩（spec 0006；其他组没有这个字段）。
	Sentences *SentenceResult `json:"sentences,omitempty"`
}

// SentenceResult 默写单句子题的成绩。
type SentenceResult struct {
	Total            int      `json:"total"`
	Correct          int      `json:"correct"`
	WrongSentenceIDs []string `json:"wrongSentenceIds"`
}

// CompleteOutput 完成接口的数据（旧 { session: { id, status }, result }）。
type CompleteOutput struct {
	Session struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	} `json:"session"`
	// Result 已完成的组原样返回库里保存的结果。
	Result json.RawMessage `json:"result"`
}

// CompleteSession 结算一组（旧 completeSession），在自己的事务里执行。
func CompleteSession(ctx context.Context, db *store.DB, loc *time.Location, now time.Time, userID, sessionID string) (*CompleteOutput, error) {
	var out *CompleteOutput
	err := db.Tx(ctx, func(tx *sql.Tx) error {
		var err error
		out, err = CompleteSessionTx(ctx, tx, loc, now, userID, sessionID)
		return err
	})
	return out, err
}

type answerRow struct {
	WordID, Mode, Phase string
	Attempt             int
	Correct, HintUsed   bool
	DurationMs          int
}

// memoryRow MemoryState 的 FSRS 字段。
type memoryRow struct {
	ID string
	core.MemoryCard
}

// CompleteSessionTx 结算一组（必须在事务 tx 里调用；写事务串行化，第二次完成直接返回已存结果，幂等）。
//
// 规则：按练习 / 检测阶段第 1 次作答推导评分（core.DeriveRating）；
//   - learn 且没有记忆：建卡（封顶到下个学习日），并发结算同一新词时后到者跳过；
//   - 其余组有记忆时更新，但检测 / 单词单重考不更新，且每个词每个学习日最多更新一次（K19 / K20）；
//   - drill 不更新记忆。
func CompleteSessionTx(ctx context.Context, tx store.Querier, loc *time.Location, now time.Time, userID, sessionID string) (*CompleteOutput, error) {
	s, err := GetSession(ctx, tx, sessionID)
	if err != nil {
		return nil, err
	}
	if s == nil || s.UserID != userID {
		return nil, httpx.NotFound("学习组不存在")
	}
	if s.Status == "completed" {
		out := &CompleteOutput{}
		out.Session.ID = s.ID
		out.Session.Status = s.Status
		out.Result = json.RawMessage("null")
		if s.Result != nil {
			out.Result = json.RawMessage(*s.Result)
		}
		return out, nil
	}
	return settleSessionTx(ctx, tx, loc, now, s, nil)
}

// settleSessionTx 结算的主体（CompleteSessionTx 与默写单批改共用）：按作答推导评分、更新记忆、写结果并把组标为已完成。
// 不检查组的状态：默写单批改直接插入一个已完成的组（避开 StudySession_active_uniq）再调用它。
// extend 非 nil 时在写库前补充结果（默写单的句子成绩）。
func settleSessionTx(ctx context.Context, tx store.Querier, loc *time.Location, now time.Time, s *SessionRow, extend func(*SessionResult)) (*CompleteOutput, error) {
	now = msTime(now)
	day, dayStart, _ := TodayRange(loc, now)
	userID, sessionID := s.UserID, s.ID
	out := &CompleteOutput{}
	out.Session.ID = s.ID

	snap, err := ParseSnapshot(s.SnapshotText)
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT "wordId","mode","phase","attempt","correct","hintUsed","durationMs" FROM "Answer" WHERE "sessionId" = ? ORDER BY "createdAt", rowid`, sessionID)
	if err != nil {
		return nil, err
	}
	answers := []answerRow{}
	for rows.Next() {
		var a answerRow
		if err := rows.Scan(&a.WordID, &a.Mode, &a.Phase, &a.Attempt, &a.Correct, &a.HintUsed, &a.DurationMs); err != nil {
			rows.Close()
			return nil, err
		}
		answers = append(answers, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	kind := s.Kind
	firstPhase := "practice"
	if core.IsTestKind(kind) {
		firstPhase = "test"
	}
	firstByWord := map[string][]core.FirstAttempt{}
	wrong := []string{}
	wrongSeen := map[string]bool{}
	first := []answerRow{}
	for _, a := range answers {
		if a.Phase != firstPhase || a.Attempt != 1 {
			continue
		}
		first = append(first, a)
		firstByWord[a.WordID] = append(firstByWord[a.WordID], core.FirstAttempt{Mode: a.Mode, Correct: a.Correct, HintUsed: a.HintUsed})
		if !a.Correct && !wrongSeen[a.WordID] {
			wrongSeen[a.WordID] = true
			wrong = append(wrong, a.WordID)
		}
	}

	memByWord, err := loadMemories(ctx, tx, userID, snap)
	if err != nil {
		return nil, err
	}
	tomorrowStart, _ := core.DayRange(core.AddDays(day, 1), loc)

	// 检测重考只算练习：同一计划（或同一张单词单）已交过卷则不再更新记忆
	isRetake := false
	if kind == "test" || kind == "sheet" {
		col, val := "planId", s.PlanID
		if kind == "sheet" {
			col, val = "sheetId", s.SheetID
		}
		cond := `"` + col + `" IS NULL`
		args := []any{userID, kind, sessionID}
		if val != nil {
			cond = `"` + col + `" = ?`
			args = append(args, *val)
		}
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM "StudySession" WHERE "userId" = ? AND "kind" = ? AND "status" = 'completed' AND "id" <> ? AND `+cond, args...).Scan(&n); err != nil {
			return nil, err
		}
		isRetake = n > 0
	}

	ratings := map[string]int{}
	settled, newLearned := 0, 0
	for i := range snap.Items {
		item := &snap.Items[i]
		rating := core.DeriveRating(kind, firstByWord[item.WordID], snap.ItemModes(item))
		if rating == 0 {
			continue
		}
		settled++
		mem := memByWord[item.WordID]
		applied := false

		if kind == "learn" && mem == nil {
			card := core.CapDue(core.ApplyRating(core.NewMemoryCard(now), rating, now), tomorrowStart, now)
			// 两个计划的新学组并发结算同一新词时，后到者跳过（不回滚整组）
			res, err := tx.ExecContext(ctx, `INSERT INTO "MemoryState" ("id","userId","wordId","due","stability","difficulty","elapsedDays","scheduledDays","learningSteps","reps","lapses","state","lastReview","introducedAt","introducedPlanId","introducedDay","updatedAt")
				VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT ("userId","wordId") DO NOTHING`,
				store.NewID(), userID, item.WordID, store.NewTime(card.Due), card.Stability, card.Difficulty, card.ElapsedDays, card.ScheduledDays,
				card.LearningSteps, card.Reps, card.Lapses, card.State, nullTimePtr(card.LastReview), store.NewTime(now), s.PlanID, s.DayKey, store.NewTime(now))
			if err != nil {
				return nil, err
			}
			if n, _ := res.RowsAffected(); n == 1 {
				newLearned++
				applied = true
				if err := insertReviewLog(ctx, tx, now, day, userID, item.WordID, sessionID, rating, 0, card); err != nil {
					return nil, err
				}
			}
		} else if mem != nil && kind != "learn" && !isRetake && !(mem.LastReview != nil && !mem.LastReview.Before(dayStart)) {
			// 每个词每个学习日最多更新一次记忆（多计划共享词、同日学完即测都不重复计）
			card := core.ApplyRating(mem.MemoryCard, rating, now)
			if _, err := tx.ExecContext(ctx, `UPDATE "MemoryState" SET "due" = ?, "stability" = ?, "difficulty" = ?, "elapsedDays" = ?, "scheduledDays" = ?,
				"learningSteps" = ?, "reps" = ?, "lapses" = ?, "state" = ?, "lastReview" = ?, "updatedAt" = ? WHERE "id" = ?`,
				store.NewTime(card.Due), card.Stability, card.Difficulty, card.ElapsedDays, card.ScheduledDays, card.LearningSteps,
				card.Reps, card.Lapses, card.State, nullTimePtr(card.LastReview), store.NewTime(now), mem.ID); err != nil {
				return nil, err
			}
			if err := insertReviewLog(ctx, tx, now, day, userID, item.WordID, sessionID, rating, mem.State, card); err != nil {
				return nil, err
			}
			applied = true
		}
		if applied {
			ratings[core.RatingName[rating]]++
		}
	}

	correctFirst := 0
	firstWords := map[string]bool{}
	for _, a := range first {
		if a.Correct {
			correctFirst++
		}
		firstWords[a.WordID] = true
	}
	duration := 0
	for _, a := range answers {
		duration += min(a.DurationMs, 60_000)
	}
	result := SessionResult{
		Words: len(snap.Items), Settled: settled, Unsettled: len(snap.Items) - settled, CorrectFirst: correctFirst,
		TotalFirst: len(first), DurationMs: duration, NewLearned: newLearned, Ratings: ratings, WrongWordIDs: wrong,
	}
	if kind == "drill" {
		result.Settled = len(firstWords)
		result.Unsettled = 0
	}
	if len(first) > 0 {
		v := float64(correctFirst) / float64(len(first))
		result.Accuracy = &v
	}
	if extend != nil {
		extend(&result)
	}
	text, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE "StudySession" SET "status" = 'completed', "completedAt" = ?, "completedDay" = ?, "result" = ? WHERE "id" = ?`,
		store.NewTime(now), day, string(text), sessionID); err != nil {
		return nil, err
	}
	out.Session.Status = "completed"
	out.Result = text
	return out, nil
}

func nullTimePtr(t *time.Time) store.NullTime {
	if t == nil {
		return store.NullTime{}
	}
	return store.NewNullTime(*t)
}

func insertReviewLog(ctx context.Context, tx store.Querier, now time.Time, day, userID, wordID, sessionID string, rating core.Rating, stateBefore int, card core.MemoryCard) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO "ReviewLog" ("id","userId","wordId","sessionId","rating","stateBefore","stabilityAfter","difficultyAfter","dueAfter","reviewedAt","dayKey")
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		store.NewID(), userID, wordID, sessionID, int(rating), stateBefore, card.Stability, card.Difficulty, store.NewTime(card.Due), store.NewTime(now), day)
	return err
}

// loadMemories 该学生在快照词里的记忆状态。
func loadMemories(ctx context.Context, q store.Querier, userID string, snap *Snapshot) (map[string]*memoryRow, error) {
	out := map[string]*memoryRow{}
	ids := make([]string, 0, len(snap.Items))
	for _, it := range snap.Items {
		ids = append(ids, it.WordID)
	}
	for i := 0; i < len(ids); i += 500 {
		chunk := ids[i:min(i+500, len(ids))]
		rows, err := q.QueryContext(ctx, `SELECT "id","wordId","due","stability","difficulty","elapsedDays","scheduledDays","learningSteps","reps","lapses","state","lastReview"
			FROM "MemoryState" WHERE "userId" = ? AND "wordId" IN (`+store.Placeholders(len(chunk))+`)`, append([]any{userID}, store.Args(chunk)...)...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var m memoryRow
			var wordID string
			var due store.Time
			var last store.NullTime
			if err := rows.Scan(&m.ID, &wordID, &due, &m.Stability, &m.Difficulty, &m.ElapsedDays, &m.ScheduledDays, &m.LearningSteps, &m.Reps, &m.Lapses, &m.State, &last); err != nil {
				rows.Close()
				return nil, err
			}
			m.Due = due.Time
			if last.Valid {
				lr := last.Time
				m.LastReview = &lr
			}
			out[wordID] = &m
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}
