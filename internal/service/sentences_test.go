package service

import (
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

func mustExec(t *testing.T, db *store.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func seedSentenceFixture(t *testing.T, db *store.DB) {
	t.Helper()
	mustExec(t, db, `INSERT INTO "User" ("id","email","passwordHash","name","role") VALUES ('u1','u1@x','h','u1','student')`)
	mustExec(t, db, `INSERT INTO "Word" ("id","spelling","definition","example","exampleCn") VALUES
		('w-go','go (went, gone)','去','I went to school yesterday.','我昨天去上学了。'),
		('w-school','school','学校',NULL,NULL),
		('w-i','I','我','  ',NULL),
		('w-be','be','是',NULL,NULL),
		('w-good','good','好的',NULL,NULL),
		('w-begood','be good at','擅长',NULL,NULL),
		('w-swim','swim','游泳','She is good at swimming.',NULL)`)
	// 句数一致的短文 / 句数不一致的短文
	mustExec(t, db, `INSERT INTO "Passage" ("id","userId","title","body","bodyCn","model","createdAt") VALUES
		('p-ok','u1','T','I went to school. I am good at it!','我去上学了。我很擅长！','m1','2026-09-01T00:00:00.000Z'),
		('p-bad','u1','T','One. Two.','一二。',NULL,'2026-09-01T00:00:00.000Z')`)
}

type sentenceRow struct {
	ID, En, Cn, Source string
	WordID             *string
}

func sentencesOf(t *testing.T, db *store.DB, where string, args ...any) []sentenceRow {
	t.Helper()
	rows, err := db.Query(`SELECT "id","en","cn","source","wordId" FROM "Sentence" WHERE `+where+` ORDER BY "en"`, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []sentenceRow
	for rows.Next() {
		var r sentenceRow
		if err := rows.Scan(&r.ID, &r.En, &r.Cn, &r.Source, &r.WordID); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func linkedWords(t *testing.T, db *store.DB, sentenceID string) []string {
	t.Helper()
	rows, err := db.Query(`SELECT "wordId" || '@' || "position" || ':' || "form" FROM "SentenceWord" WHERE "sentenceId" = ? ORDER BY "position"`, sentenceID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		rows.Scan(&s)
		out = append(out, s)
	}
	return out
}

func TestBackfillSentences(t *testing.T) {
	db := openTempDB(t)
	ctx := context.Background()
	seedSentenceFixture(t, db)
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

	run := func() {
		if err := db.Tx(ctx, func(tx *sql.Tx) error { return BackfillSentences(ctx, tx, now) }); err != nil {
			t.Fatal(err)
		}
	}
	run()

	// 例句 → Sentence（source = example），空白例句跳过
	ex := sentencesOf(t, db, `"source" = 'example'`)
	if len(ex) != 2 || ex[0].En != "I went to school yesterday." || *ex[0].WordID != "w-go" || ex[1].Cn != "" || *ex[1].WordID != "w-swim" {
		t.Fatalf("例句 = %+v", ex)
	}
	// 词形还原关联：went → go（带注释的拼写）、短语 be good at（is 变形）
	if got := linkedWords(t, db, ex[0].ID); !slices.Equal(got, []string{"w-i@0:I", "w-go@1:went", "w-school@3:school"}) {
		t.Errorf("例句关联 = %v", got)
	}
	if got := linkedWords(t, db, ex[1].ID); !slices.Equal(got, []string{"w-begood@1:is good at", "w-swim@4:swimming"}) {
		t.Errorf("短语关联 = %v", got)
	}

	// AI 短文：句数一致的逐句拆分，不一致的保持整段
	ai := sentencesOf(t, db, `"source" = 'ai'`)
	if len(ai) != 2 || ai[0].En != "I am good at it!" || ai[0].Cn != "我很擅长！" || ai[1].En != "I went to school." {
		t.Fatalf("短文句子 = %+v", ai)
	}
	var n int
	db.QueryRow(`SELECT count(*) FROM "PassageSentence" WHERE "passageId" = 'p-bad'`).Scan(&n)
	if n != 0 {
		t.Errorf("句数不一致的短文不应拆分")
	}
	var order []string
	rows, _ := db.Query(`SELECT s."en" FROM "PassageSentence" ps JOIN "Sentence" s ON s."id" = ps."sentenceId" WHERE ps."passageId" = 'p-ok' ORDER BY ps."sortOrder"`)
	for rows.Next() {
		var s string
		rows.Scan(&s)
		order = append(order, s)
	}
	rows.Close()
	if !slices.Equal(order, []string{"I went to school.", "I am good at it!"}) {
		t.Errorf("短文句序 = %v", order)
	}

	// 幂等：再跑一次不重复生成
	run()
	var total int
	db.QueryRow(`SELECT count(*) FROM "Sentence"`).Scan(&total)
	if total != 4 {
		t.Errorf("重复回填后句子数 = %d", total)
	}
}

// 迁移 0003_sentences 执行时自动回填（迁移钩子）。
func TestSentencesMigrationHook(t *testing.T) {
	db := openTempDB(t)
	ctx := context.Background()
	seedSentenceFixture(t, db)
	// 模拟升级前的库：去掉 0003 建的表和迁移记录，再跑一次迁移
	for _, q := range []string{
		`DROP TABLE "SentenceWord"`, `DROP TABLE "UnitTextSentence"`, `DROP TABLE "PassageSentence"`, `DROP TABLE "UnitText"`, `DROP TABLE "Sentence"`,
		`DELETE FROM schema_migrations WHERE version = '0003_sentences'`,
	} {
		mustExec(t, db, q)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if ex := sentencesOf(t, db, `"source" = 'example'`); len(ex) != 2 {
		t.Errorf("迁移后例句 = %+v", ex)
	}
	if ai := sentencesOf(t, db, `"source" = 'ai'`); len(ai) != 2 {
		t.Errorf("迁移后短文句子 = %+v", ai)
	}
}

// 整库替换不经过业务层（如从旧版导入，只写旧版的表）后，下次打开数据库时补齐句子并清掉孤儿句子。
func TestRepairSentencesOnOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vinx.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	// 原有内容：一篇单元课文（import 句子）+ 一个保留引用的手工句子
	mustExec(t, db, `INSERT INTO "Book" ("id","name") VALUES ('b-old','旧书')`)
	mustExec(t, db, `INSERT INTO "Unit" ("id","bookId","name") VALUES ('u-old','b-old','U1')`)
	mustExec(t, db, `INSERT INTO "UnitText" ("id","unitId","title") VALUES ('t-old','u-old','课文')`)
	mustExec(t, db, `INSERT INTO "Sentence" ("id","en","cn","source") VALUES ('s-old','Old one.','旧的。','import'),('s-keep','Keep me.','留下。','manual')`)
	mustExec(t, db, `INSERT INTO "UnitTextSentence" ("textId","sentenceId") VALUES ('t-old','s-old')`)
	mustExec(t, db, `INSERT INTO "Book" ("id","name") VALUES ('b-keep','保留')`)
	mustExec(t, db, `INSERT INTO "Unit" ("id","bookId","name") VALUES ('u-keep','b-keep','U1')`)
	mustExec(t, db, `INSERT INTO "UnitText" ("id","unitId","title") VALUES ('t-keep','u-keep','课文')`)
	mustExec(t, db, `INSERT INTO "UnitTextSentence" ("textId","sentenceId") VALUES ('t-keep','s-keep')`)
	// 模拟整库覆盖：删掉旧书（级联删篇与关联，句子成为孤儿），直接写入带例句的词和短文
	mustExec(t, db, `DELETE FROM "Book" WHERE "id" = 'b-old'`)
	seedSentenceFixture(t, db)
	if n := len(sentencesOf(t, db, `1`)); n != 2 {
		t.Fatalf("覆盖后、重新打开前句子数 = %d", n)
	}
	db.Close()

	db2, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	if ex := sentencesOf(t, db2, `"source" = 'example'`); len(ex) != 2 {
		t.Errorf("重新打开后例句 = %+v", ex)
	}
	if ai := sentencesOf(t, db2, `"source" = 'ai'`); len(ai) != 2 {
		t.Errorf("重新打开后短文句子 = %+v", ai)
	}
	if got := sentencesOf(t, db2, `"source" IN ('import','manual')`); len(got) != 1 || got[0].ID != "s-keep" {
		t.Errorf("孤儿句子应删除、有引用的保留：%+v", got)
	}
	// 幂等：再打开一次不重复生成
	db2.Close()
	db3, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db3.Close()
	if n := len(sentencesOf(t, db3, `1`)); n != 5 {
		t.Errorf("再次打开后句子数 = %d", n)
	}
}

// 编辑例句时 Word.example 与例句句子双写。
func TestUpdateWordSyncsExampleSentence(t *testing.T) {
	db := openTempDB(t)
	ctx := context.Background()
	seedSentenceFixture(t, db)
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	set := func(v sql.NullString) {
		if _, err := UpdateWord(ctx, db, now, "w-school", WordPatch{Example: &v, ExampleCn: &sql.NullString{String: "中文", Valid: v.Valid}}); err != nil {
			t.Fatal(err)
		}
	}
	set(sql.NullString{String: "I go to school.", Valid: true})
	ex := sentencesOf(t, db, `"wordId" = 'w-school'`)
	if len(ex) != 1 || ex[0].En != "I go to school." || ex[0].Cn != "中文" {
		t.Fatalf("新增例句 = %+v", ex)
	}
	set(sql.NullString{String: "School is fun.", Valid: true})
	ex2 := sentencesOf(t, db, `"wordId" = 'w-school'`)
	if len(ex2) != 1 || ex2[0].ID != ex[0].ID || ex2[0].En != "School is fun." {
		t.Fatalf("修改例句 = %+v", ex2)
	}
	if got := linkedWords(t, db, ex2[0].ID); !slices.Equal(got, []string{"w-school@0:School", "w-be@1:is"}) {
		t.Errorf("修改后重新关联 = %v", got)
	}
	set(sql.NullString{})
	if ex3 := sentencesOf(t, db, `"wordId" = 'w-school'`); len(ex3) != 0 {
		t.Errorf("清空例句后应删除句子：%+v", ex3)
	}
}

// 内置不规则表能加载，常见变形都在。
func TestBuiltinIrregular(t *testing.T) {
	irr := builtinIrregular()
	for form, base := range map[string]string{"went": "go", "children": "child", "better": "good", "is": "be", "thought": "think"} {
		if !slices.Contains(irr[form], base) {
			t.Errorf("%s → %v，缺少 %s", form, irr[form], base)
		}
	}
}
