package core

import (
	"reflect"
	"testing"
	"time"
)

func ptr(s string) *string { return &s }

func wi(t, id string) DictationItem {
	if IsSentenceItem(t) {
		return DictationItem{Type: t, SentenceID: id}
	}
	return DictationItem{Type: t, WordID: id}
}

func itemIDs(sheet []DictationItem) []string {
	out := []string{}
	for _, it := range sheet {
		if it.SentenceID != "" {
			out = append(out, it.SentenceID)
		} else {
			out = append(out, it.WordID)
		}
	}
	return out
}

func TestSplitDictation(t *testing.T) {
	lim := DictationLimits{Words: 2, Phrases: 1, Sentences: 2}
	cases := []struct {
		name    string
		items   []DictationItem
		copies  int
		want    [][]string
		wantErr string
	}{
		{"一份按分区排序", []DictationItem{wi("sentence", "s1"), wi("word", "w1"), wi("frame", "s2"), wi("phrase", "p1")}, 1,
			[][]string{{"w1", "p1", "s1", "s2"}}, ""},
		{"仿写与转换排在句子后", []DictationItem{wi("transform", "s3"), wi("sentence", "s1")}, 1, [][]string{{"s1", "s3"}}, ""},
		{"去重", []DictationItem{wi("word", "w1"), wi("word", "w1"), wi("sentence", "s1"), wi("frame", "s1")}, 1, [][]string{{"w1", "s1"}}, ""},
		{"两份平均分，前面的多", []DictationItem{wi("word", "w1"), wi("word", "w2"), wi("word", "w3"), wi("sentence", "s1"), wi("sentence", "s2"), wi("sentence", "s3")}, 2,
			[][]string{{"w1", "w2", "s1", "s2"}, {"w3", "s3"}}, ""},
		{"份数不超过题数", []DictationItem{wi("word", "w1")}, 3, [][]string{{"w1"}}, ""},
		{"短语只有一题时只在第一份", []DictationItem{wi("word", "w1"), wi("word", "w2"), wi("phrase", "p1")}, 2, [][]string{{"w1", "p1"}, {"w2"}}, ""},
		{"单词超上限", []DictationItem{wi("word", "w1"), wi("word", "w2"), wi("word", "w3")}, 1, nil, "单词 3 题，超过 1 份 × 2 题"},
		{"短语超上限", []DictationItem{wi("phrase", "p1"), wi("phrase", "p2"), wi("phrase", "p3")}, 2, nil, "短语 3 题，超过 2 份 × 1 题"},
		{"句子超上限", []DictationItem{wi("sentence", "s1"), wi("frame", "s2"), wi("transform", "s3")}, 1, nil, "句子 3 题，超过 1 份 × 2 题"},
		{"空", nil, 2, [][]string{}, ""},
		{"未知题型", []DictationItem{{Type: "x", WordID: "w"}}, 1, nil, "未知题型 x"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := SplitDictation(c.items, c.copies, lim)
			if c.wantErr != "" {
				if err == nil || err.Error() != c.wantErr {
					t.Fatalf("err = %v, want %q", err, c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			ids := [][]string{}
			for _, s := range got {
				ids = append(ids, itemIDs(s))
			}
			if !reflect.DeepEqual(ids, c.want) {
				t.Fatalf("got %v, want %v", ids, c.want)
			}
		})
	}
}

func TestSentenceStatus(t *testing.T) {
	t0 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		in   []SentenceAnswerFact
		want CoverageStatus
	}{
		{"没有作答", nil, CoverageUntested},
		{"最近一次错", []SentenceAnswerFact{{true, t0}, {false, t0.Add(time.Hour)}}, CoverageLearning},
		{"最近一次对", []SentenceAnswerFact{{false, t0}, {true, t0.Add(time.Hour)}}, CoverageKnown},
		{"乱序也按时间", []SentenceAnswerFact{{true, t0.Add(2 * time.Hour)}, {false, t0}}, CoverageKnown},
		{"同一时刻以靠后的为准", []SentenceAnswerFact{{true, t0}, {false, t0}}, CoverageLearning},
	}
	for _, c := range cases {
		if got := SentenceStatus(c.in); got != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
}

func TestOrderDictationSentences(t *testing.T) {
	got := OrderDictationSentences([]SentenceCandidate{
		{"a", CoverageKnown}, {"b", CoverageUntested}, {"c", CoverageLearning}, {"d", CoverageUntested}, {"c", CoverageLearning}, {"e", CoverageLearning},
	})
	want := []string{"c", "e", "b", "d", "a"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestDictationSentenceType(t *testing.T) {
	cases := []struct {
		name                string
		source              string
		frame, note, origin *string
		want                string
		allowed             []string
	}{
		{"课文句", "text", nil, nil, nil, DictSentence, []string{DictSentence}},
		{"带骨架", "ai", ptr("I find ... useful."), nil, nil, DictFrame, []string{DictSentence, DictFrame}},
		{"空骨架", "ai", ptr("  "), nil, nil, DictSentence, []string{DictSentence}},
		{"转换仿写", "variant", ptr("I ... it."), ptr("转换：改成一般疑问句"), ptr("o1"), DictTransform, []string{DictSentence, DictFrame, DictTransform}},
		{"替换仿写", "variant", nil, ptr("替换：换了宾语"), ptr("o1"), DictSentence, []string{DictSentence}},
		{"转换但没有原句", "variant", nil, ptr("转换"), nil, DictSentence, []string{DictSentence}},
		{"不是仿写", "ai", nil, ptr("转换"), ptr("o1"), DictSentence, []string{DictSentence}},
	}
	for _, c := range cases {
		if got := DictationSentenceType(c.source, c.frame, c.note, c.origin); got != c.want {
			t.Errorf("%s: type = %s, want %s", c.name, got, c.want)
		}
		allowed := []string{}
		for _, ty := range []string{DictSentence, DictFrame, DictTransform, DictWord} {
			if DictationSentenceTypeAllowed(ty, c.source, c.frame, c.note, c.origin) {
				allowed = append(allowed, ty)
			}
		}
		if !reflect.DeepEqual(allowed, c.allowed) {
			t.Errorf("%s: allowed = %v, want %v", c.name, allowed, c.allowed)
		}
	}
}

func TestDictationPrompt(t *testing.T) {
	cases := []struct {
		name string
		in   DictationPromptInput
		want string
	}{
		{"单词带词性", DictationPromptInput{Type: DictWord, PartOfSpeech: ptr("n."), Definition: "句子"}, "n. 句子"},
		{"单词无词性", DictationPromptInput{Type: DictWord, PartOfSpeech: ptr(""), Definition: "句子"}, "句子"},
		{"短语", DictationPromptInput{Type: DictPhrase, PartOfSpeech: ptr("v."), Definition: "大声朗读"}, "大声朗读"},
		{"句子", DictationPromptInput{Type: DictSentence, Cn: "我喜欢英语。"}, "我喜欢英语。"},
		{"仿写", DictationPromptInput{Type: DictFrame, Frame: ptr("I find ... useful."), Cn: "我发现做笔记很有用"}, "I find ___ useful.（我发现做笔记很有用）"},
		{"仿写省略号", DictationPromptInput{Type: DictFrame, Frame: ptr("I find … useful."), Cn: ""}, "I find ___ useful."},
		{"转换", DictationPromptInput{Type: DictTransform, OriginEn: "I like it.", VariantNote: ptr("转换：改成一般疑问句")}, "I like it.（改成一般疑问句）"},
		{"转换只有标签", DictationPromptInput{Type: DictTransform, OriginEn: "I like it.", VariantNote: ptr("转换")}, "I like it.（按要求转换句式）"},
		{"转换英文冒号", DictationPromptInput{Type: DictTransform, OriginEn: "I like it.", VariantNote: ptr("转换: 改成否定句")}, "I like it.（改成否定句）"},
	}
	for _, c := range cases {
		if got := DictationPrompt(c.in); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestValidateGradeResults(t *testing.T) {
	ua := ptr("x")
	cases := []struct {
		name    string
		idx     []int
		in      []GradeResult
		want    []int
		wantErr string
	}{
		{"全部且排序", []int{0, 1, 2}, []GradeResult{{2, true, nil}, {0, false, ua}, {1, true, nil}}, []int{0, 1, 2}, ""},
		{"缺题", []int{0, 1, 2}, []GradeResult{{0, true, nil}, {1, true, nil}}, nil, "需要批改全部 3 道题"},
		{"越界", []int{0, 1}, []GradeResult{{0, true, nil}, {2, true, nil}}, nil, "题号 2 超出范围"},
		{"负数", []int{0, 1}, []GradeResult{{-1, true, nil}}, nil, "题号 -1 超出范围"},
		{"重复", []int{0, 1}, []GradeResult{{0, true, nil}, {0, false, nil}}, nil, "题号 0 重复"},
		{"已删除的题不用批改", []int{0, 2}, []GradeResult{{2, true, nil}, {0, true, nil}}, []int{0, 2}, ""},
		{"已删除的题不能提交", []int{0, 2}, []GradeResult{{0, true, nil}, {1, true, nil}, {2, true, nil}}, nil, "题号 1 超出范围"},
	}
	for _, c := range cases {
		got, err := ValidateGradeResults(c.idx, c.in)
		if c.wantErr != "" {
			if err == nil || err.Error() != c.wantErr {
				t.Errorf("%s: err = %v, want %q", c.name, err, c.wantErr)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		idx := []int{}
		for _, r := range got {
			idx = append(idx, r.Index)
		}
		if !reflect.DeepEqual(idx, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, idx, c.want)
		}
	}
}

func TestIsSelfGraded(t *testing.T) {
	if !IsSelfGraded("s", "s") || IsSelfGraded("t", "s") {
		t.Fatal("IsSelfGraded")
	}
}

func TestDictationSection(t *testing.T) {
	want := map[string]int{DictWord: 1, DictPhrase: 2, DictSentence: 3, DictFrame: 4, DictTransform: 4, "x": 0}
	for ty, n := range want {
		if DictationSection(ty) != n {
			t.Errorf("%s: %d", ty, DictationSection(ty))
		}
	}
}
