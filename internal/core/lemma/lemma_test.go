package lemma

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

const testIrregular = `# 注释
be	am, is, are, was, were, been, being
do	does, did, done, doing
go	goes, went, gone
child	children
good	better, best
lie	lay, lain, lying
make	made
`

func TestParseIrregular(t *testing.T) {
	irr := ParseIrregular(testIrregular + "\n坏行没有制表符\nGood\tBetter\n")
	if got := irr["went"]; !reflect.DeepEqual(got, []string{"go"}) {
		t.Errorf("went → %v", got)
	}
	if got := irr["better"]; !reflect.DeepEqual(got, []string{"good"}) {
		t.Errorf("better 去重、小写 → %v", got)
	}
	if got := irr["lay"]; !reflect.DeepEqual(got, []string{"lie"}) {
		t.Errorf("lay → %v", got)
	}
	if _, ok := irr["坏行没有制表符"]; ok {
		t.Error("无制表符的行应忽略")
	}
}

func TestTokenize(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"I don't like it.", []string{"I", "don't", "like", "it"}},
		{"It's six o'clock now!", []string{"It's", "six", "o'clock", "now"}},
		{"Tom’s T-shirt, please.", []string{"Tom’s", "T-shirt", "please"}},
		{"  \"Hello,\" she said -- twice. ", []string{"Hello", "she", "said", "twice"}},
		{"The students' books cost 25 yuan.", []string{"The", "students'", "books", "cost", "25", "yuan"}},
		{"Mr. Smith", []string{"Mr", "Smith"}},
		{"", nil},
	}
	for _, c := range cases {
		var got []string
		for i, tk := range Tokenize(c.in) {
			if tk.Index != i {
				t.Errorf("%q 第 %d 个 token 的 Index = %d", c.in, i, tk.Index)
			}
			if c.in[tk.Start:tk.End] != tk.Text {
				t.Errorf("%q 偏移不对：%q vs %q", c.in, c.in[tk.Start:tk.End], tk.Text)
			}
			got = append(got, tk.Text)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("Tokenize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if n := Tokenize("Don’t")[0].Norm; n != "don't" {
		t.Errorf("弯撇号应统一成直撇号并小写：%q", n)
	}
	if n := Tokenize("students'")[0].Norm; n != "students'" {
		t.Errorf("词尾撇号保留在 Norm 里：%q", n)
	}
}

func TestCandidates(t *testing.T) {
	irr := ParseIrregular(testIrregular)
	cases := []struct {
		in   string
		want string // 必须出现在候选里
	}{
		// 规则变化
		{"books", "book"},
		{"watches", "watch"},
		{"boxes", "box"},
		{"studies", "study"},
		{"played", "play"},
		{"liked", "like"},
		{"studied", "study"},
		{"stopped", "stop"},
		{"planned", "plan"},
		{"playing", "play"},
		{"making", "make"},
		{"running", "run"},
		{"dying", "die"},
		{"taller", "tall"},
		{"tallest", "tall"},
		{"larger", "large"},
		{"bigger", "big"},
		{"happier", "happy"},
		{"easiest", "easy"},
		// 所有格
		{"tom's", "tom"},
		{"students'", "students"},
		{"students'", "student"},
		// 不规则表
		{"went", "go"},
		{"is", "be"},
		{"did", "do"},
		{"children", "child"},
		{"better", "good"},
		{"lying", "lie"},
	}
	for _, c := range cases {
		got := Candidates(c.in, irr)
		if len(got) == 0 || got[0] != c.in {
			t.Errorf("Candidates(%q) 第一个应是原词：%v", c.in, got)
		}
		if !slices.Contains(got, c.want) {
			t.Errorf("Candidates(%q) = %v，缺少 %q", c.in, got, c.want)
		}
	}
	// 太短的词不做规则还原（is 不应还原成 i）
	if got := Candidates("is", nil); slices.Contains(got, "i") {
		t.Errorf("is 不应还原成 i：%v", got)
	}
	// 较长的原形优先：uses / used / using → use 排在 us 前面
	for _, w := range []string{"uses", "used", "using"} {
		got := Candidates(w, nil)
		if i, j := slices.Index(got, "use"), slices.Index(got, "us"); i < 0 || (j >= 0 && j < i) {
			t.Errorf("Candidates(%q) = %v：use 应先于 us", w, got)
		}
	}
	if got := Candidates("class", nil); slices.Contains(got, "clas") {
		t.Errorf("-ss 不去 s：%v", got)
	}
}

func TestKey(t *testing.T) {
	cases := map[string]string{
		"go (went, gone)":      "go",
		"begin(began,begun)":   "begin",
		"child (复children)":    "child",
		"centre (美 center )":   "centre",
		"Be good at":           "be good at",
		"fresh in one’s memory": "fresh in one's memory",
		"Mr.":                  "mr",
		"T-shirt":              "t-shirt",
		"  take  ...  for example ": "take ... for example",
	}
	for in, want := range cases {
		if got := Key(in); got != want {
			t.Errorf("Key(%q) = %q, want %q", in, got, want)
		}
	}
}

func newTestMatcher() *Matcher {
	entries := []Entry{
		{ID: "w-go", Spelling: "go (went, gone)"},
		{ID: "w-school", Spelling: "school"},
		{ID: "w-i", Spelling: "I"},
		{ID: "w-be", Spelling: "be"},
		{ID: "w-good", Spelling: "good"},
		{ID: "w-at", Spelling: "at"},
		{ID: "w-begood", Spelling: "be good at"},
		{ID: "w-english", Spelling: "English"},
		{ID: "w-help", Spelling: "help"},
		{ID: "w-helpsb", Spelling: "help sb. with sth."},
		{ID: "w-take", Spelling: "take"},
		{ID: "w-example", Spelling: "example"},
		{ID: "w-takefor", Spelling: "take ... for example"},
		{ID: "w-for", Spelling: "for"},
		{ID: "w-lookafter", Spelling: "look after"},
		{ID: "w-look", Spelling: "look"},
		{ID: "w-after", Spelling: "after"},
		{ID: "w-dont", Spelling: "don't"},
		{ID: "w-oclock", Spelling: "o'clock"},
		{ID: "w-child", Spelling: "child (复children)"},
		{ID: "w-the", Spelling: "the"},
		{ID: "w-my", Spelling: "my"},
		{ID: "w-mind", Spelling: "make up one's mind"},
		{ID: "w-make", Spelling: "make"},
		{ID: "w-up", Spelling: "up"},
		{ID: "w-mind1", Spelling: "mind"},
		{ID: "w-do", Spelling: "do"},
		{ID: "w-dohw", Spelling: "do homework"},
		{ID: "w-homework", Spelling: "homework"},
		// 同一个 key 两条：不带注释的精确拼写优先
		{ID: "w-go2", Spelling: "go"},
	}
	return NewMatcher(entries, ParseIrregular(testIrregular))
}

func matchSummary(ms []Match) string {
	var parts []string
	for _, m := range ms {
		parts = append(parts, m.WordID+"@"+itoa(m.Position)+":"+m.Form)
	}
	return strings.Join(parts, " ")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}

func TestMatch(t *testing.T) {
	m := newTestMatcher()
	cases := []struct {
		in   string
		want string
	}{
		// 单词 + 不规则变化 + 同 key 精确拼写优先
		{"I went to school.", "w-i@0:I w-go2@1:went w-school@3:school"},
		// 短语先长后短：be good at 覆盖 is / good / at；be 变形
		{"She is good at English.", "w-begood@1:is good at w-english@4:English"},
		{"They were good at it.", "w-begood@1:were good at"},
		// sb. / sth. 占位符匹配一个或几个词
		{"Please help my little brother with his homework.", "w-helpsb@1:help my little brother with his w-my@2:my w-homework@7:homework"},
		// ... 占位符；被占位的词照常单独匹配
		{"Take English for example.", "w-takefor@0:Take English for example w-english@1:English"},
		// 短语里的词也可以变形（looked after）
		{"She looked after the child.", "w-lookafter@1:looked after w-the@3:the w-child@4:child"},
		{"The children did homework.", "w-the@0:The w-child@1:children w-dohw@2:did homework"},
		// one's 占位
		{"I made up my mind.", "w-i@0:I w-mind@1:made up my mind w-my@3:my"},
		// 撇号
		{"Don't go at six o'clock!", "w-dont@0:Don't w-go2@1:go w-at@2:at w-oclock@4:o'clock"},
	}
	for _, c := range cases {
		got := matchSummary(m.Match(c.in))
		if got != c.want {
			t.Errorf("Match(%q)\n got  %s\n want %s", c.in, got, c.want)
		}
	}
}

func TestMatchPlaceholderNeedsWord(t *testing.T) {
	m := newTestMatcher()
	// take ... for example 的 ... 至少要一个词
	if got := matchSummary(m.Match("Take for example.")); strings.Contains(got, "w-takefor") {
		t.Errorf("占位符不能匹配零个词：%s", got)
	}
}

func TestAnalyze(t *testing.T) {
	m := newTestMatcher()
	a := m.Analyze("I went to Beijing in 2024.")
	var unknown []string
	for _, tk := range a.Unknown {
		unknown = append(unknown, tk.Text)
	}
	// to / in 不在测试词库里，Beijing 也不在；数字不算
	if !reflect.DeepEqual(unknown, []string{"to", "Beijing", "in"}) {
		t.Errorf("Unknown = %v", unknown)
	}
	if len(a.Tokens) != 6 || len(a.Matches) != 2 {
		t.Errorf("Analyze = %+v", a)
	}
	known := map[string]bool{"w-i": true}
	out := OutOfScope(a.Matches, known)
	if len(out) != 1 || out[0].WordID != "w-go2" {
		t.Errorf("OutOfScope = %+v", out)
	}
}
