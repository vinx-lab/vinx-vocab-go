package service

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"time"

	vinxvocab "github.com/vinx-lab/vinx-vocab-go"
	"github.com/vinx-lab/vinx-vocab-go/internal/core"
	"github.com/vinx-lab/vinx-vocab-go/internal/core/lemma"
	"github.com/vinx-lab/vinx-vocab-go/internal/httpx"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// 句子（spec 0004）：词 ← 句 ← 篇。句子是正本；Word.example 与 Passage.body 保留并同步写入。

// 句子来源。
const (
	SentenceImport  = "import"
	SentenceManual  = "manual"
	SentenceExample = "example"
	SentenceAI      = "ai"
	SentenceVariant = "variant"
)

func init() {
	// 0003_sentences 的数据迁移：例句转句子、AI 短文逐句拆分、计算词的关联（同一事务）。
	store.RegisterMigrationHook("0003_sentences", BackfillSentences)
	// 每次打开数据库：从旧版导入（vinx-vocab import）只整库替换旧版的表，不经过业务层，
	// 导入后的第一次打开在这里补齐例句句子、短文逐句结构，并清掉没有篇引用的句子。
	store.RegisterOpenHook(RepairSentences)
}

// orphanSentenceCond 句子已经没有任何篇引用（例句跟随词条，不算孤儿）。PruneSentences 与 RepairSentences 共用。
const orphanSentenceCond = `"Sentence"."source" != 'example'
	AND NOT EXISTS (SELECT 1 FROM "UnitTextSentence" uts WHERE uts."sentenceId" = "Sentence"."id")
	AND NOT EXISTS (SELECT 1 FROM "PassageSentence" ps WHERE ps."sentenceId" = "Sentence"."id")`

// sentencesNeedRepair 是否有要补齐或清理的句子（只读，决定要不要开写事务）。
const sentencesNeedRepair = `SELECT
	EXISTS (SELECT 1 FROM "Word" w WHERE w."example" IS NOT NULL AND trim(w."example") != ''
		AND NOT EXISTS (SELECT 1 FROM "Sentence" s WHERE s."source" = 'example' AND s."wordId" = w."id"))
	OR EXISTS (SELECT 1 FROM "Passage" p WHERE NOT EXISTS (SELECT 1 FROM "PassageSentence" ps WHERE ps."passageId" = p."id"))
	OR EXISTS (SELECT 1 FROM "Sentence" WHERE ` + orphanSentenceCond + `)`

// RepairSentences 打开数据库时的一致性补齐（幂等）：删除没有篇引用的句子（例如 import --force 整库覆盖后
// 原有篇的句子），再 BackfillSentences。没有要做的事时不开写事务。
// 注：句数不一致、保持整段的短文每次打开都会被重新检查一次（不写库）。
func RepairSentences(ctx context.Context, d *store.DB, now time.Time) error {
	var need bool
	if err := d.QueryRowContext(ctx, sentencesNeedRepair).Scan(&need); err != nil {
		return err
	}
	if !need {
		return nil
	}
	return d.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM "Sentence" WHERE `+orphanSentenceCond); err != nil {
			return err
		}
		return BackfillSentences(ctx, tx, now)
	})
}

var (
	irregularOnce  sync.Once
	irregularTable lemma.Irregular
)

// builtinIrregular 内置不规则变化表（data/vocab/irregular-forms.tsv，随程序嵌入）。
func builtinIrregular() lemma.Irregular {
	irregularOnce.Do(func() { irregularTable = lemma.ParseIrregular(vinxvocab.IrregularForms) })
	return irregularTable
}

// LexWord 词库里的一条（分析结果里展示用）。
type LexWord struct {
	ID           string
	Spelling     string
	Definition   string
	PartOfSpeech string // 空串表示没有（spec 0005 结构相似度区分实词 / 虚词用）
}

// Lexicon 全部词库 + 匹配器。匹配范围是全部 Word（spec 0004 §5）。
type Lexicon struct {
	M     *lemma.Matcher
	Words map[string]LexWord
}

// LoadLexicon 读全部词库建匹配器；extra 为尚未入库的词（导入预览用，ID 自定，不与库里的冲突）。
func LoadLexicon(ctx context.Context, q store.Querier, extra ...LexWord) (*Lexicon, error) {
	rows, err := q.QueryContext(ctx, `SELECT "id","spelling","definition",coalesce("partOfSpeech",'') FROM "Word"`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	lx := &Lexicon{Words: map[string]LexWord{}}
	var entries []lemma.Entry
	for rows.Next() {
		var w LexWord
		if err := rows.Scan(&w.ID, &w.Spelling, &w.Definition, &w.PartOfSpeech); err != nil {
			return nil, err
		}
		lx.Words[w.ID] = w
		entries = append(entries, lemma.Entry{ID: w.ID, Spelling: w.Spelling})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, w := range extra {
		lx.Words[w.ID] = w
		entries = append(entries, lemma.Entry{ID: w.ID, Spelling: w.Spelling})
	}
	lx.M = lemma.NewMatcher(entries, builtinIrregular())
	return lx, nil
}

// With 加上尚未入库的词（导入预览用），返回新的词库；原词库不变。
func (lx *Lexicon) With(extra []LexWord) *Lexicon {
	out := &Lexicon{Words: make(map[string]LexWord, len(lx.Words)+len(extra))}
	entries := make([]lemma.Entry, 0, len(lx.Words)+len(extra))
	for id, w := range lx.Words {
		out.Words[id] = w
		entries = append(entries, lemma.Entry{ID: id, Spelling: w.Spelling})
	}
	for _, w := range extra {
		out.Words[w.ID] = w
		entries = append(entries, lemma.Entry{ID: w.ID, Spelling: w.Spelling})
	}
	out.M = lemma.NewMatcher(entries, builtinIrregular())
	return out
}

// SentenceWordView 句中关联到的一个词。
type SentenceWordView struct {
	WordID   string `json:"wordId"`
	Position int    `json:"position"`
	Form     string `json:"form"`
}

// SentenceView 一句（接口输出）。
type SentenceView struct {
	ID        string             `json:"id"`
	En        string             `json:"en"`
	Cn        string             `json:"cn"`
	Frame     *string            `json:"frame"`
	Source    string             `json:"source"`
	Paragraph int                `json:"paragraph"`
	Words     []SentenceWordView `json:"words"`
}

// newSentence 新建句子的字段。
type newSentence struct {
	En, Cn      string
	Frame       *string
	Source      string
	WordID      *string
	Model       *string
	CreatedByID *string
	OriginID    *string // 仿写的原句（spec 0005）
	VariantNote *string
}

func insertSentence(ctx context.Context, q store.Querier, lx *Lexicon, now time.Time, s newSentence) (string, error) {
	id := store.NewID()
	ts := store.NewTime(now)
	if _, err := q.ExecContext(ctx, `INSERT INTO "Sentence" ("id","en","cn","frame","source","wordId","model","createdById","originId","variantNote","createdAt","updatedAt") VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, s.En, s.Cn, s.Frame, s.Source, s.WordID, s.Model, s.CreatedByID, s.OriginID, s.VariantNote, ts, ts); err != nil {
		return "", err
	}
	return id, linkSentence(ctx, q, lx, id, s.En)
}

// linkSentence 重新计算一句的 SentenceWord。
func linkSentence(ctx context.Context, q store.Querier, lx *Lexicon, sentenceID, en string) error {
	if _, err := q.ExecContext(ctx, `DELETE FROM "SentenceWord" WHERE "sentenceId" = ?`, sentenceID); err != nil {
		return err
	}
	for _, m := range lx.M.Match(en) {
		if _, err := q.ExecContext(ctx, `INSERT INTO "SentenceWord" ("sentenceId","wordId","position","form") VALUES (?,?,?,?) ON CONFLICT DO NOTHING`,
			sentenceID, m.WordID, m.Position, m.Form); err != nil {
			return err
		}
	}
	return nil
}

// SyncExampleSentence 例句双写：按 Word.example / exampleCn 新建、更新或删除这个词的例句句子（spec 0004 §3）。
// lx 为 nil 时现读词库。
func SyncExampleSentence(ctx context.Context, q store.Querier, lx *Lexicon, now time.Time, wordID string) error {
	var example, exampleCn *string
	err := q.QueryRowContext(ctx, `SELECT "example","exampleCn" FROM "Word" WHERE "id" = ?`, wordID).Scan(&example, &exampleCn)
	if store.IsNoRows(err) {
		return nil
	}
	if err != nil {
		return err
	}
	en := ""
	if example != nil {
		en = strings.TrimSpace(*example)
	}
	if en == "" {
		_, err := q.ExecContext(ctx, `DELETE FROM "Sentence" WHERE "source" = 'example' AND "wordId" = ?`, wordID)
		return err
	}
	cn := ""
	if exampleCn != nil {
		cn = strings.TrimSpace(*exampleCn)
	}
	if lx == nil {
		if lx, err = LoadLexicon(ctx, q); err != nil {
			return err
		}
	}
	var id string
	err = q.QueryRowContext(ctx, `SELECT "id" FROM "Sentence" WHERE "source" = 'example' AND "wordId" = ? ORDER BY "createdAt" LIMIT 1`, wordID).Scan(&id)
	if store.IsNoRows(err) {
		_, err = insertSentence(ctx, q, lx, now, newSentence{En: en, Cn: cn, Source: SentenceExample, WordID: &wordID})
		return err
	}
	if err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, `UPDATE "Sentence" SET "en" = ?, "cn" = ?, "updatedAt" = ? WHERE "id" = ?`, en, cn, store.NewTime(now), id); err != nil {
		return err
	}
	return linkSentence(ctx, q, lx, id, en)
}

// savePassageSentences 把短文按句拆开写入 Sentence + PassageSentence；句数不一致时不拆，返回 false。
func savePassageSentences(ctx context.Context, q store.Querier, lx *Lexicon, now time.Time, passageID, userID string, model *string, body string, bodyCn *string) (bool, error) {
	cn := ""
	if bodyCn != nil {
		cn = *bodyCn
	}
	lines, ok := core.SplitPassage(body, cn)
	if !ok {
		return false, nil
	}
	for i, l := range lines {
		id, err := insertSentence(ctx, q, lx, now, newSentence{En: l.En, Cn: l.Cn, Source: SentenceAI, Model: model, CreatedByID: &userID})
		if err != nil {
			return false, err
		}
		if _, err := q.ExecContext(ctx, `INSERT INTO "PassageSentence" ("passageId","sentenceId","paragraph","sortOrder") VALUES (?,?,?,?)`, passageID, id, l.Paragraph, i); err != nil {
			return false, err
		}
	}
	return true, nil
}

// BackfillSentences 补齐句子（幂等）：有例句但还没有例句句子的词 → 例句句子；还没有逐句结构的 AI 短文 → 按句拆分
// （句数不一致的保持整段）。迁移 0003_sentences 的 Go 部分，也供「重新关联全部句子」与打开数据库时的
// RepairSentences 调用（从旧版导入后，下次打开数据库时由 RepairSentences 补齐）。
func BackfillSentences(ctx context.Context, tx *sql.Tx, now time.Time) error {
	var lx *Lexicon
	lex := func() (*Lexicon, error) {
		if lx != nil {
			return lx, nil
		}
		var err error
		lx, err = LoadLexicon(ctx, tx)
		return lx, err
	}
	wordIDs, err := queryStrings(ctx, tx, `SELECT w."id" FROM "Word" w WHERE w."example" IS NOT NULL AND trim(w."example") != ''
		AND NOT EXISTS (SELECT 1 FROM "Sentence" s WHERE s."source" = 'example' AND s."wordId" = w."id") ORDER BY w.rowid`)
	if err != nil {
		return err
	}
	for _, id := range wordIDs {
		l, err := lex()
		if err != nil {
			return err
		}
		if err := SyncExampleSentence(ctx, tx, l, now, id); err != nil {
			return err
		}
	}

	type passage struct {
		id, userID string
		model      *string
		body       string
		bodyCn     *string
	}
	rows, err := tx.QueryContext(ctx, `SELECT p."id", p."userId", p."model", p."body", p."bodyCn" FROM "Passage" p
		WHERE NOT EXISTS (SELECT 1 FROM "PassageSentence" ps WHERE ps."passageId" = p."id") ORDER BY p.rowid`)
	if err != nil {
		return err
	}
	var ps []passage
	for rows.Next() {
		var p passage
		if err := rows.Scan(&p.id, &p.userID, &p.model, &p.body, &p.bodyCn); err != nil {
			rows.Close()
			return err
		}
		ps = append(ps, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, p := range ps {
		l, err := lex()
		if err != nil {
			return err
		}
		if _, err := savePassageSentences(ctx, tx, l, now, p.id, p.userID, p.model, p.body, p.bodyCn); err != nil {
			return err
		}
	}
	return nil
}

// RelinkAllSentences 管理员操作「重新关联全部句子」：先补齐句子，再按当前词库重算每一句的关联。返回句子数。
// 词库新增词条时不回头重算旧句子（spec 0004 §5），需要时用这个操作。
func RelinkAllSentences(ctx context.Context, tx *sql.Tx, now time.Time) (int, error) {
	if err := BackfillSentences(ctx, tx, now); err != nil {
		return 0, err
	}
	lx, err := LoadLexicon(ctx, tx)
	if err != nil {
		return 0, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT "id","en" FROM "Sentence"`)
	if err != nil {
		return 0, err
	}
	type s struct{ id, en string }
	var all []s
	for rows.Next() {
		var x s
		if err := rows.Scan(&x.id, &x.en); err != nil {
			rows.Close()
			return 0, err
		}
		all = append(all, x)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, x := range all {
		if err := linkSentence(ctx, tx, lx, x.id, x.en); err != nil {
			return 0, err
		}
	}
	return len(all), nil
}

// PruneSentences 删除给定句子里已经没有任何篇引用的（例句跟随词条，不在此删除）。
// 删篇、删单元、删词书、删短文前先取出它们的句子 id，删除后调用（spec 0004 §2）。
func PruneSentences(ctx context.Context, q store.Querier, ids []string) error {
	const batch = 400
	for i := 0; i < len(ids); i += batch {
		chunk := ids[i:min(i+batch, len(ids))]
		if _, err := q.ExecContext(ctx, `DELETE FROM "Sentence" WHERE "id" IN (`+store.Placeholders(len(chunk))+`) AND `+orphanSentenceCond, store.Args(chunk)...); err != nil {
			return err
		}
	}
	return nil
}

// SentenceIDsOfUnits 这些单元里的篇引用的句子。
func SentenceIDsOfUnits(ctx context.Context, q store.Querier, unitIDs []string) ([]string, error) {
	if len(unitIDs) == 0 {
		return []string{}, nil
	}
	return queryStrings(ctx, q, `SELECT DISTINCT uts."sentenceId" FROM "UnitTextSentence" uts JOIN "UnitText" ut ON ut."id" = uts."textId"
		WHERE ut."unitId" IN (`+store.Placeholders(len(unitIDs))+`)`, store.Args(unitIDs)...)
}

// SentenceIDsOfBook 词书里所有篇引用的句子。
func SentenceIDsOfBook(ctx context.Context, q store.Querier, bookID string) ([]string, error) {
	return queryStrings(ctx, q, `SELECT DISTINCT uts."sentenceId" FROM "UnitTextSentence" uts JOIN "UnitText" ut ON ut."id" = uts."textId"
		JOIN "Unit" u ON u."id" = ut."unitId" WHERE u."bookId" = ?`, bookID)
}

// SentenceIDsOfPassage 短文引用的句子。
func SentenceIDsOfPassage(ctx context.Context, q store.Querier, passageID string) ([]string, error) {
	return queryStrings(ctx, q, `SELECT "sentenceId" FROM "PassageSentence" WHERE "passageId" = ?`, passageID)
}

// sentenceWords 一批句子的关联词（按位置排序）。
func sentenceWords(ctx context.Context, q store.Querier, ids []string) (map[string][]SentenceWordView, error) {
	out := map[string][]SentenceWordView{}
	const batch = 400
	for i := 0; i < len(ids); i += batch {
		chunk := ids[i:min(i+batch, len(ids))]
		rows, err := q.QueryContext(ctx, `SELECT "sentenceId","wordId","position","form" FROM "SentenceWord" WHERE "sentenceId" IN (`+store.Placeholders(len(chunk))+`) ORDER BY "position", "wordId"`, store.Args(chunk)...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var sid string
			var w SentenceWordView
			if err := rows.Scan(&sid, &w.WordID, &w.Position, &w.Form); err != nil {
				rows.Close()
				return nil, err
			}
			out[sid] = append(out[sid], w)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// fillWords 给句子填上关联词（没有时为空数组）。
func fillWords(ctx context.Context, q store.Querier, views []*SentenceView) error {
	ids := make([]string, len(views))
	for i, v := range views {
		ids[i] = v.ID
	}
	words, err := sentenceWords(ctx, q, ids)
	if err != nil {
		return err
	}
	for _, v := range views {
		v.Words = words[v.ID]
		if v.Words == nil {
			v.Words = []SentenceWordView{}
		}
	}
	return nil
}

// PassageSentences 短文的逐句结构（没有拆分时为空数组）。
func PassageSentences(ctx context.Context, q store.Querier, passageID string) ([]SentenceView, error) {
	rows, err := q.QueryContext(ctx, `SELECT s."id", s."en", s."cn", s."frame", s."source", ps."paragraph"
		FROM "PassageSentence" ps JOIN "Sentence" s ON s."id" = ps."sentenceId" WHERE ps."passageId" = ? ORDER BY ps."sortOrder"`, passageID)
	if err != nil {
		return nil, err
	}
	out, err := scanSentenceViews(rows)
	if err != nil {
		return nil, err
	}
	return out, fillWordsSlice(ctx, q, out)
}

func scanSentenceViews(rows *sql.Rows) ([]SentenceView, error) {
	defer rows.Close()
	out := []SentenceView{}
	for rows.Next() {
		var v SentenceView
		if err := rows.Scan(&v.ID, &v.En, &v.Cn, &v.Frame, &v.Source, &v.Paragraph); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func fillWordsSlice(ctx context.Context, q store.Querier, vs []SentenceView) error {
	ptrs := make([]*SentenceView, len(vs))
	for i := range vs {
		ptrs[i] = &vs[i]
	}
	return fillWords(ctx, q, ptrs)
}

// SavePassageSentences 新生成的短文按句拆分保存：AI 仍按旧的整段格式输出时用（spec 0005 起 AI 按句输出，
// 直接保存那些句子，见 SavePassage）。
func SavePassageSentences(ctx context.Context, q store.Querier, now time.Time, passageID string) error {
	var userID, body string
	var model, bodyCn *string
	if err := q.QueryRowContext(ctx, `SELECT "userId","model","body","bodyCn" FROM "Passage" WHERE "id" = ?`, passageID).Scan(&userID, &model, &body, &bodyCn); err != nil {
		return err
	}
	cn := ""
	if bodyCn != nil {
		cn = *bodyCn
	}
	if _, ok := core.SplitPassage(body, cn); !ok {
		return nil
	}
	lx, err := LoadLexicon(ctx, q)
	if err != nil {
		return err
	}
	_, err = savePassageSentences(ctx, q, lx, now, passageID, userID, model, body, bodyCn)
	return err
}

// ===========================================================================
// 分析：分词、关联、超纲、词库外（spec 0004 §6）
// ===========================================================================

// AnalyzedWord 句中关联到的一个词条。WordID 为 nil 表示尚未入库（导入预览里的新词）。
type AnalyzedWord struct {
	WordID     *string `json:"wordId"`
	Spelling   string  `json:"spelling"`
	Definition string  `json:"definition"`
	Position   int     `json:"position"`
	Form       string  `json:"form"`
}

// SentenceAnalysis 一句话的分析结果。
type SentenceAnalysis struct {
	Tokens     []lemma.Token  `json:"tokens"`
	Words      []AnalyzedWord `json:"words"`
	OutOfScope []AnalyzedWord `json:"outOfScope"`
	Unknown    []lemma.Token  `json:"unknown"`
}

// newWordPrefix 导入预览里尚未入库的词的临时 id 前缀。
const newWordPrefix = "new:"

// Analyze 分析一句英文；known 为已知词 id 集合（nil 表示不做超纲检查，OutOfScope 为空）。
func (lx *Lexicon) Analyze(en string, known map[string]bool) SentenceAnalysis {
	a := lx.M.Analyze(en)
	res := SentenceAnalysis{Tokens: a.Tokens, Words: []AnalyzedWord{}, OutOfScope: []AnalyzedWord{}, Unknown: a.Unknown}
	if res.Tokens == nil {
		res.Tokens = []lemma.Token{}
	}
	if res.Unknown == nil {
		res.Unknown = []lemma.Token{}
	}
	conv := func(m lemma.Match) AnalyzedWord {
		w := lx.Words[m.WordID]
		aw := AnalyzedWord{Spelling: w.Spelling, Definition: w.Definition, Position: m.Position, Form: m.Form}
		if !strings.HasPrefix(m.WordID, newWordPrefix) {
			id := m.WordID
			aw.WordID = &id
		}
		return aw
	}
	for _, m := range a.Matches {
		res.Words = append(res.Words, conv(m))
	}
	if known != nil {
		for _, m := range lemma.OutOfScope(a.Matches, known) {
			res.OutOfScope = append(res.OutOfScope, conv(m))
		}
	}
	return res
}

// KnownWordsUpToUnit 编辑单元时的已知词：这本词书到当前单元为止（按单元顺序）的词。
// spec 0004 §6 还要加上班级目标词书（spec 0003），目标词书尚未实现，暂不计入。
func KnownWordsUpToUnit(ctx context.Context, q store.Querier, unitID string) (map[string]bool, error) {
	ids, err := queryStrings(ctx, q, `SELECT DISTINCT uw."wordId" FROM "UnitWord" uw JOIN "Unit" u ON u."id" = uw."unitId"
		JOIN "Unit" cur ON cur."id" = ? WHERE u."bookId" = cur."bookId" AND u."sortOrder" <= cur."sortOrder"`, unitID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out, nil
}

// KnownWordsOfLearner 学生视角的已知词：已有记忆状态的词（目标词书见 spec 0003，尚未实现，暂不计入）。
func KnownWordsOfLearner(ctx context.Context, q store.Querier, userID string) (map[string]bool, error) {
	ids, err := queryStrings(ctx, q, `SELECT "wordId" FROM "MemoryState" WHERE "userId" = ?`, userID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out, nil
}

// AnalyzeSentence POST /sentences/analyze：unitID 非空时已知词按单元（单元不可见 → 404「单元不存在」），
// 否则按操作者自己的学习记录。
func AnalyzeSentence(ctx context.Context, q store.Querier, a *Actor, en string, unitID *string) (*SentenceAnalysis, error) {
	var known map[string]bool
	var err error
	if unitID != nil {
		u, err := VisibleUnit(ctx, q, a, *unitID)
		if err != nil {
			return nil, err
		}
		if u == nil {
			return nil, httpx.NotFound("单元不存在")
		}
		if known, err = KnownWordsUpToUnit(ctx, q, u.ID); err != nil {
			return nil, err
		}
	} else if known, err = KnownWordsOfLearner(ctx, q, a.ID); err != nil {
		return nil, err
	}
	lx, err := LoadLexicon(ctx, q)
	if err != nil {
		return nil, err
	}
	res := lx.Analyze(en, known)
	return &res, nil
}

// ===========================================================================
// 词 → 句：GET /words/:id/sentences
// ===========================================================================

// SentenceFrom 句子的出处。
type SentenceFrom struct {
	Type      string `json:"type"` // example | unitText | passage
	WordID    string `json:"wordId,omitempty"`
	Spelling  string `json:"spelling,omitempty"`
	TextID    string `json:"textId,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Title     string `json:"title,omitempty"`
	UnitID    string `json:"unitId,omitempty"`
	UnitName  string `json:"unitName,omitempty"`
	BookID    string `json:"bookId,omitempty"`
	BookName  string `json:"bookName,omitempty"`
	PassageID string `json:"passageId,omitempty"`
}

// WordSentenceItem 一句及其出处。
type WordSentenceItem struct {
	SentenceView
	From SentenceFrom `json:"from"`
}

// WordSentences 出现过某个词的句子，按来源分组：例句、课本句型（list 篇）、课文（text 篇）、我的短文。
type WordSentences struct {
	Examples []WordSentenceItem `json:"examples"`
	Patterns []WordSentenceItem `json:"patterns"`
	Texts    []WordSentenceItem `json:"texts"`
	Passages []WordSentenceItem `json:"passages"`
}

// GetWordSentences 词不存在或不可见返回 (nil, nil)（与 GET /words/:id 相同的可见性）。
// 例句：这个词自己的例句 + 别的（可见）词的例句里用到它的；单元的篇只看可见词书；短文只看自己的。
func GetWordSentences(ctx context.Context, q store.Querier, a *Actor, wordID string) (*WordSentences, error) {
	detail, err := GetWordDetail(ctx, q, a, wordID)
	if err != nil || detail == nil {
		return nil, err
	}
	where, args, err := VisibleBookFilter(ctx, q, a, "bk")
	if err != nil {
		return nil, err
	}
	res := &WordSentences{}

	// 例句
	ownerVisible := `EXISTS (SELECT 1 FROM "UnitWord" uw JOIN "Unit" u ON u."id" = uw."unitId" JOIN "Book" bk ON bk."id" = u."bookId" WHERE uw."wordId" = s."wordId" AND ` + where + `)`
	exArgs := []any{wordID, wordID}
	exArgs = append(exArgs, args...)
	exArgs = append(exArgs, wordID)
	rows, err := q.QueryContext(ctx, `SELECT s."id", s."en", s."cn", s."frame", s."source", 0, w."id", w."spelling"
		FROM "Sentence" s JOIN "Word" w ON w."id" = s."wordId"
		WHERE s."source" = 'example' AND (s."wordId" = ? OR (EXISTS (SELECT 1 FROM "SentenceWord" sw WHERE sw."sentenceId" = s."id" AND sw."wordId" = ?) AND `+ownerVisible+`))
		ORDER BY (s."wordId" = ?) DESC, w."spelling", s."id"`, exArgs...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var it WordSentenceItem
		it.From.Type = "example"
		if err := rows.Scan(&it.ID, &it.En, &it.Cn, &it.Frame, &it.Source, &it.Paragraph, &it.From.WordID, &it.From.Spelling); err != nil {
			rows.Close()
			return nil, err
		}
		res.Examples = append(res.Examples, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 单元的篇
	rows, err = q.QueryContext(ctx, `SELECT s."id", s."en", s."cn", s."frame", s."source", uts."paragraph",
		ut."id", ut."kind", ut."title", u."id", u."name", bk."id", bk."name"
		FROM "UnitTextSentence" uts JOIN "Sentence" s ON s."id" = uts."sentenceId" JOIN "UnitText" ut ON ut."id" = uts."textId"
		JOIN "Unit" u ON u."id" = ut."unitId" JOIN "Book" bk ON bk."id" = u."bookId"
		WHERE EXISTS (SELECT 1 FROM "SentenceWord" sw WHERE sw."sentenceId" = s."id" AND sw."wordId" = ?) AND `+where+`
		ORDER BY bk."isSystem" DESC, bk."sortOrder", bk."createdAt", bk."id", u."sortOrder", ut."sortOrder", ut."id", uts."sortOrder"`, append([]any{wordID}, args...)...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var it WordSentenceItem
		it.From.Type = "unitText"
		if err := rows.Scan(&it.ID, &it.En, &it.Cn, &it.Frame, &it.Source, &it.Paragraph,
			&it.From.TextID, &it.From.Kind, &it.From.Title, &it.From.UnitID, &it.From.UnitName, &it.From.BookID, &it.From.BookName); err != nil {
			rows.Close()
			return nil, err
		}
		if it.From.Kind == "list" {
			res.Patterns = append(res.Patterns, it)
		} else {
			res.Texts = append(res.Texts, it)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 我的短文
	rows, err = q.QueryContext(ctx, `SELECT s."id", s."en", s."cn", s."frame", s."source", ps."paragraph", p."id", p."title"
		FROM "PassageSentence" ps JOIN "Sentence" s ON s."id" = ps."sentenceId" JOIN "Passage" p ON p."id" = ps."passageId"
		WHERE p."userId" = ? AND EXISTS (SELECT 1 FROM "SentenceWord" sw WHERE sw."sentenceId" = s."id" AND sw."wordId" = ?)
		ORDER BY p."createdAt" DESC, p."id", ps."sortOrder"`, a.ID, wordID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var it WordSentenceItem
		it.From.Type = "passage"
		if err := rows.Scan(&it.ID, &it.En, &it.Cn, &it.Frame, &it.Source, &it.Paragraph, &it.From.PassageID, &it.From.Title); err != nil {
			rows.Close()
			return nil, err
		}
		res.Passages = append(res.Passages, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var ptrs []*SentenceView
	for _, g := range [][]WordSentenceItem{res.Examples, res.Patterns, res.Texts, res.Passages} {
		for i := range g {
			ptrs = append(ptrs, &g[i].SentenceView)
		}
	}
	if err := fillWords(ctx, q, ptrs); err != nil {
		return nil, err
	}
	for _, g := range []*[]WordSentenceItem{&res.Examples, &res.Patterns, &res.Texts, &res.Passages} {
		if *g == nil {
			*g = []WordSentenceItem{}
		}
	}
	return res, nil
}
