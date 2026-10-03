package service

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/vinx-lab/vinx-vocab-go/internal/core/vocabparser"
	"github.com/vinx-lab/vinx-vocab-go/internal/httpx"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// 单元的篇（UnitText，spec 0004）：课文、重点句型清单、句型仿写。挂在单元下，查看跟随词书可见性、
// 编辑跟随词书编辑权限（权限由路由先判定）；创建人只做记录。

// SentenceInput 篇里的一句（ID 非空表示保留已有句子）。
type SentenceInput struct {
	ID        string
	En        string
	Cn        string
	Frame     *string
	Paragraph int
}

// NewUnitText 新建一篇。
type NewUnitText struct {
	Kind        string
	Title       string
	TitleCn     *string
	Sentences   []SentenceInput
	Source      string // 句子来源：manual | import
	CreatedByID string
}

// UnitTextView 一篇（接口输出）。
type UnitTextView struct {
	ID        string         `json:"id"`
	UnitID    string         `json:"unitId"`
	Kind      string         `json:"kind"`
	Title     string         `json:"title"`
	TitleCn   *string        `json:"titleCn"`
	SortOrder int            `json:"sortOrder"`
	CreatedAt store.Time     `json:"createdAt"`
	UpdatedAt store.Time     `json:"updatedAt"`
	Sentences []SentenceView `json:"sentences"`
}

// UnitTextUnit 篇所在的单元（找不到 → NOT_FOUND「内容不存在」）。
func UnitTextUnit(ctx context.Context, q store.Querier, textID string) (*UnitRow, error) {
	var unitID string
	err := q.QueryRowContext(ctx, `SELECT "unitId" FROM "UnitText" WHERE "id" = ?`, textID).Scan(&unitID)
	if store.IsNoRows(err) {
		return nil, httpx.NotFound("内容不存在")
	}
	if err != nil {
		return nil, err
	}
	return UnitBook(ctx, q, unitID)
}

func listUnitTexts(ctx context.Context, q store.Querier, where string, args ...any) ([]UnitTextView, error) {
	rows, err := q.QueryContext(ctx, `SELECT "id","unitId","kind","title","titleCn","sortOrder","createdAt","updatedAt" FROM "UnitText" WHERE `+where+` ORDER BY "sortOrder", "createdAt", "id"`, args...)
	if err != nil {
		return nil, err
	}
	out := []UnitTextView{}
	for rows.Next() {
		var t UnitTextView
		if err := rows.Scan(&t.ID, &t.UnitID, &t.Kind, &t.Title, &t.TitleCn, &t.SortOrder, &t.CreatedAt, &t.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		srows, err := q.QueryContext(ctx, `SELECT s."id", s."en", s."cn", s."frame", s."source", uts."paragraph"
			FROM "UnitTextSentence" uts JOIN "Sentence" s ON s."id" = uts."sentenceId" WHERE uts."textId" = ? ORDER BY uts."sortOrder"`, out[i].ID)
		if err != nil {
			return nil, err
		}
		if out[i].Sentences, err = scanSentenceViews(srows); err != nil {
			return nil, err
		}
		if err := fillWordsSlice(ctx, q, out[i].Sentences); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ListUnitTexts 单元内的篇（带句子），按单元内顺序。
func ListUnitTexts(ctx context.Context, q store.Querier, unitID string) ([]UnitTextView, error) {
	return listUnitTexts(ctx, q, `"unitId" = ?`, unitID)
}

// GetUnitText 一篇；不存在返回 (nil, nil)。
func GetUnitText(ctx context.Context, q store.Querier, id string) (*UnitTextView, error) {
	ts, err := listUnitTexts(ctx, q, `"id" = ?`, id)
	if err != nil || len(ts) == 0 {
		return nil, err
	}
	return &ts[0], nil
}

// CreateUnitText 新建一篇（排在单元末尾）并写入句子、计算关联。必须在事务里调用；lx 为 nil 时现读词库。
func CreateUnitText(ctx context.Context, tx store.Querier, lx *Lexicon, now time.Time, unitID string, in NewUnitText) (string, error) {
	var last *int
	if err := tx.QueryRowContext(ctx, `SELECT max("sortOrder") FROM "UnitText" WHERE "unitId" = ?`, unitID).Scan(&last); err != nil {
		return "", err
	}
	order := 0
	if last != nil {
		order = *last + 1
	}
	id := store.NewID()
	ts := store.NewTime(now)
	if _, err := tx.ExecContext(ctx, `INSERT INTO "UnitText" ("id","unitId","kind","title","titleCn","sortOrder","createdById","createdAt","updatedAt") VALUES (?,?,?,?,?,?,?,?,?)`,
		id, unitID, in.Kind, in.Title, in.TitleCn, order, nullIfEmpty(in.CreatedByID), ts, ts); err != nil {
		return "", err
	}
	// existing 为 nil：句子一律新建（新篇里的 id 不能借用别处的句子）
	if err := writeTextSentences(ctx, tx, lx, now, id, in.Sentences, in.Source, in.CreatedByID, nil); err != nil {
		return "", err
	}
	return id, nil
}

// writeTextSentences 按顺序写入篇的句子：ID 在 existing 里的更新原句，其余新建。
func writeTextSentences(ctx context.Context, tx store.Querier, lx *Lexicon, now time.Time, textID string, sentences []SentenceInput, source, createdBy string, existing map[string]bool) error {
	if len(sentences) == 0 {
		return nil
	}
	if lx == nil {
		var err error
		if lx, err = LoadLexicon(ctx, tx); err != nil {
			return err
		}
	}
	ts := store.NewTime(now)
	for i, s := range sentences {
		id := s.ID
		if id != "" && existing[id] {
			if _, err := tx.ExecContext(ctx, `UPDATE "Sentence" SET "en" = ?, "cn" = ?, "frame" = ?, "updatedAt" = ? WHERE "id" = ?`, s.En, s.Cn, s.Frame, ts, id); err != nil {
				return err
			}
			if err := linkSentence(ctx, tx, lx, id, s.En); err != nil {
				return err
			}
		} else {
			var err error
			id, err = insertSentence(ctx, tx, lx, now, newSentence{En: s.En, Cn: s.Cn, Frame: s.Frame, Source: source, CreatedByID: strPtrOrNil(createdBy)})
			if err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO "UnitTextSentence" ("textId","sentenceId","paragraph","sortOrder") VALUES (?,?,?,?)`, textID, id, s.Paragraph, i); err != nil {
			return err
		}
	}
	return nil
}

func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// UnitTextPatch 改标题、类型、单元内顺序；nil 表示不改。TitleCn 非 nil 时按 Valid 区分设值 / 清空。
type UnitTextPatch struct {
	Title     *string
	TitleCn   *sql.NullString
	Kind      *string
	SortOrder *int
}

// UpdateUnitText 部分更新（没有字段要改时不写库）。
func UpdateUnitText(ctx context.Context, q store.Querier, now time.Time, id string, p UnitTextPatch) error {
	var sets []string
	var args []any
	if p.Title != nil {
		sets = append(sets, `"title" = ?`)
		args = append(args, *p.Title)
	}
	if p.TitleCn != nil {
		sets = append(sets, `"titleCn" = ?`)
		if p.TitleCn.Valid {
			args = append(args, p.TitleCn.String)
		} else {
			args = append(args, nil)
		}
	}
	if p.Kind != nil {
		sets = append(sets, `"kind" = ?`)
		args = append(args, *p.Kind)
	}
	if p.SortOrder != nil {
		sets = append(sets, `"sortOrder" = ?`)
		args = append(args, *p.SortOrder)
	}
	if len(sets) == 0 {
		return nil
	}
	sets = append(sets, `"updatedAt" = ?`)
	args = append(args, store.NewTime(now), id)
	_, err := q.ExecContext(ctx, `UPDATE "UnitText" SET `+strings.Join(sets, ", ")+` WHERE "id" = ?`, args...)
	return err
}

// ReplaceTextSentences PUT /texts/:id/sentences：整体替换句子列表。带 id 的必须是这篇现有的句子（否则 400），
// 保留并更新；其余新建；移出的句子没有其他引用时删除。必须在事务里调用。
func ReplaceTextSentences(ctx context.Context, tx store.Querier, now time.Time, textID string, sentences []SentenceInput, actorID string) error {
	current, err := queryStrings(ctx, tx, `SELECT "sentenceId" FROM "UnitTextSentence" WHERE "textId" = ?`, textID)
	if err != nil {
		return err
	}
	existing := map[string]bool{}
	for _, id := range current {
		existing[id] = true
	}
	seen := map[string]bool{}
	for _, s := range sentences {
		if s.ID == "" {
			continue
		}
		if !existing[s.ID] {
			return httpx.Validation("句子不属于这篇内容")
		}
		if seen[s.ID] {
			return httpx.Validation("句子重复")
		}
		seen[s.ID] = true
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM "UnitTextSentence" WHERE "textId" = ?`, textID); err != nil {
		return err
	}
	if err := writeTextSentences(ctx, tx, nil, now, textID, sentences, SentenceManual, actorID, existing); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE "UnitText" SET "updatedAt" = ? WHERE "id" = ?`, store.NewTime(now), textID); err != nil {
		return err
	}
	return PruneSentences(ctx, tx, current)
}

// DeleteUnitText 删除一篇，并清理没有其他引用的句子。
func DeleteUnitText(ctx context.Context, q store.Querier, id string) error {
	ids, err := queryStrings(ctx, q, `SELECT "sentenceId" FROM "UnitTextSentence" WHERE "textId" = ?`, id)
	if err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, `DELETE FROM "UnitText" WHERE "id" = ?`, id); err != nil {
		return err
	}
	return PruneSentences(ctx, q, ids)
}

// ReorderUnitTexts 单元内篇的排序：去重、过滤未知 id，必须覆盖该单元全部篇，否则 400。
func ReorderUnitTexts(ctx context.Context, tx store.Querier, unitID string, textIDs []string) (int, error) {
	known, err := queryStrings(ctx, tx, `SELECT "id" FROM "UnitText" WHERE "unitId" = ?`, unitID)
	if err != nil {
		return 0, err
	}
	knownSet := map[string]bool{}
	for _, id := range known {
		knownSet[id] = true
	}
	seen := map[string]bool{}
	ordered := make([]string, 0, len(textIDs))
	for _, id := range textIDs {
		if seen[id] || !knownSet[id] {
			continue
		}
		seen[id] = true
		ordered = append(ordered, id)
	}
	if len(ordered) != len(known) {
		return 0, httpx.Validation("顺序必须包含本单元的全部内容")
	}
	for i, id := range ordered {
		if _, err := tx.ExecContext(ctx, `UPDATE "UnitText" SET "sortOrder" = ? WHERE "id" = ?`, i, id); err != nil {
			return 0, err
		}
	}
	return len(ordered), nil
}

// ImportTextsResult 单元内粘贴导入的结果。
type ImportTextsResult struct {
	Texts     int `json:"texts"`
	Sentences int `json:"sentences"`
}

// ImportUnitTexts 单元内导入若干篇（事务内）。
func ImportUnitTexts(ctx context.Context, tx store.Querier, now time.Time, unitID string, texts []NewUnitText) (ImportTextsResult, error) {
	var res ImportTextsResult
	var lx *Lexicon
	for _, t := range texts {
		if lx == nil && len(t.Sentences) > 0 {
			var err error
			if lx, err = LoadLexicon(ctx, tx); err != nil {
				return res, err
			}
		}
		if _, err := CreateUnitText(ctx, tx, lx, now, unitID, t); err != nil {
			return res, err
		}
		res.Texts++
		res.Sentences += len(t.Sentences)
	}
	return res, nil
}

// ===========================================================================
// 导入预览（句型 / 课文段逐句解析 + 关联 + 超纲）
// ===========================================================================

// PreviewSentence 预览里的一句：解析结果 + 分析。
type PreviewSentence struct {
	vocabparser.ParsedSentence
	Words      []AnalyzedWord `json:"words"`
	OutOfScope []AnalyzedWord `json:"outOfScope"`
	Unknown    []string       `json:"unknown"`
}

// PreviewText 预览里的一篇。
type PreviewText struct {
	Kind      string            `json:"kind"`
	Title     string            `json:"title"`
	TitleCn   string            `json:"titleCn"`
	Line      int               `json:"line"`
	Sentences []PreviewSentence `json:"sentences"`
}

// TextsPreviewStats 预览统计（句子按状态计数）。
type TextsPreviewStats struct {
	Texts     int `json:"texts"`
	Sentences int `json:"sentences"`
	OK        int `json:"ok"`
	Warning   int `json:"warning"`
	Error     int `json:"error"`
}

// TextsPreviewResult 单元内粘贴导入的预览。
type TextsPreviewResult struct {
	Texts []PreviewText     `json:"texts"`
	Stats TextsPreviewStats `json:"stats"`
}

func previewTexts(lx *Lexicon, texts []vocabparser.ParsedText, known map[string]bool) []PreviewText {
	out := make([]PreviewText, 0, len(texts))
	for _, t := range texts {
		pt := PreviewText{Kind: t.Kind, Title: t.Title, TitleCn: t.TitleCn, Line: t.Line, Sentences: make([]PreviewSentence, 0, len(t.Sentences))}
		for _, s := range t.Sentences {
			ps := PreviewSentence{ParsedSentence: s, Words: []AnalyzedWord{}, OutOfScope: []AnalyzedWord{}, Unknown: []string{}}
			if s.Status != vocabparser.StatusError {
				a := lx.Analyze(s.En, known)
				ps.Words, ps.OutOfScope = a.Words, a.OutOfScope
				for _, tk := range a.Unknown {
					ps.Unknown = append(ps.Unknown, tk.Text)
				}
			}
			pt.Sentences = append(pt.Sentences, ps)
		}
		out = append(out, pt)
	}
	return out
}

// PreviewUnitTexts 单元内粘贴导入的预览：已知词 = 这本词书到当前单元为止的词。
func PreviewUnitTexts(ctx context.Context, q store.Querier, unitID, text, defaultKind string) (*TextsPreviewResult, error) {
	parsed := vocabparser.ParseUnitTexts(text, defaultKind)
	lx, err := LoadLexicon(ctx, q)
	if err != nil {
		return nil, err
	}
	known, err := KnownWordsUpToUnit(ctx, q, unitID)
	if err != nil {
		return nil, err
	}
	res := &TextsPreviewResult{Texts: previewTexts(lx, parsed, known)}
	for _, t := range res.Texts {
		res.Stats.Texts++
		for _, s := range t.Sentences {
			res.Stats.Sentences++
			switch s.Status {
			case vocabparser.StatusOK:
				res.Stats.OK++
			case vocabparser.StatusWarning:
				res.Stats.Warning++
			default:
				res.Stats.Error++
			}
		}
	}
	return res, nil
}
