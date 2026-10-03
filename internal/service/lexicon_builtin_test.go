package service_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vinx-lab/vinx-vocab-go/internal/seed"
	"github.com/vinx-lab/vinx-vocab-go/internal/service"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// 用内置词书演练句子关联（spec 0004「验证」）：课本风格的句子里，常见变形、带注释的拼写、短语都能关联上。
func TestBuiltinLexiconAssociation(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "vinx.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := seed.FirstRunBooks(ctx, db, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	lx, err := service.LoadLexicon(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	sentences := []string{
		"I went to school by bike yesterday.",
		"She is good at playing the guitar.",
		"The children were looking after their pets.",
		"My father bought me two books last week.",
		"We are interested in learning English.",
		"He took part in the race and won first prize.",
		"How do you learn English words?",
		"I find making word cards useful.",
		"I used to find English hard.",
		"Then I started to read aloud every day.",
		"Now I can understand more.",
	}
	matched, total := 0, 0
	var unknown []string
	for _, s := range sentences {
		a := lx.Analyze(s, nil)
		total += len(a.Tokens)
		covered := len(a.Tokens) - len(a.Unknown)
		matched += covered
		for _, u := range a.Unknown {
			unknown = append(unknown, u.Text)
		}
	}
	rate := float64(matched) / float64(total)
	t.Logf("关联率 %.1f%%（%d/%d），未关联：%s", rate*100, matched, total, strings.Join(unknown, ", "))
	if rate < 0.9 {
		t.Errorf("内置词库的句子关联率偏低：%.1f%%，未关联：%v", rate*100, unknown)
	}
	// 关键用例：went → go（中考词汇里拼写是 go (went, gone)）、be good at、look after
	a := lx.Analyze("She is good at it and went home to look after the children.", nil)
	forms := map[string]bool{}
	for _, w := range a.Words {
		forms[w.Form] = true
	}
	for _, f := range []string{"is good at", "went", "look after", "children"} {
		if !forms[f] {
			t.Errorf("没有关联上 %q：%+v", f, a.Words)
		}
	}
}
