package service

import (
	"context"
	"database/sql"
	"time"

	"github.com/vinx-lab/vinx-vocab-go/internal/core"
	"github.com/vinx-lab/vinx-vocab-go/internal/httpx"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// 目标词书与覆盖进度（spec 0003）：确定目标词书（班级并集或个人），取目标词、正式测试作答和记忆，
// 交给 core/coverage.go 计算。覆盖状态不落库，每次请求现算。

// 目标来源。
const (
	TargetSourceClass = "class"
	TargetSourceOwn   = "own"
	TargetSourceNone  = "none"
)

// TargetBook 目标里的一本词书。
type TargetBook struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Source 只出现在 GET /me/target-books 的 books 里（spec 0008）：class 由班级设置（锁定），own 自己追加。
	// 同一本书既在班级目标里又是自己加的，按班级算。
	Source string `json:"source,omitempty"`
	// ClassNames 设置这本书的班级（source 为 class 时）。
	ClassNames []string `json:"classNames,omitempty"`
}

// TargetClass 提供目标的班级。
type TargetClass struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// TargetBooksView GET /me/target-books：source 为 class 时 classes 是所在班级（books 为这些班级目标的并集，
// 有效自主时再并上自己追加的），否则 classes 为空。
type TargetBooksView struct {
	Source  string        `json:"source"`
	Classes []TargetClass `json:"classes"`
	Books   []TargetBook  `json:"books"`
	// OwnBooks 自己设置的全部目标（UserTargetBook 原样，含与班级重复的、当前不生效的），编辑时整体提交它。
	OwnBooks []TargetBook `json:"ownBooks"`
	// CanEditOwn 自己设置的目标是否生效、能否追加（spec 0008）：不在班里，或所在班级都允许自主安排。
	// 同时也是「能否自建计划」（SelfPlanAllowed）。
	CanEditOwn bool `json:"canEditOwn"`
}

// ------------------------------------------------------------------
// 设置
// ------------------------------------------------------------------

// ClassTargetBooks 班级目标词书（按 sortOrder）。
func ClassTargetBooks(ctx context.Context, q store.Querier, classID string) ([]TargetBook, error) {
	rows, err := q.QueryContext(ctx, `SELECT b."id", b."name" FROM "ClassTargetBook" t JOIN "Book" b ON b."id" = t."bookId"
		WHERE t."classId" = ? ORDER BY t."sortOrder", t.rowid`, classID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TargetBook{}
	for rows.Next() {
		var b TargetBook
		if err := rows.Scan(&b.ID, &b.Name); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func dedupe(ids []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// SetClassTargetBooks 整体替换班级目标（顺序即 sortOrder，重复去掉）；词书须对操作者可见。
// 班级的管理权限由路由先判定。
func SetClassTargetBooks(ctx context.Context, db *store.DB, a *Actor, classID string, bookIDs []string) ([]TargetBook, error) {
	ids := dedupe(bookIDs)
	if err := AssertBooksVisible(ctx, db, a, ids); err != nil {
		return nil, err
	}
	err := db.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM "ClassTargetBook" WHERE "classId" = ?`, classID); err != nil {
			return err
		}
		for i, id := range ids {
			if _, err := tx.ExecContext(ctx, `INSERT INTO "ClassTargetBook" ("classId","bookId","sortOrder") VALUES (?,?,?)`, classID, id, i); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ClassTargetBooks(ctx, db, classID)
}

// SetOwnTargetBooks 设置自己的目标（有班级时也保存，退出所有班级后生效）。
func SetOwnTargetBooks(ctx context.Context, db *store.DB, a *Actor, bookIDs []string) error {
	ids := dedupe(bookIDs)
	if err := AssertBooksVisible(ctx, db, a, ids); err != nil {
		return err
	}
	return db.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM "UserTargetBook" WHERE "userId" = ?`, a.ID); err != nil {
			return err
		}
		for i, id := range ids {
			if _, err := tx.ExecContext(ctx, `INSERT INTO "UserTargetBook" ("userId","bookId","sortOrder") VALUES (?,?,?)`, a.ID, id, i); err != nil {
				return err
			}
		}
		return nil
	})
}

// ------------------------------------------------------------------
// 有效目标
// ------------------------------------------------------------------

// EffectiveTargets 某用户当前生效的目标词书。useClasses 见 TargetUsesClasses。
func EffectiveTargets(ctx context.Context, q store.Querier, useClasses bool, userID string) (*TargetBooksView, error) {
	m, err := effectiveTargetsBatch(ctx, q, useClasses, []string{userID})
	if err != nil {
		return nil, err
	}
	return m[userID], nil
}

// effectiveTargetsBatch 一批用户的有效目标：
//   - useClasses 且至少在一个班：所在班级（按入班顺序）目标的并集，source = class（班级都没设时 books 为空）；
//     所在班级都允许自主安排时（spec 0008），再并上自己设的目标（与班级重复的按班级算）；
//   - 否则自己设的目标：有则 own，没有则 none。
func effectiveTargetsBatch(ctx context.Context, q store.Querier, useClasses bool, userIDs []string) (map[string]*TargetBooksView, error) {
	out := map[string]*TargetBooksView{}
	for _, id := range userIDs {
		out[id] = &TargetBooksView{Source: TargetSourceNone, Classes: []TargetClass{}, Books: []TargetBook{}, OwnBooks: []TargetBook{}, CanEditOwn: true}
	}
	if len(userIDs) == 0 {
		return out, nil
	}
	ph, args := store.Placeholders(len(userIDs)), store.Args(userIDs)

	if useClasses {
		rows, err := q.QueryContext(ctx, `SELECT m."userId", c."id", c."name", c."allowSelfPlan" FROM "ClassMember" m JOIN "Classroom" c ON c."id" = m."classId"
			WHERE m."userId" IN (`+ph+`) ORDER BY m."joinedAt", m.rowid`, args...)
		if err != nil {
			return nil, err
		}
		classIDs := []string{}
		seenClass := map[string]bool{}
		for rows.Next() {
			var uid string
			var c TargetClass
			var allow bool
			if err := rows.Scan(&uid, &c.ID, &c.Name, &allow); err != nil {
				rows.Close()
				return nil, err
			}
			v := out[uid]
			v.Source = TargetSourceClass
			v.Classes = append(v.Classes, c)
			v.CanEditOwn = v.CanEditOwn && allow // 多班取最严（core.SelfPlanAllowed）
			if !seenClass[c.ID] {
				seenClass[c.ID] = true
				classIDs = append(classIDs, c.ID)
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		if len(classIDs) > 0 {
			trows, err := q.QueryContext(ctx, `SELECT t."classId", b."id", b."name" FROM "ClassTargetBook" t JOIN "Book" b ON b."id" = t."bookId"
				WHERE t."classId" IN (`+store.Placeholders(len(classIDs))+`) ORDER BY t."sortOrder", t.rowid`, store.Args(classIDs)...)
			if err != nil {
				return nil, err
			}
			byClass := map[string][]TargetBook{}
			for trows.Next() {
				var cid string
				var b TargetBook
				if err := trows.Scan(&cid, &b.ID, &b.Name); err != nil {
					trows.Close()
					return nil, err
				}
				byClass[cid] = append(byClass[cid], b)
			}
			trows.Close()
			if err := trows.Err(); err != nil {
				return nil, err
			}
			for _, v := range out {
				idx := map[string]int{}
				for _, c := range v.Classes {
					for _, b := range byClass[c.ID] {
						if i, ok := idx[b.ID]; ok {
							v.Books[i].ClassNames = append(v.Books[i].ClassNames, c.Name)
							continue
						}
						idx[b.ID] = len(v.Books)
						b.Source = TargetSourceClass
						b.ClassNames = []string{c.Name}
						v.Books = append(v.Books, b)
					}
				}
			}
		}
	}

	rows, err := q.QueryContext(ctx, `SELECT t."userId", b."id", b."name" FROM "UserTargetBook" t JOIN "Book" b ON b."id" = t."bookId"
		WHERE t."userId" IN (`+ph+`) ORDER BY t."sortOrder", t.rowid`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var uid string
		var b TargetBook
		if err := rows.Scan(&uid, &b.ID, &b.Name); err != nil {
			return nil, err
		}
		v := out[uid]
		v.OwnBooks = append(v.OwnBooks, b)
		if !v.CanEditOwn {
			continue // 班级不允许自主：自己设的保留但不生效
		}
		dup := false
		for _, x := range v.Books {
			if x.ID == b.ID {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		if v.Source == TargetSourceNone {
			v.Source = TargetSourceOwn
		}
		b.Source = TargetSourceOwn
		v.Books = append(v.Books, b)
	}
	return out, rows.Err()
}

// TargetBookIDs 有效目标的词书 id。
func (v *TargetBooksView) TargetBookIDs() []string {
	ids := make([]string, len(v.Books))
	for i, b := range v.Books {
		ids[i] = b.ID
	}
	return ids
}

// IncludesOwn 有效目标里有自己追加的书（班级概览「含自选」）。
func (v *TargetBooksView) IncludesOwn() bool {
	for _, b := range v.Books {
		if b.Source == TargetSourceOwn {
			return true
		}
	}
	return false
}

// ------------------------------------------------------------------
// 覆盖计算
// ------------------------------------------------------------------

// userCoverage 一个用户的目标与每个目标词的状态（spec 0009 五级状态）。
type userCoverage struct {
	targets *TargetBooksView
	books   []core.CoverageBook
	stages  map[string]core.WordStageInfo
}

// status 一个目标词的三档覆盖状态（接口兼容）。
func (c *userCoverage) status(wordID string) core.CoverageStatus {
	return core.CoverageStatusOf(c.stages[wordID].Stage)
}

// bookWords 词书的词（按单元顺序、单元内顺序；不去重，由 core 去重）。
func bookWords(ctx context.Context, q store.Querier, bookIDs []string) (map[string][]string, error) {
	out := map[string][]string{}
	if len(bookIDs) == 0 {
		return out, nil
	}
	rows, err := q.QueryContext(ctx, `SELECT u."bookId", uw."wordId" FROM "UnitWord" uw JOIN "Unit" u ON u."id" = uw."unitId"
		WHERE u."bookId" IN (`+store.Placeholders(len(bookIDs))+`) ORDER BY u."sortOrder", u.rowid, uw."sortOrder", uw.rowid`, store.Args(bookIDs)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var b, w string
		if err := rows.Scan(&b, &w); err != nil {
			return nil, err
		}
		out[b] = append(out[b], w)
	}
	return out, rows.Err()
}

// coverageBatch 一批用户的覆盖状态（班级概览一次算全班）。
func coverageBatch(ctx context.Context, q store.Querier, loc *time.Location, now time.Time, useClasses bool, userIDs []string) (map[string]*userCoverage, error) {
	targets, err := effectiveTargetsBatch(ctx, q, useClasses, userIDs)
	if err != nil {
		return nil, err
	}
	return coverageBatchFor(ctx, q, loc, now, targets, userIDs)
}

// coverageBatchFor 按给定的目标（每个用户一份）算覆盖状态；班级概览按班级口径时所有人用同一份班级目标（spec 0008）。
func coverageBatchFor(ctx context.Context, q store.Querier, loc *time.Location, now time.Time, targets map[string]*TargetBooksView, userIDs []string) (map[string]*userCoverage, error) {
	out := map[string]*userCoverage{}
	bookSet := map[string]bool{}
	bookIDs := []string{}
	active := []string{} // 有目标的用户
	for _, id := range userIDs {
		t := targets[id]
		out[id] = &userCoverage{targets: t, books: []core.CoverageBook{}, stages: map[string]core.WordStageInfo{}}
		if len(t.Books) > 0 {
			active = append(active, id)
		}
		for _, b := range t.Books {
			if !bookSet[b.ID] {
				bookSet[b.ID] = true
				bookIDs = append(bookIDs, b.ID)
			}
		}
	}
	if len(active) == 0 {
		return out, nil
	}
	words, err := bookWords(ctx, q, bookIDs)
	if err != nil {
		return nil, err
	}
	targetWords := map[string]map[string]bool{}
	for _, id := range active {
		c := out[id]
		set := map[string]bool{}
		for _, b := range c.targets.Books {
			ws := words[b.ID]
			c.books = append(c.books, core.CoverageBook{BookID: b.ID, Name: b.Name, WordIDs: ws})
			for _, w := range ws {
				set[w] = true
			}
		}
		targetWords[id] = set
	}

	evidence, err := loadEvidence(ctx, q, active)
	if err != nil {
		return nil, err
	}
	memory, err := loadStageMemories(ctx, q, active)
	if err != nil {
		return nil, err
	}
	now = msTime(now)
	_, _, dayEnd := TodayRange(loc, now)
	for _, id := range active {
		c := out[id]
		for w := range targetWords[id] {
			c.stages[w] = core.WordStage(evidence[id][w], memory[id][w], now, dayEnd)
		}
	}
	return out, nil
}

// CoverageView GET /records/coverage。
type CoverageView struct {
	Source string                    `json:"source"`
	Total  core.CoverageCounts       `json:"total"`
	Books  []core.CoverageBookCounts `json:"books"`
}

// UserCoverage 覆盖进度（没有目标时 books 为空、数字全 0）。
func UserCoverage(ctx context.Context, q store.Querier, loc *time.Location, now time.Time, useClasses bool, userID string) (*CoverageView, error) {
	m, err := coverageBatch(ctx, q, loc, now, useClasses, []string{userID})
	if err != nil {
		return nil, err
	}
	c := m[userID]
	total, per := core.SummarizeCoverage(c.stages, c.books)
	return &CoverageView{Source: c.targets.Source, Total: total, Books: per}, nil
}

// ClassStudentCoverage 班级概览里学生的覆盖数字。
type ClassStudentCoverage struct {
	Target   int `json:"target"`
	Tested   int `json:"tested"`
	Learning int `json:"learning"`
	// SelfGraded 已测的目标词里，最近一次正式测试是自批默写单的词数（spec 0006）；
	// SelfGradedRatio = SelfGraded / Tested，还没有已测的词时为 null。
	SelfGraded      int      `json:"selfGraded"`
	SelfGradedRatio *float64 `json:"selfGradedRatio"`
}

// 班级概览「目标覆盖」的口径（spec 0008 §8）。
const (
	// CoverageModeClass 班级不允许自主：只按这个班的目标算，全班同一个分母。
	CoverageModeClass = "class"
	// CoverageModeStudent 班级允许自主：按每个学生自己的有效目标（含自选）算。
	CoverageModeStudent = "student"
)

// classCoverage 班级概览的目标覆盖：mode 为 class 时每个学生都按 classID 的目标算，
// 否则按各自的有效目标；没有目标为 nil。includesOwn 是有效目标里含自选书的学生。
func classCoverage(ctx context.Context, q store.Querier, loc *time.Location, now time.Time, classID, mode string, userIDs []string) (map[string]*ClassStudentCoverage, map[string]bool, error) {
	var targets map[string]*TargetBooksView
	includesOwn := map[string]bool{}
	if mode == CoverageModeClass {
		books, err := ClassTargetBooks(ctx, q, classID)
		if err != nil {
			return nil, nil, err
		}
		targets = map[string]*TargetBooksView{}
		for _, id := range userIDs {
			targets[id] = &TargetBooksView{Source: TargetSourceClass, Classes: []TargetClass{}, Books: books, OwnBooks: []TargetBook{}}
		}
	} else {
		var err error
		if targets, err = effectiveTargetsBatch(ctx, q, true, userIDs); err != nil {
			return nil, nil, err
		}
		for id, t := range targets {
			if t.IncludesOwn() {
				includesOwn[id] = true
			}
		}
	}
	m, err := coverageBatchFor(ctx, q, loc, now, targets, userIDs)
	if err != nil {
		return nil, nil, err
	}
	out := map[string]*ClassStudentCoverage{}
	for id, c := range m {
		if len(c.targets.Books) == 0 {
			continue
		}
		total, _ := core.SummarizeCoverage(c.stages, c.books)
		self := core.CountSelfGraded(c.stages, c.books)
		var ratio *float64
		if total.Tested > 0 {
			r := float64(self) / float64(total.Tested)
			ratio = &r
		}
		out[id] = &ClassStudentCoverage{Target: total.Target, Tested: total.Tested, Learning: total.Learning, SelfGraded: self, SelfGradedRatio: ratio}
	}
	return out, includesOwn, nil
}

// targetWordIDs 目标词（bookId 非空时只取这本书；须在目标里，否则 404）按顺序列出，status 非空时只取该状态。
func targetWordIDs(ctx context.Context, q store.Querier, loc *time.Location, now time.Time, useClasses bool, userID, bookID, status string) ([]string, *userCoverage, error) {
	m, err := coverageBatch(ctx, q, loc, now, useClasses, []string{userID})
	if err != nil {
		return nil, nil, err
	}
	c := m[userID]
	books := c.books
	if bookID != "" {
		books = nil
		for _, b := range c.books {
			if b.BookID == bookID {
				books = []core.CoverageBook{b}
				break
			}
		}
		if books == nil {
			return nil, nil, httpx.NotFound("这本书不在目标词书里")
		}
	}
	out := []string{}
	for _, w := range core.CoverageWordOrder(books) {
		if status == "" || c.status(w) == status || c.stages[w].Stage == status {
			out = append(out, w)
		}
	}
	return out, c, nil
}

// CoverageWordItem GET /records/coverage/words 的一项。
type CoverageWordItem struct {
	WordID       string  `json:"wordId"`
	Spelling     string  `json:"spelling"`
	Phonetic     *string `json:"phonetic"`
	PartOfSpeech *string `json:"partOfSpeech"`
	Definition   string  `json:"definition"`
	Status       string  `json:"status"`
	// Stage 五级状态；Due 待复查；Forgetting 可能忘了（spec 0009）。
	Stage      string `json:"stage"`
	Due        bool   `json:"due"`
	Forgetting bool   `json:"forgetting"`
}

// CoverageWords 某本书（bookID 为空时为全部目标词）某种状态（为空不过滤）的词表，按书序、书内顺序分页。
func CoverageWords(ctx context.Context, q store.Querier, loc *time.Location, now time.Time, useClasses bool, userID, bookID, status string, page, limit int) (httpx.Paginated[CoverageWordItem], error) {
	ids, cov, err := targetWordIDs(ctx, q, loc, now, useClasses, userID, bookID, status)
	if err != nil {
		return httpx.Paginated[CoverageWordItem]{}, err
	}
	total := len(ids)
	from := min((page-1)*limit, total)
	to := min(from+limit, total)
	pageIDs := ids[from:to]
	words, err := sheetWordDetails(ctx, q, pageIDs)
	if err != nil {
		return httpx.Paginated[CoverageWordItem]{}, err
	}
	items := make([]CoverageWordItem, 0, len(pageIDs))
	for _, id := range pageIDs {
		w, ok := words[id]
		if !ok {
			continue
		}
		st := cov.stages[id]
		items = append(items, CoverageWordItem{WordID: w.ID, Spelling: w.Spelling, Phonetic: w.Phonetic, PartOfSpeech: w.PartOfSpeech, Definition: w.Definition,
			Status: cov.status(id), Stage: st.Stage, Due: st.Due, Forgetting: st.Forgetting})
	}
	return httpx.Page(items, total, page, limit), nil
}
