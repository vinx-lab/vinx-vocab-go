package vocabparser

import "regexp"

// 单元里的句型、课文段（spec 0004 §7）：
//
//	[句型]
//	How do you learn English words? | 你是如何学习英文单词的？
//	I find ... useful. | 我发现……很有用。 | I find making word cards useful.
//	[课文]
//	Title: How I Learn English | 我是怎样学英语的
//	I used to find English hard. | 我过去觉得英语很难。
//	（空行分段）
//
// 遇到下一个 Unit 标题或新的段落标记时结束当前段。

// 篇的类型：text 连贯课文（按段落显示），list 句型清单（逐条显示）。
const (
	TextKindText = "text"
	TextKindList = "list"
)

// 没有 Title 行时的默认标题。
const (
	DefaultListTitle = "重点句型"
	DefaultTextTitle = "课文"
)

// ParsedSentence 段里的一句。
type ParsedSentence struct {
	En        string      `json:"en"`
	Cn        string      `json:"cn"`
	Frame     string      `json:"frame"` // 句型骨架（英文带「...」时）；没有为空串
	Paragraph int         `json:"paragraph"`
	Status    EntryStatus `json:"status"`
	Issues    []string    `json:"issues"`
	Raw       string      `json:"raw"`
	Line      int         `json:"line"`
}

// ParsedText 一个 [句型] 或 [课文] 段。
type ParsedText struct {
	Kind      string           `json:"kind"`
	Title     string           `json:"title"`
	TitleCn   string           `json:"titleCn"`
	Sentences []ParsedSentence `json:"sentences"`
	Line      int              `json:"line"`
}

var (
	sectionMarker = regexp.MustCompile(`^[\[【]` + ws + `*(句型|课文)` + ws + `*[\]】]$`)
	titleLine     = regexp.MustCompile(`(?i)^(?:title|标题)` + ws + `*[:：]` + ws + `*(.*)$`)
	colSplit      = regexp.MustCompile(`[|｜]`)
	ellipsisRe    = regexp.MustCompile(`\.\.\.|…`)
)

// markerKind 段落标记行 → 类型；不是标记返回空串。
func markerKind(trimmed string) string {
	m := sectionMarker.FindStringSubmatch(trimmed)
	if m == nil {
		return ""
	}
	if m[1] == "句型" {
		return TextKindList
	}
	return TextKindText
}

func newParsedText(kind string, line int) *ParsedText {
	title := DefaultTextTitle
	if kind == TextKindList {
		title = DefaultListTitle
	}
	return &ParsedText{Kind: kind, Title: title, Sentences: []ParsedSentence{}, Line: line}
}

// textSection 正在解析的段。
type textSection struct {
	text      *ParsedText
	paragraph int
	pending   bool // 空行之后、下一句之前：下一句开新段
}

// line 处理段里的一行（trimmed 为空表示空行）。
func (s *textSection) line(raw, trimmed string, lineNo int) {
	if trimmed == "" {
		if s.text.Kind == TextKindText && len(s.text.Sentences) > 0 {
			s.pending = true
		}
		return
	}
	if m := titleLine.FindStringSubmatch(trimmed); m != nil {
		parts := colSplit.Split(m[1], -1)
		if t := jsTrim(parts[0]); t != "" {
			s.text.Title = t
		}
		if len(parts) > 1 {
			s.text.TitleCn = jsTrim(parts[1])
		}
		return
	}
	if s.pending {
		s.paragraph++
		s.pending = false
	}
	s.text.Sentences = append(s.text.Sentences, parseSentenceLine(s.text.Kind, raw, trimmed, lineNo, s.paragraph))
}

func parseSentenceLine(kind, raw, trimmed string, lineNo, paragraph int) ParsedSentence {
	parts := colSplit.Split(trimmed, -1)
	for i := range parts {
		parts[i] = jsTrim(wsRunRe.ReplaceAllString(parts[i], " "))
	}
	ps := ParsedSentence{Paragraph: paragraph, Status: StatusOK, Issues: []string{}, Raw: raw, Line: lineNo}
	ps.En = parts[0]
	if len(parts) > 1 {
		ps.Cn = parts[1]
	}
	if ps.En == "" || !letterRe.MatchString(ps.En) {
		ps.Issues = append(ps.Issues, "缺少英文")
		ps.Status = StatusError
	}
	if ps.Cn == "" {
		ps.Issues = append(ps.Issues, "缺少中文")
		ps.Status = StatusError
	}
	if ps.Status == StatusError {
		return ps
	}
	third := ""
	if len(parts) > 2 {
		third = parts[2]
	}
	if kind == TextKindList && ellipsisRe.MatchString(ps.En) {
		// 句型骨架：第二列中文对应骨架，第三列是完整句，默写考完整句
		ps.Frame = ps.En
		if third != "" {
			ps.En = third
		} else {
			ps.Issues = append(ps.Issues, "句型缺少完整例句")
		}
		if len(parts) > 3 {
			ps.Issues = append(ps.Issues, "多余的列已忽略")
		}
	} else if len(parts) > 2 {
		ps.Issues = append(ps.Issues, "多余的列已忽略")
	}
	if len(ps.Issues) > 0 {
		ps.Status = StatusWarning
	}
	return ps
}

// ParseUnitTexts 单元页面粘贴导入句型 / 课文：格式与词表导入里的段相同，只是不需要 Unit 标题；
// 第一个段落标记之前的行按 defaultKind（空串按 list）处理。没有句子的段不返回。
func ParseUnitTexts(text, defaultKind string) []ParsedText {
	if defaultKind != TextKindText {
		defaultKind = TextKindList
	}
	var texts []*ParsedText
	var cur *textSection
	for i, line := range lineSplitRe.Split(text, -1) {
		trimmed := jsTrim(line)
		if k := markerKind(trimmed); k != "" {
			t := newParsedText(k, i+1)
			texts = append(texts, t)
			cur = &textSection{text: t}
			continue
		}
		if cur == nil {
			if trimmed == "" {
				continue
			}
			t := newParsedText(defaultKind, i+1)
			texts = append(texts, t)
			cur = &textSection{text: t}
		}
		cur.line(line, trimmed, i+1)
	}
	out := []ParsedText{}
	for _, t := range texts {
		if len(t.Sentences) > 0 {
			out = append(out, *t)
		}
	}
	return out
}
