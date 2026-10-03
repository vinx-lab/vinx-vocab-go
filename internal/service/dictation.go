package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/vinx-lab/vinx-vocab-go/internal/core"
	"github.com/vinx-lab/vinx-vocab-go/internal/httpx"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// 默写单（spec 0006）：出题（词 + 句子来源）、生成、明细、批改。规则在 internal/core/dictation.go。
//
// 批改：新建一个已完成的 kind = sheet 学习组（直接插入为 completed，避开 StudySession_active_uniq，
// 学生同时有一张自测单在线测试也不冲突），词写 Answer（mode = dictation、phase = test、attempt = 1）后
// 走与 CompleteSessionTx 相同的结算（K10：学过的词对 → Good、错 → Again，没学过的只记成绩；K19 照常），
// 句子写 SentenceAnswer（不进 FSRS）。

// ------------------------------------------------------------------
// 句子
// ------------------------------------------------------------------

// dictSentence 出题需要的句子字段（含转换题的原句）。
type dictSentence struct {
	ID, En, Cn, Source           string
	Frame, OriginID, VariantNote *string
	OriginEn, OriginCn           *string
}

func (s *dictSentence) defaultType() string {
	return core.DictationSentenceType(s.Source, s.Frame, s.VariantNote, s.OriginID)
}

func (s *dictSentence) prompt(t string) string {
	in := core.DictationPromptInput{Type: t, Cn: s.Cn, Frame: s.Frame, VariantNote: s.VariantNote}
	if s.OriginEn != nil {
		in.OriginEn = *s.OriginEn
	}
	return core.DictationPrompt(in)
}

// loadDictSentences 按 id 批量取句子。
func loadDictSentences(ctx context.Context, q store.Querier, ids []string) (map[string]*dictSentence, error) {
	out := map[string]*dictSentence{}
	for i := 0; i < len(ids); i += 500 {
		chunk := ids[i:min(i+500, len(ids))]
		rows, err := q.QueryContext(ctx, `SELECT s."id", s."en", s."cn", s."source", s."frame", s."originId", s."variantNote", o."en", o."cn"
			FROM "Sentence" s LEFT JOIN "Sentence" o ON o."id" = s."originId" WHERE s."id" IN (`+store.Placeholders(len(chunk))+`)`, store.Args(chunk)...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var s dictSentence
			if err := rows.Scan(&s.ID, &s.En, &s.Cn, &s.Source, &s.Frame, &s.OriginID, &s.VariantNote, &s.OriginEn, &s.OriginCn); err != nil {
				rows.Close()
				return nil, err
			}
			out[s.ID] = &s
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// sentenceAnswerFacts 该学生在这些句子上的全部作答（ids 为 nil 表示全部句子），按句子分组、按时间先后。
func sentenceAnswerFacts(ctx context.Context, q store.Querier, userID string, ids []string) (map[string][]core.SentenceAnswerFact, error) {
	out := map[string][]core.SentenceAnswerFact{}
	scan := func(query string, args ...any) error {
		rows, err := q.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			var f core.SentenceAnswerFact
			var at store.Time
			if err := rows.Scan(&id, &f.Correct, &at); err != nil {
				return err
			}
			f.At = at.Time
			out[id] = append(out[id], f)
		}
		return rows.Err()
	}
	const base = `SELECT "sentenceId","correct","createdAt" FROM "SentenceAnswer" WHERE "userId" = ?`
	if ids == nil {
		return out, scan(base+` ORDER BY "createdAt", rowid`, userID)
	}
	for i := 0; i < len(ids); i += 500 {
		chunk := ids[i:min(i+500, len(ids))]
		if err := scan(base+` AND "sentenceId" IN (`+store.Placeholders(len(chunk))+`) ORDER BY "createdAt", rowid`, append([]any{userID}, store.Args(chunk)...)...); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// SentenceStatuses 句子状态（未测 / 要学 / 会了，core.SentenceStatus）。
func SentenceStatuses(ctx context.Context, q store.Querier, userID string, ids []string) (map[string]core.CoverageStatus, error) {
	facts, err := sentenceAnswerFacts(ctx, q, userID, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[string]core.CoverageStatus, len(ids))
	for _, id := range ids {
		out[id] = core.SentenceStatus(facts[id])
	}
	return out, nil
}

// LearningSentenceIDs 「要学的句子」：最近一次作答写错的句子，最近写错的在前。
func LearningSentenceIDs(ctx context.Context, q store.Querier, userID string) ([]string, error) {
	facts, err := sentenceAnswerFacts(ctx, q, userID, nil)
	if err != nil {
		return nil, err
	}
	type item struct {
		id   string
		last time.Time
	}
	list := []item{}
	for id, fs := range facts {
		if core.SentenceStatus(fs) != core.CoverageLearning {
			continue
		}
		last := fs[0].At
		for _, f := range fs[1:] {
			if f.At.After(last) {
				last = f.At
			}
		}
		list = append(list, item{id, last})
	}
	sort.Slice(list, func(i, j int) bool {
		if !list[i].last.Equal(list[j].last) {
			return list[i].last.After(list[j].last)
		}
		return list[i].id < list[j].id
	})
	out := make([]string, len(list))
	for i, it := range list {
		out[i] = it.id
	}
	return out, nil
}

// ------------------------------------------------------------------
// 来源
// ------------------------------------------------------------------

// 默写单的句子来源。
const (
	SentenceSourceText     = "text"     // 单元的一篇（句型清单 / 课文，含仿写）
	SentenceSourceLearning = "learning" // 要学的句子
	SentenceSourcePassage  = "passage"  // 学生自己的 AI 短文
	SentenceSourceSession  = "session"  // 某次默写批改的错句（用错题再出一份）
)

// SentenceSourceKinds 句子来源全集（接口校验用）。
var SentenceSourceKinds = []string{SentenceSourceText, SentenceSourceLearning, SentenceSourcePassage, SentenceSourceSession}

// SentenceSource 一个句子来源。
type SentenceSource struct {
	Kind      string
	TextID    string
	PassageID string
	SessionID string
}

// sentenceSourceIDs 来源里的句子 id（按来源内顺序）。
func sentenceSourceIDs(ctx context.Context, q store.Querier, userID string, src SentenceSource) ([]string, error) {
	switch src.Kind {
	case SentenceSourceText:
		return queryStrings(ctx, q, `SELECT "sentenceId" FROM "UnitTextSentence" WHERE "textId" = ? ORDER BY "paragraph", "sortOrder", rowid`, src.TextID)
	case SentenceSourcePassage:
		return queryStrings(ctx, q, `SELECT "sentenceId" FROM "PassageSentence" WHERE "passageId" = ? ORDER BY "paragraph", "sortOrder", rowid`, src.PassageID)
	case SentenceSourceSession:
		return queryStrings(ctx, q, `SELECT "sentenceId" FROM "SentenceAnswer" WHERE "sessionId" = ? AND "correct" = 0 ORDER BY "createdAt", rowid`, src.SessionID)
	case SentenceSourceLearning:
		return LearningSentenceIDs(ctx, q, userID)
	}
	return nil, httpx.Validation("未知的句子来源")
}

// SheetSourceText GET /sheets/sources?unitId= 里单元的一篇。
type SheetSourceText struct {
	ID            string  `json:"id"`
	Kind          string  `json:"kind"` // list（句型清单，含仿写）| text（课文）
	Title         string  `json:"title"`
	TitleCn       *string `json:"titleCn"`
	SentenceCount int     `json:"sentenceCount"`
	VariantCount  int     `json:"variantCount"` // 其中的仿写句
}

// UnitSourceTexts 单元的篇及句子数（按篇的顺序）。
func UnitSourceTexts(ctx context.Context, q store.Querier, unitID string) ([]SheetSourceText, error) {
	rows, err := q.QueryContext(ctx, `SELECT t."id", t."kind", t."title", t."titleCn", count(uts."sentenceId"), coalesce(sum(s."source" = 'variant'), 0)
		FROM "UnitText" t LEFT JOIN "UnitTextSentence" uts ON uts."textId" = t."id" LEFT JOIN "Sentence" s ON s."id" = uts."sentenceId"
		WHERE t."unitId" = ? GROUP BY t."id" ORDER BY t."sortOrder", t."createdAt", t."id"`, unitID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SheetSourceText{}
	for rows.Next() {
		var t SheetSourceText
		if err := rows.Scan(&t.ID, &t.Kind, &t.Title, &t.TitleCn, &t.SentenceCount, &t.VariantCount); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ------------------------------------------------------------------
// 预览
// ------------------------------------------------------------------

// DictationPreviewInput 默写单预览的参数。
type DictationPreviewInput struct {
	Copies          int
	Limits          core.DictationLimits
	IncludeWords    bool
	IncludePhrases  bool
	Include         []string
	Source          *SheetSource
	SentenceSources []SentenceSource
}

// DictationSentencePreview 预览里的一个句子。
type DictationSentencePreview struct {
	SentenceID string              `json:"sentenceId"`
	Type       string              `json:"type"`
	En         string              `json:"en"`
	Cn         string              `json:"cn"`
	Frame      *string             `json:"frame"`
	Prompt     string              `json:"prompt"`
	Answer     string              `json:"answer"`
	Status     core.CoverageStatus `json:"status"`
}

// PreviewDictation 默写单预览：词按选词来源（不熟的词 / 错词 / 单元 / 整本书 / 目标）挑出后按单词、短语分别截到
// 份数 × 每份上限；句子按来源顺序收集后要学的优先、其次没测过的，截到 份数 × 每份上限。
func PreviewDictation(ctx context.Context, db *store.DB, loc *time.Location, now time.Time, userID string, in DictationPreviewInput) (*SheetPreviewResult, error) {
	now = msTime(now)
	items := []SheetPreviewItem{}
	if in.IncludeWords || in.IncludePhrases {
		picks, err := pickSheetWords(ctx, db, loc, now, userID, core.SheetMax, in.Include, in.Source)
		if err != nil {
			return nil, err
		}
		all, err := previewItems(ctx, db, picks, true)
		if err != nil {
			return nil, err
		}
		words, phrases := 0, 0
		for _, it := range all {
			switch {
			case it.Type == core.DictWord && in.IncludeWords && words < in.Copies*in.Limits.Words:
				words++
				items = append(items, it)
			case it.Type == core.DictPhrase && in.IncludePhrases && phrases < in.Copies*in.Limits.Phrases:
				phrases++
				items = append(items, it)
			}
		}
	}

	ids := []string{}
	for _, src := range in.SentenceSources {
		got, err := sentenceSourceIDs(ctx, db, userID, src)
		if err != nil {
			return nil, err
		}
		ids = append(ids, got...)
	}
	statuses, err := SentenceStatuses(ctx, db, userID, ids)
	if err != nil {
		return nil, err
	}
	cands := make([]core.SentenceCandidate, len(ids))
	for i, id := range ids {
		cands[i] = core.SentenceCandidate{ID: id, Status: statuses[id]}
	}
	ordered := core.OrderDictationSentences(cands)
	sents, err := loadDictSentences(ctx, db, ordered)
	if err != nil {
		return nil, err
	}
	limit := in.Copies * in.Limits.Sentences
	out := []DictationSentencePreview{}
	for _, id := range ordered {
		if len(out) >= limit {
			break
		}
		s, ok := sents[id]
		if !ok {
			continue
		}
		t := s.defaultType()
		out = append(out, DictationSentencePreview{SentenceID: s.ID, Type: t, En: s.En, Cn: s.Cn, Frame: s.Frame, Prompt: s.prompt(t), Answer: s.En, Status: statuses[id]})
	}
	return &SheetPreviewResult{Items: items, Sentences: &out}, nil
}

// ------------------------------------------------------------------
// 生成
// ------------------------------------------------------------------

// CreateDictationSheet 生成默写单：题目去重后按 份数 × 各题型每份上限 切分（core.SplitDictation），同一事务连续取号。
// wordIds 列写入每份里的词（单词、短语），沿用「待测单子上的词」「词数」等现有查询。
func CreateDictationSheet(ctx context.Context, db *store.DB, now time.Time, userID, creatorID string, items []core.DictationItem, copies int, lim core.DictationLimits) (*SheetCreateResult, error) {
	items = core.DedupeDictationItems(items)
	if len(items) == 0 {
		return nil, httpx.Validation("至少选一道题")
	}
	var wordIDs, sentIDs []string
	for _, it := range items {
		if core.IsSentenceItem(it.Type) {
			sentIDs = append(sentIDs, it.SentenceID)
		} else {
			wordIDs = append(wordIDs, it.WordID)
		}
	}
	words, err := sheetWordDetails(ctx, db, wordIDs)
	if err != nil {
		return nil, err
	}
	if len(words) != len(wordIDs) {
		return nil, httpx.Validation("有单词不存在")
	}
	sents, err := loadDictSentences(ctx, db, sentIDs)
	if err != nil {
		return nil, err
	}
	if len(sents) != len(sentIDs) {
		return nil, httpx.Validation("有句子不存在")
	}
	for _, it := range items {
		if !core.IsSentenceItem(it.Type) {
			continue
		}
		s := sents[it.SentenceID]
		if !core.DictationSentenceTypeAllowed(it.Type, s.Source, s.Frame, s.VariantNote, s.OriginID) {
			return nil, httpx.Validation(fmt.Sprintf("句子「%s」不能出成%s题", s.En, dictTypeLabel[it.Type]))
		}
	}
	groups, err := core.SplitDictation(items, copies, lim)
	if err != nil {
		return nil, httpx.Validation(err.Error())
	}
	rows := make([]sheetInsert, len(groups))
	for i, g := range groups {
		ws := []string{}
		for _, it := range g {
			if !core.IsSentenceItem(it.Type) {
				ws = append(ws, it.WordID)
			}
		}
		rows[i] = sheetInsert{format: core.SheetFormatDictation, wordIDs: ws, modes: []string{}, items: g}
	}
	return insertSheets(ctx, db, now, userID, creatorID, rows)
}

var dictTypeLabel = map[string]string{
	core.DictWord: "单词", core.DictPhrase: "短语", core.DictSentence: "句子", core.DictFrame: "仿写", core.DictTransform: "转换",
}

// ------------------------------------------------------------------
// 明细
// ------------------------------------------------------------------

// SentenceRef 句子的英文与中文（转换题的原句）。
type SentenceRef struct {
	ID string `json:"id"`
	En string `json:"en"`
	Cn string `json:"cn"`
}

// DictationItemView 明细里的一道题。Index 是在默写单 items 里的下标（批改时按它提交）；
// 词或句子已被删除的题不出现。
type DictationItemView struct {
	Index      int          `json:"index"`
	Type       string       `json:"type"`
	Section    int          `json:"section"` // 1 单词、2 短语、3 句子、4 仿写与转换
	WordID     string       `json:"wordId,omitempty"`
	SentenceID string       `json:"sentenceId,omitempty"`
	Prompt     string       `json:"prompt"`
	Answer     string       `json:"answer"`
	Cn         string       `json:"cn,omitempty"`     // 句子题的中文
	Origin     *SentenceRef `json:"origin,omitempty"` // 转换题的原句
}

// dictationItemViews 题目 → 卷面提示与答案。
func dictationItemViews(ctx context.Context, q store.Querier, items []core.DictationItem) ([]DictationItemView, error) {
	var wordIDs, sentIDs []string
	for _, it := range items {
		if core.IsSentenceItem(it.Type) {
			sentIDs = append(sentIDs, it.SentenceID)
		} else {
			wordIDs = append(wordIDs, it.WordID)
		}
	}
	words, err := sheetWordDetails(ctx, q, wordIDs)
	if err != nil {
		return nil, err
	}
	sents, err := loadDictSentences(ctx, q, sentIDs)
	if err != nil {
		return nil, err
	}
	out := make([]DictationItemView, 0, len(items))
	for i, it := range items {
		v := DictationItemView{Index: i, Type: it.Type, Section: core.DictationSection(it.Type)}
		if core.IsSentenceItem(it.Type) {
			s, ok := sents[it.SentenceID]
			if !ok {
				continue
			}
			v.SentenceID, v.Prompt, v.Answer, v.Cn = s.ID, s.prompt(it.Type), s.En, s.Cn
			if it.Type == core.DictTransform && s.OriginID != nil && s.OriginEn != nil {
				v.Origin = &SentenceRef{ID: *s.OriginID, En: *s.OriginEn, Cn: derefStr(s.OriginCn)}
			}
		} else {
			w, ok := words[it.WordID]
			if !ok {
				continue
			}
			v.WordID, v.Answer = w.ID, w.Spelling
			v.Prompt = core.DictationPrompt(core.DictationPromptInput{Type: it.Type, PartOfSpeech: w.PartOfSpeech, Definition: w.Definition})
		}
		out = append(out, v)
	}
	return out, nil
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// GradeResultView 批改结果里的一道题。
type GradeResultView struct {
	Index      int     `json:"index"`
	Correct    bool    `json:"correct"`
	UserAnswer *string `json:"userAnswer"`
}

// SheetGrading 默写单的批改结果（成绩单）。
type SheetGrading struct {
	SessionID  string            `json:"sessionId"`
	GradedAt   store.NullTime    `json:"gradedAt"`
	GradedBy   SnapshotUser      `json:"gradedBy"`
	SelfGraded bool              `json:"selfGraded"`
	Correct    int               `json:"correct"`
	Total      int               `json:"total"`
	Results    []GradeResultView `json:"results"`
}

type gradedAnswer struct {
	correct    bool
	userAnswer *string
}

// sheetGrading 默写单的批改结果；未批改返回 nil。
func sheetGrading(ctx context.Context, q store.Querier, sheetID string, views []DictationItemView) (*SheetGrading, error) {
	var g SheetGrading
	var snapText string
	err := q.QueryRowContext(ctx, `SELECT "id","snapshot","completedAt" FROM "StudySession" WHERE "sheetId" = ? AND "status" = 'completed' ORDER BY "startedAt", rowid LIMIT 1`, sheetID).
		Scan(&g.SessionID, &snapText, &g.GradedAt)
	if store.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	snap, err := ParseSnapshot(snapText)
	if err != nil {
		return nil, err
	}
	if snap.GradedBy != nil {
		g.GradedBy = *snap.GradedBy
	}
	if snap.SelfGraded != nil {
		g.SelfGraded = *snap.SelfGraded
	}
	load := func(query string) (map[string]gradedAnswer, error) {
		rows, err := q.QueryContext(ctx, query, g.SessionID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := map[string]gradedAnswer{}
		for rows.Next() {
			var id string
			var a gradedAnswer
			if err := rows.Scan(&id, &a.correct, &a.userAnswer); err != nil {
				return nil, err
			}
			out[id] = a
		}
		return out, rows.Err()
	}
	wordAns, err := load(`SELECT "wordId","correct","userAnswer" FROM "Answer" WHERE "sessionId" = ? AND "mode" = 'dictation' AND "phase" = 'test' AND "attempt" = 1`)
	if err != nil {
		return nil, err
	}
	sentAns, err := load(`SELECT "sentenceId","correct","userAnswer" FROM "SentenceAnswer" WHERE "sessionId" = ?`)
	if err != nil {
		return nil, err
	}
	g.Results = []GradeResultView{}
	for _, v := range views {
		a, ok := wordAns[v.WordID]
		if v.SentenceID != "" {
			a, ok = sentAns[v.SentenceID]
		}
		if !ok {
			continue
		}
		g.Results = append(g.Results, GradeResultView{Index: v.Index, Correct: a.correct, UserAnswer: a.userAnswer})
		g.Total++
		if a.correct {
			g.Correct++
		}
	}
	return &g, nil
}

// ------------------------------------------------------------------
// 批改
// ------------------------------------------------------------------

// normalizeUserAnswer 记下的学生答案：去掉首尾空白，空串记为 NULL，最多 200 个字符。
func normalizeUserAnswer(s *string) *string {
	if s == nil {
		return nil
	}
	t := strings.TrimSpace(*s)
	if t == "" {
		return nil
	}
	t = sliceUTF16(t, 200)
	return &t
}

// GradeSheet 提交默写单的批改（只能提交一次；权限由路由判定：能查看该学生的人）。
// 学生本人的账号提交的标记为自批。返回批改后的明细（含成绩单）。
func GradeSheet(ctx context.Context, db *store.DB, loc *time.Location, now time.Time, grader *Actor, sheetID string, results []core.GradeResult) (*SheetDetailView, error) {
	now = msTime(now)
	day, _, _ := TodayRange(loc, now)
	var detail *SheetDetailView
	err := db.Tx(ctx, func(tx *sql.Tx) error {
		var userID, format string
		var seq int
		var items store.JSON[[]core.DictationItem]
		err := tx.QueryRowContext(ctx, `SELECT "userId","seq","format","items" FROM "WordSheet" WHERE "id" = ?`, sheetID).Scan(&userID, &seq, &format, &items)
		if store.IsNoRows(err) {
			return httpx.NotFound("单词单不存在")
		}
		if err != nil {
			return err
		}
		if format != core.SheetFormatDictation {
			return httpx.NewError(httpx.CodeInvalidAction, "只有默写单需要批改", nil)
		}
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM "StudySession" WHERE "sheetId" = ? AND "status" = 'completed'`, sheetID).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return httpx.NewError(httpx.CodeInvalidStatus, "这份默写单已经批改过", nil).WithStatus(409)
		}
		views, err := dictationItemViews(ctx, tx, items.V)
		if err != nil {
			return err
		}
		indexes := make([]int, len(views))
		for i, v := range views {
			indexes[i] = v.Index
		}
		sorted, err := core.ValidateGradeResults(indexes, results)
		if err != nil {
			return httpx.Validation(err.Error())
		}
		byIndex := make(map[int]core.GradeResult, len(sorted))
		for _, r := range sorted {
			byIndex[r.Index] = r
		}
		var graderName string
		if err := tx.QueryRowContext(ctx, `SELECT "name" FROM "User" WHERE "id" = ?`, grader.ID).Scan(&graderName); err != nil && !store.IsNoRows(err) {
			return err
		}

		// 快照：词按卷面顺序（结算用），句子题单独记下
		var wordIDs []string
		var sentences []SnapshotSentence
		for _, v := range views {
			if v.SentenceID != "" {
				sentences = append(sentences, SnapshotSentence{Index: v.Index, SentenceID: v.SentenceID, Type: v.Type, En: v.Answer, Cn: v.Cn, Prompt: v.Prompt})
			} else {
				wordIDs = append(wordIDs, v.WordID)
			}
		}
		name := fmt.Sprintf("默写单 #%d", seq)
		snap, err := BuildSnapshot(ctx, tx, nil, wordIDs, SnapshotOptions{PlanName: &name, Modes: []string{core.DictationMode}})
		if err != nil {
			return err
		}
		f := core.SheetFormatDictation
		self := core.IsSelfGraded(grader.ID, userID)
		snap.Format, snap.GradedBy, snap.SelfGraded, snap.Sentences = &f, &SnapshotUser{ID: grader.ID, Name: graderName}, &self, sentences
		snapText, err := json.Marshal(snap)
		if err != nil {
			return err
		}
		sessionID := store.NewID()
		ts := store.NewTime(now)
		if _, err := tx.ExecContext(ctx, `INSERT INTO "StudySession" ("id","userId","planId","sheetId","kind","wordCount","status","dayKey","snapshot","startedAt","completedAt","completedDay")
			VALUES (?,?,NULL,?,'sheet',?,'completed',?,?,?,?,?)`, sessionID, userID, sheetID, len(snap.Items), day, string(snapText), ts, ts, day); err != nil {
			return err
		}

		sentRes := &SentenceResult{WrongSentenceIDs: []string{}}
		for _, v := range views {
			r := byIndex[v.Index]
			ua := normalizeUserAnswer(r.UserAnswer)
			if v.SentenceID != "" {
				if _, err := tx.ExecContext(ctx, `INSERT INTO "SentenceAnswer" ("id","sessionId","userId","sentenceId","itemType","correct","userAnswer","dayKey","createdAt") VALUES (?,?,?,?,?,?,?,?,?)`,
					store.NewID(), sessionID, userID, v.SentenceID, v.Type, r.Correct, ua, day, ts); err != nil {
					return err
				}
				sentRes.Total++
				if r.Correct {
					sentRes.Correct++
				} else {
					sentRes.WrongSentenceIDs = append(sentRes.WrongSentenceIDs, v.SentenceID)
				}
				continue
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO "Answer" ("id","sessionId","userId","wordId","mode","phase","attempt","correct","userAnswer","hintUsed","dontKnow","durationMs","dayKey","createdAt")
				VALUES (?,?,?,?,?,'test',1,?,?,0,0,0,?,?)`, store.NewID(), sessionID, userID, v.WordID, core.DictationMode, r.Correct, ua, day, ts); err != nil {
				return err
			}
		}

		s, err := GetSession(ctx, tx, sessionID)
		if err != nil {
			return err
		}
		if _, err := settleSessionTx(ctx, tx, loc, now, s, func(res *SessionResult) { res.Sentences = sentRes }); err != nil {
			return err
		}
		detail, err = SheetDetail(ctx, tx, sheetID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return detail, nil
}

// sessionSentenceDetails 学习组详情里的句子题（按卷面顺序）与对错。
func sessionSentenceDetails(ctx context.Context, q store.Querier, sessionID string, sents []SnapshotSentence) ([]SessionDetailSentence, error) {
	rows, err := q.QueryContext(ctx, `SELECT "sentenceId","correct","userAnswer" FROM "SentenceAnswer" WHERE "sessionId" = ?`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ans := map[string]gradedAnswer{}
	for rows.Next() {
		var id string
		var a gradedAnswer
		if err := rows.Scan(&id, &a.correct, &a.userAnswer); err != nil {
			return nil, err
		}
		ans[id] = a
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]SessionDetailSentence, 0, len(sents))
	for _, s := range sents {
		a := ans[s.SentenceID]
		out = append(out, SessionDetailSentence{Index: s.Index, SentenceID: s.SentenceID, Type: s.Type, En: s.En, Cn: s.Cn, Prompt: s.Prompt, Correct: a.correct, UserAnswer: a.userAnswer})
	}
	return out, nil
}
