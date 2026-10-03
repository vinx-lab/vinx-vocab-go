package api

import (
	"context"
	"strings"
	"testing"

	"github.com/vinx-lab/vinx-vocab-go/internal/core"
	"github.com/vinx-lab/vinx-vocab-go/internal/service"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// newDevEnv 与 newEnv 相同，但以 --dev 启动（Cfg.Dev = true，路由在构造 Handler 时按它注册）。
func newDevEnv(t *testing.T) *testEnv {
	t.Helper()
	e := newEnv(t, nil)
	e.d.Cfg.Dev = true
	e.h = Handler(e.d)
	return e
}

func devUser(t *testing.T, e *testEnv, email, name, role string) *service.UserRow {
	t.Helper()
	u, err := service.CreateUser(context.Background(), e.d.DB, e.now, service.NewUser{Email: email, Name: name, Role: role, PasswordHash: "x"})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestDevRoutesAbsentWithoutFlag(t *testing.T) {
	e := newEnv(t, nil)
	if r := e.do("GET", "/api/dev/users", "", nil); r.Status != 404 || errCode(r) != errCode(e.do("GET", "/api/nope", "", nil)) {
		t.Fatalf("GET /dev/users = %d %s", r.Status, errCode(r))
	}
	if r := e.do("POST", "/api/dev/impersonate", `{"userId":"x","scope":"tab"}`, nil); r.Status != 404 {
		t.Fatalf("POST /dev/impersonate = %d", r.Status)
	}
	r := e.do("GET", "/api/config", "", nil)
	data := r.Body["data"].(map[string]any)
	if v, ok := data["dev"]; !ok || v != false {
		t.Fatalf("config.dev = %v (present %v)", v, ok)
	}
}

func TestDevUsersListSorted(t *testing.T) {
	e := newDevEnv(t)
	if r := e.do("GET", "/api/config", "", nil); r.Body["data"].(map[string]any)["dev"] != true {
		t.Fatalf("config.dev = %v", r.Body["data"])
	}
	s2 := devUser(t, e, "b-stu@x.test", "学生乙", core.RoleStudent)
	devUser(t, e, "a-stu@x.test", "学生甲", core.RoleStudent)
	teacher := devUser(t, e, "t@x.test", "老师", core.RoleTeacher)
	devUser(t, e, "z-admin@x.test", "管理员", core.RoleAdmin)
	ctx := context.Background()
	ts := store.NewTime(e.now)
	if _, err := e.d.DB.ExecContext(ctx, `INSERT INTO "Classroom" ("id","name","teacherId","inviteCode","createdAt","updatedAt") VALUES ('c1','一班',?,'C1',?,?)`, teacher.ID, ts, ts); err != nil {
		t.Fatal(err)
	}
	if _, err := e.d.DB.ExecContext(ctx, `INSERT INTO "ClassMember" ("classId","userId") VALUES ('c1',?)`, s2.ID); err != nil {
		t.Fatal(err)
	}

	r := e.do("GET", "/api/dev/users", "", nil)
	if r.Status != 200 {
		t.Fatalf("dev/users = %d %s", r.Status, errCode(r))
	}
	items := r.Body["data"].([]any)
	var got []string
	for _, it := range items {
		m := it.(map[string]any)
		got = append(got, m["role"].(string)+":"+m["email"].(string))
		if _, ok := m["passwordHash"]; ok {
			t.Fatal("不应返回密码哈希")
		}
	}
	want := "admin:z-admin@x.test teacher:t@x.test student:a-stu@x.test student:b-stu@x.test"
	if strings.Join(got, " ") != want {
		t.Fatalf("排序 = %v", got)
	}
	classesOf := func(email string) []any {
		for _, it := range items {
			m := it.(map[string]any)
			if m["email"] == email {
				return m["classNames"].([]any)
			}
		}
		t.Fatalf("没有 %s", email)
		return nil
	}
	if c := classesOf("b-stu@x.test"); len(c) != 1 || c[0] != "一班" {
		t.Fatalf("学生班级 = %v", c)
	}
	if c := classesOf("t@x.test"); len(c) != 1 || c[0] != "一班" {
		t.Fatalf("老师班级 = %v", c)
	}
	if c := classesOf("a-stu@x.test"); len(c) != 0 {
		t.Fatalf("无班级应为空数组 = %v", c)
	}
}

func TestDevImpersonate(t *testing.T) {
	e := newDevEnv(t)
	student := devUser(t, e, "s@x.test", "学生", core.RoleStudent)

	// scope=tab：不写 Cookie，返回 token，Bearer 能访问 /auth/me
	r := e.do("POST", "/api/dev/impersonate", `{"userId":"`+student.ID+`","scope":"tab"}`, nil)
	if r.Status != 200 {
		t.Fatalf("tab = %d %s", r.Status, errCode(r))
	}
	if len(r.Header.Values("Set-Cookie")) != 0 {
		t.Fatalf("scope=tab 不应写 Cookie：%v", r.Header.Values("Set-Cookie"))
	}
	data := r.Body["data"].(map[string]any)
	token, _ := data["token"].(string)
	if token == "" || data["user"].(map[string]any)["id"] != student.ID {
		t.Fatalf("tab data = %v", data)
	}
	me := e.do("GET", "/api/auth/me", "", map[string]string{"Authorization": "Bearer " + token})
	if me.Status != 200 || me.Body["data"].(map[string]any)["email"] != "s@x.test" {
		t.Fatalf("me(bearer) = %d %v", me.Status, me.Body)
	}

	// scope=browser：与正常登录一样写 Cookie，不返回 token
	r = e.do("POST", "/api/dev/impersonate", `{"userId":"`+student.ID+`","scope":"browser"}`, nil)
	if r.Status != 200 {
		t.Fatalf("browser = %d %s", r.Status, errCode(r))
	}
	cookie := cookieOf(t, r)
	if _, ok := r.Body["data"].(map[string]any)["token"]; ok {
		t.Fatalf("scope=browser 不应返回 token：%v", r.Body["data"])
	}
	if me := e.do("GET", "/api/auth/me", "", map[string]string{"Cookie": cookie}); me.Status != 200 {
		t.Fatalf("me(cookie) = %d", me.Status)
	}

	// 账号不存在 → 404；scope 非法 → 400；外站 Origin → 403
	if r := e.do("POST", "/api/dev/impersonate", `{"userId":"nope","scope":"tab"}`, nil); r.Status != 404 {
		t.Fatalf("不存在 = %d %s", r.Status, errCode(r))
	}
	if r := e.do("POST", "/api/dev/impersonate", `{"userId":"`+student.ID+`","scope":"all"}`, nil); r.Status != 400 {
		t.Fatalf("scope 非法 = %d %s", r.Status, errCode(r))
	}
	if r := e.do("POST", "/api/dev/impersonate", `{"userId":"`+student.ID+`","scope":"tab"}`, map[string]string{"Origin": "http://evil.example"}); r.Status != 403 {
		t.Fatalf("外站 Origin = %d", r.Status)
	}
}
