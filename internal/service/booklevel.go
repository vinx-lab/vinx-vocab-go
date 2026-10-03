package service

import (
	"context"
	"database/sql"
	"time"

	coreai "github.com/vinx-lab/vinx-vocab-go/internal/core/ai"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

func init() {
	// 从服务器版导入（vinx-vocab import）在迁移之后整表替换 Book，服务器版没有 level 列，
	// 导入后所有词书的学段都是 NULL；SeedBooks 又因同名系统词书已存在而跳过。
	// 导入后的第一次打开在这里按书名回填一次（与迁移 0004 的回填规则一致）。
	store.RegisterOpenHook(RepairBookLevels)
}

// bookLevelsNeedRepair 没有任何词书设了学段、但有系统词书——即刚从服务器版导入的库。
// 只要有一本书设过学段就不再回填，管理员手动清空的学段不会被改回来。
const bookLevelsNeedRepair = `SELECT
	NOT EXISTS (SELECT 1 FROM "Book" WHERE "level" IS NOT NULL)
	AND EXISTS (SELECT 1 FROM "Book" WHERE "isSystem" = 1)`

// RepairBookLevels 打开数据库时按书名给系统词书回填学段（幂等；没有要做的事时不开写事务）。
// 不刷新 updatedAt（与迁移里的回填一致）。
func RepairBookLevels(ctx context.Context, d *store.DB, _ time.Time) error {
	var need bool
	if err := d.QueryRowContext(ctx, bookLevelsNeedRepair).Scan(&need); err != nil {
		return err
	}
	if !need {
		return nil
	}
	return d.Tx(ctx, func(tx *sql.Tx) error {
		return BackfillBookLevels(ctx, tx)
	})
}

// BackfillBookLevels 学段为空的系统词书按书名（coreai.LevelForBookName）回填；名单外的书名不动。
func BackfillBookLevels(ctx context.Context, q store.Querier) error {
	rows, err := q.QueryContext(ctx, `SELECT "id","name" FROM "Book" WHERE "isSystem" = 1 AND "level" IS NULL`)
	if err != nil {
		return err
	}
	type book struct{ id, level string }
	var todo []book
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			rows.Close()
			return err
		}
		if lv := coreai.LevelForBookName(name); lv != nil {
			todo = append(todo, book{id, *lv})
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, b := range todo {
		if _, err := q.ExecContext(ctx, `UPDATE "Book" SET "level" = ? WHERE "id" = ?`, b.level, b.id); err != nil {
			return err
		}
	}
	return nil
}
