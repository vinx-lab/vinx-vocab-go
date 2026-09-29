package auth

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestBcryptCompatWithBcryptjs(t *testing.T) {
	// 旧版 bcryptjs 生成的 dev123456 哈希（seed 代价 10、注册代价 12）
	for _, h := range []string{
		"$2b$10$kfwTt11pGj5aZLlAdtQp/uFOjx3iwUgMjh2Brx8QuCIJ3/G.3agmm",
		"$2b$12$wor1MYVV2JwFMkJwNFqXpuXlm4O47j/xnI8MGR22P5L7dsrS6RAxC",
	} {
		if !VerifyPassword("dev123456", h) || VerifyPassword("dev12345", h) {
			t.Errorf("bcryptjs 哈希校验失败：%s", h)
		}
	}
	h, _ := HashPasswordCost("abc123", 4)
	if !VerifyPassword("abc123", h) {
		t.Error("自产哈希校验失败")
	}
}

func reqWith(cookie, authz string) *http.Request {
	r := httptest.NewRequest("GET", "/auth/me", nil)
	if cookie != "" {
		r.AddCookie(&http.Cookie{Name: CookieName, Value: cookie})
	}
	if authz != "" {
		r.Header.Set("Authorization", authz)
	}
	return r
}

func TestVerifyMirrorsFastifyJwt(t *testing.T) {
	now := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	a := New("secret", 7*24*time.Hour, "auto", func() time.Time { return now })
	tok, err := a.Sign("u1", "a@x", "A", "student")
	if err != nil {
		t.Fatal(err)
	}
	// 载荷字段与有效期
	parts := strings.Split(tok, ".")
	pb, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var payload map[string]any
	json.Unmarshal(pb, &payload)
	if len(payload) != 6 || payload["exp"].(float64)-payload["iat"].(float64) != 7*24*3600 || payload["sub"] != "u1" {
		t.Fatalf("payload = %v", payload)
	}
	c, herr := a.Verify(reqWith(tok, ""))
	if herr != nil || c.Sub != "u1" || c.Role != "student" {
		t.Fatalf("verify = %+v %v", c, herr)
	}
	// Bearer 优先于 Cookie
	if _, herr := a.Verify(reqWith("garbage", "Bearer "+tok)); herr != nil {
		t.Fatalf("bearer = %v", herr)
	}

	other := New("other", time.Hour, "auto", func() time.Time { return now })
	forged, _ := other.Sign("u1", "a@x", "A", "admin")
	hs512, _ := jwt.NewWithClaims(jwt.SigningMethodHS512, Claims{Sub: "u1", Exp: now.Add(time.Hour).Unix()}).SignedString([]byte("secret"))
	cases := []struct {
		cookie, authz string
		status        int
		code, msg     string
	}{
		{"", "", 401, "UNAUTHORIZED", "未登录或登录已失效"},
		{"", "Basic abc", 401, "UNAUTHORIZED", "未登录或登录已失效"},
		{"", "Bearer a b", 400, "VALIDATION", "Format is Authorization: Bearer [token]"},
		{"not-a-jwt", "", 401, "VALIDATION", "Authorization token is invalid: The token is malformed."},
		{parts[0] + "." + parts[1], "", 401, "VALIDATION", "Authorization token is invalid: The token is malformed."},
		{"a.b.c", "", 401, "VALIDATION", "Authorization token is invalid: The token header is not a valid base64url serialized JSON."},
		{parts[0] + ".e30x.c", "", 401, "VALIDATION", "Authorization token is invalid: The token payload is not a valid base64url serialized JSON."},
		{forged, "", 401, "VALIDATION", "Authorization token is invalid: The token signature is invalid."},
	}
	for _, c := range cases {
		_, herr := a.Verify(reqWith(c.cookie, c.authz))
		if herr == nil || herr.Status != c.status || herr.Code != c.code || herr.Message != c.msg {
			t.Errorf("cookie=%q authz=%q → %+v", c.cookie, c.authz, herr)
		}
	}
	// 同一密钥的 HS512 令牌也接受（与 fast-jwt 对共享密钥允许 HS256/384/512 一致）
	if _, herr := a.Verify(reqWith(hs512, "")); herr != nil {
		t.Errorf("HS512 = %v", herr)
	}
	// 过期
	now = now.Add(7*24*time.Hour + time.Second)
	if _, herr := a.Verify(reqWith(tok, "")); herr == nil || herr.Status != 401 || herr.Message != "Authorization token expired" {
		t.Errorf("expired = %+v", herr)
	}
}

func TestCookies(t *testing.T) {
	a := New("s", time.Hour, "auto", nil)
	w := httptest.NewRecorder()
	a.SetCookie(w, httptest.NewRequest("POST", "/", nil), "tok")
	got := w.Header().Get("Set-Cookie")
	for _, want := range []string{"vinx_token=tok", "Path=/", "Max-Age=604800", "HttpOnly", "SameSite=Lax"} {
		if !strings.Contains(got, want) {
			t.Errorf("Set-Cookie %q 缺少 %s", got, want)
		}
	}
	if strings.Contains(got, "Secure") {
		t.Error("非 TLS 不应 Secure")
	}
	tlsReq := httptest.NewRequest("POST", "https://x/", nil)
	w = httptest.NewRecorder()
	a.SetCookie(w, tlsReq, "tok")
	if !strings.Contains(w.Header().Get("Set-Cookie"), "Secure") {
		t.Error("TLS 请求在 auto 下应 Secure")
	}
	w = httptest.NewRecorder()
	a.ClearCookie(w)
	if c := w.Header().Get("Set-Cookie"); !strings.Contains(c, "Max-Age=0") || !strings.Contains(c, "Expires=Thu, 01 Jan 1970") {
		t.Errorf("clear = %q", c)
	}
}
