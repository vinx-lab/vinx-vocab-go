package ai

import (
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"
)

var (
	thinkRe = regexp.MustCompile(`(?is)<think>.*?</think>`)
	fenceRe = regexp.MustCompile(`(?i)` + "```(?:json)?")
)

// normalizeForScan 剥掉 <think> 段与 ```json 代码围栏（转成换行），供扫描 JSON 片段用。
func normalizeForScan(raw string) string {
	text := thinkRe.ReplaceAllString(raw, "")
	text = fenceRe.ReplaceAllString(text, "```")
	return strings.ReplaceAll(text, "```", "\n")
}

// scanJSONFragments 在文本里找出所有语法上合法的 JSON 片段（{...} 或 [...]，正确处理字符串里的括号），
// 容忍前后说明文字与多段并列输出。
func scanJSONFragments(body string) []string {
	var out []string
	i := 0
	for i < len(body) {
		ch := body[i]
		if ch != '{' && ch != '[' {
			i++
			continue
		}
		open, close := ch, byte('}')
		if ch == '[' {
			close = ']'
		}
		depth := 0
		inString := false
		escaped := false
		end := -1
		for j := i; j < len(body); j++ {
			c := body[j]
			if inString {
				if escaped {
					escaped = false
				} else if c == '\\' {
					escaped = true
				} else if c == '"' {
					inString = false
				}
				continue
			}
			if c == '"' {
				inString = true
			} else if c == open {
				depth++
			} else if c == close {
				depth--
				if depth == 0 {
					end = j
					break
				}
			}
		}
		if end < 0 {
			break
		}
		frag := body[i : end+1]
		if json.Valid([]byte(frag)) {
			out = append(out, frag)
		}
		i = end + 1
	}
	return out
}

// ExtractJSONValues 从回复中提取所有完整的 JSON 值（容忍 ```json 包裹、<think> 推理段、前后说明文字，
// 以及小模型常见的“一次吐好几段 JSON”）。
func ExtractJSONValues(raw string) []any {
	frags := scanJSONFragments(normalizeForScan(raw))
	out := make([]any, 0, len(frags))
	for _, f := range frags {
		var v any
		if json.Unmarshal([]byte(f), &v) == nil {
			out = append(out, v)
		}
	}
	return out
}

// ParseAiObject 解析成对象（取第一个对象；找不到则报错并回显开头内容便于排查）。
//
// 不对字段类型做任何校验（与旧版 lib/ai-parse.ts 的运行时语义一致：TS 泛型 `as T` 只是编译期断言，
// JSON.parse 出来的其实是 any）。调用方用 AsString 等宽松转换逐字段取值，一个字段类型不对不影响其他字段。
func ParseAiObject(raw string) (map[string]any, error) {
	for _, v := range ExtractJSONValues(raw) {
		if m, ok := v.(map[string]any); ok {
			return m, nil
		}
	}
	return nil, NewAPIError("SERVER", UnparseableMessage(raw))
}

// ParseAiList 解析成对象数组：合并所有数组片段与散落的对象（含 {items:[...]} 形式）。
//
// 元素类型不做校验，某一项字段类型不对（甚至整项不是对象）不会连累数组里其他项——与旧版一致：
// `parseAiList` 本身只按结构（是不是数组 / 有没有 items）分流，字段级取值交给调用方。
func ParseAiList(raw string) ([]any, error) {
	var out []any
	for _, v := range ExtractJSONValues(raw) {
		switch t := v.(type) {
		case []any:
			out = append(out, t...)
		case map[string]any:
			if items, ok := t["items"].([]any); ok {
				out = append(out, items...)
			} else {
				out = append(out, t)
			}
		}
	}
	if len(out) == 0 {
		return nil, NewAPIError("SERVER", UnparseableMessage(raw))
	}
	return out, nil
}

// AsMap 把 AI 回复数组里的一项安全转成 map（不是对象时返回 nil map，读取任意字段都得到零值，
// 与 JS 在非对象上访问未知属性得到 undefined 效果一致，不会 panic）。
func AsMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

// AsString JS `String(x ?? "")` 语义：nil → ""；字符串原样；数字按 JS Number→String 规则格式化；
// 布尔转 "true"/"false"；其余（对象、数组等业务字段不会出现的类型）按空串兜底。
func AsString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return formatJSNumber(t)
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}

// formatJSNumber 近似 JS Number.prototype.toString()：整数不带小数点，其余用最短十进制表示。
// 不处理科学计数法边界（AI 输出里不会出现极端大小的数字，字段本来就该是字符串）。
func formatJSNumber(f float64) string {
	if math.IsNaN(f) {
		return "NaN"
	}
	if math.IsInf(f, 1) {
		return "Infinity"
	}
	if math.IsInf(f, -1) {
		return "-Infinity"
	}
	if f == math.Trunc(f) && math.Abs(f) < 1e21 {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

var (
	regexEscapeRe = regexp.MustCompile(`[.*+?^${}()|[\]\\]`)
	spaceRe       = regexp.MustCompile(`\s+`)
	funcWords     = map[string]bool{"be": true, "do": true, "get": true, "a": true, "an": true, "the": true, "one's": true, "somebody": true, "something": true, "sth": true, "sb": true}
)

// ExampleUsesWord 例句是否真的用上了目标词：单词按词形变化匹配；短语允许 be 动词等变形，
// 只要大部分实词出现即可。
func ExampleUsesWord(example, spelling string) bool {
	text := strings.ToLower(example)
	target := strings.ToLower(spelling)
	target = strings.SplitN(target, "=", 2)[0]
	target = parenRe.ReplaceAllString(target, "")
	target = strings.TrimSpace(target)
	tokens := []string{}
	for _, t := range spaceRe.Split(target, -1) {
		if t != "" {
			tokens = append(tokens, t)
		}
	}
	if len(tokens) == 0 {
		return false
	}
	hasToken := func(t string) bool {
		// Compile（非 MustCompile）：regexEscapeRe 转义过的字面量理论上总能编译成功，但对拼写异常
		// （如含非法 UTF-8）防御一下，编译失败按不命中处理，不 panic（I3：后台任务不能被 panic 打断）。
		re, err := regexp.Compile(`\b` + regexEscapeRe.ReplaceAllString(t, `\$0`) + `(s|es|ed|ing|d)?\b`)
		if err != nil {
			return false
		}
		return re.MatchString(text)
	}
	if len(tokens) == 1 {
		return hasToken(tokens[0])
	}
	var content []string
	for _, t := range tokens {
		if !funcWords[t] {
			content = append(content, t)
		}
	}
	hits := 0
	for _, t := range content {
		if hasToken(t) {
			hits++
		}
	}
	if len(content) == 0 {
		for _, t := range tokens {
			if hasToken(t) {
				return true
			}
		}
		return false
	}
	need := (len(content) + 1) / 2 // ceil(len/2)
	if need < 1 {
		need = 1
	}
	return hits >= need
}

// parenRe 去掉括号注释（全角/半角），与 audioplan / questions.ts 里的用法一致。
var parenRe = regexp.MustCompile(`[（(][^)）]*[)）]`)
