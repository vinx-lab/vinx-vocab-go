package seed

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vinx-lab/vinx-vocab-go/internal/auth"
	"github.com/vinx-lab/vinx-vocab-go/internal/core/vocabparser"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

func openTemp(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "vinx.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// dump 与 testdata/oracle-seed.tsv 同格式：词书 单元 单元序 词序 拼写 类型 音标 词性 释义
func dump(t *testing.T, db *store.DB) []string {
	t.Helper()
	rows, err := db.Query(`SELECT b."name", u."name", u."sortOrder", uw."sortOrder", w."spelling", w."type", coalesce(w."phonetic",'<null>'), coalesce(w."partOfSpeech",'<null>'), w."definition"
		FROM "UnitWord" uw JOIN "Unit" u ON u."id"=uw."unitId" JOIN "Book" b ON b."id"=u."bookId" JOIN "Word" w ON w."id"=uw."wordId"
		ORDER BY b."sortOrder", u."sortOrder", uw."sortOrder"`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var f [9]string
		if err := rows.Scan(&f[0], &f[1], &f[2], &f[3], &f[4], &f[5], &f[6], &f[7], &f[8]); err != nil {
			t.Fatal(err)
		}
		out = append(out, strings.Join(f[:], "\t"))
	}
	return out
}

// coreBook 中考核心词汇：spec 0007 起拼写末尾的注释由解析器规整，不再与旧 seed 一致。
const coreBook = "中考核心词汇"

func readLines(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		out = append(out, sc.Text())
	}
	return out
}

// compareLines 逐行比较，最多报 10 处。
func compareLines(t *testing.T, name string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s：行数 %d，期望 %d", name, len(got), len(want))
	}
	diffs := 0
	for i := range want {
		if got[i] != want[i] {
			if diffs < 10 {
				t.Errorf("%s 第 %d 行\n go:   %s\n want: %s", name, i+1, got[i], want[i])
			}
			diffs++
		}
	}
	if diffs > 0 {
		t.Fatalf("%s：共 %d 行不一致", name, diffs)
	}
}

// 五本课本与旧 seed（Node + PostgreSQL）导入结果逐行一致：testdata/oracle-seed.tsv 由旧仓库 seed 后导出。
// 中考核心词汇与 testdata/core-seed.tsv 一致：spec 0007 之后单文件版自己的快照（拼写注释规整、
// 源文件个别行修正），不再是旧 seed 的结果。改了解析器或源词表后用 UPDATE_CORE_SEED=1 重新生成并审阅差异。
func TestSeedMatchesOracle(t *testing.T) {
	db := openTemp(t)
	ctx := context.Background()
	if err := Demo(ctx, db, time.Now()); err != nil {
		t.Fatal(err)
	}
	isCore := func(line string) bool { return strings.HasPrefix(line, coreBook+"\t") }
	var wantText, gotText, gotCore []string
	for _, l := range readLines(t, "testdata/oracle-seed.tsv") {
		if !isCore(l) {
			wantText = append(wantText, l)
		}
	}
	got := dump(t, db)
	for _, l := range got {
		if isCore(l) {
			gotCore = append(gotCore, l)
		} else {
			gotText = append(gotText, l)
		}
	}
	compareLines(t, "课本", gotText, wantText)
	if os.Getenv("UPDATE_CORE_SEED") != "" {
		if err := os.WriteFile("testdata/core-seed.tsv", []byte(strings.Join(gotCore, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	compareLines(t, coreBook, gotCore, readLines(t, "testdata/core-seed.tsv"))
	want := append(wantText, gotCore...)

	counts := map[string]int{}
	for _, q := range []string{"User", "Book", "Unit", "Word", "UnitWord", "Classroom", "ClassMember", "Plan", "PlanUnit", "PlanTarget"} {
		var n int
		db.QueryRow(fmt.Sprintf(`SELECT count(*) FROM "%s"`, q)).Scan(&n)
		counts[q] = n
	}
	want2 := map[string]int{"User": 4, "Book": 6, "Unit": 66, "Word": 3730, "UnitWord": 4245, "Classroom": 1, "ClassMember": 2, "Plan": 1, "PlanUnit": 2, "PlanTarget": 1}
	for k, v := range want2 {
		if counts[k] != v {
			t.Errorf("%s = %d, want %d", k, counts[k], v)
		}
	}

	// spec 0007：系统词书里的拼写都已规整（短语里有意写的括号除外）；课本词与核心词汇共用 Word
	rows, err := db.Query(`SELECT DISTINCT w."spelling" FROM "Word" w JOIN "UnitWord" uw ON uw."wordId" = w."id"
		JOIN "Unit" u ON u."id" = uw."unitId" JOIN "Book" b ON b."id" = u."bookId" WHERE b."isSystem" = 1`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var sp string
		rows.Scan(&sp)
		if c, _, _ := vocabparser.CleanSpelling(sp); c != sp {
			t.Errorf("拼写未规整：%q", sp)
		}
	}
	rows.Close()
	var coreDirty int
	db.QueryRow(`SELECT count(*) FROM "Word" w JOIN "UnitWord" uw ON uw."wordId" = w."id" JOIN "Unit" u ON u."id" = uw."unitId"
		JOIN "Book" b ON b."id" = u."bookId" WHERE b."name" = ? AND (w."spelling" GLOB '*[[(=/]*' OR w."spelling" LIKE '%（%')`, coreBook).Scan(&coreDirty)
	if coreDirty != 0 {
		t.Errorf("核心词汇里还有 %d 个带括号、斜杠、等号的拼写", coreDirty)
	}
	for _, sp := range []string{"awake", "fridge", "throw", "wolf"} {
		var books int
		db.QueryRow(`SELECT count(DISTINCT b."name") FROM "Word" w JOIN "UnitWord" uw ON uw."wordId" = w."id" JOIN "Unit" u ON u."id" = uw."unitId"
			JOIN "Book" b ON b."id" = u."bookId" WHERE w."spelling" = ?`, sp).Scan(&books)
		if books < 2 {
			t.Errorf("%q 只出现在 %d 本书里，课本与核心词汇应共用同一个 Word", sp, books)
		}
	}

	// 演示账号密码、系统词书所有者
	var hash, owner string
	db.QueryRow(`SELECT "passwordHash" FROM "User" WHERE "email"='teacher@vinx.test'`).Scan(&hash)
	if !auth.VerifyPassword(DemoPassword, hash) {
		t.Error("演示密码不对")
	}
	db.QueryRow(`SELECT u."email" FROM "Book" b JOIN "User" u ON u."id"=b."ownerId" WHERE b."name"='八年级上册'`).Scan(&owner)
	if owner != "admin@vinx.test" {
		t.Errorf("系统词书所有者 = %q", owner)
	}

	// 可重跑：不重复创建
	if err := Demo(ctx, db, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got2 := dump(t, db); len(got2) != len(want) {
		t.Fatalf("重跑后 %d 行", len(got2))
	}
	var n int
	db.QueryRow(`SELECT count(*) FROM "Plan"`).Scan(&n)
	if n != 1 {
		t.Fatalf("重跑后计划 %d 个", n)
	}
}

func TestFirstRunBooksOnlyWhenEmpty(t *testing.T) {
	db := openTemp(t)
	ctx := context.Background()
	if err := FirstRunBooks(ctx, db, time.Now()); err != nil {
		t.Fatal(err)
	}
	var books, users int
	var owner sql.NullString
	db.QueryRow(`SELECT count(*) FROM "Book"`).Scan(&books)
	db.QueryRow(`SELECT count(*) FROM "User"`).Scan(&users)
	db.QueryRow(`SELECT "ownerId" FROM "Book" LIMIT 1`).Scan(&owner)
	if books != 6 || users != 0 || owner.Valid {
		t.Fatalf("books=%d users=%d owner=%v", books, users, owner)
	}
	// 删掉一本后再启动不再导入（只在库里没有任何词书时导入）
	db.Exec(`DELETE FROM "Book" WHERE "name"='七年级上册'`)
	if err := FirstRunBooks(ctx, db, time.Now()); err != nil {
		t.Fatal(err)
	}
	db.QueryRow(`SELECT count(*) FROM "Book"`).Scan(&books)
	if books != 5 {
		t.Fatalf("books=%d", books)
	}
}

// M3：先 serve（无主系统词书）再 seed-demo，系统词书归到演示管理员名下，与旧 seed 一致。
func TestDemoAfterFirstRunSetsOwner(t *testing.T) {
	db := openTemp(t)
	ctx := context.Background()
	if err := FirstRunBooks(ctx, db, time.Now()); err != nil {
		t.Fatal(err)
	}
	// 管理员自建的系统词书（非内置）不受影响
	db.Exec(`INSERT INTO "Book" ("id","name","isSystem") VALUES ('own','自建',1)`)
	if err := Demo(ctx, db, time.Now()); err != nil {
		t.Fatal(err)
	}
	var withOwner, total int
	db.QueryRow(`SELECT count(*) FROM "Book" b JOIN "User" u ON u."id"=b."ownerId" WHERE u."email"='admin@vinx.test'`).Scan(&withOwner)
	db.QueryRow(`SELECT count(*) FROM "Book"`).Scan(&total)
	if withOwner != 6 || total != 7 {
		t.Fatalf("withOwner=%d total=%d", withOwner, total)
	}
}
