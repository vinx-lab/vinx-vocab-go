package core

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// 出题（纯函数，对应旧 lib/questions.ts）：认义选择题的干扰项生成、题序打乱、例句挖空。
// 随机源可注入（Rng），测试用 SeededRng 得到确定结果。

// QuestionWord 出题需要的词字段。
type QuestionWord struct {
	ID           string
	Spelling     string
	Definition   string
	PartOfSpeech *string
}

// ClozeBlank 挖空占位符（前端按长度渲染下划线）。
const ClozeBlank = "____"

// SeededRng mulberry32 确定性随机（与旧 seededRng 逐位一致）。
func SeededRng(seed uint32) Rng {
	a := seed
	return func() float64 {
		a += 0x6d2b79f5
		t := a
		t = (t ^ (t >> 15)) * (t | 1)
		t ^= t + (t^(t>>7))*(t|61)
		return float64(t^(t>>14)) / 4294967296
	}
}

// Shuffle Fisher-Yates 洗牌（旧 shuffle），不修改入参；rng 为 nil 用默认随机源。
func Shuffle[T any](xs []T, rng Rng) []T {
	if rng == nil {
		rng = DefaultRng
	}
	out := append([]T(nil), xs...)
	for i := len(out) - 1; i > 0; i-- {
		j := int(rng() * float64(i+1))
		if j > i {
			j = i
		}
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func isASCIILetter(r rune) bool { return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') }

// foldEq 单个字符忽略大小写相等（正则 i 标志）。
func foldEq(a, b rune) bool { return a == b || strings.EqualFold(string(a), string(b)) }

// matchFold 在 s[i:] 处忽略大小写匹配 lit，返回匹配结束位置或 -1。
func matchFold(s string, i int, lit string) int {
	for _, lr := range lit {
		if i >= len(s) {
			return -1
		}
		r, n := utf8.DecodeRuneInString(s[i:])
		if !foldEq(r, lr) {
			return -1
		}
		i += n
	}
	return i
}

// matchTokens 目标词（按空白分成的若干段，段间 \s+）在 s[i:] 处的匹配结束位置或 -1。
func matchTokens(s string, i int, tokens []string) int {
	for k, tok := range tokens {
		if k > 0 {
			n := 0
			for i < len(s) {
				r, w := utf8.DecodeRuneInString(s[i:])
				if !isJSSpace(r) {
					break
				}
				i += w
				n++
			}
			if n == 0 {
				return -1
			}
		}
		if i = matchFold(s, i, tok); i < 0 {
			return -1
		}
	}
	return i
}

var clozeSuffixes = []string{"s", "es", "ed", "ing", "d", "'s", ""}

// BuildCloze 例句挖空：把例句中的目标词（忽略大小写，允许常见词形变化后缀）替换为空格占位符。
// 找不到目标词时返回 nil —— 该词不出题，避免出现“挖空后仍有答案”的题目。
//
// 等价于旧版正则 /(^|[^A-Za-z])(目标)(s|es|ed|ing|d|'s)?(?![A-Za-z])/gi 的全局替换（RE2 没有后行断言，手写匹配）。
func BuildCloze(example *string, spelling string) *string {
	if example == nil || *example == "" {
		return nil
	}
	target := jsTrimSpace(parenNoteRe.ReplaceAllString(strings.SplitN(spelling, "=", 2)[0], ""))
	if target == "" {
		return nil
	}
	tokens := strings.FieldsFunc(target, isJSSpace)
	s := *example
	var out strings.Builder
	hit := false
	last := 0 // 已输出到的位置
	p := 0
	for p <= len(s) {
		// 在位置 p 尝试匹配：先试 ^ 分支（仅 p == 0），再试「一个非字母字符 + 目标」
		type attempt struct{ preEnd int }
		tries := []attempt{}
		if p == 0 {
			tries = append(tries, attempt{preEnd: 0})
		}
		if p < len(s) {
			r, w := utf8.DecodeRuneInString(s[p:])
			if !isASCIILetter(r) {
				tries = append(tries, attempt{preEnd: p + w})
			}
		}
		matched := false
		for _, a := range tries {
			end := matchTokens(s, a.preEnd, tokens)
			if end < 0 {
				continue
			}
			for _, suf := range clozeSuffixes {
				e := end
				if suf != "" {
					if e = matchFold(s, end, suf); e < 0 {
						continue
					}
				}
				if e < len(s) {
					if r, _ := utf8.DecodeRuneInString(s[e:]); isASCIILetter(r) {
						continue
					}
				}
				// 命中：pre 原样保留，目标词换成占位符，后缀（'s 除外）保留
				out.WriteString(s[last:a.preEnd])
				out.WriteString(ClozeBlank)
				sufText := s[end:e]
				if sufText != "" && sufText != "'s" {
					out.WriteString(sufText)
				}
				last = e
				p = e
				hit = true
				matched = true
				break
			}
			if matched {
				break
			}
		}
		if matched {
			continue
		}
		if p >= len(s) {
			break
		}
		_, w := utf8.DecodeRuneInString(s[p:])
		p += w
	}
	if !hit {
		return nil
	}
	out.WriteString(s[last:])
	r := out.String()
	return &r
}

var (
	posVerbRe = regexp.MustCompile(`^(v|vt|vi)\.`)
	posAdjRe  = regexp.MustCompile(`^(adj|a)\.`)
	posAdvRe  = regexp.MustCompile(`^(adv|ad)\.`)
	posNounRe = regexp.MustCompile(`^n\.`)
)

// posClass 词性主类（n./v./adj. …）用于优先挑同类干扰项。
func posClass(pos *string) string {
	if pos == nil || *pos == "" {
		return "phrase"
	}
	p := strings.ToLower(*pos)
	switch {
	case posVerbRe.MatchString(p):
		return "v"
	case posAdjRe.MatchString(p):
		return "adj"
	case posAdvRe.MatchString(p):
		return "adv"
	case posNounRe.MatchString(p):
		return "n"
	}
	return "other"
}

// BuildChoiceOptions 为目标词生成 size 个（默认 4）中文释义选项（含正确项）。
// 干扰项优先同词性、释义不重复；候选不足时选项变少。
func BuildChoiceOptions(target QuestionWord, pool []QuestionWord, rng Rng, size int) []string {
	if size <= 0 {
		size = 4
	}
	correct := jsTrimSpace(target.Definition)
	used := map[string]bool{correct: true}
	cls := posClass(target.PartOfSpeech)
	filtered := make([]QuestionWord, 0, len(pool))
	for _, w := range pool {
		if w.ID != target.ID && strings.ToLower(w.Spelling) != strings.ToLower(target.Spelling) {
			filtered = append(filtered, w)
		}
	}
	candidates := Shuffle(filtered, rng)
	ordered := make([]QuestionWord, 0, len(candidates))
	for _, w := range candidates {
		if posClass(w.PartOfSpeech) == cls {
			ordered = append(ordered, w)
		}
	}
	for _, w := range candidates {
		if posClass(w.PartOfSpeech) != cls {
			ordered = append(ordered, w)
		}
	}
	distractors := []string{}
	for _, w := range ordered {
		if len(distractors) >= size-1 {
			break
		}
		d := jsTrimSpace(w.Definition)
		if d == "" || used[d] {
			continue
		}
		used[d] = true
		distractors = append(distractors, d)
	}
	return Shuffle(append([]string{correct}, distractors...), rng)
}
