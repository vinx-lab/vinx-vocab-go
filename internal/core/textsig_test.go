package core

import "testing"

func TestUnitTextSignature(t *testing.T) {
	base := UnitTextSignature("text", "Music", []string{"Music is fun.", "I like it."})
	cases := []struct {
		name  string
		kind  string
		title string
		ens   []string
		same  bool
	}{
		{"完全相同", "text", "Music", []string{"Music is fun.", "I like it."}, true},
		{"空白差异", "text", "  Music ", []string{"Music  is fun.", " I like it.\t"}, true},
		{"类型不同", "list", "Music", []string{"Music is fun.", "I like it."}, false},
		{"标题不同", "text", "Songs", []string{"Music is fun.", "I like it."}, false},
		{"句序不同", "text", "Music", []string{"I like it.", "Music is fun."}, false},
		{"少一句", "text", "Music", []string{"Music is fun."}, false},
		{"句子拼接不混淆", "text", "Music", []string{"Music is fun. I like it."}, false},
		{"大小写敏感", "text", "music", []string{"Music is fun.", "I like it."}, false},
	}
	for _, c := range cases {
		got := UnitTextSignature(c.kind, c.title, c.ens) == base
		if got != c.same {
			t.Errorf("%s: same = %v, want %v", c.name, got, c.same)
		}
	}
	// 默认标题相同、内容不同的两篇课文不相同
	if UnitTextSignature("text", "课文", []string{"A."}) == UnitTextSignature("text", "课文", []string{"B."}) {
		t.Error("默认标题相同、内容不同的课文不应判为相同")
	}
}
