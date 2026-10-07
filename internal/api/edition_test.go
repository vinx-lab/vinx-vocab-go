package api

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/vinx-lab/vinx-vocab-go/internal/core"
	"github.com/vinx-lab/vinx-vocab-go/internal/service"
)

// 首次运行向导、版本切换、降级后的数据范围、改角色保留管理员（Go 版新增 / 契约测试不便覆盖的部分）。

func configOf(t *testing.T, e *testEnv) map[string]any {
	t.Helper()
	r := e.do("GET", "/api/config", "", nil)
	if r.Status != 200 {
		t.Fatalf("config = %d", r.Status)
	}
	return r.Body["data"].(map[string]any)
}

func signupAs(t *testing.T, e *testEnv, email string) (cookie string, user map[string]any) {
	t.Helper()
	r := e.do("POST", "/api/auth/signup", `{"email":"`+email+`","password":"123456"}`, nil)
	if r.Status != 201 {
		t.Fatalf("signup %s = %d %s", email, r.Status, errCode(r))
	}
	return cookieOf(t, r), r.Body["data"].(map[string]any)["user"].(map[string]any)
}

// /config 带上程序版本，页面据此显示「版本 …」。
func TestConfigVersion(t *testing.T) {
	e := newEnv(t, nil)
	if v := configOf(t, e)["version"]; v != "dev" {
		t.Fatalf("version = %v", v)
	}
}

func TestFirstRunSetupPersonal(t *testing.T) {
	e := newEnv(t, nil)
	cfg := configOf(t, e)
	if cfg["needsSetup"] != true || cfg["editionLocked"] != false || cfg["edition"] != "school" {
		t.Fatalf("全新安装 = %v", cfg)
	}
	if r := e.do("POST", "/api/setup/edition", `{"edition":"solo"}`, nil); r.Status != 400 {
		t.Fatalf("非法版本 = %d", r.Status)
	}
	r := e.do("POST", "/api/setup/edition", `{"edition":"personal"}`, nil)
	if r.Status != 200 {
		t.Fatalf("setup = %d %s", r.Status, errCode(r))
	}
	data := r.Body["data"].(map[string]any)
	if data["edition"] != "personal" || data["needsSetup"] != false || data["signupEnabled"] != true {
		t.Fatalf("setup 返回 = %v", data)
	}
	// 已选择：不能再走向导
	r = e.do("POST", "/api/setup/edition", `{"edition":"school"}`, nil)
	if r.Status != 403 || errCode(r) != "FORBIDDEN 已完成初始设置，如需切换版本请由管理员在系统设置里操作" {
		t.Fatalf("重复 setup = %d %s", r.Status, errCode(r))
	}
	// 个人版功能立即关闭；第一个账号为管理员
	if r := e.do("GET", "/api/classes", "", nil); r.Status != 404 {
		t.Fatalf("个人版 /classes = %d", r.Status)
	}
	_, u := signupAs(t, e, "me@x.test")
	if u["role"] != "admin" {
		t.Fatalf("个人版首个账号 = %v", u["role"])
	}
}

func TestFirstRunSetupSchoolFirstAccountIsAdmin(t *testing.T) {
	e := newEnv(t, nil)
	if r := e.do("POST", "/api/setup/edition", `{"edition":"school"}`, nil); r.Status != 200 {
		t.Fatalf("setup = %d", r.Status)
	}
	cookie, u := signupAs(t, e, "owner@x.test")
	if u["role"] != "admin" {
		t.Fatalf("班级版全新安装的第一个账号应为管理员：%v", u["role"])
	}
	if _, u2 := signupAs(t, e, "kid@x.test"); u2["role"] != "student" {
		t.Fatalf("之后注册的是学生：%v", u2["role"])
	}
	// 管理员可以建老师
	r := e.do("POST", "/api/users", `{"email":"t@x.test","name":"老师","password":"123456","role":"teacher"}`, map[string]string{"Cookie": cookie})
	if r.Status != 200 {
		t.Fatalf("建老师 = %d %s", r.Status, errCode(r))
	}
}

func TestNeedsSetupFalseWhenUsersExist(t *testing.T) {
	e := newEnv(t, nil)
	signupAs(t, e, "a@x.test") // 不走向导直接注册（如导入的旧数据、seed-demo）
	cfg := configOf(t, e)
	if cfg["needsSetup"] != false || cfg["edition"] != "school" {
		t.Fatalf("已有账号 = %v", cfg)
	}
	if r := e.do("POST", "/api/setup/edition", `{"edition":"personal"}`, nil); r.Status != 403 {
		t.Fatalf("已有账号时 setup = %d", r.Status)
	}
}

func TestSetupEditionConcurrent(t *testing.T) {
	e := newEnv(t, nil)
	var wg sync.WaitGroup
	codes := make([]int, 8)
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ed := "personal"
			if i%2 == 1 {
				ed = "school"
			}
			codes[i] = e.do("POST", "/api/setup/edition", `{"edition":"`+ed+`"}`, nil).Status
		}(i)
	}
	wg.Wait()
	ok := 0
	for _, c := range codes {
		if c == 200 {
			ok++
		} else if c != 403 {
			t.Fatalf("意外状态 %d", c)
		}
	}
	if ok != 1 {
		t.Fatalf("并发 setup 应只成功一次：%v", codes)
	}
}

func TestPersonalConcurrentSignupOnlyOne(t *testing.T) {
	e := newEnv(t, map[string]string{"VINX_EDITION": "personal"})
	var wg sync.WaitGroup
	codes := make([]int, 6)
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i] = e.do("POST", "/api/auth/signup", `{"email":"p`+string(rune('a'+i))+`@x.test","password":"123456"}`, nil).Status
		}(i)
	}
	wg.Wait()
	n := 0
	for _, c := range codes {
		if c == 201 {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("个人版并发注册应只成功一个：%v", codes)
	}
}

func TestEditionLocked(t *testing.T) {
	e := newEnv(t, map[string]string{"VINX_EDITION": "school"})
	cfg := configOf(t, e)
	if cfg["editionLocked"] != true || cfg["needsSetup"] != false {
		t.Fatalf("锁定 = %v", cfg)
	}
	const locked = "FORBIDDEN 版本由 VINX_EDITION 指定，不能在界面上切换"
	if r := e.do("POST", "/api/setup/edition", `{"edition":"personal"}`, nil); errCode(r) != locked {
		t.Fatalf("锁定时 setup = %s", errCode(r))
	}
	cookie, _ := signupAs(t, e, "boss@x.test") // 空库第一个账号为管理员
	r := e.do("PUT", "/api/settings/edition", `{"edition":"personal"}`, map[string]string{"Cookie": cookie})
	if r.Status != 403 || errCode(r) != locked {
		t.Fatalf("锁定时切换 = %d %s", r.Status, errCode(r))
	}
}

func TestDowngradeKeepsDataAndNarrowsScope(t *testing.T) {
	e := newEnv(t, nil)
	unit := seedSystemUnit(t, e, 4)
	adminCookie, _ := signupAs(t, e, "admin@x.test")
	h := func(c string) map[string]string { return map[string]string{"Cookie": c} }
	if r := e.do("POST", "/api/users", `{"email":"t@x.test","name":"老师","password":"123456","role":"teacher"}`, h(adminCookie)); r.Status != 200 {
		t.Fatal(errCode(r))
	}
	tr := e.do("POST", "/api/auth/login", `{"email":"t@x.test","password":"123456"}`, nil)
	tc := cookieOf(t, tr)
	cls := e.do("POST", "/api/classes", `{"name":"一班"}`, h(tc)).Body["data"].(map[string]any)
	sr := e.do("POST", "/api/auth/signup", `{"email":"s@x.test","password":"123456","inviteCode":"`+cls["inviteCode"].(string)+`"}`, nil)
	sc := cookieOf(t, sr)
	sid := sr.Body["data"].(map[string]any)["user"].(map[string]any)["id"].(string)
	plan := e.do("POST", "/api/plans", `{"name":"班级计划","unitIds":["`+unit+`"],"targets":{"classIds":["`+cls["id"].(string)+`"],"userIds":[]}}`, h(tc))
	if plan.Status != 200 {
		t.Fatalf("plan = %d %s", plan.Status, errCode(plan))
	}
	planID := plan.Body["data"].(map[string]any)["id"].(string)

	r := e.do("PUT", "/api/settings/edition", `{"edition":"personal"}`, h(adminCookie))
	if r.Status != 200 || r.Body["data"].(map[string]any)["edition"] != "personal" {
		t.Fatalf("降级 = %d %s", r.Status, errCode(r))
	}
	// 学生：计划仍在今日；老师 / 管理员：看不到学生记录、不能再安排
	today := e.do("GET", "/api/today", "", h(sc))
	if today.Status != 200 || !strings.Contains(mustJSON(today.Body), planID) {
		t.Fatalf("降级后学生今日 = %d", today.Status)
	}
	for _, c := range []string{tc, adminCookie} {
		if r := e.do("GET", "/api/records/summary?userId="+sid, "", h(c)); r.Status != 403 {
			t.Fatalf("降级后看学生记录 = %d", r.Status)
		}
	}
	if r := e.do("GET", "/api/plans/"+planID, "", h(adminCookie)); r.Status != 404 {
		t.Fatalf("降级后管理员看老师的计划 = %d", r.Status)
	}
	if r := e.do("PATCH", "/api/plans/"+planID, `{"newPerDay":5}`, h(adminCookie)); r.Status != 403 {
		t.Fatalf("降级后管理员改老师的计划 = %d", r.Status)
	}
	if r := e.do("GET", "/api/users", "", h(adminCookie)); r.Status != 404 {
		t.Fatalf("降级后 /users = %d", r.Status)
	}
	// 升级：原样恢复
	e.do("PUT", "/api/settings/edition", `{"edition":"school"}`, h(adminCookie))
	d := e.do("GET", "/api/classes/"+cls["id"].(string), "", h(tc))
	if d.Status != 200 || !strings.Contains(mustJSON(d.Body), sid) || !strings.Contains(mustJSON(d.Body), planID) {
		t.Fatalf("升级后班级 = %d %v", d.Status, d.Body)
	}
	if r := e.do("GET", "/api/records/summary?userId="+sid, "", h(tc)); r.Status != 200 {
		t.Fatalf("升级后看学生记录 = %d", r.Status)
	}
}

func TestLastAdminCannotDemoteSelf(t *testing.T) {
	e := newEnv(t, nil)
	cookie, u := signupAs(t, e, "only@x.test")
	id := u["id"].(string)
	h := map[string]string{"Cookie": cookie}
	r := e.do("PATCH", "/api/users/"+id, `{"role":"teacher"}`, h)
	if r.Status != 400 || errCode(r) != "VALIDATION 系统至少保留一个管理员" {
		t.Fatalf("唯一管理员降级 = %d %s", r.Status, errCode(r))
	}
	// 保持管理员角色可以
	if r := e.do("PATCH", "/api/users/"+id, `{"role":"admin"}`, h); r.Status != 200 {
		t.Fatalf("改为 admin = %d", r.Status)
	}
	// 有第二个管理员后可以降级自己
	if r := e.do("POST", "/api/users", `{"email":"two@x.test","name":"二号","password":"123456","role":"admin"}`, h); r.Status != 200 {
		t.Fatal(errCode(r))
	}
	if r := e.do("PATCH", "/api/users/"+id, `{"role":"teacher"}`, h); r.Status != 200 {
		t.Fatalf("有两个管理员时降级 = %d %s", r.Status, errCode(r))
	}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// 个人版下（运行时降级后可能仍有多个账号）的能力与数据范围：service.Can / SeesAll。
func TestServiceAccessPersonal(t *testing.T) {
	e := newEnv(t, nil)
	ctx := context.Background()
	db := e.d.DB
	for _, u := range [][2]string{{"t1", "teacher"}, {"s1", "student"}, {"a1", "admin"}} {
		db.Exec(`INSERT INTO "User" ("id","email","passwordHash","name","role") VALUES (?,?,?,?,?)`, u[0], u[0]+"@x", "h", u[0], u[1])
	}
	db.Exec(`INSERT INTO "Classroom" ("id","name","teacherId","inviteCode") VALUES ('c1','班','t1','AAAAAA')`)
	db.Exec(`INSERT INTO "ClassMember" ("classId","userId") VALUES ('c1','s1')`)
	db.Exec(`INSERT INTO "Book" ("id","name","isSystem","ownerId") VALUES ('sys','系统',1,NULL),('bt1','老师1',0,'t1'),('ba1','管理员',0,'a1')`)
	p := core.EditionPersonal
	t1 := &service.Actor{ID: "t1", Role: "teacher", Edition: p}
	s1 := &service.Actor{ID: "s1", Role: "student", Edition: p}
	a1 := &service.Actor{ID: "a1", Role: "admin", Edition: p}

	for _, c := range []core.Capability{core.CapClasses, core.CapUsers, core.CapPlansAssign, core.CapStudentsView} {
		if service.Can(t1, c) || service.Can(a1, c) {
			t.Errorf("个人版不应有 %s", c)
		}
	}
	if !service.Can(a1, core.CapSystem) || !service.Can(t1, core.CapBooksEdit) {
		t.Error("与多用户无关的能力保留")
	}
	if service.SeesAll(a1) || !service.SeesAll(&service.Actor{ID: "a1", Role: "admin", Edition: core.EditionSchool}) || !service.SeesAll(&service.Actor{ID: "a1", Role: "admin"}) {
		t.Error("SeesAll 只对班级版（或未知版本）的管理员为真")
	}
	for _, a := range []*service.Actor{t1, a1} {
		if err := service.AssertCanViewUser(ctx, db, a, "s1"); errCodeOf(err) != "FORBIDDEN" {
			t.Errorf("%s 个人版看学生 = %v", a.ID, err)
		}
		if err := service.AssertCanViewUser(ctx, db, a, a.ID); err != nil {
			t.Errorf("看自己 = %v", err)
		}
	}
	if ids, _ := service.ManageableClassIDs(ctx, db, t1); ids == nil || len(ids) != 0 {
		t.Errorf("个人版老师可管理班级 = %v", ids)
	}
	if ids, _ := service.ManageableClassIDs(ctx, db, a1); ids == nil || len(ids) != 0 {
		t.Errorf("个人版管理员可管理班级 = %v", ids)
	}
	visible := func(a *service.Actor) string {
		where, args, err := service.VisibleBookFilter(ctx, db, a, "b")
		if err != nil {
			t.Fatal(err)
		}
		rows, _ := db.Query(`SELECT b."id" FROM "Book" b WHERE `+where+` ORDER BY b."id"`, args...)
		defer rows.Close()
		var out []string
		for rows.Next() {
			var id string
			rows.Scan(&id)
			out = append(out, id)
		}
		return strings.Join(out, ",")
	}
	if got := visible(a1); got != "ba1,sys" {
		t.Errorf("个人版管理员可见 = %s", got)
	}
	// 学生仍能看到所在班级老师的词书（降级前安排的计划要能继续学）
	if got := visible(s1); got != "bt1,sys" {
		t.Errorf("个人版学生可见 = %s", got)
	}
	if _, err := service.AssertCanEditBook(ctx, db, a1, "sys"); err != nil {
		t.Errorf("个人版管理员改系统词书 = %v", err)
	}
	if _, err := service.AssertCanEditBook(ctx, db, a1, "bt1"); errCodeOf(err) != "FORBIDDEN" {
		t.Errorf("个人版管理员改老师的词书 = %v", err)
	}
	if !service.CanEditBook(a1, true, nil) || service.CanEditBook(a1, false, strPtr("t1")) {
		t.Error("CanEditBook 与 AssertCanEditBook 一致")
	}
}

func strPtr(s string) *string { return &s }

// 个人版下管理员也只能动自己的内容（A6 review M1）：只被他人私有词书引用的词条看不到、改不了，他人的单词单删不掉；
// 系统词书里的词仍可编辑；切回班级版后恢复管理员的全部权限。
func TestPersonalAdminCannotTouchOthersWordsOrSheets(t *testing.T) {
	e := newEnv(t, nil)
	seedSystemUnit(t, e, 2)
	adminCookie, _ := signupAs(t, e, "admin2@x.test")
	h := func(c string) map[string]string { return map[string]string{"Cookie": c} }
	if r := e.do("POST", "/api/users", `{"email":"t2@x.test","name":"老师","password":"123456","role":"teacher"}`, h(adminCookie)); r.Status != 200 {
		t.Fatal(errCode(r))
	}
	tc := cookieOf(t, e.do("POST", "/api/auth/login", `{"email":"t2@x.test","password":"123456"}`, nil))
	imp := e.do("POST", "/api/books/import", `{"newBook":{"name":"老师私有"},"units":[{"name":"U1","entries":[{"spelling":"privateword","definition":"私有"}]}]}`, h(tc))
	if imp.Status != 200 {
		t.Fatalf("import = %d %v", imp.Status, imp.Body)
	}
	var wordID, teacherID string
	if err := e.d.DB.QueryRow(`SELECT "id" FROM "Word" WHERE "spelling" = 'privateword'`).Scan(&wordID); err != nil {
		t.Fatal(err)
	}
	if err := e.d.DB.QueryRow(`SELECT "id" FROM "User" WHERE "email" = 't2@x.test'`).Scan(&teacherID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.d.DB.Exec(`INSERT INTO "WordSheet" ("id","userId","creatorId","seq") VALUES ('ws1',?,?,1),('ws2',?,?,2)`, teacherID, teacherID, teacherID, teacherID); err != nil {
		t.Fatal(err)
	}

	e.do("PUT", "/api/settings/edition", `{"edition":"personal"}`, h(adminCookie))
	if r := e.do("GET", "/api/words/"+wordID, "", h(adminCookie)); r.Status != 404 {
		t.Fatalf("个人版管理员看他人私有词 = %d", r.Status)
	}
	if r := e.do("PATCH", "/api/words/"+wordID, `{"definition":"改"}`, h(adminCookie)); r.Status != 403 {
		t.Fatalf("个人版管理员改他人私有词 = %d %s", r.Status, errCode(r))
	}
	if r := e.do("PATCH", "/api/words/sw00", `{"definition":"系统词可改"}`, h(adminCookie)); r.Status != 200 {
		t.Fatalf("个人版管理员改系统词 = %d %s", r.Status, errCode(r))
	}
	if r := e.do("DELETE", "/api/sheets/ws1", "", h(adminCookie)); r.Status != 403 {
		t.Fatalf("个人版管理员删他人单词单 = %d %s", r.Status, errCode(r))
	}

	e.do("PUT", "/api/settings/edition", `{"edition":"school"}`, h(adminCookie))
	if r := e.do("GET", "/api/words/"+wordID, "", h(adminCookie)); r.Status != 200 {
		t.Fatalf("班级版管理员看私有词 = %d", r.Status)
	}
	if r := e.do("PATCH", "/api/words/"+wordID, `{"definition":"改"}`, h(adminCookie)); r.Status != 200 {
		t.Fatalf("班级版管理员改私有词 = %d %s", r.Status, errCode(r))
	}
	if r := e.do("DELETE", "/api/sheets/ws2", "", h(adminCookie)); r.Status != 200 {
		t.Fatalf("班级版管理员删单词单 = %d %s", r.Status, errCode(r))
	}
}
