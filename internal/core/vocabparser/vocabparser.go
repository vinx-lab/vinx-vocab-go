// Package vocabparser 词表文本解析（纯函数，逐行移植旧 apps/api/src/lib/vocab-parser.ts）。
//
// 支持两类来源：
//   - 教材词表：`Unit 1` 标题分节，行格式 `spelling /phonetic/ pos. 释义`，短语行无音标无词性；
//   - 考纲词表：无分节、多词性段（`about /əˈbaʊt/ ad. 大约 prep. 关于`）。
//
// 容忍 OCR 常见问题：星号/序号前缀、音标只剩右斜杠、中英文无空格粘连。
// 词性保留原文标记（如 `vt.`、`prep./ad.`），不做英文全称映射。
//
// 与 JS 的差异处理：JS 正则的 \s 与 String.trim 覆盖 Unicode 空白（含全角空格、BOM），
// Go RE2 的 \s 只含 ASCII，这里统一用 jsSpace 字符类与 jsTrim 对齐。
package vocabparser

import (
	"regexp"
	"sort"
	"strings"
)

// EntryStatus 条目状态：ok | warning | error。
type EntryStatus = string

const (
	StatusOK      EntryStatus = "ok"
	StatusWarning EntryStatus = "warning"
	StatusError   EntryStatus = "error"
)

// ParsedEntry 解析出的一行词条。
type ParsedEntry struct {
	Spelling     string      `json:"spelling"`
	Phonetic     string      `json:"phonetic"`
	PartOfSpeech string      `json:"partOfSpeech"`
	Definition   string      `json:"definition"`
	Type         string      `json:"type"` // word | phrase
	Status       EntryStatus `json:"status"`
	Issues       []string    `json:"issues"`
	Raw          string      `json:"raw"`
	Line         int         `json:"line"`
}

// ParsedUnit 一个单元及其词条。
type ParsedUnit struct {
	Name    string        `json:"name"`
	Entries []ParsedEntry `json:"entries"`
}

// jsSpace JS 正则 \s 的字符集（不含外层方括号）。
const jsSpace = `\t\n\v\f\r \x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}`

const ws = `[` + jsSpace + `]`

const cjkClass = `[\x{3400}-\x{9fff}（；，…]`

const posToken = `(?:(?:n|v|vt|vi|a|adj|ad|adv|prep|conj|pron|art|num|int|interj|aux|modal|abbr|exclam|pl|det)\.|exclamation)`

var (
	cjkRe        = regexp.MustCompile(cjkClass)
	leadingPos   = regexp.MustCompile(`(?i)^(` + posToken + `(?:` + ws + `*[/&,]` + ws + `*` + posToken + `)*)` + ws + `*`)
	unitHeading  = regexp.MustCompile(`(?i)^(?:#+` + ws + `*)?((?:unit|module|starter|lesson|welcome|chapter)\b[^\x{3400}-\x{9fff}]*)$`)
	mdHeading    = regexp.MustCompile(`^#+` + ws + `*(.+)$`)
	prefixRe     = regexp.MustCompile(`^` + ws + `*(?:\*+|\d+[.)、]` + ws + `*)`)
	wsRunRe      = regexp.MustCompile(ws + `+`)
	wsRe         = regexp.MustCompile(ws)
	fullPhonetic = regexp.MustCompile(`/([^/]*)/`)
	partialPhon  = regexp.MustCompile(ws + `'([^/` + jsSpace + `][^/]*)/` + ws + `*`)
	posInLine    = regexp.MustCompile(`(?i)` + ws + `(` + posToken + `)`)
	letterRe     = regexp.MustCompile(`(?i)[a-z]`)
	phoneticInDf = regexp.MustCompile(`/[^/]*/`)
	oddCharRe    = regexp.MustCompile(`(?i)[^a-z0-9\x{00c0}-\x{024f}` + jsSpace + `'’.\-()!?,&/…]`)
	lineSplitRe  = regexp.MustCompile(`\r?\n`)
)

func isJSSpace(r rune) bool {
	switch {
	case r == '\t', r == '\n', r == '\v', r == '\f', r == '\r', r == ' ', r == 0xa0, r == 0x1680,
		r >= 0x2000 && r <= 0x200a, r == 0x2028, r == 0x2029, r == 0x202f, r == 0x205f, r == 0x3000, r == 0xfeff:
		return true
	}
	return false
}

// jsTrim 等价于 JS String.prototype.trim。
func jsTrim(s string) string { return strings.TrimFunc(s, isJSSpace) }

// NormalizeSpelling 规整拼写：去首尾空白、合并内部空白、去星号与序号前缀。
func NormalizeSpelling(s string) string {
	if loc := prefixRe.FindStringIndex(s); loc != nil {
		s = s[loc[1]:]
	}
	s = wsRunRe.ReplaceAllString(s, " ")
	return jsTrim(s)
}

func parseLine(raw string, lineNo int) ParsedEntry {
	issues := []string{}
	s := NormalizeSpelling(raw)

	// 1. 音标：完整 /.../，或只有右斜杠的碎片 'xxx/
	phonetic, spellingPart, rest := "", "", ""
	if m := fullPhonetic.FindStringSubmatchIndex(s); m != nil && m[0] > 0 {
		phonetic = jsTrim(s[m[2]:m[3]])
		spellingPart = s[:m[0]]
		rest = s[m[1]:]
	} else if m := partialPhon.FindStringSubmatchIndex(s); m != nil {
		phonetic = jsTrim(s[m[2]:m[3]])
		spellingPart = s[:m[0]]
		rest = s[m[1]:]
		issues = append(issues, "音标不完整")
	}

	if phonetic == "" {
		// 无音标：拼写到第一个“独立词性标记”或第一个中文字符为止
		posIdx := -1
		if m := posInLine.FindStringIndex(s); m != nil {
			posIdx = m[0]
		}
		cjkIdx := -1
		if m := cjkRe.FindStringIndex(s); m != nil {
			cjkIdx = m[0]
		}
		cut := cjkIdx
		if posIdx >= 0 && (cjkIdx < 0 || posIdx < cjkIdx) {
			cut = posIdx
		}
		if cut < 0 {
			return ParsedEntry{Spelling: s, Type: "word", Status: StatusError, Issues: []string{"缺少中文释义"}, Raw: raw, Line: lineNo}
		}
		spellingPart = s[:cut]
		rest = s[cut:]
	}

	spelling := jsTrim(spellingPart)
	rest = jsTrim(rest)
	partOfSpeech := ""
	definition := rest
	if m := leadingPos.FindStringSubmatchIndex(rest); m != nil {
		partOfSpeech = wsRunRe.ReplaceAllString(rest[m[2]:m[3]], "")
		definition = rest[m[1]:]
	}
	definition = jsTrim(definition)

	typ := "word"
	if wsRe.MatchString(spelling) {
		typ = "phrase"
	}
	status := StatusOK

	if spelling == "" || !letterRe.MatchString(spelling) {
		issues = append(issues, "缺少英文拼写")
		status = StatusError
	}
	if definition == "" || !cjkRe.MatchString(definition) {
		issues = append(issues, "缺少中文释义")
		status = StatusError
	}
	if status != StatusError {
		if phoneticInDf.MatchString(definition) {
			issues = append(issues, "释义中含音标，可能两词粘连")
		}
		if oddCharRe.MatchString(spelling) {
			issues = append(issues, "拼写含异常字符")
		}
		if typ == "word" && phonetic == "" {
			issues = append(issues, "缺少音标")
		}
		if typ == "word" && partOfSpeech == "" {
			issues = append(issues, "缺少词性")
		}
		if len(issues) > 0 {
			status = StatusWarning
		}
	}

	return ParsedEntry{Spelling: spelling, Phonetic: phonetic, PartOfSpeech: partOfSpeech, Definition: definition, Type: typ, Status: status, Issues: issues, Raw: raw, Line: lineNo}
}

// ParseVocabText 解析整段词表文本，按单元标题分节；无标题的条目归入 defaultUnit（空串时用「未分组」）。
func ParseVocabText(text, defaultUnit string) []ParsedUnit {
	if defaultUnit == "" {
		defaultUnit = "未分组"
	}
	var units []*ParsedUnit
	var current *ParsedUnit
	find := func(name string) *ParsedUnit {
		for _, u := range units {
			if u.Name == name {
				return u
			}
		}
		return nil
	}

	for i, line := range lineSplitRe.Split(text, -1) {
		trimmed := jsTrim(line)
		if trimmed == "" {
			continue
		}
		name, isHeading := "", false
		if m := unitHeading.FindStringSubmatch(trimmed); m != nil {
			name, isHeading = m[1], true
		} else if m := mdHeading.FindStringSubmatch(trimmed); m != nil {
			name, isHeading = m[1], true
		}
		if isHeading {
			name = jsTrim(name)
			current = find(name)
			if current == nil {
				current = &ParsedUnit{Name: name, Entries: []ParsedEntry{}}
				units = append(units, current)
			}
			continue
		}
		if current == nil {
			current = &ParsedUnit{Name: defaultUnit, Entries: []ParsedEntry{}}
			units = append(units, current)
		}
		current.Entries = append(current.Entries, parseLine(trimmed, i+1))
	}

	out := []ParsedUnit{}
	for _, u := range units {
		if len(u.Entries) > 0 {
			out = append(out, *u)
		}
	}
	return out
}

// SplitByInitial 无分节的大词表按首字母切成单元（A、B、C…），便于按单元安排计划。
func SplitByInitial(units []ParsedUnit) []ParsedUnit {
	buckets := map[string][]ParsedEntry{}
	for _, u := range units {
		for _, e := range u.Entries {
			letter := "#"
			if m := letterRe.FindString(e.Spelling); m != "" {
				letter = strings.ToUpper(m)
			}
			buckets[letter] = append(buckets[letter], e)
		}
	}
	keys := make([]string, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]ParsedUnit, 0, len(keys))
	for _, k := range keys {
		out = append(out, ParsedUnit{Name: k, Entries: buckets[k]})
	}
	return out
}

// DedupeUnitEntries 同一单元内按拼写（忽略大小写）去重，保留首次出现。
func DedupeUnitEntries(u ParsedUnit) ParsedUnit {
	seen := map[string]bool{}
	entries := []ParsedEntry{}
	for _, e := range u.Entries {
		key := strings.ToLower(e.Spelling)
		if seen[key] {
			continue
		}
		seen[key] = true
		entries = append(entries, e)
	}
	return ParsedUnit{Name: u.Name, Entries: entries}
}
