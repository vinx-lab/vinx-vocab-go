package store

import (
	"context"
	"database/sql"
	"sync/atomic"
)

// 事务误用防护：在 Tx 回调里必须用 tx 读写，不能再用 *DB。
//
// 原因：写事务是 BEGIN IMMEDIATE，已持有写锁；回调里经 *DB（连接池里的另一条连接）写库会一直等到
// busy_timeout（10 秒）后 SQLITE_BUSY，读库则看不到本事务尚未提交的写入。
//
// 做法：API 路由给每个请求的 ctx 挂一个 TxGuard（WithTxGuard）；DB.Tx 在回调期间把它标为「事务中」；
// 此时再用同一个 ctx 调 DB.ExecContext / QueryContext / QueryRowContext / Tx 会立即 panic（路由的 recover
// 转成 500 并记日志），而不是卡 10 秒。没有挂 TxGuard 的 ctx（命令行、seed、迁移）不做检查。
// 局限：只检查带 ctx 的方法；同一请求里开 goroutine 并行做事务时会误报（目前没有这种用法）。

type txGuardKey struct{}

type txGuard struct{ depth atomic.Int32 }

// WithTxGuard 给 ctx 挂上事务误用防护（api 路由对每个请求调用）。
func WithTxGuard(ctx context.Context) context.Context {
	if _, ok := ctx.Value(txGuardKey{}).(*txGuard); ok {
		return ctx
	}
	return context.WithValue(ctx, txGuardKey{}, &txGuard{})
}

// ErrDBInTx 在事务回调里误用 *DB 时 panic 的值。
const ErrDBInTx = "store: 在 DB.Tx 回调里使用了 *store.DB（应使用回调参数 tx），否则写会卡 busy_timeout、读会看不到本事务的写入"

func guardOf(ctx context.Context) *txGuard {
	g, _ := ctx.Value(txGuardKey{}).(*txGuard)
	return g
}

func checkNotInTx(ctx context.Context) {
	if g := guardOf(ctx); g != nil && g.depth.Load() > 0 {
		panic(ErrDBInTx)
	}
}

// ExecContext 同 sql.DB.ExecContext，另做事务误用检查。
func (d *DB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	checkNotInTx(ctx)
	return d.DB.ExecContext(ctx, query, args...)
}

// QueryContext 同 sql.DB.QueryContext，另做事务误用检查。
func (d *DB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	checkNotInTx(ctx)
	return d.DB.QueryContext(ctx, query, args...)
}

// QueryRowContext 同 sql.DB.QueryRowContext，另做事务误用检查。
func (d *DB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	checkNotInTx(ctx)
	return d.DB.QueryRowContext(ctx, query, args...)
}
