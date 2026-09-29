package service

import (
	"context"
	"testing"
	"time"

	"github.com/vinx-lab/vinx-vocab-go/internal/core"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// 单词单服务层测试：core.SelectUnfamiliarWords / core.SelectFromPool / core.SplitSheets 的纯函数部分
// 已在 internal/core 表驱动覆盖；这里覆盖装配与数据库查询的部分（打分候选的 SQL、批量取号、列表状态、
// 今日页取下一份 NextSheet——旧 pickNextSheet 的等价场景，NextSheet 本身是 A4 已实现的函数，不在这里重复定义）。

func insertMemory(t *testing.T, db *store.DB, userID, wordID string, due time.Time, stability float64, lapses int) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO "MemoryState" ("id","userId","wordId","due","stability","difficulty","lapses","introducedDay") VALUES (?,?,?,?,?,?,?,?)`,
		store.NewID(), userID, wordID, store.NewTime(due), stability, 5.0, lapses, "2026-09-01"); err != nil {
		t.Fatal(err)
	}
}

func TestPreviewSheetUnfamiliarDefault(t *testing.T) {
	f := newLearnFixture(t, 5)
	ctx := context.Background()
	now := sh(2026, 9, 23, 12, 0, 0)
	// w00：巩固中（不熟）；w01：已掌握但遗忘过（保留）；w02：已掌握且干净（排除）；w03/w04：未学过（无记忆）
	insertMemory(t, f.db, f.userID, f.wordIDs[0], now.Add(30*24*time.Hour), 10, 0)
	insertMemory(t, f.db, f.userID, f.wordIDs[1], now.Add(30*24*time.Hour), 25, 1)
	insertMemory(t, f.db, f.userID, f.wordIDs[2], now.Add(30*24*time.Hour), 25, 0)

	out, err := PreviewSheet(ctx, f.db, shanghai, now, f.userID, 10, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, it := range out.Items {
		ids[it.WordID] = true
		if it.Spelling == "" {
			t.Fatalf("item %+v missing spelling", it)
		}
	}
	if !ids[f.wordIDs[0]] || !ids[f.wordIDs[1]] {
		t.Fatalf("want w00/w01 included, got %+v", out.Items)
	}
	if ids[f.wordIDs[2]] {
		t.Fatalf("w02 (mastered, clean) should be excluded, got %+v", out.Items)
	}

	// count 越界由 API 层校验；服务层直接按传入 count 截断
	out2, err := PreviewSheet(ctx, f.db, shanghai, now, f.userID, 1, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out2.Items) != 1 {
		t.Fatalf("want 1 item, got %d", len(out2.Items))
	}
}

func TestPreviewSheetSourceUnit(t *testing.T) {
	f := newLearnFixture(t, 4)
	ctx := context.Background()
	now := sh(2026, 9, 23, 12, 0, 0)
	insertMemory(t, f.db, f.userID, f.wordIDs[0], now.Add(30*24*time.Hour), 25, 0) // 已掌握且干净：排除
	insertMemory(t, f.db, f.userID, f.wordIDs[1], now.Add(30*24*time.Hour), 3, 1)  // 遗忘过：最不熟

	out, err := PreviewSheet(ctx, f.db, shanghai, now, f.userID, 10, nil, &SheetSource{Kind: "unit", UnitID: f.unitID})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 3 { // 4 词 - 1 已掌握且干净
		t.Fatalf("want 3 items, got %d: %+v", len(out.Items), out.Items)
	}
	if out.Items[0].WordID != f.wordIDs[1] {
		t.Fatalf("want w01 first (遗忘过最不熟), got %+v", out.Items[0])
	}
	for _, it := range out.Items {
		if it.WordID == f.wordIDs[0] {
			t.Fatalf("w00 (mastered, clean) should be excluded")
		}
	}
}

func TestCreateSheetSeqAndValidation(t *testing.T) {
	f := newLearnFixture(t, 3)
	ctx := context.Background()
	now := sh(2026, 9, 23, 12, 0, 0)
	modes := []string{"recognition", "spelling"}

	r1, err := CreateSheet(ctx, f.db, now, f.userID, f.userID, f.wordIDs[:2], modes, 1, core.SheetPerMax)
	if err != nil {
		t.Fatal(err)
	}
	if r1.Seq != 1 || len(r1.Items) != 1 {
		t.Fatalf("got %+v", r1)
	}
	r2, err := CreateSheet(ctx, f.db, now, f.userID, f.userID, f.wordIDs[2:3], modes, 1, core.SheetPerMax)
	if err != nil {
		t.Fatal(err)
	}
	if r2.Seq != 2 {
		t.Fatalf("want seq 2, got %d", r2.Seq)
	}

	if _, err := CreateSheet(ctx, f.db, now, f.userID, f.userID, []string{"nope"}, modes, 1, core.SheetPerMax); err == nil {
		t.Fatal("want error for unknown word")
	} else if got := err.Error(); got != "VALIDATION: 有单词不存在" {
		t.Fatalf("got %q", got)
	}

	if _, err := CreateSheet(ctx, f.db, now, f.userID, f.userID, f.wordIDs, modes, 1, 1); err == nil {
		t.Fatal("want split error")
	}
}

func TestListSheetsStatusTransitions(t *testing.T) {
	f := newLearnFixture(t, 3)
	ctx := context.Background()
	now := sh(2026, 9, 23, 12, 0, 0)
	insertMemory(t, f.db, f.userID, f.wordIDs[0], now.Add(-time.Hour), 5, 0)
	insertMemory(t, f.db, f.userID, f.wordIDs[1], now.Add(-time.Hour), 5, 0)

	created, err := CreateSheet(ctx, f.db, now, f.userID, f.userID, f.wordIDs[:2], []string{"recognition", "spelling"}, 1, core.SheetPerMax)
	if err != nil {
		t.Fatal(err)
	}

	page, err := ListSheets(ctx, f.db, f.userID, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Items[0].Status != "pending" || page.Items[0].WordCount != 2 || page.Items[0].FirstResult != nil || page.Items[0].ActiveSessionID != nil {
		t.Fatalf("got %+v", page.Items[0])
	}

	res, err := StartSession(ctx, f.db, shanghai, now, core.SeededRng(1), f.userID, StartInput{Kind: "sheet", SheetID: &created.ID})
	if err != nil {
		t.Fatal(err)
	}
	page, err = ListSheets(ctx, f.db, f.userID, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if page.Items[0].Status != "testing" || page.Items[0].ActiveSessionID == nil || *page.Items[0].ActiveSessionID != res.Session.ID {
		t.Fatalf("got %+v", page.Items[0])
	}

	snap := snapOf(t, res.Session)
	for _, item := range snap.Items {
		if _, err := RecordAnswer(ctx, f.db, shanghai, now, f.userID, res.Session.ID, AnswerInput{WordID: item.WordID, Mode: "recognition", Phase: "test", Attempt: 1, Answer: item.Definition}); err != nil {
			t.Fatal(err)
		}
		if _, err := RecordAnswer(ctx, f.db, shanghai, now, f.userID, res.Session.ID, AnswerInput{WordID: item.WordID, Mode: "spelling", Phase: "test", Attempt: 1, Answer: item.Answer}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := CompleteSession(ctx, f.db, shanghai, now, f.userID, res.Session.ID); err != nil {
		t.Fatal(err)
	}
	page, err = ListSheets(ctx, f.db, f.userID, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	item := page.Items[0]
	if item.Status != "tested" || item.ActiveSessionID != nil || item.FirstResult == nil {
		t.Fatalf("got %+v", item)
	}
	if item.FirstResult.SessionID != res.Session.ID || item.FirstResult.Correct != 4 || item.FirstResult.Total != 4 {
		t.Fatalf("got %+v", item.FirstResult)
	}
}

func TestSheetDetailOwnerAndDelete(t *testing.T) {
	f := newLearnFixture(t, 2)
	ctx := context.Background()
	now := sh(2026, 9, 23, 12, 0, 0)
	created, err := CreateSheet(ctx, f.db, now, f.userID, f.userID, f.wordIDs, []string{"cloze"}, 1, core.SheetPerMax)
	if err != nil {
		t.Fatal(err)
	}

	detail, err := SheetDetail(ctx, f.db, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.UserID != f.userID || detail.Seq != 1 || len(detail.Words) != 2 || len(detail.Modes) != 1 || detail.Modes[0] != "cloze" {
		t.Fatalf("got %+v", detail)
	}
	if detail.Student.ID != f.userID {
		t.Fatalf("got student %+v", detail.Student)
	}

	owner, err := SheetOwner(ctx, f.db, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if owner.UserID != f.userID || owner.CreatorID != f.userID {
		t.Fatalf("got %+v", owner)
	}

	if _, err := SheetDetail(ctx, f.db, "nope"); err == nil {
		t.Fatal("want NOT_FOUND")
	}
	if _, err := SheetOwner(ctx, f.db, "nope"); err == nil {
		t.Fatal("want NOT_FOUND")
	}

	// 未开测：可以删
	if err := DeleteSheet(ctx, f.db, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := SheetDetail(ctx, f.db, created.ID); err == nil {
		t.Fatal("want deleted")
	}

	// 开过测（哪怕只是进行中）：不能删
	created2, err := CreateSheet(ctx, f.db, now, f.userID, f.userID, f.wordIDs, []string{"recognition"}, 1, core.SheetPerMax)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := StartSession(ctx, f.db, shanghai, now, core.SeededRng(1), f.userID, StartInput{Kind: "sheet", SheetID: &created2.ID}); err != nil {
		t.Fatal(err)
	}
	if err := DeleteSheet(ctx, f.db, created2.ID); err == nil {
		t.Fatal("want INVALID_ACTION")
	}
}

// TestNextSheet 覆盖旧 pickNextSheet 的场景（NextSheet 本身是 A4 已实现的函数：有进行中测试的优先，
// 否则取编号最小的未测单子；remaining 数其余未测的；全部测完时为 nil）。
func TestNextSheet(t *testing.T) {
	f := newLearnFixture(t, 6)
	ctx := context.Background()
	now := sh(2026, 9, 23, 12, 0, 0)

	if next, err := NextSheet(ctx, f.db, f.userID); err != nil || next != nil {
		t.Fatalf("want nil, got %+v err=%v", next, err)
	}

	s1, err := CreateSheet(ctx, f.db, now, f.userID, f.userID, f.wordIDs[0:1], []string{"recognition"}, 1, core.SheetPerMax)
	if err != nil {
		t.Fatal(err)
	}
	s2, err := CreateSheet(ctx, f.db, now, f.userID, f.userID, f.wordIDs[1:2], []string{"recognition"}, 1, core.SheetPerMax)
	if err != nil {
		t.Fatal(err)
	}

	// 两份都待测：取编号最小的（seq 1），remaining 数其余未测
	next, err := NextSheet(ctx, f.db, f.userID)
	if err != nil {
		t.Fatal(err)
	}
	if next == nil || next.ID != s1.ID || next.Seq != 1 || next.Remaining != 1 || next.ActiveSessionID != nil {
		t.Fatalf("got %+v", next)
	}

	// s1 测完：下一份是 s2
	res1, err := StartSession(ctx, f.db, shanghai, now, core.SeededRng(1), f.userID, StartInput{Kind: "sheet", SheetID: &s1.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range snapOf(t, res1.Session).Items {
		if _, err := RecordAnswer(ctx, f.db, shanghai, now, f.userID, res1.Session.ID, AnswerInput{WordID: item.WordID, Mode: "recognition", Phase: "test", Attempt: 1, Answer: item.Definition}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := CompleteSession(ctx, f.db, shanghai, now, f.userID, res1.Session.ID); err != nil {
		t.Fatal(err)
	}
	next, err = NextSheet(ctx, f.db, f.userID)
	if err != nil {
		t.Fatal(err)
	}
	if next == nil || next.ID != s2.ID || next.Remaining != 0 {
		t.Fatalf("got %+v", next)
	}

	// s1 重测（进行中）：优先于 s2，即使 s2 编号更小的未测状态仍在
	res1b, err := StartSession(ctx, f.db, shanghai, now, core.SeededRng(1), f.userID, StartInput{Kind: "sheet", SheetID: &s1.ID})
	if err != nil {
		t.Fatal(err)
	}
	if res1b.Resumed {
		t.Fatalf("want new retake session, got resumed=true")
	}
	next, err = NextSheet(ctx, f.db, f.userID)
	if err != nil {
		t.Fatal(err)
	}
	if next == nil || next.ID != s1.ID || next.ActiveSessionID == nil || *next.ActiveSessionID != res1b.Session.ID || next.Remaining != 1 {
		t.Fatalf("got %+v", next)
	}

	// 放弃重测、把 s2 也测完：全部测完 → nil
	if err := DiscardSession(ctx, f.db, f.userID, res1b.Session.ID); err != nil {
		t.Fatal(err)
	}
	res2, err := StartSession(ctx, f.db, shanghai, now, core.SeededRng(1), f.userID, StartInput{Kind: "sheet", SheetID: &s2.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range snapOf(t, res2.Session).Items {
		if _, err := RecordAnswer(ctx, f.db, shanghai, now, f.userID, res2.Session.ID, AnswerInput{WordID: item.WordID, Mode: "recognition", Phase: "test", Attempt: 1, Answer: item.Definition}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := CompleteSession(ctx, f.db, shanghai, now, f.userID, res2.Session.ID); err != nil {
		t.Fatal(err)
	}
	if next, err := NextSheet(ctx, f.db, f.userID); err != nil || next != nil {
		t.Fatalf("want nil, got %+v err=%v", next, err)
	}
}

func TestSheetSources(t *testing.T) {
	f := newLearnFixture(t, 4)
	ctx := context.Background()
	now := sh(2026, 9, 23, 12, 0, 0)
	for _, w := range f.wordIDs {
		insertMemory(t, f.db, f.userID, w, now.Add(-time.Hour), 20, 0)
	}
	created, err := CreateSheet(ctx, f.db, now, f.userID, f.userID, f.wordIDs, []string{"recognition"}, 1, core.SheetPerMax)
	if err != nil {
		t.Fatal(err)
	}
	res, err := StartSession(ctx, f.db, shanghai, now, core.SeededRng(1), f.userID, StartInput{Kind: "sheet", SheetID: &created.ID})
	if err != nil {
		t.Fatal(err)
	}
	items := snapOf(t, res.Session).Items
	for i, item := range items {
		answer := item.Definition
		if i < 2 { // 前两个答错
			answer = "__wrong__"
		}
		if _, err := RecordAnswer(ctx, f.db, shanghai, now, f.userID, res.Session.ID, AnswerInput{WordID: item.WordID, Mode: "recognition", Phase: "test", Attempt: 1, Answer: answer}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := CompleteSession(ctx, f.db, shanghai, now, f.userID, res.Session.ID); err != nil {
		t.Fatal(err)
	}

	out, err := SheetSources(ctx, f.db, now, f.userID)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Sessions) != 1 {
		t.Fatalf("got %+v", out.Sessions)
	}
	s := out.Sessions[0]
	if s.ID != res.Session.ID || s.Kind != "sheet" || s.WrongCount != 2 || s.PlanName != "单词单 #1" {
		t.Fatalf("got %+v", s)
	}

	owner, err := SheetSourceSessionOwner(ctx, f.db, res.Session.ID)
	if err != nil || owner != f.userID {
		t.Fatalf("got %q err=%v", owner, err)
	}
	if _, err := SheetSourceSessionOwner(ctx, f.db, "nope"); err == nil {
		t.Fatal("want NOT_FOUND")
	}
}
