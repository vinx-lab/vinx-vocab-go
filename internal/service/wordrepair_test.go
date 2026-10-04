package service

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

func execAll(t *testing.T, db *store.DB, stmts ...string) {
	t.Helper()
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
}

func countWhere(t *testing.T, db *store.DB, table, where string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM "`+table+`" WHERE `+where, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func runRepair(t *testing.T, db *store.DB) WordRepairStats {
	t.Helper()
	var st WordRepairStats
	if err := db.Tx(context.Background(), func(tx *sql.Tx) error {
		var err error
		st, err = RepairSystemWordSpellings(context.Background(), tx, time.Now())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return st
}

// spec 0007 §3：课本词 fly 与核心词 `fly (flew, flown)` 两边都有记忆状态、作答、复习记录、单元词、例句、
// 单词单、学习组快照，并有同一学习组里两个词都答过的冲突。修复后核对每张表保留哪条，再跑一次确认幂等。
func TestRepairSystemWordSpellingsMerge(t *testing.T) {
	db := openTempDB(t)
	execAll(t, db,
		`INSERT INTO "User" ("id","email","passwordHash","name") VALUES ('u1','u1@x','h','甲'),('u2','u2@x','h','乙'),('u3','u3@x','h','丙')`,
		`INSERT INTO "Book" ("id","name","isSystem") VALUES ('bt','七年级上册',1),('bc','中考核心词汇',1),('bm','我的词书',0)`,
		`INSERT INTO "Unit" ("id","bookId","name") VALUES ('ut','bt','Unit 1'),('uc','bc','F'),('um','bm','Unit 1')`,
		// wk：课本 fly；wo：核心词汇的脏拼写；wg：没有同名词，直接改名；wb1/wb2：规整成同一个新词（先改名后合并）；
		// wm：只在老师词书里，不动；wx：另一个干净的词
		`INSERT INTO "Word" ("id","spelling","type","phonetic","definition","example") VALUES
			('wk','fly','word','flaɪ','飞',NULL),
			('wo','fly (flew, flown)','phrase','flaɪ','飞；飞行','Birds fly.'),
			('wg','gladness //ˈɡlædnəs//','phrase',NULL,'高兴',NULL),
			('wb1','begin(began,begun)','word','bɪˈɡɪn','开始',NULL),
			('wb2','begin (began, begun)','phrase','','开始，着手',NULL),
			('wm','bus (pl. buses)','phrase',NULL,'公共汽车',NULL),
			('wx','apple','word',NULL,'苹果',NULL)`,
		// 课本单元里两个都有（旧的 sortOrder 更小）；核心单元只有旧的
		`INSERT INTO "UnitWord" ("unitId","wordId","sortOrder") VALUES ('ut','wk',5),('ut','wo',2),('uc','wo',7),('uc','wg',8),('uc','wb1',9),('uc','wb2',10),('um','wm',0),('ut','wx',6)`,
		// 记忆状态：u1 两条都有（旧的 lastReview 更晚，保留旧的）；u2 两条 lastReview 相同（保留 reps 多的，即保留词的）；u3 只有旧的
		`INSERT INTO "MemoryState" ("id","userId","wordId","due","stability","difficulty","reps","lastReview","introducedDay") VALUES
			('m1k','u1','wk','2026-10-01',1,5,1,'2026-09-01T00:00:00.000Z','2026-09-01'),
			('m1o','u1','wo','2026-10-01',1,5,3,'2026-09-05T00:00:00.000Z','2026-09-01'),
			('m2k','u2','wk','2026-10-01',1,5,4,'2026-09-05T00:00:00.000Z','2026-09-01'),
			('m2o','u2','wo','2026-10-01',1,5,2,'2026-09-05T00:00:00.000Z','2026-09-01'),
			('m3o','u3','wo','2026-10-01',1,5,1,NULL,'2026-09-01')`,
		`INSERT INTO "StudySession" ("id","userId","kind","dayKey","snapshot","progress","result") VALUES
			('s1','u1','test','2026-09-05','{"words":[{"id":"wo"},{"id":"wk"}]}','{"wo":1}','{"wrong":["wo"]}'),
			('s2','u3','learn','2026-09-05','{"words":[{"id":"wo"}]}',NULL,NULL)`,
		// 作答：s1 里两个词同一 mode/phase/attempt（冲突，旧的先写入，保留旧的）；另一组不冲突
		`INSERT INTO "Answer" ("id","sessionId","userId","wordId","mode","phase","attempt","correct","dayKey","createdAt") VALUES
			('a1o','s1','u1','wo','spelling','test',1,1,'2026-09-05','2026-09-05T01:00:00.000Z'),
			('a1k','s1','u1','wk','spelling','test',1,0,'2026-09-05','2026-09-05T02:00:00.000Z'),
			('a2o','s1','u1','wo','recognition','test',1,1,'2026-09-05','2026-09-05T03:00:00.000Z'),
			('a3k','s1','u1','wk','recognition','test',2,1,'2026-09-05','2026-09-05T00:30:00.000Z'),
			('a4o','s2','u3','wo','recognition','learn',1,1,'2026-09-05','2026-09-05T04:00:00.000Z')`,
		// 复习记录：s1 两条（冲突，保留词的先写入，保留它）；sessionId 为空的两条不冲突
		`INSERT INTO "ReviewLog" ("id","userId","wordId","sessionId","rating","stateBefore","stabilityAfter","difficultyAfter","dueAfter","reviewedAt","dayKey") VALUES
			('r1k','u1','wk','s1',3,0,1,5,'2026-09-06','2026-09-05T01:00:00.000Z','2026-09-05'),
			('r1o','u1','wo','s1',3,0,1,5,'2026-09-06','2026-09-05T02:00:00.000Z','2026-09-05'),
			('r2k','u1','wk',NULL,3,0,1,5,'2026-09-06','2026-09-04T01:00:00.000Z','2026-09-04'),
			('r2o','u1','wo',NULL,3,0,1,5,'2026-09-06','2026-09-04T02:00:00.000Z','2026-09-04')`,
		`INSERT INTO "Sentence" ("id","en","cn","source","wordId") VALUES ('se1','Birds fly.','鸟会飞。','example','wo'),('se2','I can fly a kite.','我会放风筝。','ai','wk')`,
		// 句中词：se2 同一位置两个词都关联了（冲突，删旧的）；se1 只有旧的
		`INSERT INTO "SentenceWord" ("sentenceId","wordId","position","form") VALUES ('se1','wo',1,'fly'),('se2','wo',3,'fly'),('se2','wk',3,'fly')`,
		`INSERT INTO "WordSheet" ("id","userId","creatorId","seq","wordIds","items") VALUES
			('ws1','u1','u1',1,'["wo","wx","wk"]','[{"type":"word","wordId":"wo"},{"type":"word","wordId":"wk"}]'),
			('ws2','u1','u1',2,'["wx"]','[]')`,
		`INSERT INTO "Passage" ("id","userId","title","body","wordIds") VALUES ('p1','u1','T','Birds fly.','["wk","wo"]')`,
	)

	st := runRepair(t, db)
	want := WordRepairStats{Renamed: 2, Merged: 2, UnitWordDropped: 2, MemoryStateDropped: 2, AnswerDropped: 1, ReviewLogDropped: 1, SentenceWordDropped: 1}
	if st != want {
		t.Fatalf("stats = %+v, want %+v", st, want)
	}

	// Word：旧词删除；保留词不变；改名的补音标、接注释、类型改为 word；老师词书里的不动
	if n := countWhere(t, db, "Word", `"id" IN ('wo','wb2')`); n != 0 {
		t.Errorf("旧词还剩 %d 个", n)
	}
	type wrow struct{ spelling, typ, phonetic, definition string }
	words := map[string]wrow{
		"wk":  {"fly", "word", "flaɪ", "飞"},
		"wg":  {"gladness", "word", "ˈɡlædnəs", "高兴"},
		"wb1": {"begin", "word", "bɪˈɡɪn", "开始（began,begun）"},
		"wm":  {"bus (pl. buses)", "phrase", "", "公共汽车"},
	}
	for id, w := range words {
		var got wrow
		if err := db.QueryRow(`SELECT "spelling","type",coalesce("phonetic",''),"definition" FROM "Word" WHERE "id" = ?`, id).Scan(&got.spelling, &got.typ, &got.phonetic, &got.definition); err != nil {
			t.Fatal(id, err)
		}
		if got != w {
			t.Errorf("%s = %+v, want %+v", id, got, w)
		}
	}

	// UnitWord：课本单元留一条，sortOrder 取小的；核心单元改指向保留词
	var so int
	db.QueryRow(`SELECT "sortOrder" FROM "UnitWord" WHERE "unitId"='ut' AND "wordId"='wk'`).Scan(&so)
	if n := countWhere(t, db, "UnitWord", `"unitId"='ut' AND "wordId"='wk'`); n != 1 || so != 2 {
		t.Errorf("课本单元 fly：%d 条，sortOrder %d", n, so)
	}
	if n := countWhere(t, db, "UnitWord", `"unitId"='uc' AND "wordId" IN ('wk','wg','wb1')`); n != 3 {
		t.Errorf("核心单元关联 %d 条", n)
	}
	if n := countWhere(t, db, "UnitWord", `1`); n != 6 { // 课本单元 fly 与核心单元 begin 各去掉一条
		t.Errorf("UnitWord %d 条", n)
	}

	// MemoryState：u1 保留旧的那条（改指向）、u2 保留保留词的、u3 改指向
	for id, wantWord := range map[string]string{"m1o": "wk", "m2k": "wk", "m3o": "wk"} {
		var w string
		if err := db.QueryRow(`SELECT "wordId" FROM "MemoryState" WHERE "id" = ?`, id).Scan(&w); err != nil || w != wantWord {
			t.Errorf("MemoryState %s wordId = %q err %v", id, w, err)
		}
	}
	if n := countWhere(t, db, "MemoryState", `"id" IN ('m1k','m2o')`); n != 0 {
		t.Errorf("该删的记忆状态还在 %d 条", n)
	}

	// Answer：冲突保留先写入的 a1o；其余改指向
	if n := countWhere(t, db, "Answer", `"id"='a1k'`); n != 0 {
		t.Error("a1k 应删除")
	}
	if n := countWhere(t, db, "Answer", `"wordId"='wk' AND "id" IN ('a1o','a2o','a3k','a4o')`); n != 4 {
		t.Errorf("Answer 改指向 %d 条", n)
	}

	// ReviewLog：冲突保留先写入的 r1k；sessionId 为空的两条都保留
	if n := countWhere(t, db, "ReviewLog", `"id"='r1o'`); n != 0 {
		t.Error("r1o 应删除")
	}
	if n := countWhere(t, db, "ReviewLog", `"wordId"='wk'`); n != 3 {
		t.Errorf("ReviewLog 保留 %d 条", n)
	}

	// Sentence / SentenceWord
	if n := countWhere(t, db, "Sentence", `"id"='se1' AND "wordId"='wk'`); n != 1 {
		t.Error("例句未改指向")
	}
	if n := countWhere(t, db, "SentenceWord", `1`); n != 2 {
		t.Errorf("SentenceWord %d 条", n)
	}
	if n := countWhere(t, db, "SentenceWord", `"wordId"='wo'`); n != 0 {
		t.Error("SentenceWord 还指向旧词")
	}

	// JSON 列
	str := func(q string) string {
		var s string
		if err := db.QueryRow(q).Scan(&s); err != nil {
			t.Fatal(q, err)
		}
		return s
	}
	checks := map[string]string{
		`SELECT "wordIds" FROM "WordSheet" WHERE "id"='ws1'`:                     `["wk","wx"]`,
		`SELECT "items" FROM "WordSheet" WHERE "id"='ws1'`:                       `[{"type":"word","wordId":"wk"},{"type":"word","wordId":"wk"}]`,
		`SELECT "wordIds" FROM "WordSheet" WHERE "id"='ws2'`:                     `["wx"]`,
		`SELECT "wordIds" FROM "Passage" WHERE "id"='p1'`:                        `["wk"]`,
		`SELECT "snapshot" FROM "StudySession" WHERE "id"='s1'`:                  `{"words":[{"id":"wk"},{"id":"wk"}]}`,
		`SELECT "progress" FROM "StudySession" WHERE "id"='s1'`:                  `{"wk":1}`,
		`SELECT "result" FROM "StudySession" WHERE "id"='s1'`:                    `{"wrong":["wk"]}`,
		`SELECT "snapshot" FROM "StudySession" WHERE "id"='s2'`:                  `{"words":[{"id":"wk"}]}`,
		`SELECT coalesce("progress",'null') FROM "StudySession" WHERE "id"='s2'`: `null`,
	}
	for q, w := range checks {
		if got := str(q); got != w {
			t.Errorf("%s = %s, want %s", q, got, w)
		}
	}
	var fk int
	db.QueryRow(`SELECT count(*) FROM pragma_foreign_key_check`).Scan(&fk)
	if fk != 0 {
		t.Errorf("外键检查 %d 处不一致", fk)
	}

	// 幂等：第二次什么都不做
	if st2 := runRepair(t, db); st2 != (WordRepairStats{}) {
		t.Errorf("第二次 = %+v", st2)
	}
}

// 打开数据库时的钩子：修一次之后再打开不再开写事务；大小写不同不合并。
func TestRepairWordSpellingsOnOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vinx.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	execAll(t, db,
		`INSERT INTO "Book" ("id","name","isSystem") VALUES ('bc','中考核心词汇',1)`,
		`INSERT INTO "Unit" ("id","bookId","name") VALUES ('uc','bc','M')`,
		`INSERT INTO "Word" ("id","spelling","definition") VALUES ('w1','mom','妈妈'),('w2','Mom =Mum','妈妈'),('w3','metre (美meter)','米')`,
		`INSERT INTO "UnitWord" ("unitId","wordId","sortOrder") VALUES ('uc','w1',0),('uc','w2',1),('uc','w3',2)`,
	)
	db.Close()
	if db, err = store.Open(path); err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	got := map[string]string{}
	rows, err := db.Query(`SELECT "id","spelling" || '|' || "definition" FROM "Word"`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id, s string
		rows.Scan(&id, &s)
		got[id] = s
	}
	rows.Close()
	want := map[string]string{"w1": "mom|妈妈", "w2": "Mom|妈妈（= Mum）", "w3": "metre|米（美meter）"}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("%s = %q, want %q", id, got[id], w)
		}
	}
	todo, err := systemWordsToClean(context.Background(), db)
	if err != nil || len(todo) != 0 {
		t.Errorf("修复后仍有 %d 个待修（%v）", len(todo), err)
	}
}
