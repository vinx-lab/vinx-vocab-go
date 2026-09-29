// Package store SQLite 存取：连接参数、嵌入式迁移链、事务、id 与时间 / JSON 编解码。
//
// 约定（详见 docs/architecture.md）：
//   - 表名、列名与旧 Prisma 一致，SQL 里一律加双引号（"User"、"passwordHash"）。
//   - 时间列存 UTC 毫秒 RFC3339 文本（FormatTime），读出用 Time / NullTime 扫描。
//   - Json 与 String[] 列存 JSON 文本，用 JSON[T] 扫描与写入。
//   - 写事务统一走 DB.Tx（BEGIN IMMEDIATE，替代 PostgreSQL 的 FOR UPDATE）。
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	sqlite "modernc.org/sqlite"
)

// Querier *sql.DB、*sql.Tx 与 *DB 的公共子集：服务层函数以它为参数，事务内外都能调用。
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// DB 包装 *sql.DB（可直接当 *sql.DB 用），附带数据库文件路径。
type DB struct {
	*sql.DB
	Path string
}

// dsn 连接参数：WAL、busy_timeout、外键、写事务 BEGIN IMMEDIATE。
func dsn(path string) string {
	return path + "?_pragma=busy_timeout(10000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_txlock=immediate"
}

// Open 打开（不存在则创建）数据库并执行待执行的迁移；有待执行迁移且库非空时先备份（保留最近 3 份）。
func Open(path string) (*DB, error) {
	drv := &sqlite.Driver{}
	if err := registerUnicodeFunctions(drv); err != nil {
		return nil, fmt.Errorf("注册 SQL 函数失败：%w", err)
	}
	db := sql.OpenDB(&connector{dsn: dsn(path), drv: drv})
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("打开数据库 %s 失败：%w", path, err)
	}
	d := &DB{DB: db, Path: path}
	if err := d.Migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return d, nil
}

// Tx 在一个写事务（BEGIN IMMEDIATE）里执行 fn：fn 返回错误或 panic 时回滚，否则提交。
//
// 回调里所有读写都必须走 tx，不要再用 d（见 txguard.go）；回调里也不要开新的 Tx。
func (d *DB) Tx(ctx context.Context, fn func(tx *sql.Tx) error) (err error) {
	checkNotInTx(ctx)
	if g := guardOf(ctx); g != nil {
		g.depth.Add(1)
		defer g.depth.Add(-1)
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if p := recover(); p != nil {
			tx.Rollback()
			panic(p)
		}
		if err != nil {
			tx.Rollback()
		}
	}()
	if err = fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// IsUniqueViolation 是否为唯一约束冲突（对应 Prisma P2002）。
func IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") || strings.Contains(msg, "constraint failed: UNIQUE")
}

// IsNoRows 是否为查询无结果。
func IsNoRows(err error) bool { return errors.Is(err, sql.ErrNoRows) }

// Placeholders 生成 n 个 "?" 占位符（IN 子句用），n=0 时返回 "NULL"（IN (NULL) 恒不匹配）。
func Placeholders(n int) string {
	if n <= 0 {
		return "NULL"
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// Args 把字符串切片转为 []any，配合 Placeholders 使用。
func Args[T any](xs []T) []any {
	out := make([]any, len(xs))
	for i, x := range xs {
		out[i] = x
	}
	return out
}
