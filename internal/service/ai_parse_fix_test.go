package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	coreai "github.com/vinx-lab/vinx-vocab-go/internal/core/ai"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// I1（评审 round 1）：端到端确认 GenerateExamples / GeneratePassage 对评审复现的三种「字段类型不对」
// 回复保持容错（与 oracle 一致），不会因为一个字段类型不对就整段/整个任务失败。

func fakeChatServer(t *testing.T, reply string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		w.Header().Set("content-type", "application/json")
		body, _ := json.Marshal(map[string]any{"choices": []map[string]any{{"message": map[string]any{"content": reply}}}})
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func insertWord(t *testing.T, db *store.DB, id, spelling, definition string) {
	t.Helper()
	now := store.NewTime(time.Now())
	if _, err := db.Exec(`INSERT INTO "Word" ("id","spelling","definition","createdAt","updatedAt") VALUES (?,?,?,?,?)`, id, spelling, definition, now, now); err != nil {
		t.Fatal(err)
	}
}

func callConfigFor(url string) coreai.CallConfig {
	return coreai.CallConfig{Provider: coreai.ProviderOpenAI, BaseURL: &url, Model: "fake", TimeoutMs: 10_000}
}

func TestGeneratePassageToleratesWrongTypedFields(t *testing.T) {
	db := openTempDB(t)
	ctx := context.Background()
	insertWord(t, db, "w1", "apple", "苹果")
	insertWord(t, db, "w2", "banana", "香蕉")
	insertWord(t, db, "w3", "cherry", "樱桃")
	cache := NewAIConfigCache()

	// 复现用例 1 + 2 合并：title 是数字，questions 不是数组。
	reply := `{"title":123,"passage":"apple banana cherry are fruit.","questions":"none"}`
	srv := fakeChatServer(t, reply)

	result, err := GeneratePassage(ctx, cache, db, callConfigFor(srv.URL), []string{"w1", "w2", "w3"}, nil, "")
	if err != nil {
		t.Fatalf("expected success (oracle tolerates wrong-typed title/questions), got error: %v", err)
	}
	if result.Title != "123" {
		t.Fatalf("title = %q, want \"123\" (String(123))", result.Title)
	}
	if len(result.Questions) != 0 {
		t.Fatalf("questions should default to [] when not an array, got %+v", result.Questions)
	}
	if result.Passage == "" {
		t.Fatal("passage should not be empty")
	}
}

func TestGenerateExamplesOneBadSpellingDoesNotDropGoodItem(t *testing.T) {
	db := openTempDB(t)
	ctx := context.Background()
	insertWord(t, db, "w1", "alpha", "阿尔法")
	insertWord(t, db, "w2", "beta", "贝塔")
	cache := NewAIConfigCache()

	// 复现用例 3：数组里第二项 spelling 是数字；oracle 解析出 2 项，不整段丢弃。
	// 第一项按 spelling 精确匹配到 w1；第二项 spelling 不匹配任何真实词，但因为 list.length==words.length，
	// 按位置兜底给 w2（与 oracle 的 fallback 逻辑一致）。
	reply := `[{"spelling":"alpha","example":"I love alpha testing today.","exampleCn":"我喜欢阿尔法测试。"},{"spelling":2,"example":"This has beta in it now.","exampleCn":"这里面现在有贝塔。"}]`
	srv := fakeChatServer(t, reply)

	result, err := GenerateExamples(ctx, cache, db, time.Now(), callConfigFor(srv.URL), []string{"w1", "w2"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Items) != 2 {
		t.Fatalf("expected both words to get examples (positional fallback), got %d items, failed=%v", len(result.Items), result.Failed)
	}
	byWordID := map[string]ExampleResultItem{}
	for _, it := range result.Items {
		byWordID[it.WordID] = it
	}
	if byWordID["w1"].Example != "I love alpha testing today." {
		t.Fatalf("w1 example = %q", byWordID["w1"].Example)
	}
	if byWordID["w2"].Example != "This has beta in it now." {
		t.Fatalf("w2 example = %q", byWordID["w2"].Example)
	}
}
