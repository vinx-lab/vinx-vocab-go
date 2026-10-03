package service

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/vinx-lab/vinx-vocab-go/internal/core"
	"github.com/vinx-lab/vinx-vocab-go/internal/core/vocabparser"
	"github.com/vinx-lab/vinx-vocab-go/internal/httpx"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// 词书内容装配（对应旧 services/content.ts）。A1 只实现 seed 需要的 ImportUnits，A2 在本文件继续补充。

// ImportEntry 导入的一个词条（Type、Example、ExampleCn 可为空）。
type ImportEntry struct {
	Spelling     string
	Phonetic     string
	PartOfSpeech string
	Definition   string
	Type         string
	Example      string
	ExampleCn    string
}

// ImportUnit 导入的一个单元。Texts 为单元的篇（[句型] / [课文] 段，spec 0004）。
type ImportUnit struct {
	Name    string
	Entries []ImportEntry
	Texts   []NewUnitText
}

// ImportResult 导入统计（JSON 字段与旧版一致；texts / sentences 是 spec 0004 新增，只在导入了篇时出现）。
type ImportResult struct {
	Units        int `json:"units"`
	UnitsCreated int `json:"unitsCreated"`
	WordsLinked  int `json:"wordsLinked"`
	WordsCreated int `json:"wordsCreated"`
	WordsReused  int `json:"wordsReused"`
	Texts        int `json:"texts,omitempty"`
	Sentences    int `json:"sentences,omitempty"`
}

// EntriesFromParsed 解析结果 → 导入条目（跳过 error 条目，与旧 seed 一致）。
func EntriesFromParsed(entries []vocabparser.ParsedEntry) []ImportEntry {
	out := make([]ImportEntry, 0, len(entries))
	for _, e := range entries {
		if e.Status == vocabparser.StatusError {
			continue
		}
		out = append(out, ImportEntry{Spelling: e.Spelling, Phonetic: e.Phonetic, PartOfSpeech: e.PartOfSpeech, Definition: e.Definition, Type: e.Type})
	}
	return out
}

var anySpace = regexp.MustCompile(`[\t\n\v\f\r \x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]`)

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// ImportUnits 把若干单元导入到词书：单元按名称复用（新单元排在末尾），单词按规整后的拼写全局去重复用，
// 单元内已有的词不重复关联；单元的篇（spec 0004）与单元里已有的篇类型、标题、句子都相同时不重复创建
// （Texts / Sentences 只计新建的）。必须在事务里调用（旧版 importUnits(tx, …)）。
func ImportUnits(ctx context.Context, tx store.Querier, now time.Time, bookID string, units []ImportUnit) (ImportResult, error) {
	var res ImportResult
	ts := store.NewTime(now)

	var lastUnit *int
	if err := tx.QueryRowContext(ctx, `SELECT max("sortOrder") FROM "Unit" WHERE "bookId" = ?`, bookID).Scan(&lastUnit); err != nil {
		return res, err
	}
	unitOrder := 0
	if lastUnit != nil {
		unitOrder = *lastUnit + 1
	}

	// 新词的例句、单元的篇在全部词入库之后再写（句子关联要用到本次导入的新词）
	var exampleWordIDs []string
	type pendingTexts struct {
		unitID string
		texts  []NewUnitText
	}
	var pending []pendingTexts

	for _, u := range units {
		entries := dedupeImport(u.Entries)
		if len(entries) == 0 && len(u.Texts) == 0 {
			continue
		}
		res.Units++

		var unitID string
		err := tx.QueryRowContext(ctx, `SELECT "id" FROM "Unit" WHERE "bookId" = ? AND "name" = ?`, bookID, u.Name).Scan(&unitID)
		if store.IsNoRows(err) {
			unitID = store.NewID()
			if _, err := tx.ExecContext(ctx, `INSERT INTO "Unit" ("id","bookId","name","sortOrder","createdAt") VALUES (?,?,?,?,?)`, unitID, bookID, u.Name, unitOrder, ts); err != nil {
				return res, err
			}
			unitOrder++
			res.UnitsCreated++
		} else if err != nil {
			return res, err
		}
		if len(u.Texts) > 0 {
			pending = append(pending, pendingTexts{unitID: unitID, texts: u.Texts})
		}
		if len(entries) == 0 {
			continue
		}

		ids := map[string]string{}
		for _, e := range entries {
			var id string
			err := tx.QueryRowContext(ctx, `SELECT "id" FROM "Word" WHERE "spelling" = ?`, e.Spelling).Scan(&id)
			if store.IsNoRows(err) {
				continue
			}
			if err != nil {
				return res, err
			}
			ids[e.Spelling] = id
		}
		created := 0
		for _, e := range entries {
			if _, ok := ids[e.Spelling]; ok {
				continue
			}
			typ := e.Type
			if typ == "" {
				typ = "word"
				if anySpace.MatchString(e.Spelling) {
					typ = "phrase"
				}
			}
			id := store.NewID()
			if _, err := tx.ExecContext(ctx, `INSERT INTO "Word" ("id","spelling","type","phonetic","partOfSpeech","definition","example","exampleCn","createdAt","updatedAt") VALUES (?,?,?,?,?,?,?,?,?,?)`,
				id, e.Spelling, typ, nullIfEmpty(e.Phonetic), nullIfEmpty(e.PartOfSpeech), httpx.JSTrim(e.Definition), nullIfEmpty(e.Example), nullIfEmpty(e.ExampleCn), ts, ts); err != nil {
				return res, err
			}
			ids[e.Spelling] = id
			created++
			if strings.TrimSpace(e.Example) != "" {
				exampleWordIDs = append(exampleWordIDs, id)
			}
		}
		res.WordsCreated += created
		res.WordsReused += len(entries) - created

		var last *int
		if err := tx.QueryRowContext(ctx, `SELECT max("sortOrder") FROM "UnitWord" WHERE "unitId" = ?`, unitID).Scan(&last); err != nil {
			return res, err
		}
		order := 0
		if last != nil {
			order = *last + 1
		}
		for _, e := range entries {
			r, err := tx.ExecContext(ctx, `INSERT INTO "UnitWord" ("unitId","wordId","sortOrder") VALUES (?,?,?) ON CONFLICT DO NOTHING`, unitID, ids[e.Spelling], order)
			if err != nil {
				return res, err
			}
			order++ // 与旧版 createMany 一致：跳过的重复项也占一个序号
			n, _ := r.RowsAffected()
			res.WordsLinked += int(n)
		}
	}

	if len(exampleWordIDs) == 0 && len(pending) == 0 {
		return res, nil
	}
	lx, err := LoadLexicon(ctx, tx)
	if err != nil {
		return res, err
	}
	for _, id := range exampleWordIDs {
		if err := SyncExampleSentence(ctx, tx, lx, now, id); err != nil {
			return res, err
		}
	}
	for _, p := range pending {
		// 单元里已有相同的篇（类型、标题、句子都相同）时不重复创建：同一份文件再导入一次（例如补词）保持幂等
		sigs, err := unitTextSignatures(ctx, tx, p.unitID)
		if err != nil {
			return res, err
		}
		for _, t := range p.texts {
			sig := newUnitTextSignature(t)
			if sigs[sig] {
				continue
			}
			if _, err := CreateUnitText(ctx, tx, lx, now, p.unitID, t); err != nil {
				return res, err
			}
			sigs[sig] = true
			res.Texts++
			res.Sentences += len(t.Sentences)
		}
	}
	return res, nil
}

func newUnitTextSignature(t NewUnitText) string {
	ens := make([]string, len(t.Sentences))
	for i, s := range t.Sentences {
		ens[i] = s.En
	}
	return core.UnitTextSignature(t.Kind, t.Title, ens)
}

// unitTextSignatures 单元里已有各篇的识别键（core.UnitTextSignature）。
func unitTextSignatures(ctx context.Context, q store.Querier, unitID string) (map[string]bool, error) {
	rows, err := q.QueryContext(ctx, `SELECT t."id", t."kind", t."title", s."en" FROM "UnitText" t
		LEFT JOIN "UnitTextSentence" uts ON uts."textId" = t."id"
		LEFT JOIN "Sentence" s ON s."id" = uts."sentenceId"
		WHERE t."unitId" = ? ORDER BY t."sortOrder", t."id", uts."sortOrder"`, unitID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type text struct {
		kind, title string
		ens         []string
	}
	var order []string
	byID := map[string]*text{}
	for rows.Next() {
		var id, kind, title string
		var en *string
		if err := rows.Scan(&id, &kind, &title, &en); err != nil {
			return nil, err
		}
		t := byID[id]
		if t == nil {
			t = &text{kind: kind, title: title}
			byID[id] = t
			order = append(order, id)
		}
		if en != nil {
			t.ens = append(t.ens, *en)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(order))
	for _, id := range order {
		t := byID[id]
		out[core.UnitTextSignature(t.kind, t.title, t.ens)] = true
	}
	return out, nil
}

// dedupeImport 规整拼写、去掉空拼写或空释义、单元内按拼写（区分大小写）去重。
func dedupeImport(entries []ImportEntry) []ImportEntry {
	seen := map[string]bool{}
	out := []ImportEntry{}
	for _, e := range entries {
		sp := vocabparser.NormalizeSpelling(e.Spelling)
		if sp == "" || strings.TrimSpace(httpx.JSTrim(e.Definition)) == "" {
			continue
		}
		if seen[sp] {
			continue
		}
		seen[sp] = true
		e.Spelling = sp
		out = append(out, e)
	}
	return out
}

// ScopeWordIDs 计划范围词表：按单元顺序 → 单元内顺序展开并去重（旧 scopeWordIds）。
func ScopeWordIDs(ctx context.Context, q store.Querier, unitIDs []string) ([]string, error) {
	if len(unitIDs) == 0 {
		return []string{}, nil
	}
	rows, err := q.QueryContext(ctx, `SELECT "unitId","wordId","sortOrder" FROM "UnitWord" WHERE "unitId" IN (`+store.Placeholders(len(unitIDs))+`)`, store.Args(unitIDs)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type row struct {
		unitID, wordID string
		sortOrder      int
	}
	var all []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.unitID, &r.wordID, &r.sortOrder); err != nil {
			return nil, err
		}
		all = append(all, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rank := make(map[string]int, len(unitIDs))
	for i, id := range unitIDs {
		rank[id] = i
	}
	sort.SliceStable(all, func(i, j int) bool {
		ri, rj := rank[all[i].unitID], rank[all[j].unitID]
		if ri != rj {
			return ri < rj
		}
		return all[i].sortOrder < all[j].sortOrder
	})
	seen := map[string]bool{}
	out := []string{}
	for _, r := range all {
		if seen[r.wordID] {
			continue
		}
		seen[r.wordID] = true
		out = append(out, r.wordID)
	}
	return out, nil
}

// ===========================================================================
// 词书 / 单元 / 单词（对应旧 routes/books.ts）：A2 补充。
// ===========================================================================

// BookRow Book 表的一行（旧 db.book.create / update 直接返回的原始模型）。
type BookRow struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Description *string    `json:"description"`
	IsSystem    bool       `json:"isSystem"`
	OwnerID     *string    `json:"ownerId"`
	SortOrder   int        `json:"sortOrder"`
	CreatedAt   store.Time `json:"createdAt"`
	UpdatedAt   store.Time `json:"updatedAt"`
}

const bookColumns = `"id","name","description","isSystem","ownerId","sortOrder","createdAt","updatedAt"`

func scanBook(row interface{ Scan(...any) error }) (*BookRow, error) {
	var b BookRow
	if err := row.Scan(&b.ID, &b.Name, &b.Description, &b.IsSystem, &b.OwnerID, &b.SortOrder, &b.CreatedAt, &b.UpdatedAt); err != nil {
		return nil, err
	}
	return &b, nil
}

// GetBookRow 按 id 取原始行；不存在返回 (nil, nil)。
func GetBookRow(ctx context.Context, q store.Querier, id string) (*BookRow, error) {
	b, err := scanBook(q.QueryRowContext(ctx, `SELECT `+bookColumns+` FROM "Book" WHERE "id" = ?`, id))
	if store.IsNoRows(err) {
		return nil, nil
	}
	return b, err
}

// CanEditBook canEdit 展示字段：管理员，或自己拥有的非系统词书（与 AssertCanEditBook 一致：个人版的管理员为系统词书 + 自己的）。
func CanEditBook(a *Actor, isSystem bool, ownerID *string) bool {
	own := ownerID != nil && *ownerID == a.ID
	if SeesAll(a) || (IsAdmin(a) && (isSystem || own)) {
		return true
	}
	return !isSystem && own
}

// NewBookInput 建词书的输入（旧 bookSchema）。
type NewBookInput struct {
	Name        string
	Description *string
	IsSystem    bool
}

// CreateBook 建词书；isSystem 由调用方按 actor 是否管理员决定是否生效。
func CreateBook(ctx context.Context, q store.Querier, now time.Time, ownerID string, in NewBookInput) (*BookRow, error) {
	id := store.NewID()
	ts := store.NewTime(now)
	_, err := q.ExecContext(ctx, `INSERT INTO "Book" ("id","name","description","isSystem","ownerId","sortOrder","createdAt","updatedAt") VALUES (?,?,?,?,?,0,?,?)`,
		id, in.Name, in.Description, in.IsSystem, ownerID, ts, ts)
	if err != nil {
		return nil, err
	}
	return GetBookRow(ctx, q, id)
}

// BookPatch 词书部分更新：nil 表示不改；Description 为非 nil 的 *sql.NullString 时按 Valid 区分「设为值 / 清空为 null」。
type BookPatch struct {
	Name        *string
	Description *sql.NullString
	IsSystem    *bool
}

// UpdateBook 更新词书。与 Prisma 的 `update({ data: {} })` 一致：没有任何字段要改时不执行 UPDATE，
// `updatedAt` 也不刷新（M3；旧版 @updatedAt 只在真正发生一次写入时才生效，空 data 不产生 SQL）。
func UpdateBook(ctx context.Context, q store.Querier, now time.Time, id string, p BookPatch) error {
	var sets []string
	var args []any
	if p.Name != nil {
		sets = append(sets, `"name" = ?`)
		args = append(args, *p.Name)
	}
	if p.Description != nil {
		sets = append(sets, `"description" = ?`)
		if p.Description.Valid {
			args = append(args, p.Description.String)
		} else {
			args = append(args, nil)
		}
	}
	if p.IsSystem != nil {
		sets = append(sets, `"isSystem" = ?`)
		args = append(args, *p.IsSystem)
	}
	if len(sets) == 0 {
		return nil
	}
	sets = append(sets, `"updatedAt" = ?`)
	args = append(args, store.NewTime(now), id)
	_, err := q.ExecContext(ctx, `UPDATE "Book" SET `+strings.Join(sets, ", ")+` WHERE "id" = ?`, args...)
	return err
}

// DeleteBook 删除词书（级联删除其单元、单元关联、单元的篇；不影响全局 Word 行），并清理不再被引用的句子。
func DeleteBook(ctx context.Context, q store.Querier, id string) error {
	sentenceIDs, err := SentenceIDsOfBook(ctx, q, id)
	if err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, `DELETE FROM "Book" WHERE "id" = ?`, id); err != nil {
		return err
	}
	return PruneSentences(ctx, q, sentenceIDs)
}

// AssertBookNotUsedByPlans 词书被未归档计划引用时拒绝删除（旧 assertNotUsedByPlans({ bookId })）。
func AssertBookNotUsedByPlans(ctx context.Context, q store.Querier, bookID string) error {
	var n int
	err := q.QueryRowContext(ctx, `SELECT count(*) FROM "PlanUnit" pu JOIN "Unit" u ON u."id" = pu."unitId" JOIN "Plan" p ON p."id" = pu."planId" WHERE u."bookId" = ? AND p."status" != 'archived'`, bookID).Scan(&n)
	if err != nil {
		return err
	}
	if n > 0 {
		return httpx.NewError(httpx.CodeInvalidAction, fmt.Sprintf("有 %d 个未归档的学习计划正在使用，请先调整计划", n), nil)
	}
	return nil
}

// AssertUnitNotUsedByPlans 单元被未归档计划引用时拒绝删除（旧 assertNotUsedByPlans({ unitId })）。
func AssertUnitNotUsedByPlans(ctx context.Context, q store.Querier, unitID string) error {
	var n int
	err := q.QueryRowContext(ctx, `SELECT count(*) FROM "PlanUnit" pu JOIN "Plan" p ON p."id" = pu."planId" WHERE pu."unitId" = ? AND p."status" != 'archived'`, unitID).Scan(&n)
	if err != nil {
		return err
	}
	if n > 0 {
		return httpx.NewError(httpx.CodeInvalidAction, fmt.Sprintf("有 %d 个未归档的学习计划正在使用，请先调整计划", n), nil)
	}
	return nil
}

// BookListItem 词书列表的一项（旧 GET /books 的 item 形状）。
type BookListItem struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Description *string    `json:"description"`
	IsSystem    bool       `json:"isSystem"`
	OwnerName   *string    `json:"ownerName"`
	UnitCount   int        `json:"unitCount"`
	WordCount   int        `json:"wordCount"`
	CanEdit     bool       `json:"canEdit"`
	CreatedAt   store.Time `json:"createdAt"`
}

// ListBooks 当前操作者可见的词书列表（含单元数、词数），排序：系统词书优先 → sortOrder → 创建时间。
func ListBooks(ctx context.Context, q store.Querier, a *Actor) ([]BookListItem, error) {
	where, args, err := VisibleBookFilter(ctx, q, a, "b")
	if err != nil {
		return nil, err
	}
	query := `SELECT b."id", b."name", b."description", b."isSystem", b."ownerId", u."name", b."createdAt",
		(SELECT count(*) FROM "Unit" un WHERE un."bookId" = b."id") AS unitCount,
		(SELECT count(*) FROM "UnitWord" uw JOIN "Unit" un2 ON un2."id" = uw."unitId" WHERE un2."bookId" = b."id") AS wordCount
		FROM "Book" b LEFT JOIN "User" u ON u."id" = b."ownerId"
		WHERE ` + where + ` ORDER BY b."isSystem" DESC, b."sortOrder" ASC, b."createdAt" ASC`
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []BookListItem{}
	for rows.Next() {
		var it BookListItem
		var ownerID *string
		if err := rows.Scan(&it.ID, &it.Name, &it.Description, &it.IsSystem, &ownerID, &it.OwnerName, &it.CreatedAt, &it.UnitCount, &it.WordCount); err != nil {
			return nil, err
		}
		it.CanEdit = CanEditBook(a, it.IsSystem, ownerID)
		items = append(items, it)
	}
	return items, rows.Err()
}

// BookUnitItem 词书详情里的一个单元。
type BookUnitItem struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	SortOrder int    `json:"sortOrder"`
	WordCount int    `json:"wordCount"`
}

// BookDetail 词书详情（旧 GET /books/:id）。
type BookDetail struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description *string        `json:"description"`
	IsSystem    bool           `json:"isSystem"`
	OwnerName   *string        `json:"ownerName"`
	CanEdit     bool           `json:"canEdit"`
	Units       []BookUnitItem `json:"units"`
}

// GetBookDetail 可见词书的详情；不可见 / 不存在返回 (nil, nil)（路由统一转 404「词书不存在」）。
func GetBookDetail(ctx context.Context, q store.Querier, a *Actor, id string) (*BookDetail, error) {
	where, args, err := VisibleBookFilter(ctx, q, a, "b")
	if err != nil {
		return nil, err
	}
	row := q.QueryRowContext(ctx, `SELECT b."id", b."name", b."description", b."isSystem", b."ownerId", u."name"
		FROM "Book" b LEFT JOIN "User" u ON u."id" = b."ownerId"
		WHERE b."id" = ? AND `+where, append([]any{id}, args...)...)
	var d BookDetail
	var ownerID *string
	if err := row.Scan(&d.ID, &d.Name, &d.Description, &d.IsSystem, &ownerID, &d.OwnerName); err != nil {
		if store.IsNoRows(err) {
			return nil, nil
		}
		return nil, err
	}
	d.CanEdit = CanEditBook(a, d.IsSystem, ownerID)
	rows, err := q.QueryContext(ctx, `SELECT u."id", u."name", u."sortOrder", (SELECT count(*) FROM "UnitWord" uw WHERE uw."unitId" = u."id")
		FROM "Unit" u WHERE u."bookId" = ? ORDER BY u."sortOrder" ASC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	d.Units = []BookUnitItem{}
	for rows.Next() {
		var it BookUnitItem
		if err := rows.Scan(&it.ID, &it.Name, &it.SortOrder, &it.WordCount); err != nil {
			return nil, err
		}
		d.Units = append(d.Units, it)
	}
	return &d, rows.Err()
}

// UnitRow Unit 表的一行（原始模型）。
type UnitRow struct {
	ID        string     `json:"id"`
	BookID    string     `json:"bookId"`
	Name      string     `json:"name"`
	SortOrder int        `json:"sortOrder"`
	CreatedAt store.Time `json:"createdAt"`
}

func scanUnit(row interface{ Scan(...any) error }) (*UnitRow, error) {
	var u UnitRow
	if err := row.Scan(&u.ID, &u.BookID, &u.Name, &u.SortOrder, &u.CreatedAt); err != nil {
		return nil, err
	}
	return &u, nil
}

// GetUnitRow 按 id 取原始行；不存在返回 (nil, nil)。
func GetUnitRow(ctx context.Context, q store.Querier, id string) (*UnitRow, error) {
	u, err := scanUnit(q.QueryRowContext(ctx, `SELECT "id","bookId","name","sortOrder","createdAt" FROM "Unit" WHERE "id" = ?`, id))
	if store.IsNoRows(err) {
		return nil, nil
	}
	return u, err
}

// UnitBook 单元所属词书的归属信息（旧 unitBook()：只取 id/bookId/name，找不到 → NOT_FOUND「单元不存在」）。
func UnitBook(ctx context.Context, q store.Querier, unitID string) (*UnitRow, error) {
	u, err := GetUnitRow(ctx, q, unitID)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, httpx.NotFound("单元不存在")
	}
	return u, nil
}

// CreateUnit 新建单元；sortOrder 为 nil 时排在末尾。
func CreateUnit(ctx context.Context, q store.Querier, now time.Time, bookID, name string, sortOrder *int) (*UnitRow, error) {
	order := 0
	if sortOrder != nil {
		order = *sortOrder
	} else {
		var last *int
		if err := q.QueryRowContext(ctx, `SELECT max("sortOrder") FROM "Unit" WHERE "bookId" = ?`, bookID).Scan(&last); err != nil {
			return nil, err
		}
		if last != nil {
			order = *last + 1
		}
	}
	id := store.NewID()
	if _, err := q.ExecContext(ctx, `INSERT INTO "Unit" ("id","bookId","name","sortOrder","createdAt") VALUES (?,?,?,?,?)`, id, bookID, name, order, store.NewTime(now)); err != nil {
		return nil, err
	}
	return GetUnitRow(ctx, q, id)
}

// UpdateUnit 部分更新（Unit 无 updatedAt 列）。
func UpdateUnit(ctx context.Context, q store.Querier, id string, name *string, sortOrder *int) (*UnitRow, error) {
	var sets []string
	var args []any
	if name != nil {
		sets = append(sets, `"name" = ?`)
		args = append(args, *name)
	}
	if sortOrder != nil {
		sets = append(sets, `"sortOrder" = ?`)
		args = append(args, *sortOrder)
	}
	if len(sets) > 0 {
		args = append(args, id)
		if _, err := q.ExecContext(ctx, `UPDATE "Unit" SET `+strings.Join(sets, ", ")+` WHERE "id" = ?`, args...); err != nil {
			return nil, err
		}
	}
	return GetUnitRow(ctx, q, id)
}

// DeleteUnit 删除单元（级联删除 UnitWord 关联与单元的篇，不影响全局 Word 行），并清理不再被引用的句子。
func DeleteUnit(ctx context.Context, q store.Querier, id string) error {
	sentenceIDs, err := SentenceIDsOfUnits(ctx, q, []string{id})
	if err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, `DELETE FROM "Unit" WHERE "id" = ?`, id); err != nil {
		return err
	}
	return PruneSentences(ctx, q, sentenceIDs)
}

// ReorderUnits 整册一次提交单元顺序：去重、过滤未知 id，必须覆盖该词书全部单元，否则 400。
func ReorderUnits(ctx context.Context, tx store.Querier, bookID string, unitIDs []string) (int, error) {
	known, err := queryStrings(ctx, tx, `SELECT "id" FROM "Unit" WHERE "bookId" = ?`, bookID)
	if err != nil {
		return 0, err
	}
	knownSet := map[string]bool{}
	for _, id := range known {
		knownSet[id] = true
	}
	seen := map[string]bool{}
	ordered := make([]string, 0, len(unitIDs))
	for _, id := range unitIDs {
		if seen[id] || !knownSet[id] {
			continue
		}
		seen[id] = true
		ordered = append(ordered, id)
	}
	if len(ordered) != len(known) {
		return 0, httpx.Validation("单元顺序必须包含本词书的全部单元")
	}
	for i, id := range ordered {
		if _, err := tx.ExecContext(ctx, `UPDATE "Unit" SET "sortOrder" = ? WHERE "id" = ?`, i, id); err != nil {
			return 0, err
		}
	}
	return len(ordered), nil
}

// UnitWordItem 单元内一个词条（旧 GET /units/:id/words 的 item 形状）。
type UnitWordItem struct {
	ID            string  `json:"id"`
	Spelling      string  `json:"spelling"`
	Type          string  `json:"type"`
	Phonetic      *string `json:"phonetic"`
	PartOfSpeech  *string `json:"partOfSpeech"`
	Definition    string  `json:"definition"`
	Example       *string `json:"example"`
	ExampleCn     *string `json:"exampleCn"`
	ExampleSource *string `json:"exampleSource"`
	SortOrder     int     `json:"sortOrder"`
	UsedByUnits   int     `json:"usedByUnits"`
}

// UnitWordsUnitInfo 单元内词条列表里附带的单元信息。
type UnitWordsUnitInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	BookID   string `json:"bookId"`
	BookName string `json:"bookName"`
	CanEdit  bool   `json:"canEdit"`
}

// UnitWordsResult 旧 GET /units/:id/words 的响应体。
type UnitWordsResult struct {
	Unit  UnitWordsUnitInfo `json:"unit"`
	Items []UnitWordItem    `json:"items"`
	Total int               `json:"total"`
	Page  int               `json:"page"`
	Limit int               `json:"limit"`
}

// ListUnitWords 单元内的词条（分页 + 可选按拼写/释义搜索）；单元不可见 / 不存在返回 (nil, nil)。
func ListUnitWords(ctx context.Context, q store.Querier, a *Actor, unitID, search string, page, limit int) (*UnitWordsResult, error) {
	where, args, err := VisibleBookFilter(ctx, q, a, "bk")
	if err != nil {
		return nil, err
	}
	row := q.QueryRowContext(ctx, `SELECT u."id", u."name", bk."id", bk."name", bk."isSystem", bk."ownerId"
		FROM "Unit" u JOIN "Book" bk ON bk."id" = u."bookId" WHERE u."id" = ? AND `+where, append([]any{unitID}, args...)...)
	var res UnitWordsResult
	var isSystem bool
	var ownerID *string
	if err := row.Scan(&res.Unit.ID, &res.Unit.Name, &res.Unit.BookID, &res.Unit.BookName, &isSystem, &ownerID); err != nil {
		if store.IsNoRows(err) {
			return nil, nil
		}
		return nil, err
	}
	res.Unit.CanEdit = CanEditBook(a, isSystem, ownerID)

	filter := `uw."unitId" = ?`
	fargs := []any{unitID}
	if search != "" {
		// 字面子串匹配（不是通配符），与旧版 Prisma `contains` 语义一致：
		// spelling 用 Unicode 大小写不敏感（M4），definition 保持大小写敏感（instr 内置函数，天然按字节比较）。
		filter += ` AND (VINX_ICONTAINS(w."spelling", ?) = 1 OR instr(w."definition", ?) > 0)`
		fargs = append(fargs, search, search)
	}
	var total int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM "UnitWord" uw JOIN "Word" w ON w."id" = uw."wordId" WHERE `+filter, fargs...).Scan(&total); err != nil {
		return nil, err
	}
	res.Total, res.Page, res.Limit = total, page, limit
	rows, err := q.QueryContext(ctx, `SELECT w."id", w."spelling", w."type", w."phonetic", w."partOfSpeech", w."definition", w."example", w."exampleCn", w."exampleSource",
		uw."sortOrder", (SELECT count(*) FROM "UnitWord" uw2 WHERE uw2."wordId" = w."id")
		FROM "UnitWord" uw JOIN "Word" w ON w."id" = uw."wordId" WHERE `+filter+` ORDER BY uw."sortOrder" ASC LIMIT ? OFFSET ?`,
		append(fargs, limit, (page-1)*limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	res.Items = []UnitWordItem{}
	for rows.Next() {
		var it UnitWordItem
		if err := rows.Scan(&it.ID, &it.Spelling, &it.Type, &it.Phonetic, &it.PartOfSpeech, &it.Definition, &it.Example, &it.ExampleCn, &it.ExampleSource, &it.SortOrder, &it.UsedByUnits); err != nil {
			return nil, err
		}
		res.Items = append(res.Items, it)
	}
	return &res, rows.Err()
}

// RemoveUnitWord 移除单元与词条的关联（不删除全局 Word 行）。
func RemoveUnitWord(ctx context.Context, q store.Querier, unitID, wordID string) error {
	_, err := q.ExecContext(ctx, `DELETE FROM "UnitWord" WHERE "unitId" = ? AND "wordId" = ?`, unitID, wordID)
	return err
}

// WordRow Word 表的一行（原始模型，旧 PATCH /words/:id、GET /words 直接返回）。
type WordRow struct {
	ID            string         `json:"id"`
	Spelling      string         `json:"spelling"`
	Type          string         `json:"type"`
	Phonetic      *string        `json:"phonetic"`
	PartOfSpeech  *string        `json:"partOfSpeech"`
	Definition    string         `json:"definition"`
	Example       *string        `json:"example"`
	ExampleCn     *string        `json:"exampleCn"`
	ExampleSource *string        `json:"exampleSource"`
	ExampleAt     store.NullTime `json:"exampleAt"`
	AudioFile     *string        `json:"audioFile"`
	CreatedAt     store.Time     `json:"createdAt"`
	UpdatedAt     store.Time     `json:"updatedAt"`
}

const wordColumns = `"id","spelling","type","phonetic","partOfSpeech","definition","example","exampleCn","exampleSource","exampleAt","audioFile","createdAt","updatedAt"`

func scanWord(row interface{ Scan(...any) error }) (*WordRow, error) {
	var w WordRow
	if err := row.Scan(&w.ID, &w.Spelling, &w.Type, &w.Phonetic, &w.PartOfSpeech, &w.Definition, &w.Example, &w.ExampleCn, &w.ExampleSource, &w.ExampleAt, &w.AudioFile, &w.CreatedAt, &w.UpdatedAt); err != nil {
		return nil, err
	}
	return &w, nil
}

// GetWordRow 按 id 取原始行；不存在返回 (nil, nil)。
func GetWordRow(ctx context.Context, q store.Querier, id string) (*WordRow, error) {
	w, err := scanWord(q.QueryRowContext(ctx, `SELECT `+wordColumns+` FROM "Word" WHERE "id" = ?`, id))
	if store.IsNoRows(err) {
		return nil, nil
	}
	return w, err
}

// FindWordBySpelling 按拼写（唯一列）取原始行；不存在返回 (nil, nil)。
func FindWordBySpelling(ctx context.Context, q store.Querier, spelling string) (*WordRow, error) {
	w, err := scanWord(q.QueryRowContext(ctx, `SELECT `+wordColumns+` FROM "Word" WHERE "spelling" = ?`, spelling))
	if store.IsNoRows(err) {
		return nil, nil
	}
	return w, err
}

// WordUsage 词条在某个可见单元/词书里的引用。
type WordUsage struct {
	UnitID   string `json:"unitId"`
	UnitName string `json:"unitName"`
	BookID   string `json:"bookId"`
	BookName string `json:"bookName"`
}

// WordDetail 词条详情 + 引用它的（可见）单元列表。
type WordDetail struct {
	WordRow
	UsedBy []WordUsage `json:"usedBy"`
}

// wordUsages 某词条在可见词书范围内被引用的单元列表。
func wordUsages(ctx context.Context, q store.Querier, a *Actor, wordID string) ([]WordUsage, error) {
	where, args, err := VisibleBookFilter(ctx, q, a, "bk")
	if err != nil {
		return nil, err
	}
	// 旧版 include 没有 orderBy，实际顺序接近 UnitWord 的插入顺序；用隐式 rowid 近似（M6）。
	rows, err := q.QueryContext(ctx, `SELECT u."id", u."name", bk."id", bk."name"
		FROM "UnitWord" uw JOIN "Unit" u ON u."id" = uw."unitId" JOIN "Book" bk ON bk."id" = u."bookId"
		WHERE uw."wordId" = ? AND `+where+` ORDER BY uw.rowid`, append([]any{wordID}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WordUsage{}
	for rows.Next() {
		var u WordUsage
		if err := rows.Scan(&u.UnitID, &u.UnitName, &u.BookID, &u.BookName); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// GetWordDetail 词条详情：只暴露可见词书中的引用；对非管理员，完全不在可见词书里的词视为不存在。
func GetWordDetail(ctx context.Context, q store.Querier, a *Actor, id string) (*WordDetail, error) {
	w, err := GetWordRow(ctx, q, id)
	if err != nil || w == nil {
		return nil, err
	}
	usages, err := wordUsages(ctx, q, a, id)
	if err != nil {
		return nil, err
	}
	if !SeesAll(a) && len(usages) == 0 {
		return nil, nil
	}
	return &WordDetail{WordRow: *w, UsedBy: usages}, nil
}

// WordEditableBy 某词条是否只被「当前操作者可编辑的词书」引用（旧「该词被系统词书或他人词书引用」判断）。
// 班级版管理员始终可编辑；否则（含个人版管理员）词条必须至少被引用一次，且引用它的每本词书都可编辑。
func WordEditableBy(ctx context.Context, q store.Querier, a *Actor, wordID string) (bool, error) {
	if SeesAll(a) {
		return true, nil
	}
	rows, err := q.QueryContext(ctx, `SELECT DISTINCT bk."isSystem", bk."ownerId"
		FROM "UnitWord" uw JOIN "Unit" u ON u."id" = uw."unitId" JOIN "Book" bk ON bk."id" = u."bookId"
		WHERE uw."wordId" = ?`, wordID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		n++
		var isSystem bool
		var ownerID *string
		if err := rows.Scan(&isSystem, &ownerID); err != nil {
			return false, err
		}
		if !CanEditBook(a, isSystem, ownerID) {
			return false, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return n > 0, nil
}

// WordPatch 词条部分更新（旧 wordSchema.partial()）；nil 表示不改，非 nil 的 *sql.NullString 区分「设为值 / 清空为 null」。
type WordPatch struct {
	Spelling     *string
	Phonetic     *sql.NullString
	PartOfSpeech *sql.NullString
	Definition   *string
	Example      *sql.NullString
	ExampleCn    *sql.NullString
}

// UpdateWord 编辑词条：权限由调用方（handler）先用 WordEditableBy 校验；拼写变更前查重复。
// 影响所有引用它的词书（全局唯一的 Word 行）。与 Prisma 的 `update({ data: {} })` 一致：没有字段要改时
// 不执行 UPDATE，`updatedAt` 也不刷新（M3）。
func UpdateWord(ctx context.Context, q store.Querier, now time.Time, id string, p WordPatch) (*WordRow, error) {
	var sets []string
	var args []any
	add := func(col string, v *sql.NullString) {
		if v == nil {
			return
		}
		sets = append(sets, `"`+col+`" = ?`)
		if v.Valid {
			args = append(args, v.String)
		} else {
			args = append(args, nil)
		}
	}
	if p.Spelling != nil {
		sets = append(sets, `"spelling" = ?`)
		args = append(args, *p.Spelling)
	}
	add("phonetic", p.Phonetic)
	add("partOfSpeech", p.PartOfSpeech)
	if p.Definition != nil {
		sets = append(sets, `"definition" = ?`)
		args = append(args, *p.Definition)
	}
	add("example", p.Example)
	add("exampleCn", p.ExampleCn)
	if len(sets) > 0 {
		sets = append(sets, `"updatedAt" = ?`)
		args = append(args, store.NewTime(now), id)
		if _, err := q.ExecContext(ctx, `UPDATE "Word" SET `+strings.Join(sets, ", ")+` WHERE "id" = ?`, args...); err != nil {
			return nil, err
		}
	}
	if p.Example != nil || p.ExampleCn != nil {
		// 例句双写（spec 0004 §3）
		if err := SyncExampleSentence(ctx, q, nil, now, id); err != nil {
			return nil, err
		}
	}
	return GetWordRow(ctx, q, id)
}

// SearchWords 全局按拼写 / 释义模糊搜索（旧 GET /words，不做可见性过滤，与旧版一致）。
func SearchWords(ctx context.Context, q store.Querier, query string, page, limit int) ([]WordRow, int, error) {
	// 字面子串匹配，与 ListUnitWords 相同的大小写语义（M4）。
	filter := `VINX_ICONTAINS("spelling", ?) = 1 OR instr("definition", ?) > 0`
	var total int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM "Word" WHERE `+filter, query, query).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := q.QueryContext(ctx, `SELECT `+wordColumns+` FROM "Word" WHERE `+filter+` ORDER BY "spelling" ASC LIMIT ? OFFSET ?`, query, query, limit, (page-1)*limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []WordRow{}
	for rows.Next() {
		w, err := scanWord(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *w)
	}
	return items, total, rows.Err()
}

// ===========================================================================
// 导入（旧 POST /books/import/preview、POST /books/import）
// ===========================================================================

// PreviewEntry 导入预览的一个词条（解析结果 + 是否已存在于库中）。
type PreviewEntry struct {
	Spelling           string   `json:"spelling"`
	Phonetic           string   `json:"phonetic"`
	PartOfSpeech       string   `json:"partOfSpeech"`
	Definition         string   `json:"definition"`
	Type               string   `json:"type"`
	Status             string   `json:"status"`
	Issues             []string `json:"issues"`
	Raw                string   `json:"raw"`
	Line               int      `json:"line"`
	Existing           bool     `json:"existing"`
	ExistingDefinition *string  `json:"existingDefinition"`
}

// PreviewUnit 导入预览的一个单元。Texts 为 [句型] / [课文] 段（spec 0004，只在有时出现）。
type PreviewUnit struct {
	Name    string         `json:"name"`
	Entries []PreviewEntry `json:"entries"`
	Texts   []PreviewText  `json:"texts,omitempty"`
}

// PreviewStats 导入预览的统计。Texts / Sentences 是 spec 0004 新增，只在有句型 / 课文时出现。
type PreviewStats struct {
	Units     int `json:"units"`
	Entries   int `json:"entries"`
	OK        int `json:"ok"`
	Warning   int `json:"warning"`
	Error     int `json:"error"`
	Existing  int `json:"existing"`
	Texts     int `json:"texts,omitempty"`
	Sentences int `json:"sentences,omitempty"`
}

// PreviewResult 导入预览的响应体。
type PreviewResult struct {
	Units []PreviewUnit `json:"units"`
	Stats PreviewStats  `json:"stats"`
}

// existingDefinitions 批量查已存在的拼写 → 释义（分批，避免单条 IN 子句参数过多）。
func existingDefinitions(ctx context.Context, q store.Querier, spellings []string) (map[string]string, error) {
	seen := map[string]bool{}
	uniq := make([]string, 0, len(spellings))
	for _, s := range spellings {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		uniq = append(uniq, s)
	}
	out := map[string]string{}
	const batch = 400
	for i := 0; i < len(uniq); i += batch {
		end := i + batch
		if end > len(uniq) {
			end = len(uniq)
		}
		chunk := uniq[i:end]
		rows, err := q.QueryContext(ctx, `SELECT "spelling","definition" FROM "Word" WHERE "spelling" IN (`+store.Placeholders(len(chunk))+`)`, store.Args(chunk)...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var sp, def string
			if err := rows.Scan(&sp, &def); err != nil {
				rows.Close()
				return nil, err
			}
			out[sp] = def
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}

// PreviewImport 解析词表文本并标注已存在的词条（不写库）。
func PreviewImport(ctx context.Context, q store.Querier, text string, splitByInitial bool, defaultUnit string) (*PreviewResult, error) {
	raw := vocabparser.ParseVocabText(text, defaultUnit)
	parsed := make([]vocabparser.ParsedUnit, len(raw))
	for i, u := range raw {
		parsed[i] = vocabparser.DedupeUnitEntries(u)
	}
	if splitByInitial {
		parsed = vocabparser.SplitByInitial(parsed)
	}
	var spellings []string
	for _, u := range parsed {
		for _, e := range u.Entries {
			spellings = append(spellings, e.Spelling)
		}
	}
	existing, err := existingDefinitions(ctx, q, spellings)
	if err != nil {
		return nil, err
	}
	// 句型 / 课文：关联范围是词库 + 本次导入的新词；已知词 = 本次导入到当前单元为止的词
	var lx *Lexicon
	idBySpelling := map[string]string{}
	for _, u := range parsed {
		if len(u.Texts) == 0 {
			continue
		}
		base, err := LoadLexicon(ctx, q)
		if err != nil {
			return nil, err
		}
		for id, w := range base.Words {
			idBySpelling[w.Spelling] = id
		}
		var extra []LexWord
		for _, pu := range parsed {
			for _, e := range pu.Entries {
				if e.Status == vocabparser.StatusError || e.Spelling == "" {
					continue
				}
				if _, ok := idBySpelling[e.Spelling]; !ok {
					id := newWordPrefix + e.Spelling
					idBySpelling[e.Spelling] = id
					extra = append(extra, LexWord{ID: id, Spelling: e.Spelling, Definition: e.Definition})
				}
			}
		}
		lx = base.With(extra)
		break
	}
	known := map[string]bool{}

	res := &PreviewResult{Units: make([]PreviewUnit, 0, len(parsed))}
	for _, u := range parsed {
		pu := PreviewUnit{Name: u.Name, Entries: make([]PreviewEntry, 0, len(u.Entries))}
		if lx != nil {
			for _, e := range u.Entries {
				if id, ok := idBySpelling[e.Spelling]; ok {
					known[id] = true
				}
			}
			if len(u.Texts) > 0 {
				pu.Texts = previewTexts(lx, u.Texts, known)
				for _, t := range u.Texts {
					res.Stats.Texts++
					res.Stats.Sentences += len(t.Sentences)
				}
			}
		}
		for _, e := range u.Entries {
			def, ok := existing[e.Spelling]
			pe := PreviewEntry{
				Spelling: e.Spelling, Phonetic: e.Phonetic, PartOfSpeech: e.PartOfSpeech, Definition: e.Definition,
				Type: e.Type, Status: e.Status, Issues: e.Issues, Raw: e.Raw, Line: e.Line, Existing: ok,
			}
			if ok {
				d := def
				pe.ExistingDefinition = &d
			}
			res.Stats.Entries++
			switch e.Status {
			case vocabparser.StatusOK:
				res.Stats.OK++
			case vocabparser.StatusWarning:
				res.Stats.Warning++
			case vocabparser.StatusError:
				res.Stats.Error++
			}
			if ok {
				res.Stats.Existing++
			}
			pu.Entries = append(pu.Entries, pe)
		}
		res.Units = append(res.Units, pu)
		res.Stats.Units++
	}
	return res, nil
}

// ImportBooksResult 正式导入的响应体（旧 { bookId, ...importUnits() }）。
type ImportBooksResult struct {
	BookID string `json:"bookId"`
	ImportResult
}

// ImportIntoBook 在事务里：必要时新建词书，再导入各单元（旧 POST /books/import 的事务体）。
func ImportIntoBook(ctx context.Context, tx store.Querier, now time.Time, ownerID string, bookID *string, newBook *NewBookInput, units []ImportUnit) (*ImportBooksResult, error) {
	id := ""
	if bookID != nil {
		id = *bookID
	} else {
		b, err := CreateBook(ctx, tx, now, ownerID, *newBook)
		if err != nil {
			return nil, err
		}
		id = b.ID
	}
	r, err := ImportUnits(ctx, tx, now, id, units)
	if err != nil {
		return nil, err
	}
	return &ImportBooksResult{BookID: id, ImportResult: r}, nil
}
