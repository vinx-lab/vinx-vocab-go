package vocabparser

import "testing"

func TestCleanSpelling(t *testing.T) {
	cases := []struct{ in, spelling, phonetic, note string }{
		// 双斜杠音标、方括号音标
		{"gladness //ˈɡlædnəs//", "gladness", "ˈɡlædnəs", ""},
		{"bus-stop //ˈbʌs stɒp//", "bus-stop", "ˈbʌs stɒp", ""},
		{"between [bɪˈtwiːn]", "between", "bɪˈtwiːn", ""},
		// 末尾括号：半角、全角、无空格
		{"fly (flew, flown)", "fly", "", "flew, flown"},
		{"a (an)", "a", "", "an"},
		{"begin(began,begun)", "begin", "", "began,begun"},
		{"best（good, well的最高级）", "best", "", "good, well的最高级"},
		{"wolf（复wolves）", "wolf", "", "复wolves"},
		{"metre (美meter)", "metre", "", "美meter"},
		{"centre (美 center )", "centre", "", "美 center"},
		// 紧贴的可省字母：注释写成完整拼写
		{"toward(s)", "toward", "", "towards"},
		{"Olympic(s)", "Olympic", "", "Olympics"},
		// 缺右括号（源词表断行）
		{"burn (-ed, -ed", "burn", "", "-ed, -ed"},
		// 等号
		{"bike = bicycle", "bike", "", "= bicycle"},
		{"fridge =refrigerator", "fridge", "", "= refrigerator"},
		{"Mom =Mum", "Mom", "", "= Mum"},
		// 组合：先等号再括号
		{"No.(缩) = number", "No.", "", "缩；= number"},
		{"P.C.(缩) =personal computer", "P.C.", "", "缩；= personal computer"},
		// 短语不动
		{"look after (sb.)", "look after (sb.)", "", ""},
		{"lead (somebody) to", "lead (somebody) to", "", ""},
		{"advice column （", "advice column （", "", ""},
		{"make a point of doing something (", "make a point of doing something (", "", ""},
		{"ice-cream = ice cream", "ice-cream", "", "= ice cream"},
		// 中间的括号不动
		{"practice(s)e", "practice(s)e", "", ""},
		// 只剩符号时原样返回
		{"(an)", "(an)", "", ""},
		{"123 (x)", "123 (x)", "", ""},
		{"= bicycle", "= bicycle", "", ""},
		{"bike =", "bike =", "", ""},
		{"//ə//", "//ə//", "", ""},
		// 干净的拼写原样返回
		{"fly", "fly", "", ""},
		{"o'clock", "o'clock", "", ""},
		{"T-shirt", "T-shirt", "", ""},
		{"be good at", "be good at", "", ""},
	}
	for _, c := range cases {
		sp, ph, note := CleanSpelling(c.in)
		if sp != c.spelling || ph != c.phonetic || note != c.note {
			t.Errorf("CleanSpelling(%q) = (%q, %q, %q), want (%q, %q, %q)", c.in, sp, ph, note, c.spelling, c.phonetic, c.note)
		}
		// 不动点：再规整一次不变（启动修复的幂等依赖这一点）
		if sp2, ph2, note2 := CleanSpelling(sp); sp2 != sp || ph2 != "" || note2 != "" {
			t.Errorf("CleanSpelling(%q) 不是不动点：(%q, %q, %q)", sp, sp2, ph2, note2)
		}
	}
}

func TestParseLineCleansSpelling(t *testing.T) {
	cases := []struct{ in, spelling, phonetic, pos, def, typ string }{
		{"fly (flew, flown) /flaɪ/ vi. 飞；飞行\n", "fly", "flaɪ", "vi.", "飞；飞行（flew, flown）", "word"},
		{"begin(began,begun) /bɪˈɡɪn/ v. 开始,着手\n", "begin", "bɪˈɡɪn", "v.", "开始,着手（began,begun）", "word"},
		{"best（good, well的最高级） /best/ a. & ad. 最好的\n", "best", "best", "a.&ad.", "最好的（good, well的最高级）", "word"},
		// 双斜杠音标：解析器原先把整段当拼写，现在拆出音标
		{"gladness //ˈɡlædnəs// n. 高兴\n", "gladness", "ˈɡlædnəs", "n.", "高兴", "word"},
		{"between [bɪˈtwiːn] prep. 在（两者）之间\n", "between", "bɪˈtwiːn", "prep.", "在（两者）之间", "word"},
		// 已有音标时不覆盖
		{"bike = bicycle /baɪk/ n. 自行车\n", "bike", "baɪk", "n.", "自行车（= bicycle）", "word"},
		{"mathematics = math, maths /mæθəˈmætɪks/ n. 数学\n", "mathematics", "mæθəˈmætɪks", "n.", "数学（= math, maths）", "word"},
		// 短语里的括号不动
		{"lead (somebody) to 带着（某人）到\n", "lead (somebody) to", "", "", "带着（某人）到", "phrase"},
	}
	for _, c := range cases {
		e := first(t, c.in)
		if e.Spelling != c.spelling || e.Phonetic != c.phonetic || e.PartOfSpeech != c.pos || e.Definition != c.def || e.Type != c.typ {
			t.Errorf("%q => %+v", c.in, e)
		}
		if e.Status == StatusError {
			t.Errorf("%q 解析为 error：%v", c.in, e.Issues)
		}
	}
	// 缺释义的行不因注释的「（…）」而通过检查
	if e := first(t, "fly (flew, flown) /flaɪ/ v.\n"); e.Status != StatusError {
		t.Errorf("缺释义应为 error：%+v", e)
	}
}
