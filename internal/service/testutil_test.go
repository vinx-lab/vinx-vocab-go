package service

import (
	"path/filepath"
	"testing"

	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// openTempDB 临时库（跑完迁移的全新 SQLite），供本包的测试用。与 internal/seed/seed_test.go 的同名
// 助手同构（那边是包外测试，这里是包内白盒测试）：多个测试文件共用这一份，避免各自重复定义。
func openTempDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "vinx.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
