package migrate

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vinx-lab/vinx-vocab-go/internal/config"
	"github.com/vinx-lab/vinx-vocab-go/internal/core/secretbox"
	"github.com/vinx-lab/vinx-vocab-go/internal/seed"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// 这些测试需要一个 PostgreSQL（当前角色可 CREATE DATABASE）：
//
//	VINX_TEST_PG_URL=postgresql://user:pass@localhost:5433/postgres go test ./internal/migrate/
//
// 每个测试新建一个临时库（vinx_import_test_<随机>），用 testdata/pg_schema.sql（旧版结构）建表并写入样例，结束后删除。
// 没设该变量时跳过。

// 旧实例的设置加密密钥与用它（node:crypto，旧 lib/secret-box.ts）加密的 Key：取自 secretbox 的互通样本。
const (
	oldSecret = "interop-test-settings-secret-xyz"
	oldBox    = "v1:nu_kFXJxLOs83K5sXTRsgtgI1ZFzbIUrIxaUoVKWgVwQfj1cYBGcQGY1vfAVz4P2Blg"
	oldPlain  = "sk-test-1234567890abcd"
)

const fixtureSQL = `
INSERT INTO "User" (id,email,"passwordHash",name,role,"currentGrade",theme,"createdById","createdAt","updatedAt") VALUES
 ('u_admin','admin@x.test','$2a$12$abcdefghijklmnopqrstuuJ4v0y3b5x3tq9wX1e9m1Yl8m6oW7Qx2','管理员','admin',NULL,'system',NULL,'2026-09-01 08:00:00.123','2026-09-01 08:00:00.123'),
 ('u_stu','stu@x.test','$2b$12$abcdefghijklmnopqrstuuJ4v0y3b5x3tq9wX1e9m1Yl8m6oW7Qx2','小明','student','初二','dark','u_admin','2026-09-02 09:30:00.000','2026-09-03 10:00:00.999');
INSERT INTO "Book" (id,name,description,"isSystem","ownerId","sortOrder","createdAt","updatedAt") VALUES
 ('b1','七年级上册',NULL,true,NULL,0,'2026-09-01 00:00:00.001','2026-09-01 00:00:00.001');
INSERT INTO "Unit" (id,"bookId",name,"sortOrder","createdAt") VALUES ('un1','b1','Unit 1',0,'2026-09-01 00:00:00.002');
INSERT INTO "Word" (id,spelling,type,phonetic,"partOfSpeech",definition,example,"exampleCn","exampleSource","exampleAt","audioFile","createdAt","updatedAt") VALUES
 ('w1','apple','word','ˈæpl','n.','苹果','I like apples.','我喜欢苹果。','ai','2026-09-05 01:02:03.456','apple.mp3','2026-09-01 00:00:00.003','2026-09-01 00:00:00.003'),
 ('w2','look after','phrase',NULL,NULL,'照顾',NULL,NULL,NULL,NULL,NULL,'2026-09-01 00:00:00.004','2026-09-01 00:00:00.004'),
 ('w3','zoo','word',NULL,'n.','动物园',NULL,NULL,NULL,NULL,NULL,'2026-09-01 00:00:00.005','2026-09-01 00:00:00.005');
-- 故意不按主键顺序插入：导入后 rowid 顺序应与源库物理顺序一致
INSERT INTO "UnitWord" ("unitId","wordId","sortOrder") VALUES ('un1','w3',2),('un1','w1',0),('un1','w2',1);
INSERT INTO "Classroom" (id,name,"teacherId","inviteCode",archived,"createdAt","updatedAt") VALUES ('c1','一班','u_admin','ABC123',false,'2026-09-02 00:00:00.000','2026-09-02 00:00:00.000');
INSERT INTO "ClassMember" ("classId","userId","joinedAt") VALUES ('c1','u_stu','2026-09-02 00:00:01.000');
INSERT INTO "Plan" (id,name,"creatorId",kind,status,"newPerDay","reviewPerDay",modes,"order","testSize","testScope","startDate","endDate","createdAt","updatedAt") VALUES
 ('p1','每日背词','u_admin','daily','active',10,50,ARRAY['recognition','spelling'],'sequential',20,'all','2026-09-02',NULL,'2026-09-02 00:00:02.000','2026-09-02 00:00:02.000'),
 ('p2','检测','u_admin','test','paused',5,20,ARRAY[]::text[],'random',20,'learned',NULL,NULL,'2026-09-02 00:00:03.000','2026-09-02 00:00:03.000');
INSERT INTO "PlanUnit" ("planId","unitId","sortOrder") VALUES ('p1','un1',0);
INSERT INTO "PlanTarget" (id,"planId","classId","userId") VALUES ('pt1','p1','c1',NULL),('pt2','p2',NULL,'u_stu');
-- 旧数据形态：非新卡但没有 lastReview、d = s = 0
INSERT INTO "MemoryState" (id,"userId","wordId",due,stability,difficulty,"elapsedDays","scheduledDays","learningSteps",reps,lapses,state,"lastReview","introducedAt","introducedPlanId","introducedDay","updatedAt") VALUES
 ('m1','u_stu','w1','2026-09-10 16:00:00.000',3.17295633,5.28289744,0,3,0,1,0,2,'2026-09-07 12:34:56.789','2026-09-07 12:34:56.789','p1','2026-09-07','2026-09-07 12:34:56.789'),
 ('m2','u_stu','w2','2026-09-08 16:00:00.000',0,0,0,0,0,3,1,2,NULL,'2026-09-06 12:00:00.000',NULL,'2026-09-06','2026-09-06 12:00:00.000');
INSERT INTO "WordSheet" (id,"userId","creatorId",seq,"wordIds",modes,"createdAt") VALUES ('ws1','u_stu','u_admin',1,ARRAY['w1','w3'],ARRAY['spelling'],'2026-09-08 00:00:00.000');
INSERT INTO "StudySession" (id,"userId","planId","sheetId",kind,"wordCount",status,"dayKey",snapshot,progress,result,"startedAt","completedAt","completedDay") VALUES
 ('s1','u_stu','p1',NULL,'learn',2,'completed','2026-09-07','{"planName":"每日背词","modes":["recognition","spelling"],"items":[{"wordId":"w1","options":["苹果","照顾"],"n":1.5e-7}]}',NULL,'{"correct":2,"total":2}','2026-09-07 12:00:00.000','2026-09-07 12:34:56.789','2026-09-07'),
 ('s2','u_stu','p2',NULL,'test',1,'active','2026-09-08','{"planName":"检测","modes":[],"items":[{"wordId":"w3"}]}','{"phase":"test","index":0}',NULL,'2026-09-08 01:00:00.000',NULL,NULL),
 ('s3','u_stu',NULL,'ws1','sheet',2,'active','2026-09-08','{"items":[]}',NULL,NULL,'2026-09-08 02:00:00.000',NULL,NULL);
INSERT INTO "Answer" (id,"sessionId","userId","wordId",mode,phase,attempt,correct,"userAnswer","hintUsed","durationMs","dayKey","createdAt","dontKnow") VALUES
 ('a1','s1','u_stu','w1','recognition','practice',1,true,NULL,false,1200,'2026-09-07','2026-09-07 12:10:00.000',false),
 ('a2','s1','u_stu','w2','spelling','practice',1,false,'look at',true,3400,'2026-09-07','2026-09-07 12:11:00.000',true);
INSERT INTO "Passage" (id,"userId",title,"titleCn",body,"bodyCn",questions,"wordIds",source,model,"createdAt") VALUES
 ('pa1','u_stu','At the Zoo',NULL,'We went to the zoo.',NULL,NULL,ARRAY['w3'],'ai','qwen','2026-09-08 03:00:00.000'),
 ('pa2','u_stu','Apples','苹果','I like "apples".','我喜欢苹果。','[{"q":"What?","options":["a","b"],"answer":0}]',ARRAY[]::text[],'manual',NULL,'2026-09-08 04:00:00.000');
INSERT INTO "ReviewLog" (id,"userId","wordId","sessionId",rating,"stateBefore","stabilityAfter","difficultyAfter","dueAfter","reviewedAt","dayKey") VALUES
 ('r1','u_stu','w1','s1',3,0,3.17295633,5.28289744,'2026-09-10 16:00:00.000','2026-09-07 12:34:56.789','2026-09-07');
INSERT INTO "AppSetting" (key,value,"updatedById","updatedAt") VALUES
 ('ai','{"provider":"openai","baseUrl":"http://llm.local/v1?a=1&b=2","apiKeyEnc":"` + oldBox + `","model":"m","timeoutMs":60000}','u_admin','2026-09-09 00:00:00.000'),
 ('ai.promptTemplates','{"example":"自定义 <要求> & 说明"}','u_admin','2026-09-09 00:00:01.000');
`

type pgFixture struct {
	url string // 源库连接串
}

func newSource(t *testing.T) *pgFixture {
	t.Helper()
	admin := os.Getenv("VINX_TEST_PG_URL")
	if admin == "" {
		if os.Getenv("VINX_REQUIRE_PG") != "" {
			t.Fatal("VINX_REQUIRE_PG 已设置但没有 VINX_TEST_PG_URL")
		}
		t.Skip("未设置 VINX_TEST_PG_URL，跳过需要 PostgreSQL 的导入测试")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		t.Fatalf("连接 VINX_TEST_PG_URL: %v", err)
	}
	defer conn.Close(ctx)
	name := fmt.Sprintf("vinx_import_test_%d_%d", time.Now().UnixNano()%1e9, rand.Intn(1e6))
	if _, err := conn.Exec(ctx, `CREATE DATABASE `+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c, err := pgx.Connect(context.Background(), admin)
		if err == nil {
			c.Exec(context.Background(), `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`)
			c.Close(context.Background())
		}
	})
	u, _ := url.Parse(admin)
	u.Path = "/" + name
	// 源会话时区固定为非 UTC：验证 timestamp(3) without time zone 导入时不随会话时区偏移（CI 的 PG 可能是 UTC）
	q := u.Query()
	q.Set("timezone", "Asia/Shanghai")
	u.RawQuery = q.Encode()
	f := &pgFixture{url: u.String()}
	schema, err := os.ReadFile("testdata/pg_schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	f.exec(t, string(schema))
	f.exec(t, fixtureSQL)
	return f
}

func (f *pgFixture) exec(t *testing.T, sql string) {
	t.Helper()
	ctx := context.Background()
	c, err := pgx.Connect(ctx, f.url)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(ctx)
	if _, err := c.Exec(ctx, sql); err != nil {
		t.Fatalf("exec: %v", err)
	}
}

func newTarget(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Load(config.Options{DataDir: t.TempDir(), Lookup: func(string) (string, bool) { return "", false }})
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func openDB(t *testing.T, cfg *config.Config) *store.DB {
	t.Helper()
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func count(t *testing.T, q store.Querier, table string) int {
	t.Helper()
	var n int
	if err := q.QueryRowContext(context.Background(), `SELECT count(*) FROM "`+table+`"`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func str(t *testing.T, q store.Querier, query string, args ...any) string {
	t.Helper()
	var s sql.NullString
	if err := q.QueryRowContext(context.Background(), query, args...).Scan(&s); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	if !s.Valid {
		return "<NULL>"
	}
	return s.String
}

var fixed = time.Date(2026, 9, 28, 1, 2, 3, 0, time.UTC)

func run(t *testing.T, cfg *config.Config, o Options) (*Result, error) {
	t.Helper()
	o.Now = func() time.Time { return fixed }
	return Import(context.Background(), cfg, o)
}

// 全新数据目录（首次启动已自动导入内置词书、还没有账号）→ 导入：内置词书被替换，结果与源库完全一致。
func TestImportFreshReplacesAutoSeededBooks(t *testing.T) {
	src := newSource(t)
	cfg := newTarget(t)
	{
		db := openDB(t, cfg)
		if err := seed.FirstRunBooks(context.Background(), db, time.Now()); err != nil {
			t.Fatal(err)
		}
		if count(t, db, "Book") == 0 {
			t.Fatal("内置词书没有导入")
		}
		db.Close()
	}
	res, err := run(t, cfg, Options{SourceURL: src.url, SettingsSecret: oldSecret})
	if err != nil {
		t.Fatal(err)
	}
	if res.BackupPath != "" {
		t.Errorf("空库不应备份，得到 %s", res.BackupPath)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("warnings: %v", res.Warnings)
	}
	want := map[string]int64{"User": 2, "Book": 1, "Unit": 1, "Word": 3, "UnitWord": 3, "Classroom": 1, "ClassMember": 1, "Plan": 2, "PlanUnit": 1, "PlanTarget": 2, "MemoryState": 2, "WordSheet": 1, "StudySession": 3, "Answer": 2, "Passage": 2, "ReviewLog": 1, "AppSetting": 2}
	if len(res.Counts) != len(Tables) {
		t.Fatalf("counts: %d tables", len(res.Counts))
	}
	for _, c := range res.Counts {
		if !c.OK() || c.Source != want[c.Table] {
			t.Errorf("%s: %+v want source %d", c.Table, c, want[c.Table])
		}
	}
	db := openDB(t, cfg)
	if n := count(t, db, "Book"); n != 1 {
		t.Errorf("Book 应只有源库的 1 本，得到 %d", n)
	}
	if n := count(t, db, "AppSetting"); n != 3 {
		t.Errorf("AppSetting 应为 2 + edition，得到 %d", n)
	}
	checks := []struct{ query, want string }{
		{`SELECT "createdAt" FROM "User" WHERE id='u_admin'`, "2026-09-01T08:00:00.123Z"},
		{`SELECT "updatedAt" FROM "User" WHERE id='u_stu'`, "2026-09-03T10:00:00.999Z"},
		{`SELECT "createdById" FROM "User" WHERE id='u_stu'`, "u_admin"},
		{`SELECT "currentGrade" FROM "User" WHERE id='u_admin'`, "<NULL>"},
		{`SELECT "passwordHash" FROM "User" WHERE id='u_stu'`, "$2b$12$abcdefghijklmnopqrstuuJ4v0y3b5x3tq9wX1e9m1Yl8m6oW7Qx2"},
		{`SELECT typeof("isSystem") || ':' || "isSystem" FROM "Book"`, "integer:1"},
		{`SELECT "archived" FROM "Classroom"`, "0"},
		{`SELECT "exampleAt" FROM "Word" WHERE id='w1'`, "2026-09-05T01:02:03.456Z"},
		{`SELECT "exampleAt" FROM "Word" WHERE id='w2'`, "<NULL>"},
		{`SELECT "modes" FROM "Plan" WHERE id='p1'`, `["recognition","spelling"]`},
		{`SELECT "modes" FROM "Plan" WHERE id='p2'`, `[]`},
		{`SELECT "wordIds" FROM "WordSheet"`, `["w1","w3"]`},
		{`SELECT "classId" || '|' || coalesce("userId",'null') FROM "PlanTarget" WHERE id='pt1'`, "c1|null"},
		{`SELECT "lastReview" FROM "MemoryState" WHERE id='m2'`, "<NULL>"},
		{`SELECT typeof("stability") || ':' || "stability" FROM "MemoryState" WHERE id='m2'`, "real:0.0"},
		{`SELECT "stability" FROM "MemoryState" WHERE id='m1'`, "3.17295633"},
		{`SELECT "progress" FROM "StudySession" WHERE id='s1'`, "<NULL>"},
		{`SELECT "progress" FROM "StudySession" WHERE id='s2'`, `{"index":0,"phase":"test"}`},
		{`SELECT "result" FROM "StudySession" WHERE id='s1'`, `{"total":2,"correct":2}`},
		{`SELECT "correct" || "hintUsed" || "dontKnow" FROM "Answer" WHERE id='a2'`, "011"},
		{`SELECT "questions" FROM "Passage" WHERE id='pa1'`, "<NULL>"},
		{`SELECT "questions" FROM "Passage" WHERE id='pa2'`, `[{"q":"What?","answer":0,"options":["a","b"]}]`},
		{`SELECT "body" FROM "Passage" WHERE id='pa2'`, `I like "apples".`},
		{`SELECT "wordIds" FROM "Passage" WHERE id='pa2'`, `[]`},
		{`SELECT "dueAfter" FROM "ReviewLog"`, "2026-09-10T16:00:00.000Z"},
		{`SELECT "value" FROM "AppSetting" WHERE key='ai.promptTemplates'`, `{"example":"自定义 <要求> & 说明"}`},
		{`SELECT "value" FROM "AppSetting" WHERE key='edition'`, `"school"`},
		{`SELECT "updatedById" FROM "AppSetting" WHERE key='edition'`, "<NULL>"},
		{`SELECT group_concat("wordId") FROM (SELECT "wordId" FROM "UnitWord" ORDER BY rowid)`, "w3,w1,w2"},
	}
	for _, c := range checks {
		if got := str(t, db, c.query); got != c.want {
			t.Errorf("%s\n got  %q\n want %q", c.query, got, c.want)
		}
	}
	// JSON 原样（jsonb 的键顺序、数字写法），只去空白
	var snap string
	db.QueryRow(`SELECT "snapshot" FROM "StudySession" WHERE id='s1'`).Scan(&snap)
	if strings.Contains(snap, ": ") || !json.Valid([]byte(snap)) || !strings.Contains(snap, `"n":1.5e-7`) && !strings.Contains(snap, `"n":0.00000015`) {
		t.Errorf("snapshot: %s", snap)
	}
	// AI Key：用新密钥能解开，库里不再有旧密文，旧密钥不落盘
	var raw string
	db.QueryRow(`SELECT "value" FROM "AppSetting" WHERE key='ai'`).Scan(&raw)
	var ai map[string]any
	if err := json.Unmarshal([]byte(raw), &ai); err != nil {
		t.Fatal(err)
	}
	enc, _ := ai["apiKeyEnc"].(string)
	if enc == oldBox || enc == "" {
		t.Fatalf("apiKeyEnc 未重新加密: %q", enc)
	}
	if plain, ok := secretbox.Decrypt(enc, cfg.SettingsSecret); !ok || plain != oldPlain {
		t.Fatalf("新密钥解密失败: %q %v", plain, ok)
	}
	if ai["baseUrl"] != "http://llm.local/v1?a=1&b=2" || ai["model"] != "m" || ai["timeoutMs"] != float64(60000) || ai["provider"] != "openai" {
		t.Errorf("ai 其余字段: %v", ai)
	}
	if strings.Contains(raw, `\u00`) || !strings.Contains(raw, "a=1&b=2") {
		t.Errorf("不应转义 &: %s", raw)
	}
	db.Close()
	assertNoSecretOnDisk(t, cfg.DataDir)
}

func assertNoSecretOnDisk(t *testing.T, dir string) {
	t.Helper()
	filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		b, _ := os.ReadFile(p)
		if bytes.Contains(b, []byte(oldSecret)) {
			t.Errorf("旧密钥出现在 %s", p)
		}
		return nil
	})
}

// 已有账号的库：不加 --force 拒绝且不改动；加 --force 先备份再覆盖。
func TestImportTwiceNeedsForceAndBacksUp(t *testing.T) {
	src := newSource(t)
	cfg := newTarget(t)
	if _, err := run(t, cfg, Options{SourceURL: src.url, SettingsSecret: oldSecret}); err != nil {
		t.Fatal(err)
	}
	// 目标库改一点，便于确认没有被覆盖 / 备份里是改动后的内容
	db := openDB(t, cfg)
	if _, err := db.Exec(`UPDATE "User" SET "name"='改过' WHERE id='u_stu'`); err != nil {
		t.Fatal(err)
	}
	_, err := run(t, cfg, Options{SourceURL: src.url})
	if !errors.Is(err, ErrTargetNotEmpty) {
		t.Fatalf("第二次导入应拒绝，得到 %v", err)
	}
	if got := str(t, db, `SELECT "name" FROM "User" WHERE id='u_stu'`); got != "改过" {
		t.Errorf("拒绝后数据被改动: %q", got)
	}
	matches, _ := filepath.Glob(cfg.DBPath + ".before-import-*")
	if len(matches) != 0 {
		t.Errorf("拒绝时不应备份: %v", matches)
	}

	res, err := run(t, cfg, Options{SourceURL: src.url, SettingsSecret: oldSecret, Force: true, Edition: "personal"})
	if err != nil {
		t.Fatal(err)
	}
	if res.BackupPath == "" {
		t.Fatal("--force 应先备份")
	}
	if got := str(t, db, `SELECT "name" FROM "User" WHERE id='u_stu'`); got != "小明" {
		t.Errorf("覆盖后 name = %q", got)
	}
	if got := str(t, db, `SELECT "value" FROM "AppSetting" WHERE key='edition'`); got != `"personal"` {
		t.Errorf("edition = %s", got)
	}
	bak, err := sql.Open("sqlite", "file:"+res.BackupPath+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer bak.Close()
	if got := str(t, bak, `SELECT "name" FROM "User" WHERE id='u_stu'`); got != "改过" {
		t.Errorf("备份内容 name = %q", got)
	}
}

// 源库连接串错误 / 连不上：干净地失败，不创建目标库。
func TestImportBrokenSource(t *testing.T) {
	cfg := newTarget(t)
	for _, u := range []string{"not a url ::: %%%", "postgresql://nobody:secretpw@127.0.0.1:1/none?connect_timeout=2"} {
		_, err := run(t, cfg, Options{SourceURL: u})
		if err == nil {
			t.Fatalf("%q 应失败", u)
		}
		if strings.Contains(err.Error(), "secretpw") {
			t.Errorf("错误信息泄露密码: %v", err)
		}
	}
	if _, err := os.Stat(cfg.DBPath); !os.IsNotExist(err) {
		t.Errorf("源库连不上时不应创建目标库: %v", err)
	}
	if RedactURL("postgresql://u:secretpw@h:5432/db") != "postgresql://u:***@h:5432/db" {
		t.Errorf("RedactURL: %s", RedactURL("postgresql://u:secretpw@h:5432/db"))
	}
}

// 中途失败：整个事务回滚，目标库保持原状（含 --force 覆盖时）。
func TestImportMidFailureRollsBack(t *testing.T) {
	src := newSource(t)
	cfg := newTarget(t)
	if _, err := run(t, cfg, Options{SourceURL: src.url, SettingsSecret: oldSecret}); err != nil {
		t.Fatal(err)
	}
	db := openDB(t, cfg)
	db.Exec(`UPDATE "User" SET "name"='改过' WHERE id='u_stu'`)
	before := map[string]int{}
	for _, tb := range Tables {
		before[tb] = count(t, db, tb)
	}
	src.exec(t, `INSERT INTO "User" (id,email,"passwordHash",name,"updatedAt") VALUES ('u_new','new@x.test','h','新','2026-09-10 00:00:00')`)

	testHookAfterTable = func(table string, _ *sql.Tx) error {
		if table == "Answer" {
			return errors.New("模拟中途失败")
		}
		return nil
	}
	defer func() { testHookAfterTable = nil }()
	_, err := run(t, cfg, Options{SourceURL: src.url, SettingsSecret: oldSecret, Force: true})
	if err == nil || !strings.Contains(err.Error(), "模拟中途失败") || !strings.Contains(err.Error(), "回滚") {
		t.Fatalf("err = %v", err)
	}
	for _, tb := range Tables {
		if n := count(t, db, tb); n != before[tb] {
			t.Errorf("%s: %d → %d（应回滚）", tb, before[tb], n)
		}
	}
	if got := str(t, db, `SELECT "name" FROM "User" WHERE id='u_stu'`); got != "改过" {
		t.Errorf("回滚后 name = %q", got)
	}
}

// 没给旧密钥：其余照常导入，Key 置空并警告；给错密钥：失败并回滚。
func TestImportAISecretHandling(t *testing.T) {
	src := newSource(t)
	cfg := newTarget(t)
	res, err := run(t, cfg, Options{SourceURL: src.url})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "重新填写") {
		t.Errorf("warnings: %v", res.Warnings)
	}
	db := openDB(t, cfg)
	var ai map[string]any
	json.Unmarshal([]byte(str(t, db, `SELECT "value" FROM "AppSetting" WHERE key='ai'`)), &ai)
	if v, ok := ai["apiKeyEnc"]; !ok || v != nil {
		t.Errorf("apiKeyEnc 应为 null: %v", ai)
	}
	if ai["model"] != "m" {
		t.Errorf("其余字段应保留: %v", ai)
	}

	cfg2 := newTarget(t)
	_, err = run(t, cfg2, Options{SourceURL: src.url, SettingsSecret: "wrong-secret"})
	if err == nil || !strings.Contains(err.Error(), "解不开") {
		t.Fatalf("错误密钥应失败: %v", err)
	}
	if strings.Contains(err.Error(), "wrong-secret") {
		t.Errorf("错误信息不应回显密钥: %v", err)
	}
	db2 := openDB(t, cfg2)
	if n := count(t, db2, "User"); n != 0 {
		t.Errorf("失败后应回滚，User = %d", n)
	}
}

// 源库结构与本版本不一致（旧版本缺列 / 多出不认识的列）：拒绝导入。
func TestImportColumnMismatch(t *testing.T) {
	src := newSource(t)
	src.exec(t, `ALTER TABLE "User" ADD COLUMN "nickname" text`)
	cfg := newTarget(t)
	_, err := run(t, cfg, Options{SourceURL: src.url, SettingsSecret: oldSecret})
	if err == nil || !strings.Contains(err.Error(), "nickname") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "升级") {
		t.Errorf("不应提示升级旧版: %v", err)
	}
	src.exec(t, `ALTER TABLE "User" DROP COLUMN "nickname"; ALTER TABLE "Answer" DROP COLUMN "dayKey"`)
	_, err = run(t, cfg, Options{SourceURL: src.url, SettingsSecret: oldSecret})
	if err == nil || !strings.Contains(err.Error(), "缺少必填列 dayKey") {
		t.Fatalf("err = %v", err)
	}
	db := openDB(t, cfg)
	if n := count(t, db, "User"); n != 0 {
		t.Errorf("User = %d", n)
	}
}

// 源库连接是只读的：导入过程不可能写源库。
func TestSourceConnectionIsReadOnly(t *testing.T) {
	src := newSource(t)
	conn, err := connectSource(context.Background(), src.url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	if _, err := conn.Exec(context.Background(), `UPDATE "User" SET name = 'x'`); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("源库连接应只读: %v", err)
	}
}

func TestSameColumnsAndConvert(t *testing.T) {
	if err := sameColumns("T", []column{{name: "a"}, {name: "b"}}, []targetColumn{{name: "b"}, {name: "a"}}); err != nil {
		t.Error(err)
	}
	// 目标多出可空 / 有默认值的列：允许
	if err := sameColumns("T", []column{{name: "a"}}, []targetColumn{{name: "a"}, {name: "b"}}); err != nil {
		t.Error(err)
	}
	// 目标多出必填列：拒绝
	if err := sameColumns("T", []column{{name: "a"}}, []targetColumn{{name: "a"}, {name: "b", required: true}}); err == nil || !strings.Contains(err.Error(), "缺少必填列 b") {
		t.Error(err)
	}
	// 源库多出列：拒绝
	if err := sameColumns("T", []column{{name: "a"}, {name: "z"}}, []targetColumn{{name: "a"}}); err == nil || !strings.Contains(err.Error(), "不认识的列 z") {
		t.Error(err)
	}
	ts := time.Date(2026, 9, 1, 8, 0, 0, 123_456_789, time.UTC)
	if v, _ := convert(column{udt: "timestamp"}, ts); v != "2026-09-01T08:00:00.123Z" {
		t.Errorf("timestamp → %v", v)
	}
	if v, _ := convert(column{udt: "bool"}, true); v != int64(1) {
		t.Errorf("bool → %v", v)
	}
	if v, _ := convert(column{udt: "jsonb"}, `{"b": [1, 2], "a": {"x": null}}`); v != `{"b":[1,2],"a":{"x":null}}` {
		t.Errorf("jsonb → %v", v)
	}
	if _, err := convert(column{udt: "int4"}, "x"); err == nil {
		t.Error("类型不符应报错")
	}
	if _, err := selectExpr(column{name: "x", udt: "bytea"}); err == nil {
		t.Error("不支持的类型应报错")
	}
}

// 本版本新增、可空或有默认值的列：源库没有也能导入，取默认值（例如旧库没有 Answer.dontKnow）。
func TestImportTargetExtraDefaultedColumn(t *testing.T) {
	src := newSource(t)
	src.exec(t, `ALTER TABLE "Answer" DROP COLUMN "dontKnow"; ALTER TABLE "User" DROP COLUMN "currentGrade"`)
	cfg := newTarget(t)
	if _, err := run(t, cfg, Options{SourceURL: src.url, SettingsSecret: oldSecret}); err != nil {
		t.Fatal(err)
	}
	db := openDB(t, cfg)
	if got := str(t, db, `SELECT "dontKnow" FROM "Answer" WHERE id='a2'`); got != "0" {
		t.Errorf("dontKnow = %s", got)
	}
	if got := str(t, db, `SELECT "currentGrade" FROM "User" WHERE id='u_stu'`); got != "<NULL>" {
		t.Errorf("currentGrade = %s", got)
	}
}

// 行数核对不一致：回滚并返回 ErrMismatch（用测试钩子在事务里多插一行模拟）。
func TestImportCountMismatchRollsBack(t *testing.T) {
	src := newSource(t)
	cfg := newTarget(t)
	testHookAfterTable = func(table string, tx *sql.Tx) error {
		if table == "Passage" {
			_, err := tx.Exec(`INSERT INTO "Passage" ("id","userId","title","body","wordIds","createdAt") VALUES ('extra','u_stu','x','y','[]','2026-09-01T00:00:00.000Z')`)
			return err
		}
		return nil
	}
	defer func() { testHookAfterTable = nil }()
	res, err := run(t, cfg, Options{SourceURL: src.url, SettingsSecret: oldSecret})
	if !errors.Is(err, ErrMismatch) {
		t.Fatalf("err = %v", err)
	}
	bad := 0
	for _, c := range res.Counts {
		if !c.OK() {
			bad++
			if c.Table != "Passage" || c.Target != c.Source+1 {
				t.Errorf("%+v", c)
			}
		}
	}
	if bad != 1 {
		t.Errorf("不一致的表 = %d", bad)
	}
	db := openDB(t, cfg)
	if n := count(t, db, "User"); n != 0 {
		t.Errorf("应回滚，User = %d", n)
	}
}

// 特殊值：数组里的引号 / 反斜杠 / 中文 / NULL 元素；jsonb 'null'（Prisma JsonNull）与 SQL NULL 区分；AI 设置数字原样。
func TestImportSpecialValues(t *testing.T) {
	src := newSource(t)
	src.exec(t, `UPDATE "Passage" SET "wordIds" = ARRAY['a"b', 'c\d', '中文', NULL] WHERE id = 'pa1';
		UPDATE "Passage" SET "questions" = 'null'::jsonb WHERE id = 'pa2';
		UPDATE "AppSetting" SET value = jsonb_set(value, '{timeoutMs}', '60000.0') WHERE key = 'ai'`)
	cfg := newTarget(t)
	if _, err := run(t, cfg, Options{SourceURL: src.url, SettingsSecret: oldSecret}); err != nil {
		t.Fatal(err)
	}
	db := openDB(t, cfg)
	var ids []*string
	if err := json.Unmarshal([]byte(str(t, db, `SELECT "wordIds" FROM "Passage" WHERE id='pa1'`)), &ids); err != nil {
		t.Fatal(err)
	}
	if len(ids) != 4 || *ids[0] != `a"b` || *ids[1] != `c\d` || *ids[2] != "中文" || ids[3] != nil {
		t.Errorf("wordIds = %v", str(t, db, `SELECT "wordIds" FROM "Passage" WHERE id='pa1'`))
	}
	if got := str(t, db, `SELECT "questions" FROM "Passage" WHERE id='pa2'`); got != "null" {
		t.Errorf("jsonb null 应为文本 null，得到 %q", got)
	}
	if got := str(t, db, `SELECT "questions" FROM "Passage" WHERE id='pa1'`); got != "<NULL>" {
		t.Errorf("SQL NULL 应保持 NULL，得到 %q", got)
	}
	if got := str(t, db, `SELECT "value" FROM "AppSetting" WHERE key='ai'`); !strings.Contains(got, `"timeoutMs":60000.0`) {
		t.Errorf("数字写法应原样: %s", got)
	}
}

// Prisma 风格的 ?schema= 参数：去掉并改设 search_path。
func TestImportPrismaSchemaParam(t *testing.T) {
	src := newSource(t)
	u, _ := url.Parse(src.url)
	q := u.Query()
	q.Set("schema", "public")
	u.RawQuery = q.Encode()
	cfg := newTarget(t)
	if _, err := run(t, cfg, Options{SourceURL: u.String(), SettingsSecret: oldSecret}); err != nil {
		t.Fatal(err)
	}
}

// 导入时的 SETTINGS_SECRET 环境变量不参与重新加密：按数据目录保存的密钥加密，之后 serve（不设该变量）一定能解开。
func TestImportSecretResolutionMatchesServe(t *testing.T) {
	src := newSource(t)
	dir := t.TempDir()
	env := map[string]string{"SETTINGS_SECRET": oldSecret} // 典型误操作：把旧密钥 export 成了 SETTINGS_SECRET
	lookup := func(k string) (string, bool) { v, ok := env[k]; return v, ok }
	cfg, info, err := LoadTargetConfig(config.Options{DataDir: dir, Lookup: lookup})
	if err != nil {
		t.Fatal(err)
	}
	if !info.EnvIgnored || info.Source != "secret.key" || cfg.SettingsSecret == oldSecret {
		t.Fatalf("info = %+v secretIsOld=%v", info, cfg.SettingsSecret == oldSecret)
	}
	if _, err := run(t, cfg, Options{SourceURL: src.url, SettingsSecret: oldSecret}); err != nil {
		t.Fatal(err)
	}
	serveCfg, err := config.Load(config.Options{DataDir: dir, Lookup: func(string) (string, bool) { return "", false }})
	if err != nil {
		t.Fatal(err)
	}
	db := openDB(t, serveCfg)
	var ai map[string]any
	json.Unmarshal([]byte(str(t, db, `SELECT "value" FROM "AppSetting" WHERE key='ai'`)), &ai)
	if plain, ok := secretbox.Decrypt(ai["apiKeyEnc"].(string), serveCfg.SettingsSecret); !ok || plain != oldPlain {
		t.Fatalf("serve 的密钥解不开: %q %v", plain, ok)
	}

	// config.toml 里写了 SETTINGS_SECRET：以它为准（serve 同样读它）
	dir2 := t.TempDir()
	os.WriteFile(filepath.Join(dir2, "config.toml"), []byte("SETTINGS_SECRET = \"from-toml\"\n"), 0o600)
	cfg2, info2, err := LoadTargetConfig(config.Options{DataDir: dir2, Lookup: func(string) (string, bool) { return "", false }})
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.SettingsSecret != "from-toml" || info2.Source != "config.toml" || info2.EnvIgnored {
		t.Errorf("toml: %q %+v", cfg2.SettingsSecret, info2)
	}
}
