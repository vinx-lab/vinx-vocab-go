package ai

import (
	"strings"
	"testing"
)

func TestParseAiListStandard(t *testing.T) {
	got, err := ParseAiList(`[{"a":1},{"a":2}]`)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || AsMap(got[0])["a"] != 1.0 || AsMap(got[1])["a"] != 2.0 {
		t.Fatalf("got %+v", got)
	}
}

func TestParseAiListMergesSplitFragments(t *testing.T) {
	raw := `[{"spelling":"a"}]` + "\n" + `[{"spelling":"b"},{"spelling":"c"}]`
	got, err := ParseAiList(raw)
	if err != nil {
		t.Fatal(err)
	}
	var sp []string
	for _, g := range got {
		sp = append(sp, AsString(AsMap(g)["spelling"]))
	}
	if strings.Join(sp, ",") != "a,b,c" {
		t.Fatalf("got %v", sp)
	}
}

func TestParseAiListStripsFencesAndThink(t *testing.T) {
	raw := "<think>先想一下</think>好的，结果如下：\n```json\n{\"items\":[{\"a\":1}]}\n```\n希望有帮助。"
	got, err := ParseAiList(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || AsMap(got[0])["a"] != 1.0 {
		t.Fatalf("got %+v", got)
	}
}

func TestParseAiObjectStringWithBrackets(t *testing.T) {
	got, err := ParseAiObject(`{"t":"a [bracket] and {brace} \" inside"}`)
	if err != nil {
		t.Fatal(err)
	}
	want := `a [bracket] and {brace} " inside`
	if got["t"] != want {
		t.Fatalf("got %q want %q", got["t"], want)
	}
}

func TestParseAiListSingleObjectAndFailure(t *testing.T) {
	got, err := ParseAiList(`{"a":1}`)
	if err != nil || len(got) != 1 || AsMap(got[0])["a"] != 1.0 {
		t.Fatalf("got %+v err %v", got, err)
	}
	if _, err := ParseAiList("抱歉，我无法完成"); err == nil || !strings.Contains(err.Error(), "无法解析") {
		t.Fatalf("expected parse error, got %v", err)
	}
	if v := ExtractJSONValues("没有 JSON"); len(v) != 0 {
		t.Fatalf("expected empty, got %v", v)
	}
}

// ---------------------------------------------------------------------------
// I1（评审）：解析容错要与 oracle 一致——字段类型不符不能连累整段/整项。
// 三个复现用例照抄评审报告，另加混合类型场景。
// ---------------------------------------------------------------------------

func TestParseAiObjectQuestionsWrongType(t *testing.T) {
	// 复现用例 1：{"title":"T","passage":"p","questions":"none"} —— oracle 能保存短文（questions 取 []）。
	got, err := ParseAiObject(`{"title":"T","passage":"p","questions":"none"}`)
	if err != nil {
		t.Fatal(err)
	}
	if AsString(got["title"]) != "T" || AsString(got["passage"]) != "p" {
		t.Fatalf("got %+v", got)
	}
	arr, isArray := got["questions"].([]any)
	if isArray {
		t.Fatalf("questions should not be an array here, got %v", arr)
	}
	// 调用方（GeneratePassage）据此把非数组的 questions 当作 []，不整段报错；这里只需确认解析没有丢字段/没有报错。
}

func TestParseAiObjectTitleNumber(t *testing.T) {
	// 复现用例 2：{"title":123,"passage":"p"} —— oracle 把标题转成 "123"。
	got, err := ParseAiObject(`{"title":123,"passage":"p"}`)
	if err != nil {
		t.Fatal(err)
	}
	if AsString(got["title"]) != "123" {
		t.Fatalf("title = %q, want \"123\"", AsString(got["title"]))
	}
	if AsString(got["passage"]) != "p" {
		t.Fatalf("passage = %q", AsString(got["passage"]))
	}
}

func TestParseAiListOneBadSpellingDoesNotDropOthers(t *testing.T) {
	// 复现用例 3：[{"spelling":"a","example":"x"},{"spelling":2,"example":"y"}] —— oracle 解析出 2 项
	// （不会因为第二项 spelling 是数字就把整个数组丢掉）。
	got, err := ParseAiList(`[{"spelling":"a","example":"x"},{"spelling":2,"example":"y"}]`)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 items, got %d: %+v", len(got), got)
	}
	m0, m1 := AsMap(got[0]), AsMap(got[1])
	if AsString(m0["spelling"]) != "a" || AsString(m0["example"]) != "x" {
		t.Fatalf("item0 = %+v", m0)
	}
	if AsString(m1["spelling"]) != "2" || AsString(m1["example"]) != "y" {
		t.Fatalf("item1 = %+v", m1)
	}
}

func TestParseAiListNonObjectItemsDoNotCrash(t *testing.T) {
	// 数组里混了非对象元素（字符串、数字、null）：与 JS 在非对象上访问未知属性得到 undefined 一致，
	// AsMap 对非 map 值返回 nil map，AsString(nil map 上取键) 返回 ""，不 panic。
	got, err := ParseAiList(`["not an object", 42, null, {"spelling":"ok","example":"e","exampleCn":"c"}]`)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("expected 4 items, got %d", len(got))
	}
	for i := 0; i < 3; i++ {
		m := AsMap(got[i])
		if m != nil {
			t.Fatalf("item %d expected nil map for non-object value, got %v", i, m)
		}
		if AsString(m["spelling"]) != "" {
			t.Fatalf("item %d expected empty spelling", i)
		}
	}
	last := AsMap(got[3])
	if AsString(last["spelling"]) != "ok" || AsString(last["example"]) != "e" || AsString(last["exampleCn"]) != "c" {
		t.Fatalf("last item = %+v", last)
	}
}

func TestParseAiObjectQuestionItemsMixedTypes(t *testing.T) {
	// questions 数组存在，但里面的 q/a 类型五花八门（数字、bool、缺失、null）：每项各自宽松转换，不报错。
	got, err := ParseAiObject(`{"passage":"p","questions":[{"q":1,"a":true},{"q":"字符串问题"},{},null]}`)
	if err != nil {
		t.Fatal(err)
	}
	arr, ok := got["questions"].([]any)
	if !ok || len(arr) != 4 {
		t.Fatalf("questions = %+v", got["questions"])
	}
	q0 := AsMap(arr[0])
	if AsString(q0["q"]) != "1" || AsString(q0["a"]) != "true" {
		t.Fatalf("q0 = %+v", q0)
	}
	q1 := AsMap(arr[1])
	if AsString(q1["q"]) != "字符串问题" || AsString(q1["a"]) != "" {
		t.Fatalf("q1 = %+v", q1)
	}
	q3 := AsMap(arr[3]) // null 元素
	if AsString(q3["q"]) != "" || AsString(q3["a"]) != "" {
		t.Fatalf("q3 (null) = %+v", q3)
	}
}

func TestAsString(t *testing.T) {
	cases := []struct {
		v    any
		want string
	}{
		{nil, ""},
		{"hello", "hello"},
		{float64(2), "2"},
		{float64(2.5), "2.5"},
		{float64(0), "0"},
		{float64(-3), "-3"},
		{true, "true"},
		{false, "false"},
		{map[string]any{"x": 1}, ""}, // 业务字段不会是对象；兜底空串
		{[]any{1, 2}, ""},
	}
	for _, c := range cases {
		if got := AsString(c.v); got != c.want {
			t.Errorf("AsString(%#v) = %q, want %q", c.v, got, c.want)
		}
	}
}

func TestAsMap(t *testing.T) {
	if m := AsMap(map[string]any{"a": 1}); m["a"] != 1 {
		t.Fatalf("got %v", m)
	}
	if m := AsMap("not a map"); m != nil {
		t.Fatalf("expected nil, got %v", m)
	}
	if m := AsMap(nil); m != nil {
		t.Fatalf("expected nil, got %v", m)
	}
}

func TestExampleUsesWordInflection(t *testing.T) {
	cases := []struct {
		example, spelling string
		want              bool
	}{
		{"I like reading about dinosaurs.", "dinosaur", true},
		{"They organized a trip.", "organize", true},
		{"She is smart.", "intelligent", false},
	}
	for _, c := range cases {
		if got := ExampleUsesWord(c.example, c.spelling); got != c.want {
			t.Errorf("ExampleUsesWord(%q,%q) = %v, want %v", c.example, c.spelling, got, c.want)
		}
	}
}

func TestExampleUsesWordPhrase(t *testing.T) {
	cases := []struct {
		example, spelling string
		want              bool
	}{
		{"She is good at singing.", "be good at", true},
		{"We take part in the game.", "take part in", true},
		{"She sings well.", "be good at", false},
	}
	for _, c := range cases {
		if got := ExampleUsesWord(c.example, c.spelling); got != c.want {
			t.Errorf("ExampleUsesWord(%q,%q) = %v, want %v", c.example, c.spelling, got, c.want)
		}
	}
}

func TestExampleUsesWordNoAffixFalsePositive(t *testing.T) {
	if ExampleUsesWord("Artistic people see it.", "art") {
		t.Fatal("should not match affix")
	}
}
