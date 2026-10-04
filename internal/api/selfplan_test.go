package api

import (
	"reflect"
	"strings"
	"testing"
)

// 班级自主开关与「先定目标词书，再建计划」（spec 0008）。夹具见 coverage_test.go 的 newCovEnv：
// c1（t1 带）：s1、s2；c2（t2 带）：s1；s3 没有班级。b1 = u1a(w1,w2) + u1b(w3,w4)，b2 = u2(w4,w5)，b3 = u3(w6)。

func (c *covEnv) mustOK(r resp, what string) map[string]any {
	c.t.Helper()
	if r.Status != 200 {
		c.t.Fatalf("%s = %d %v", what, r.Status, r.Body)
	}
	return dataOf(r)
}

func (c *covEnv) setAllow(classID string, teacher map[string]string, allow bool) {
	c.t.Helper()
	v := "false"
	if allow {
		v = "true"
	}
	d := c.mustOK(c.do("PATCH", "/api/classes/"+classID, `{"allowSelfPlan":`+v+`}`, teacher), "PATCH allowSelfPlan")
	if d["allowSelfPlan"] != allow {
		c.t.Fatalf("PATCH 后 allowSelfPlan = %v", d["allowSelfPlan"])
	}
}

func (c *covEnv) createPlan(h map[string]string, body string) resp {
	return c.do("POST", "/api/plans", body, h)
}

func sourcesOf(t *testing.T, v any) []string {
	t.Helper()
	out := []string{}
	for _, b := range v.([]any) {
		out = append(out, b.(map[string]any)["source"].(string))
	}
	return out
}

func TestClassAllowSelfPlanField(t *testing.T) {
	c := newCovEnv(t, nil)
	// 缺省为 true（旧客户端、现有契约用例不受影响）
	d := c.mustOK(c.do("POST", "/api/classes", `{"name":"三班"}`, c.t1), "建班")
	if d["allowSelfPlan"] != true {
		t.Fatalf("缺省 allowSelfPlan = %v", d["allowSelfPlan"])
	}
	d = c.mustOK(c.do("POST", "/api/classes", `{"name":"四班","allowSelfPlan":false}`, c.t1), "建班 false")
	if d["allowSelfPlan"] != false {
		t.Fatalf("allowSelfPlan = %v", d["allowSelfPlan"])
	}
	id := d["id"].(string)
	if r := c.do("POST", "/api/classes", `{"name":"五班","allowSelfPlan":"no"}`, c.t1); r.Status != 400 ||
		!reflect.DeepEqual(r.Body["error"].(map[string]any)["details"], map[string]any{"allowSelfPlan": "Expected boolean, received string"}) {
		t.Fatalf("类型错误 = %d %v", r.Status, r.Body)
	}
	if r := c.do("POST", "/api/classes", `{"name":"五班","allowSelfPlan":null}`, c.t1); r.Status != 400 {
		t.Fatalf("null = %d", r.Status)
	}
	if d := c.mustOK(c.do("GET", "/api/classes/"+id, "", c.t1), "详情"); d["allowSelfPlan"] != false {
		t.Fatalf("详情 = %v", d["allowSelfPlan"])
	}
	// 只改名不动开关
	if d := c.mustOK(c.do("PATCH", "/api/classes/"+id, `{"name":"四班改"}`, c.t1), "改名"); d["allowSelfPlan"] != false {
		t.Fatalf("改名后 = %v", d["allowSelfPlan"])
	}
	c.setAllow(id, c.t1, true)
	items := c.mustOK(c.do("GET", "/api/classes", "", c.t1), "列表")["items"].([]any)
	for _, it := range items {
		if _, ok := it.(map[string]any)["allowSelfPlan"].(bool); !ok {
			t.Fatalf("列表缺 allowSelfPlan：%v", it)
		}
	}
	// 别班老师不能改
	if r := c.do("PATCH", "/api/classes/"+id, `{"allowSelfPlan":false}`, c.t2); r.Status != 403 {
		t.Fatalf("别班老师 = %d", r.Status)
	}
}

func TestEffectiveTargetsWithSelfPlan(t *testing.T) {
	c := newCovEnv(t, nil)
	c.mustOK(c.do("PUT", "/api/classes/c1/target-books", `{"bookIds":["b1"]}`, c.t1), "c1 目标")

	// 班级允许自主：班级的书（锁定）+ 自己追加的；同一本书按班级算，显示一次
	d := c.mustOK(c.do("PUT", "/api/me/target-books", `{"bookIds":["b3","b1"]}`, c.s2), "s2 追加")
	if d["source"] != "class" || d["canEditOwn"] != true ||
		!reflect.DeepEqual(bookIDsOf(t, d["books"]), []string{"b1", "b3"}) || !reflect.DeepEqual(sourcesOf(t, d["books"]), []string{"class", "own"}) ||
		!reflect.DeepEqual(bookIDsOf(t, d["ownBooks"]), []string{"b3", "b1"}) {
		t.Fatalf("s2 允许 = %v", d)
	}
	if names := d["books"].([]any)[0].(map[string]any)["classNames"]; !reflect.DeepEqual(names, []any{"一班"}) {
		t.Fatalf("classNames = %v", names)
	}
	// 覆盖进度也按有效目标（b1 4 词 + b3 1 词）
	if cov := c.mustOK(c.do("GET", "/api/records/coverage", "", c.s2), "coverage"); cov["total"].(map[string]any)["target"] != float64(5) {
		t.Fatalf("coverage = %v", cov)
	}
	// 班级目标接口不带 source
	if items := c.mustOK(c.do("GET", "/api/classes/c1/target-books", "", c.t1), "c1")["items"].([]any); len(items) != 1 || items[0].(map[string]any)["source"] != nil {
		t.Fatalf("班级目标 = %v", items)
	}

	// 不允许：只有班级目标，自己设的保留但不生效
	c.setAllow("c1", c.t1, false)
	d = c.mustOK(c.do("GET", "/api/me/target-books", "", c.s2), "s2 不允许")
	if d["canEditOwn"] != false || !reflect.DeepEqual(bookIDsOf(t, d["books"]), []string{"b1"}) || !reflect.DeepEqual(bookIDsOf(t, d["ownBooks"]), []string{"b3", "b1"}) {
		t.Fatalf("s2 不允许 = %v", d)
	}
	c.setAllow("c1", c.t1, true)

	// 多班取最严：s1 在 c1（允许）和 c2（不允许）
	c.mustOK(c.do("PUT", "/api/me/target-books", `{"bookIds":["b3"]}`, c.s1), "s1 追加")
	if d := c.mustOK(c.do("GET", "/api/me/target-books", "", c.s1), "s1"); d["canEditOwn"] != true || !reflect.DeepEqual(bookIDsOf(t, d["books"]), []string{"b1", "b3"}) {
		t.Fatalf("s1 都允许 = %v", d)
	}
	c.setAllow("c2", c.t2, false)
	if d := c.mustOK(c.do("GET", "/api/me/target-books", "", c.s1), "s1"); d["canEditOwn"] != false || !reflect.DeepEqual(bookIDsOf(t, d["books"]), []string{"b1"}) {
		t.Fatalf("s1 多班取最严 = %v", d)
	}

	// 没有班级：自己的，source = own，永远能编辑
	d = c.mustOK(c.do("PUT", "/api/me/target-books", `{"bookIds":["b2"]}`, c.s3), "s3")
	if d["source"] != "own" || d["canEditOwn"] != true || !reflect.DeepEqual(sourcesOf(t, d["books"]), []string{"own"}) {
		t.Fatalf("s3 = %v", d)
	}
}

func TestPlanMustFallInTargets(t *testing.T) {
	c := newCovEnv(t, nil)

	// 目标为空不约束
	early := c.mustOK(c.createPlan(c.s3, `{"name":"早","unitIds":["u3"]}`), "空目标建计划")
	c.mustOK(c.do("PUT", "/api/me/target-books", `{"bookIds":["b1"]}`, c.s3), "s3 目标")

	// 自己的目标：b3 不在 → 400；b1 可以
	r := c.createPlan(c.s3, `{"name":"x","unitIds":["u1a","u3"]}`)
	if r.Status != 400 || errCode(r) != "VALIDATION 所选单元不在目标词书内：《九上》" {
		t.Fatalf("超出目标 = %d %s", r.Status, errCode(r))
	}
	ok := c.mustOK(c.createPlan(c.s3, `{"name":"y","unitIds":["u1a"]}`), "目标内")
	if ok["outsideTarget"] != false || ok["selfPlanPaused"] != false {
		t.Fatalf("标记 = %v", ok)
	}

	// 已有的超出目标的计划：不改不停，只改节奏不检查；单元集合没变也不检查；新增超出目标的单元才拒绝
	earlyID := early["id"].(string)
	if d := c.mustOK(c.do("PATCH", "/api/plans/"+earlyID, `{"newPerDay":5}`, c.s3), "只改节奏"); d["outsideTarget"] != true {
		t.Fatalf("outsideTarget = %v", d["outsideTarget"])
	}
	c.mustOK(c.do("PATCH", "/api/plans/"+earlyID, `{"unitIds":["u3"],"name":"早2"}`, c.s3), "单元没变")
	c.mustOK(c.do("PATCH", "/api/plans/"+earlyID, `{"unitIds":["u3","u1b"]}`, c.s3), "新增目标内单元")
	if r := c.do("PATCH", "/api/plans/"+earlyID, `{"unitIds":["u3","u1b","u2"]}`, c.s3); r.Status != 400 || errCode(r) != "VALIDATION 所选单元不在目标词书内：《中考》" {
		t.Fatalf("新增超出目标 = %d %s", r.Status, errCode(r))
	}
	// 列表与今日页标「不在目标词书内」
	list := c.mustOK(c.do("GET", "/api/plans", "", c.s3), "列表")["items"].([]any)
	flags := map[string]any{}
	for _, it := range list {
		m := it.(map[string]any)
		flags[m["id"].(string)] = m["outsideTarget"]
	}
	if flags[earlyID] != true || flags[ok["id"].(string)] != false {
		t.Fatalf("列表标记 = %v", flags)
	}
	today := c.mustOK(c.do("GET", "/api/today", "", c.s3), "今日")
	if !reflect.DeepEqual(today["outsideTargetPlanIds"], []any{earlyID}) || today["pausedSelfPlans"] != float64(0) {
		t.Fatalf("今日 = %v %v", today["outsideTargetPlanIds"], today["pausedSelfPlans"])
	}

	// 给班级：班级目标；c2 没设目标 → 不约束
	c.mustOK(c.do("PUT", "/api/classes/c1/target-books", `{"bookIds":["b1","b3"]}`, c.t1), "c1 目标")
	if r := c.createPlan(c.t1, `{"name":"班","unitIds":["u2"],"targets":{"classIds":["c1"]}}`); r.Status != 400 || errCode(r) != "VALIDATION 所选单元不在目标词书内：《中考》" {
		t.Fatalf("班级超出 = %d %s", r.Status, errCode(r))
	}
	c.mustOK(c.createPlan(c.t1, `{"name":"班","unitIds":["u3"],"targets":{"classIds":["c1"]}}`), "班级目标内")
	c.mustOK(c.createPlan(c.t2, `{"name":"二班","unitIds":["u2"],"targets":{"classIds":["c2"]}}`), "空目标班级")

	// 多对象取交集：c1 [b1,b3] ∩ s2 的有效目标 [b1,b3,b2(自选)] ∩ s1 ... s1 在 c1、c2（都允许）→ [b1,b3]
	c.mustOK(c.do("PUT", "/api/me/target-books", `{"bookIds":["b2"]}`, c.s2), "s2 自选")
	c.mustOK(c.createPlan(c.t1, `{"name":"s2","unitIds":["u2"],"targets":{"userIds":["`+c.s2ID+`"]}}`), "给 s2 用自选书")
	if r := c.createPlan(c.t1, `{"name":"交集","unitIds":["u2"],"targets":{"classIds":["c1"],"userIds":["`+c.s2ID+`"]}}`); r.Status != 400 {
		t.Fatalf("交集 = %d %v", r.Status, r.Body)
	}
	// 约束集合接口：交集 [b1,b3]
	d := c.mustOK(c.do("POST", "/api/plans/allowed-books", `{"targets":{"classIds":["c1"],"userIds":["`+c.s2ID+`"]}}`, c.t1), "allowed-books")
	if d["constrained"] != true || !reflect.DeepEqual(bookIDsOf(t, d["books"]), []string{"b1", "b3"}) {
		t.Fatalf("allowed-books = %v", d)
	}
	// 老师自己（没有目标）→ 不约束
	if d := c.mustOK(c.do("POST", "/api/plans/allowed-books", `{}`, c.t1), "老师自己"); d["constrained"] != false || d["selfPlanAllowed"] != true {
		t.Fatalf("老师自己 = %v", d)
	}
	// 学生不能查别人的
	if r := c.do("POST", "/api/plans/allowed-books", `{"targets":{"classIds":["c1"]}}`, c.s2); r.Status != 403 {
		t.Fatalf("学生查班级 = %d", r.Status)
	}
}

func TestSelfPlanClosedAndComputedPause(t *testing.T) {
	c := newCovEnv(t, nil)
	// 允许时 s2 自建计划，并开一组新学（未作答）
	plan := c.mustOK(c.createPlan(c.s2, `{"name":"自建","newPerDay":2,"modes":["recognition"],"unitIds":["u1a"]}`), "自建")
	planID := plan["id"].(string)
	assigned := c.mustOK(c.createPlan(c.t1, `{"name":"老师布置","newPerDay":2,"modes":["recognition"],"unitIds":["u1b"],"targets":{"userIds":["`+c.s2ID+`"]}}`), "老师布置")
	start := c.mustOK(c.do("POST", "/api/study/sessions", `{"kind":"learn","planId":"`+planID+`"}`, c.s2), "开组")
	if start["id"] == nil {
		t.Fatalf("开组 = %v", start)
	}
	todayPlans := func() []string {
		ids := []string{}
		for _, p := range c.mustOK(c.do("GET", "/api/today", "", c.s2), "今日")["plans"].([]any) {
			ids = append(ids, p.(map[string]any)["planId"].(string))
		}
		return ids
	}
	if got := todayPlans(); len(got) != 2 {
		t.Fatalf("允许时今日 = %v", got)
	}

	// 影响统计：s1（自建 0、自选 0）、s2（自建 1）
	c.mustOK(c.do("PUT", "/api/me/target-books", `{"bookIds":["b3","b2"]}`, c.s1), "s1 自选")
	imp := c.mustOK(c.do("GET", "/api/classes/c1/self-plan-impact", "", c.t1), "影响")
	if !reflect.DeepEqual(imp, map[string]any{"students": float64(2), "selfPlans": float64(1), "ownBooks": float64(2)}) {
		t.Fatalf("影响 = %v", imp)
	}
	if r := c.do("GET", "/api/classes/c1/self-plan-impact", "", c.t2); r.Status != 403 {
		t.Fatalf("别班老师看影响 = %d", r.Status)
	}

	c.setAllow("c1", c.t1, false)
	// 已经不允许：再关一次没有影响
	if imp := c.mustOK(c.do("GET", "/api/classes/c1/self-plan-impact", "", c.t1), "影响"); imp["selfPlans"] != float64(0) || imp["students"] != float64(0) {
		t.Fatalf("已关闭的影响 = %v", imp)
	}

	// 不能自建：403；老师照样能布置
	if r := c.createPlan(c.s2, `{"name":"再建","unitIds":["u1a"]}`); r.Status != 403 || errCode(r) != "FORBIDDEN 班级未开放自主安排计划" {
		t.Fatalf("不允许自建 = %d %s", r.Status, errCode(r))
	}
	if d := c.mustOK(c.do("POST", "/api/plans/allowed-books", `{}`, c.s2), "allowed"); d["selfPlanAllowed"] != false {
		t.Fatalf("selfPlanAllowed = %v", d)
	}
	// 改单元也拒绝；只改节奏可以
	if r := c.do("PATCH", "/api/plans/"+planID, `{"unitIds":["u1a","u1b"]}`, c.s2); r.Status != 403 {
		t.Fatalf("不允许时改单元 = %d", r.Status)
	}
	c.mustOK(c.do("PATCH", "/api/plans/"+planID, `{"newPerDay":3}`, c.s2), "改节奏")

	// 算出来的暂停：今日不出现，status 不变，列表标记；老师布置的不受影响
	if got := todayPlans(); !reflect.DeepEqual(got, []string{assigned["id"].(string)}) {
		t.Fatalf("不允许时今日 = %v", got)
	}
	if n := c.mustOK(c.do("GET", "/api/today", "", c.s2), "今日")["pausedSelfPlans"]; n != float64(1) {
		t.Fatalf("pausedSelfPlans = %v", n)
	}
	d := c.mustOK(c.do("GET", "/api/plans/"+planID, "", c.s2), "详情")
	if d["status"] != "active" || d["selfPlanPaused"] != true {
		t.Fatalf("详情 = %v %v", d["status"], d["selfPlanPaused"])
	}
	if d := c.mustOK(c.do("GET", "/api/plans/"+assigned["id"].(string), "", c.s2), "布置的"); d["selfPlanPaused"] != false {
		t.Fatalf("布置的 = %v", d["selfPlanPaused"])
	}
	// 老师看学生的自建计划，也按学生本人算
	found := false
	for _, it := range c.mustOK(c.do("GET", "/api/plans", "", c.t1), "老师列表")["items"].([]any) {
		m := it.(map[string]any)
		if m["id"] == planID {
			found = true
			if m["selfPlanPaused"] != true {
				t.Fatalf("老师看到 = %v", m["selfPlanPaused"])
			}
		}
	}
	if !found {
		t.Fatal("老师看不到学生的自建计划")
	}
	// 进行中的组按 K22：先保存（这里没作答，丢弃），再提示
	if r := c.do("POST", "/api/study/sessions", `{"kind":"learn","planId":"`+planID+`"}`, c.s2); r.Status != 404 || errCode(r) != "NOT_FOUND 计划已暂停、结束或不再安排给你，之前的作答已保存" {
		t.Fatalf("K22 = %d %s", r.Status, errCode(r))
	}
	// 再开：明确提示暂停
	if r := c.do("POST", "/api/study/sessions", `{"kind":"learn","planId":"`+planID+`"}`, c.s2); r.Status != 403 || !strings.Contains(errCode(r), "暂停") {
		t.Fatalf("暂停中开组 = %d %s", r.Status, errCode(r))
	}
	// 学生自己点的暂停不受影响：重新允许后，自己暂停的仍是暂停
	c.mustOK(c.do("PATCH", "/api/plans/"+planID, `{"status":"paused"}`, c.s2), "自己暂停")
	c.setAllow("c1", c.t1, true)
	if got := todayPlans(); len(got) != 1 {
		t.Fatalf("自己暂停的不恢复 = %v", got)
	}
	c.mustOK(c.do("PATCH", "/api/plans/"+planID, `{"status":"active"}`, c.s2), "恢复")
	if got := todayPlans(); len(got) != 2 {
		t.Fatalf("重新允许后恢复 = %v", got)
	}
	c.mustOK(c.do("POST", "/api/study/sessions", `{"kind":"learn","planId":"`+planID+`"}`, c.s2), "恢复后开组")
}

func TestOverviewCoverageModes(t *testing.T) {
	c := newCovEnv(t, nil)
	c.mustOK(c.do("PUT", "/api/classes/c1/target-books", `{"bookIds":["b1"]}`, c.t1), "c1 目标")
	c.mustOK(c.do("PUT", "/api/classes/c2/target-books", `{"bookIds":["b2"]}`, c.t2), "c2 目标")
	c.mustOK(c.do("PUT", "/api/me/target-books", `{"bookIds":["b3"]}`, c.s2), "s2 自选")

	rows := func() (string, map[string]map[string]any) {
		d := c.mustOK(c.do("GET", "/api/classes/c1/overview", "", c.t1), "概览")
		out := map[string]map[string]any{}
		for _, s := range d["students"].([]any) {
			m := s.(map[string]any)
			out[m["userId"].(string)] = m
		}
		return d["coverageMode"].(string), out
	}
	target := func(m map[string]any) any { return m["coverage"].(map[string]any)["target"] }

	// 允许自主：按学生自己的有效目标；s1 = c1∪c2 = b1(4)+b2(w4 重复,w5) = 5；s2 = b1 + 自选 b3 = 5，标「含自选」
	mode, r := rows()
	if mode != "student" || target(r[c.s1ID]) != float64(5) || target(r[c.s2ID]) != float64(5) ||
		r[c.s2ID]["coverageIncludesOwn"] != true || r[c.s1ID]["coverageIncludesOwn"] != false {
		t.Fatalf("允许 = %s %v", mode, r)
	}
	// 不允许：只按本班目标，同一个分母
	c.setAllow("c1", c.t1, false)
	mode, r = rows()
	if mode != "class" || target(r[c.s1ID]) != float64(4) || target(r[c.s2ID]) != float64(4) || r[c.s2ID]["coverageIncludesOwn"] != false {
		t.Fatalf("不允许 = %s %v", mode, r)
	}
}

func TestPersonalEditionIgnoresClassSelfPlan(t *testing.T) {
	e := newEnv(t, map[string]string{"VINX_EDITION": "personal"})
	cookie, uid := studentCookie(t, e, "me@x.test")
	h := map[string]string{"Cookie": cookie}
	unit := seedSystemUnit(t, e, 3)
	// 降级前留下的班级关系：所在班级不允许自主
	for _, q := range []string{
		`INSERT INTO "User" ("id","email","passwordHash","name","role") VALUES ('tt','tt@x.test','x','老师','teacher')`,
		`INSERT INTO "Classroom" ("id","name","teacherId","inviteCode","allowSelfPlan") VALUES ('cx','旧班','tt','CCCCCC',0)`,
		`INSERT INTO "ClassMember" ("classId","userId","joinedAt") VALUES ('cx','` + uid + `','2026-09-01T00:00:00.000Z')`,
	} {
		if _, err := e.d.DB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	r := e.do("POST", "/api/plans", `{"name":"p","newPerDay":3,"modes":["recognition"],"unitIds":["`+unit+`"]}`, h)
	if r.Status != 200 {
		t.Fatalf("个人版自建 = %d %v", r.Status, r.Body)
	}
	if d := dataOf(r); d["selfPlanPaused"] != false {
		t.Fatalf("selfPlanPaused = %v", d["selfPlanPaused"])
	}
	if plans := dataOf(e.do("GET", "/api/today", "", h))["plans"].([]any); len(plans) != 1 {
		t.Fatalf("个人版今日 = %v", plans)
	}
	if d := dataOf(e.do("GET", "/api/me/target-books", "", h)); d["canEditOwn"] != true {
		t.Fatalf("个人版 canEditOwn = %v", d)
	}
}
