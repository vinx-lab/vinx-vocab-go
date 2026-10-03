// AI 生成（spec 0005）：学段、生成后检查、句型 / 仿写的草稿与保存、重写一句。
//
// 例句与短文保持生成即写库（ai.go）；句型与仿写的草稿只存在于后台任务的结果里（内存，JobTTL），
// 保存时由页面把（可能编辑过的）句子提交上来。业务规则在 internal/core/ai（学段、模板、解析、检查），
// 这里只做装配：读库、调 AI、算检查、写库。
package service

import (
	"context"
	"strings"
	"time"

	coreai "github.com/vinx-lab/vinx-vocab-go/internal/core/ai"
	"github.com/vinx-lab/vinx-vocab-go/internal/core/lemma"
	"github.com/vinx-lab/vinx-vocab-go/internal/httpx"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// ===========================================================================
// 学段
// ===========================================================================

// UnitLevel 单元相关生成（例句、句型、仿写）的学段：override 合法时用它，否则用词书的学段，都没有用默认学段。
func UnitLevel(ctx context.Context, q store.Querier, unitID string, override *string) (coreai.Level, error) {
	if override != nil && coreai.IsLevel(*override) {
		return *override, nil
	}
	var level *string
	err := q.QueryRowContext(ctx, `SELECT b."level" FROM "Unit" u JOIN "Book" b ON b."id" = u."bookId" WHERE u."id" = ?`, unitID).Scan(&level)
	if err != nil && !store.IsNoRows(err) {
		return "", err
	}
	return coreai.LevelOr(level, coreai.DefaultLevel), nil
}

// WordLevel 单个词补例句的学段：override 优先，否则取这个词所在词书里最高的学段。
func WordLevel(ctx context.Context, q store.Querier, wordID string, override *string) (coreai.Level, error) {
	if override != nil && coreai.IsLevel(*override) {
		return *override, nil
	}
	levels, err := nullStrings(ctx, q, `SELECT DISTINCT b."level" FROM "UnitWord" uw JOIN "Unit" u ON u."id" = uw."unitId" JOIN "Book" b ON b."id" = u."bookId" WHERE uw."wordId" = ?`, wordID)
	if err != nil {
		return "", err
	}
	return coreai.MaxLevel(levels), nil
}

// LearnerLevel 学生短文的学段：override 优先，否则取学生目标词书里最高的学段；没有目标时用默认学段（初中）。
func LearnerLevel(ctx context.Context, q store.Querier, useClasses bool, userID string, override *string) (coreai.Level, error) {
	if override != nil && coreai.IsLevel(*override) {
		return *override, nil
	}
	t, err := EffectiveTargets(ctx, q, useClasses, userID)
	if err != nil {
		return "", err
	}
	if len(t.Books) == 0 {
		return coreai.DefaultLevel, nil
	}
	ids := make([]string, len(t.Books))
	for i, b := range t.Books {
		ids[i] = b.ID
	}
	levels, err := nullStrings(ctx, q, `SELECT "level" FROM "Book" WHERE "id" IN (`+store.Placeholders(len(ids))+`)`, store.Args(ids)...)
	if err != nil {
		return "", err
	}
	return coreai.MaxLevel(levels), nil
}

func nullStrings(ctx context.Context, q store.Querier, query string, args ...any) ([]*string, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*string
	for rows.Next() {
		var s *string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ===========================================================================
// 生成后检查
// ===========================================================================

// TargetWord 要用上的目标词。
type TargetWord struct {
	ID       string
	Spelling string
}

// SentenceChecker 用一份词库和已知词集合检查生成的句子（spec 0005 §4）。known 为 nil 时不做超纲检查。
type SentenceChecker struct {
	lx       *Lexicon
	known    map[string]bool
	maxWords int
}

// NewSentenceChecker 构造；单句上限取学段参数。
func NewSentenceChecker(lx *Lexicon, known map[string]bool, level coreai.Level) *SentenceChecker {
	return &SentenceChecker{lx: lx, known: known, maxWords: coreai.ParamsOf(level).MaxWords}
}

func (c *SentenceChecker) entry(id string) (string, bool) {
	w := c.lx.Words[id]
	return w.PartOfSpeech, strings.Contains(lemma.Key(w.Spelling), " ")
}

// functionSeq 一句话的虚词序列（结构相似度用）。
func (c *SentenceChecker) functionSeq(en string) []string {
	return coreai.FunctionSequence(coreai.StructWordsOf(c.lx.M.Analyze(en), c.entry))
}

// Check 检查一句：targets 为要用上的目标词（例句、短文；其他传 nil），origin 为仿写的原句（其他传空串）。
func (c *SentenceChecker) Check(en string, targets []TargetWord, origin string) coreai.SentenceChecks {
	a := c.lx.M.Analyze(en)
	in := coreai.CheckInput{Words: coreai.CountWords(a.Tokens), MaxWords: c.maxWords}
	if len(targets) > 0 {
		matched := map[string]bool{}
		for _, m := range a.Matches {
			matched[m.WordID] = true
		}
		for _, t := range targets {
			in.Targets = append(in.Targets, coreai.TargetUse{Spelling: t.Spelling, Used: matched[t.ID] || coreai.ExampleUsesWord(en, t.Spelling)})
		}
	}
	if c.known != nil {
		for _, m := range lemma.OutOfScope(a.Matches, c.known) {
			in.OutOfScope = append(in.OutOfScope, m.Form)
		}
	}
	if strings.TrimSpace(origin) != "" {
		sim := coreai.StructureSimilarity(c.functionSeq(origin), coreai.FunctionSequence(coreai.StructWordsOf(a, c.entry)))
		in.Similarity = &sim
	}
	return coreai.BuildChecks(in)
}

// unionKnown 合并已知词集合（返回新集合）。
func unionKnown(sets ...map[string]bool) map[string]bool {
	out := map[string]bool{}
	for _, s := range sets {
		for k := range s {
			out[k] = true
		}
	}
	return out
}

// KnownWordsForWord 单个词补例句时的已知词：这个词所在的每个单元「到当前单元为止」的词的并集。
func KnownWordsForWord(ctx context.Context, q store.Querier, wordID string) (map[string]bool, error) {
	ids, err := queryStrings(ctx, q, `SELECT DISTINCT uw2."wordId" FROM "UnitWord" uw JOIN "Unit" cur ON cur."id" = uw."unitId"
		JOIN "Unit" u ON u."bookId" = cur."bookId" AND u."sortOrder" <= cur."sortOrder" JOIN "UnitWord" uw2 ON uw2."unitId" = u."id"
		WHERE uw."wordId" = ?`, wordID)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{wordID: true}
	for _, id := range ids {
		out[id] = true
	}
	return out, nil
}

// ===========================================================================
// 单元词汇
// ===========================================================================

// unitVocabLimit 提示词里最多列出的单元 / 目标词数。
const unitVocabLimit = 80

// unitWordIDs 单元的词（按单元内顺序）。
func unitWordIDs(ctx context.Context, q store.Querier, unitID string) ([]string, error) {
	return queryStrings(ctx, q, `SELECT "wordId" FROM "UnitWord" WHERE "unitId" = ? ORDER BY "sortOrder", "wordId"`, unitID)
}

// bookWordIDs 一组词书的词（按词书顺序、单元顺序、单元内顺序，去重）。
func bookWordIDs(ctx context.Context, q store.Querier, bookIDs []string) ([]string, error) {
	var out []string
	for _, id := range bookIDs {
		ids, err := queryStrings(ctx, q, `SELECT uw."wordId" FROM "UnitWord" uw JOIN "Unit" u ON u."id" = uw."unitId" WHERE u."bookId" = ? ORDER BY u."sortOrder", uw."sortOrder"`, id)
		if err != nil {
			return nil, err
		}
		out = append(out, ids...)
	}
	return dedupe(out), nil
}

// unitPatternSentences 单元里已有的句型（list 篇的句子英文，按篇、句顺序），让 AI 不要重复。
func unitPatternSentences(ctx context.Context, q store.Querier, unitID string, limit int) ([]string, error) {
	return queryStrings(ctx, q, `SELECT s."en" FROM "UnitText" ut JOIN "UnitTextSentence" uts ON uts."textId" = ut."id" JOIN "Sentence" s ON s."id" = uts."sentenceId"
		WHERE ut."unitId" = ? AND ut."kind" = 'list' ORDER BY ut."sortOrder", ut."createdAt", ut."id", uts."sortOrder" LIMIT ?`, unitID, limit)
}

// ===========================================================================
// 句型
// ===========================================================================

// PatternCountMin / Max / Default 一次生成的句型数（spec 0005 §6）。
const (
	PatternCountMin     = 4
	PatternCountMax     = 12
	PatternCountDefault = 8
)

// PatternOptions 句型生成的输入。Topic 为空时（只在预览里）用单元名预填。
type PatternOptions struct {
	Topic  string
	Count  int
	Level  *string // 临时学段（只对这一次有效）
	Prompt *string // 页面上改过的可见提示词
}

// PatternPreview 句型预览：要用的输入、学段与默认可见提示词，不调用 AI。
type PatternPreview struct {
	UnitID   string          `json:"unitId"`
	UnitName string          `json:"unitName"`
	Level    coreai.Level    `json:"level"`
	Topic    string          `json:"topic"`
	Count    int             `json:"count"`
	Words    []AiPreviewWord `json:"words"`
	Existing []string        `json:"existing"`
	Prompt   string          `json:"prompt"`
}

type patternPrep struct {
	preview PatternPreview
	rows    []wordRow
}

func preparePatterns(ctx context.Context, cache *AIConfigCache, q store.Querier, unit *UnitRow, o PatternOptions) (*patternPrep, error) {
	level, err := UnitLevel(ctx, q, unit.ID, o.Level)
	if err != nil {
		return nil, err
	}
	ids, err := unitWordIDs(ctx, q, unit.ID)
	if err != nil {
		return nil, err
	}
	rows, err := loadWords(ctx, q, ids, unitVocabLimit)
	if err != nil {
		return nil, err
	}
	existing, err := unitPatternSentences(ctx, q, unit.ID, 50)
	if err != nil {
		return nil, err
	}
	tpl, err := cache.Template(ctx, q, coreai.PromptPattern)
	if err != nil {
		return nil, err
	}
	topic := strings.TrimSpace(o.Topic)
	if topic == "" {
		topic = unit.Name
	}
	p := PatternPreview{UnitID: unit.ID, UnitName: unit.Name, Level: level, Topic: topic, Count: o.Count, Words: previewWordsOf(rows), Existing: existing}
	p.Prompt = coreai.BuildPatternPrompt(coreai.FillPlaceholders(tpl, level), coreai.PatternInput{
		UnitName: unit.Name, Topic: topic, Count: o.Count, Words: promptWordsOf(rows), Existing: existing,
	})
	return &patternPrep{preview: p, rows: rows}, nil
}

// PreviewPatterns POST /ai/units/:id/patterns/preview。
func PreviewPatterns(ctx context.Context, cache *AIConfigCache, q store.Querier, unit *UnitRow, o PatternOptions) (PatternPreview, error) {
	p, err := preparePatterns(ctx, cache, q, unit, o)
	if err != nil {
		return PatternPreview{}, err
	}
	return p.preview, nil
}

// DraftSentence 句型草稿的一句（带检查结果）。
type DraftSentence struct {
	En     string                `json:"en"`
	Cn     string                `json:"cn"`
	Frame  *string               `json:"frame"`
	Checks coreai.SentenceChecks `json:"checks"`
}

// PatternDraft 句型任务的结果（草稿，只在内存里，JobTTL 后丢失）。
type PatternDraft struct {
	UnitID string          `json:"unitId"`
	Level  coreai.Level    `json:"level"`
	Title  string          `json:"title"` // 保存时的默认标题
	Model  string          `json:"model"`
	Items  []DraftSentence `json:"items"`
}

// PatternTitle / VariantTitle 保存时的默认标题。
func PatternTitle(unitName string) string { return unitName + " 重点句型" }
func VariantTitle(unitName string) string { return unitName + " 句型仿写" }

// GeneratePatterns 生成句型草稿（不写库）。
func GeneratePatterns(ctx context.Context, cache *AIConfigCache, q store.Querier, cfg coreai.CallConfig, unit *UnitRow, o PatternOptions) (PatternDraft, error) {
	p, err := preparePatterns(ctx, cache, q, unit, o)
	if err != nil {
		return PatternDraft{}, err
	}
	visible := p.preview.Prompt
	if o.Prompt != nil {
		visible = *o.Prompt
	}
	reply, err := askFor(ctx, cfg, coreai.PromptPattern, visible, 3000)
	if err != nil {
		return PatternDraft{}, err
	}
	items, err := coreai.ParsePatternItems(reply)
	if err != nil {
		return PatternDraft{}, err
	}
	if len(items) == 0 {
		return PatternDraft{}, coreai.NewAPIError("SERVER", "AI 没有生成句型，请重试")
	}
	lx, err := LoadLexicon(ctx, q)
	if err != nil {
		return PatternDraft{}, err
	}
	known, err := KnownWordsUpToUnit(ctx, q, unit.ID)
	if err != nil {
		return PatternDraft{}, err
	}
	checker := NewSentenceChecker(lx, known, p.preview.Level)
	d := PatternDraft{UnitID: unit.ID, Level: p.preview.Level, Title: PatternTitle(unit.Name), Model: cfg.Model, Items: []DraftSentence{}}
	for _, it := range items {
		d.Items = append(d.Items, DraftSentence{En: it.En, Cn: it.Cn, Frame: it.Frame, Checks: checker.Check(it.En, nil, "")})
	}
	return d, nil
}

// ===========================================================================
// 仿写
// ===========================================================================

// VariantOriginLimit 一次最多几个例句；VariantPerItemMax 每个例句最多几个变式；PastedMaxBytes 粘贴 / 上传上限。
const (
	VariantOriginLimit     = 10
	VariantPerItemMin      = 1
	VariantPerItemMax      = 5
	VariantPerItemDefault  = 3
	PastedMaxBytes         = 20 * 1024
	VariantVocabUnit       = "unit"
	VariantVocabTarget     = "target"
	variantPromptWordLimit = unitVocabLimit
)

// VariantOptions 仿写的输入。
type VariantOptions struct {
	SentenceIDs []string // 系统里已有的句子（先）
	Pasted      string   // 粘贴 / 上传的文本（后）
	Modes       []string
	PerItem     int
	Vocab       string // unit | target
	Level       *string
	Prompt      *string
}

// VariantOrigin 一个例句：已有句子带 id，粘贴的 id 为 null。
type VariantOrigin struct {
	ID *string `json:"id"`
	En string  `json:"en"`
	Cn string  `json:"cn"`
}

// VariantPreview 仿写预览。
type VariantPreview struct {
	UnitID  string          `json:"unitId"`
	Level   coreai.Level    `json:"level"`
	Origins []VariantOrigin `json:"origins"`
	Modes   []string        `json:"modes"`
	PerItem int             `json:"perItem"`
	Vocab   string          `json:"vocab"`
	Words   []AiPreviewWord `json:"words"`
	Prompt  string          `json:"prompt"`
}

type variantPrep struct {
	preview VariantPreview
	known   map[string]bool
}

func prepareVariants(ctx context.Context, cache *AIConfigCache, q store.Querier, a *Actor, unit *UnitRow, o VariantOptions) (*variantPrep, error) {
	ids := dedupe(o.SentenceIDs)
	if err := AssertSentencesVisible(ctx, q, a, ids); err != nil {
		return nil, err
	}
	origins := []VariantOrigin{}
	if len(ids) > 0 {
		rows, err := q.QueryContext(ctx, `SELECT "id","en","cn" FROM "Sentence" WHERE "id" IN (`+store.Placeholders(len(ids))+`)`, store.Args(ids)...)
		if err != nil {
			return nil, err
		}
		byID := map[string]VariantOrigin{}
		for rows.Next() {
			var id string
			var v VariantOrigin
			if err := rows.Scan(&id, &v.En, &v.Cn); err != nil {
				rows.Close()
				return nil, err
			}
			v.ID = &id
			byID[id] = v
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		for _, id := range ids {
			origins = append(origins, byID[id])
		}
	}
	for _, p := range coreai.ParsePastedOrigins(o.Pasted) {
		origins = append(origins, VariantOrigin{En: p.En, Cn: p.Cn})
	}
	if len(origins) == 0 || len(origins) > VariantOriginLimit {
		return nil, httpx.Validation("例句需要 1～10 句")
	}

	level, err := UnitLevel(ctx, q, unit.ID, o.Level)
	if err != nil {
		return nil, err
	}
	known, err := KnownWordsUpToUnit(ctx, q, unit.ID)
	if err != nil {
		return nil, err
	}
	var vocabIDs []string
	if o.Vocab == VariantVocabTarget {
		bookIDs, err := TeacherTargetBookIDs(ctx, q, a)
		if err != nil {
			return nil, err
		}
		if vocabIDs, err = bookWordIDs(ctx, q, bookIDs); err != nil {
			return nil, err
		}
		if len(vocabIDs) == 0 {
			return nil, httpx.NewError(httpx.CodeNoData, "没有可用的目标词（先给班级设置目标词书）", nil)
		}
		for _, id := range vocabIDs {
			known[id] = true
		}
	} else if vocabIDs, err = unitWordIDs(ctx, q, unit.ID); err != nil {
		return nil, err
	}
	rows, err := loadWords(ctx, q, vocabIDs, variantPromptWordLimit)
	if err != nil {
		return nil, err
	}
	tpl, err := cache.Template(ctx, q, coreai.PromptVariant)
	if err != nil {
		return nil, err
	}
	pasted := make([]coreai.PastedOrigin, len(origins))
	for i, x := range origins {
		pasted[i] = coreai.PastedOrigin{En: x.En, Cn: x.Cn}
	}
	p := VariantPreview{UnitID: unit.ID, Level: level, Origins: origins, Modes: o.Modes, PerItem: o.PerItem, Vocab: o.Vocab, Words: previewWordsOf(rows)}
	p.Prompt = coreai.BuildVariantPrompt(coreai.FillPlaceholders(tpl, level), coreai.VariantInput{Origins: pasted, Modes: o.Modes, PerItem: o.PerItem, Words: promptWordsOf(rows)})
	return &variantPrep{preview: p, known: known}, nil
}

// PreviewVariants POST /ai/variants/preview。
func PreviewVariants(ctx context.Context, cache *AIConfigCache, q store.Querier, a *Actor, unit *UnitRow, o VariantOptions) (VariantPreview, error) {
	p, err := prepareVariants(ctx, cache, q, a, unit, o)
	if err != nil {
		return VariantPreview{}, err
	}
	return p.preview, nil
}

// VariantDraftItem 仿写草稿的一句。Origin 是例句序号（从 1 开始），OriginID 为已有句子的 id（粘贴的为 null）。
type VariantDraftItem struct {
	Origin   int                   `json:"origin"`
	OriginID *string               `json:"originId"`
	OriginEn string                `json:"originEn"`
	En       string                `json:"en"`
	Cn       string                `json:"cn"`
	Change   string                `json:"change"` // replace | transform | expand | transfer；认不出为空串
	Note     string                `json:"note"`
	Checks   coreai.SentenceChecks `json:"checks"`
}

// VariantDraft 仿写任务的结果（草稿）。
type VariantDraft struct {
	UnitID  string             `json:"unitId"`
	Level   coreai.Level       `json:"level"`
	Title   string             `json:"title"`
	Model   string             `json:"model"`
	Origins []VariantOrigin    `json:"origins"`
	Items   []VariantDraftItem `json:"items"`
}

// GenerateVariants 生成仿写草稿（不写库）。准备工作（取例句、可见性、词汇）在请求里先做，见 PrepareVariantJob。
func GenerateVariants(ctx context.Context, q store.Querier, cfg coreai.CallConfig, unit *UnitRow, job *VariantJob) (VariantDraft, error) {
	reply, err := askFor(ctx, cfg, coreai.PromptVariant, job.prompt, 4000)
	if err != nil {
		return VariantDraft{}, err
	}
	origins := job.prep.preview.Origins
	items, err := coreai.ParseVariantItems(reply, len(origins))
	if err != nil {
		return VariantDraft{}, err
	}
	if len(items) == 0 {
		return VariantDraft{}, coreai.NewAPIError("SERVER", "AI 没有生成变式，请重试")
	}
	lx, err := LoadLexicon(ctx, q)
	if err != nil {
		return VariantDraft{}, err
	}
	checker := NewSentenceChecker(lx, job.prep.known, job.prep.preview.Level)
	d := VariantDraft{UnitID: unit.ID, Level: job.prep.preview.Level, Title: VariantTitle(unit.Name), Model: cfg.Model, Origins: origins, Items: []VariantDraftItem{}}
	for _, it := range items {
		o := origins[it.Origin-1]
		d.Items = append(d.Items, VariantDraftItem{
			Origin: it.Origin, OriginID: o.ID, OriginEn: o.En, En: it.En, Cn: it.Cn, Change: it.Change, Note: it.Note,
			Checks: checker.Check(it.En, nil, o.En),
		})
	}
	return d, nil
}

// VariantJob 请求里准备好的仿写任务（例句、已知词、可见提示词），交给后台任务生成。
type VariantJob struct {
	prep   *variantPrep
	prompt string
}

// PrepareVariantJob 在请求里校验并准备仿写任务（错误直接返回给页面，不进后台任务）。
func PrepareVariantJob(ctx context.Context, cache *AIConfigCache, q store.Querier, a *Actor, unit *UnitRow, o VariantOptions) (*VariantJob, error) {
	p, err := prepareVariants(ctx, cache, q, a, unit, o)
	if err != nil {
		return nil, err
	}
	prompt := p.preview.Prompt
	if o.Prompt != nil {
		prompt = *o.Prompt
	}
	return &VariantJob{prep: p, prompt: prompt}, nil
}

// ===========================================================================
// 保存句型 / 仿写草稿
// ===========================================================================

// AISentenceInput 保存的一句。
type AISentenceInput struct {
	En          string
	Cn          string
	Frame       *string
	OriginID    *string
	VariantNote *string
}

// SaveDraftInput 保存草稿：TextID 非空时追加到本单元已有的句型清单（list 篇），否则新建一篇 list。
type SaveDraftInput struct {
	Source    string // SentenceAI（句型）| SentenceVariant（仿写）
	TextID    *string
	Title     string // 新建时的标题（调用方已按默认标题补好）
	Sentences []AISentenceInput
	ActorID   string
	Model     *string
}

// SaveDraft 保存句型 / 仿写草稿（必须在事务里调用）。返回篇 id。
func SaveDraft(ctx context.Context, tx store.Querier, now time.Time, unitID string, in SaveDraftInput) (string, error) {
	var origins []string
	for _, s := range in.Sentences {
		if s.OriginID != nil {
			origins = append(origins, *s.OriginID)
		}
	}
	if origins = dedupe(origins); len(origins) > 0 {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM "Sentence" WHERE "id" IN (`+store.Placeholders(len(origins))+`)`, store.Args(origins)...).Scan(&n); err != nil {
			return "", err
		}
		if n != len(origins) {
			return "", httpx.Validation("原句不存在")
		}
	}

	textID := ""
	start := 0
	if in.TextID != nil {
		var unit, kind string
		err := tx.QueryRowContext(ctx, `SELECT "unitId","kind" FROM "UnitText" WHERE "id" = ?`, *in.TextID).Scan(&unit, &kind)
		if store.IsNoRows(err) || (err == nil && (unit != unitID || kind != "list")) {
			return "", httpx.Validation("只能追加到本单元的句型清单")
		}
		if err != nil {
			return "", err
		}
		var last *int
		if err := tx.QueryRowContext(ctx, `SELECT max("sortOrder") FROM "UnitTextSentence" WHERE "textId" = ?`, *in.TextID).Scan(&last); err != nil {
			return "", err
		}
		if last != nil {
			start = *last + 1
		}
		textID = *in.TextID
		if _, err := tx.ExecContext(ctx, `UPDATE "UnitText" SET "updatedAt" = ? WHERE "id" = ?`, store.NewTime(now), textID); err != nil {
			return "", err
		}
	} else {
		var err error
		textID, err = CreateUnitText(ctx, tx, nil, now, unitID, NewUnitText{Kind: "list", Title: in.Title, CreatedByID: in.ActorID})
		if err != nil {
			return "", err
		}
	}

	lx, err := LoadLexicon(ctx, tx)
	if err != nil {
		return "", err
	}
	for i, s := range in.Sentences {
		id, err := insertSentence(ctx, tx, lx, now, newSentence{
			En: s.En, Cn: s.Cn, Frame: s.Frame, Source: in.Source, Model: in.Model, CreatedByID: strPtrOrNil(in.ActorID),
			OriginID: s.OriginID, VariantNote: s.VariantNote,
		})
		if err != nil {
			return "", err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO "UnitTextSentence" ("textId","sentenceId","paragraph","sortOrder") VALUES (?,?,0,?)`, textID, id, start+i); err != nil {
			return "", err
		}
	}
	return textID, nil
}

// VariantNoteOf 仿写句的 variantNote：「改造方式：改了什么」（只有其一时只写那一项；都没有为 nil）。
func VariantNoteOf(change, note string) *string {
	label := coreai.VariantModeLabel[change]
	note = strings.TrimSpace(note)
	var s string
	switch {
	case label != "" && note != "":
		s = label + "：" + note
	case label != "":
		s = label
	case note != "":
		s = note
	default:
		return nil
	}
	return &s
}

// ===========================================================================
// 重写一句
// ===========================================================================

// RewriteTimeoutMs 重写一句是同步调用，超时不超过 60 秒。
const RewriteTimeoutMs = 60_000

// RewriteOptions 重写一句的输入。
type RewriteOptions struct {
	Kind   coreai.PromptKey
	Level  *string
	En     string
	Cn     string
	Issues []string
	Unit   *UnitRow // 单元相关的句子：已知词按单元；否则按操作者自己的学习记录
	Origin string   // 仿写的原句
	Target string   // 例句的目标词
}

// RewriteResult 重写结果（带检查）。
type RewriteResult struct {
	En     string                `json:"en"`
	Cn     string                `json:"cn"`
	Frame  *string               `json:"frame,omitempty"`
	Level  coreai.Level          `json:"level"`
	Checks coreai.SentenceChecks `json:"checks"`
}

// RewriteSentence POST /ai/sentences/rewrite：把这句和标出的问题一起发给 AI，只返回重写后的这一句（不写库）。
func RewriteSentence(ctx context.Context, q store.Querier, cfg coreai.CallConfig, actorID string, o RewriteOptions) (RewriteResult, error) {
	level := coreai.LevelOr(o.Level, "")
	var err error
	if level == "" {
		if o.Unit != nil {
			if level, err = UnitLevel(ctx, q, o.Unit.ID, nil); err != nil {
				return RewriteResult{}, err
			}
		} else {
			level = coreai.DefaultLevel
		}
	}
	if cfg.TimeoutMs <= 0 || cfg.TimeoutMs > RewriteTimeoutMs {
		cfg.TimeoutMs = RewriteTimeoutMs
	}
	user := coreai.BuildRewritePrompt(coreai.RewriteInput{Kind: o.Kind, Level: level, En: o.En, Cn: o.Cn, Issues: o.Issues, Target: o.Target, Origin: o.Origin})
	reply, err := AskWith(ctx, cfg, coreai.RewriteOutputFormat, user, 800)
	if err != nil {
		return RewriteResult{}, err
	}
	item, err := coreai.ParseRewriteReply(reply)
	if err != nil {
		return RewriteResult{}, err
	}
	lx, err := LoadLexicon(ctx, q)
	if err != nil {
		return RewriteResult{}, err
	}
	var known map[string]bool
	if o.Unit != nil {
		known, err = KnownWordsUpToUnit(ctx, q, o.Unit.ID)
	} else {
		known, err = KnownWordsOfLearner(ctx, q, actorID)
	}
	if err != nil {
		return RewriteResult{}, err
	}
	var targets []TargetWord
	if t := strings.TrimSpace(o.Target); t != "" {
		targets = []TargetWord{{Spelling: t}}
	}
	r := RewriteResult{En: item.En, Cn: item.Cn, Level: level, Checks: NewSentenceChecker(lx, known, level).Check(item.En, targets, o.Origin)}
	if item.Frame != "" {
		f := item.Frame
		r.Frame = &f
	}
	return r, nil
}

// ===========================================================================
// 短文逐句保存
// ===========================================================================

// savePassageItems AI 按句输出的短文：直接保存这些句子（不再拆分，spec 0005 §5）。
func savePassageItems(ctx context.Context, q store.Querier, lx *Lexicon, now time.Time, passageID, userID string, model *string, items []coreai.PassageSentenceItem) error {
	for i, s := range items {
		id, err := insertSentence(ctx, q, lx, now, newSentence{En: s.En, Cn: s.Cn, Source: SentenceAI, Model: model, CreatedByID: &userID})
		if err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx, `INSERT INTO "PassageSentence" ("passageId","sentenceId","paragraph","sortOrder") VALUES (?,?,?,?)`, passageID, id, s.Paragraph, i); err != nil {
			return err
		}
	}
	return nil
}

// PassageSentenceCheck 短文任务结果里的一句（带检查）。
type PassageSentenceCheck struct {
	En        string                `json:"en"`
	Cn        string                `json:"cn"`
	Paragraph int                   `json:"paragraph"`
	Checks    coreai.SentenceChecks `json:"checks"`
}

// CheckPassageSentences 已保存短文的逐句检查：已知词 = 学生的学习记录 + 这篇的目标词。
func CheckPassageSentences(ctx context.Context, q store.Querier, passageID, userID string, wordIDs []string, level coreai.Level) ([]PassageSentenceCheck, error) {
	ss, err := PassageSentences(ctx, q, passageID)
	if err != nil {
		return nil, err
	}
	out := []PassageSentenceCheck{}
	if len(ss) == 0 {
		return out, nil
	}
	known, err := KnownWordsOfLearner(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	for _, id := range wordIDs {
		known[id] = true
	}
	lx, err := LoadLexicon(ctx, q)
	if err != nil {
		return nil, err
	}
	checker := NewSentenceChecker(lx, known, level)
	for _, s := range ss {
		out = append(out, PassageSentenceCheck{En: s.En, Cn: s.Cn, Paragraph: s.Paragraph, Checks: checker.Check(s.En, nil, "")})
	}
	return out, nil
}
