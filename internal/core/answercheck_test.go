package core

import "testing"

// 逐条翻译旧 apps/api/tests/lib/answer-check.test.ts。

func TestSpellingJudge(t *testing.T) {
	cases := []struct {
		answer, target string
		want           bool
	}{
		// 忽略大小写、空白、弯撇号
		{" Dinosaur ", "dinosaur", true},
		{"begoodat", "be good at", true},
		{"take somebody's place", "take somebody’s place", true},
		// 括号注释与等号别名
		{"a", "a (an)", true},
		{"begin", "begin(began,begun)", true},
		{"bicycle", "bike = bicycle", true},
		{"bike", "bike = bicycle", true},
		// 空答案与错拼判错
		{"", "dinosaur", false},
		{"dinosour", "dinosaur", false},
		// 省略号与全角括号
		{"so...that", "so…that", true},
		{"look", "look（看）", true},
	}
	for _, c := range cases {
		if got := IsSpellingCorrect(c.answer, c.target); got != c.want {
			t.Errorf("IsSpellingCorrect(%q, %q) = %v", c.answer, c.target, got)
		}
	}
}

func TestSpellingTarget(t *testing.T) {
	for in, want := range map[string]string{"begin(began,begun)": "begin", "bike = bicycle": "bike", "be  good\tat": "be good at"} {
		if got := SpellingTarget(in); got != want {
			t.Errorf("SpellingTarget(%q) = %q", in, got)
		}
	}
}

func TestChoiceJudge(t *testing.T) {
	if !IsChoiceCorrect("恐龙", "恐龙 ") || IsChoiceCorrect("恐", "恐龙") {
		t.Fatal("选择题严格匹配")
	}
}

func TestJudgeAnswer(t *testing.T) {
	def, sp := "恐龙", "dinosaur"
	// 正常作答按题型判定，挖空按拼写口径
	if !JudgeAnswer("recognition", "恐龙", false, def, sp) || !JudgeAnswer("spelling", "Dinosaur", false, def, sp) ||
		!JudgeAnswer("cloze", "dinosaur", false, def, sp) || JudgeAnswer("cloze", "dino", false, def, sp) {
		t.Fatal("正常作答判定不对")
	}
	// 点「不会」一律判错，不看填写内容
	for _, mode := range []string{"recognition", "spelling", "cloze"} {
		if JudgeAnswer(mode, "", true, def, sp) {
			t.Errorf("%s 不会应判错", mode)
		}
	}
	if JudgeAnswer("recognition", "恐龙", true, def, sp) || JudgeAnswer("spelling", "dinosaur", true, def, sp) {
		t.Fatal("不会带正确内容仍判错")
	}
}
