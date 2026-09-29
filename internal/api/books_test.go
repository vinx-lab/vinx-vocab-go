package api

import (
	"testing"
)

// 删除保护（旧 assertNotUsedByPlans）：HTTP 契约测试暂时覆盖不到，因为 Go 还没有 /plans 路由（A3）。
// 这里直接在库里插入 Plan + PlanUnit 模拟被生效计划引用的情况。
func TestBookAndUnitDeleteProtectedByActivePlan(t *testing.T) {
	e := newEnv(t, nil)
	cookie := signupAndCookie(t, e, "teacher-del@x.test")

	book := e.do("POST", "/api/books", `{"name":"受保护的词书"}`, map[string]string{"Cookie": cookie})
	bookID := book.Body["data"].(map[string]any)["id"].(string)
	unit := e.do("POST", "/api/books/"+bookID+"/units", `{"name":"U1"}`, map[string]string{"Cookie": cookie})
	unitID := unit.Body["data"].(map[string]any)["id"].(string)

	db := e.d.DB
	if _, err := db.Exec(`INSERT INTO "User" ("id","email","passwordHash","name","role") VALUES ('creator1','creator1@x','h','c','teacher')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO "Plan" ("id","name","creatorId") VALUES ('plan1','计划','creator1')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO "PlanUnit" ("planId","unitId") VALUES ('plan1', ?)`, unitID); err != nil {
		t.Fatal(err)
	}

	delUnit := e.do("DELETE", "/api/units/"+unitID, "", map[string]string{"Cookie": cookie})
	if delUnit.Status != 400 || errCode(delUnit) != "INVALID_ACTION 有 1 个未归档的学习计划正在使用，请先调整计划" {
		t.Fatalf("删除被引用的单元 = %d %s", delUnit.Status, errCode(delUnit))
	}
	delBook := e.do("DELETE", "/api/books/"+bookID, "", map[string]string{"Cookie": cookie})
	if delBook.Status != 400 || errCode(delBook) != "INVALID_ACTION 有 1 个未归档的学习计划正在使用，请先调整计划" {
		t.Fatalf("删除被引用的词书 = %d %s", delBook.Status, errCode(delBook))
	}

	// 计划归档后不再阻挡
	if _, err := db.Exec(`UPDATE "Plan" SET "status" = 'archived' WHERE "id" = 'plan1'`); err != nil {
		t.Fatal(err)
	}
	if r := e.do("DELETE", "/api/units/"+unitID, "", map[string]string{"Cookie": cookie}); r.Status != 200 {
		t.Fatalf("归档后应可删除单元 = %d %s", r.Status, errCode(r))
	}
	if r := e.do("DELETE", "/api/books/"+bookID, "", map[string]string{"Cookie": cookie}); r.Status != 200 {
		t.Fatalf("归档后应可删除词书 = %d %s", r.Status, errCode(r))
	}
}

// 导入请求体的结构错误（units 不是数组 / 缺失、entries 缺失）与 zod 风格 details 对齐。
func TestImportBodyValidation(t *testing.T) {
	e := newEnv(t, nil)
	cookie := signupAndCookie(t, e, "teacher-imp@x.test")

	missing := e.do("POST", "/api/books/import", `{"newBook":{"name":"x"}}`, map[string]string{"Cookie": cookie})
	if missing.Status != 400 {
		t.Fatalf("units 缺省 = %d %v", missing.Status, missing.Body)
	}
	details := missing.Body["error"].(map[string]any)["details"].(map[string]any)
	if details["units"] != "Required" {
		t.Fatalf("units 缺省 details = %v", details)
	}

	badEntries := e.do("POST", "/api/books/import", `{"newBook":{"name":"y"},"units":[{"name":"U1"}]}`, map[string]string{"Cookie": cookie})
	if badEntries.Status != 400 {
		t.Fatalf("entries 缺省 = %d %v", badEntries.Status, badEntries.Body)
	}
	details2 := badEntries.Body["error"].(map[string]any)["details"].(map[string]any)
	if details2["units.0.entries"] != "Required" {
		t.Fatalf("entries 缺省 details = %v", details2)
	}

	badSpelling := e.do("POST", "/api/books/import", `{"newBook":{"name":"z"},"units":[{"name":"U1","entries":[{"spelling":"","definition":"x"}]}]}`, map[string]string{"Cookie": cookie})
	if badSpelling.Status != 400 {
		t.Fatalf("spelling 为空 = %d %v", badSpelling.Status, badSpelling.Body)
	}
	details3 := badSpelling.Body["error"].(map[string]any)["details"].(map[string]any)
	if details3["units.0.entries.0.spelling"] != "拼写不能为空" {
		t.Fatalf("spelling 为空 details = %v", details3)
	}
}

// signupAndCookie 注册一个学生账号并提升为老师角色（直接改库，绕开没有的 /users 接口），返回登录 Cookie。
func signupAndCookie(t *testing.T, e *testEnv, email string) string {
	t.Helper()
	r := e.do("POST", "/api/auth/signup", `{"email":"`+email+`","password":"123456"}`, nil)
	if r.Status != 201 {
		t.Fatalf("signup %s = %d %v", email, r.Status, r.Body)
	}
	id := r.Body["data"].(map[string]any)["user"].(map[string]any)["id"].(string)
	if _, err := e.d.DB.Exec(`UPDATE "User" SET "role" = 'teacher' WHERE "id" = ?`, id); err != nil {
		t.Fatal(err)
	}
	login := e.do("POST", "/api/auth/login", `{"email":"`+email+`","password":"123456"}`, nil)
	if login.Status != 200 {
		t.Fatalf("login %s = %d %v", email, login.Status, login.Body)
	}
	return cookieOf(t, login)
}

// 导入请求体的类型错误与 oracle（zod）一致：出错的路径只报类型错误，不再叠加该路径及其子路径上的其他校验。
func TestImportBodyTypeErrors(t *testing.T) {
	e := newEnv(t, nil)
	cookie := signupAndCookie(t, e, "teacher-imp-type@x.test")
	cases := []struct {
		body string
		want map[string]any
	}{
		{`{"newBook":{"name":"x"},"units":"bad"}`, map[string]any{"units": "Expected array, received string"}},
		{`{"newBook":{"name":"x"},"units":[{"name":"U","entries":"bad"}]}`, map[string]any{"units.0.entries": "Expected array, received string"}},
		{`{"newBook":"bad","units":[]}`, map[string]any{"newBook": "Expected object, received string", "units": "没有可导入的单元"}},
		{`{"newBook":{"name":"x"},"units":[5]}`, map[string]any{"units.0": "Expected object, received number"}},
		{`{"bookId":5,"units":"bad"}`, map[string]any{"bookId": "Expected string, received number", "units": "Expected array, received string"}},
	}
	for _, c := range cases {
		r := e.do("POST", "/api/books/import", c.body, map[string]string{"Cookie": cookie})
		if r.Status != 400 {
			t.Fatalf("%s → %d %v", c.body, r.Status, r.Body)
		}
		got := r.Body["error"].(map[string]any)["details"]
		if mustJSON(got) != mustJSON(c.want) {
			t.Errorf("%s → details %s，期望 %s", c.body, mustJSON(got), mustJSON(c.want))
		}
	}
}
