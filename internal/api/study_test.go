package api

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/vinx-lab/vinx-vocab-go/internal/core"
)

// 学习流接口测试（固定时钟 + 固定随机源）：跨上海 00:00 的今日队列、进行中检测不泄露答案（含进度接口）。

func studentCookie(t *testing.T, e *testEnv, email string) (cookie, id string) {
	t.Helper()
	r := e.do("POST", "/api/auth/signup", `{"email":"`+email+`","password":"123456"}`, nil)
	if r.Status != 201 {
		t.Fatalf("signup = %d %v", r.Status, r.Body)
	}
	return cookieOf(t, r), r.Body["data"].(map[string]any)["user"].(map[string]any)["id"].(string)
}

func seedSystemUnit(t *testing.T, e *testEnv, n int) string {
	t.Helper()
	db := e.d.DB
	if _, err := db.Exec(`INSERT INTO "Book" ("id","name","isSystem") VALUES ('sb','系统书',1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO "Unit" ("id","bookId","name") VALUES ('su','sb','U1')`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("sw%02d", i)
		if _, err := db.Exec(`INSERT INTO "Word" ("id","spelling","partOfSpeech","definition") VALUES (?,?,?,?)`, id, fmt.Sprintf("spell%c", 'a'+i), "n.", fmt.Sprintf("义%d", i)); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO "UnitWord" ("unitId","wordId","sortOrder") VALUES ('su',?,?)`, id, i); err != nil {
			t.Fatal(err)
		}
	}
	return "su"
}

func dataOf(r resp) map[string]any { d, _ := r.Body["data"].(map[string]any); return d }

func TestStudyHTTPAcrossMidnightAndTestSanitize(t *testing.T) {
	e := newEnv(t, nil)
	e.d.Rand = core.SeededRng(42)
	loc, _ := time.LoadLocation("Asia/Shanghai")
	e.now = time.Date(2026, 9, 28, 23, 59, 30, 0, loc)
	unit := seedSystemUnit(t, e, 6)
	cookie, _ := studentCookie(t, e, "s@x.test")
	h := map[string]string{"Cookie": cookie}

	plan := e.do("POST", "/api/plans", `{"name":"p","newPerDay":6,"modes":["recognition","spelling"],"unitIds":["`+unit+`"]}`, h)
	if plan.Status != 200 {
		t.Fatalf("plan = %v", plan.Body)
	}
	planID := dataOf(plan)["id"].(string)
	if day := dataOf(e.do("GET", "/api/today", "", h))["day"]; day != "2026-09-28" {
		t.Fatalf("23:59:30 学习日 %v", day)
	}

	start := e.do("POST", "/api/study/sessions", `{"kind":"learn","planId":"`+planID+`"}`, h)
	sid := dataOf(start)["id"].(string)
	sess := dataOf(e.do("GET", "/api/study/sessions/"+sid, "", h))
	for _, raw := range sess["items"].([]any) {
		it := raw.(map[string]any)
		for _, mode := range []string{"recognition", "spelling"} {
			ans := it["answer"].(string)
			if mode == "recognition" {
				ans = it["definition"].(string)
			}
			body, _ := json.Marshal(map[string]any{"wordId": it["wordId"], "mode": mode, "phase": "practice", "attempt": 1, "answer": ans})
			if r := e.do("POST", "/api/study/sessions/"+sid+"/answers", string(body), h); r.Status != 200 || dataOf(r)["correct"] != true {
				t.Fatalf("answer = %v", r.Body)
			}
		}
	}
	// 00:00:30 结算：completedDay 与今日队列都是新的一天
	e.now = time.Date(2026, 9, 29, 0, 0, 30, 0, loc)
	done := e.do("POST", "/api/study/sessions/"+sid+"/complete", "", h)
	if dataOf(done)["result"].(map[string]any)["newLearned"] != float64(6) {
		t.Fatalf("complete = %v", done.Body)
	}
	today := dataOf(e.do("GET", "/api/today", "", h))
	card := today["plans"].([]any)[0].(map[string]any)
	// 新学词记在开组那天（09-28），所以 09-29 的新词额度没有被占用
	if today["day"] != "2026-09-29" || card["newDoneToday"] != float64(0) || today["streak"] != float64(1) {
		t.Fatalf("today = %v", today)
	}
	var cd string
	e.d.DB.QueryRow(`SELECT "completedDay" FROM "StudySession" WHERE "id" = ?`, sid).Scan(&cd)
	if cd != "2026-09-29" {
		t.Fatalf("completedDay = %s", cd)
	}

	// 检测：进行中不下发答案；作答、进度接口都不返回对错
	tp := e.do("POST", "/api/plans", `{"name":"t","kind":"test","modes":["spelling"],"testSize":3,"testScope":"learned","unitIds":["`+unit+`"]}`, h)
	ts := e.do("POST", "/api/study/sessions", `{"kind":"test","planId":"`+dataOf(tp)["id"].(string)+`"}`, h)
	tid := dataOf(ts)["id"].(string)
	detail := dataOf(e.do("GET", "/api/study/sessions/"+tid, "", h))
	items := detail["items"].([]any)
	if len(items) != 3 {
		t.Fatalf("items = %v", items)
	}
	for _, raw := range items {
		it := raw.(map[string]any)
		// 没有认义题：不给英文；有拼写题：给释义与字母数
		if it["answer"] != "" || it["spelling"] != "" || it["phonetic"] != nil || it["example"] != nil || it["definition"] == "" || it["letters"] != float64(6) || it["wordsInAnswer"] != float64(1) {
			t.Fatalf("进行中检测泄露了答案：%v", it)
		}
		body, _ := json.Marshal(map[string]any{"wordId": it["wordId"], "mode": "spelling", "phase": "test", "attempt": 1, "answer": "x"})
		r := e.do("POST", "/api/study/sessions/"+tid+"/answers", string(body), h)
		if len(dataOf(r)) != 1 || dataOf(r)["recorded"] != true {
			t.Fatalf("检测作答不应反馈对错：%v", r.Body)
		}
	}
	prog := e.do("PATCH", "/api/study/sessions/"+tid+"/progress", `{"progress":{"idx":3}}`, h)
	if _, has := prog.Body["data"]; prog.Status != 200 || has {
		t.Fatalf("progress = %v", prog.Body)
	}
	after := dataOf(e.do("GET", "/api/study/sessions/"+tid, "", h))
	for _, a := range after["answers"].([]any) {
		if a.(map[string]any)["correct"] != nil {
			t.Fatalf("进行中检测的作答对错应为 null：%v", a)
		}
	}
	if after["progress"].(map[string]any)["idx"] != float64(3) {
		t.Fatalf("progress = %v", after["progress"])
	}
	if r := e.do("GET", "/api/records/sessions/"+tid, "", h); errCode(r) != "INVALID_STATUS 检测进行中，交卷后才能查看详情" {
		t.Fatalf("records = %v", r.Body)
	}
	if sum := dataOf(e.do("GET", "/api/records/summary", "", h)); sum["accuracy30d"].(map[string]any)["total"] != float64(12) {
		t.Fatalf("进行中检测的作答不应计入正确率：%v", sum["accuracy30d"])
	}
}
