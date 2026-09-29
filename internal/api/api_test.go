package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vinx-lab/vinx-vocab-go/internal/auth"
	"github.com/vinx-lab/vinx-vocab-go/internal/config"
	"github.com/vinx-lab/vinx-vocab-go/internal/core"
	"github.com/vinx-lab/vinx-vocab-go/internal/httpx"
	"github.com/vinx-lab/vinx-vocab-go/internal/service"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// testEnv 临时库 + 可调时钟的 API（流程测试的公共夹具，后续任务可复用）。
type testEnv struct {
	t    *testing.T
	d    *Deps
	h    http.Handler
	now  time.Time
	root *Router
}

func newEnv(t *testing.T, env map[string]string) *testEnv {
	t.Helper()
	dir := t.TempDir()
	cfg, err := config.Load(config.Options{DataDir: dir, Lookup: func(k string) (string, bool) { v, ok := env[k]; return v, ok }})
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(dir, "vinx.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	e := &testEnv{t: t, now: time.Date(2026, 9, 28, 15, 59, 0, 0, time.UTC)}
	e.d = NewDeps(db, cfg, func() time.Time { return e.now })
	e.h = Handler(e.d)
	return e
}

type resp struct {
	Status int
	Body   map[string]any
	Header http.Header
}

func (e *testEnv) do(method, path, body string, hdr map[string]string) resp {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	out := resp{Status: w.Code, Header: w.Header()}
	json.Unmarshal(w.Body.Bytes(), &out.Body)
	return out
}

func errCode(r resp) string {
	if e, ok := r.Body["error"].(map[string]any); ok {
		return e["code"].(string) + " " + e["message"].(string)
	}
	return ""
}

func cookieOf(t *testing.T, r resp) string {
	t.Helper()
	for _, c := range r.Header.Values("Set-Cookie") {
		if strings.HasPrefix(c, auth.CookieName+"=") {
			return strings.SplitN(c, ";", 2)[0]
		}
	}
	t.Fatal("no cookie")
	return ""
}

func TestFeatureGateEvaluatedPerRequest(t *testing.T) {
	e := newEnv(t, nil)
	// 模拟后续任务注册一个班级版专属路由
	api := NewRouter(e.d)
	api.Feature(core.FeatureClasses).Get("/classes", func(w http.ResponseWriter, r *http.Request) error {
		httpx.OK(w, "classes")
		return nil
	}, auth.RequireCap(core.CapClasses))
	h := func(method, path string) int {
		w := httptest.NewRecorder()
		api.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		return w.Code
	}
	if got := h("GET", "/classes"); got != 401 {
		t.Fatalf("班级版未登录应 401，得到 %d", got)
	}
	ctx := context.Background()
	set := func(ed string) {
		if _, err := e.d.DB.ExecContext(ctx, `INSERT INTO "AppSetting" ("key","value") VALUES ('edition', ?) ON CONFLICT("key") DO UPDATE SET "value"=excluded."value"`, `"`+ed+`"`); err != nil {
			t.Fatal(err)
		}
	}
	set("personal")
	if got := h("GET", "/classes"); got != 404 {
		t.Fatalf("切到个人版后应 404，得到 %d", got)
	}
	set("school")
	if got := h("GET", "/classes"); got != 401 {
		t.Fatalf("切回班级版后应 401，得到 %d", got)
	}
	// 方法不匹配 / 不规整路径 → 404 包络
	for _, c := range []struct{ m, p string }{{"POST", "/classes"}, {"GET", "/classes/"}, {"GET", "//classes"}, {"GET", "/a/../classes"}} {
		if got := h(c.m, c.p); got != 404 {
			t.Errorf("%s %s → %d", c.m, c.p, got)
		}
	}
	// VINX_EDITION 锁定时以环境变量为准
	e2 := newEnv(t, map[string]string{"VINX_EDITION": "personal"})
	st, _ := e2.d.Edition(ctx)
	if st.Edition != "personal" || !st.Locked {
		t.Fatalf("locked = %+v", st)
	}
}

func TestAuthFlowWithClock(t *testing.T) {
	e := newEnv(t, nil)
	r := e.do("POST", "/api/auth/signup", `{"email":"A@x.test","password":"123456"}`, nil)
	if r.Status != 201 {
		t.Fatalf("signup = %d %v", r.Status, r.Body)
	}
	user := r.Body["data"].(map[string]any)["user"].(map[string]any)
	if user["email"] != "a@x.test" || user["currentGrade"] != nil || user["createdAt"] != "2026-09-28T15:59:00.000Z" {
		t.Fatalf("user = %v", user)
	}
	if _, ok := user["currentGrade"]; !ok {
		t.Fatal("currentGrade 应为 null 而不是缺省")
	}
	cookie := cookieOf(t, r)
	if me := e.do("GET", "/api/auth/me", "", map[string]string{"Cookie": cookie}); me.Status != 200 {
		t.Fatalf("me = %d", me.Status)
	}
	// 7 天后令牌过期（固定时钟）
	e.now = e.now.Add(7*24*time.Hour + time.Second)
	me := e.do("GET", "/api/auth/me", "", map[string]string{"Cookie": cookie})
	if me.Status != 401 || errCode(me) != "VALIDATION Authorization token expired" {
		t.Fatalf("过期 = %d %s", me.Status, errCode(me))
	}
	// 外站 Origin 的写请求被拒；同主机放行
	r = e.do("POST", "/api/auth/login", `{"email":"a@x.test","password":"123456"}`, map[string]string{"Origin": "http://evil.example"})
	if r.Status != 403 || errCode(r) != "FORBIDDEN 非法请求来源" {
		t.Fatalf("origin = %d %s", r.Status, errCode(r))
	}
	r = e.do("POST", "/api/auth/login", `{"email":"a@x.test","password":"123456"}`, map[string]string{"Origin": "http://example.com"})
	if r.Status != 200 {
		t.Fatalf("同主机 origin = %d %s", r.Status, errCode(r))
	}
	// Secure：auto 且非 TLS → 不带 Secure
	if strings.Contains(strings.Join(r.Header.Values("Set-Cookie"), ";"), "Secure") {
		t.Fatal("非 TLS 不应带 Secure")
	}
}

func TestOriginAllowList(t *testing.T) {
	e := newEnv(t, map[string]string{"ALLOWED_ORIGINS": "http://app.example, https://b.example"})
	if r := e.do("POST", "/api/auth/logout", `{}`, map[string]string{"Origin": "http://app.example"}); r.Status != 401 {
		t.Fatalf("白名单来源应放行到认证（401），得到 %d", r.Status)
	}
	if r := e.do("POST", "/api/auth/logout", `{}`, map[string]string{"Origin": "http://c.example"}); r.Status != 403 {
		t.Fatalf("非白名单应 403，得到 %d", r.Status)
	}
	e2 := newEnv(t, map[string]string{"ALLOWED_ORIGINS": "*", "COOKIE_SECURE": "true"})
	if r := e2.do("POST", "/api/auth/logout", `{}`, map[string]string{"Origin": "http://c.example"}); r.Status != 401 {
		t.Fatalf("* 应不限来源，得到 %d", r.Status)
	}
	r := e2.do("POST", "/api/auth/signup", `{"email":"s@x.test","password":"123456"}`, nil)
	if !strings.Contains(strings.Join(r.Header.Values("Set-Cookie"), ";"), "Secure") {
		t.Fatal("COOKIE_SECURE=true 应带 Secure")
	}
}

func TestPersonalEditionSignup(t *testing.T) {
	e := newEnv(t, map[string]string{"VINX_EDITION": "personal"})
	r := e.do("POST", "/api/auth/signup", `{"email":"me@x.test","password":"123456","inviteCode":"DEMO01"}`, nil)
	if r.Status != 201 {
		t.Fatalf("个人版首个账号 = %d %s", r.Status, errCode(r))
	}
	user := r.Body["data"].(map[string]any)["user"].(map[string]any)
	if user["role"] != "admin" {
		t.Fatalf("个人版首个账号应为 admin：%v", user)
	}
	r = e.do("POST", "/api/auth/signup", `{"email":"two@x.test","password":"123456"}`, nil)
	if r.Status != 403 || errCode(r) != "FORBIDDEN 个人版只允许一个账号，请直接登录" {
		t.Fatalf("第二个账号 = %d %s", r.Status, errCode(r))
	}
	cfg := e.do("GET", "/api/config", "", nil)
	data := cfg.Body["data"].(map[string]any)
	if data["signupEnabled"] != false || data["edition"] != "personal" {
		t.Fatalf("config = %v", data)
	}
	// 全新安装（库里还没有任何账号）忽略 SIGNUP_ENABLED：否则照抄旧 .env 把它设成 false 的人，
	// 全新安装会连第一个管理员都建不出来（K44）。有账号之后才照常生效。
	e2 := newEnv(t, map[string]string{"SIGNUP_ENABLED": "false"})
	if r := e2.do("POST", "/api/auth/signup", `{"email":"first@x.test","password":"123456"}`, nil); r.Status != 201 {
		t.Fatalf("全新安装首个账号应忽略 SIGNUP_ENABLED=false = %d %s", r.Status, errCode(r))
	}
	if r := e2.do("POST", "/api/auth/signup", `{"email":"x@x.test","password":"123456"}`, nil); errCode(r) != "FORBIDDEN 当前未开放注册" {
		t.Fatalf("关闭注册 = %s", errCode(r))
	}
}

func TestSPAAndAPIMount(t *testing.T) {
	e := newEnv(t, nil)
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, httptest.NewRequest("GET", "/today", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "<html") {
		t.Fatalf("SPA = %d", w.Code)
	}
	for _, p := range []string{"/api", "/api/", "/api/nope", "/health"} {
		r := e.do("GET", p, "", nil)
		if p == "/health" {
			if r.Status != 200 || r.Body != nil {
				t.Errorf("/health 不在 /api 下，应走前端：%d", r.Status)
			}
			continue
		}
		if r.Status != 404 || errCode(r) != "NOT_FOUND 接口不存在" {
			t.Errorf("%s → %d %s", p, r.Status, errCode(r))
		}
	}
	if r := e.do("HEAD", "/api/health", "", nil); r.Status != 200 || r.Header.Get(InstanceHeader) == "" {
		t.Errorf("HEAD health = %d", r.Status)
	}
}

func TestServiceAccess(t *testing.T) {
	e := newEnv(t, nil)
	ctx := context.Background()
	db := e.d.DB
	mk := func(id, role string) {
		db.Exec(`INSERT INTO "User" ("id","email","passwordHash","name","role") VALUES (?,?,?,?,?)`, id, id+"@x", "h", id, role)
	}
	mk("t1", "teacher")
	mk("t2", "teacher")
	mk("s1", "student")
	mk("a1", "admin")
	db.Exec(`INSERT INTO "Classroom" ("id","name","teacherId","inviteCode") VALUES ('c1','班','t1','AAAAAA')`)
	db.Exec(`INSERT INTO "ClassMember" ("classId","userId") VALUES ('c1','s1')`)
	db.Exec(`INSERT INTO "Book" ("id","name","isSystem","ownerId") VALUES ('sys','系统',1,NULL),('bt1','老师1',0,'t1'),('bt2','老师2',0,'t2')`)
	t1 := &service.Actor{ID: "t1", Role: "teacher"}
	t2 := &service.Actor{ID: "t2", Role: "teacher"}
	s1 := &service.Actor{ID: "s1", Role: "student"}
	a1 := &service.Actor{ID: "a1", Role: "admin"}

	if err := service.AssertCanManageClass(ctx, db, t2, "c1"); errCodeOf(err) != "FORBIDDEN" {
		t.Errorf("t2 管理 c1 = %v", err)
	}
	if err := service.AssertCanManageClass(ctx, db, a1, "nope"); errCodeOf(err) != "NOT_FOUND" {
		t.Errorf("不存在的班级 = %v", err)
	}
	if err := service.AssertCanViewUser(ctx, db, t1, "s1"); err != nil {
		t.Errorf("班主任看学生 = %v", err)
	}
	if err := service.AssertCanViewUser(ctx, db, t2, "s1"); errCodeOf(err) != "FORBIDDEN" {
		t.Errorf("别班老师看学生 = %v", err)
	}
	if err := service.AssertCanViewUser(ctx, db, s1, "t1"); errCodeOf(err) != "FORBIDDEN" {
		t.Errorf("学生看他人 = %v", err)
	}
	ids, _ := service.ManageableClassIDs(ctx, db, a1)
	if ids != nil {
		t.Error("管理员应不限")
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
	if got := visible(s1); got != "bt1,sys" {
		t.Errorf("学生可见 = %s", got)
	}
	if got := visible(t2); got != "bt2,sys" {
		t.Errorf("t2 可见 = %s", got)
	}
	if got := visible(a1); got != "bt1,bt2,sys" {
		t.Errorf("管理员可见 = %s", got)
	}
	if _, err := service.AssertCanEditBook(ctx, db, t1, "sys"); errCodeOf(err) != "FORBIDDEN" {
		t.Errorf("老师改系统词书 = %v", err)
	}
	if _, err := service.AssertCanEditBook(ctx, db, t1, "bt1"); err != nil {
		t.Errorf("老师改自己的词书 = %v", err)
	}
	if _, err := service.AssertCanEditBook(ctx, db, s1, "bt1"); errCodeOf(err) != "FORBIDDEN" {
		t.Errorf("学生改词书 = %v", err)
	}
	if ok, _ := service.IsLearnerOnly(ctx, db, "t1"); ok {
		t.Error("老师不是学习者")
	}
}

func errCodeOf(err error) string {
	if e, ok := err.(*httpx.Error); ok {
		return e.Code
	}
	return ""
}

// I3 + M2：处理函数在事务里误用 d.DB → 立即 500 包络（不卡 busy_timeout）；panic 不会断开连接。
func TestRouterTxGuardAndRecover(t *testing.T) {
	e := newEnv(t, nil)
	api := NewRouter(e.d)
	api.Post("/misuse", func(w http.ResponseWriter, r *http.Request) error {
		ctx := r.Context()
		return e.d.DB.Tx(ctx, func(tx *sql.Tx) error {
			_, err := e.d.DB.ExecContext(ctx, `INSERT INTO "User" ("id","email","passwordHash","name") VALUES ('x','x@x','h','x')`)
			return err
		})
	})
	api.Get("/nil-actor", func(w http.ResponseWriter, r *http.Request) error {
		httpx.OK(w, auth.ActorFrom(r.Context()).ID) // 没挂守卫：nil 解引用
		return nil
	})
	api.Get("/must-actor", func(w http.ResponseWriter, r *http.Request) error {
		a, err := auth.MustActor(r.Context())
		if err != nil {
			return err
		}
		httpx.OK(w, a.ID)
		return nil
	})
	call := func(method, path string) (int, string) {
		w := httptest.NewRecorder()
		api.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		return w.Code, w.Body.String()
	}
	start := time.Now()
	code, body := call("POST", "/misuse")
	if code != 500 || !strings.Contains(body, `"code":"SERVER"`) || time.Since(start) > 2*time.Second {
		t.Fatalf("misuse = %d %s（%v）", code, body, time.Since(start))
	}
	var n int
	e.d.DB.QueryRow(`SELECT count(*) FROM "User"`).Scan(&n)
	if n != 0 {
		t.Fatal("误用的事务应回滚")
	}
	if code, body := call("GET", "/nil-actor"); code != 500 || !strings.Contains(body, `"requestId":"req-`) {
		t.Fatalf("nil-actor = %d %s", code, body)
	}
	if code, body := call("GET", "/must-actor"); code != 401 || !strings.Contains(body, "未登录或登录已失效") {
		t.Fatalf("must-actor = %d %s", code, body)
	}
}
