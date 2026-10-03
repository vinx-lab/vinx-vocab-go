package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// 迁移 0004_book_level：加列并按书名回填已有的系统词书（非系统词书、映射外的书名不动）。
func TestMigrationBookLevelBackfill(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vinx.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	// 退回到 0004 之前：去掉列和迁移记录，再放入几本词书
	for _, s := range []string{
		`ALTER TABLE "Book" DROP COLUMN "level"`,
		`DELETE FROM schema_migrations WHERE version = '0004_book_level'`,
		`INSERT INTO "Book" ("id","name","isSystem") VALUES ('b1','七年级上册',1),('b2','九年级上册',1),('b3','中考核心词汇',1),('b4','中考差集',1),('b5','七年级上册',0),('b6','我的词书',1)`,
	} {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	db.Close()
	db, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	want := map[string]*string{"b1": strp("junior"), "b2": strp("junior"), "b3": strp("exam"), "b4": strp("exam"), "b5": nil, "b6": nil}
	for id, w := range want {
		var got *string
		if err := db.QueryRowContext(context.Background(), `SELECT "level" FROM "Book" WHERE "id" = ?`, id).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if (got == nil) != (w == nil) || (got != nil && *got != *w) {
			t.Errorf("%s level = %v want %v", id, got, w)
		}
	}
}

// 从服务器版导入后（迁移之后整表替换 Book，level 全为 NULL）：下次打开时按书名回填；
// 已有书设了学段时不再回填（手动清空的学段不被改回）。
func TestRepairBookLevelsOnOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vinx.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	// 模拟导入：迁移已跑完，再写入没有学段的词书
	if _, err := db.Exec(`INSERT INTO "Book" ("id","name","isSystem") VALUES ('b1','七年级上册',1),('b3','中考核心词汇',1),('b4','中考差集',1),('b5','七年级上册',0),('b6','我的词书',1)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	check := func(want map[string]*string) {
		t.Helper()
		for id, w := range want {
			var got *string
			if err := db.QueryRowContext(context.Background(), `SELECT "level" FROM "Book" WHERE "id" = ?`, id).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if (got == nil) != (w == nil) || (got != nil && *got != *w) {
				t.Errorf("%s level = %v want %v", id, got, w)
			}
		}
	}
	check(map[string]*string{"b1": strp("junior"), "b3": strp("exam"), "b4": strp("exam"), "b5": nil, "b6": nil})

	// 管理员清空了一本的学段：再打开不改回
	if _, err := db.Exec(`UPDATE "Book" SET "level" = NULL WHERE "id" = 'b1'`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	check(map[string]*string{"b1": nil, "b3": strp("exam"), "b4": strp("exam")})
}

func strp(s string) *string { return &s }
