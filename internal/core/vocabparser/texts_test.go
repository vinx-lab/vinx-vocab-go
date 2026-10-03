package vocabparser

import (
	"slices"
	"testing"
)

const unitWithTexts = `Unit 1
guitar /ɡɪˈtɑː(r)/ n. 吉他
[句型]
How do you learn English words? | 你是如何学习英文单词的？
I find ... useful. | 我发现……很有用。 | I find making word cards useful.
[课文]
Title: How I Learn English | 我是怎样学英语的
I used to find English hard. | 我过去觉得英语很难。
Then I started to read aloud every day. | 后来我开始每天大声朗读。

Now I can understand more. | 现在我能听懂更多了。
Unit 2
violin /ˌvaɪəˈlɪn/ n. 小提琴
`

func TestTextSections(t *testing.T) {
	units := ParseVocabText(unitWithTexts, "")
	if len(units) != 2 {
		t.Fatalf("units = %+v", units)
	}
	u1 := units[0]
	if len(u1.Entries) != 1 || u1.Entries[0].Spelling != "guitar" {
		t.Errorf("句型 / 课文行不应当成词条：%+v", u1.Entries)
	}
	if len(u1.Texts) != 2 {
		t.Fatalf("Unit 1 texts = %+v", u1.Texts)
	}

	list := u1.Texts[0]
	if list.Kind != "list" || list.Title != "重点句型" || len(list.Sentences) != 2 {
		t.Fatalf("句型段 = %+v", list)
	}
	s0 := list.Sentences[0]
	if s0.En != "How do you learn English words?" || s0.Cn != "你是如何学习英文单词的？" || s0.Frame != "" || s0.Status != StatusOK || s0.Line != 4 {
		t.Errorf("句型第 1 行 = %+v", s0)
	}
	// 三列句型：骨架 + 中文 + 完整句；默写考完整句
	s1 := list.Sentences[1]
	if s1.En != "I find making word cards useful." || s1.Frame != "I find ... useful." || s1.Cn != "我发现……很有用。" || s1.Status != StatusOK {
		t.Errorf("三列句型 = %+v", s1)
	}

	text := u1.Texts[1]
	if text.Kind != "text" || text.Title != "How I Learn English" || text.TitleCn != "我是怎样学英语的" || len(text.Sentences) != 3 {
		t.Fatalf("课文段 = %+v", text)
	}
	paras := []int{}
	for _, s := range text.Sentences {
		paras = append(paras, s.Paragraph)
	}
	if !slices.Equal(paras, []int{0, 0, 1}) {
		t.Errorf("空行分段 = %v", paras)
	}

	// 下一个 Unit 标题结束课文段，词条照常解析
	if units[1].Name != "Unit 2" || len(units[1].Entries) != 1 || len(units[1].Texts) != 0 {
		t.Errorf("Unit 2 = %+v", units[1])
	}
}

func TestTextSectionIssues(t *testing.T) {
	units := ParseVocabText("Unit 3\n[句型]\nI think ... is fun. | 我认为……很有趣。\nNo Chinese here\n| 只有中文\n[课文]\nA. | 一。 | 多余\n", "")
	if len(units) != 1 {
		t.Fatalf("只有句型、课文的单元也要保留：%+v", units)
	}
	list := units[0].Texts[0]
	if len(list.Sentences) != 3 {
		t.Fatalf("句型 = %+v", list.Sentences)
	}
	// 带 ... 但没有完整句：warning，英文用骨架
	if s := list.Sentences[0]; s.Status != StatusWarning || s.En != "I think ... is fun." || s.Frame != "I think ... is fun." || !slices.Contains(s.Issues, "句型缺少完整例句") {
		t.Errorf("缺完整句 = %+v", s)
	}
	if s := list.Sentences[1]; s.Status != StatusError || !slices.Contains(s.Issues, "缺少中文") {
		t.Errorf("缺中文 = %+v", s)
	}
	if s := list.Sentences[2]; s.Status != StatusError || !slices.Contains(s.Issues, "缺少英文") {
		t.Errorf("缺英文 = %+v", s)
	}
	text := units[0].Texts[1]
	if text.Title != "课文" {
		t.Errorf("没有 Title 行时默认标题 = %q", text.Title)
	}
	if s := text.Sentences[0]; s.Status != StatusWarning || s.En != "A." || s.Cn != "一。" {
		t.Errorf("多余的列 = %+v", s)
	}
}

func TestParseUnitTexts(t *testing.T) {
	// 单元页面粘贴导入：不需要 Unit 标题；第一个段落标记之前的行按默认类型
	texts := ParseUnitTexts("Hello! | 你好！\n[课文]\nTitle: Hi\nHi. | 嗨。\n", "list")
	if len(texts) != 2 || texts[0].Kind != "list" || len(texts[0].Sentences) != 1 || texts[1].Kind != "text" || texts[1].Title != "Hi" || texts[1].TitleCn != "" {
		t.Fatalf("texts = %+v", texts)
	}
	// Unit 标题在这里不是标题
	texts = ParseUnitTexts("Unit 1\n", "text")
	if len(texts) != 1 || texts[0].Sentences[0].Status != StatusError {
		t.Errorf("Unit 行 = %+v", texts)
	}
	if got := ParseUnitTexts("  \n", "text"); len(got) != 0 {
		t.Errorf("空文本 = %+v", got)
	}
}
