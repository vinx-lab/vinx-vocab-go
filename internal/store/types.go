package store

import (
	"crypto/rand"
	"database/sql"
	"database/sql/driver"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

type sqlTx = sql.Tx

// TimeLayout 时间文本格式：UTC 毫秒 RFC3339，与旧版 JSON 输出（Date.toISOString）一致。
const TimeLayout = "2006-01-02T15:04:05.000Z"

// FormatTime 时间 → 存储 / 输出文本（转 UTC、截到毫秒）。
func FormatTime(t time.Time) string { return t.UTC().Format(TimeLayout) }

// ParseTime 解析存储的时间文本（也接受其他 RFC3339 变体，如导入时的微秒精度）。
func ParseTime(s string) (time.Time, error) {
	if t, err := time.Parse(TimeLayout, s); err == nil {
		return t, nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("无法解析时间 %q：%w", s, err)
	}
	return t.UTC(), nil
}

// Time 非空时间列：扫描 TEXT、写入 FormatTime 文本、JSON 输出为 ISO 毫秒串。
type Time struct{ time.Time }

// NewTime 包装时间（截到毫秒）。
func NewTime(t time.Time) Time { return Time{t.UTC().Truncate(time.Millisecond)} }

func (t *Time) Scan(src any) error {
	switch v := src.(type) {
	case string:
		p, err := ParseTime(v)
		t.Time = p
		return err
	case []byte:
		p, err := ParseTime(string(v))
		t.Time = p
		return err
	case time.Time:
		t.Time = v.UTC()
		return nil
	case nil:
		return fmt.Errorf("store.Time: 列为 NULL，请改用 NullTime")
	}
	return fmt.Errorf("store.Time: 不支持的类型 %T", src)
}

func (t Time) Value() (driver.Value, error) { return FormatTime(t.Time), nil }

func (t Time) MarshalJSON() ([]byte, error) { return json.Marshal(FormatTime(t.Time)) }

func (t *Time) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	p, err := ParseTime(s)
	t.Time = p
	return err
}

// NullTime 可空时间列：NULL ↔ JSON null。
type NullTime struct {
	Time  time.Time
	Valid bool
}

// NewNullTime 非空的 NullTime。
func NewNullTime(t time.Time) NullTime {
	return NullTime{Time: t.UTC().Truncate(time.Millisecond), Valid: true}
}

func (t *NullTime) Scan(src any) error {
	if src == nil {
		t.Time, t.Valid = time.Time{}, false
		return nil
	}
	var inner Time
	if err := inner.Scan(src); err != nil {
		return err
	}
	t.Time, t.Valid = inner.Time, true
	return nil
}

func (t NullTime) Value() (driver.Value, error) {
	if !t.Valid {
		return nil, nil
	}
	return FormatTime(t.Time), nil
}

func (t NullTime) MarshalJSON() ([]byte, error) {
	if !t.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(FormatTime(t.Time))
}

func (t *NullTime) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		t.Valid = false
		return nil
	}
	var inner Time
	if err := inner.UnmarshalJSON(b); err != nil {
		return err
	}
	t.Time, t.Valid = inner.Time, true
	return nil
}

// JSON 存为 JSON 文本的列（Prisma 的 Json 与 String[]）。
// 写入：V 序列化为文本（Null=true 时写 NULL）；读出：NULL → Null=true。
// JSON 输出：Null=true 时为 null，否则为 V 本身。
// V 为 nil 切片 / nil map 时（Null=false）写入与输出都规整为 [] / {}；只规整顶层，嵌套结构里的 nil 切片仍输出 null。
type JSON[T any] struct {
	V    T
	Null bool
}

// NewJSON 非空 JSON 值。
func NewJSON[T any](v T) JSON[T] { return JSON[T]{V: v} }

func (j *JSON[T]) Scan(src any) error {
	var b []byte
	switch v := src.(type) {
	case nil:
		var zero T
		j.V, j.Null = zero, true
		return nil
	case string:
		b = []byte(v)
	case []byte:
		b = v
	default:
		return fmt.Errorf("store.JSON: 不支持的类型 %T", src)
	}
	j.Null = false
	return json.Unmarshal(b, &j.V)
}

// normalized nil 切片 / nil map 规整为空数组 / 空对象（与旧版输出 [] 一致，避免把文本 "null" 写进 NOT NULL 列）。
func (j JSON[T]) normalized() any {
	rv := reflect.ValueOf(&j.V).Elem()
	switch rv.Kind() {
	case reflect.Slice:
		if rv.IsNil() {
			return []any{}
		}
	case reflect.Map:
		if rv.IsNil() {
			return map[string]any{}
		}
	}
	return j.V
}

func (j JSON[T]) Value() (driver.Value, error) {
	if j.Null {
		return nil, nil
	}
	b, err := json.Marshal(j.normalized())
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

func (j JSON[T]) MarshalJSON() ([]byte, error) {
	if j.Null {
		return []byte("null"), nil
	}
	return json.Marshal(j.normalized())
}

func (j *JSON[T]) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		var zero T
		j.V, j.Null = zero, true
		return nil
	}
	j.Null = false
	return json.Unmarshal(b, &j.V)
}

// ---------------------------------------------------------------------------
// cuid 风格 id（与 Prisma @default(cuid()) 同形：c + 时间戳 + 计数 + 指纹 + 随机，25 位小写 base36）
// ---------------------------------------------------------------------------

var (
	idCounter     atomic.Uint32
	idFingerprint = func() string {
		host, _ := os.Hostname()
		sum := os.Getpid()
		for _, r := range host {
			sum += int(r)
		}
		sum += len(host) + 36
		return pad(strconv.FormatInt(int64(os.Getpid()), 36), 2) + pad(strconv.FormatInt(int64(sum), 36), 2)
	}()
)

func init() {
	var b [4]byte
	rand.Read(b[:])
	idCounter.Store(binary.LittleEndian.Uint32(b[:]) % (36 * 36 * 36 * 36))
}

func pad(s string, n int) string {
	if len(s) >= n {
		return s[len(s)-n:]
	}
	return strings.Repeat("0", n-len(s)) + s
}

func randomBlock() string {
	n, _ := rand.Int(rand.Reader, big.NewInt(36*36*36*36))
	return pad(strconv.FormatInt(n.Int64(), 36), 4)
}

// NewID 生成 cuid 风格 id（如 cmg3x0k2a0000abcd1234wxyz），按时间大致递增，全局唯一。
func NewID() string {
	ts := strconv.FormatInt(time.Now().UnixMilli(), 36)
	cnt := pad(strconv.FormatUint(uint64(idCounter.Add(1)%(36*36*36*36)), 36), 4)
	return "c" + pad(ts, 8) + cnt + idFingerprint + randomBlock() + randomBlock()
}
