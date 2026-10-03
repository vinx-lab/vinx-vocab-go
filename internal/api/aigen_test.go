package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// spec 0005：学段、四个模板、逐句输出、生成后检查、句型 / 仿写草稿与保存、重写一句。

type fakeAICall struct{ System, User string }

type fakeAI struct {
	mu    sync.Mutex
	calls []fakeAICall
	reply func(system, user string) string
}

func (f *fakeAI) last() fakeAICall {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return fakeAICall{}
	}
	return f.calls[len(f.calls)-1]
}

func (f *fakeAI) set(reply func(system, user string) string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reply = reply
}

// useFakeAI 起一个假的 OpenAI 兼容服务（httptest，本机随机端口）并把 AI 配置指向它。
func useFakeAI(t *testing.T, e *testEnv) *fakeAI {
	t.Helper()
	f := &fakeAI{reply: func(string, string) string { return "OK" }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Messages []struct{ Role, Content string } `json:"messages"`
		}
		json.Unmarshal(raw, &body)
		var c fakeAICall
		for _, m := range body.Messages {
			if m.Role == "system" {
				c.System = m.Content
			} else if m.Role == "user" {
				c.User = m.Content
			}
		}
		f.mu.Lock()
		f.calls = append(f.calls, c)
		reply := f.reply
		f.mu.Unlock()
		out, _ := json.Marshal(map[string]any{"choices": []map[string]any{{"message": map[string]any{"content": reply(c.System, c.User)}}}})
		w.Header().Set("content-type", "application/json")
		w.Write(out)
	}))
	t.Cleanup(srv.Close)
	value, _ := json.Marshal(map[string]any{"provider": "openai", "baseUrl": srv.URL + "/v1", "model": "fake-model", "timeoutMs": 10_000})
	if _, err := e.d.DB.Exec(`INSERT INTO "AppSetting" ("key","value","updatedAt") VALUES ('ai',?,?)`, string(value), store.NewTime(time.Now())); err != nil {
		t.Fatal(err)
	}
	e.d.AIConfig.Invalidate()
	return f
}

// signupRole 注册并改成指定角色后重新登录。
func signupRole(t *testing.T, e *testEnv, email, role string) map[string]string {
	t.Helper()
	cookie := signupAndCookie(t, e, email)
	if role != "teacher" {
		if _, err := e.d.DB.Exec(`UPDATE "User" SET "role" = ? WHERE "email" = ?`, role, email); err != nil {
			t.Fatal(err)
		}
		login := e.do("POST", "/api/auth/login", `{"email":"`+email+`","password":"123456"}`, nil)
		cookie = cookieOf(t, login)
	}
	return map[string]string{"Cookie": cookie}
}

// waitJob 轮询任务直到结束，返回 data。
func waitJob(t *testing.T, e *testEnv, h map[string]string, jobID string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		v := okData(t, e.do("GET", "/api/ai/jobs/"+jobID, "", h))
		if v["status"] != "running" {
			return v
		}
		if time.Now().After(deadline) {
			t.Fatalf("job %s 没有结束", jobID)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func startJob(t *testing.T, e *testEnv, h map[string]string, path, body string) map[string]any {
	t.Helper()
	r := e.do("POST", path, body, h)
	id := okData(t, r)["jobId"].(string)
	job := waitJob(t, e, h, id)
	if job["status"] != "done" {
		t.Fatalf("%s job = %v", path, job)
	}
	return job["result"].(map[string]any)
}

func checksOf(t *testing.T, item any) map[string]any {
	t.Helper()
	c, ok := item.(map[string]any)["checks"].(map[string]any)
	if !ok {
		t.Fatalf("没有 checks：%v", item)
	}
	return c
}

func strList(v any) []string {
	out := []string{}
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}

func TestBookLevelPatchAndViews(t *testing.T) {
	e := newEnv(t, nil)
	cookie, bookID, _ := setupTextBook(t, e, "lv@x.test")
	h := map[string]string{"Cookie": cookie}
	if b := okData(t, e.do("GET", "/api/books/"+bookID, "", h)); b["level"] != nil {
		t.Fatalf("新词书 level = %v", b["level"])
	} else if _, ok := b["level"]; !ok {
		t.Fatal("词书详情应有 level 字段（null）")
	}
	r := okData(t, e.do("PATCH", "/api/books/"+bookID, `{"level":"primary"}`, h))
	if r["level"] != "primary" {
		t.Fatalf("PATCH level = %v", r)
	}
	if b := okData(t, e.do("GET", "/api/books/"+bookID, "", h)); b["level"] != "primary" {
		t.Fatalf("detail level = %v", b["level"])
	}
	list := okData(t, e.do("GET", "/api/books", "", h))["items"].([]any)
	found := false
	for _, it := range list {
		m := it.(map[string]any)
		if m["id"] == bookID {
			found = m["level"] == "primary"
		}
	}
	if !found {
		t.Fatalf("列表里的 level 不对：%v", list)
	}
	if r := e.do("PATCH", "/api/books/"+bookID, `{"level":"senior"}`, h); r.Status != 400 {
		t.Fatalf("非法学段 = %d", r.Status)
	}
	// 只改名字不动学段；null 清空
	okData(t, e.do("PATCH", "/api/books/"+bookID, `{"name":"改名"}`, h))
	if b := okData(t, e.do("GET", "/api/books/"+bookID, "", h)); b["level"] != "primary" {
		t.Fatalf("改名后 level = %v", b["level"])
	}
	if r := okData(t, e.do("PATCH", "/api/books/"+bookID, `{"level":null}`, h)); r["level"] != nil {
		t.Fatalf("清空 = %v", r)
	}
	// 别人的词书改不了
	other := signupRole(t, e, "lv-other@x.test", "teacher")
	if r := e.do("PATCH", "/api/books/"+bookID, `{"level":"exam"}`, other); r.Status != 403 {
		t.Fatalf("别人的词书 = %d", r.Status)
	}
}

func TestPromptSettingsFourItems(t *testing.T) {
	e := newEnv(t, nil)
	admin := signupRole(t, e, "admin-pr@x.test", "admin")
	v := okData(t, e.do("GET", "/api/settings/ai/prompts", "", admin))
	for _, k := range []string{"example", "passage", "pattern", "variant"} {
		item, ok := v[k].(map[string]any)
		if !ok || item["key"] != k || item["isDefault"] != true {
			t.Fatalf("%s = %v", k, v[k])
		}
		if !strings.Contains(item["current"].(string), "{学段}") {
			t.Errorf("%s 默认模板应保留占位符：%v", k, item["current"])
		}
	}
	v = okData(t, e.do("PUT", "/api/settings/ai/prompts", `{"pattern":"自定义句型模板","variant":"自定义仿写模板"}`, admin))
	if v["pattern"].(map[string]any)["current"] != "自定义句型模板" || v["variant"].(map[string]any)["isDefault"] != false {
		t.Fatalf("PUT = %v", v)
	}
	v = okData(t, e.do("DELETE", "/api/settings/ai/prompts/variant", "", admin))
	if v["variant"].(map[string]any)["isDefault"] != true || v["pattern"].(map[string]any)["isDefault"] != false {
		t.Fatalf("DELETE variant = %v", v)
	}
	if r := e.do("DELETE", "/api/settings/ai/prompts/bogus", "", admin); r.Status != 400 {
		t.Fatalf("DELETE bogus = %d", r.Status)
	}
	if r := e.do("PUT", "/api/settings/ai/prompts", `{"variant":"  "}`, admin); r.Status != 400 {
		t.Fatalf("空模板 = %d", r.Status)
	}
}

const patternReply = `[{"en":"I find it useful.","cn":"我觉得它有用。","frame":"I find ... useful."},
{"en":"I find English useful.","cn":"我觉得英语有用。"},
{"en":"I find it very very very very very very very very very very very very very very very very very very useful.","cn":"很长。"}]`

func TestPatternsFlow(t *testing.T) {
	e := newEnv(t, nil)
	f := useFakeAI(t, e)
	cookie, bookID, unitID := setupTextBook(t, e, "pat@x.test")
	h := map[string]string{"Cookie": cookie}

	// 预览：学段默认取词书（没设 → 初中），带上本单元的词
	p := okData(t, e.do("POST", "/api/ai/units/"+unitID+"/patterns/preview", `{"topic":"学习方法","count":6}`, h))
	prompt := p["prompt"].(string)
	if p["level"] != "junior" || !strings.Contains(prompt, "你是初中英语教材编辑") || !strings.Contains(prompt, "请写 6 个重点句型") ||
		!strings.Contains(prompt, "话题或语法点：学习方法") || !strings.Contains(prompt, "be good at —— 擅长") || strings.Contains(prompt, "{") {
		t.Fatalf("preview = %v", p)
	}
	if len(p["words"].([]any)) != 4 || p["count"].(float64) != 6 || p["topic"] != "学习方法" {
		t.Fatalf("preview words/count = %v", p)
	}
	// 话题默认从单元名预填
	if p := okData(t, e.do("POST", "/api/ai/units/"+unitID+"/patterns/preview", `{}`, h)); p["topic"] != "Unit 1" || p["count"].(float64) != 8 {
		t.Fatalf("默认 topic/count = %v", p)
	}
	okData(t, e.do("PATCH", "/api/books/"+bookID, `{"level":"primary"}`, h))
	if p := okData(t, e.do("POST", "/api/ai/units/"+unitID+"/patterns/preview", `{"topic":"x"}`, h)); p["level"] != "primary" || !strings.Contains(p["prompt"].(string), "你是小学英语教材编辑") {
		t.Fatalf("词书学段 = %v", p)
	}
	if p := okData(t, e.do("POST", "/api/ai/units/"+unitID+"/patterns/preview", `{"topic":"x","level":"exam"}`, h)); p["level"] != "exam" || !strings.Contains(p["prompt"].(string), "中考") {
		t.Fatalf("临时学段 = %v", p)
	}
	for _, bad := range []string{`{"topic":"x","count":3}`, `{"topic":"x","count":13}`, `{"topic":"x","level":"senior"}`, `{"topic":"   "}`} {
		if r := e.do("POST", "/api/ai/units/"+unitID+"/patterns", bad, h); r.Status != 400 {
			t.Errorf("%s = %d", bad, r.Status)
		}
	}

	// 生成：草稿只在任务结果里，带每句检查
	f.set(func(string, string) string { return patternReply })
	res := startJob(t, e, h, "/api/ai/units/"+unitID+"/patterns", `{"topic":"学习方法","count":4}`)
	if !strings.Contains(f.last().System, "重点句型") || !strings.Contains(f.last().User, "请写 4 个重点句型") || !strings.Contains(f.last().User, "你是小学英语") {
		t.Fatalf("发给 AI 的消息 = %+v", f.last())
	}
	if res["title"] != "Unit 1 重点句型" || res["level"] != "primary" || res["unitId"] != unitID {
		t.Fatalf("result = %v", res)
	}
	items := res["items"].([]any)
	if len(items) != 3 {
		t.Fatalf("items = %v", items)
	}
	if it := items[0].(map[string]any); it["frame"] != "I find ... useful." || it["en"] != "I find it useful." {
		t.Errorf("item0 = %v", it)
	}
	if c := checksOf(t, items[0]); c["warning"] != false || len(c["issues"].([]any)) != 0 {
		t.Errorf("item0 checks = %v", c)
	}
	if c := checksOf(t, items[1]); !slices.Equal(strList(c["outOfScope"]), []string{"English"}) || c["warning"] != true {
		t.Errorf("item1 超纲 = %v", c)
	}
	if c := checksOf(t, items[2]); c["tooLong"] != true || c["maxWords"].(float64) != 12 {
		t.Errorf("item2 太长 = %v", c)
	}
	// 自定义的可见提示词原样发出
	startJob(t, e, h, "/api/ai/units/"+unitID+"/patterns", `{"topic":"x","prompt":"我自己写的提示词"}`)
	if f.last().User != "我自己写的提示词" {
		t.Fatalf("user = %q", f.last().User)
	}

	// 保存：新建 list 篇，默认标题
	saved := okData(t, e.do("POST", "/api/ai/units/"+unitID+"/patterns/save", jsonBody(map[string]any{
		"sentences": []any{map[string]any{"en": "I find it useful.", "cn": "我觉得它有用。", "frame": "I find ... useful."}, map[string]any{"en": "Is it useful?", "cn": "它有用吗？"}},
	}), h))
	if saved["kind"] != "list" || saved["title"] != "Unit 1 重点句型" || len(saved["sentences"].([]any)) != 2 {
		t.Fatalf("saved = %v", saved)
	}
	s0 := saved["sentences"].([]any)[0].(map[string]any)
	if s0["source"] != "ai" || s0["frame"] != "I find ... useful." || len(s0["words"].([]any)) == 0 {
		t.Errorf("s0 = %v", s0)
	}
	var model, createdBy *string
	e.d.DB.QueryRow(`SELECT "model","createdById" FROM "Sentence" WHERE "id" = ?`, s0["id"]).Scan(&model, &createdBy)
	if model == nil || *model != "fake-model" || createdBy == nil {
		t.Errorf("model=%v createdBy=%v", model, createdBy)
	}
	textID := saved["id"].(string)
	// 追加到已有的句型清单：顺序接在后面
	app := okData(t, e.do("POST", "/api/ai/units/"+unitID+"/patterns/save", jsonBody(map[string]any{
		"textId": textID, "title": "忽略", "sentences": []any{map[string]any{"en": "I am good at English.", "cn": "我擅长英语。"}},
	}), h))
	if app["id"] != textID || app["title"] != "Unit 1 重点句型" || !slices.Equal(sentenceTexts(app["sentences"].([]any)), []string{"I find it useful.", "Is it useful?", "I am good at English."}) {
		t.Fatalf("append = %v", app)
	}
	// 自定义标题
	if r := okData(t, e.do("POST", "/api/ai/units/"+unitID+"/patterns/save", `{"title":"问路","sentences":[{"en":"Where is it?","cn":"在哪？"}]}`, h)); r["title"] != "问路" {
		t.Fatalf("title = %v", r)
	}
	// 校验：空句子、别的单元的篇、课文篇
	if r := e.do("POST", "/api/ai/units/"+unitID+"/patterns/save", `{"sentences":[]}`, h); r.Status != 400 {
		t.Errorf("空 = %d", r.Status)
	}
	if r := e.do("POST", "/api/ai/units/"+unitID+"/patterns/save", `{"textId":"nope","sentences":[{"en":"a","cn":"b"}]}`, h); r.Status != 400 {
		t.Errorf("不存在的篇 = %d %s", r.Status, errCode(r))
	}
	txt := okData(t, e.do("POST", "/api/units/"+unitID+"/texts", `{"title":"课文","kind":"text","sentences":[{"en":"Hi.","cn":"嗨。"}]}`, h))
	if r := e.do("POST", "/api/ai/units/"+unitID+"/patterns/save", jsonBody(map[string]any{"textId": txt["id"], "sentences": []any{map[string]any{"en": "a", "cn": "b"}}}), h); r.Status != 400 {
		t.Errorf("课文篇 = %d", r.Status)
	}
}

func TestPatternsPermissions(t *testing.T) {
	e := newEnv(t, nil)
	useFakeAI(t, e)
	_, _, unitID := setupTextBook(t, e, "pp-owner@x.test")
	other := signupRole(t, e, "pp-other@x.test", "teacher")
	student := signupRole(t, e, "pp-stu@x.test", "student")
	paths := []struct{ path, body string }{
		{"/api/ai/units/" + unitID + "/patterns/preview", `{"topic":"x"}`},
		{"/api/ai/units/" + unitID + "/patterns", `{"topic":"x"}`},
		{"/api/ai/units/" + unitID + "/patterns/save", `{"sentences":[{"en":"a","cn":"b"}]}`},
		{"/api/ai/variants/preview", `{"unitId":"` + unitID + `","pasted":"I like it.","modes":["replace"]}`},
		{"/api/ai/variants", `{"unitId":"` + unitID + `","pasted":"I like it.","modes":["replace"]}`},
		{"/api/ai/variants/save", `{"unitId":"` + unitID + `","sentences":[{"en":"a","cn":"b"}]}`},
	}
	for _, p := range paths {
		if r := e.do("POST", p.path, p.body, student); r.Status != 403 {
			t.Errorf("学生 %s = %d", p.path, r.Status)
		}
		if r := e.do("POST", p.path, p.body, other); r.Status != 403 {
			t.Errorf("别人的词书 %s = %d %s", p.path, r.Status, errCode(r))
		}
		if r := e.do("POST", p.path, p.body, nil); r.Status != 401 {
			t.Errorf("未登录 %s = %d", p.path, r.Status)
		}
	}
	// 系统词书：只有管理员
	e.d.DB.Exec(`UPDATE "Book" SET "isSystem" = 1, "ownerId" = NULL`)
	owner := signupRole(t, e, "pp-owner2@x.test", "teacher")
	if r := e.do("POST", "/api/ai/units/"+unitID+"/patterns/preview", `{"topic":"x"}`, owner); r.Status != 403 {
		t.Errorf("老师生成系统词书 = %d", r.Status)
	}
	admin := signupRole(t, e, "pp-admin@x.test", "admin")
	if r := e.do("POST", "/api/ai/units/"+unitID+"/patterns/preview", `{"topic":"x"}`, admin); r.Status != 200 {
		t.Errorf("管理员生成系统词书 = %d %s", r.Status, errCode(r))
	}
	// AI 未配置：生成不可用，预览可用
	e.d.DB.Exec(`DELETE FROM "AppSetting" WHERE "key" = 'ai'`)
	e.d.AIConfig.Invalidate()
	if r := e.do("POST", "/api/ai/units/"+unitID+"/patterns", `{"topic":"x"}`, admin); r.Status != 400 || !strings.Contains(errCode(r), "未配置 AI") {
		t.Errorf("AI 未配置 = %d %s", r.Status, errCode(r))
	}
}

func TestVariantsFlow(t *testing.T) {
	e := newEnv(t, nil)
	f := useFakeAI(t, e)
	cookie, _, unitID := setupTextBook(t, e, "var@x.test")
	h := map[string]string{"Cookie": cookie}
	list := okData(t, e.do("POST", "/api/units/"+unitID+"/texts", `{"title":"句型","kind":"list","sentences":[{"en":"I find making word cards useful.","cn":"我发现做单词卡很有用。"}]}`, h))
	sid := list["sentences"].([]any)[0].(map[string]any)["id"].(string)

	body := jsonBody(map[string]any{"unitId": unitID, "sentenceIds": []string{sid}, "pasted": "It is fun to learn English. | 学英语很有趣。\n\nI am good at English.", "modes": []string{"replace", "transform"}, "perItem": 2})
	p := okData(t, e.do("POST", "/api/ai/variants/preview", body, h))
	prompt := p["prompt"].(string)
	if p["level"] != "junior" || !strings.Contains(prompt, "你是初中英语老师") || !strings.Contains(prompt, "改造方式：替换") || !strings.Contains(prompt, "每个例句写 2 个变式") ||
		!strings.Contains(prompt, "1. I find making word cards useful. | 我发现做单词卡很有用。") || !strings.Contains(prompt, "3. I am good at English. | （中文请补上）") || !strings.Contains(prompt, "长度不超过 20 词") {
		t.Fatalf("preview = %v", p)
	}
	origins := p["origins"].([]any)
	if len(origins) != 3 || origins[0].(map[string]any)["id"] != sid || origins[1].(map[string]any)["id"] != nil {
		t.Fatalf("origins = %v", origins)
	}
	if len(p["words"].([]any)) != 4 {
		t.Fatalf("words = %v", p["words"])
	}
	for _, bad := range []map[string]any{
		{"unitId": unitID, "pasted": "a", "modes": []string{}},
		{"unitId": unitID, "pasted": "a", "modes": []string{"bogus"}},
		{"unitId": unitID, "modes": []string{"replace"}},
		{"unitId": unitID, "pasted": " \n ", "modes": []string{"replace"}},
		{"unitId": unitID, "pasted": "a", "modes": []string{"replace"}, "perItem": 6},
		{"unitId": unitID, "pasted": "a", "modes": []string{"replace"}, "vocab": "all"},
		{"unitId": unitID, "pasted": strings.Repeat("a\n", 11), "modes": []string{"replace"}},
		{"unitId": unitID, "pasted": strings.Repeat("a", 20*1024+1), "modes": []string{"replace"}},
		{"pasted": "a", "modes": []string{"replace"}},
	} {
		if r := e.do("POST", "/api/ai/variants/preview", jsonBody(bad), h); r.Status != 400 {
			t.Errorf("%v = %d", bad, r.Status)
		}
	}
	// 看不到的句子 → 404
	if r := e.do("POST", "/api/ai/variants/preview", jsonBody(map[string]any{"unitId": unitID, "sentenceIds": []string{"nope"}, "modes": []string{"replace"}}), h); r.Status != 404 {
		t.Errorf("不存在的句子 = %d", r.Status)
	}

	f.set(func(string, string) string {
		return `[{"origin":1,"en":"I find reading cards useful.","cn":"我发现读卡片有用。","change":"替换","note":"换了动名词"},
{"origin":1,"en":"Making word cards is useful.","cn":"做单词卡有用。","change":"转换","note":"改了主语"},
{"origin":3,"en":"I am good at English and card games.","cn":"我擅长英语和卡牌。","change":"扩展","note":"加了宾语"}]`
	})
	res := startJob(t, e, h, "/api/ai/variants", body)
	if !strings.Contains(f.last().System, "仿写") {
		t.Fatalf("system = %q", f.last().System)
	}
	if res["title"] != "Unit 1 句型仿写" || res["unitId"] != unitID {
		t.Fatalf("result = %v", res)
	}
	items := res["items"].([]any)
	if len(items) != 3 {
		t.Fatalf("items = %v", items)
	}
	i0, i1, i2 := items[0].(map[string]any), items[1].(map[string]any), items[2].(map[string]any)
	if i0["originId"] != sid || i0["originEn"] != "I find making word cards useful." || i0["change"] != "replace" || i0["note"] != "换了动名词" {
		t.Errorf("i0 = %v", i0)
	}
	if i2["originId"] != nil || i2["originEn"] != "I am good at English." || i2["origin"].(float64) != 3 {
		t.Errorf("i2 = %v", i2)
	}
	if c := checksOf(t, i0); c["structureDeviates"] != false || c["similarity"] == nil {
		t.Errorf("i0 结构 = %v", c)
	}
	if c := checksOf(t, i1); c["structureDeviates"] != true || c["warning"] != true {
		t.Errorf("i1 结构偏离 = %v", c)
	}
	// card 在 Unit 2，对 Unit 1 是超纲
	if c := checksOf(t, i2); !slices.Contains(strList(c["outOfScope"]), "card") {
		t.Errorf("i2 超纲 = %v", c)
	}

	// 保存
	saved := okData(t, e.do("POST", "/api/ai/variants/save", jsonBody(map[string]any{
		"unitId": unitID,
		"sentences": []any{
			map[string]any{"en": "I find reading cards useful.", "cn": "我发现读卡片有用。", "originId": sid, "variantNote": "换了动名词", "change": "replace"},
			map[string]any{"en": "I am good at English and card games.", "cn": "我擅长英语和卡牌。", "variantNote": "加了宾语", "change": "expand"},
		},
	}), h))
	if saved["kind"] != "list" || saved["title"] != "Unit 1 句型仿写" || len(saved["sentences"].([]any)) != 2 {
		t.Fatalf("saved = %v", saved)
	}
	var source, note string
	var origin *string
	e.d.DB.QueryRow(`SELECT "source","originId","variantNote" FROM "Sentence" WHERE "id" = ?`, saved["sentences"].([]any)[0].(map[string]any)["id"]).Scan(&source, &origin, &note)
	if source != "variant" || origin == nil || *origin != sid || note != "替换：换了动名词" {
		t.Errorf("source=%s origin=%v note=%q", source, origin, note)
	}
	e.d.DB.QueryRow(`SELECT "originId","variantNote" FROM "Sentence" WHERE "id" = ?`, saved["sentences"].([]any)[1].(map[string]any)["id"]).Scan(&origin, &note)
	if origin != nil || note != "扩展：加了宾语" {
		t.Errorf("pasted origin=%v note=%q", origin, note)
	}
	// 追加到已有的句型清单
	app := okData(t, e.do("POST", "/api/ai/variants/save", jsonBody(map[string]any{"unitId": unitID, "textId": list["id"], "sentences": []any{map[string]any{"en": "Do you find it useful?", "cn": "你觉得有用吗？", "originId": sid, "change": "transform"}}}), h))
	if len(app["sentences"].([]any)) != 2 {
		t.Fatalf("append = %v", app)
	}
	// originId 不存在 → 400
	if r := e.do("POST", "/api/ai/variants/save", jsonBody(map[string]any{"unitId": unitID, "sentences": []any{map[string]any{"en": "a", "cn": "b", "originId": "nope"}}}), h); r.Status != 400 {
		t.Errorf("originId 不存在 = %d", r.Status)
	}
	if r := e.do("POST", "/api/ai/variants/save", jsonBody(map[string]any{"unitId": unitID, "sentences": []any{map[string]any{"en": "a", "cn": "b", "change": "bogus"}}}), h); r.Status != 400 {
		t.Errorf("change 非法 = %d", r.Status)
	}
}

func TestVariantsTargetVocab(t *testing.T) {
	e := newEnv(t, nil)
	f := useFakeAI(t, e)
	cookie, bookID, unitID := setupTextBook(t, e, "vt@x.test")
	h := map[string]string{"Cookie": cookie}
	// 没有班级目标词书 → 没有可用的目标词
	if r := e.do("POST", "/api/ai/variants/preview", jsonBody(map[string]any{"unitId": unitID, "pasted": "I like it.", "modes": []string{"replace"}, "vocab": "target"}), h); r.Status != 404 || !strings.Contains(errCode(r), "NO_DATA") {
		t.Fatalf("没有目标 = %d %s", r.Status, errCode(r))
	}
	cls := okData(t, e.do("POST", "/api/classes", `{"name":"一班"}`, h))
	okData(t, e.do("PUT", "/api/classes/"+cls["id"].(string)+"/target-books", jsonBody(map[string]any{"bookIds": []string{bookID}}), h))
	p := okData(t, e.do("POST", "/api/ai/variants/preview", jsonBody(map[string]any{"unitId": unitID, "pasted": "I like it.", "modes": []string{"replace"}, "vocab": "target"}), h))
	// 目标词书包括 Unit 2 的词
	if len(p["words"].([]any)) != 6 || !strings.Contains(p["prompt"].(string), "card —— 卡片") {
		t.Fatalf("target words = %v", p)
	}
	// 目标词不算超纲
	f.set(func(string, string) string { return `[{"origin":1,"en":"I like card games.","cn":"我喜欢卡牌。","change":"替换","note":"x"}]` })
	res := startJob(t, e, h, "/api/ai/variants", jsonBody(map[string]any{"unitId": unitID, "pasted": "I like it.", "modes": []string{"replace"}, "vocab": "target"}))
	if c := checksOf(t, res["items"].([]any)[0]); len(c["outOfScope"].([]any)) != 0 {
		t.Fatalf("目标词不应超纲 = %v", c)
	}
}

func TestRewriteSentence(t *testing.T) {
	e := newEnv(t, nil)
	f := useFakeAI(t, e)
	cookie, _, unitID := setupTextBook(t, e, "rw@x.test")
	h := map[string]string{"Cookie": cookie}
	f.set(func(string, string) string { return "```json\n{\"en\":\"I find English useful.\",\"cn\":\"我觉得英语有用。\"}\n```" })
	r := okData(t, e.do("POST", "/api/ai/sentences/rewrite", jsonBody(map[string]any{
		"en": "I find the giraffe useful.", "cn": "我觉得长颈鹿有用。", "issues": []string{"超纲词：giraffe"}, "level": "primary", "kind": "pattern", "unitId": unitID,
	}), h))
	if r["en"] != "I find English useful." || r["cn"] != "我觉得英语有用。" {
		t.Fatalf("rewrite = %v", r)
	}
	if c := r["checks"].(map[string]any); !slices.Equal(strList(c["outOfScope"]), []string{"English"}) || c["maxWords"].(float64) != 12 {
		t.Errorf("checks = %v", c)
	}
	last := f.last()
	if !strings.Contains(last.User, "- 超纲词：giraffe") || !strings.Contains(last.User, "原句：I find the giraffe useful.") || !strings.Contains(last.User, "小学") || !strings.Contains(last.System, `"en"`) {
		t.Errorf("rewrite messages = %+v", last)
	}
	// 仿写带原句：检查结构相似度
	f.set(func(string, string) string { return `{"en":"Making cards is useful.","cn":"做卡片有用。"}` })
	r = okData(t, e.do("POST", "/api/ai/sentences/rewrite", jsonBody(map[string]any{"en": "x", "cn": "y", "issues": []string{}, "level": "junior", "kind": "variant", "unitId": unitID, "origin": "I find making word cards useful."}), h))
	if c := r["checks"].(map[string]any); c["structureDeviates"] != true {
		t.Errorf("variant rewrite checks = %v", c)
	}
	if !strings.Contains(f.last().User, "仿写的原句：I find making word cards useful.") {
		t.Errorf("user = %q", f.last().User)
	}
	// 不带单元的句型 / 仿写：不做超纲检查（老师没有学习记录，按他的记录算会全部超纲）
	f.set(func(string, string) string { return `{"en":"I find English useful.","cn":"我觉得英语有用。"}` })
	for _, kind := range []string{"pattern", "variant"} {
		r = okData(t, e.do("POST", "/api/ai/sentences/rewrite", jsonBody(map[string]any{"en": "x", "kind": kind, "level": "primary"}), h))
		if c := r["checks"].(map[string]any); len(strList(c["outOfScope"])) != 0 || c["maxWords"].(float64) != 12 {
			t.Errorf("%s 不带单元 checks = %v", kind, c)
		}
	}
	// 校验与权限
	for _, bad := range []string{`{"en":"","kind":"pattern"}`, `{"en":"x","kind":"bogus"}`, `{"en":"x","kind":"pattern","level":"senior"}`, `{"en":"x"}`} {
		if r := e.do("POST", "/api/ai/sentences/rewrite", bad, h); r.Status != 400 {
			t.Errorf("%s = %d", bad, r.Status)
		}
	}
	student := signupRole(t, e, "rw-stu@x.test", "student")
	if r := e.do("POST", "/api/ai/sentences/rewrite", `{"en":"x","kind":"pattern"}`, student); r.Status != 403 {
		t.Errorf("学生重写句型 = %d", r.Status)
	}
	// 不带单元的短文句：按学生自己的学习记录（还没学过 → 句中词库里的词算超纲）
	if r := okData(t, e.do("POST", "/api/ai/sentences/rewrite", `{"en":"I run.","cn":"我跑。","kind":"passage"}`, student)); !slices.Contains(strList(r["checks"].(map[string]any)["outOfScope"]), "English") {
		t.Errorf("学生重写短文句 checks = %v", r["checks"])
	}
	other := signupRole(t, e, "rw-other@x.test", "teacher")
	if r := e.do("POST", "/api/ai/sentences/rewrite", jsonBody(map[string]any{"en": "x", "kind": "pattern", "unitId": unitID}), other); r.Status != 403 {
		t.Errorf("别人的单元 = %d", r.Status)
	}
	// AI 回复解析不了
	f.set(func(string, string) string { return "抱歉" })
	if r := e.do("POST", "/api/ai/sentences/rewrite", `{"en":"x","kind":"pattern"}`, h); r.Status != 500 || !strings.Contains(errCode(r), "AI") {
		t.Errorf("解析失败 = %d %s", r.Status, errCode(r))
	}
}

func TestExamplesLevelAndChecks(t *testing.T) {
	e := newEnv(t, nil)
	f := useFakeAI(t, e)
	cookie, bookID, unitID := setupTextBook(t, e, "ex@x.test")
	h := map[string]string{"Cookie": cookie}
	okData(t, e.do("PATCH", "/api/books/"+bookID, `{"level":"exam"}`, h))
	p := okData(t, e.do("POST", "/api/ai/units/"+unitID+"/examples/preview", `{}`, h))
	if p["level"] != "exam" || !strings.Contains(p["prompt"].(string), "你是中考英语教材的例句编辑") || !strings.Contains(p["prompt"].(string), "8～20 词") {
		t.Fatalf("preview = %v", p)
	}
	if p := okData(t, e.do("POST", "/api/ai/units/"+unitID+"/examples/preview", `{"level":"primary"}`, h)); !strings.Contains(p["prompt"].(string), "你是小学英语") {
		t.Fatalf("临时学段 = %v", p)
	}
	// 新字段与旧字段名混用；useful 句子里带 Unit 2 的 card（超纲）
	f.set(func(string, string) string {
		return `[{"spelling":"I","en":"I like it.","cn":"我喜欢。"},{"spelling":"find","example":"I find a key.","exampleCn":"我找到钥匙。"},{"spelling":"useful","en":"The card is useful.","cn":"卡片有用。"}]`
	})
	res := startJob(t, e, h, "/api/ai/units/"+unitID+"/examples", `{"level":"primary"}`)
	if !strings.Contains(f.last().User, "你是小学英语") || !strings.Contains(f.last().System, `"en"`) {
		t.Fatalf("messages = %+v", f.last())
	}
	items := res["items"].([]any)
	if len(items) != 3 {
		t.Fatalf("items = %v / failed %v", items, res["failed"])
	}
	byWord := map[string]map[string]any{}
	for _, it := range items {
		byWord[it.(map[string]any)["spelling"].(string)] = it.(map[string]any)
	}
	if byWord["find"]["example"] != "I find a key." {
		t.Errorf("旧字段名 = %v", byWord["find"])
	}
	if c := checksOf(t, byWord["useful"]); !slices.Equal(strList(c["outOfScope"]), []string{"card"}) || c["error"] != false || c["maxWords"].(float64) != 12 {
		t.Errorf("useful checks = %v", c)
	}
	// 例句仍写进词条和例句句子
	var n int
	e.d.DB.QueryRow(`SELECT count(*) FROM "Sentence" WHERE "source" = 'example'`).Scan(&n)
	if n != 3 {
		t.Errorf("例句句子 = %d", n)
	}
	// 单词的例句：学段取所在词书
	var wordID string
	e.d.DB.QueryRow(`SELECT "id" FROM "Word" WHERE "spelling" = 'find'`).Scan(&wordID)
	if p := okData(t, e.do("POST", "/api/ai/words/"+wordID+"/example/preview", `{}`, h)); p["level"] != "exam" {
		t.Errorf("单词例句学段 = %v", p)
	}
}

func TestPassageSentencesAndLevel(t *testing.T) {
	e := newEnv(t, nil)
	f := useFakeAI(t, e)
	_, bookID, _ := setupTextBook(t, e, "ps-owner@x.test")
	e.d.DB.Exec(`UPDATE "Book" SET "isSystem" = 1, "level" = 'primary' WHERE "id" = ?`, bookID)
	stu := signupRole(t, e, "ps-stu@x.test", "student")
	var ids []string
	rows, _ := e.d.DB.Query(`SELECT "id" FROM "Word" WHERE "spelling" IN ('find','useful','English') ORDER BY "spelling"`)
	for rows.Next() {
		var id string
		rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	// 没有目标词书 → 初中
	p := okData(t, e.do("POST", "/api/ai/passages/preview", jsonBody(map[string]any{"wordIds": ids}), stu))
	if p["level"] != "junior" || !strings.Contains(p["prompt"].(string), "你是初中英语阅读材料编辑") || !strings.Contains(p["prompt"].(string), "80～140 词") {
		t.Fatalf("preview = %v", p)
	}
	// 目标词书里最高的学段
	okData(t, e.do("PUT", "/api/me/target-books", jsonBody(map[string]any{"bookIds": []string{bookID}}), stu))
	p = okData(t, e.do("POST", "/api/ai/passages/preview", jsonBody(map[string]any{"wordIds": ids}), stu))
	if p["level"] != "primary" || !strings.Contains(p["prompt"].(string), "你是小学英语阅读材料编辑") {
		t.Fatalf("目标学段 = %v", p)
	}
	if p := okData(t, e.do("POST", "/api/ai/passages/preview", jsonBody(map[string]any{"wordIds": ids, "level": "exam"}), stu)); p["level"] != "exam" {
		t.Fatalf("临时学段 = %v", p)
	}
	// 逐句输出：中英句数即使拆不对齐也直接按 AI 的句子保存
	f.set(func(string, string) string {
		return `{"title":"My Day","titleCn":"我的一天","sentences":[{"en":"I find English useful.","cn":"我发现英语有用。","paragraph":0},{"en":"Mr. Li says so, too.","cn":"李老师也这么说。","paragraph":0},{"en":"I find it fun every single day of the week and every single month.","cn":"我每天都觉得有趣。","paragraph":1}],"questions":[{"q":"谁说的？","a":"李老师"}]}`
	})
	res := startJob(t, e, stu, "/api/passages/generate", jsonBody(map[string]any{"wordIds": ids}))
	if res["level"] != "primary" || res["title"] != "My Day" || len(res["missingWords"].([]any)) != 0 {
		t.Fatalf("result = %v", res)
	}
	ss := res["sentences"].([]any)
	if len(ss) != 3 {
		t.Fatalf("sentences = %v", ss)
	}
	if c := checksOf(t, ss[2]); c["tooLong"] != true {
		t.Errorf("太长 = %v", c)
	}
	detail := okData(t, e.do("GET", "/api/passages/"+res["id"].(string), "", stu))
	if detail["passage"] != "I find English useful. Mr. Li says so, too.\n\nI find it fun every single day of the week and every single month." {
		t.Errorf("body = %q", detail["passage"])
	}
	ds := detail["sentences"].([]any)
	if len(ds) != 3 || ds[1].(map[string]any)["en"] != "Mr. Li says so, too." || ds[2].(map[string]any)["paragraph"].(float64) != 1 || ds[0].(map[string]any)["source"] != "ai" {
		t.Errorf("detail sentences = %v", ds)
	}
	// 旧格式回复仍可用（按句拆分）
	f.set(func(string, string) string {
		return `{"title":"Old","passage":"I find it. It is useful.","passageCn":"我找到了。它有用。","questions":[]}`
	})
	res = startJob(t, e, stu, "/api/passages/generate", jsonBody(map[string]any{"wordIds": ids}))
	detail = okData(t, e.do("GET", "/api/passages/"+res["id"].(string), "", stu))
	if len(detail["sentences"].([]any)) != 2 || len(res["sentences"].([]any)) != 2 {
		t.Errorf("旧格式 = %v / %v", detail["sentences"], res["sentences"])
	}
}
