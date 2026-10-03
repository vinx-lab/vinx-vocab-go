package api

import (
	"encoding/json"
	"slices"
	"testing"
	"time"
)

// 默写单（spec 0006）：出题（题型、来源、要学优先）、明细（提示与答案）、批改（自批标记、只能提交一次、
// 写入 Answer / SentenceAnswer、K10 / K19 记忆更新）、句子状态、错题再出一份、权限。

// newDictEnv 在 newCovEnv 的基础上：w5 改为短语；单元 u1a（b1）有一篇句型清单 tx1（带骨架句 sx1、普通句 sx2、
// 转换仿写 sxv，原句 sx2）和一篇课文 tx2（sx3）；s1、s2 各有一篇 AI 短文（ps1：sp1；ps2：sp2）；
// t2 私有词书 bt2 的单元 ut2 有一篇 txp（sxp）。
func newDictEnv(t *testing.T) *covEnv {
	c := newCovEnv(t, nil)
	c.exec(`UPDATE "Word" SET "type" = 'phrase' WHERE "id" = 'w5'`)
	c.exec(`UPDATE "Word" SET "partOfSpeech" = 'n.' WHERE "id" = 'w1'`)
	c.exec(`INSERT INTO "Sentence" ("id","en","cn","frame","source","originId","variantNote") VALUES
		('sx1','I find English useful.','我发现英语很有用。','I find ... useful.','manual',NULL,NULL),
		('sx2','I am good at English.','我擅长英语。',NULL,'manual',NULL,NULL),
		('sxv','Are you good at English?','你擅长英语吗？',NULL,'variant','sx2','转换：改成一般疑问句'),
		('sx3','It is a card.','这是一张卡片。',NULL,'manual',NULL,NULL),
		('sp1','I run fast.','我跑得快。',NULL,'ai',NULL,NULL),
		('sp2','She runs.','她跑步。',NULL,'ai',NULL,NULL),
		('sxp','Private one.','私有句子。',NULL,'manual',NULL,NULL)`)
	c.exec(`INSERT INTO "UnitText" ("id","unitId","kind","title","sortOrder") VALUES ('tx1','u1a','list','重点句型',0),('tx2','u1a','text','课文',1),('txp','ut2','text','私有',0)`)
	c.exec(`INSERT INTO "UnitTextSentence" ("textId","sentenceId","sortOrder") VALUES ('tx1','sx1',0),('tx1','sx2',1),('tx1','sxv',2),('tx2','sx3',0),('txp','sxp',0)`)
	c.exec(`INSERT INTO "Passage" ("id","userId","title","body") VALUES ('ps1',?,'跑步','I run fast.'),('ps2',?,'她','She runs.')`, c.s1ID, c.s2ID)
	c.exec(`INSERT INTO "PassageSentence" ("passageId","sentenceId","sortOrder") VALUES ('ps1','sp1',0),('ps2','sp2',0)`)
	return c
}

func listOf(v any) []map[string]any {
	out := []map[string]any{}
	for _, x := range v.([]any) {
		out = append(out, x.(map[string]any))
	}
	return out
}

func fieldOf(items []map[string]any, key string) []string {
	out := []string{}
	for _, it := range items {
		s, _ := it[key].(string)
		out = append(out, s)
	}
	return out
}

func TestDictationPreview(t *testing.T) {
	c := newDictEnv(t)
	// sx2 之前在一次已完成的默写里写错过 → 要学的句子
	c.exec(`INSERT INTO "StudySession" ("id","userId","kind","status","dayKey","snapshot","startedAt","completedAt","completedDay")
		VALUES ('old',?,'sheet','completed','2026-10-01','{"modes":["dictation"],"items":[]}','2026-10-01T00:00:00.000Z','2026-10-01T00:00:00.000Z','2026-10-01')`, c.s1ID)
	c.exec(`INSERT INTO "SentenceAnswer" ("id","sessionId","userId","sentenceId","itemType","correct","dayKey","createdAt") VALUES ('sa0','old',?,'sx2','sentence',0,'2026-10-01','2026-10-01T00:00:00.000Z')`, c.s1ID)

	// 自测格式不变：没有 sentences 字段
	plain := okData(t, c.do("POST", "/api/sheets/preview", `{"count":10,"source":{"kind":"book","bookId":"b2"}}`, c.s1))
	if _, ok := plain["sentences"]; ok {
		t.Fatalf("selftest preview should not have sentences: %v", plain)
	}
	if _, ok := listOf(plain["items"])[0]["type"]; ok {
		t.Fatalf("selftest preview item should not have type: %v", plain)
	}

	r := c.do("POST", "/api/sheets/preview", jsonBody(map[string]any{
		"format": "dictation", "source": map[string]any{"kind": "book", "bookId": "b2"},
		"sentenceSources": []any{map[string]any{"kind": "text", "textId": "tx1"}, map[string]any{"kind": "text", "textId": "tx2"}},
	}), c.s1)
	data := okData(t, r)
	items := listOf(data["items"])
	if !slices.Equal(fieldOf(items, "wordId"), []string{"w4", "w5"}) || !slices.Equal(fieldOf(items, "type"), []string{"word", "phrase"}) {
		t.Fatalf("items = %v", items)
	}
	sents := listOf(data["sentences"])
	// 要学的 sx2 优先，其次未测的按来源顺序
	if !slices.Equal(fieldOf(sents, "sentenceId"), []string{"sx2", "sx1", "sxv", "sx3"}) {
		t.Fatalf("sentences = %v", sents)
	}
	if !slices.Equal(fieldOf(sents, "type"), []string{"sentence", "frame", "transform", "sentence"}) ||
		!slices.Equal(fieldOf(sents, "status"), []string{"learning", "untested", "untested", "untested"}) {
		t.Fatalf("sentences = %v", sents)
	}
	if sents[1]["prompt"] != "I find ___ useful.（我发现英语很有用。）" || sents[2]["prompt"] != "I am good at English.（改成一般疑问句）" || sents[2]["answer"] != "Are you good at English?" {
		t.Fatalf("prompts = %v", sents)
	}

	// 不要短语、句子数量上限 2（× 1 份）
	data = okData(t, c.do("POST", "/api/sheets/preview", jsonBody(map[string]any{
		"format": "dictation", "source": map[string]any{"kind": "book", "bookId": "b2"}, "includePhrases": false, "sentenceCount": 2,
		"sentenceSources": []any{map[string]any{"kind": "text", "textId": "tx1"}},
	}), c.s1))
	if !slices.Equal(fieldOf(listOf(data["items"]), "wordId"), []string{"w4"}) || !slices.Equal(fieldOf(listOf(data["sentences"]), "sentenceId"), []string{"sx2", "sx1"}) {
		t.Fatalf("limited = %v", data)
	}
	// 两份：句子上限 2 × 2
	data = okData(t, c.do("POST", "/api/sheets/preview", jsonBody(map[string]any{
		"format": "dictation", "includeWords": false, "includePhrases": false, "sentenceCount": 2, "copies": 2,
		"sentenceSources": []any{map[string]any{"kind": "text", "textId": "tx1"}, map[string]any{"kind": "text", "textId": "tx2"}},
	}), c.s1))
	if len(listOf(data["items"])) != 0 || len(listOf(data["sentences"])) != 4 {
		t.Fatalf("copies = %v", data)
	}

	// 要学的句子、自己的 AI 短文
	data = okData(t, c.do("POST", "/api/sheets/preview", `{"format":"dictation","includeWords":false,"includePhrases":false,"sentenceSources":[{"kind":"learning"},{"kind":"passage","passageId":"ps1"}]}`, c.s1))
	if !slices.Equal(fieldOf(listOf(data["sentences"]), "sentenceId"), []string{"sx2", "sp1"}) {
		t.Fatalf("learning+passage = %v", data)
	}
	// 别人的短文、看不到的词书的篇
	if r := c.do("POST", "/api/sheets/preview", `{"format":"dictation","sentenceSources":[{"kind":"passage","passageId":"ps2"}]}`, c.s1); r.Status != 403 {
		t.Fatalf("other passage = %d %v", r.Status, r.Body)
	}
	// bt2 是 t2 的私有词书：s1 在 t2 的班里能用，s2 不在
	if r := c.do("POST", "/api/sheets/preview", `{"format":"dictation","sentenceSources":[{"kind":"text","textId":"txp"}]}`, c.s1); r.Status != 200 {
		t.Fatalf("class teacher text = %d %v", r.Status, r.Body)
	}
	if r := c.do("POST", "/api/sheets/preview", `{"format":"dictation","sentenceSources":[{"kind":"text","textId":"txp"}]}`, c.s2); r.Status != 404 {
		t.Fatalf("private text = %d %v", r.Status, r.Body)
	}
	// 校验
	r = c.do("POST", "/api/sheets/preview", `{"format":"x","sentenceSources":[{"kind":"text"},{"kind":"bad"}],"sentenceCount":21}`, c.s1)
	if r.Status != 400 {
		t.Fatalf("validation = %d", r.Status)
	}
	details := r.Body["error"].(map[string]any)["details"].(map[string]any)
	if details["format"] == nil || details["sentenceSources.0.textId"] != "Required" || details["sentenceSources.1.kind"] == nil || details["sentenceCount"] == nil {
		t.Fatalf("details = %v", details)
	}

	// 来源列表：要学的句子数量、指定单元时列出单元的篇
	src := okData(t, c.do("GET", "/api/sheets/sources?unitId=u1a", "", c.s1))
	if src["learningSentences"] != float64(1) {
		t.Fatalf("sources = %v", src)
	}
	texts := listOf(src["texts"])
	if !slices.Equal(fieldOf(texts, "id"), []string{"tx1", "tx2"}) || texts[0]["sentenceCount"] != float64(3) || texts[0]["kind"] != "list" {
		t.Fatalf("texts = %v", texts)
	}
	if _, ok := okData(t, c.do("GET", "/api/sheets/sources", "", c.s1))["texts"]; ok {
		t.Fatal("texts should be omitted without unitId")
	}
}

func createDictation(t *testing.T, c *covEnv, h map[string]string, body map[string]any) string {
	t.Helper()
	body["format"] = "dictation"
	return okData(t, c.do("POST", "/api/sheets", jsonBody(body), h))["id"].(string)
}

func TestDictationCreateGradeFlow(t *testing.T) {
	c := newDictEnv(t)
	// w4 已学过（昨天复习过）；w5 没学过
	c.exec(`INSERT INTO "MemoryState" ("id","userId","wordId","due","stability","difficulty","reps","state","lastReview","introducedDay") VALUES ('m4',?,'w4','2026-10-06T00:00:00.000Z',3,5,2,2,'2026-10-05T00:00:00.000Z','2026-09-30')`, c.s1ID)

	items := []any{
		map[string]any{"type": "sentence", "sentenceId": "sx2"},
		map[string]any{"type": "word", "wordId": "w4"},
		map[string]any{"type": "phrase", "wordId": "w5"},
		map[string]any{"type": "frame", "sentenceId": "sx1"},
		map[string]any{"type": "transform", "sentenceId": "sxv"},
	}
	// 校验：题型与句子不符、缺 id、自测单的 wordIds 不再必填
	for _, bad := range []map[string]any{
		{"items": []any{map[string]any{"type": "frame", "sentenceId": "sx2"}}},
		{"items": []any{map[string]any{"type": "transform", "sentenceId": "sx1"}}},
		{"items": []any{map[string]any{"type": "word"}}},
		{"items": []any{map[string]any{"type": "sentence", "sentenceId": "nope"}}},
		{"items": []any{}},
	} {
		bad["format"] = "dictation"
		if r := c.do("POST", "/api/sheets", jsonBody(bad), c.s1); r.Status != 400 {
			t.Fatalf("bad %v = %d %v", bad, r.Status, r.Body)
		}
	}
	// 别人的短文句子不能出
	if r := c.do("POST", "/api/sheets", `{"format":"dictation","items":[{"type":"sentence","sentenceId":"sp2"}]}`, c.s1); r.Status != 403 {
		t.Fatalf("other passage sentence = %d %v", r.Status, r.Body)
	}

	sheetID := createDictation(t, c, c.s1, map[string]any{"items": items})
	detail := okData(t, c.do("GET", "/api/sheets/"+sheetID, "", c.s1))
	if detail["format"] != "dictation" || detail["grading"] != nil {
		t.Fatalf("detail = %v", detail)
	}
	di := listOf(detail["items"])
	// 卷面按分区：单词、短语、句子、仿写与转换
	if !slices.Equal(fieldOf(di, "type"), []string{"word", "phrase", "sentence", "frame", "transform"}) {
		t.Fatalf("items = %v", di)
	}
	if di[0]["prompt"] != "义4" || di[0]["answer"] != "worde" || di[0]["index"] != float64(0) || di[0]["section"] != float64(1) ||
		di[2]["prompt"] != "我擅长英语。" || di[2]["answer"] != "I am good at English." ||
		di[4]["origin"].(map[string]any)["en"] != "I am good at English." {
		t.Fatalf("items = %v", di)
	}
	if len(listOf(detail["words"])) != 2 {
		t.Fatalf("words = %v", detail["words"])
	}

	// 默写单不能在线测试
	if r := c.do("POST", "/api/study/sessions", `{"kind":"sheet","sheetId":"`+sheetID+`"}`, c.s1); r.Status != 400 || errCode(r) != "INVALID_ACTION 默写单不能在线测试，请打印后批改" {
		t.Fatalf("start = %d %v", r.Status, r.Body)
	}
	// 今日页：待批改的默写单
	if ns := okData(t, c.do("GET", "/api/today", "", c.s1))["sheet"].(map[string]any); ns["format"] != "dictation" || ns["id"] != sheetID {
		t.Fatalf("nextSheet = %v", ns)
	}

	// 权限：s2 看不到；t1（同班老师）能看；批改要全部题目
	if r := c.do("GET", "/api/sheets/"+sheetID, "", c.s2); r.Status != 403 {
		t.Fatalf("s2 view = %d", r.Status)
	}
	if r := c.do("POST", "/api/sheets/"+sheetID+"/grade", `{"results":[{"index":0,"correct":true}]}`, c.s2); r.Status != 403 {
		t.Fatalf("s2 grade = %d", r.Status)
	}
	if r := c.do("POST", "/api/sheets/"+sheetID+"/grade", `{"results":[{"index":0,"correct":true}]}`, c.s1); r.Status != 400 || errCode(r) != "VALIDATION 需要批改全部 5 道题" {
		t.Fatalf("partial grade = %d %v", r.Status, r.Body)
	}
	if r := c.do("POST", "/api/sheets/"+sheetID+"/grade", `{"results":[{"index":"a"}]}`, c.s1); r.Status != 400 {
		t.Fatalf("bad body = %d", r.Status)
	}

	// 学生本人提交：自批；w4 错（Again），sx1 错
	res := []any{
		map[string]any{"index": 0, "correct": false, "userAnswer": "wordx"},
		map[string]any{"index": 1, "correct": true},
		map[string]any{"index": 2, "correct": true},
		map[string]any{"index": 3, "correct": false, "userAnswer": "I find it useful."},
		map[string]any{"index": 4, "correct": true},
	}
	graded := okData(t, c.do("POST", "/api/sheets/"+sheetID+"/grade", jsonBody(map[string]any{"results": res}), c.s1))
	g := graded["grading"].(map[string]any)
	if g["selfGraded"] != true || g["gradedBy"].(map[string]any)["id"] != c.s1ID || g["correct"] != float64(3) || g["total"] != float64(5) {
		t.Fatalf("grading = %v", g)
	}
	gr := listOf(g["results"])
	if len(gr) != 5 || gr[0]["correct"] != false || gr[0]["userAnswer"] != "wordx" || gr[1]["userAnswer"] != nil {
		t.Fatalf("results = %v", gr)
	}
	sessionID := g["sessionId"].(string)

	// 今日页：批改后 sheet 不再是这份（没有其他单子 → null），gradedSheets 显示「已批改」
	today := okData(t, c.do("GET", "/api/today", "", c.s1))
	if today["sheet"] != nil {
		t.Fatalf("today sheet after grading = %v", today["sheet"])
	}
	gs := listOf(today["gradedSheets"])
	if len(gs) != 1 || gs[0]["id"] != sheetID || gs[0]["seq"] != float64(1) || gs[0]["itemCount"] != float64(5) || gs[0]["sessionId"] != sessionID ||
		gs[0]["correct"] != float64(3) || gs[0]["total"] != float64(5) || gs[0]["selfGraded"] != true || gs[0]["gradedAt"] != "2026-10-06T04:00:00.000Z" {
		t.Fatalf("gradedSheets = %v", gs)
	}
	// 第二天不再显示
	c.now = c.now.Add(24 * time.Hour)
	if gs := listOf(okData(t, c.do("GET", "/api/today", "", c.s1))["gradedSheets"]); len(gs) != 0 {
		t.Fatalf("gradedSheets next day = %v", gs)
	}
	c.now = c.now.Add(-24 * time.Hour)

	var n int
	c.d.DB.QueryRow(`SELECT count(*) FROM "Answer" WHERE "sessionId" = ? AND "mode" = 'dictation' AND "phase" = 'test' AND "attempt" = 1`, sessionID).Scan(&n)
	if n != 2 {
		t.Fatalf("answers = %d", n)
	}
	var ua string
	c.d.DB.QueryRow(`SELECT "userAnswer" FROM "Answer" WHERE "sessionId" = ? AND "wordId" = 'w4'`, sessionID).Scan(&ua)
	if ua != "wordx" {
		t.Fatalf("userAnswer = %q", ua)
	}
	c.d.DB.QueryRow(`SELECT count(*) FROM "SentenceAnswer" WHERE "sessionId" = ?`, sessionID).Scan(&n)
	if n != 3 {
		t.Fatalf("sentence answers = %d", n)
	}
	var itemType string
	var correct bool
	c.d.DB.QueryRow(`SELECT "itemType","correct" FROM "SentenceAnswer" WHERE "sessionId" = ? AND "sentenceId" = 'sx1'`, sessionID).Scan(&itemType, &correct)
	if itemType != "frame" || correct {
		t.Fatalf("sx1 = %s %v", itemType, correct)
	}
	// K10：学过的错 → Again；没学过的只记成绩
	var rating int
	if err := c.d.DB.QueryRow(`SELECT "rating" FROM "ReviewLog" WHERE "sessionId" = ? AND "wordId" = 'w4'`, sessionID).Scan(&rating); err != nil || rating != 1 {
		t.Fatalf("w4 rating = %d %v", rating, err)
	}
	c.d.DB.QueryRow(`SELECT count(*) FROM "MemoryState" WHERE "userId" = ? AND "wordId" = 'w5'`, c.s1ID).Scan(&n)
	if n != 0 {
		t.Fatal("w5 should not get memory")
	}
	var snapText, status string
	c.d.DB.QueryRow(`SELECT "snapshot","status" FROM "StudySession" WHERE "id" = ?`, sessionID).Scan(&snapText, &status)
	var snap map[string]any
	json.Unmarshal([]byte(snapText), &snap)
	if status != "completed" || snap["format"] != "dictation" || snap["selfGraded"] != true || snap["gradedBy"].(map[string]any)["id"] != c.s1ID {
		t.Fatalf("session = %s %v", status, snap)
	}

	// 只能提交一次
	if r := c.do("POST", "/api/sheets/"+sheetID+"/grade", jsonBody(map[string]any{"results": res}), c.s1); r.Status != 409 || errCode(r) != "INVALID_STATUS 这份默写单已经批改过" {
		t.Fatalf("regrade = %d %v", r.Status, r.Body)
	}
	// 批改过的不能删
	if r := c.do("DELETE", "/api/sheets/"+sheetID, "", c.s1); r.Status != 400 {
		t.Fatalf("delete graded = %d", r.Status)
	}
	// 明细带批改结果；老师也能看到自批标记
	td := okData(t, c.do("GET", "/api/sheets/"+sheetID, "", c.t1))
	if td["grading"].(map[string]any)["selfGraded"] != true {
		t.Fatalf("teacher detail = %v", td)
	}
	// 列表：格式、自批、成绩含句子
	list := listOf(okData(t, c.do("GET", "/api/sheets?userId="+c.s1ID, "", c.t1))["items"])
	if list[0]["format"] != "dictation" || list[0]["status"] != "tested" || list[0]["selfGraded"] != true {
		t.Fatalf("list = %v", list[0])
	}
	fr := list[0]["firstResult"].(map[string]any)
	if fr["correct"] != float64(3) || fr["total"] != float64(5) || fr["sessionId"] != sessionID {
		t.Fatalf("firstResult = %v", fr)
	}
	// 记录页：学习组详情带句子题与自批标记
	rec := okData(t, c.do("GET", "/api/records/sessions/"+sessionID, "", c.t1))
	if rec["format"] != "dictation" || rec["selfGraded"] != true || len(listOf(rec["sentences"])) != 3 || rec["planName"] != "默写单 #1" {
		t.Fatalf("record = %v", rec)
	}
	recSents := listOf(rec["sentences"])
	if recSents[1]["sentenceId"] != "sx1" || recSents[1]["correct"] != false || recSents[1]["userAnswer"] != "I find it useful." {
		t.Fatalf("record sentences = %v", recSents)
	}
	sessions := listOf(okData(t, c.do("GET", "/api/records/sessions?userId="+c.s1ID, "", c.t1))["items"])
	if sessions[0]["selfGraded"] != true || sessions[0]["format"] != "dictation" {
		t.Fatalf("sessions = %v", sessions[0])
	}

	// 句子状态：sx1 要学；错题再出一份（词用 session 来源，句子用 session 来源）
	if okData(t, c.do("GET", "/api/sheets/sources", "", c.s1))["learningSentences"] != float64(1) {
		t.Fatal("learningSentences")
	}
	again := okData(t, c.do("POST", "/api/sheets/preview", jsonBody(map[string]any{
		"format": "dictation", "source": map[string]any{"kind": "session", "sessionId": sessionID},
		"sentenceSources": []any{map[string]any{"kind": "session", "sessionId": sessionID}},
	}), c.s1))
	if !slices.Equal(fieldOf(listOf(again["items"]), "wordId"), []string{"w4"}) || !slices.Equal(fieldOf(listOf(again["sentences"]), "sentenceId"), []string{"sx1"}) {
		t.Fatalf("again = %v", again)
	}

	// 老师出题并批改：不是自批；K19：w4 今天已更新过记忆，不再写 ReviewLog
	sheet2 := createDictation(t, c, c.t1, map[string]any{"userId": c.s1ID, "items": []any{map[string]any{"type": "word", "wordId": "w4"}}})
	g2 := okData(t, c.do("POST", "/api/sheets/"+sheet2+"/grade", `{"results":[{"index":0,"correct":true}]}`, c.t1))["grading"].(map[string]any)
	if g2["selfGraded"] != false || g2["gradedBy"].(map[string]any)["id"] != c.t1ID {
		t.Fatalf("teacher grading = %v", g2)
	}
	c.d.DB.QueryRow(`SELECT count(*) FROM "ReviewLog" WHERE "userId" = ? AND "wordId" = 'w4'`, c.s1ID).Scan(&n)
	if n != 1 {
		t.Fatalf("w4 review logs = %d (K19)", n)
	}

	// 别班老师不能为 s2 出题 / 批改；t1 为 s2 出的默写单，t2 批改 → 403
	sheet3 := createDictation(t, c, c.t1, map[string]any{"userId": c.s2ID, "items": []any{map[string]any{"type": "word", "wordId": "w1"}}})
	if r := c.do("POST", "/api/sheets/"+sheet3+"/grade", `{"results":[{"index":0,"correct":true}]}`, c.t2); r.Status != 403 {
		t.Fatalf("t2 grade s2 = %d", r.Status)
	}
	// 自测单不需要批改
	selftest := okData(t, c.do("POST", "/api/sheets", `{"wordIds":["w1"]}`, c.s1))["id"].(string)
	if r := c.do("POST", "/api/sheets/"+selftest+"/grade", `{"results":[{"index":0,"correct":true}]}`, c.s1); r.Status != 400 || errCode(r) != "INVALID_ACTION 只有默写单需要批改" {
		t.Fatalf("grade selftest = %d %v", r.Status, r.Body)
	}
	st := okData(t, c.do("GET", "/api/sheets/"+selftest, "", c.s1))
	if st["format"] != "selftest" || len(listOf(st["items"])) != 0 || st["grading"] != nil {
		t.Fatalf("selftest detail = %v", st)
	}
}

// 班级概览的目标覆盖旁带自批数与比例：按每个已测目标词最近一次正式测试是否自批。
func TestDictationSelfGradedCoverage(t *testing.T) {
	c := newDictEnv(t)
	c.do("PUT", "/api/classes/c1/target-books", `{"bookIds":["b2"]}`, c.t1) // b2 = w4、w5
	coverageOf := func(userID string) map[string]any {
		t.Helper()
		for _, s := range listOf(okData(t, c.do("GET", "/api/classes/c1/overview", "", c.t1))["students"]) {
			if s["userId"] == userID {
				return s["coverage"].(map[string]any)
			}
		}
		t.Fatalf("student %s not in overview", userID)
		return nil
	}
	if cov := coverageOf(c.s1ID); cov["tested"] != float64(0) || cov["selfGraded"] != float64(0) || cov["selfGradedRatio"] != nil {
		t.Fatalf("初始 = %v", cov)
	}

	// 学生本人批改 w4、w5（自批）
	s1Sheet := createDictation(t, c, c.s1, map[string]any{"items": []any{
		map[string]any{"type": "word", "wordId": "w4"}, map[string]any{"type": "phrase", "wordId": "w5"},
	}})
	okData(t, c.do("POST", "/api/sheets/"+s1Sheet+"/grade", `{"results":[{"index":0,"correct":true},{"index":1,"correct":false}]}`, c.s1))
	if cov := coverageOf(c.s1ID); cov["tested"] != float64(2) || cov["learning"] != float64(1) || cov["selfGraded"] != float64(2) || cov["selfGradedRatio"] != float64(1) {
		t.Fatalf("自批后 = %v", cov)
	}

	// 老师复核 w4：w4 最近一次改为老师批改
	tSheet := createDictation(t, c, c.t1, map[string]any{"userId": c.s1ID, "items": []any{map[string]any{"type": "word", "wordId": "w4"}}})
	okData(t, c.do("POST", "/api/sheets/"+tSheet+"/grade", `{"results":[{"index":0,"correct":true}]}`, c.t1))
	if cov := coverageOf(c.s1ID); cov["tested"] != float64(2) || cov["selfGraded"] != float64(1) || cov["selfGradedRatio"] != 0.5 {
		t.Fatalf("老师复核后 = %v", cov)
	}
	// s2 没有作答
	if cov := coverageOf(c.s2ID); cov["selfGraded"] != float64(0) || cov["selfGradedRatio"] != nil {
		t.Fatalf("s2 = %v", cov)
	}
}

func TestDictationCreateCopies(t *testing.T) {
	c := newDictEnv(t)
	created := okData(t, c.do("POST", "/api/sheets", jsonBody(map[string]any{
		"format": "dictation", "copies": 2, "wordCount": 1, "sentenceCount": 1,
		"items": []any{
			map[string]any{"type": "word", "wordId": "w1"}, map[string]any{"type": "word", "wordId": "w2"},
			map[string]any{"type": "sentence", "sentenceId": "sx1"}, map[string]any{"type": "sentence", "sentenceId": "sx2"},
		},
	}), c.s1))
	got := listOf(created["items"])
	if len(got) != 2 {
		t.Fatalf("created = %v", created)
	}
	second := okData(t, c.do("GET", "/api/sheets/"+got[1]["id"].(string), "", c.s1))
	if !slices.Equal(fieldOf(listOf(second["items"]), "type"), []string{"word", "sentence"}) || listOf(second["items"])[1]["sentenceId"] != "sx2" {
		t.Fatalf("second = %v", second)
	}
	// 超出 份数 × 每份上限
	r := c.do("POST", "/api/sheets", jsonBody(map[string]any{
		"format": "dictation", "copies": 1, "wordCount": 1,
		"items": []any{map[string]any{"type": "word", "wordId": "w1"}, map[string]any{"type": "word", "wordId": "w2"}},
	}), c.s1)
	if r.Status != 400 || errCode(r) != "VALIDATION 单词 2 题，超过 1 份 × 1 题" {
		t.Fatalf("over = %d %v", r.Status, r.Body)
	}
}
