package ai

import (
	"strings"
	"testing"
)

var testWords = []PromptWord{
	{Spelling: "apple", PartOfSpeech: "n.", Definition: "苹果"},
	{Spelling: "run", Definition: "跑"},
	{Spelling: "be good at", Definition: "擅长"},
}

func TestDefaultTemplatesSeparateFromOutputFormat(t *testing.T) {
	for _, k := range PromptKeys {
		if strings.Contains(strings.ToLower(DefaultPromptTemplates[k]), "json") {
			t.Errorf("%s default template should not mention json", k)
		}
		if !strings.Contains(OutputFormats[k], "JSON") {
			t.Errorf("%s output format should mention JSON", k)
		}
	}
	if !strings.Contains(DefaultPromptTemplates[PromptExample], "例句编辑") {
		t.Error("missing 例句编辑")
	}
	if !strings.Contains(OutputFormats[PromptExample], `"exampleCn"`) {
		t.Error("missing exampleCn")
	}
	if !strings.Contains(OutputFormats[PromptPassage], `"questions"`) || !strings.Contains(OutputFormats[PromptPassage], `"q"`) {
		t.Error("missing questions/q")
	}
}

func TestResolveTemplate(t *testing.T) {
	if r := ResolveTemplate(PromptExample, nil); r.Custom || r.Text != DefaultPromptTemplates[PromptExample] {
		t.Fatalf("nil stored = %+v", r)
	}
	if r := ResolveTemplate(PromptExample, StoredPromptTemplates{PromptPassage: "x"}); r.Custom || r.Text != DefaultPromptTemplates[PromptExample] {
		t.Fatalf("unrelated stored = %+v", r)
	}
	if r := ResolveTemplate(PromptExample, StoredPromptTemplates{PromptExample: "例句更短"}); !r.Custom || r.Text != "例句更短" {
		t.Fatalf("custom = %+v", r)
	}
}

func TestBuildExamplePrompt(t *testing.T) {
	p := BuildExamplePrompt("  例句不超过 8 个词。\n", testWords)
	want := strings.Join([]string{"例句不超过 8 个词。", "", "请为下面 3 个词各写一个例句：", "1. apple (n.) —— 苹果", "2. run —— 跑", "3. be good at —— 擅长"}, "\n")
	if p != want {
		t.Fatalf("got %q want %q", p, want)
	}
}

func TestBuildPassagePrompt(t *testing.T) {
	p := BuildPassagePrompt("写一篇短文。", testWords[:2], " 校园运动会 ")
	want := strings.Join([]string{"写一篇短文。", "", "目标单词（2 个）：", "1. apple —— 苹果", "2. run —— 跑", "", "主题倾向：校园运动会"}, "\n")
	if p != want {
		t.Fatalf("got %q want %q", p, want)
	}
	if strings.Contains(BuildPassagePrompt("写一篇短文。", testWords[:1], "  "), "主题倾向") {
		t.Fatal("blank topic should not add 主题倾向")
	}
	if strings.Contains(BuildPassagePrompt("写一篇短文。", testWords[:1], ""), "主题倾向") {
		t.Fatal("empty topic should not add 主题倾向")
	}
}

func TestVisiblePromptsHaveNoJSON(t *testing.T) {
	if strings.Contains(strings.ToLower(BuildPassagePrompt(DefaultPromptTemplates[PromptPassage], testWords, "")), "json") {
		t.Fatal("visible passage prompt should not mention json")
	}
	if strings.Contains(strings.ToLower(BuildExamplePrompt(DefaultPromptTemplates[PromptExample], testWords)), "json") {
		t.Fatal("visible example prompt should not mention json")
	}
}

func TestBuildMessages(t *testing.T) {
	visible := "用户改过的提示词\n1. apple —— 苹果"
	m := BuildMessages(PromptPassage, visible)
	if m.System != OutputFormats[PromptPassage] || m.User != visible {
		t.Fatalf("got %+v", m)
	}
	if BuildMessages(PromptExample, visible).System != OutputFormats[PromptExample] {
		t.Fatal("example system mismatch")
	}
}

func TestValidatePromptText(t *testing.T) {
	if r := ValidatePromptText("   \n "); r.OK || r.Message != "提示词不能为空" {
		t.Fatalf("blank = %+v", r)
	}
	if r := ValidatePromptText(strings.Repeat("x", 6001)); r.OK || !strings.Contains(r.Message, "6000") {
		t.Fatalf("too long = %+v", r)
	}
	if r := ValidatePromptText(strings.Repeat("x", 6000)); !r.OK {
		t.Fatalf("exactly 6000 should pass: %+v", r)
	}
	if r := ValidatePromptText("  写一篇短文\n"); !r.OK || r.Value != "写一篇短文" {
		t.Fatalf("trim = %+v", r)
	}
}

func TestParseStoredTemplates(t *testing.T) {
	if got := ParseStoredTemplates(nil); len(got) != 0 {
		t.Fatalf("nil = %v", got)
	}
	if got := ParseStoredTemplates(map[string]any{"example": "a", "passage": "  ", "other": "x"}); len(got) != 1 || got["example"] != "a" {
		t.Fatalf("got %v", got)
	}
}

func TestMergeStoredTemplates(t *testing.T) {
	got := MergeStoredTemplates(StoredPromptTemplates{PromptPassage: "p"}, StoredPromptTemplates{PromptExample: "e"})
	if got[PromptPassage] != "p" || got[PromptExample] != "e" {
		t.Fatalf("got %v", got)
	}
	got2 := MergeStoredTemplates(StoredPromptTemplates{PromptExample: "e", PromptPassage: "p"}, StoredPromptTemplates{PromptExample: DefaultPromptTemplates[PromptExample]})
	if _, ok := got2[PromptExample]; ok {
		t.Fatal("reverting to default should remove the key")
	}
	if got2[PromptPassage] != "p" {
		t.Fatalf("got2 = %v", got2)
	}
}

func TestToPromptsView(t *testing.T) {
	v := ToPromptsView(StoredPromptTemplates{PromptExample: "e"})
	if v.Example != (PromptItem{Key: PromptExample, Current: "e", IsDefault: false, DefaultText: DefaultPromptTemplates[PromptExample]}) {
		t.Fatalf("example = %+v", v.Example)
	}
	if v.Passage != (PromptItem{Key: PromptPassage, Current: DefaultPromptTemplates[PromptPassage], IsDefault: true, DefaultText: DefaultPromptTemplates[PromptPassage]}) {
		t.Fatalf("passage = %+v", v.Passage)
	}
}
