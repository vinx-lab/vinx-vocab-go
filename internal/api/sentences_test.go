package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"github.com/vinx-lab/vinx-vocab-go/internal/service"
)

func okData(t *testing.T, r resp) map[string]any {
	t.Helper()
	if r.Status != 200 {
		t.Fatalf("status = %d %s %v", r.Status, errCode(r), r.Body)
	}
	return r.Body["data"].(map[string]any)
}

func jsonBody(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// 老师建词书 + 单元 + 几个词，返回 cookie、bookID、unitID。
func setupTextBook(t *testing.T, e *testEnv, email string) (string, string, string) {
	t.Helper()
	cookie := signupAndCookie(t, e, email)
	h := map[string]string{"Cookie": cookie}
	r := e.do("POST", "/api/books/import", jsonBody(map[string]any{
		"newBook": map[string]any{"name": "句子词书 " + email},
		"units": []any{
			map[string]any{"name": "Unit 1", "entries": []any{
				map[string]any{"spelling": "I", "definition": "我"},
				map[string]any{"spelling": "find", "definition": "发现"},
				map[string]any{"spelling": "useful", "definition": "有用的"},
				map[string]any{"spelling": "be good at", "definition": "擅长"},
			}},
			map[string]any{"name": "Unit 2", "entries": []any{
				map[string]any{"spelling": "English", "definition": "英语"},
				map[string]any{"spelling": "card", "definition": "卡片"},
			}},
		},
	}), h)
	bookID := okData(t, r)["bookId"].(string)
	detail := okData(t, e.do("GET", "/api/books/"+bookID, "", h))
	unitID := detail["units"].([]any)[0].(map[string]any)["id"].(string)
	return cookie, bookID, unitID
}

func sentenceTexts(items []any) []string {
	var out []string
	for _, it := range items {
		s := it.(map[string]any)
		out = append(out, s["en"].(string))
	}
	return out
}

func TestUnitTextsCRUD(t *testing.T) {
	e := newEnv(t, nil)
	_ = signupAndCookie(t, e, "admin-first@x.test") // 第一个账号是管理员，之后的才是普通老师
	cookie, _, unitID := setupTextBook(t, e, "t-texts@x.test")
	h := map[string]string{"Cookie": cookie}

	// 新建句型清单
	r := e.do("POST", "/api/units/"+unitID+"/texts", jsonBody(map[string]any{
		"title": "重点句型", "kind": "list",
		"sentences": []any{
			map[string]any{"en": "I find English useful.", "cn": "我发现英语很有用。", "frame": "I find ... useful."},
			map[string]any{"en": "I am good at English.", "cn": "我擅长英语。"},
		},
	}), h)
	created := okData(t, r)
	textID := created["id"].(string)
	if created["kind"] != "list" || created["unitId"] != unitID || created["titleCn"] != nil {
		t.Fatalf("created = %v", created)
	}
	sents := created["sentences"].([]any)
	if !slices.Equal(sentenceTexts(sents), []string{"I find English useful.", "I am good at English."}) {
		t.Fatalf("sentences = %v", sents)
	}
	first := sents[0].(map[string]any)
	if first["frame"] != "I find ... useful." || first["source"] != "manual" {
		t.Errorf("first = %v", first)
	}
	words := first["words"].([]any)
	if len(words) != 4 { // I / find / English / useful
		t.Errorf("words = %v", words)
	}

	// 第二篇：课文
	r = e.do("POST", "/api/units/"+unitID+"/texts", jsonBody(map[string]any{
		"title": "My Day", "titleCn": "我的一天",
		"sentences": []any{
			map[string]any{"en": "I find a card.", "cn": "我找到一张卡片。"},
			map[string]any{"en": "It is useful.", "cn": "它很有用。", "paragraph": 1},
		},
	}), h)
	text2 := okData(t, r)
	text2ID := text2["id"].(string)
	if text2["kind"] != "text" || text2["sortOrder"].(float64) != 1 {
		t.Errorf("text2 = %v", text2)
	}

	// 列表按 sortOrder
	list := okData(t, e.do("GET", "/api/units/"+unitID+"/texts", "", h))
	items := list["items"].([]any)
	if len(items) != 2 || items[0].(map[string]any)["id"] != textID {
		t.Fatalf("list = %v", list)
	}

	// 排序
	r = e.do("PATCH", "/api/units/"+unitID+"/texts/order", jsonBody(map[string]any{"textIds": []string{text2ID, textID}}), h)
	if okData(t, r)["ordered"].(float64) != 2 {
		t.Fatalf("order = %v", r.Body)
	}
	items = okData(t, e.do("GET", "/api/units/"+unitID+"/texts", "", h))["items"].([]any)
	if items[0].(map[string]any)["id"] != text2ID {
		t.Errorf("排序后 = %v", items)
	}
	if r := e.do("PATCH", "/api/units/"+unitID+"/texts/order", jsonBody(map[string]any{"textIds": []string{textID}}), h); r.Status != 400 {
		t.Errorf("顺序不完整应 400：%d", r.Status)
	}

	// PATCH 标题 / kind
	r = e.do("PATCH", "/api/texts/"+textID, jsonBody(map[string]any{"title": "句型仿写", "titleCn": "仿写"}), h)
	if p := okData(t, r); p["title"] != "句型仿写" || p["titleCn"] != "仿写" || p["kind"] != "list" {
		t.Errorf("patch = %v", p)
	}

	// PUT 整体替换：保留第二句（带 id，改了英文），删除第一句，新增一句
	keepID := sents[1].(map[string]any)["id"].(string)
	dropID := sents[0].(map[string]any)["id"].(string)
	r = e.do("PUT", "/api/texts/"+textID+"/sentences", jsonBody(map[string]any{"sentences": []any{
		map[string]any{"en": "Cards are useful.", "cn": "卡片很有用。"},
		map[string]any{"id": keepID, "en": "I was good at English.", "cn": "我以前擅长英语。"},
	}}), h)
	replaced := okData(t, r)["sentences"].([]any)
	if !slices.Equal(sentenceTexts(replaced), []string{"Cards are useful.", "I was good at English."}) || replaced[1].(map[string]any)["id"] != keepID {
		t.Fatalf("replaced = %v", replaced)
	}
	var n int
	e.d.DB.QueryRow(`SELECT count(*) FROM "Sentence" WHERE "id" = ?`, dropID).Scan(&n)
	if n != 0 {
		t.Error("移出的句子没有其他引用时应删除")
	}
	// 关联重新计算：was → be good at
	var linked []string
	for _, w := range replaced[1].(map[string]any)["words"].([]any) {
		wm := w.(map[string]any)
		linked = append(linked, fmt.Sprintf("%v@%v", wm["form"], wm["position"]))
	}
	if !slices.Equal(linked, []string{"I@0", "was good at@1", "English@4"}) {
		t.Errorf("重新关联 = %v", linked)
	}
	// 别的篇里的句子 id 不能借用
	otherID := text2["sentences"].([]any)[0].(map[string]any)["id"].(string)
	r = e.do("PUT", "/api/texts/"+textID+"/sentences", jsonBody(map[string]any{"sentences": []any{
		map[string]any{"id": otherID, "en": "Hijack.", "cn": "劫持。"},
	}}), h)
	if r.Status != 400 {
		t.Errorf("借用别篇句子应 400：%d %s", r.Status, errCode(r))
	}

	// 删除篇：句子随之清理
	if r := e.do("DELETE", "/api/texts/"+text2ID, "", h); r.Status != 200 {
		t.Fatalf("delete = %d %s", r.Status, errCode(r))
	}
	e.d.DB.QueryRow(`SELECT count(*) FROM "Sentence" WHERE "id" = ?`, otherID).Scan(&n)
	if n != 0 {
		t.Error("删除篇后句子应清理")
	}
	if r := e.do("GET", "/api/texts/"+text2ID, "", h); r.Status != 404 {
		t.Errorf("不存在的路由或篇 = %d", r.Status)
	}
	if r := e.do("PATCH", "/api/texts/"+text2ID, `{"title":"x"}`, h); r.Status != 404 || errCode(r) != "NOT_FOUND 内容不存在" {
		t.Errorf("已删除的篇 = %d %s", r.Status, errCode(r))
	}

	// 删除单元：篇和句子一起清理
	if r := e.do("DELETE", "/api/units/"+unitID, "", h); r.Status != 200 {
		t.Fatalf("delete unit = %d", r.Status)
	}
	e.d.DB.QueryRow(`SELECT count(*) FROM "Sentence" WHERE "source" != 'example'`).Scan(&n)
	if n != 0 {
		t.Errorf("删除单元后残留句子 %d 条", n)
	}
}

func TestUnitTextsValidation(t *testing.T) {
	e := newEnv(t, nil)
	_ = signupAndCookie(t, e, "admin-v@x.test")
	cookie, _, unitID := setupTextBook(t, e, "t-v@x.test")
	h := map[string]string{"Cookie": cookie}
	r := e.do("POST", "/api/units/"+unitID+"/texts", `{"kind":"poem","sentences":[{"en":"","cn":"x"}]}`, h)
	if r.Status != 400 {
		t.Fatalf("校验 = %d", r.Status)
	}
	details := r.Body["error"].(map[string]any)["details"].(map[string]any)
	if details["title"] != "Required" || details["sentences.0.en"] != "英文不能为空" || details["kind"] == nil {
		t.Errorf("details = %v", details)
	}
	if r := e.do("POST", "/api/units/nope/texts", `{"title":"x","sentences":[]}`, h); r.Status != 404 || errCode(r) != "NOT_FOUND 单元不存在" {
		t.Errorf("不存在的单元 = %d %s", r.Status, errCode(r))
	}
}

func TestUnitTextsPermissions(t *testing.T) {
	e := newEnv(t, nil)
	_ = signupAndCookie(t, e, "admin-p@x.test")
	cookie, _, unitID := setupTextBook(t, e, "owner@x.test")
	other := signupAndCookie(t, e, "other@x.test")
	r := e.do("POST", "/api/units/"+unitID+"/texts", jsonBody(map[string]any{"title": "T", "sentences": []any{map[string]any{"en": "I find it.", "cn": "我找到了。"}}}), map[string]string{"Cookie": cookie})
	textID := okData(t, r)["id"].(string)

	// 别的老师：看不到这本词书 → 404；改 → 403
	oh := map[string]string{"Cookie": other}
	if r := e.do("GET", "/api/units/"+unitID+"/texts", "", oh); r.Status != 404 || errCode(r) != "NOT_FOUND 单元不存在" {
		t.Errorf("不可见词书 = %d %s", r.Status, errCode(r))
	}
	if r := e.do("POST", "/api/units/"+unitID+"/texts", `{"title":"x","sentences":[]}`, oh); r.Status != 403 {
		t.Errorf("别人的单元新建 = %d", r.Status)
	}
	if r := e.do("PATCH", "/api/texts/"+textID, `{"title":"x"}`, oh); r.Status != 403 {
		t.Errorf("别人的篇修改 = %d", r.Status)
	}
	if r := e.do("PUT", "/api/texts/"+textID+"/sentences", `{"sentences":[]}`, oh); r.Status != 403 {
		t.Errorf("别人的篇替换句子 = %d", r.Status)
	}
	if r := e.do("DELETE", "/api/texts/"+textID, "", oh); r.Status != 403 {
		t.Errorf("别人的篇删除 = %d", r.Status)
	}
	// 未登录
	if r := e.do("GET", "/api/units/"+unitID+"/texts", "", nil); r.Status != 401 {
		t.Errorf("未登录 = %d", r.Status)
	}
}

func TestUnitTextsImport(t *testing.T) {
	e := newEnv(t, nil)
	_ = signupAndCookie(t, e, "admin-i@x.test")
	cookie, _, unitID := setupTextBook(t, e, "t-imp@x.test")
	h := map[string]string{"Cookie": cookie}

	text := "[句型]\nI find ... useful. | 我发现……很有用。 | I find English cards useful.\n[课文]\nTitle: Cards | 卡片\nI find a card. | 我找到一张卡片。\n\nIt is useful. | 它很有用。\n"
	pv := okData(t, e.do("POST", "/api/units/"+unitID+"/texts/import/preview", jsonBody(map[string]any{"text": text}), h))
	texts := pv["texts"].([]any)
	if len(texts) != 2 {
		t.Fatalf("preview = %v", pv)
	}
	s0 := texts[0].(map[string]any)["sentences"].([]any)[0].(map[string]any)
	// Unit 1 只有 I / find / useful / be good at：English、card 在 Unit 2 → 超纲；词库里没有的 → 词库外
	oos := []string{}
	for _, w := range s0["outOfScope"].([]any) {
		oos = append(oos, w.(map[string]any)["spelling"].(string))
	}
	if !slices.Equal(oos, []string{"English", "card"}) {
		t.Errorf("超纲词 = %v", oos)
	}
	stats := pv["stats"].(map[string]any)
	if stats["texts"].(float64) != 2 || stats["sentences"].(float64) != 3 || stats["error"].(float64) != 0 {
		t.Errorf("stats = %v", stats)
	}

	// 正式导入：页面把（可能改过的）解析结果交回来
	r := e.do("POST", "/api/units/"+unitID+"/texts/import", jsonBody(map[string]any{"texts": []any{
		map[string]any{"title": "重点句型", "kind": "list", "sentences": []any{
			map[string]any{"en": "I find English cards useful.", "cn": "我发现……很有用。", "frame": "I find ... useful."},
		}},
		map[string]any{"title": "Cards", "titleCn": "卡片", "kind": "text", "sentences": []any{
			map[string]any{"en": "I find a card.", "cn": "我找到一张卡片。"},
			map[string]any{"en": "It is useful.", "cn": "它很有用。", "paragraph": 1},
		}},
	}}), h)
	res := okData(t, r)
	if res["texts"].(float64) != 2 || res["sentences"].(float64) != 3 {
		t.Fatalf("import = %v", res)
	}
	items := okData(t, e.do("GET", "/api/units/"+unitID+"/texts", "", h))["items"].([]any)
	if len(items) != 2 || items[1].(map[string]any)["title"] != "Cards" {
		t.Errorf("导入后 = %v", items)
	}
}

func TestBooksImportWithTexts(t *testing.T) {
	e := newEnv(t, nil)
	_ = signupAndCookie(t, e, "admin-b@x.test")
	cookie := signupAndCookie(t, e, "t-b@x.test")
	h := map[string]string{"Cookie": cookie}
	text := "Unit 1\nguitar /ɡɪˈtɑː(r)/ n. 吉他\n[句型]\nI play the guitar. | 我弹吉他。\nUnit 2\n[课文]\nTitle: Music\nMusic is fun. | 音乐很有趣。\n"
	pv := okData(t, e.do("POST", "/api/books/import/preview", jsonBody(map[string]any{"text": text}), h))
	units := pv["units"].([]any)
	if len(units) != 2 {
		t.Fatalf("preview units = %v", units)
	}
	u1 := units[0].(map[string]any)
	if len(u1["entries"].([]any)) != 1 || len(u1["texts"].([]any)) != 1 {
		t.Errorf("Unit 1 = %v", u1)
	}
	if st := pv["stats"].(map[string]any); st["sentences"].(float64) != 2 || st["entries"].(float64) != 1 {
		t.Errorf("stats = %v", st)
	}
	// 没有句型 / 课文的旧格式：响应里不出现新增字段（与旧版对拍一致）
	old := okData(t, e.do("POST", "/api/books/import/preview", jsonBody(map[string]any{"text": "guitar /ɡɪˈtɑː(r)/ n. 吉他\n"}), h))
	if _, ok := old["units"].([]any)[0].(map[string]any)["texts"]; ok {
		t.Error("没有句型时不应输出 texts")
	}
	if _, ok := old["stats"].(map[string]any)["sentences"]; ok {
		t.Error("没有句型时不应输出 stats.sentences")
	}

	r := e.do("POST", "/api/books/import", jsonBody(map[string]any{
		"newBook": map[string]any{"name": "带课文"},
		"units": []any{
			map[string]any{"name": "Unit 1", "entries": []any{map[string]any{"spelling": "guitar", "definition": "吉他"}},
				"texts": []any{map[string]any{"title": "重点句型", "kind": "list", "sentences": []any{map[string]any{"en": "I play the guitar.", "cn": "我弹吉他。"}}}}},
			map[string]any{"name": "Unit 2", "entries": []any{},
				"texts": []any{map[string]any{"title": "Music", "kind": "text", "sentences": []any{map[string]any{"en": "Music is fun.", "cn": "音乐很有趣。"}}}}},
		},
	}), h)
	res := okData(t, r)
	if res["units"].(float64) != 2 || res["unitsCreated"].(float64) != 2 || res["texts"].(float64) != 2 || res["sentences"].(float64) != 2 {
		t.Fatalf("import = %v", res)
	}
	detail := okData(t, e.do("GET", "/api/books/"+res["bookId"].(string), "", h))
	u2 := detail["units"].([]any)[1].(map[string]any)["id"].(string)
	items := okData(t, e.do("GET", "/api/units/"+u2+"/texts", "", h))["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["kind"] != "text" {
		t.Errorf("只有课文的单元 = %v", items)
	}
}

func TestWordSentencesAndAnalyze(t *testing.T) {
	e := newEnv(t, nil)
	_ = signupAndCookie(t, e, "admin-w@x.test")
	cookie, bookID, unitID := setupTextBook(t, e, "t-w@x.test")
	h := map[string]string{"Cookie": cookie}

	// 例句
	var findID string
	e.d.DB.QueryRow(`SELECT "id" FROM "Word" WHERE "spelling" = 'find'`).Scan(&findID)
	if r := e.do("PATCH", "/api/words/"+findID, `{"example":"I found my card.","exampleCn":"我找到了我的卡片。"}`, h); r.Status != 200 {
		t.Fatalf("patch word = %d %s", r.Status, errCode(r))
	}
	// 句型清单 + 课文
	okData(t, e.do("POST", "/api/units/"+unitID+"/texts", jsonBody(map[string]any{"title": "句型", "kind": "list", "sentences": []any{
		map[string]any{"en": "I find English useful.", "cn": "我发现英语有用。"},
	}}), h))
	okData(t, e.do("POST", "/api/units/"+unitID+"/texts", jsonBody(map[string]any{"title": "课文", "kind": "text", "sentences": []any{
		map[string]any{"en": "She finds a card.", "cn": "她找到一张卡片。"},
		map[string]any{"en": "Nothing here.", "cn": "这里没有。"},
	}}), h))

	ws := okData(t, e.do("GET", "/api/words/"+findID+"/sentences", "", h))
	group := func(k string) []string { return sentenceTexts(ws[k].([]any)) }
	if !slices.Equal(group("examples"), []string{"I found my card."}) ||
		!slices.Equal(group("patterns"), []string{"I find English useful."}) ||
		!slices.Equal(group("texts"), []string{"She finds a card."}) ||
		len(ws["passages"].([]any)) != 0 {
		t.Fatalf("word sentences = %v", ws)
	}
	pat := ws["patterns"].([]any)[0].(map[string]any)
	if src := pat["from"].(map[string]any); src["unitId"] != unitID || src["bookId"] != bookID || src["title"] != "句型" {
		t.Errorf("来源 = %v", src)
	}

	// 别的老师看不到这本词书里的词 → 404
	other := signupAndCookie(t, e, "t-w2@x.test")
	if r := e.do("GET", "/api/words/"+findID+"/sentences", "", map[string]string{"Cookie": other}); r.Status != 404 || errCode(r) != "NOT_FOUND 单词不存在" {
		t.Errorf("不可见的词 = %d %s", r.Status, errCode(r))
	}

	// analyze：带 unitId 时已知词 = 本册到当前单元为止
	an := okData(t, e.do("POST", "/api/sentences/analyze", jsonBody(map[string]any{"en": "I found English cards in Beijing.", "unitId": unitID}), h))
	spell := func(k string) []string {
		var out []string
		for _, w := range an[k].([]any) {
			out = append(out, w.(map[string]any)["spelling"].(string))
		}
		return out
	}
	if !slices.Equal(spell("words"), []string{"I", "find", "English", "card"}) || !slices.Equal(spell("outOfScope"), []string{"English", "card"}) {
		t.Errorf("analyze = %v", an)
	}
	var unknown []string
	for _, tk := range an["unknown"].([]any) {
		unknown = append(unknown, tk.(map[string]any)["text"].(string))
	}
	if !slices.Equal(unknown, []string{"in", "Beijing"}) || len(an["tokens"].([]any)) != 6 {
		t.Errorf("unknown = %v tokens = %v", unknown, an["tokens"])
	}
	if r := e.do("POST", "/api/sentences/analyze", `{"en":""}`, h); r.Status != 400 {
		t.Errorf("空句子 = %d", r.Status)
	}
	if r := e.do("POST", "/api/sentences/analyze", jsonBody(map[string]any{"en": "Hi.", "unitId": unitID}), map[string]string{"Cookie": other}); r.Status != 404 {
		t.Errorf("不可见单元 analyze = %d", r.Status)
	}
}

func TestPassageDetailSentences(t *testing.T) {
	e := newEnv(t, nil)
	cookie := signupAndCookie(t, e, "stu-p@x.test")
	h := map[string]string{"Cookie": cookie}
	var uid string
	e.d.DB.QueryRow(`SELECT "id" FROM "User" WHERE "email" = 'stu-p@x.test'`).Scan(&uid)
	e.d.DB.Exec(`INSERT INTO "Word" ("id","spelling","definition") VALUES ('w-run','run','跑')`)
	e.d.DB.Exec(`INSERT INTO "Passage" ("id","userId","title","body","bodyCn") VALUES ('p1',?,'T','I run. You run!','我跑。你跑！')`, uid)
	// 迁移前的旧短文没有句子：sentences 为空数组
	p := okData(t, e.do("GET", "/api/passages/p1", "", h))
	if s, ok := p["sentences"].([]any); !ok || len(s) != 0 {
		t.Fatalf("未拆分的短文 sentences = %v", p["sentences"])
	}
	// 管理员「重新关联全部句子」会顺带拆分；这里直接调回填
	if r := e.do("POST", "/api/sentences/relink", "", h); r.Status != 403 {
		t.Errorf("非管理员 relink = %d", r.Status)
	}
	if err := relinkForTest(e); err != nil {
		t.Fatal(err)
	}
	p = okData(t, e.do("GET", "/api/passages/p1", "", h))
	s := p["sentences"].([]any)
	if len(s) != 2 {
		t.Fatalf("sentences = %v", s)
	}
	s1 := s[1].(map[string]any)
	if s1["en"] != "You run!" || s1["cn"] != "你跑！" || s1["paragraph"].(float64) != 0 {
		t.Errorf("s1 = %v", s1)
	}
	w := s1["words"].([]any)
	if len(w) != 1 || w[0].(map[string]any)["wordId"] != "w-run" || w[0].(map[string]any)["position"].(float64) != 1 || w[0].(map[string]any)["form"] != "run" {
		t.Errorf("words = %v", w)
	}
	// 我的短文出现在单词的句子列表里（把 w-run 放进一本自己的词书，让它可见）
	e.d.DB.Exec(`INSERT INTO "Book" ("id","name","ownerId") VALUES ('b-run','B',?)`, uid)
	e.d.DB.Exec(`INSERT INTO "Unit" ("id","bookId","name") VALUES ('u-run','b-run','U')`)
	e.d.DB.Exec(`INSERT INTO "UnitWord" ("unitId","wordId") VALUES ('u-run','w-run')`)
	ws := okData(t, e.do("GET", "/api/words/w-run/sentences", "", h))
	if got := sentenceTexts(ws["passages"].([]any)); !slices.Equal(got, []string{"I run.", "You run!"}) {
		t.Errorf("我的短文 = %v", ws["passages"])
	}
	if from := ws["passages"].([]any)[0].(map[string]any)["from"].(map[string]any); from["passageId"] != "p1" || from["type"] != "passage" {
		t.Errorf("短文来源 = %v", from)
	}
	// 别人看不到我的短文句子
	other := signupAndCookie(t, e, "stu-p2@x.test")
	e.d.DB.Exec(`UPDATE "Book" SET "isSystem" = 1 WHERE "id" = 'b-run'`)
	ws2 := okData(t, e.do("GET", "/api/words/w-run/sentences", "", map[string]string{"Cookie": other}))
	if len(ws2["passages"].([]any)) != 0 {
		t.Errorf("别人的短文不应出现：%v", ws2["passages"])
	}
	// 删除短文：句子一起清理
	if r := e.do("DELETE", "/api/passages/p1", "", h); r.Status != 200 {
		t.Fatal(r.Status)
	}
	var n int
	e.d.DB.QueryRow(`SELECT count(*) FROM "Sentence"`).Scan(&n)
	if n != 0 {
		t.Errorf("删除短文后残留句子 %d", n)
	}
}

func TestSentencesRelinkAdmin(t *testing.T) {
	e := newEnv(t, nil)
	// 全新库的第一个账号是管理员
	r := e.do("POST", "/api/auth/signup", `{"email":"admin-r@x.test","password":"123456"}`, nil)
	admin := cookieOf(t, r)
	e.d.DB.Exec(`INSERT INTO "Word" ("id","spelling","definition","example") VALUES ('w-x','run','跑','I run.')`)
	res := okData(t, e.do("POST", "/api/sentences/relink", "", map[string]string{"Cookie": admin}))
	if res["sentences"].(float64) != 1 {
		t.Errorf("relink = %v", res)
	}
}

func relinkForTest(e *testEnv) error {
	ctx := context.Background()
	return e.d.DB.Tx(ctx, func(tx *sql.Tx) error {
		_, err := service.RelinkAllSentences(ctx, tx, e.now)
		return err
	})
}
