package httpx

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

type loginBody struct {
	Email    Opt[string]  `json:"email"`
	Password Opt[string]  `json:"password"`
	Name     Opt[string]  `json:"name"`
	Grade    Opt[string]  `json:"currentGrade"`
	Theme    Opt[string]  `json:"theme"`
	Count    Opt[float64] `json:"count"`

	email, password string
}

func (b *loginBody) Validate(v *V) {
	b.email = v.Str("email", b.Email, Trim(), Email("邮箱格式不正确"), Lower())
	b.password = v.Str("password", b.Password, Min(1, "密码不能为空"))
	v.OptStr("name", b.Name, Min(1), Max(50))
	v.NullStr("currentGrade", b.Grade, Max(20))
	v.OptEnum("theme", b.Theme, []string{"system", "light", "dark"}, "", "")
	v.Int("count", b.Count, nil, Between(1, 20))
}

func req(method, ct, body string) *http.Request {
	r := httptest.NewRequest(method, "/x", strings.NewReader(body))
	if ct != "" {
		r.Header.Set("Content-Type", ct)
	}
	w := httptest.NewRecorder()
	return Prepare(w, r)
}

func details(t *testing.T, err error) map[string]any {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("期望 *Error，得到 %v", err)
	}
	if e.Code != CodeValidation || e.Message != "参数校验失败" {
		t.Fatalf("err = %+v", e)
	}
	return e.Details
}

func TestDecodeZodMessages(t *testing.T) {
	cases := []struct {
		name, ct, body string
		want           map[string]any
	}{
		{"空体", "", "", map[string]any{"email": "Required", "password": "Required", "count": "Required"}},
		{"null", "application/json", "null", map[string]any{"email": "Required", "password": "Required", "count": "Required"}},
		{"数组", "application/json", "[]", map[string]any{"body": "Expected object, received array"}},
		{"文本", "text/plain", "x", map[string]any{"body": "Expected object, received string"}},
		{"类型不符", "application/json", `{"email":5,"password":true,"count":"a"}`, map[string]any{"email": "Expected string, received number", "password": "Expected string, received boolean", "count": "Expected number, received string"}},
		{"null 字段", "application/json", `{"email":null,"password":"","count":1.5}`, map[string]any{"email": "Expected string, received null", "password": "密码不能为空", "count": "Expected integer, received float"}},
		{"检查", "application/json", `{"email":" a@b ","password":"x","name":"","currentGrade":"` + strings.Repeat("x", 21) + `","theme":"sepia","count":0}`, map[string]any{
			"email": "邮箱格式不正确", "name": "String must contain at least 1 character(s)", "currentGrade": "String must contain at most 20 character(s)",
			"theme": "Invalid enum value. Expected 'system' | 'light' | 'dark', received 'sepia'", "count": "Number must be greater than or equal to 1",
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Decode[loginBody](req("POST", c.ct, c.body))
			if got := details(t, err); !reflect.DeepEqual(got, c.want) {
				t.Errorf("details = %v\nwant %v", got, c.want)
			}
		})
	}

	b, err := Decode[loginBody](req("POST", "application/json", `{"email":"  A@B.CO ","password":"x","currentGrade":null,"count":3}`))
	if err != nil {
		t.Fatal(err)
	}
	if b.email != "a@b.co" || !b.Grade.Set || !b.Grade.Null || b.Name.Set {
		t.Errorf("decoded = %+v", b)
	}
}

func TestPrepareBodyErrors(t *testing.T) {
	cases := []struct {
		ct, body string
		status   int
		msg      string
	}{
		{"application/json", "", 400, "Body cannot be empty when content-type is set to 'application/json'"},
		{"application/json; charset=utf-8", "{bad", 400, "Body is not valid JSON but content-type is set to 'application/json'"},
		{"application/x-www-form-urlencoded", "x=1", 415, "Unsupported Media Type"},
		{"", "x", 415, "Unsupported Media Type"},
		{"application/json", `"` + strings.Repeat("x", BodyLimit) + `"`, 413, "Request body is too large"},
	}
	for _, c := range cases {
		r := httptest.NewRequest("POST", "/x", strings.NewReader(c.body))
		if c.ct != "" {
			r.Header.Set("Content-Type", c.ct)
		}
		w := httptest.NewRecorder()
		if Prepare(w, r) != nil {
			t.Fatalf("%s %q 应失败", c.ct, c.body[:min(len(c.body), 10)])
		}
		var env struct {
			Success bool
			Error   struct{ Code, Message, RequestID string }
		}
		json.Unmarshal(w.Body.Bytes(), &env)
		if w.Code != c.status || env.Error.Code != "VALIDATION" || env.Error.Message != c.msg || !strings.HasPrefix(env.Error.RequestID, "req-") {
			t.Errorf("%s: %d %s", c.ct, w.Code, w.Body.String())
		}
	}
	// GET 不解析请求体
	r := httptest.NewRequest("GET", "/x", nil)
	r.Header.Set("Content-Type", "application/json")
	if Prepare(httptest.NewRecorder(), r) == nil {
		t.Error("GET 不应检查请求体")
	}
}

func TestEnvelopes(t *testing.T) {
	Now = func() time.Time { return time.Date(2026, 9, 28, 5, 51, 26, 656000000, time.UTC) }
	defer func() { Now = time.Now }()

	w := httptest.NewRecorder()
	OK(w, map[string]string{"status": "ok"})
	if w.Body.String() != `{"success":true,"data":{"status":"ok"},"timestamp":"2026-09-28T05:51:26.656Z"}` || w.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Errorf("OK = %s", w.Body.String())
	}
	w = httptest.NewRecorder()
	OK(w, nil)
	if w.Body.String() != `{"success":true,"timestamp":"2026-09-28T05:51:26.656Z"}` {
		t.Errorf("OK(nil) = %s", w.Body.String())
	}
	w = httptest.NewRecorder()
	OK(w, List[int](nil))
	if !strings.Contains(w.Body.String(), `"data":{"items":[],"total":0}`) {
		t.Errorf("List = %s", w.Body.String())
	}
	w = httptest.NewRecorder()
	OK(w, map[string]string{"html": "<a>&"})
	if !strings.Contains(w.Body.String(), `"<a>&"`) {
		t.Errorf("不应转义 HTML：%s", w.Body.String())
	}

	r := req("GET", "", "")
	w = httptest.NewRecorder()
	Fail(w, r, NewError(CodeEmailExists, "该邮箱已注册", nil))
	if w.Code != 409 || !strings.Contains(w.Body.String(), `"error":{"code":"EMAIL_EXISTS","message":"该邮箱已注册","requestId":"req-`) {
		t.Errorf("Fail = %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	Fail(w, r, errors.New("constraint failed: UNIQUE constraint failed: User.email (2067)"))
	if w.Code != 409 || !strings.Contains(w.Body.String(), `"DUPLICATE","message":"数据已存在，唯一性冲突"`) {
		t.Errorf("unique = %s", w.Body.String())
	}
	w = httptest.NewRecorder()
	Fail(w, r, errors.New("boom"))
	if w.Code != 500 || !strings.Contains(w.Body.String(), `"SERVER","message":"服务器内部错误"`) {
		t.Errorf("500 = %s", w.Body.String())
	}
	if id := NextRequestID(); !strings.HasPrefix(id, "req-") {
		t.Error(id)
	}
}

type pageQuery struct {
	Page  Opt[string] `json:"page"`
	Limit Opt[string] `json:"limit"`
	Q     Opt[string] `json:"q"`

	page, limit int
}

func (q *pageQuery) Validate(v *V) {
	one, twenty := 1, 20
	q.page = v.CoerceInt("page", q.Page, &one, IntRange{Min: &one})
	q.limit = v.CoerceInt("limit", q.Limit, &twenty, Between(1, 100))
	v.OptStr("q", q.Q, Trim())
}

func TestDecodeQuery(t *testing.T) {
	q, err := DecodeQuery[pageQuery](httptest.NewRequest("GET", "/x?page=2&limit=50&extra=1", nil))
	if err != nil || q.page != 2 || q.limit != 50 {
		t.Fatalf("q=%+v err=%v", q, err)
	}
	q, err = DecodeQuery[pageQuery](httptest.NewRequest("GET", "/x", nil))
	if err != nil || q.page != 1 || q.limit != 20 {
		t.Fatalf("defaults q=%+v err=%v", q, err)
	}
	_, err = DecodeQuery[pageQuery](httptest.NewRequest("GET", "/x?page=abc&limit=500", nil))
	got := details(t, err)
	want := map[string]any{"page": "Expected number, received nan", "limit": "Number must be less than or equal to 100"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("details = %v", got)
	}
}
