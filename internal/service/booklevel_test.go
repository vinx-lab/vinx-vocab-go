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

func strp(s string) *string { return &s }
