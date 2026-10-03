// Package lemma 句子里的词与词库的自动关联（spec 0004 §5）：分词、词形还原、短语匹配。纯函数，不触库。
//
//   - 分词：按空格和标点切开，保留词内的撇号（don't、o'clock）与连字符（T-shirt），比较时一律小写；
//   - 词形还原：规则变化（-s/-es/-ies、-ed/-d/-ied、双写辅音、-ing、-er/-est）+ 内置不规则表；
//   - 短语：多词短语按词序匹配，短语里的词可以变形（is good at、looked after），
//     sb. / sth. / one's / ... 是占位符，匹配一个或几个词；
//   - 先长后短：短语优先，被短语覆盖的词不再单独匹配；占位符位置上的词照常单独匹配。
package lemma

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Irregular 不规则变化：变化形式（小写）→ 原形（小写，可能多个）。
type Irregular map[string][]string

// ParseIrregular 解析不规则表：每行「原形<TAB>变化形式（逗号分隔）」，# 开头为注释，其余行忽略。
func ParseIrregular(text string) Irregular {
	out := Irregular{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		base, forms, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		base = normWord(strings.TrimSpace(base))
		if base == "" {
			continue
		}
		for _, f := range strings.Split(forms, ",") {
			f = normWord(strings.TrimSpace(f))
			if f == "" || f == base {
				continue
			}
			if !contains(out[f], base) {
				out[f] = append(out[f], base)
			}
		}
	}
	return out
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// normWord 小写，弯撇号统一为直撇号。
func normWord(s string) string {
	return strings.ToLower(strings.NewReplacer("’", "'", "‘", "'").Replace(s))
}

// Token 句子里的一个词。
type Token struct {
	Text  string `json:"text"`  // 原文
	Norm  string `json:"-"`     // 小写、撇号统一
	Index int    `json:"index"` // 第几个词（从 0 开始）
	Start int    `json:"-"`     // 原文字节偏移
	End   int    `json:"-"`
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }
func isApos(r rune) bool     { return r == '\'' || r == '’' }

// Tokenize 分词：字母数字连成词；撇号、连字符夹在词中间时算词内（don't、T-shirt），
// 词尾跟在 s 后面的撇号也算（students'，复数所有格）。其余字符都是分隔符。
func Tokenize(s string) []Token {
	var out []Token
	start := -1
	runes := []rune{}
	offs := []int{}
	for i, r := range s {
		runes = append(runes, r)
		offs = append(offs, i)
	}
	offs = append(offs, len(s))
	flush := func(end int) {
		if start >= 0 {
			text := s[offs[start]:offs[end]]
			out = append(out, Token{Text: text, Norm: normWord(text), Index: len(out), Start: offs[start], End: offs[end]})
		}
		start = -1
	}
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case isWordRune(r):
			if start < 0 {
				start = i
			}
		case start >= 0 && (isApos(r) || r == '-') && i+1 < len(runes) && isWordRune(runes[i+1]):
			// 词内撇号 / 连字符
		case start >= 0 && isApos(r) && (runes[i-1] == 's' || runes[i-1] == 'S') && (i+1 == len(runes) || !isWordRune(runes[i+1])):
			// students'：复数所有格
			flush(i + 1)
		default:
			flush(i)
		}
	}
	flush(len(runes))
	return out
}

const vowels = "aeiou"

func isConsonant(b byte) bool { return b >= 'a' && b <= 'z' && !strings.ContainsRune(vowels, rune(b)) }

// undouble 去掉双写的词尾辅音（stopp → stop）；不是双写返回空串。
func undouble(stem string) string {
	n := len(stem)
	if n >= 3 && stem[n-1] == stem[n-2] && isConsonant(stem[n-1]) && !strings.ContainsRune("lsz", rune(stem[n-1])) {
		return stem[:n-1]
	}
	if n >= 3 && stem[n-1] == stem[n-2] && stem[n-1] == 'l' { // travelled → travel
		return stem[:n-1]
	}
	return ""
}

// Candidates 一个词（Norm）可能的原形，按优先级：原词 → 去所有格 → 不规则表 → 规则变化。
func Candidates(word string, irr Irregular) []string {
	out := []string{}
	add := func(s string) {
		if s != "" && !contains(out, s) {
			out = append(out, s)
		}
	}
	add(word)
	forms := []string{word}
	if strings.HasSuffix(word, "'s") && len(word) > 2 {
		forms = append(forms, word[:len(word)-2])
	} else if strings.HasSuffix(word, "'") && len(word) > 1 {
		forms = append(forms, word[:len(word)-1])
	}
	for _, f := range forms {
		add(f)
		for _, b := range irr[f] {
			add(b)
		}
		for _, r := range ruleStems(f) {
			add(r)
			for _, b := range irr[r] {
				add(b)
			}
		}
	}
	return out
}

// ruleStems 规则变化的可能原形（只对纯字母的词）。
func ruleStems(w string) []string {
	for i := 0; i < len(w); i++ {
		if w[i] < 'a' || w[i] > 'z' {
			return nil
		}
	}
	var out []string
	add := func(s string) {
		if len(s) >= 2 {
			out = append(out, s)
		}
	}
	n := len(w)
	has := func(suf string) bool { return strings.HasSuffix(w, suf) }
	// 顺序有讲究：较长的原形在前（uses → use 先于 us，using → use 先于 us）。
	// 名词复数 / 动词三单
	if n >= 4 && has("s") && !has("ss") && !has("us") && !has("is") {
		add(w[:n-1])
	}
	if n >= 5 && has("ies") {
		add(w[:n-3] + "y")
	}
	if n >= 4 && has("es") {
		add(w[:n-2])
	}
	// 过去式 / 过去分词
	if n >= 5 && has("ied") {
		add(w[:n-3] + "y")
	}
	if n >= 4 && has("ed") {
		stem := w[:n-2]
		add(w[:n-1]) // liked → like
		add(stem)
		add(undouble(stem))
	}
	// 现在分词
	if n >= 5 && has("ing") {
		stem := w[:n-3]
		add(stem + "e")
		add(stem)
		add(undouble(stem))
		if strings.HasSuffix(stem, "y") && len(stem) >= 2 { // dying → die
			add(stem[:len(stem)-1] + "ie")
		}
	}
	// 比较级 / 最高级
	if n >= 5 && has("iest") {
		add(w[:n-4] + "y")
	}
	if n >= 4 && has("ier") {
		add(w[:n-3] + "y")
	}
	if n >= 5 && has("est") {
		stem := w[:n-3]
		add(stem + "e")
		add(stem)
		add(undouble(stem))
	}
	if n >= 4 && has("er") {
		stem := w[:n-2]
		add(stem + "e")
		add(stem)
		add(undouble(stem))
	}
	return out
}

var parenRe = regexp.MustCompile(`\([^)]*\)|（[^）]*）`)

// Ellipsis 短语里的占位符「...」在 Key 里的写法。
const Ellipsis = "..."

// Key 词库拼写的比较键：去掉括号注释（go (went, gone) → go）、小写、撇号统一，
// 按分词结果用单个空格连接；「...」/「…」保留为占位符。
func Key(spelling string) string {
	s := parenRe.ReplaceAllString(spelling, " ")
	s = strings.ReplaceAll(s, "…", " ... ")
	s = strings.ReplaceAll(s, "...", " ... ")
	var parts []string
	for _, chunk := range strings.Fields(s) {
		if strings.Trim(chunk, ".") == "" && strings.Count(chunk, ".") >= 3 {
			parts = append(parts, Ellipsis)
			continue
		}
		for _, tk := range Tokenize(chunk) {
			parts = append(parts, tk.Norm)
		}
	}
	return strings.Join(parts, " ")
}

// Entry 词库里的一条（Word 的 id 与拼写）。
type Entry struct {
	ID       string
	Spelling string
}

// placeholder 短语里的占位词（Key 之后的写法）。
var placeholders = map[string]bool{
	Ellipsis: true, "sb": true, "sth": true, "sb's": true, "one's": true, "somebody": true, "something": true,
}

const maxPlaceholderWords = 5

type phrase struct {
	id       string
	pattern  []string // 字面词或占位符
	literals int
}

type candidate struct {
	id    string
	exact bool
	sp    string
}

// Matcher 用一份词库匹配句子。
type Matcher struct {
	irr     Irregular
	words   map[string]candidate
	phrases map[string][]*phrase // 第一个字面词 → 短语（字面词多的在前）
	lead    []*phrase            // 以占位符开头的短语
}

// better 同一个键有多条词条时：拼写本身就是键（不带注释）的优先，其次拼写字典序小的。结果确定。
func better(a, b candidate) bool {
	if a.exact != b.exact {
		return a.exact
	}
	return a.sp < b.sp
}

// NewMatcher 建立匹配器。entries 为全部词库；irr 为不规则表（可为 nil）。
func NewMatcher(entries []Entry, irr Irregular) *Matcher {
	m := &Matcher{irr: irr, words: map[string]candidate{}, phrases: map[string][]*phrase{}}
	phraseBest := map[string]candidate{}
	for _, e := range entries {
		key := Key(e.Spelling)
		if key == "" {
			continue
		}
		c := candidate{id: e.ID, exact: key == normWord(strings.Join(strings.Fields(e.Spelling), " ")), sp: e.Spelling}
		if !strings.Contains(key, " ") {
			if old, ok := m.words[key]; !ok || better(c, old) {
				m.words[key] = c
			}
			continue
		}
		if old, ok := phraseBest[key]; !ok || better(c, old) {
			phraseBest[key] = c
		}
	}
	for key, c := range phraseBest {
		p := &phrase{id: c.id, pattern: strings.Split(key, " ")}
		for _, w := range p.pattern {
			if !placeholders[w] {
				p.literals++
			}
		}
		if p.literals == 0 {
			continue
		}
		if placeholders[p.pattern[0]] {
			m.lead = append(m.lead, p)
		} else {
			m.phrases[p.pattern[0]] = append(m.phrases[p.pattern[0]], p)
		}
	}
	for k := range m.phrases {
		sortPhrases(m.phrases[k])
	}
	sortPhrases(m.lead)
	return m
}

func sortPhrases(ps []*phrase) {
	sort.Slice(ps, func(i, j int) bool {
		if ps[i].literals != ps[j].literals {
			return ps[i].literals > ps[j].literals
		}
		if len(ps[i].pattern) != len(ps[j].pattern) {
			return len(ps[i].pattern) > len(ps[j].pattern)
		}
		return strings.Join(ps[i].pattern, " ") < strings.Join(ps[j].pattern, " ")
	})
}

// Match 句中关联到的一个词条。
type Match struct {
	WordID   string `json:"wordId"`
	Position int    `json:"position"` // 第几个词（短语取起始位置）
	Form     string `json:"form"`     // 句中的实际写法
	// Covered 短语里字面词所在的位置（占位符位置不算）；单词就是 [Position]。
	Covered []int `json:"-"`
}

// Analysis 一句话的分析结果。
type Analysis struct {
	Tokens  []Token
	Matches []Match
	Unknown []Token // 没有关联到任何词条、也不在短语里的词（数字除外）
}

func (m *Matcher) literalMatches(tok Token, lit string, cands []string) bool {
	if tok.Norm == lit {
		return true
	}
	return contains(cands, lit)
}

// tryPhrase 从第 i 个词开始匹配短语；成功返回字面词位置与最后一个词的下标。
func (m *Matcher) tryPhrase(p *phrase, toks []Token, cands [][]string, i int) ([]int, int, bool) {
	var covered []int
	var rec func(pi, ti int) (int, bool)
	rec = func(pi, ti int) (int, bool) {
		if pi == len(p.pattern) {
			return ti - 1, true
		}
		w := p.pattern[pi]
		if placeholders[w] {
			for n := 1; n <= maxPlaceholderWords && ti+n <= len(toks); n++ {
				if end, ok := rec(pi+1, ti+n); ok {
					return end, true
				}
				if pi+1 == len(p.pattern) { // 结尾的占位符只取一个词
					break
				}
			}
			return 0, false
		}
		if ti >= len(toks) || !m.literalMatches(toks[ti], w, cands[ti]) {
			return 0, false
		}
		covered = append(covered, ti)
		end, ok := rec(pi+1, ti+1)
		if !ok {
			covered = covered[:len(covered)-1]
		}
		return end, ok
	}
	end, ok := rec(0, i)
	return covered, end, ok
}

// lookupWord 一个词按候选原形依次查词库。
func (m *Matcher) lookupWord(cands []string) (string, bool) {
	for _, c := range cands {
		if e, ok := m.words[c]; ok {
			return e.id, true
		}
	}
	return "", false
}

// Analyze 分词并匹配：先短语（字面词多的优先、靠前的优先），再单词。
func (m *Matcher) Analyze(sentence string) Analysis {
	toks := Tokenize(sentence)
	cands := make([][]string, len(toks))
	for i, t := range toks {
		cands[i] = Candidates(t.Norm, m.irr)
	}
	type hit struct {
		p       *phrase
		start   int
		end     int
		covered []int
	}
	var hits []hit
	for i := range toks {
		seen := map[*phrase]bool{}
		try := func(ps []*phrase) {
			for _, p := range ps {
				if seen[p] {
					continue
				}
				seen[p] = true
				if cov, end, ok := m.tryPhrase(p, toks, cands, i); ok {
					hits = append(hits, hit{p: p, start: i, end: end, covered: append([]int(nil), cov...)})
				}
			}
		}
		for _, c := range cands[i] {
			try(m.phrases[c])
		}
		try(m.lead)
	}
	sort.SliceStable(hits, func(a, b int) bool {
		if hits[a].p.literals != hits[b].p.literals {
			return hits[a].p.literals > hits[b].p.literals
		}
		return hits[a].start < hits[b].start
	})
	covered := make([]bool, len(toks))
	var matches []Match
	for _, h := range hits {
		free := true
		for _, c := range h.covered {
			if covered[c] {
				free = false
				break
			}
		}
		if !free {
			continue
		}
		for _, c := range h.covered {
			covered[c] = true
		}
		matches = append(matches, Match{WordID: h.p.id, Position: h.start, Form: sentence[toks[h.start].Start:toks[h.end].End], Covered: h.covered})
	}
	var unknown []Token
	for i, t := range toks {
		if covered[i] {
			continue
		}
		if id, ok := m.lookupWord(cands[i]); ok {
			matches = append(matches, Match{WordID: id, Position: i, Form: t.Text, Covered: []int{i}})
			continue
		}
		if hasLetter(t.Text) {
			unknown = append(unknown, t)
		}
	}
	sort.SliceStable(matches, func(a, b int) bool { return matches[a].Position < matches[b].Position })
	return Analysis{Tokens: toks, Matches: matches, Unknown: unknown}
}

// Match 只返回关联结果。
func (m *Matcher) Match(sentence string) []Match { return m.Analyze(sentence).Matches }

func hasLetter(s string) bool {
	for len(s) > 0 {
		r, n := utf8.DecodeRuneInString(s)
		if unicode.IsLetter(r) {
			return true
		}
		s = s[n:]
	}
	return false
}

// OutOfScope 超纲词：关联到的词条里不在已知词集合中的（spec 0004 §6）。同一词条只列一次。
func OutOfScope(matches []Match, known map[string]bool) []Match {
	out := []Match{}
	seen := map[string]bool{}
	for _, mt := range matches {
		if known[mt.WordID] || seen[mt.WordID] {
			continue
		}
		seen[mt.WordID] = true
		out = append(out, mt)
	}
	return out
}
