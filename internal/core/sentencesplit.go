package core

import (
	"strings"
	"unicode"
)

// 整段文字拆成句子（spec 0004 §4：已有 AI 短文按句号、问号、叹号拆分，中英句数一致时逐句对应）。

// sentenceAbbrevs 句点后面不断句的常见缩写（小写，不含最后的点）。
var sentenceAbbrevs = map[string]bool{
	"mr": true, "mrs": true, "ms": true, "dr": true, "st": true, "prof": true, "no": true,
	"a.m": true, "p.m": true, "e.g": true, "i.e": true, "u.s": true, "u.k": true, "vs": true,
}

func isEnTerminator(r rune) bool { return r == '.' || r == '?' || r == '!' }
func isCnTerminator(r rune) bool { return r == '。' || r == '？' || r == '！' }
func isClosingQuote(r rune) bool {
	return r == '"' || r == '\'' || r == '”' || r == '’' || r == '」' || r == '』' || r == ')' || r == '）'
}

// SplitSentencesEn 英文按 . ? ! 断句：标点（连同紧跟的右引号、右括号）后面是空白且下一个词不是小写开头时断开；
// 省略号（两个以上的点）、数字里的点、Mr. / a.m. 这类缩写不断。
func SplitSentencesEn(text string) []string {
	return splitSentences(text, false)
}

// SplitSentencesCn 中文按 。？！ 断句（右引号后面紧跟正文时不断，如「“真的吗？”她问。」）；
// 夹在中文里的英文句子按英文规则断。
func SplitSentencesCn(text string) []string {
	return splitSentences(text, true)
}

func splitSentences(text string, cn bool) []string {
	runes := []rune(text)
	var out []string
	start := 0
	emit := func(end int) {
		s := strings.TrimSpace(string(runes[start:end]))
		if s != "" {
			out = append(out, s)
		}
		start = end
	}
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if cn && isCnTerminator(r) {
			j := i + 1
			for j < len(runes) && isCnTerminator(runes[j]) {
				j++
			}
			quoted := false
			for j < len(runes) && isClosingQuote(runes[j]) {
				j++
				quoted = true
			}
			if !quoted || j == len(runes) || unicode.IsSpace(runes[j]) || runes[j] == '“' || runes[j] == '"' {
				emit(j)
			}
			i = j - 1
			continue
		}
		if !isEnTerminator(r) {
			continue
		}
		j := i + 1
		for j < len(runes) && isEnTerminator(runes[j]) {
			j++
		}
		run := string(runes[i:j])
		for j < len(runes) && isClosingQuote(runes[j]) {
			j++
		}
		next := i
		i = j - 1
		// 省略号不断句
		if strings.Trim(run, ".") == "" && len(run) >= 2 {
			continue
		}
		if j < len(runes) && !unicode.IsSpace(runes[j]) {
			continue // 3.5、a.m 的中间点、紧跟中文等
		}
		if run == "." && isAbbrev(runes, next) {
			continue
		}
		// 下一个词小写开头：同一句（"Really?" she asked.）
		k := j
		for k < len(runes) && unicode.IsSpace(runes[k]) {
			k++
		}
		if k < len(runes) && unicode.IsLower(runes[k]) {
			continue
		}
		emit(j)
	}
	emit(len(runes))
	return out
}

// isAbbrev 第 dot 个字符的点前面那个词是不是常见缩写。
func isAbbrev(runes []rune, dot int) bool {
	k := dot
	for k > 0 && !unicode.IsSpace(runes[k-1]) && runes[k-1] != '"' && runes[k-1] != '“' {
		k--
	}
	return sentenceAbbrevs[strings.ToLower(string(runes[k:dot]))]
}

// PassageLine 拆好的一句。
type PassageLine struct {
	En        string
	Cn        string
	Paragraph int
}

// SplitPassage 英文正文与中文译文逐句对应：英文按换行分段、段内断句；中文整体断句。
// 两边句数一致（且非零）时返回逐句结果与 true；否则 false（保留整段，不拆）。
func SplitPassage(en, cn string) ([]PassageLine, bool) {
	var lines []PassageLine
	para := 0
	for _, p := range strings.Split(strings.ReplaceAll(en, "\r\n", "\n"), "\n") {
		ss := SplitSentencesEn(p)
		if len(ss) == 0 {
			continue
		}
		for _, s := range ss {
			lines = append(lines, PassageLine{En: s, Paragraph: para})
		}
		para++
	}
	var cnSents []string
	for _, p := range strings.Split(strings.ReplaceAll(cn, "\r\n", "\n"), "\n") {
		cnSents = append(cnSents, SplitSentencesCn(p)...)
	}
	if len(lines) == 0 || len(lines) != len(cnSents) {
		return nil, false
	}
	for i := range lines {
		lines[i].Cn = cnSents[i]
	}
	return lines, true
}
