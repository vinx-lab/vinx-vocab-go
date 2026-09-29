package audioplan

import (
	"reflect"
	"regexp"
	"testing"
)

func w(id, spelling string, audioFile ...string) Word {
	af := ""
	if len(audioFile) > 0 {
		af = audioFile[0]
	}
	return Word{ID: id, Spelling: spelling, AudioFile: af}
}

func TestTargetStripsParensAndAlias(t *testing.T) {
	cases := map[string]string{
		"Mr (Mister)":     "Mr",
		"e-mail=email":    "e-mail",
		"look  after（照顾）": "look after",
		"(the)":           "",
	}
	for in, want := range cases {
		if got := Target(in); got != want {
			t.Errorf("Target(%q) = %q want %q", in, got, want)
		}
	}
}

func TestTargetStripsResidualPhoneticsAndBrackets(t *testing.T) {
	cases := map[string]string{
		"flowerbed //ˈflaʊəbed//":           "flowerbed",
		"footballer //ˈfʊtbɔːlə(r)//":       "footballer",
		"yo-yo //ˈjəʊ jəʊ//":                "yo-yo",
		"between [bɪˈtwiːn]":                "between",
		"go by （":                           "go by",
		"pocket money（":                     "pocket money",
		"burn (-ed, -ed":                    "burn",
		"make a point of doing something (": "make a point of doing something",
		"café":                              "café",
	}
	for in, want := range cases {
		if got := Target(in); got != want {
			t.Errorf("Target(%q) = %q want %q", in, got, want)
		}
	}
}

func TestFileNameCaseInsensitive(t *testing.T) {
	if FileName("China") != FileName("china") {
		t.Fatal("case should not matter")
	}
	re := regexp.MustCompile(`^[0-9a-f]{40}\.mp3$`)
	if !re.MatchString(FileName("china")) {
		t.Fatalf("unexpected filename: %s", FileName("china"))
	}
}

func TestSharedGrouping(t *testing.T) {
	plan := PlanAudioFetch([]Word{w("1", "May"), w("2", "may"), w("3", "may (情态)"), w("4", "cat")}, nil)
	if plan.Total != 4 {
		t.Fatalf("total = %d", plan.Total)
	}
	if len(plan.Todo) != 2 {
		t.Fatalf("todo = %+v", plan.Todo)
	}
	var may *Item
	for i := range plan.Todo {
		if plan.Todo[i].File == FileName("may") {
			may = &plan.Todo[i]
		}
	}
	if may == nil {
		t.Fatal("missing may group")
	}
	if may.Text != "May" {
		t.Fatalf("text = %q", may.Text)
	}
	if !reflect.DeepEqual(may.WordIDs, []string{"1", "2", "3"}) {
		t.Fatalf("wordIds = %v", may.WordIDs)
	}
	if !reflect.DeepEqual(plan.Shared, []Item{*may}) {
		t.Fatalf("shared = %v", plan.Shared)
	}
}

func TestCachedAndUnlinked(t *testing.T) {
	cat := FileName("cat")
	plan := PlanAudioFetch([]Word{w("1", "cat", cat), w("2", "Cat"), w("3", "dog")}, map[string]bool{cat: true})
	var texts []string
	for _, it := range plan.Todo {
		texts = append(texts, it.Text)
	}
	if !reflect.DeepEqual(texts, []string{"dog"}) {
		t.Fatalf("todo texts = %v", texts)
	}
	if plan.CachedFiles != 1 || plan.CachedWords != 2 {
		t.Fatalf("cachedFiles=%d cachedWords=%d", plan.CachedFiles, plan.CachedWords)
	}
	want := []Item{{Text: "Cat", File: cat, WordIDs: []string{"2"}}}
	if !reflect.DeepEqual(plan.Unlinked, want) {
		t.Fatalf("unlinked = %+v want %+v", plan.Unlinked, want)
	}
}

func TestEmptyTargetsNotFetched(t *testing.T) {
	plan := PlanAudioFetch([]Word{w("1", "(the)"), w("2", "the")}, nil)
	if !reflect.DeepEqual(plan.Empty, []string{"1"}) {
		t.Fatalf("empty = %v", plan.Empty)
	}
	if len(plan.Todo) != 1 || !reflect.DeepEqual(plan.Todo[0].WordIDs, []string{"2"}) {
		t.Fatalf("todo = %+v", plan.Todo)
	}
}

func TestSuspectResidual(t *testing.T) {
	plan := PlanAudioFetch([]Word{
		w("1", "and/or"), w("2", "a ) b"), w("3", "flowerbed //ˈflaʊəbed//"), w("4", "go by （"), w("5", "café"),
	}, nil)
	var suspect, todo []string
	for _, it := range plan.Suspect {
		suspect = append(suspect, it.Text)
	}
	for _, it := range plan.Todo {
		todo = append(todo, it.Text)
	}
	sortStrings(suspect)
	sortStrings(todo)
	if !reflect.DeepEqual(suspect, []string{"a ) b", "and/or"}) {
		t.Fatalf("suspect = %v", suspect)
	}
	if !reflect.DeepEqual(todo, []string{"café", "flowerbed", "go by"}) {
		t.Fatalf("todo = %v", todo)
	}
}

func TestOrderIsStableAndSortedByFile(t *testing.T) {
	words := []Word{w("1", "apple"), w("2", "zoo"), w("3", "Apple"), w("4", "book"), w("5", "cat")}
	a := PlanAudioFetch(words, nil)
	reversed := make([]Word, len(words))
	for i, wd := range words {
		reversed[len(words)-1-i] = wd
	}
	b := PlanAudioFetch(reversed, nil)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("plan should be independent of input order")
	}
	files := make([]string, len(a.Todo))
	for i, it := range a.Todo {
		files[i] = it.File
	}
	sorted := append([]string{}, files...)
	sortStrings(sorted)
	if !reflect.DeepEqual(files, sorted) {
		t.Fatalf("todo should be sorted by file: %v", files)
	}
	for _, it := range a.Todo {
		if it.File == FileName("apple") && it.Text != "Apple" {
			t.Fatalf("expected 'Apple' as canonical text, got %q", it.Text)
		}
	}
}

// TestEncodeURIComponent 与 node -e 'encodeURIComponent(...)' 逐条核对过的样例（评审 I2）。
func TestEncodeURIComponent(t *testing.T) {
	cases := map[string]string{
		"look after it's":       "look%20after%20it's",
		"50%":                   "50%25",
		"café":                  "caf%C3%A9",
		"(experiment)":          "(experiment)",
		"a & b":                 "a%20%26%20b",
		"don't":                 "don't",
		"a b~c_d-e.f!g*h(i)":    "a%20b~c_d-e.f!g*h(i)",
		"":                      "",
		"simple":                "simple",
		"多个 空格   在一起":           "%E5%A4%9A%E4%B8%AA%20%E7%A9%BA%E6%A0%BC%20%20%20%E5%9C%A8%E4%B8%80%E8%B5%B7",
		"be good at":            "be%20good%20at",
		"look after somebody's": "look%20after%20somebody's",
		"100% sure?":            "100%25%20sure%3F",
	}
	for in, want := range cases {
		if got := EncodeURIComponent(in); got != want {
			t.Errorf("EncodeURIComponent(%q) = %q, want %q", in, got, want)
		}
	}
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
