package vocabparser

import (
	"reflect"
	"slices"
	"testing"
)

// 逐条翻译旧 tests/lib/vocab-parser.test.ts

func first(t *testing.T, text string) ParsedEntry {
	t.Helper()
	units := ParseVocabText(text, "")
	if len(units) == 0 || len(units[0].Entries) == 0 {
		t.Fatalf("没有解析出条目：%q", text)
	}
	return units[0].Entries[0]
}

func TestSections(t *testing.T) {
	// 按 Unit 标题分节，跳过空行
	r := ParseVocabText("Unit 1\n\noutstanding /aʊt'stændɪŋ/ adj. 优秀的;杰出的\n\nUnit 2\nremove /rɪ'muːv/ v. 去掉\n", "")
	if names := []string{r[0].Name, r[1].Name}; len(r) != 2 || !reflect.DeepEqual(names, []string{"Unit 1", "Unit 2"}) {
		t.Fatalf("units = %+v", r)
	}
	if len(r[0].Entries) != 1 {
		t.Errorf("Unit 1 entries = %d", len(r[0].Entries))
	}

	// 无标题归入默认单元，Starter/Module 也识别为标题
	cases := map[string]string{
		"abandon /əˈbændən/ v. 放弃\n":               "未分组",
		"Starter Unit 1\nhello /hə'ləʊ/ int. 你好\n": "Starter Unit 1",
		"# 自定义组\nhello /hə'ləʊ/ int. 你好\n":         "自定义组",
	}
	for in, want := range cases {
		if got := ParseVocabText(in, "")[0].Name; got != want {
			t.Errorf("unit name of %q = %q, want %q", in, got, want)
		}
	}

	// 重复标题合并到同一单元
	r = ParseVocabText("Unit 1\na /ə/ art. 一\nUnit 1\nb /biː/ n. 字母B\n", "")
	if len(r) != 1 || len(r[0].Entries) != 2 {
		t.Errorf("合并失败：%+v", r)
	}
}

func TestEntries(t *testing.T) {
	// 标准单词行
	e := first(t, "dinosaur /'daɪnəsɔː(r)/ n. 恐龙\n")
	if e.Spelling != "dinosaur" || e.Phonetic != "'daɪnəsɔː(r)" || e.PartOfSpeech != "n." || e.Definition != "恐龙" || e.Type != "word" || e.Status != "ok" {
		t.Errorf("标准单词行 = %+v", e)
	}

	// 多词性段保留在释义中
	e = first(t, "about /əˈbaʊt/ ad. 大约;到处 prep. 关于\n")
	if e.PartOfSpeech != "ad." || e.Definition != "大约;到处 prep. 关于" {
		t.Errorf("多词性 = %+v", e)
	}

	// 组合词性 prep./ad.
	e = first(t, "across /əˈkrɔs/ prep./ad. 横过;穿过\n")
	if e.PartOfSpeech != "prep./ad." || e.Definition != "横过;穿过" {
		t.Errorf("组合词性 = %+v", e)
	}

	// 短语行：无音标无词性
	e = first(t, "be good at 擅长……\n")
	if e.Spelling != "be good at" || e.Type != "phrase" || e.Definition != "擅长……" || e.Status != "ok" {
		t.Errorf("短语 = %+v", e)
	}

	// 中英粘连无空格
	e = first(t, "fresh in one's memory记忆犹新\n")
	if e.Spelling != "fresh in one's memory" || e.Definition != "记忆犹新" {
		t.Errorf("粘连 = %+v", e)
	}

	// 星号与序号前缀被剥离
	for _, in := range []string{"*reject /rɪ'dʒekt/ v. 拒绝\n", "12. reject /rɪ'dʒekt/ v. 拒绝\n"} {
		if got := first(t, in).Spelling; got != "reject" {
			t.Errorf("前缀 %q → %q", in, got)
		}
	}

	// 音标只有右斜杠 → warning
	e = first(t, "pardon 'pɑːdn/ int. 请再说一遍\n")
	if e.Spelling != "pardon" || e.Phonetic != "pɑːdn" || e.Status != "warning" {
		t.Errorf("半个音标 = %+v", e)
	}

	// 无音标但有词性的单词 → warning 缺少音标
	e = first(t, "abandon v. 放弃\n")
	if e.Spelling != "abandon" || e.PartOfSpeech != "v." || !slices.Contains(e.Issues, "缺少音标") {
		t.Errorf("缺少音标 = %+v", e)
	}

	// 两词粘连 → warning
	if e = first(t, "match /mætʃ/ n. 火柴 shame /ʃeɪm/ n. 遗憾\n"); e.Status != "warning" {
		t.Errorf("两词粘连 = %+v", e)
	}

	// 无中文释义 → error
	for _, in := range []string{"decision /dɪ'sɪʒn/ n.\n", "hello world\n"} {
		if e = first(t, in); e.Status != "error" {
			t.Errorf("%q status = %s", in, e.Status)
		}
	}
}

func TestHelpers(t *testing.T) {
	if got := NormalizeSpelling("  be   good  at "); got != "be good at" {
		t.Errorf("NormalizeSpelling = %q", got)
	}
	// JS \s 含全角空格
	if got := NormalizeSpelling("　be　 good "); got != "be good" {
		t.Errorf("NormalizeSpelling 全角空格 = %q", got)
	}

	r := SplitByInitial(ParseVocabText("banana /bə'nɑːnə/ n. 香蕉\napple /'æpl/ n. 苹果\nboy /bɔɪ/ n. 男孩\n", ""))
	got := [][2]any{}
	for _, u := range r {
		got = append(got, [2]any{u.Name, len(u.Entries)})
	}
	if !reflect.DeepEqual(got, [][2]any{{"A", 1}, {"B", 2}}) {
		t.Errorf("SplitByInitial = %v", got)
	}

	u := ParseVocabText("Apple /'æpl/ n. 苹果\napple /'æpl/ n. 苹果\n", "")[0]
	if n := len(DedupeUnitEntries(u).Entries); n != 1 {
		t.Errorf("DedupeUnitEntries = %d", n)
	}
}
