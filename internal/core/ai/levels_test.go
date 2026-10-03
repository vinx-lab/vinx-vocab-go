package ai

import (
	"strings"
	"testing"
)

func strp(s string) *string { return &s }

func TestFillPlaceholders(t *testing.T) {
	cases := []struct {
		level Level
		tpl   string
		want  string
	}{
		{LevelJunior, "你是{学段}英语老师，长度 {句长}。", "你是初中英语老师，长度 6～14 词。"},
		{LevelPrimary, "总长 {短文长度}，单句不超过 {单句上限}", "总长 50～80 词，单句不超过 12 词"},
		{LevelExam, "{学段}{学段} {单句上限}", "中考中考 25 词"},
		{LevelJunior, "没有占位符", "没有占位符"},
		{"bogus", "{学段}", "初中"}, // 不认识的学段按初中
		{LevelJunior, "{未知}", "{未知}"},
	}
	for _, c := range cases {
		if got := FillPlaceholders(c.tpl, c.level); got != c.want {
			t.Errorf("FillPlaceholders(%q, %s) = %q, want %q", c.tpl, c.level, got, c.want)
		}
	}
}

func TestLevelParams(t *testing.T) {
	cases := []struct {
		level   Level
		name    string
		maxWord int
	}{
		{LevelPrimary, "小学", 12},
		{LevelJunior, "初中", 20},
		{LevelExam, "中考", 25},
	}
	for _, c := range cases {
		p := ParamsOf(c.level)
		if p.Name != c.name || p.MaxWords != c.maxWord {
			t.Errorf("%s = %+v", c.level, p)
		}
	}
	if ParamsOf("x").Name != "初中" {
		t.Error("unknown level should fall back to junior")
	}
}

func TestMaxLevel(t *testing.T) {
	cases := []struct {
		in   []*string
		want Level
	}{
		{nil, LevelJunior},
		{[]*string{nil, nil}, LevelJunior},
		{[]*string{strp(LevelPrimary)}, LevelPrimary},
		{[]*string{strp(LevelPrimary), strp(LevelExam), strp(LevelJunior)}, LevelExam},
		{[]*string{strp(LevelPrimary), nil, strp("bogus")}, LevelPrimary},
		{[]*string{strp(LevelJunior), strp(LevelPrimary)}, LevelJunior},
	}
	for i, c := range cases {
		if got := MaxLevel(c.in); got != c.want {
			t.Errorf("#%d MaxLevel = %s, want %s", i, got, c.want)
		}
	}
}

func TestLevelOr(t *testing.T) {
	if LevelOr(nil, LevelExam) != LevelExam || LevelOr(strp("bogus"), LevelExam) != LevelExam || LevelOr(strp(LevelPrimary), LevelExam) != LevelPrimary {
		t.Fatal("LevelOr")
	}
	if !IsLevel(LevelPrimary) || IsLevel("") || IsLevel("senior") {
		t.Fatal("IsLevel")
	}
}

func TestLevelForBookName(t *testing.T) {
	cases := []struct {
		name string
		want *string
	}{
		{"七年级上册", strp(LevelJunior)},
		{"七年级下册", strp(LevelJunior)},
		{"八年级上册", strp(LevelJunior)},
		{"八年级下册", strp(LevelJunior)},
		{"九年级上册", strp(LevelJunior)},
		{"中考核心词汇", strp(LevelExam)},
		{"中考差集", strp(LevelExam)},
		{"我的词书", nil},
		{"", nil},
	}
	for _, c := range cases {
		got := LevelForBookName(c.name)
		if (got == nil) != (c.want == nil) || (got != nil && *got != *c.want) {
			t.Errorf("LevelForBookName(%q) = %v", c.name, got)
		}
	}
}

func TestDefaultTemplatesUsePlaceholders(t *testing.T) {
	for _, k := range PromptKeys {
		tpl := DefaultPromptTemplates[k]
		if !strings.Contains(tpl, "{学段}") {
			t.Errorf("%s template should use {学段}", k)
		}
		filled := FillPlaceholders(tpl, LevelPrimary)
		if strings.Contains(filled, "{") {
			t.Errorf("%s filled template still has placeholder: %q", k, filled)
		}
		if strings.Contains(filled, "初中") {
			t.Errorf("%s template still hard-codes 初中", k)
		}
	}
	if !strings.Contains(DefaultPromptTemplates[PromptPassage], "{短文长度}") || !strings.Contains(DefaultPromptTemplates[PromptPassage], "单句不超过 {单句上限}") {
		t.Error("passage template should use {短文长度} and {单句上限}")
	}
}
