package core

import (
	"regexp"
	"strings"
)

// 作答判定（纯函数，对应旧 lib/answer-check.ts）。

// isJSSpace JS 正则 \s 与 String.prototype.trim 的空白集合。
func isJSSpace(r rune) bool {
	switch {
	case r == '\t', r == '\n', r == '\v', r == '\f', r == '\r', r == ' ', r == 0xa0, r == 0x1680,
		r >= 0x2000 && r <= 0x200a, r == 0x2028, r == 0x2029, r == 0x202f, r == 0x205f, r == 0x3000, r == 0xfeff:
		return true
	}
	return false
}

// jsTrimSpace String.prototype.trim。
func jsTrimSpace(s string) string { return strings.TrimFunc(s, isJSSpace) }

// removeJSSpace s.replace(/\s+/g, "")。
func removeJSSpace(s string) string {
	var b strings.Builder
	for _, r := range s {
		if !isJSSpace(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// collapseJSSpace s.replace(/\s+/g, " ")。
func collapseJSSpace(s string) string {
	var b strings.Builder
	inSpace := false
	for _, r := range s {
		if isJSSpace(r) {
			if !inSpace {
				b.WriteByte(' ')
			}
			inSpace = true
			continue
		}
		inSpace = false
		b.WriteRune(r)
	}
	return b.String()
}

var (
	quoteRe    = regexp.MustCompile("[’‘`]")
	ellipsisRe = regexp.MustCompile(`\.{3}|…`)
	// 括号注释：/[（(][^)）]*[)）]/g
	parenNoteRe = regexp.MustCompile(`[（(][^)）]*[)）]`)
)

// NormalizeAnswer 拼写归一化：小写、统一撇号、去空白与省略号。
func NormalizeAnswer(s string) string {
	s = strings.ToLower(s)
	s = quoteRe.ReplaceAllString(s, "'")
	s = ellipsisRe.ReplaceAllString(s, "")
	return removeJSSpace(s)
}

// IsSpellingCorrect 拼写题判定：归一化后相等；带括号的可选部分（如 "a (an)"）任一形式均可。
func IsSpellingCorrect(answer, target string) bool {
	a := NormalizeAnswer(answer)
	if a == "" {
		return false
	}
	candidates := map[string]bool{NormalizeAnswer(target): true}
	// "a (an)" / "begin(began,begun)" → 允许只写主体
	candidates[NormalizeAnswer(parenNoteRe.ReplaceAllString(target, ""))] = true
	// "bike = bicycle" / "bus-stop" 等：等号两侧均可
	for _, part := range strings.Split(target, "=") {
		candidates[NormalizeAnswer(parenNoteRe.ReplaceAllString(part, ""))] = true
	}
	return candidates[a]
}

// SpellingTarget 拼写题展示用的主体（去掉括号注释）。
func SpellingTarget(spelling string) string {
	first := strings.SplitN(spelling, "=", 2)[0]
	return jsTrimSpace(collapseJSSpace(parenNoteRe.ReplaceAllString(first, "")))
}

// IsChoiceCorrect 选择题判定：选项文本严格相等（两端去空白）。
func IsChoiceCorrect(answer, correct string) bool {
	return jsTrimSpace(answer) == jsTrimSpace(correct)
}

// JudgeAnswer 一次作答的判定：点「不会」直接记为答错（K39），不再看填写内容；
// 否则认义题按选项判定，挖空与拼写都按拼写口径判定。
func JudgeAnswer(mode, answer string, dontKnow bool, definition, spelling string) bool {
	if dontKnow {
		return false
	}
	if mode == "recognition" {
		return IsChoiceCorrect(answer, definition)
	}
	return IsSpellingCorrect(answer, spelling)
}
