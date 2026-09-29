package web

import (
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestSPAFallback(t *testing.T) {
	files := fstest.MapFS{
		"index.html":    {Data: []byte("<html>app</html>")},
		"assets/app.js": {Data: []byte("console.log(1)")},
		"favicon.svg":   {Data: []byte("<svg/>")},
	}
	h := Handler(files)
	cases := []struct {
		path   string
		status int
		body   string
	}{
		{"/", 200, "app"},
		{"/today", 200, "app"},
		{"/books/abc/units", 200, "app"},
		{"/assets/app.js", 200, "console.log"},
		{"/favicon.svg", 200, "<svg/>"},
		{"/assets/missing.js", 404, ""},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", c.path, nil))
		if w.Code != c.status || !strings.Contains(w.Body.String(), c.body) {
			t.Errorf("%s → %d %q", c.path, w.Code, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/assets/app.js", nil))
	if !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
		t.Error("assets 应长期缓存")
	}
	// 嵌入的占位页存在
	w = httptest.NewRecorder()
	Handler(Dist()).ServeHTTP(w, httptest.NewRequest("GET", "/login", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "<html") {
		t.Errorf("embedded index = %d", w.Code)
	}
}

// 没构建前端时 dist 里只有占位页：前端路由都回落到它；构建后 index.html 优先，且不缓存。
func TestPlaceholderAndIndexCache(t *testing.T) {
	only := fstest.MapFS{"placeholder.html": {Data: []byte("<html>placeholder</html>")}}
	w := httptest.NewRecorder()
	Handler(only).ServeHTTP(w, httptest.NewRequest("GET", "/today", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "placeholder") {
		t.Fatalf("placeholder = %d %q", w.Code, w.Body.String())
	}
	both := fstest.MapFS{
		"placeholder.html": {Data: []byte("<html>placeholder</html>")},
		"index.html":       {Data: []byte("<html>app</html>")},
	}
	w = httptest.NewRecorder()
	Handler(both).ServeHTTP(w, httptest.NewRequest("GET", "/today", nil))
	if !strings.Contains(w.Body.String(), "app") || w.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("index = %q cache=%q", w.Body.String(), w.Header().Get("Cache-Control"))
	}
}
