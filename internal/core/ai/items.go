package ai

import (
	"math"
	"strconv"
	"strings"
)

// 逐句输出的解析（spec 0005 §3）：沿用 parse.go 的容错提取（去掉代码块、<think>、前后说明文字、多段并列），
// 字段按 JS String(x ?? "") 的宽松语义取值，一项字段不对不连累其他项。

// ---------------------------------------------------------------------------
// 仿写的改造方式
// ---------------------------------------------------------------------------

const (
	ModeReplace   = "replace"   // 替换：保留句型，换内容
	ModeTransform = "transform" // 转换：肯定 ↔ 否定 ↔ 疑问，时态，主动 ↔ 被动
	ModeExpand    = "expand"    // 扩展：加原因、时间、地点等成分
	ModeTransfer  = "transfer"  // 迁移：同一个句型换一个话题场景
)

// VariantModes 合法取值（顺序即提示词里的顺序）。
var VariantModes = []string{ModeReplace, ModeTransform, ModeExpand, ModeTransfer}

// VariantModeLabel 中文名（AI 输出 change 用中文）。
var VariantModeLabel = map[string]string{ModeReplace: "替换", ModeTransform: "转换", ModeExpand: "扩展", ModeTransfer: "迁移"}

var variantModeHint = map[string]string{
	ModeReplace:   "保留句型，换内容",
	ModeTransform: "肯定 ↔ 否定 ↔ 疑问，时态，主动 ↔ 被动",
	ModeExpand:    "加原因、时间、地点等成分",
	ModeTransfer:  "同一个句型换一个话题场景",
}

// NormalizeVariantMode AI 输出或页面提交的改造方式 → 英文键；认不出返回空串。
func NormalizeVariantMode(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	low := strings.ToLower(s)
	for _, m := range VariantModes {
		if low == m || s == VariantModeLabel[m] {
			return m
		}
	}
	for _, m := range VariantModes {
		if strings.Contains(s, VariantModeLabel[m]) {
			return m
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// 粘贴的例句
// ---------------------------------------------------------------------------

// PastedOrigin 一句例句：英文必填，中文可空。
type PastedOrigin struct {
	En string `json:"en"`
	Cn string `json:"cn"`
}

// ParsePastedOrigins 粘贴文本 / 上传的 .txt：每行一句，「英文 | 中文」或者只写英文；空行与没有英文的行忽略。
func ParsePastedOrigins(text string) []PastedOrigin {
	out := []PastedOrigin{}
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.ReplaceAll(line, "｜", "|")
		en, cn, _ := strings.Cut(line, "|")
		en, cn = strings.TrimSpace(en), strings.TrimSpace(cn)
		if en == "" {
			continue
		}
		out = append(out, PastedOrigin{En: en, Cn: cn})
	}
	return out
}

// ---------------------------------------------------------------------------
// 例句
// ---------------------------------------------------------------------------

// ExampleItem 例句回复的一项。
type ExampleItem struct {
	Spelling string
	En       string
	Cn       string
}

func firstNonEmpty(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s := strings.TrimSpace(AsString(m[k])); s != "" {
			return s
		}
	}
	return ""
}

// ParseExampleItems 例句：[{spelling, en, cn}]；兼容旧字段名 example / exampleCn。
func ParseExampleItems(raw string) ([]ExampleItem, error) {
	list, err := ParseAiList(raw)
	if err != nil {
		return nil, err
	}
	out := make([]ExampleItem, len(list))
	for i, it := range list {
		m := AsMap(it)
		out[i] = ExampleItem{
			Spelling: strings.TrimSpace(AsString(m["spelling"])),
			En:       firstNonEmpty(m, "en", "example"),
			Cn:       firstNonEmpty(m, "cn", "exampleCn"),
		}
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// 句型
// ---------------------------------------------------------------------------

// PatternItem 句型回复的一项。
type PatternItem struct {
	En    string  `json:"en"`
	Cn    string  `json:"cn"`
	Frame *string `json:"frame"`
}

// ParsePatternItems 句型：[{en, cn, frame?}]；没有英文的项丢弃。
func ParsePatternItems(raw string) ([]PatternItem, error) {
	list, err := ParseAiList(raw)
	if err != nil {
		return nil, err
	}
	out := []PatternItem{}
	for _, it := range list {
		m := AsMap(it)
		p := PatternItem{En: strings.TrimSpace(AsString(m["en"])), Cn: strings.TrimSpace(AsString(m["cn"]))}
		if p.En == "" {
			continue
		}
		if f := strings.TrimSpace(AsString(m["frame"])); f != "" {
			p.Frame = &f
		}
		out = append(out, p)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// 仿写
// ---------------------------------------------------------------------------

// VariantItem 仿写回复的一项。Origin 为原句序号（从 1 开始）。
type VariantItem struct {
	Origin int
	En     string
	Cn     string
	Change string // ModeReplace 等；认不出为空串
	Note   string
}

// asInt 数字或数字字符串 → 整数；不是整数返回 (0, false)。
func asInt(v any) (int, bool) {
	switch t := v.(type) {
	case float64:
		if t == math.Trunc(t) {
			return int(t), true
		}
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(t)); err == nil {
			return n, true
		}
	}
	return 0, false
}

// ParseVariantItems 仿写：[{origin, en, cn, change, note}]。origin 超出 1..originCount 的项丢弃；
// 缺 origin 时只有一个原句则归到第 1 句，否则丢弃；没有英文的项丢弃。
func ParseVariantItems(raw string, originCount int) ([]VariantItem, error) {
	list, err := ParseAiList(raw)
	if err != nil {
		return nil, err
	}
	out := []VariantItem{}
	for _, it := range list {
		m := AsMap(it)
		v := VariantItem{
			En:     strings.TrimSpace(AsString(m["en"])),
			Cn:     strings.TrimSpace(AsString(m["cn"])),
			Change: NormalizeVariantMode(AsString(m["change"])),
			Note:   strings.TrimSpace(AsString(m["note"])),
		}
		if v.En == "" {
			continue
		}
		n, ok := asInt(m["origin"])
		if !ok {
			if m["origin"] != nil || originCount != 1 {
				continue
			}
			n = 1
		}
		if n < 1 || n > originCount {
			continue
		}
		v.Origin = n
		out = append(out, v)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// 短文
// ---------------------------------------------------------------------------

// PassageSentenceItem 短文的一句。
type PassageSentenceItem struct {
	En        string `json:"en"`
	Cn        string `json:"cn"`
	Paragraph int    `json:"paragraph"`
}

// PassageQA 理解题。
type PassageQA struct {
	Q string `json:"q"`
	A string `json:"a"`
}

// PassageReply 短文回复。Legacy 表示 AI 仍按旧格式（整段 passage / passageCn）输出，Sentences 为空，
// 由调用方按句拆分；否则 Body / BodyCn 由逐句结果拼成（同段英文用空格、中文直接连接，段落之间空一行）。
type PassageReply struct {
	Title     string
	TitleCn   string
	Sentences []PassageSentenceItem
	Questions []PassageQA
	Body      string
	BodyCn    string
	Legacy    bool
}

// PassageQuestionLimit 理解题最多保留几道。
const PassageQuestionLimit = 5

// ParsePassageReply 短文：{title, titleCn, sentences: [{en, cn, paragraph}], questions}；
// 兼容旧格式 {passage, passageCn}。没有任何正文时报错「AI 没有生成短文」。
func ParsePassageReply(raw string) (PassageReply, error) {
	obj, err := ParseAiObject(raw)
	if err != nil {
		return PassageReply{}, err
	}
	r := PassageReply{Title: "Reading", TitleCn: strings.TrimSpace(AsString(obj["titleCn"])), Questions: []PassageQA{}}
	if t := strings.TrimSpace(AsString(obj["title"])); t != "" {
		r.Title = t
	}
	if arr, ok := obj["questions"].([]any); ok {
		for i, qv := range arr {
			if i >= PassageQuestionLimit {
				break
			}
			qm := AsMap(qv)
			r.Questions = append(r.Questions, PassageQA{Q: AsString(qm["q"]), A: AsString(qm["a"])})
		}
	}

	if arr, ok := obj["sentences"].([]any); ok {
		// 段落号归一成从 0 开始连续编号；缺省的沿用上一句的段落
		para, last, seen := 0, 0, false
		for _, sv := range arr {
			sm := AsMap(sv)
			en := strings.TrimSpace(AsString(sm["en"]))
			if en == "" {
				continue
			}
			if n, ok := asInt(sm["paragraph"]); ok {
				if seen && n != last {
					para++
				}
				last, seen = n, true
			}
			r.Sentences = append(r.Sentences, PassageSentenceItem{En: en, Cn: strings.TrimSpace(AsString(sm["cn"])), Paragraph: para})
		}
	}
	if len(r.Sentences) > 0 {
		var en, cn strings.Builder
		for i, s := range r.Sentences {
			if i > 0 {
				if s.Paragraph != r.Sentences[i-1].Paragraph {
					en.WriteString("\n\n")
					cn.WriteString("\n\n")
				} else {
					en.WriteString(" ")
				}
			}
			en.WriteString(s.En)
			cn.WriteString(s.Cn)
		}
		r.Body, r.BodyCn = en.String(), cn.String()
		return r, nil
	}

	r.Sentences = []PassageSentenceItem{}
	r.Body = strings.TrimSpace(AsString(obj["passage"]))
	r.BodyCn = strings.TrimSpace(AsString(obj["passageCn"]))
	r.Legacy = true
	if r.Body == "" {
		return PassageReply{}, NewAPIError("SERVER", "AI 没有生成短文")
	}
	return r, nil
}

// ---------------------------------------------------------------------------
// 重写一句
// ---------------------------------------------------------------------------

// RewriteItem 重写后的一句。
type RewriteItem struct {
	En    string `json:"en"`
	Cn    string `json:"cn"`
	Frame string `json:"frame,omitempty"`
}

// ParseRewriteReply {en, cn}（也接受只有一项的数组）；没有英文时报错。
func ParseRewriteReply(raw string) (RewriteItem, error) {
	list, err := ParseAiList(raw)
	if err != nil {
		return RewriteItem{}, err
	}
	for _, it := range list {
		m := AsMap(it)
		r := RewriteItem{En: strings.TrimSpace(AsString(m["en"])), Cn: strings.TrimSpace(AsString(m["cn"])), Frame: strings.TrimSpace(AsString(m["frame"]))}
		if r.En != "" {
			return r, nil
		}
	}
	return RewriteItem{}, NewAPIError("SERVER", "AI 没有给出重写后的句子")
}
