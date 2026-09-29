package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// I1：time.Time 作为 SQL 参数时写成规范文本，字典序比较与 FormatTime 一致。
func TestTimeArgsCanonical(t *testing.T) {
	db := openTemp(t)
	ts := time.Date(2026, 9, 28, 13, 51, 26, 656789000, time.FixedZone("CST", 8*3600))
	want := "2026-09-28T05:51:26.656Z"
	nt := sql.NullTime{Time: ts, Valid: true}
	for name, arg := range map[string]any{"time.Time": ts, "*time.Time": &ts, "sql.NullTime": nt, "store.Time": NewTime(ts)} {
		var got string
		if err := db.QueryRow(`SELECT CAST(? AS TEXT)`, arg).Scan(&got); err != nil || got != want {
			t.Errorf("%s → %q (%v)", name, got, err)
		}
	}
	var null sql.NullString
	db.QueryRow(`SELECT ?`, sql.NullTime{}).Scan(&null)
	if null.Valid {
		t.Error("无效 sql.NullTime 应为 NULL")
	}

	// 写入裸 time.Time，再按字典序比较
	seedUser(t, db, "u1")
	db.Exec(`INSERT INTO "Word" ("id","spelling","definition") VALUES ('w1','a','一')`)
	due := time.Date(2026, 9, 28, 5, 0, 0, 0, time.UTC)
	if _, err := db.Exec(`INSERT INTO "MemoryState" ("id","userId","wordId","due","stability","difficulty","introducedDay","introducedAt","updatedAt") VALUES ('m1','u1','w1',?,1,1,'2026-09-28',?,?)`, due, due, due); err != nil {
		t.Fatal(err)
	}
	var raw string
	db.QueryRow(`SELECT "due" FROM "MemoryState"`).Scan(&raw)
	if raw != "2026-09-28T05:00:00.000Z" {
		t.Fatalf("存储文本 %q", raw)
	}
	var n int
	db.QueryRow(`SELECT count(*) FROM "MemoryState" WHERE "due" <= ?`, due.Add(time.Hour)).Scan(&n)
	if n != 1 {
		t.Fatal("同一天稍后的时刻应判为已到期")
	}
	db.QueryRow(`SELECT count(*) FROM "MemoryState" WHERE "due" <= ?`, due.Add(-time.Millisecond)).Scan(&n)
	if n != 0 {
		t.Fatal("早 1 毫秒应判为未到期")
	}
	// 事务里同样生效
	err := db.Tx(context.Background(), func(tx *sql.Tx) error {
		return tx.QueryRow(`SELECT count(*) FROM "MemoryState" WHERE "due" <= ?`, due).Scan(&n)
	})
	if err != nil || n != 1 {
		t.Fatalf("tx n=%d err=%v", n, err)
	}
	// 其他参数类型不受影响
	var s string
	var i int64
	var b bool
	if err := db.QueryRow(`SELECT ?, ?, ?`, "x", 42, true).Scan(&s, &i, &b); err != nil || s != "x" || i != 42 || !b {
		t.Fatalf("普通参数 %q %d %v %v", s, i, b, err)
	}
}

// I2：nil 切片 / nil map 写入与输出都规整为 [] / {}。
func TestJSONNilNormalized(t *testing.T) {
	var ids []string
	v, _ := NewJSON(ids).Value()
	if v != "[]" {
		t.Errorf("nil 切片 Value = %v", v)
	}
	var m map[string]any
	v, _ = NewJSON(m).Value()
	if v != "{}" {
		t.Errorf("nil map Value = %v", v)
	}
	v, _ = JSON[[]string]{}.Value()
	if v != "[]" {
		t.Errorf("零值 Value = %v", v)
	}
	b, _ := json.Marshal(map[string]any{"a": JSON[[]string]{}, "b": NewJSON(m), "c": JSON[[]string]{Null: true}, "d": NewJSON([]int{1})})
	if string(b) != `{"a":[],"b":{},"c":null,"d":[1]}` {
		t.Errorf("JSON = %s", b)
	}
	// 写库再读出仍是 []
	db := openTemp(t)
	seedUser(t, db, "u1")
	if _, err := db.Exec(`INSERT INTO "WordSheet" ("id","userId","creatorId","seq","wordIds") VALUES ('s1','u1','u1',1,?)`, NewJSON(ids)); err != nil {
		t.Fatal(err)
	}
	var raw string
	var back JSON[[]string]
	db.QueryRow(`SELECT "wordIds", "wordIds" FROM "WordSheet"`).Scan(&raw, &back)
	b, _ = json.Marshal(back)
	if raw != "[]" || string(b) != "[]" {
		t.Errorf("库里 %q，输出 %s", raw, b)
	}
}

// I3：事务回调里误用 *DB 立即 panic，而不是卡 busy_timeout。
func TestDBInsideTxPanicsFast(t *testing.T) {
	db := openTemp(t)
	ctx := WithTxGuard(context.Background())
	expectPanic := func(name string, fn func()) {
		t.Helper()
		start := time.Now()
		defer func() {
			r := recover()
			if r == nil || !strings.Contains(r.(string), "tx") {
				t.Errorf("%s 应 panic，得到 %v", name, r)
			}
			if time.Since(start) > time.Second {
				t.Errorf("%s 耗时 %v", name, time.Since(start))
			}
		}()
		fn()
	}
	insert := `INSERT INTO "User" ("id","email","passwordHash","name") VALUES ('a','a@x','h','a')`
	expectPanic("ExecContext", func() {
		db.Tx(ctx, func(tx *sql.Tx) error { _, err := db.ExecContext(ctx, insert); return err })
	})
	expectPanic("QueryRowContext", func() {
		db.Tx(ctx, func(tx *sql.Tx) error { var n int; return db.QueryRowContext(ctx, `SELECT 1`).Scan(&n) })
	})
	expectPanic("QueryContext", func() {
		db.Tx(ctx, func(tx *sql.Tx) error { _, err := db.QueryContext(ctx, `SELECT 1`); return err })
	})
	expectPanic("嵌套 Tx", func() {
		db.Tx(ctx, func(tx *sql.Tx) error { return db.Tx(ctx, func(*sql.Tx) error { return nil }) })
	})
	// panic 后事务已回滚、防护已复位：正常用法照常工作
	if err := db.Tx(ctx, func(tx *sql.Tx) error { _, err := tx.ExecContext(ctx, insert); return err }); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM "User"`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	// 通过 Querier 接口传入 *DB 同样被拦住（服务层函数的典型误用）
	expectPanic("Querier", func() {
		db.Tx(ctx, func(tx *sql.Tx) error {
			var q Querier = db
			_, err := q.ExecContext(ctx, `DELETE FROM "User"`)
			return err
		})
	})
}
