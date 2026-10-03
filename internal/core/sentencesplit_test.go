package core

import (
	"reflect"
	"testing"
)

func TestSplitSentencesEn(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"I like it. Do you? Yes!", []string{"I like it.", "Do you?", "Yes!"}},
		{"Mr. Smith gets up at 6 a.m. every day. He is busy.", []string{"Mr. Smith gets up at 6 a.m. every day.", "He is busy."}},
		{`"Really?" she asked. "Yes."`, []string{`"Really?" she asked.`, `"Yes."`}},
		{"It costs 3.5 yuan. No end", []string{"It costs 3.5 yuan.", "No end"}},
		{"Wait... what?! OK.", []string{"Wait... what?!", "OK."}},
		{"  ", nil},
	}
	for _, c := range cases {
		if got := SplitSentencesEn(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("SplitSentencesEn(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSplitSentencesCn(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"我喜欢它。你呢？是的！", []string{"我喜欢它。", "你呢？", "是的！"}},
		{"“真的吗？”她问。“是的。”", []string{"“真的吗？”她问。", "“是的。”"}},
		{"他说：好的…… 走吧", []string{"他说：好的…… 走吧"}},
		{"Hello. 你好。", []string{"Hello.", "你好。"}},
	}
	for _, c := range cases {
		if got := SplitSentencesCn(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("SplitSentencesCn(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSplitPassage(t *testing.T) {
	// 句数一致：逐句对应，英文空行 / 换行分段
	lines, ok := SplitPassage("I get up early. I run.\n\nThen I eat breakfast!", "我起得早。我跑步。然后我吃早饭！")
	if !ok {
		t.Fatal("句数一致应能拆分")
	}
	want := []PassageLine{
		{En: "I get up early.", Cn: "我起得早。", Paragraph: 0},
		{En: "I run.", Cn: "我跑步。", Paragraph: 0},
		{En: "Then I eat breakfast!", Cn: "然后我吃早饭！", Paragraph: 1},
	}
	if !reflect.DeepEqual(lines, want) {
		t.Errorf("lines = %+v", lines)
	}

	// 句数不一致：不拆
	if _, ok := SplitPassage("One. Two.", "一。"); ok {
		t.Error("句数不一致不应拆分")
	}
	// 没有译文：不拆
	if _, ok := SplitPassage("One. Two.", ""); ok {
		t.Error("没有译文不应拆分")
	}
}
