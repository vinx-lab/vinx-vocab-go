package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"
	"testing/fstest"
	"time"
)

func openTemp(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "vinx.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestMigrateCreatesAllTables(t *testing.T) {
	db := openTemp(t)
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name <> 'schema_migrations' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for rows.Next() {
		var n string
		rows.Scan(&n)
		names = append(names, n)
	}
	rows.Close()
	want := []string{"Answer", "AppSetting", "Book", "ClassMember", "Classroom", "MemoryState", "Passage", "Plan", "PlanTarget", "PlanUnit", "ReviewLog", "StudySession", "Unit", "UnitWord", "User", "Word", "WordSheet",
		"ClassTargetBook", "UserTargetBook"} // 0002_target_books
	sort.Strings(want)
	if !equal(names, want) {
		t.Fatalf("tables = %v", names)
	}
	var idx int
	db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='index' AND name='StudySession_active_uniq'`).Scan(&idx)
	if idx != 1 {
		t.Fatal("缺少 StudySession_active_uniq")
	}
	// 连接参数生效
	var fk, busy int
	var mode string
	db.QueryRow(`PRAGMA foreign_keys`).Scan(&fk)
	db.QueryRow(`PRAGMA busy_timeout`).Scan(&busy)
	db.QueryRow(`PRAGMA journal_mode`).Scan(&mode)
	if fk != 1 || busy < 1000 || mode != "wal" {
		t.Fatalf("pragma: fk=%d busy=%d mode=%s", fk, busy, mode)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func seedUser(t *testing.T, db *DB, id string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO "User" ("id","email","passwordHash","name") VALUES (?,?,?,?)`, id, id+"@x.test", "h", id); err != nil {
		t.Fatal(err)
	}
}

func insertSession(db *DB, id, user string, plan any, kind, status string) error {
	_, err := db.Exec(`INSERT INTO "StudySession" ("id","userId","planId","kind","status","dayKey","snapshot") VALUES (?,?,?,?,?,?,?)`,
		id, user, plan, kind, status, "2026-09-28", `{"items":[]}`)
	return err
}

func TestActiveSessionPartialUniqueIndex(t *testing.T) {
	db := openTemp(t)
	seedUser(t, db, "u1")
	seedUser(t, db, "u2")
	if _, err := db.Exec(`INSERT INTO "Plan" ("id","name","creatorId") VALUES ('p1','计划','u1')`); err != nil {
		t.Fatal(err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(insertSession(db, "s1", "u1", "p1", "learn", "active"))
	// 同一学生同一计划同一类型的第二个 active → 冲突
	if err := insertSession(db, "s2", "u1", "p1", "learn", "active"); !IsUniqueViolation(err) {
		t.Fatalf("期望唯一冲突，得到 %v", err)
	}
	// 无计划（NULL）按 '' 参与唯一判断
	must(insertSession(db, "s3", "u1", nil, "drill", "active"))
	if err := insertSession(db, "s4", "u1", nil, "drill", "active"); !IsUniqueViolation(err) {
		t.Fatalf("planId NULL 时期望唯一冲突，得到 %v", err)
	}
	// 已完成的不受限制；不同类型、不同学生不冲突
	must(insertSession(db, "s5", "u1", "p1", "learn", "completed"))
	must(insertSession(db, "s6", "u1", "p1", "review", "active"))
	must(insertSession(db, "s7", "u2", "p1", "learn", "active"))
	// 完成后可以再开
	_, err := db.Exec(`UPDATE "StudySession" SET "status"='completed' WHERE "id"='s1'`)
	must(err)
	must(insertSession(db, "s8", "u1", "p1", "learn", "active"))
}

func TestForeignKeysCascade(t *testing.T) {
	db := openTemp(t)
	seedUser(t, db, "u1")
	if _, err := db.Exec(`INSERT INTO "Book" ("id","name","ownerId") VALUES ('b1','书','u1')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO "Classroom" ("id","name","teacherId","inviteCode") VALUES ('c1','班','u1','ABCDEF')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM "User" WHERE "id"='u1'`); err != nil {
		t.Fatal(err)
	}
	var owner sql.NullString
	db.QueryRow(`SELECT "ownerId" FROM "Book" WHERE "id"='b1'`).Scan(&owner)
	if owner.Valid {
		t.Error("Book.ownerId 应 SET NULL")
	}
	var n int
	db.QueryRow(`SELECT count(*) FROM "Classroom"`).Scan(&n)
	if n != 0 {
		t.Error("Classroom 应级联删除")
	}
	if _, err := db.Exec(`INSERT INTO "Unit" ("id","bookId","name") VALUES ('x','nope','U')`); err == nil {
		t.Error("外键未生效")
	}
}

func TestTxRollbackAndCommit(t *testing.T) {
	db := openTemp(t)
	ctx := context.Background()
	err := db.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO "User" ("id","email","passwordHash","name") VALUES ('a','a@x','h','a')`); err != nil {
			return err
		}
		return os.ErrInvalid
	})
	if err != os.ErrInvalid {
		t.Fatalf("err = %v", err)
	}
	var n int
	db.QueryRow(`SELECT count(*) FROM "User"`).Scan(&n)
	if n != 0 {
		t.Fatal("回滚失败")
	}
	if err := db.Tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO "User" ("id","email","passwordHash","name") VALUES ('a','a@x','h','a')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	db.QueryRow(`SELECT count(*) FROM "User"`).Scan(&n)
	if n != 1 {
		t.Fatal("提交失败")
	}
}

func TestDefaultsAndTimeCodec(t *testing.T) {
	db := openTemp(t)
	seedUser(t, db, "u1")
	var created Time
	var role, theme string
	if err := db.QueryRow(`SELECT "createdAt","role","theme" FROM "User"`).Scan(&created, &role, &theme); err != nil {
		t.Fatal(err)
	}
	if role != "student" || theme != "system" || time.Since(created.Time) > time.Minute {
		t.Fatalf("defaults: %s %s %v", role, theme, created)
	}
	var raw string
	db.QueryRow(`SELECT "createdAt" FROM "User"`).Scan(&raw)
	if !regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$`).MatchString(raw) {
		t.Fatalf("默认时间格式 %q", raw)
	}

	ts := time.Date(2026, 9, 28, 5, 51, 26, 656789000, time.FixedZone("CST", 8*3600))
	if got := FormatTime(ts); got != "2026-09-27T21:51:26.656Z" {
		t.Fatalf("FormatTime = %s", got)
	}
	p, err := ParseTime("2026-09-27T21:51:26.656Z")
	if err != nil || !p.Equal(time.Date(2026, 9, 27, 21, 51, 26, 656000000, time.UTC)) {
		t.Fatalf("ParseTime = %v %v", p, err)
	}
	b, _ := json.Marshal(struct {
		A Time     `json:"a"`
		B NullTime `json:"b"`
		C NullTime `json:"c"`
	}{NewTime(ts), NullTime{}, NewNullTime(ts)})
	if string(b) != `{"a":"2026-09-27T21:51:26.656Z","b":null,"c":"2026-09-27T21:51:26.656Z"}` {
		t.Fatalf("JSON = %s", b)
	}

	// NullTime 往返
	if _, err := db.Exec(`UPDATE "User" SET "updatedAt" = ?`, NewTime(ts)); err != nil {
		t.Fatal(err)
	}
	var nt NullTime
	db.QueryRow(`SELECT "currentGrade" FROM "User"`).Scan(&nt)
	if nt.Valid {
		t.Fatal("NULL 应扫描为无效 NullTime")
	}
}

func TestJSONColumn(t *testing.T) {
	db := openTemp(t)
	seedUser(t, db, "u1")
	if _, err := db.Exec(`INSERT INTO "Plan" ("id","name","creatorId","modes") VALUES ('p1','计划','u1',?)`, NewJSON([]string{"recognition"})); err != nil {
		t.Fatal(err)
	}
	var modes JSON[[]string]
	db.QueryRow(`SELECT "modes" FROM "Plan"`).Scan(&modes)
	if modes.Null || len(modes.V) != 1 || modes.V[0] != "recognition" {
		t.Fatalf("modes = %+v", modes)
	}
	if _, err := db.Exec(`INSERT INTO "Plan" ("id","name","creatorId") VALUES ('p2','默认','u1')`); err != nil {
		t.Fatal(err)
	}
	db.QueryRow(`SELECT "modes" FROM "Plan" WHERE "id"='p2'`).Scan(&modes)
	if len(modes.V) != 2 {
		t.Fatalf("默认 modes = %+v", modes)
	}
	insertSession(db, "s1", "u1", nil, "learn", "active")
	var progress JSON[map[string]any]
	db.QueryRow(`SELECT "progress" FROM "StudySession"`).Scan(&progress)
	b, _ := json.Marshal(map[string]any{"progress": progress})
	if !progress.Null || string(b) != `{"progress":null}` {
		t.Fatalf("progress = %s", b)
	}
}

func TestNewID(t *testing.T) {
	seen := map[string]bool{}
	re := regexp.MustCompile(`^c[0-9a-z]{24}$`)
	for i := 0; i < 5000; i++ {
		id := NewID()
		if !re.MatchString(id) {
			t.Fatalf("id 格式 %q", id)
		}
		if seen[id] {
			t.Fatalf("id 重复 %q", id)
		}
		seen[id] = true
	}
}

func TestBackupBeforePendingMigrationKeepsThree(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vinx.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	seedUser(t, db, "u1")
	// 首次建库不备份
	if m, _ := filepath.Glob(path + ".bak-*"); len(m) != 0 {
		t.Fatalf("首次建库不应备份：%v", m)
	}
	// 模拟「有新迁移待执行」：删掉记录后重新迁移会触发备份（迁移本身失败于已存在的表，这里只验证备份与保留份数）
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if _, err := db.Backup(ctx, time.Date(2026, 9, 28, 10, 0, i, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
	}
	m, _ := filepath.Glob(path + ".bak-*")
	sort.Strings(m)
	if len(m) != 3 || filepath.Base(m[0]) != "vinx.db.bak-20260928-100002" {
		t.Fatalf("备份保留 = %v", m)
	}
	// 备份是完整可打开的库
	bak, err := sql.Open("sqlite", m[2])
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := bak.QueryRow(`SELECT count(*) FROM "User"`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("备份内容 n=%d err=%v", n, err)
	}
	bak.Close()
	db.Close()

	for _, f := range m {
		os.Remove(f)
	}
	db2, err := Open(path) // 没有待执行迁移 → 不备份
	if err != nil {
		t.Fatal(err)
	}
	db2.Close()
	if m, _ := filepath.Glob(path + ".bak-*"); len(m) != 0 {
		t.Fatalf("无待执行迁移时不应备份：%v", m)
	}

	// 新增一条迁移：重新 Open 时先备份（备份里没有新表），再执行迁移
	init0001, _ := fs.ReadFile(migrationsFS, "migrations/0001_init.sql")
	migrationSource = fstest.MapFS{
		"migrations/0001_init.sql":  {Data: init0001},
		"migrations/0002_extra.sql": {Data: []byte(`CREATE TABLE "Extra" ("x" TEXT);`)},
	}
	t.Cleanup(func() { migrationSource = migrationsFS })
	db3, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db3.Close()
	if _, err := db3.Exec(`INSERT INTO "Extra" VALUES ('1')`); err != nil {
		t.Fatal("新迁移未执行：", err)
	}
	m, _ = filepath.Glob(path + ".bak-*")
	if len(m) != 1 {
		t.Fatalf("应有 1 份迁移前备份：%v", m)
	}
	bak2, _ := sql.Open("sqlite", m[0])
	defer bak2.Close()
	if err := bak2.QueryRow(`SELECT count(*) FROM "Extra"`).Scan(&n); err == nil {
		t.Fatal("备份应是迁移前的状态")
	}
	// 前面按真实迁移目录建库（已执行全部内置迁移），这里再加上 0002_extra
	builtin, _ := fs.ReadDir(migrationsFS, "migrations")
	var versions int
	db3.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&versions)
	if versions != len(builtin)+1 {
		t.Fatalf("schema_migrations = %d", versions)
	}
}
