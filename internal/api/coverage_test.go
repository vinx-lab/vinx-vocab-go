package api

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"
)

// 目标词书与覆盖进度（spec 0003）：班级目标并集、退班回落到个人目标、没有目标、权限、覆盖口径、单词单「目标」来源。

type covEnv struct {
	*testEnv
	t                *testing.T
	admin, t1, t2    map[string]string
	s1, s2, s3       map[string]string
	s1ID, s2ID, s3ID string
	t1ID, t2ID       string
}

func (c *covEnv) exec(q string, args ...any) {
	c.t.Helper()
	if _, err := c.d.DB.Exec(q, args...); err != nil {
		c.t.Fatalf("%s: %v", q, err)
	}
}

func signupWithRole(t *testing.T, e *testEnv, email, role string) (map[string]string, string) {
	t.Helper()
	_, id := studentCookie(t, e, email)
	if role != "" {
		if _, err := e.d.DB.Exec(`UPDATE "User" SET "role" = ? WHERE "id" = ?`, role, id); err != nil {
			t.Fatal(err)
		}
	}
	r := e.do("POST", "/api/auth/login", `{"email":"`+email+`","password":"123456"}`, nil)
	if r.Status != 200 {
		t.Fatalf("login = %d %v", r.Status, r.Body)
	}
	return map[string]string{"Cookie": cookieOf(t, r)}, id
}

// newCovEnv 班级版：admin、老师 t1 / t2、学生 s1（c1 + c2）、s2（c1）、s3（没有班级）。
// 词书：b1（w1–w4）、b2（w4、w5，与 b1 重叠 w4）、b3（w6），都是系统词书；bt2 是 t2 的私有词书（t1 不可见）。
func newCovEnv(t *testing.T, env map[string]string) *covEnv {
	e := newEnv(t, env)
	e.now = time.Date(2026, 10, 6, 4, 0, 0, 0, time.UTC) // 晚于造出的作答记录（令牌按这个时钟签发）
	c := &covEnv{testEnv: e, t: t}
	c.admin, _ = signupWithRole(t, e, "admin@x.test", "")
	c.t1, c.t1ID = signupWithRole(t, e, "t1@x.test", "teacher")
	c.t2, c.t2ID = signupWithRole(t, e, "t2@x.test", "teacher")
	c.s1, c.s1ID = signupWithRole(t, e, "s1@x.test", "")
	c.s2, c.s2ID = signupWithRole(t, e, "s2@x.test", "")
	c.s3, c.s3ID = signupWithRole(t, e, "s3@x.test", "")
	c.exec(`INSERT INTO "Book" ("id","name","isSystem","sortOrder") VALUES ('b1','七上',1,1),('b2','中考',1,2),('b3','九上',1,3)`)
	c.exec(`INSERT INTO "Book" ("id","name","isSystem","ownerId") VALUES ('bt2','t2 的书',0,?)`, c.t2ID)
	c.exec(`INSERT INTO "Unit" ("id","bookId","name","sortOrder") VALUES ('u1a','b1','U1',0),('u1b','b1','U2',1),('u2','b2','U1',0),('u3','b3','U1',0),('ut2','bt2','U1',0)`)
	for i := 1; i <= 7; i++ {
		c.exec(`INSERT INTO "Word" ("id","spelling","definition") VALUES (?,?,?)`, fmt.Sprintf("w%d", i), fmt.Sprintf("word%c", 'a'+i), fmt.Sprintf("义%d", i))
	}
	c.exec(`INSERT INTO "UnitWord" ("unitId","wordId","sortOrder") VALUES ('u1a','w1',0),('u1a','w2',1),('u1b','w3',0),('u1b','w4',1),('u2','w4',0),('u2','w5',1),('u3','w6',0),('ut2','w7',0)`)
	c.exec(`INSERT INTO "Classroom" ("id","name","teacherId","inviteCode") VALUES ('c1','一班',?,'AAAAAA'),('c2','二班',?,'BBBBBB')`, c.t1ID, c.t2ID)
	c.exec(`INSERT INTO "ClassMember" ("classId","userId","joinedAt") VALUES ('c1',?,'2026-09-01T00:00:00.000Z'),('c2',?,'2026-09-02T00:00:00.000Z'),('c1',?,'2026-09-03T00:00:00.000Z')`, c.s1ID, c.s1ID, c.s2ID)
	return c
}

func bookIDsOf(t *testing.T, v any) []string {
	t.Helper()
	out := []string{}
	for _, b := range v.([]any) {
		out = append(out, b.(map[string]any)["id"].(string))
	}
	return out
}

func TestTargetBooksSettingAndSource(t *testing.T) {
	c := newCovEnv(t, nil)

	// 有班级但班级还没设目标：source = class，books 为空；没有班级：none
	if d := dataOf(c.do("GET", "/api/me/target-books", "", c.s1)); d["source"] != "class" || len(d["books"].([]any)) != 0 || len(d["classes"].([]any)) != 2 {
		t.Fatalf("s1 初始 = %v", d)
	}
	if d := dataOf(c.do("GET", "/api/me/target-books", "", c.s3)); d["source"] != "none" || len(d["books"].([]any)) != 0 {
		t.Fatalf("s3 初始 = %v", d)
	}

	// 老师设置班级目标（顺序即 sortOrder，重复去掉）
	r := c.do("PUT", "/api/classes/c1/target-books", `{"bookIds":["b2","b1","b2"]}`, c.t1)
	if r.Status != 200 || !reflect.DeepEqual(bookIDsOf(t, dataOf(r)["items"]), []string{"b2", "b1"}) {
		t.Fatalf("PUT c1 = %d %v", r.Status, r.Body)
	}
	if got := bookIDsOf(t, dataOf(c.do("GET", "/api/classes/c1/target-books", "", c.t1))["items"]); !reflect.DeepEqual(got, []string{"b2", "b1"}) {
		t.Fatalf("GET c1 = %v", got)
	}
	// 管理员可看可改
	if r := c.do("GET", "/api/classes/c1/target-books", "", c.admin); r.Status != 200 {
		t.Fatalf("admin GET = %d", r.Status)
	}
	// 别班老师 403，学生 403，不存在的班级 404
	if r := c.do("GET", "/api/classes/c1/target-books", "", c.t2); r.Status != 403 {
		t.Fatalf("t2 GET c1 = %d", r.Status)
	}
	if r := c.do("PUT", "/api/classes/c1/target-books", `{"bookIds":["b3"]}`, c.t2); r.Status != 403 {
		t.Fatalf("t2 PUT c1 = %d", r.Status)
	}
	if r := c.do("PUT", "/api/classes/c1/target-books", `{"bookIds":["b3"]}`, c.s1); r.Status != 403 {
		t.Fatalf("学生 PUT = %d", r.Status)
	}
	if r := c.do("GET", "/api/classes/nope/target-books", "", c.t1); r.Status != 404 {
		t.Fatalf("不存在的班级 = %d", r.Status)
	}
	// 不可见的词书 / 不存在的词书 → 400；参数校验
	if r := c.do("PUT", "/api/classes/c1/target-books", `{"bookIds":["bt2"]}`, c.t1); r.Status != 400 || errCode(r) != "VALIDATION 有词书不存在或不可见" {
		t.Fatalf("不可见词书 = %d %s", r.Status, errCode(r))
	}
	if r := c.do("PUT", "/api/classes/c1/target-books", `{"bookIds":["nope"]}`, c.t1); r.Status != 400 {
		t.Fatalf("不存在的词书 = %d", r.Status)
	}
	if r := c.do("PUT", "/api/classes/c1/target-books", `{}`, c.t1); r.Status != 400 || errCode(r) != "VALIDATION 参数校验失败" {
		t.Fatalf("缺 bookIds = %d %s", r.Status, errCode(r))
	}
	// 失败的 PUT 不改原有目标
	if got := bookIDsOf(t, dataOf(c.do("GET", "/api/classes/c1/target-books", "", c.t1))["items"]); !reflect.DeepEqual(got, []string{"b2", "b1"}) {
		t.Fatalf("失败后 c1 = %v", got)
	}
	if r := c.do("PUT", "/api/classes/c2/target-books", `{"bookIds":["b3","bt2"]}`, c.t2); r.Status != 200 {
		t.Fatalf("t2 PUT c2 = %d %v", r.Status, r.Body)
	}

	// 学生在两个班：并集（按入班顺序，去重）
	d := dataOf(c.do("GET", "/api/me/target-books", "", c.s1))
	if d["source"] != "class" || !reflect.DeepEqual(bookIDsOf(t, d["books"]), []string{"b2", "b1", "b3", "bt2"}) {
		t.Fatalf("s1 并集 = %v", d)
	}
	// 有班级时仍可保存自己的目标，但 source 仍是 class
	d = dataOf(c.do("PUT", "/api/me/target-books", `{"bookIds":["b1"]}`, c.s1))
	if d["source"] != "class" || !reflect.DeepEqual(bookIDsOf(t, d["books"]), []string{"b2", "b1", "b3", "bt2"}) {
		t.Fatalf("s1 PUT = %v", d)
	}
	// 自己设：只能选自己可见的词书
	if r := c.do("PUT", "/api/me/target-books", `{"bookIds":["bt2"]}`, c.s3); r.Status != 400 {
		t.Fatalf("s3 不可见 = %d", r.Status)
	}
	d = dataOf(c.do("PUT", "/api/me/target-books", `{"bookIds":["b3","b1"]}`, c.s3))
	if d["source"] != "own" || !reflect.DeepEqual(bookIDsOf(t, d["books"]), []string{"b3", "b1"}) {
		t.Fatalf("s3 PUT = %v", d)
	}
	// 老师自己学习时也可以设
	if d := dataOf(c.do("PUT", "/api/me/target-books", `{"bookIds":["b1"]}`, c.t1)); d["source"] != "own" {
		t.Fatalf("t1 自己 = %v", d)
	}

	// 退班：先剩一个班，再一个不剩 → 回落到自己的目标
	if r := c.do("DELETE", "/api/me/classes/c2", "", c.s1); r.Status != 200 {
		t.Fatalf("退 c2 = %d", r.Status)
	}
	if d := dataOf(c.do("GET", "/api/me/target-books", "", c.s1)); !reflect.DeepEqual(bookIDsOf(t, d["books"]), []string{"b2", "b1"}) {
		t.Fatalf("退 c2 后 = %v", d)
	}
	c.do("DELETE", "/api/me/classes/c1", "", c.s1)
	if d := dataOf(c.do("GET", "/api/me/target-books", "", c.s1)); d["source"] != "own" || !reflect.DeepEqual(bookIDsOf(t, d["books"]), []string{"b1"}) {
		t.Fatalf("退完所有班 = %v", d)
	}
	// 空数组清空
	if d := dataOf(c.do("PUT", "/api/me/target-books", `{"bookIds":[]}`, c.s1)); d["source"] != "none" {
		t.Fatalf("清空 = %v", d)
	}
	// 词书删除：从目标里级联移除
	c.exec(`DELETE FROM "Book" WHERE "id" = 'b3'`)
	if d := dataOf(c.do("GET", "/api/me/target-books", "", c.s3)); !reflect.DeepEqual(bookIDsOf(t, d["books"]), []string{"b1"}) {
		t.Fatalf("删书后 = %v", d)
	}
}

// seedCoverageAnswers 给 s2 造正式测试记录（c1 目标 = b2、b1，目标词 w4 w5 w1 w2 w3）：
//   - T1（计划 P1，10-01）：w1 对、w2 错、w4 认义对 + 拼写错（算错）
//   - T2（计划 P1，10-02，重测）：w2 对、w4 对 → 不计
//   - T3 / T4（计划已删除，10-03 / 10-04）：w3 错 → 对，各自算正式
//   - 进行中的检测 w5 对、新学组 w5 对 → 不计
//   - w2 的记忆在 10-05 达到已掌握 → 会了
//
// 结果：w1 w2 w3 会了，w4 要学，w5 未测。
func seedCoverageAnswers(c *covEnv) {
	s := c.s2ID
	c.exec(`INSERT INTO "Plan" ("id","name","creatorId","kind") VALUES ('P1','测一',?,'test')`, s)
	sess := func(id string, plan any, kind, status, completed string) {
		var comp any
		if completed != "" {
			comp = completed
		}
		c.exec(`INSERT INTO "StudySession" ("id","userId","planId","kind","status","dayKey","snapshot","startedAt","completedAt","completedDay")
			VALUES (?,?,?,?,?,?,?,?,?,?)`, id, s, plan, kind, status, "2026-10-01", `{"modes":["spelling"],"items":[]}`, "2026-10-01T00:00:00.000Z", comp, nil)
	}
	n := 0
	ans := func(session, word, mode, phase string, correct bool, at string) {
		n++
		c.exec(`INSERT INTO "Answer" ("id","sessionId","userId","wordId","mode","phase","attempt","correct","dayKey","createdAt") VALUES (?,?,?,?,?,?,1,?,?,?)`,
			fmt.Sprintf("a%d", n), session, s, word, mode, phase, correct, "2026-10-01", at)
	}
	sess("T1", "P1", "test", "completed", "2026-10-01T02:00:00.000Z")
	ans("T1", "w1", "spelling", "test", true, "2026-10-01T01:00:00.000Z")
	ans("T1", "w2", "spelling", "test", false, "2026-10-01T01:00:00.000Z")
	ans("T1", "w4", "recognition", "test", true, "2026-10-01T01:00:00.000Z")
	ans("T1", "w4", "spelling", "test", false, "2026-10-01T01:00:00.000Z")
	sess("T2", "P1", "test", "completed", "2026-10-02T02:00:00.000Z")
	ans("T2", "w2", "spelling", "test", true, "2026-10-02T01:00:00.000Z")
	ans("T2", "w4", "spelling", "test", true, "2026-10-02T01:00:00.000Z")
	sess("T3", nil, "test", "completed", "2026-10-03T02:00:00.000Z")
	ans("T3", "w3", "spelling", "test", false, "2026-10-03T01:00:00.000Z")
	sess("T4", nil, "test", "completed", "2026-10-04T02:00:00.000Z")
	ans("T4", "w3", "spelling", "test", true, "2026-10-04T01:00:00.000Z")
	sess("TA", nil, "test", "active", "")
	ans("TA", "w5", "spelling", "test", true, "2026-10-05T01:00:00.000Z")
	sess("L1", nil, "learn", "completed", "2026-10-05T02:00:00.000Z")
	ans("L1", "w5", "spelling", "practice", true, "2026-10-05T01:00:00.000Z")
	c.exec(`INSERT INTO "MemoryState" ("id","userId","wordId","due","stability","difficulty","lastReview","introducedDay") VALUES ('m1',?,'w2','2026-11-01T00:00:00.000Z',30,5,'2026-10-05T00:00:00.000Z','2026-09-30')`, s)
}

func countsOf(v any) map[string]float64 {
	m := v.(map[string]any)
	out := map[string]float64{}
	for _, k := range []string{"target", "tested", "known", "learning", "untested"} {
		if x, ok := m[k]; ok {
			out[k] = x.(float64)
		}
	}
	return out
}

func TestCoverageRecordsAndOverview(t *testing.T) {
	c := newCovEnv(t, nil)
	seedCoverageAnswers(c)

	// 班级还没有目标：概览 coverage 为 null；覆盖进度 source=class、全 0
	ovr := c.do("GET", "/api/classes/c1/overview", "", c.t1)
	if ovr.Status != 200 {
		t.Fatalf("overview = %d %v", ovr.Status, ovr.Body)
	}
	ov := dataOf(ovr)
	for _, s := range ov["students"].([]any) {
		if cov, ok := s.(map[string]any)["coverage"]; !ok || cov != nil {
			t.Fatalf("没有目标时 coverage 应为 null：%v", s)
		}
	}
	d := dataOf(c.do("GET", "/api/records/coverage", "", c.s2))
	if d["source"] != "class" || len(d["books"].([]any)) != 0 || countsOf(d["total"])["target"] != 0 {
		t.Fatalf("没有目标 = %v", d)
	}
	if d := dataOf(c.do("GET", "/api/records/coverage", "", c.s3)); d["source"] != "none" {
		t.Fatalf("s3 = %v", d)
	}

	c.do("PUT", "/api/classes/c1/target-books", `{"bookIds":["b2","b1"]}`, c.t1)
	want := map[string]float64{"target": 5, "tested": 4, "known": 3, "learning": 1, "untested": 1}
	for name, h := range map[string]map[string]string{"本人": c.s2, "班主任": c.t1, "管理员": c.admin} {
		q := "/api/records/coverage?userId=" + c.s2ID
		r := c.do("GET", q, "", h)
		if r.Status != 200 {
			t.Fatalf("%s = %d %v", name, r.Status, r.Body)
		}
		d := dataOf(r)
		if got := countsOf(d["total"]); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s total = %v", name, got)
		}
		books := d["books"].([]any)
		b2, b1 := books[0].(map[string]any), books[1].(map[string]any)
		if b2["bookId"] != "b2" || b2["name"] != "中考" || !reflect.DeepEqual(countsOf(b2), map[string]float64{"target": 2, "tested": 1, "known": 0, "learning": 1, "untested": 1}) {
			t.Fatalf("%s b2 = %v", name, b2)
		}
		if b1["bookId"] != "b1" || !reflect.DeepEqual(countsOf(b1), map[string]float64{"target": 4, "tested": 4, "known": 3, "learning": 1, "untested": 0}) {
			t.Fatalf("%s b1 = %v", name, b1)
		}
	}
	// 别班老师 403、其他学生 403
	if r := c.do("GET", "/api/records/coverage?userId="+c.s2ID, "", c.t2); r.Status != 403 {
		t.Fatalf("别班老师 = %d", r.Status)
	}
	if r := c.do("GET", "/api/records/coverage/words?userId="+c.s2ID, "", c.t2); r.Status != 403 {
		t.Fatalf("别班老师 words = %d", r.Status)
	}
	if r := c.do("GET", "/api/records/coverage?userId="+c.s2ID, "", c.s3); r.Status != 403 {
		t.Fatalf("其他学生 = %d", r.Status)
	}

	// 词表
	words := func(q string) []string {
		t.Helper()
		r := c.do("GET", "/api/records/coverage/words?userId="+c.s2ID+q, "", c.t1)
		if r.Status != 200 {
			t.Fatalf("words%s = %d %v", q, r.Status, r.Body)
		}
		out := []string{}
		for _, it := range dataOf(r)["items"].([]any) {
			m := it.(map[string]any)
			out = append(out, m["wordId"].(string)+":"+m["status"].(string))
		}
		return out
	}
	if got := words(""); !reflect.DeepEqual(got, []string{"w4:learning", "w5:untested", "w1:known", "w2:known", "w3:known"}) {
		t.Fatalf("全部 = %v", got)
	}
	if got := words("&status=learning"); !reflect.DeepEqual(got, []string{"w4:learning"}) {
		t.Fatalf("要学 = %v", got)
	}
	if got := words("&status=untested&bookId=b2"); !reflect.DeepEqual(got, []string{"w5:untested"}) {
		t.Fatalf("b2 未测 = %v", got)
	}
	if got := words("&status=known&bookId=b1&limit=2&page=2"); !reflect.DeepEqual(got, []string{"w3:known"}) {
		t.Fatalf("分页 = %v", got)
	}
	r := c.do("GET", "/api/records/coverage/words?status=known&bookId=b1&limit=2", "", c.s2)
	if dd := dataOf(r); dd["total"] != float64(3) || dd["page"] != float64(1) || dd["limit"] != float64(2) {
		t.Fatalf("分页信息 = %v", dd)
	}
	if r := c.do("GET", "/api/records/coverage/words?bookId=b3", "", c.s2); r.Status != 404 {
		t.Fatalf("不在目标里的书 = %d", r.Status)
	}
	if r := c.do("GET", "/api/records/coverage/words?status=bad", "", c.s2); r.Status != 400 {
		t.Fatalf("status 非法 = %d", r.Status)
	}

	// 概览：每个学生按自己的有效目标（s1 = c1 ∪ c2）
	c.do("PUT", "/api/classes/c2/target-books", `{"bookIds":["b3"]}`, c.t2)
	ov = dataOf(c.do("GET", "/api/classes/c1/overview", "", c.t1))
	got := map[string]any{}
	for _, s := range ov["students"].([]any) {
		m := s.(map[string]any)
		got[m["userId"].(string)] = m["coverage"]
	}
	if !reflect.DeepEqual(got[c.s2ID], map[string]any{"target": float64(5), "tested": float64(4), "learning": float64(1)}) {
		t.Fatalf("概览 s2 = %v", got[c.s2ID])
	}
	if !reflect.DeepEqual(got[c.s1ID], map[string]any{"target": float64(6), "tested": float64(0), "learning": float64(0)}) {
		t.Fatalf("概览 s1 = %v", got[c.s1ID])
	}
}

func TestSheetTargetSources(t *testing.T) {
	c := newCovEnv(t, nil)
	seedCoverageAnswers(c)
	c.do("PUT", "/api/classes/c1/target-books", `{"bookIds":["b2","b1"]}`, c.t1)

	preview := func(h map[string]string, body string) resp {
		return c.do("POST", "/api/sheets/preview", body, h)
	}
	ids := func(r resp) []string {
		t.Helper()
		if r.Status != 200 {
			t.Fatalf("preview = %d %v", r.Status, r.Body)
		}
		out := []string{}
		for _, it := range dataOf(r)["items"].([]any) {
			out = append(out, it.(map[string]any)["wordId"].(string))
		}
		return out
	}
	if got := ids(preview(c.s2, `{"count":10,"source":{"kind":"target","status":"learning"}}`)); !reflect.DeepEqual(got, []string{"w4"}) {
		t.Fatalf("要学 = %v", got)
	}
	if got := ids(preview(c.s2, `{"count":10,"source":{"kind":"target","status":"untested"}}`)); !reflect.DeepEqual(got, []string{"w5"}) {
		t.Fatalf("未测 = %v", got)
	}
	if got := ids(preview(c.s2, `{"count":10,"source":{"kind":"target","status":"untested","bookId":"b1"}}`)); len(got) != 0 {
		t.Fatalf("b1 未测 = %v", got)
	}
	// 老师给本班学生出
	if got := ids(preview(c.t1, `{"userId":"`+c.s2ID+`","count":10,"source":{"kind":"target","status":"learning","bookId":"b1"}}`)); !reflect.DeepEqual(got, []string{"w4"}) {
		t.Fatalf("老师出 = %v", got)
	}
	if r := preview(c.t2, `{"userId":"`+c.s2ID+`","count":10,"source":{"kind":"target","status":"learning"}}`); r.Status != 403 {
		t.Fatalf("别班老师 = %d", r.Status)
	}
	if r := preview(c.s2, `{"count":10,"source":{"kind":"target","status":"learning","bookId":"b3"}}`); r.Status != 404 {
		t.Fatalf("不在目标里的书 = %d %v", r.Status, r.Body)
	}
	r := preview(c.s2, `{"count":10,"source":{"kind":"target","status":"known"}}`)
	if r.Status != 400 {
		t.Fatalf("status 非法 = %d", r.Status)
	}
	if det := r.Body["error"].(map[string]any)["details"].(map[string]any); det["source.status"] == nil {
		t.Fatalf("details = %v", det)
	}
	// 原有来源的错误信息不变
	r = preview(c.s2, `{"count":10,"source":{"kind":"nope"}}`)
	if det := r.Body["error"].(map[string]any)["details"].(map[string]any); det["source.kind"] != "Invalid discriminator value. Expected 'unfamiliar' | 'session' | 'unit' | 'book'" {
		t.Fatalf("discriminator = %v", det)
	}
}

// 走一遍真实的检测流程：设目标 → 开检测 → 交卷 → 覆盖进度变化。
func TestCoverageAfterRealTest(t *testing.T) {
	c := newCovEnv(t, nil)
	h := c.s3
	c.do("PUT", "/api/me/target-books", `{"bookIds":["b1"]}`, h)
	if d := dataOf(c.do("GET", "/api/records/coverage", "", h)); !reflect.DeepEqual(countsOf(d["total"]), map[string]float64{"target": 4, "tested": 0, "known": 0, "learning": 0, "untested": 4}) {
		t.Fatalf("测前 = %v", d)
	}
	tp := c.do("POST", "/api/plans", `{"name":"t","kind":"test","modes":["spelling"],"testSize":2,"testScope":"all","unitIds":["u1a"]}`, h)
	if tp.Status != 200 {
		t.Fatalf("plan = %v", tp.Body)
	}
	ts := c.do("POST", "/api/study/sessions", `{"kind":"test","planId":"`+dataOf(tp)["id"].(string)+`"}`, h)
	tid := dataOf(ts)["id"].(string)
	items := dataOf(c.do("GET", "/api/study/sessions/"+tid, "", h))["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("items = %v", items)
	}
	for _, raw := range items {
		body, _ := json.Marshal(map[string]any{"wordId": raw.(map[string]any)["wordId"], "mode": "spelling", "phase": "test", "attempt": 1, "answer": "x"})
		c.do("POST", "/api/study/sessions/"+tid+"/answers", string(body), h)
	}
	// 交卷前不计
	if d := dataOf(c.do("GET", "/api/records/coverage", "", h)); countsOf(d["total"])["tested"] != 0 {
		t.Fatalf("交卷前 = %v", d)
	}
	if r := c.do("POST", "/api/study/sessions/"+tid+"/complete", "", h); r.Status != 200 {
		t.Fatalf("complete = %v", r.Body)
	}
	if d := dataOf(c.do("GET", "/api/records/coverage", "", h)); !reflect.DeepEqual(countsOf(d["total"]), map[string]float64{"target": 4, "tested": 2, "known": 0, "learning": 2, "untested": 2}) {
		t.Fatalf("交卷后 = %v", d)
	}
}

// 个人版：不看班级成员关系，用自己设的目标；班级目标接口 404。
func TestTargetBooksPersonalEdition(t *testing.T) {
	e := newEnv(t, map[string]string{"VINX_EDITION": "personal"})
	h, id := signupWithRole(t, e, "me@x.test", "")
	db := e.d.DB
	db.Exec(`INSERT INTO "User" ("id","email","passwordHash","name","role") VALUES ('tt','tt@x','h','tt','teacher')`)
	db.Exec(`INSERT INTO "Book" ("id","name","isSystem") VALUES ('b1','书',1)`)
	db.Exec(`INSERT INTO "Classroom" ("id","name","teacherId","inviteCode") VALUES ('c1','班','tt','AAAAAA')`)
	db.Exec(`INSERT INTO "ClassMember" ("classId","userId") VALUES ('c1',?)`, id)
	db.Exec(`INSERT INTO "ClassTargetBook" ("classId","bookId") VALUES ('c1','b1')`)
	if d := dataOf(e.do("GET", "/api/me/target-books", "", h)); d["source"] != "none" {
		t.Fatalf("个人版初始 = %v", d)
	}
	if d := dataOf(e.do("PUT", "/api/me/target-books", `{"bookIds":["b1"]}`, h)); d["source"] != "own" {
		t.Fatalf("个人版 PUT = %v", d)
	}
	if r := e.do("GET", "/api/classes/c1/target-books", "", h); r.Status != 404 {
		t.Fatalf("个人版班级目标 = %d", r.Status)
	}
}
