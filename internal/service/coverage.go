package service

import (
	"context"
	"database/sql"

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
}

// TargetClass 提供目标的班级。
type TargetClass struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// TargetBooksView GET /me/target-books：source 为 class 时 classes 是所在班级（books 为这些班级目标的并集），
// 否则 classes 为空。
type TargetBooksView struct {
	Source  string        `json:"source"`
	Classes []TargetClass `json:"classes"`
	Books   []TargetBook  `json:"books"`
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
//   - 否则自己设的目标：有则 own，没有则 none。
func effectiveTargetsBatch(ctx context.Context, q store.Querier, useClasses bool, userIDs []string) (map[string]*TargetBooksView, error) {
	out := map[string]*TargetBooksView{}
	for _, id := range userIDs {
		out[id] = &TargetBooksView{Source: TargetSourceNone, Classes: []TargetClass{}, Books: []TargetBook{}}
	}
	if len(userIDs) == 0 {
		return out, nil
	}
	ph, args := store.Placeholders(len(userIDs)), store.Args(userIDs)

	if useClasses {
		rows, err := q.QueryContext(ctx, `SELECT m."userId", c."id", c."name" FROM "ClassMember" m JOIN "Classroom" c ON c."id" = m."classId"
			WHERE m."userId" IN (`+ph+`) ORDER BY m."joinedAt", m.rowid`, args...)
		if err != nil {
			return nil, err
		}
		classIDs := []string{}
		seenClass := map[string]bool{}
		for rows.Next() {
			var uid string
			var c TargetClass
			if err := rows.Scan(&uid, &c.ID, &c.Name); err != nil {
				rows.Close()
				return nil, err
			}
			v := out[uid]
			v.Source = TargetSourceClass
			v.Classes = append(v.Classes, c)
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
				seen := map[string]bool{}
				for _, c := range v.Classes {
					for _, b := range byClass[c.ID] {
						if !seen[b.ID] {
							seen[b.ID] = true
							v.Books = append(v.Books, b)
						}
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
		if v.Source == TargetSourceClass {
			continue
		}
		v.Source = TargetSourceOwn
		v.Books = append(v.Books, b)
	}
	return out, rows.Err()
}

// ------------------------------------------------------------------
// 覆盖计算
// ------------------------------------------------------------------

// userCoverage 一个用户的目标与每个目标词的状态。
type userCoverage struct {
	targets *TargetBooksView
	books   []core.CoverageBook
	status  map[string]core.CoverageStatus
	// selfGraded 最近一次正式测试是自批默写单的词（spec 0006）。
	selfGraded map[string]bool
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
func coverageBatch(ctx context.Context, q store.Querier, useClasses bool, userIDs []string) (map[string]*userCoverage, error) {
	targets, err := effectiveTargetsBatch(ctx, q, useClasses, userIDs)
	if err != nil {
		return nil, err
	}
	out := map[string]*userCoverage{}
	bookSet := map[string]bool{}
	bookIDs := []string{}
	active := []string{} // 有目标的用户
	for _, id := range userIDs {
		t := targets[id]
		out[id] = &userCoverage{targets: t, books: []core.CoverageBook{}, status: map[string]core.CoverageStatus{}, selfGraded: map[string]bool{}}
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

	ph, args := store.Placeholders(len(active)), store.Args(active)
	// 已交卷的检测类组（判定重测）
	// 默写单的批改组另取快照（判定自批，spec 0006）
	srows, err := q.QueryContext(ctx, `SELECT s."id",s."userId",s."kind",s."planId",s."sheetId",s."completedAt",
			CASE WHEN w."format" = 'dictation' THEN s."snapshot" END
		FROM "StudySession" s LEFT JOIN "WordSheet" w ON w."id" = s."sheetId"
		WHERE s."userId" IN (`+ph+`) AND s."kind" IN ('test','sheet') AND s."status" = 'completed' ORDER BY s."completedAt", s.rowid`, args...)
	if err != nil {
		return nil, err
	}
	type sessInfo struct {
		kind       string
		at         store.NullTime
		selfGraded bool
	}
	sessions := map[string]sessInfo{}
	refs := []core.TestSessionRef{}
	for srows.Next() {
		var id, uid, kind string
		var planID, sheetID, dictSnapshot *string
		var at store.NullTime
		if err := srows.Scan(&id, &uid, &kind, &planID, &sheetID, &at, &dictSnapshot); err != nil {
			srows.Close()
			return nil, err
		}
		key := planID
		if kind == "sheet" {
			key = sheetID
		}
		self := false
		if dictSnapshot != nil {
			sg, err := snapshotSelfGraded(*dictSnapshot)
			if err != nil {
				srows.Close()
				return nil, err
			}
			self = sg != nil && *sg
		}
		sessions[id] = sessInfo{kind: kind, at: at, selfGraded: self}
		refs = append(refs, core.TestSessionRef{ID: id, Kind: kind, GroupKey: key, CompletedAt: at.Time})
	}
	srows.Close()
	if err := srows.Err(); err != nil {
		return nil, err
	}
	formal := core.FormalTestSessions(refs)

	answers := map[string]map[string][]core.CoverageAnswer{} // userId → wordId → 作答
	arows, err := q.QueryContext(ctx, `SELECT a."userId", a."wordId", a."sessionId", a."phase", a."attempt", a."correct"
		FROM "Answer" a JOIN "StudySession" s ON s."id" = a."sessionId"
		WHERE a."userId" IN (`+ph+`) AND s."kind" IN ('test','sheet') AND s."status" = 'completed' AND a."phase" = 'test' AND a."attempt" = 1
		ORDER BY s."completedAt", s.rowid, a.rowid`, args...)
	if err != nil {
		return nil, err
	}
	for arows.Next() {
		var uid, wid, sid, phase string
		var attempt int
		var correct bool
		if err := arows.Scan(&uid, &wid, &sid, &phase, &attempt, &correct); err != nil {
			arows.Close()
			return nil, err
		}
		if !targetWords[uid][wid] {
			continue
		}
		info := sessions[sid]
		if answers[uid] == nil {
			answers[uid] = map[string][]core.CoverageAnswer{}
		}
		answers[uid][wid] = append(answers[uid][wid], core.CoverageAnswer{
			SessionID: sid, Kind: info.kind, Phase: phase, Attempt: attempt, Correct: correct, Retake: !formal[sid], At: info.at.Time,
			SelfGraded: info.selfGraded,
		})
	}
	arows.Close()
	if err := arows.Err(); err != nil {
		return nil, err
	}

	memory := map[string]map[string]*core.CoverageMemory{}
	mrows, err := q.QueryContext(ctx, `SELECT "userId","wordId","stability","lastReview" FROM "MemoryState" WHERE "userId" IN (`+ph+`)`, args...)
	if err != nil {
		return nil, err
	}
	for mrows.Next() {
		var uid, wid string
		var st float64
		var last store.NullTime
		if err := mrows.Scan(&uid, &wid, &st, &last); err != nil {
			mrows.Close()
			return nil, err
		}
		if !targetWords[uid][wid] {
			continue
		}
		m := &core.CoverageMemory{Stability: st}
		if last.Valid {
			t := last.Time
			m.LastReview = &t
		}
		if memory[uid] == nil {
			memory[uid] = map[string]*core.CoverageMemory{}
		}
		memory[uid][wid] = m
	}
	mrows.Close()
	if err := mrows.Err(); err != nil {
		return nil, err
	}

	for _, id := range active {
		c := out[id]
		for w := range targetWords[id] {
			st, self := core.WordCoverageDetail(answers[id][w], memory[id][w])
			c.status[w] = st
			if self {
				c.selfGraded[w] = true
			}
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
func UserCoverage(ctx context.Context, q store.Querier, useClasses bool, userID string) (*CoverageView, error) {
	m, err := coverageBatch(ctx, q, useClasses, []string{userID})
	if err != nil {
		return nil, err
	}
	c := m[userID]
	total, per := core.SummarizeCoverage(c.status, c.books)
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

// classCoverage 班级概览：每个学生按自己的有效目标（所在全部班级的并集）；没有目标为 nil。
func classCoverage(ctx context.Context, q store.Querier, userIDs []string) (map[string]*ClassStudentCoverage, error) {
	m, err := coverageBatch(ctx, q, true, userIDs)
	if err != nil {
		return nil, err
	}
	out := map[string]*ClassStudentCoverage{}
	for id, c := range m {
		if len(c.targets.Books) == 0 {
			continue
		}
		total, _ := core.SummarizeCoverage(c.status, c.books)
		self := core.CountSelfGraded(c.status, c.selfGraded, c.books)
		var ratio *float64
		if total.Tested > 0 {
			r := float64(self) / float64(total.Tested)
			ratio = &r
		}
		out[id] = &ClassStudentCoverage{Target: total.Target, Tested: total.Tested, Learning: total.Learning, SelfGraded: self, SelfGradedRatio: ratio}
	}
	return out, nil
}

// targetWordIDs 目标词（bookId 非空时只取这本书；须在目标里，否则 404）按顺序列出，status 非空时只取该状态。
func targetWordIDs(ctx context.Context, q store.Querier, useClasses bool, userID, bookID, status string) ([]string, map[string]core.CoverageStatus, error) {
	m, err := coverageBatch(ctx, q, useClasses, []string{userID})
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
		if status == "" || c.status[w] == status {
			out = append(out, w)
		}
	}
	return out, c.status, nil
}

// CoverageWordItem GET /records/coverage/words 的一项。
type CoverageWordItem struct {
	WordID       string  `json:"wordId"`
	Spelling     string  `json:"spelling"`
	Phonetic     *string `json:"phonetic"`
	PartOfSpeech *string `json:"partOfSpeech"`
	Definition   string  `json:"definition"`
	Status       string  `json:"status"`
}

// CoverageWords 某本书（bookID 为空时为全部目标词）某种状态（为空不过滤）的词表，按书序、书内顺序分页。
func CoverageWords(ctx context.Context, q store.Querier, useClasses bool, userID, bookID, status string, page, limit int) (httpx.Paginated[CoverageWordItem], error) {
	ids, status2, err := targetWordIDs(ctx, q, useClasses, userID, bookID, status)
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
		items = append(items, CoverageWordItem{WordID: w.ID, Spelling: w.Spelling, Phonetic: w.Phonetic, PartOfSpeech: w.PartOfSpeech, Definition: w.Definition, Status: status2[id]})
	}
	return httpx.Page(items, total, page, limit), nil
}
