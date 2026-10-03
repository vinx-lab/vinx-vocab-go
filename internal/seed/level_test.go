package seed

import (
	"context"
	"testing"
	"time"

	coreai "github.com/vinx-lab/vinx-vocab-go/internal/core/ai"
)

// spec 0005：内置词书插入时按书名写入学段；书名映射与 core/ai 的 LevelForBookName 一致。
func TestSeedBooksWriteLevel(t *testing.T) {
	for _, b := range Books {
		lv := coreai.LevelForBookName(b.Name)
		if lv == nil {
			t.Errorf("内置词书「%s」在书名映射里没有学段", b.Name)
		}
	}
	db := openTemp(t)
	ctx := context.Background()
	if err := FirstRunBooks(ctx, db, time.Now()); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(`SELECT "name", "level" FROM "Book" ORDER BY "sortOrder"`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var name string
		var level *string
		if err := rows.Scan(&name, &level); err != nil {
			t.Fatal(err)
		}
		if level == nil {
			t.Errorf("%s level = NULL", name)
			continue
		}
		got[name] = *level
	}
	want := map[string]string{"七年级上册": "junior", "七年级下册": "junior", "八年级上册": "junior", "八年级下册": "junior", "九年级上册": "junior", "中考核心词汇": "exam"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q want %q", k, got[k], v)
		}
	}
}
