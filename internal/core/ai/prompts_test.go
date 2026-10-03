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
	// spec 0005：四种生成统一逐句输出
	if !strings.Contains(OutputFormats[PromptExample], `"spelling"`) || !strings.Contains(OutputFormats[PromptExample], `"en"`) || !strings.Contains(OutputFormats[PromptExample], `"cn"`) {
		t.Error("example format should be spelling/en/cn")
	}
	if !strings.Contains(OutputFormats[PromptPassage], `"questions"`) || !strings.Contains(OutputFormats[PromptPassage], `"q"`) {
		t.Error("missing questions/q")
	}
	if !strings.Contains(OutputFormats[PromptPassage], `"sentences"`) || !strings.Contains(OutputFormats[PromptPassage], `"paragraph"`) {
		t.Error("passage format should be sentence by sentence")
	}
	if !strings.Contains(OutputFormats[PromptPattern], `"frame"`) {
		t.Error("pattern format should have frame")
	}
	for _, f := range []string{`"origin"`, `"change"`, `"note"`} {
		if !strings.Contains(OutputFormats[PromptVariant], f) {
			t.Errorf("variant format missing %s", f)
		}
	}
	if len(PromptKeys) != 4 || PromptKeys[2] != PromptPattern || PromptKeys[3] != PromptVariant {
		t.Errorf("PromptKeys = %v", PromptKeys)
	}
	for _, k := range PromptKeys {
		if PromptLabel[k] == "" || OutputFormats[k] == "" || DefaultPromptTemplates[k] == "" {
			t.Errorf("%s incomplete", k)
		}
	}
}

func TestDefaultTemplateTexts(t *testing.T) {
	if !strings.HasPrefix(DefaultPromptTemplates[PromptExample], "你是{学段}英语教材的例句编辑。") {
		t.Error("example template")
	}
	if !strings.HasPrefix(DefaultPromptTemplates[PromptPattern], "你是{学段}英语教材编辑。根据给定单元的话题和词汇") {
		t.Error("pattern template")
	}
	if !strings.HasPrefix(DefaultPromptTemplates[PromptVariant], "你是{学段}英语老师。照着给定的例句做仿写和变式练习") {
		t.Error("variant template")
	}
	if !strings.Contains(DefaultPromptTemplates[PromptVariant], "长度不超过 {单句上限}") {
		t.Error("variant template max words")
	}
}

func TestBuildPatternPrompt(t *testing.T) {
	p := BuildPatternPrompt("写句型。", PatternInput{UnitName: "Unit 1", Topic: " 学习方法；by doing 结构 ", Count: 8, Words: testWords[:2], Existing: []string{"How do you learn English?"}})
	want := strings.Join([]string{"写句型。", "", "单元：Unit 1", "话题或语法点：学习方法；by doing 结构", "请写 8 个重点句型。", "",
		"本单元的单词和短语（2 个）：", "1. apple (n.) —— 苹果", "2. run —— 跑", "",
		"本单元已有的句型（不要重复）：", "1. How do you learn English?"}, "\n")
	if p != want {
		t.Fatalf("got %q\nwant %q", p, want)
	}
	p2 := BuildPatternPrompt("写句型。", PatternInput{UnitName: "U", Topic: "t", Count: 4})
	if strings.Contains(p2, "已有的句型") || strings.Contains(p2, "单词和短语") {
		t.Fatalf("empty sections should be omitted: %q", p2)
	}
}

func TestBuildVariantPrompt(t *testing.T) {
	p := BuildVariantPrompt("仿写。", VariantInput{
		Origins: []PastedOrigin{{En: "I find making word cards useful.", Cn: "我发现做单词卡很有用。"}, {En: "It is fun."}},
		Modes:   []string{ModeReplace, ModeTransform},
		PerItem: 3,
		Words:   testWords[2:],
	})
	want := strings.Join([]string{"仿写。", "", "改造方式：替换（保留句型，换内容）、转换（肯定 ↔ 否定 ↔ 疑问，时态，主动 ↔ 被动）", "每个例句写 3 个变式。", "",
		"例句（2 句）：", "1. I find making word cards useful. | 我发现做单词卡很有用。", "2. It is fun. | （中文请补上）", "",
		"替换时优先使用的词汇（1 个）：", "1. be good at —— 擅长"}, "\n")
	if p != want {
		t.Fatalf("got %q\nwant %q", p, want)
	}
}

func TestBuildRewritePrompt(t *testing.T) {
	p := BuildRewritePrompt(RewriteInput{Kind: PromptVariant, Level: LevelPrimary, En: "I find it useful.", Cn: "我觉得有用。", Issues: []string{"超纲词：useful"}, Origin: "I like it."})
	for _, s := range []string{"小学", "12 词", "原句：I find it useful.", "中文：我觉得有用。", "- 超纲词：useful", "仿写的原句：I like it."} {
		if !strings.Contains(p, s) {
			t.Errorf("rewrite prompt missing %q: %q", s, p)
		}
	}
	p2 := BuildRewritePrompt(RewriteInput{Kind: PromptExample, Level: LevelJunior, En: "x", Target: "apple"})
	if !strings.Contains(p2, "必须用上：apple") || strings.Contains(p2, "仿写的原句") || !strings.Contains(p2, "（没有标出问题") {
		t.Errorf("p2 = %q", p2)
	}
	if !strings.Contains(RewriteOutputFormat, `"en"`) || !strings.Contains(RewriteOutputFormat, `"cn"`) {
		t.Error("rewrite format")
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
	v2 := ToPromptsView(StoredPromptTemplates{PromptVariant: "v"})
	if v2.Variant.Current != "v" || v2.Variant.IsDefault || v2.Variant.Key != PromptVariant {
		t.Fatalf("variant = %+v", v2.Variant)
	}
	if !v2.Pattern.IsDefault || v2.Pattern.Current != DefaultPromptTemplates[PromptPattern] {
		t.Fatalf("pattern = %+v", v2.Pattern)
	}
	if got := ParseStoredTemplates(map[string]any{"pattern": "p", "variant": "q"}); got[PromptPattern] != "p" || got[PromptVariant] != "q" {
		t.Fatalf("parse 4 keys = %v", got)
	}
}
