package ai

import (
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/vinx-lab/vinx-vocab-go/internal/core/lemma"
)

func TestIsContentPOS(t *testing.T) {
	cases := map[string]bool{
		"n.":           true,
		"v.":           true,
		"vt.":          true,
		"vi.":          true,
		"adj.":         true,
		"adv.":         true,
		"a.":           true,
		"ad.":          true,
		"n./v.":        true,
		"prep.":        false,
		"pron.":        false,
		"conj.":        false,
		"art.":         false,
		"prep. 在 ad.":  false, // 看第一个词性
		"pron. 什么 a.":  false,
		"(found) vt.":  true,
		"":             false,
		"num.":         false,
		"modal v.":     false,
		"aux. v.":      false,
		"interj.":      false,
		"v. aux.":      true,
	}
	for pos, want := range cases {
		if got := IsContentPOS(pos); got != want {
			t.Errorf("IsContentPOS(%q) = %v, want %v", pos, got, want)
		}
	}
}

// 测试用词库：词 → 词性（短语用空格）。
var structLex = map[string]string{
	"i": "pron.", "you": "pron.", "he": "pron.", "my": "pron.", "it": "pron.",
	"find": "vt.", "make": "v.", "word": "n.", "card": "n.", "useful": "adj.", "helpful": "adj.",
	"read": "v.", "aloud": "adv.", "because": "conj.", "they": "pron.", "help": "v.", "me": "pron.",
	"remember": "v.", "new": "adj.", "take": "v.", "note": "n.", "in": "prep. 在 ad.", "history": "n.",
	"class": "n.", "the": "art.", "a": "art.", "to": "prep.", "do": "v.", "be": "v.", "have": "v.",
	"what": "pron. 什么 a.", "not": "ad.", "think": "v.", "that": "conj.", "when": "conj.",
	"if": "conj.", "would": "modal v.", "like": "prep. v.", "go": "v.", "park": "n.", "with": "prep.",
	"friend": "n.", "and": "conj.", "by": "prep.", "learn": "v.", "english": "n.", "how": "adv.",
	"study": "v.", "for": "prep.", "test": "n.", "good": "adj.", "at": "prep.", "be good at": "",
}

// structWords 用 lemma 匹配器 + 测试词库构造 StructWord（与 service 的装配方式一致）。
func structWords(sentence string) []StructWord {
	var entries []lemma.Entry
	for sp := range structLex {
		entries = append(entries, lemma.Entry{ID: sp, Spelling: sp})
	}
	m := lemma.NewMatcher(entries, lemma.ParseIrregular("be\tam,is,are,was,were,been,being\nwrite\twrote,written\ntake\ttook,taken"))
	a := m.Analyze(sentence)
	return StructWordsOf(a, func(id string) (string, bool) {
		return structLex[id], strings.Contains(id, " ")
	})
}

func TestFunctionSequence(t *testing.T) {
	cases := []struct {
		sentence string
		want     []string
	}{
		{"I find making word cards useful.", []string{"<pron>"}},
		{"Do you find making word cards useful?", []string{"<do>", "<pron>"}},
		{"I don't find it useful.", []string{"<pron>", "<do>", "<pron>"}},
		{"I find making word cards useful because they help me remember new words.", []string{"<pron>", "because", "<pron>", "<pron>"}},
		{"I find taking notes useful in history class.", []string{"<pron>", "in"}},
		{"He is good at English.", []string{"<pron>"}}, // be good at 是短语，整体算实词
		{"What is the card?", []string{"what", "<be>", "the"}},
		{"Zorblax quux.", []string{}}, // 词库外的词去掉
	}
	for _, c := range cases {
		got := FunctionSequence(structWords(c.sentence))
		if got == nil {
			got = []string{}
		}
		if !slices.Equal(got, c.want) {
			t.Errorf("FunctionSequence(%q) = %v, want %v", c.sentence, got, c.want)
		}
	}
}

func TestFunctionSequenceNoPOS(t *testing.T) {
	// 没填词性的词条按实词去掉；词库外的词去掉；短语里的代词也去掉
	words := []StructWord{
		{Norm: "i", Matched: true, POS: "pron."},
		{Norm: "like", Matched: true, POS: ""},
		{Norm: "of", Matched: true, POS: "prep."},
		{Norm: "zorb", Matched: false},
		{Norm: "it", Matched: true, InPhrase: true},
	}
	if got := FunctionSequence(words); !slices.Equal(got, []string{"<pron>", "of"}) {
		t.Fatalf("got %v", got)
	}
}

func TestStructureSimilarity(t *testing.T) {
	orig := "I find making word cards useful."
	cases := []struct {
		name    string
		variant string
		similar bool
	}{
		{"替换", "I find reading aloud helpful.", true},
		{"转换：疑问", "Do you find making word cards useful?", true},
		{"转换：否定", "I don't find making word cards useful.", true},
		{"扩展", "I find making word cards useful because they help me remember new words.", true},
		{"迁移", "I find taking notes useful in history class.", true},
		{"换了句型", "Making word cards is useful.", false},
		{"换了句型 2", "What a useful card!", false},
	}
	o := FunctionSequence(structWords(orig))
	for _, c := range cases {
		v := FunctionSequence(structWords(c.variant))
		sim := StructureSimilarity(o, v)
		if got := sim >= StructureThreshold; got != c.similar {
			t.Errorf("%s: %q sim=%.2f, similar=%v want %v (orig %v variant %v)", c.name, c.variant, sim, got, c.similar, o, v)
		}
	}

	// 更复杂的原句
	orig2 := "If I have time, I would like to go to the park with my friends."
	o2 := FunctionSequence(structWords(orig2))
	for _, c := range []struct {
		variant string
		similar bool
	}{
		{"If you have time, you would like to go to the park with your friends.", true},
		{"If I have time, I would like to read in the park with my friends and learn English.", true},
		{"I go to the park.", false},
		{"How do you study for the test?", false},
	} {
		sim := StructureSimilarity(o2, FunctionSequence(structWords(c.variant)))
		if got := sim >= StructureThreshold; got != c.similar {
			t.Errorf("%q sim=%.2f want similar=%v", c.variant, sim, c.similar)
		}
	}
}

func TestStructureSimilarityEdge(t *testing.T) {
	cases := []struct {
		a, b []string
		want float64
	}{
		{nil, nil, 1},
		{nil, []string{"x"}, 1}, // 原句没有虚词：无法判断，不标
		{[]string{"a", "b"}, nil, 0},
		{[]string{"a", "b", "c", "d"}, []string{"a", "x", "c", "d"}, 0.75},
		{[]string{"a", "b"}, []string{"b", "a"}, 0.5},
		{[]string{"a"}, []string{"x", "a", "y"}, 1},
	}
	for _, c := range cases {
		if got := StructureSimilarity(c.a, c.b); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("StructureSimilarity(%v,%v) = %v want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestCountWords(t *testing.T) {
	cases := map[string]int{
		"I find it useful.":           4,
		"  ":                          0,
		"Don't run, it's 3 o'clock!": 4, // 纯数字不算
		"T-shirt and jeans":           3,
	}
	for s, want := range cases {
		if got := CountWords(lemma.Tokenize(s)); got != want {
			t.Errorf("CountWords(%q) = %d want %d", s, got, want)
		}
	}
}

func f(v float64) *float64 { return &v }

func TestBuildChecks(t *testing.T) {
	cases := []struct {
		name   string
		in     CheckInput
		issues []string
		warn   bool
		bad    bool
	}{
		{"全部通过", CheckInput{Words: 8, MaxWords: 20, Targets: []TargetUse{{Spelling: "apple", Used: true}}}, nil, false, false},
		{"没用上目标词", CheckInput{Words: 8, MaxWords: 20, Targets: []TargetUse{{Spelling: "apple", Used: false}, {Spelling: "pear", Used: true}}}, []string{"没有用上目标词：apple"}, false, true},
		{"超纲", CheckInput{Words: 8, MaxWords: 20, OutOfScope: []string{"giraffe", "zoo"}}, []string{"超纲词：giraffe、zoo"}, true, false},
		{"太长", CheckInput{Words: 21, MaxWords: 20}, []string{"句子太长：21 词，上限 20 词"}, true, false},
		{"结构偏离", CheckInput{Words: 5, MaxWords: 20, Similarity: f(0.5)}, []string{"和原句的句型结构偏离（相似度 0.50）"}, true, false},
		{"结构相似", CheckInput{Words: 5, MaxWords: 20, Similarity: f(0.6)}, nil, false, false},
		{"全部问题", CheckInput{Words: 30, MaxWords: 25, Targets: []TargetUse{{Spelling: "go", Used: false}}, OutOfScope: []string{"x"}, Similarity: f(0)},
			[]string{"没有用上目标词：go", "超纲词：x", "句子太长：30 词，上限 25 词", "和原句的句型结构偏离（相似度 0.00）"}, true, true},
	}
	for _, c := range cases {
		got := BuildChecks(c.in)
		issues := got.Issues
		if len(issues) == 0 {
			issues = nil
		}
		if !slices.Equal(issues, c.issues) {
			t.Errorf("%s: issues = %v want %v", c.name, got.Issues, c.issues)
		}
		if got.Warning != c.warn || got.Error != c.bad {
			t.Errorf("%s: warning=%v error=%v", c.name, got.Warning, got.Error)
		}
		if got.Issues == nil || got.MissingTargets == nil || got.OutOfScope == nil {
			t.Errorf("%s: slices must be non-nil for JSON: %+v", c.name, got)
		}
	}
	c := BuildChecks(CheckInput{Words: 30, MaxWords: 25, Similarity: f(0.25)})
	if !c.TooLong || !c.StructureDeviates || c.Similarity == nil || *c.Similarity != 0.25 || c.Words != 30 || c.MaxWords != 25 {
		t.Errorf("fields = %+v", c)
	}
}

func TestParsePastedOrigins(t *testing.T) {
	text := "I find making word cards useful. | 我发现做单词卡很有用。\n\n  It's fun to learn English.  \r\nShe is good at swimming｜她擅长游泳。\n | 只有中文\n"
	got := ParsePastedOrigins(text)
	want := []PastedOrigin{
		{En: "I find making word cards useful.", Cn: "我发现做单词卡很有用。"},
		{En: "It's fun to learn English."},
		{En: "She is good at swimming", Cn: "她擅长游泳。"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %+v", got)
	}
	if len(ParsePastedOrigins("  \n ")) != 0 {
		t.Fatal("blank")
	}
}

func TestNormalizeVariantMode(t *testing.T) {
	cases := map[string]string{
		"替换": ModeReplace, "replace": ModeReplace, " 转换 ": ModeTransform, "扩展": ModeExpand, "迁移": ModeTransfer,
		"transfer": ModeTransfer, "转换（肯定→疑问）": ModeTransform, "随便": "", "": "",
	}
	for in, want := range cases {
		if got := NormalizeVariantMode(in); got != want {
			t.Errorf("NormalizeVariantMode(%q) = %q want %q", in, got, want)
		}
	}
}
