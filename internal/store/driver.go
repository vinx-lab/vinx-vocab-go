package store

import (
	"context"
	"database/sql/driver"
	"strings"
	"time"

	sqlite "modernc.org/sqlite"
)

// 连接包装：让 time.Time 作为 SQL 参数时自动写成规范文本（FormatTime：UTC 毫秒 RFC3339）。
//
// modernc 默认把 time.Time 参数写成 "2006-01-02 15:04:05.999 +0000 UTC"，与库里的 "2006-01-02T15:04:05.000Z"
// 字典序不可比（"due" <= ? 会静默出错）。这里在 database/sql 的参数检查阶段（driver.NamedValueChecker）统一转换，
// 覆盖 time.Time、*time.Time、sql.NullTime 以及任何 Value() 返回 time.Time 的 Valuer。

type connector struct {
	dsn string
	drv *sqlite.Driver
}

func (c *connector) Connect(context.Context) (driver.Conn, error) {
	inner, err := c.drv.Open(c.dsn)
	if err != nil {
		return nil, err
	}
	return &conn{inner: inner}, nil
}

func (c *connector) Driver() driver.Driver { return c.drv }

type conn struct{ inner driver.Conn }

// CheckNamedValue 先按 database/sql 默认规则转换（Valuer、指针、命名类型），再把 time.Time 格式化为规范文本。
func (c *conn) CheckNamedValue(nv *driver.NamedValue) error {
	v, err := driver.DefaultParameterConverter.ConvertValue(nv.Value)
	if err != nil {
		return err
	}
	if t, ok := v.(time.Time); ok {
		v = FormatTime(t)
	}
	nv.Value = v
	return nil
}

func (c *conn) Prepare(query string) (driver.Stmt, error) { return c.inner.Prepare(query) }
func (c *conn) Close() error                              { return c.inner.Close() }
func (c *conn) Begin() (driver.Tx, error)                 { return c.inner.Begin() } //nolint:staticcheck

func (c *conn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	return c.inner.(driver.ConnBeginTx).BeginTx(ctx, opts)
}

func (c *conn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	return c.inner.(driver.ConnPrepareContext).PrepareContext(ctx, query)
}

func (c *conn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	return c.inner.(driver.ExecerContext).ExecContext(ctx, query, args)
}

func (c *conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	return c.inner.(driver.QueryerContext).QueryContext(ctx, query, args)
}

func (c *conn) Ping(ctx context.Context) error { return c.inner.(driver.Pinger).Ping(ctx) }

func (c *conn) ResetSession(ctx context.Context) error {
	return c.inner.(driver.SessionResetter).ResetSession(ctx)
}

func (c *conn) IsValid() bool { return c.inner.(driver.Validator).IsValid() }

// registerUnicodeFunctions 注册 VINX_ICONTAINS(haystack, needle)：Unicode 大小写不敏感的字面子串匹配（不是通配符）。
//
// SQLite 内置 LIKE 默认只在 ASCII 范围内不区分大小写；旧版 Prisma 的 `mode: "insensitive"`（Postgres ILIKE）
// 对整个 Unicode 都不区分大小写（如 É/é）。这里用 strings.ToLower（Unicode 大小写折叠）在 Go 侧比较，
// 注册到本次 Open 用的这一个 *sqlite.Driver 实例上，只影响它开出的连接（M4）。
func registerUnicodeFunctions(drv *sqlite.Driver) error {
	return drv.RegisterDeterministicScalarFunction("VINX_ICONTAINS", 2,
		func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			haystack, ok := sqlText(args[0])
			if !ok {
				return int64(0), nil
			}
			needle, ok := sqlText(args[1])
			if !ok {
				return int64(0), nil
			}
			if strings.Contains(strings.ToLower(haystack), strings.ToLower(needle)) {
				return int64(1), nil
			}
			return int64(0), nil
		})
}

// sqlText 把 SQL 函数参数（string / []byte / nil）规整为字符串；NULL 参数返回 ("", false)。
func sqlText(v driver.Value) (string, bool) {
	switch t := v.(type) {
	case string:
		return t, true
	case []byte:
		return string(t), true
	default:
		return "", false
	}
}
