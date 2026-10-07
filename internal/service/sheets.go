package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"
	"time"

	"github.com/vinx-lab/vinx-vocab-go/internal/core"
	"github.com/vinx-lab/vinx-vocab-go/internal/httpx"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// 单词单：预览（三种选词来源）、批量生成、列表、明细、删除（对应旧 services/sheets.ts）。
// 选词打分在 internal/core/unfamiliar.go，批量切分在 internal/core/sheetbatch.go。
// 今日页下一份用 NextSheet（records.go，A4 已实现，这里不重复定义）；开组 / 结算（kind="sheet"）
// 用 StartSession / CompleteSessionTx（learning.go，A4 已实现）。
// 状态不落库：没有已完成的 sheet 组 = 待测；有 = 已测（成绩取首次交卷）。

// SheetSourceDays 错词来源：近 N 天有首次答错的学习组。
const SheetSourceDays = 30

// sheetSourceKinds 错词来源可选的学习组：检测、单词单测试、学习组（新学 / 复习）；错词强化不算。
var sheetSourceKinds = []string{"learn", "review", "test", "sheet"}

// wrongFirstWhere 首次作答答错（进行中检测类组的作答不计入，避免交卷前看出对错）。
// 拼进 Answer a JOIN StudySession s 的查询（旧 WRONG_FIRST）。
const wrongFirstWhere = `a."correct" = 0 AND a."attempt" = 1 AND a."phase" IN ('practice','test') AND ` + notActiveTestAnswer

// normalizeSheetModes 按固定顺序保留合法题型、去重；不像 NormalizeModes 那样兜底成 recognition
// （旧 createSheet 里 MODES.filter(...)，空结果由调用方报错）。
func normalizeSheetModes(modes []string) []string {
	out := []string{}
	for _, m := range Modes {
		if slices.Contains(modes, m) {
			out = append(out, m)
		}
	}
	return out
}

// ------------------------------------------------------------------
// 选词候选
// ------------------------------------------------------------------

// loadUnfamiliarCandidates 该学生全部已学词的记忆状态 + 近 14 天首次作答答错次数（旧 loadCandidates）。
func loadUnfamiliarCandidates(ctx context.Context, q store.Querier, loc *time.Location, now time.Time, userID string) ([]core.UnfamiliarCandidate, error) {
	day, _, _ := TodayRange(loc, now)
	since := core.AddDays(day, -13)
	rows, err := q.QueryContext(ctx, `SELECT "wordId","stability","lapses","due" FROM "MemoryState" WHERE "userId" = ?`, userID)
	if err != nil {
		return nil, err
	}
	out := []core.UnfamiliarCandidate{}
	for rows.Next() {
		var c core.UnfamiliarCandidate
		var due store.Time
		if err := rows.Scan(&c.WordID, &c.Stability, &c.Lapses, &due); err != nil {
			rows.Close()
			return nil, err
		}
		c.Due = due.Time
		out = append(out, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	wrongRows, err := q.QueryContext(ctx, `SELECT a."wordId", count(*) FROM "Answer" a JOIN "StudySession" s ON s."id" = a."sessionId"
		WHERE a."userId" = ? AND `+wrongFirstWhere+` AND a."dayKey" >= ? GROUP BY a."wordId"`, userID, since)
	if err != nil {
		return nil, err
	}
	wrongBy := map[string]int{}
	for wrongRows.Next() {
		var w string
		var n int
		if err := wrongRows.Scan(&w, &n); err != nil {
			wrongRows.Close()
			return nil, err
		}
		wrongBy[w] = n
	}
	wrongRows.Close()
	if err := wrongRows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Wrong14 = wrongBy[out[i].WordID]
	}
	return out, nil
}

// sessionWrongWordIDs 某次学习组里首次作答答错的词（按作答先后，去重；旧 sessionWrongWordIds）。
func sessionWrongWordIDs(ctx context.Context, q store.Querier, sessionID string) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT a."wordId" FROM "Answer" a JOIN "StudySession" s ON s."id" = a."sessionId"
		WHERE a."sessionId" = ? AND `+wrongFirstWhere+` ORDER BY a."createdAt", a.rowid`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	out := []string{}
	for rows.Next() {
		var w string
		if err := rows.Scan(&w); err != nil {
			return nil, err
		}
		if !seen[w] {
			seen[w] = true
			out = append(out, w)
		}
	}
	return out, rows.Err()
}

// pendingSheetWordIDs 该学生「待测」单子（从未交卷）上的词（旧 pendingSheetWordIds）。
func pendingSheetWordIDs(ctx context.Context, q store.Querier, userID string) (map[string]bool, error) {
	rows, err := q.QueryContext(ctx, `SELECT "wordIds" FROM "WordSheet" w WHERE "userId" = ? AND NOT EXISTS
		(SELECT 1 FROM "StudySession" s WHERE s."sheetId" = w."id" AND s."status" = 'completed')`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var ids store.JSON[[]string]
		if err := rows.Scan(&ids); err != nil {
			return nil, err
		}
		for _, id := range ids.V {
			out[id] = true
		}
	}
	return out, rows.Err()
}

// unitWordIDsOrdered 单元内的词 id，按单元内顺序。
func unitWordIDsOrdered(ctx context.Context, q store.Querier, unitID string) ([]string, error) {
	return queryStrings(ctx, q, `SELECT "wordId" FROM "UnitWord" WHERE "unitId" = ? ORDER BY "sortOrder"`, unitID)
}

// bookUnitIDsOrdered 词书下的单元 id，按单元顺序。
func bookUnitIDsOrdered(ctx context.Context, q store.Querier, bookID string) ([]string, error) {
	return queryStrings(ctx, q, `SELECT "id" FROM "Unit" WHERE "bookId" = ? ORDER BY "sortOrder"`, bookID)
}

// sheetWordRow 预览 / 明细里一个词的展示字段。
type sheetWordRow struct {
	ID           string
	Spelling     string
	Phonetic     *string
	PartOfSpeech *string
	Definition   string
	Type         string
}

const sheetWordSelect = `"id","spelling","phonetic","partOfSpeech","definition","type"`

// sheetWordDetails 按 id 批量取词的展示字段。
func sheetWordDetails(ctx context.Context, q store.Querier, ids []string) (map[string]sheetWordRow, error) {
	out := map[string]sheetWordRow{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := q.QueryContext(ctx, `SELECT `+sheetWordSelect+` FROM "Word" WHERE "id" IN (`+store.Placeholders(len(ids))+`)`, store.Args(ids)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var w sheetWordRow
		if err := rows.Scan(&w.ID, &w.Spelling, &w.Phonetic, &w.PartOfSpeech, &w.Definition, &w.Type); err != nil {
			return nil, err
		}
		out[w.ID] = w
	}
	return out, rows.Err()
}

// ------------------------------------------------------------------
// 预览 / 错词来源列表
// ------------------------------------------------------------------

// SheetSource 选词来源：不熟的词（默认）/ 某次测试的错词 / 某个单元 / 整本书（旧 SheetSource 可辨识联合）。
type SheetSource struct {
	Kind      string // unfamiliar | session | unit | book | target
	SessionID string
	UnitID    string
	BookID    string // book：整本书；target：只出这本目标词书的词（空 = 全部目标词）
	// Status target 来源：untested（目标：未测的词）| learning（目标：要学的词）（spec 0003）。
	Status string
	// UseClasses target 来源确定目标词书时是否看班级（TargetUsesClasses，由路由按本次请求的版本填）。
	UseClasses bool
}

// SheetTargetStatuses target 来源可选的状态。
var SheetTargetStatuses = []string{core.CoverageUntested, core.CoverageLearning}

// SheetPreviewItem 预览里的一个词。
type SheetPreviewItem struct {
	WordID       string                  `json:"wordId"`
	Spelling     string                  `json:"spelling"`
	Phonetic     *string                 `json:"phonetic"`
	PartOfSpeech *string                 `json:"partOfSpeech"`
	Definition   string                  `json:"definition"`
	Score        int                     `json:"score"`
	Reasons      []core.UnfamiliarReason `json:"reasons"`
	// Type 默写单预览才有：word | phrase（spec 0006）。
	Type string `json:"type,omitempty"`
}

// SheetPreviewResult POST /sheets/preview 的返回。
type SheetPreviewResult struct {
	Items []SheetPreviewItem `json:"items"`
	// Sentences 默写单预览才有（spec 0006）。
	Sentences *[]DictationSentencePreview `json:"sentences,omitempty"`
}

// PreviewSheet 预览选词（旧 previewSheet）。
func PreviewSheet(ctx context.Context, db *store.DB, loc *time.Location, now time.Time, userID string, count int, include []string, source *SheetSource) (*SheetPreviewResult, error) {
	now = msTime(now)
	picks, err := pickSheetWords(ctx, db, loc, now, userID, count, include, source)
	if err != nil {
		return nil, err
	}
	items, err := previewItems(ctx, db, picks, false)
	if err != nil {
		return nil, err
	}
	return &SheetPreviewResult{Items: items}, nil
}

// pickSheetWords 按来源选词（已按从难到易排好）。
func pickSheetWords(ctx context.Context, db *store.DB, loc *time.Location, now time.Time, userID string, count int, include []string, source *SheetSource) ([]core.UnfamiliarPick, error) {
	candidates, err := loadUnfamiliarCandidates(ctx, db, loc, now, userID)
	if err != nil {
		return nil, err
	}
	var picks []core.UnfamiliarPick
	kind := "unfamiliar"
	if source != nil {
		kind = source.Kind
	}
	switch kind {
	case "session":
		pool, err := sessionWrongWordIDs(ctx, db, source.SessionID)
		if err != nil {
			return nil, err
		}
		picks = core.SelectFromPool(pool, candidates, core.PoolOptions{Now: now, Count: count, KeepFamiliar: true, Tag: &core.UnfamiliarReason{Kind: "sessionWrong"}})
	case "unit":
		pool, err := unitWordIDsOrdered(ctx, db, source.UnitID)
		if err != nil {
			return nil, err
		}
		picks = core.SelectFromPool(pool, candidates, core.PoolOptions{Now: now, Count: count, KeepFamiliar: false})
	case "book":
		unitIDs, err := bookUnitIDsOrdered(ctx, db, source.BookID)
		if err != nil {
			return nil, err
		}
		pool, err := ScopeWordIDs(ctx, db, unitIDs)
		if err != nil {
			return nil, err
		}
		picks = core.SelectFromPool(pool, candidates, core.PoolOptions{Now: now, Count: count, KeepFamiliar: false})
	case "target":
		// 目标词按书序；未测 / 要学的词都是要正式测一次的，不按记忆排除已掌握的
		pool, _, err := targetWordIDs(ctx, db, loc, now, source.UseClasses, userID, source.BookID, source.Status)
		if err != nil {
			return nil, err
		}
		picks = core.SelectFromPool(pool, candidates, core.PoolOptions{Now: now, Count: count, KeepFamiliar: true})
	default:
		exclude, err := pendingSheetWordIDs(ctx, db, userID)
		if err != nil {
			return nil, err
		}
		picks = core.SelectUnfamiliarWords(candidates, core.SelectOptions{Now: now, Count: count, Exclude: exclude, Include: include})
	}
	return picks, nil
}

// previewItems 选词结果补上词的展示字段（词已不存在的跳过）。
func previewItems(ctx context.Context, db store.Querier, picks []core.UnfamiliarPick, withType bool) ([]SheetPreviewItem, error) {
	ids := make([]string, len(picks))
	for i, p := range picks {
		ids[i] = p.WordID
	}
	words, err := sheetWordDetails(ctx, db, ids)
	if err != nil {
		return nil, err
	}
	items := make([]SheetPreviewItem, 0, len(picks))
	for _, p := range picks {
		w, ok := words[p.WordID]
		if !ok {
			continue
		}
		reasons := p.Reasons
		if reasons == nil {
			reasons = []core.UnfamiliarReason{}
		}
		it := SheetPreviewItem{WordID: w.ID, Spelling: w.Spelling, Phonetic: w.Phonetic, PartOfSpeech: w.PartOfSpeech, Definition: w.Definition, Score: p.Score, Reasons: reasons}
		if withType {
			it.Type = wordItemType(w.Type)
		}
		items = append(items, it)
	}
	return items, nil
}

// wordItemType 词条在默写单里的题型：Word.type = phrase → 短语，其余 → 单词。
func wordItemType(wordType string) string {
	if wordType == core.DictPhrase {
		return core.DictPhrase
	}
	return core.DictWord
}

// SheetSourceItem 错词来源列表里的一项。
type SheetSourceItem struct {
	ID         string     `json:"id"`
	Kind       string     `json:"kind"`
	PlanName   string     `json:"planName"`
	DayKey     string     `json:"dayKey"`
	StartedAt  store.Time `json:"startedAt"`
	Status     string     `json:"status"`
	WrongCount int        `json:"wrongCount"`
}

// SheetSourcesResult GET /sheets/sources 的返回。
type SheetSourcesResult struct {
	Sessions []SheetSourceItem `json:"sessions"`
	// LearningSentences 「要学的句子」数量（默写单的句子来源，spec 0006）。
	LearningSentences int `json:"learningSentences"`
	// Texts 查询带 unitId 时：该单元的篇（句型清单 / 课文），供默写单选句子来源。
	Texts *[]SheetSourceText `json:"texts,omitempty"`
}

// SheetSources 错词来源列表：近 30 天有首次答错的学习组（检测、单词单测试、学习组），新的在前（旧 sheetSources）。
func SheetSources(ctx context.Context, q store.Querier, now time.Time, userID string) (*SheetSourcesResult, error) {
	since := now.Add(-time.Duration(SheetSourceDays) * 24 * time.Hour)
	args := append([]any{userID}, store.Args(sheetSourceKinds)...)
	args = append(args, store.NewTime(since))
	rows, err := q.QueryContext(ctx, `SELECT a."sessionId", count(DISTINCT a."wordId") FROM "Answer" a JOIN "StudySession" s ON s."id" = a."sessionId"
		WHERE a."userId" = ? AND `+wrongFirstWhere+` AND s."kind" IN (`+store.Placeholders(len(sheetSourceKinds))+`) AND s."startedAt" >= ?
		GROUP BY a."sessionId"`, args...)
	if err != nil {
		return nil, err
	}
	countBy := map[string]int{}
	ids := []string{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			rows.Close()
			return nil, err
		}
		countBy[id] = n
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return &SheetSourcesResult{Sessions: []SheetSourceItem{}}, nil
	}
	srows, err := q.QueryContext(ctx, `SELECT `+sessionCols+` FROM "StudySession" WHERE "id" IN (`+store.Placeholders(len(ids))+`) ORDER BY "startedAt" DESC, rowid DESC`, store.Args(ids)...)
	if err != nil {
		return nil, err
	}
	defer srows.Close()
	items := []SheetSourceItem{}
	for srows.Next() {
		s, err := scanSession(srows)
		if err != nil {
			return nil, err
		}
		snap, err := ParseSnapshot(s.SnapshotText)
		if err != nil {
			return nil, err
		}
		planName := ""
		if snap.PlanName != nil {
			planName = *snap.PlanName
		}
		items = append(items, SheetSourceItem{ID: s.ID, Kind: s.Kind, PlanName: planName, DayKey: s.DayKey, StartedAt: s.StartedAt, Status: s.Status, WrongCount: countBy[s.ID]})
	}
	if err := srows.Err(); err != nil {
		return nil, err
	}
	return &SheetSourcesResult{Sessions: items}, nil
}

// SheetSourceSessionOwner 学习组归属（路由做范围判定；旧 sessionOwner，消息与「学习组不存在」不同，专属单词单来源校验）。
func SheetSourceSessionOwner(ctx context.Context, q store.Querier, id string) (string, error) {
	var userID string
	err := q.QueryRowContext(ctx, `SELECT "userId" FROM "StudySession" WHERE "id" = ?`, id).Scan(&userID)
	if store.IsNoRows(err) {
		return "", httpx.NotFound("这次学习记录不存在")
	}
	return userID, err
}

// ------------------------------------------------------------------
// 生成 / 列表 / 明细 / 删除
// ------------------------------------------------------------------

// SheetCreateItem 生成结果里的一份。
type SheetCreateItem struct {
	ID  string `json:"id"`
	Seq int    `json:"seq"`
}

// SheetCreateResult POST /sheets 的返回（旧 { id, seq, items }：id/seq 是第一份的）。
type SheetCreateResult struct {
	ID    string            `json:"id"`
	Seq   int               `json:"seq"`
	Items []SheetCreateItem `json:"items"`
}

// CreateSheet 生成：wordIds 已按从难到易排好，按 份数 × 每份词数 切分（core.SplitSheets），
// 同一事务里连续取号；并发撞 (userId, seq) 唯一键时整批重试（K34）。返回第一份的 id/seq 与全部 items。
func CreateSheet(ctx context.Context, db *store.DB, now time.Time, userID, creatorID string, wordIDs, modesIn []string, copies, perSheet int) (*SheetCreateResult, error) {
	seen := map[string]bool{}
	ids := make([]string, 0, len(wordIDs))
	for _, id := range wordIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	groups, err := core.SplitSheets(ids, copies, perSheet)
	if err != nil {
		return nil, httpx.Validation(err.Error())
	}
	if len(groups) == 0 {
		return nil, httpx.Validation("至少选一个单词")
	}
	var found int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM "Word" WHERE "id" IN (`+store.Placeholders(len(ids))+`)`, store.Args(ids)...).Scan(&found); err != nil {
		return nil, err
	}
	if found != len(ids) {
		return nil, httpx.Validation("有单词不存在")
	}
	modes := normalizeSheetModes(modesIn)
	if len(modes) == 0 {
		return nil, httpx.Validation("至少选择一种题型")
	}
	rows := make([]sheetInsert, len(groups))
	for i, g := range groups {
		rows[i] = sheetInsert{format: core.SheetFormatSelftest, wordIDs: g, modes: modes}
	}
	return insertSheets(ctx, db, now, userID, creatorID, rows)
}

// sheetInsert 要插入的一份单词单。
type sheetInsert struct {
	format  string
	wordIDs []string
	modes   []string
	items   []core.DictationItem
}

// insertSheets 同一事务里连续取号插入多份；并发撞 (userId, seq) 唯一键时整批重试（K34）。
func insertSheets(ctx context.Context, db *store.DB, now time.Time, userID, creatorID string, sheets []sheetInsert) (*SheetCreateResult, error) {
	for attempt := 0; attempt < 5; attempt++ {
		var items []SheetCreateItem
		txErr := db.Tx(ctx, func(tx *sql.Tx) error {
			var base sql.NullInt64
			if err := tx.QueryRowContext(ctx, `SELECT max("seq") FROM "WordSheet" WHERE "userId" = ?`, userID).Scan(&base); err != nil {
				return err
			}
			start := 0
			if base.Valid {
				start = int(base.Int64)
			}
			items = make([]SheetCreateItem, 0, len(sheets))
			for i, g := range sheets {
				idsJSON, err := json.Marshal(store.JSON[[]string]{V: g.wordIDs})
				if err != nil {
					return err
				}
				modesJSON, err := json.Marshal(store.JSON[[]string]{V: g.modes})
				if err != nil {
					return err
				}
				itemsJSON, err := json.Marshal(store.JSON[[]core.DictationItem]{V: g.items})
				if err != nil {
					return err
				}
				id := store.NewID()
				seq := start + 1 + i
				if _, err := tx.ExecContext(ctx, `INSERT INTO "WordSheet" ("id","userId","creatorId","seq","wordIds","modes","format","items","createdAt") VALUES (?,?,?,?,?,?,?,?,?)`,
					id, userID, creatorID, seq, string(idsJSON), string(modesJSON), g.format, string(itemsJSON), store.NewTime(now)); err != nil {
					return err
				}
				items = append(items, SheetCreateItem{ID: id, Seq: seq})
			}
			return nil
		})
		if txErr == nil {
			return &SheetCreateResult{ID: items[0].ID, Seq: items[0].Seq, Items: items}, nil
		}
		if !store.IsUniqueViolation(txErr) {
			return nil, txErr
		}
	}
	return nil, httpx.NewError(httpx.CodeServer, "生成单词单失败，请重试", nil)
}

// SheetListFirstResult 列表项里首次交卷的成绩。
type SheetListFirstResult struct {
	SessionID string `json:"sessionId"`
	Correct   int    `json:"correct"`
	Total     int    `json:"total"`
}

// SheetListItem GET /sheets 列表的一项。
type SheetListItem struct {
	ID              string                `json:"id"`
	Seq             int                   `json:"seq"`
	WordCount       int                   `json:"wordCount"`
	CreatedAt       store.Time            `json:"createdAt"`
	CreatorName     string                `json:"creatorName"`
	Status          string                `json:"status"` // pending | testing | tested（默写单：pending 待批改 / tested 已批改）
	FirstResult     *SheetListFirstResult `json:"firstResult"`
	ActiveSessionID *string               `json:"activeSessionId"`
	// spec 0006：格式、题数（自测表 = 词数；默写单 = 全部题目）、默写单是否自批（只有已批改的默写单才有）。
	Format     string `json:"format"`
	ItemCount  int    `json:"itemCount"`
	SelfGraded *bool  `json:"selfGraded,omitempty"`
}

type sheetSessionBrief struct {
	id, status string
	result     *string
	snapshot   string
}

// sheetFirstResult 首次交卷（默写单：批改）的成绩：默写单的句子题一并计入。
func sheetFirstResult(b *sheetSessionBrief) (*SheetListFirstResult, error) {
	var res SessionResult
	if b.result != nil {
		if err := json.Unmarshal([]byte(*b.result), &res); err != nil {
			return nil, err
		}
	}
	out := &SheetListFirstResult{SessionID: b.id, Correct: res.CorrectFirst, Total: res.TotalFirst}
	if res.Sentences != nil {
		out.Correct += res.Sentences.Correct
		out.Total += res.Sentences.Total
	}
	return out, nil
}

// snapshotSelfGraded 默写单批改组快照里的自批标记（其他组为 nil）。
func snapshotSelfGraded(text string) (*bool, error) {
	snap, err := ParseSnapshot(text)
	if err != nil {
		return nil, err
	}
	return snap.SelfGraded, nil
}

// ListSheets 单词单列表（旧 listSheets），按编号倒序。
func ListSheets(ctx context.Context, q store.Querier, userID string, page, limit int) (httpx.Paginated[SheetListItem], error) {
	var total int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM "WordSheet" WHERE "userId" = ?`, userID).Scan(&total); err != nil {
		return httpx.Paginated[SheetListItem]{}, err
	}
	rows, err := q.QueryContext(ctx, `SELECT w."id", w."seq", w."wordIds", w."createdAt", u."name", w."format", w."items"
		FROM "WordSheet" w JOIN "User" u ON u."id" = w."creatorId"
		WHERE w."userId" = ? ORDER BY w."seq" DESC LIMIT ? OFFSET ?`, userID, limit, (page-1)*limit)
	if err != nil {
		return httpx.Paginated[SheetListItem]{}, err
	}
	type row struct {
		id, creatorName, format   string
		seq, wordCount, itemCount int
		createdAt                 store.Time
	}
	list := []row{}
	for rows.Next() {
		var r row
		var wordIDs store.JSON[[]string]
		var items store.JSON[[]core.DictationItem]
		if err := rows.Scan(&r.id, &r.seq, &wordIDs, &r.createdAt, &r.creatorName, &r.format, &items); err != nil {
			rows.Close()
			return httpx.Paginated[SheetListItem]{}, err
		}
		r.wordCount = len(wordIDs.V)
		r.itemCount = r.wordCount
		if r.format == core.SheetFormatDictation {
			r.itemCount = len(items.V)
		}
		list = append(list, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return httpx.Paginated[SheetListItem]{}, err
	}

	ids := make([]string, len(list))
	for i, r := range list {
		ids[i] = r.id
	}
	sessionsBySheet := map[string][]sheetSessionBrief{}
	if len(ids) > 0 {
		srows, err := q.QueryContext(ctx, `SELECT "sheetId","id","status","result","snapshot" FROM "StudySession" WHERE "sheetId" IN (`+store.Placeholders(len(ids))+`) ORDER BY "startedAt", rowid`, store.Args(ids)...)
		if err != nil {
			return httpx.Paginated[SheetListItem]{}, err
		}
		for srows.Next() {
			var sheetID string
			var b sheetSessionBrief
			if err := srows.Scan(&sheetID, &b.id, &b.status, &b.result, &b.snapshot); err != nil {
				srows.Close()
				return httpx.Paginated[SheetListItem]{}, err
			}
			sessionsBySheet[sheetID] = append(sessionsBySheet[sheetID], b)
		}
		if err := srows.Err(); err != nil {
			srows.Close()
			return httpx.Paginated[SheetListItem]{}, err
		}
		srows.Close()
	}

	items := make([]SheetListItem, 0, len(list))
	for _, r := range list {
		var first, active *sheetSessionBrief
		for i, s := range sessionsBySheet[r.id] {
			if s.status == "completed" && first == nil {
				first = &sessionsBySheet[r.id][i]
			}
			if s.status == "active" && active == nil {
				active = &sessionsBySheet[r.id][i]
			}
		}
		status := "pending"
		var firstResult *SheetListFirstResult
		var selfGraded *bool
		if first != nil {
			status = "tested"
			var err error
			if firstResult, err = sheetFirstResult(first); err != nil {
				return httpx.Paginated[SheetListItem]{}, err
			}
			if r.format == core.SheetFormatDictation {
				if selfGraded, err = snapshotSelfGraded(first.snapshot); err != nil {
					return httpx.Paginated[SheetListItem]{}, err
				}
			}
		} else if active != nil {
			status = "testing"
		}
		var activeID *string
		if active != nil {
			activeID = &active.id
		}
		items = append(items, SheetListItem{ID: r.id, Seq: r.seq, WordCount: r.wordCount, CreatedAt: r.createdAt, CreatorName: r.creatorName, Status: status, FirstResult: firstResult, ActiveSessionID: activeID,
			Format: r.format, ItemCount: r.itemCount, SelfGraded: selfGraded})
	}
	return httpx.Page(items, total, page, limit), nil
}

// SheetDetailWord 明细里的一个词。
type SheetDetailWord struct {
	WordID       string  `json:"wordId"`
	Spelling     string  `json:"spelling"`
	Phonetic     *string `json:"phonetic"`
	PartOfSpeech *string `json:"partOfSpeech"`
	Definition   string  `json:"definition"`
}

// SheetStudent 明细里的学生（旧 { id, name }）。
type SheetStudent struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// SheetDetailView GET /sheets/:id（UserID 只供路由做范围判定，不下发，旧路由会 omit userId）。
type SheetDetailView struct {
	ID        string            `json:"id"`
	UserID    string            `json:"-"`
	Seq       int               `json:"seq"`
	CreatedAt store.Time        `json:"createdAt"`
	Student   SheetStudent      `json:"student"`
	Modes     []string          `json:"modes"`
	Words     []SheetDetailWord `json:"words"`
	// spec 0006：格式；默写单的题目（含中文提示、答案、转换题的原句；自测表为 []）；批改结果（未批改或自测表为 null）。
	Format  string              `json:"format"`
	Items   []DictationItemView `json:"items"`
	Grading *SheetGrading       `json:"grading"`
}

// SheetDetail 明细（旧 sheetDetail）。
func SheetDetail(ctx context.Context, q store.Querier, id string) (*SheetDetailView, error) {
	var v SheetDetailView
	var wordIDs store.JSON[[]string]
	var modes store.JSON[[]string]
	var items store.JSON[[]core.DictationItem]
	err := q.QueryRowContext(ctx, `SELECT w."id", w."userId", w."seq", w."createdAt", w."wordIds", w."modes", u."id", u."name", w."format", w."items"
		FROM "WordSheet" w JOIN "User" u ON u."id" = w."userId" WHERE w."id" = ?`, id).
		Scan(&v.ID, &v.UserID, &v.Seq, &v.CreatedAt, &wordIDs, &modes, &v.Student.ID, &v.Student.Name, &v.Format, &items)
	if store.IsNoRows(err) {
		return nil, httpx.NotFound("单词单不存在")
	}
	if err != nil {
		return nil, err
	}
	v.Modes = modes.V
	words, err := sheetWordDetails(ctx, q, wordIDs.V)
	if err != nil {
		return nil, err
	}
	v.Words = make([]SheetDetailWord, 0, len(wordIDs.V))
	for _, wid := range wordIDs.V {
		w, ok := words[wid]
		if !ok {
			continue
		}
		v.Words = append(v.Words, SheetDetailWord{WordID: w.ID, Spelling: w.Spelling, Phonetic: w.Phonetic, PartOfSpeech: w.PartOfSpeech, Definition: w.Definition})
	}
	v.Items = []DictationItemView{}
	if v.Format == core.SheetFormatDictation {
		if v.Items, err = dictationItemViews(ctx, q, items.V); err != nil {
			return nil, err
		}
		if v.Grading, err = sheetGrading(ctx, q, v.ID, v.Items); err != nil {
			return nil, err
		}
	}
	return &v, nil
}

// SheetOwnerView 归属信息（旧 sheetOwner）。
type SheetOwnerView struct {
	UserID    string
	CreatorID string
}

// SheetOwner 单词单归属（路由做范围与删除权限判定）。
func SheetOwner(ctx context.Context, q store.Querier, id string) (*SheetOwnerView, error) {
	var v SheetOwnerView
	err := q.QueryRowContext(ctx, `SELECT "userId","creatorId" FROM "WordSheet" WHERE "id" = ?`, id).Scan(&v.UserID, &v.CreatorID)
	if store.IsNoRows(err) {
		return nil, httpx.NotFound("单词单不存在")
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// DeleteSheet 只能删从未开过测试的单子（开过测的有作答事实，不能抹掉；旧 deleteSheet）。
func DeleteSheet(ctx context.Context, q store.Querier, id string) error {
	var n int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM "StudySession" WHERE "sheetId" = ?`, id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return httpx.NewError(httpx.CodeInvalidAction, "已经开始测试的单词单不能删除", nil)
	}
	_, err := q.ExecContext(ctx, `DELETE FROM "WordSheet" WHERE "id" = ?`, id)
	return err
}
