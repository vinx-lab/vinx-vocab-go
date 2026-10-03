package ai

import (
	"slices"
	"testing"
)

func TestParseExampleItems(t *testing.T) {
	cases := []struct {
		name  string
		reply string
		want  []ExampleItem
	}{
		{"新字段", `[{"spelling":"apple","en":"I eat an apple.","cn":"我吃苹果。"}]`, []ExampleItem{{Spelling: "apple", En: "I eat an apple.", Cn: "我吃苹果。"}}},
		{"旧字段名", `[{"spelling":"apple","example":"I eat an apple.","exampleCn":"我吃苹果。"}]`, []ExampleItem{{Spelling: "apple", En: "I eat an apple.", Cn: "我吃苹果。"}}},
		{"代码块 + 说明文字", "好的：\n```json\n[{\"spelling\":\"run\",\"en\":\" I run. \",\"cn\":\"我跑。\"}]\n```", []ExampleItem{{Spelling: "run", En: "I run.", Cn: "我跑。"}}},
		{"items 包裹 + 多段", `{"items":[{"spelling":"a","en":"x","cn":"y"}]} [{"spelling":"b","en":"z","cn":"w"}]`, []ExampleItem{{Spelling: "a", En: "x", Cn: "y"}, {Spelling: "b", En: "z", Cn: "w"}}},
		{"字段类型不对不连累其他项", `[{"spelling":1,"en":2,"cn":true}, "bad", {"spelling":"c","en":"e","cn":"f"}]`, []ExampleItem{{Spelling: "1", En: "2", Cn: "true"}, {}, {Spelling: "c", En: "e", Cn: "f"}}},
	}
	for _, c := range cases {
		got, err := ParseExampleItems(c.reply)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if !slices.Equal(got, c.want) {
			t.Errorf("%s: got %+v want %+v", c.name, got, c.want)
		}
	}
	if _, err := ParseExampleItems("没有 JSON"); err == nil {
		t.Error("unparseable should error")
	}
}

func TestParsePatternItems(t *testing.T) {
	got, err := ParsePatternItems("```json\n[{\"en\":\"How do you learn English?\",\"cn\":\"你怎样学英语？\"},{\"frame\":\"I learn ... by doing ...\",\"en\":\"I learn English by reading.\",\"cn\":\"我通过阅读学英语。\"},{\"en\":\"\",\"cn\":\"空\"},{\"en\":\"No cn.\"}]\n```")
	if err != nil {
		t.Fatal(err)
	}
	frame := "I learn ... by doing ..."
	if len(got) != 3 {
		t.Fatalf("got %+v", got)
	}
	if got[0].En != "How do you learn English?" || got[0].Frame != nil || got[0].Cn != "你怎样学英语？" {
		t.Errorf("0 = %+v", got[0])
	}
	if got[1].Frame == nil || *got[1].Frame != frame || got[1].En != "I learn English by reading." {
		t.Errorf("1 = %+v", got[1])
	}
	if got[2].En != "No cn." || got[2].Cn != "" {
		t.Errorf("2 = %+v", got[2])
	}
	// 轻微格式错误：尾逗号修不了的整体解析失败
	if _, err := ParsePatternItems(`[{"en":"x",}]`); err == nil {
		t.Error("broken json should error")
	}
}

func TestParseVariantItems(t *testing.T) {
	reply := `[{"origin":1,"en":"I find reading aloud helpful.","cn":"我发现朗读有帮助。","change":"替换","note":"换了动名词和形容词"},
{"origin":"2","en":"Do you find it useful?","cn":"你觉得它有用吗？","change":"转换","note":"改成一般疑问句"},
{"origin":9,"en":"out of range","cn":"x","change":"扩展","note":""},
{"origin":2,"en":"","cn":"empty en"},
{"en":"no origin","cn":"y","change":"迁移"}]`
	got, err := ParseVariantItems(reply, 2)
	if err != nil {
		t.Fatal(err)
	}
	want := []VariantItem{
		{Origin: 1, En: "I find reading aloud helpful.", Cn: "我发现朗读有帮助。", Change: ModeReplace, Note: "换了动名词和形容词"},
		{Origin: 2, En: "Do you find it useful?", Cn: "你觉得它有用吗？", Change: ModeTransform, Note: "改成一般疑问句"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %+v", got)
	}
	// 只有一个原句时，缺 origin 的归到第 1 句
	got, _ = ParseVariantItems(`[{"en":"a","cn":"b","change":"迁移"}]`, 1)
	if len(got) != 1 || got[0].Origin != 1 || got[0].Change != ModeTransfer {
		t.Fatalf("single = %+v", got)
	}
}

func TestParsePassageReply(t *testing.T) {
	reply := "<think>先想想</think>```json\n{\"title\":\"A Day\",\"titleCn\":\"一天\",\"sentences\":[{\"en\":\"I get up early.\",\"cn\":\"我起得早。\",\"paragraph\":0},{\"en\":\"Then I run.\",\"cn\":\"然后我跑步。\",\"paragraph\":\"0\"},{\"en\":\"At night I read.\",\"cn\":\"晚上我读书。\",\"paragraph\":1},{\"en\":\" \",\"cn\":\"空\"}],\"questions\":[{\"q\":\"何时起床？\",\"a\":\"很早\"}]}\n```"
	got, err := ParsePassageReply(reply)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "A Day" || got.TitleCn != "一天" || got.Legacy {
		t.Fatalf("got %+v", got)
	}
	want := []PassageSentenceItem{{En: "I get up early.", Cn: "我起得早。", Paragraph: 0}, {En: "Then I run.", Cn: "然后我跑步。", Paragraph: 0}, {En: "At night I read.", Cn: "晚上我读书。", Paragraph: 1}}
	if !slices.Equal(got.Sentences, want) {
		t.Fatalf("sentences = %+v", got.Sentences)
	}
	if len(got.Questions) != 1 || got.Questions[0].Q != "何时起床？" {
		t.Fatalf("questions = %+v", got.Questions)
	}
	if got.Body != "I get up early. Then I run.\n\nAt night I read." || got.BodyCn != "我起得早。然后我跑步。\n\n晚上我读书。" {
		t.Fatalf("body = %q / %q", got.Body, got.BodyCn)
	}

	// 旧格式：整段 passage
	old, err := ParsePassageReply(`{"title":123,"passage":"I run. You run.","passageCn":"我跑。你跑。","questions":"none"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !old.Legacy || old.Body != "I run. You run." || old.BodyCn != "我跑。你跑。" || old.Title != "123" || len(old.Questions) != 0 || len(old.Sentences) != 0 {
		t.Fatalf("legacy = %+v", old)
	}
	// 段落号缺省时：没有任何段落号 → 全部第 0 段
	np, _ := ParsePassageReply(`{"sentences":[{"en":"A.","cn":"甲。"},{"en":"B.","cn":"乙。"}]}`)
	if np.Title != "Reading" || np.Body != "A. B." || np.Sentences[1].Paragraph != 0 {
		t.Fatalf("no paragraph = %+v", np)
	}
	// 没有短文
	if _, err := ParsePassageReply(`{"title":"x","sentences":[]}`); err == nil {
		t.Fatal("empty passage should error")
	}
	// 问题最多 5 道
	many, _ := ParsePassageReply(`{"passage":"x","questions":[{"q":"1"},{"q":"2"},{"q":"3"},{"q":"4"},{"q":"5"},{"q":"6"}]}`)
	if len(many.Questions) != 5 {
		t.Fatalf("questions = %d", len(many.Questions))
	}
}

func TestParseRewriteReply(t *testing.T) {
	cases := []struct {
		reply string
		want  RewriteItem
		ok    bool
	}{
		{`{"en":"I like it.","cn":"我喜欢。"}`, RewriteItem{En: "I like it.", Cn: "我喜欢。"}, true},
		{`[{"en":"I like it.","cn":"我喜欢。"}]`, RewriteItem{En: "I like it.", Cn: "我喜欢。"}, true},
		{"```json\n{\"en\":\" x \",\"cn\":\"y\",\"frame\":\"f ...\"}\n```", RewriteItem{En: "x", Cn: "y", Frame: "f ..."}, true},
		{`{"cn":"只有中文"}`, RewriteItem{}, false},
		{`nothing`, RewriteItem{}, false},
	}
	for _, c := range cases {
		got, err := ParseRewriteReply(c.reply)
		if (err == nil) != c.ok || (c.ok && got != c.want) {
			t.Errorf("ParseRewriteReply(%q) = %+v, %v", c.reply, got, err)
		}
	}
}
