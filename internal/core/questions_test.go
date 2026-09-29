package core

import (
	"sort"
	"testing"
)

// 逐条翻译旧 apps/api/tests/lib/questions.test.ts。

func qw(id, def string, pos ...string) QuestionWord {
	p := "n."
	if len(pos) > 0 {
		p = pos[0]
	}
	return QuestionWord{ID: id, Spelling: id, Definition: def, PartOfSpeech: &p}
}

func TestChoiceOptions(t *testing.T) {
	target := qw("dog", "狗")
	pool := []QuestionWord{target, qw("cat", "猫"), qw("run", "跑", "v."), qw("pig", "猪"), qw("cow", "牛"), qw("big", "大的", "adj.")}

	// 4 个选项、含正确答案、无重复
	opts := BuildChoiceOptions(target, pool, SeededRng(1), 4)
	set := map[string]bool{}
	for _, o := range opts {
		set[o] = true
	}
	if len(opts) != 4 || !set["狗"] || len(set) != 4 {
		t.Fatalf("opts = %v", opts)
	}

	// 优先同词性干扰项（任何随机源下都成立）
	for seed := uint32(0); seed < 50; seed++ {
		opts = BuildChoiceOptions(target, pool, SeededRng(seed), 4)
		n := 0
		for _, o := range opts {
			if o == "猫" || o == "猪" || o == "牛" {
				n++
			}
		}
		if n != 3 {
			t.Fatalf("seed %d: %v", seed, opts)
		}
	}

	// 候选不足时选项变少但不重复释义
	opts = BuildChoiceOptions(target, []QuestionWord{target, qw("cat", "猫"), qw("kitty", "猫")}, SeededRng(3), 4)
	sort.Strings(opts)
	if len(opts) != 2 || opts[0] != "狗" && opts[1] != "狗" {
		t.Fatalf("opts = %v", opts)
	}

	// 拼写相同（忽略大小写）的词不作干扰项
	opts = BuildChoiceOptions(target, []QuestionWord{target, {ID: "x", Spelling: "DOG", Definition: "狗狗"}}, SeededRng(4), 4)
	if len(opts) != 1 {
		t.Fatalf("opts = %v", opts)
	}
}

func TestShuffleKeepsElements(t *testing.T) {
	got := Shuffle([]int{1, 2, 3, 4}, SeededRng(9))
	sort.Ints(got)
	if len(got) != 4 || got[0] != 1 || got[3] != 4 {
		t.Fatalf("got %v", got)
	}
}

func TestSeededRngMatchesMulberry32(t *testing.T) {
	// 旧 seededRng(1) 的前三个值（node 实测）
	r := SeededRng(1)
	want := []float64{0.6270739405881613, 0.002735721180215478, 0.5274470399599522}
	for i, w := range want {
		if got := r(); got != w {
			t.Fatalf("第 %d 个 = %v want %v", i, got, w)
		}
	}
}

func TestBuildCloze(t *testing.T) {
	s := func(v string) *string { return &v }
	cases := []struct {
		example  *string
		spelling string
		want     *string
	}{
		// 挖掉目标词，保留词形变化后缀
		{s("The dinosaur is very big."), "dinosaur", s("The ____ is very big.")},
		{s("He organized the books."), "organize", s("He ____d the books.")},
		{s("She organizes her notes daily."), "organize", s("She ____s her notes daily.")},
		// 忽略大小写并只挖独立单词
		{s("Art is everywhere. Artistic people see it."), "art", s("____ is everywhere. Artistic people see it.")},
		{s("Order matters."), "order", s("____ matters.")},
		// 短语按整体匹配
		{s("We should be good at English."), "be good at", s("We should ____ English.")},
		// 没有例句或例句不含目标词 → null（不出题）
		{nil, "dinosaur", nil},
		{s("Nothing here."), "dinosaur", nil},
		{s("The dog runs."), "a (an)", nil},
		// 额外（期望值由旧正则在 node 下实测）：多处命中、所有格、es 后缀、短语间多空白、大写后缀
		{s("Dogs and dog's toys: a dog."), "dog", s("____s and ____ toys: a ____.")},
		{s("Two boxes."), "box", s("Two ____es.")},
		{s("be   good at it"), "be good at", s("____ it")},
		{s(""), "dog", nil},
		{s("dogdog dog-dog"), "dog", s("dogdog ____-____")},
		{s("Dogged dogs"), "dog", s("Dogged ____s")},
		{s("I SEEING it"), "see", s("I ____ING it")},
	}
	for _, c := range cases {
		got := BuildCloze(c.example, c.spelling)
		if (got == nil) != (c.want == nil) || (got != nil && *got != *c.want) {
			g, w := "<nil>", "<nil>"
			if got != nil {
				g = *got
			}
			if c.want != nil {
				w = *c.want
			}
			t.Errorf("BuildCloze(%v, %q) = %q want %q", c.example, c.spelling, g, w)
		}
	}
}
