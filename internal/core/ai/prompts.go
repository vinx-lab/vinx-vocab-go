package ai

import (
	"strconv"
	"strings"
	"unicode/utf16"
)

// jsLen JS String.length（UTF-16 code unit 数），与 httpx.jsLen 语义一致（core 不依赖 httpx）。
func jsLen(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

// PromptKey 可编辑的默认要求模板：例句 / 短文。
type PromptKey = string

const (
	PromptExample PromptKey = "example"
	PromptPassage PromptKey = "passage"
)

// PromptKeys 顺序固定（与旧版 AI_PROMPT_KEYS 一致）。
var PromptKeys = []PromptKey{PromptExample, PromptPassage}

// PromptLabel 中文名。
var PromptLabel = map[PromptKey]string{PromptExample: "例句", PromptPassage: "短文"}

// PromptMaxLength 模板、以及生成前页面上可见提示词的最大长度（去掉首尾空白后，按 JS String.length 计）。
const PromptMaxLength = 6000

// DefaultPromptTemplates 默认要求模板（不含输出格式）。
var DefaultPromptTemplates = map[PromptKey]string{
	PromptExample: `你是初中英语教材的例句编辑。为每个给定单词写一个例句。要求：
1. 句子必须用上该单词（可用复数、过去式、现在分词等词形变化；短语中的 be/do 可以变形，如 be good at → is good at）；
2. 长度 6–14 个英文单词，语法正确，只用初中生认识的常用词；
3. 场景贴近校园与日常生活，语义要体现给定的中文释义；
4. 配一句自然的中文翻译；
5. 不要俚语、文化梗或生僻专有名词。`,

	PromptPassage: `你是初中英语阅读材料编辑。用给定的目标单词写一篇短文。要求：
1. 每个目标单词至少出现一次（允许常见词形变化），出现得自然；
2. 总长 80–140 个英文单词，情节连贯、贴近中学生生活；
3. 除目标词外只用初中常用词汇，单句不超过 20 词；
4. 给出整篇中文翻译，以及 2 道中文理解问题与参考答案；
5. 内容积极健康，不涉及暴力、政治、宗教等敏感话题。`,
}

// OutputFormats 输出格式（系统消息）：程序按这些字段解析回复，不对用户开放。
var OutputFormats = map[PromptKey]string{
	PromptExample: `按用户消息里的要求为给定单词写例句。输出格式（必须遵守）：
只输出一个 JSON 数组（所有单词放在同一个数组里）：[{"spelling":"给定单词","example":"英文例句","exampleCn":"中文翻译"}]。不要输出任何其他文字。`,

	PromptPassage: `按用户消息里的要求用目标单词写短文。输出格式（必须遵守）：
只输出 JSON：{"title":"英文标题","titleCn":"中文标题","passage":"英文短文","passageCn":"中文翻译","questions":[{"q":"问题","a":"答案"}]}。段落之间用 \n\n 分隔。不要输出其他文字。`,
}

// PromptCheck 校验结果。
type PromptCheck struct {
	OK      bool
	Value   string
	Message string
}

// ValidatePromptText 模板和可见提示词共用的校验：去掉首尾空白、不能为空、不超过上限。
func ValidatePromptText(raw string) PromptCheck {
	value := strings.TrimSpace(raw)
	if value == "" {
		return PromptCheck{Message: "提示词不能为空"}
	}
	if jsLen(value) > PromptMaxLength {
		return PromptCheck{Message: "提示词最多 " + strconv.Itoa(PromptMaxLength) + " 字（现在 " + strconv.Itoa(jsLen(value)) + " 字）"}
	}
	return PromptCheck{OK: true, Value: value}
}

// StoredPromptTemplates 只存改过的项（AppSetting key = "ai.promptTemplates"）。
type StoredPromptTemplates map[PromptKey]string

// ParseStoredTemplates 校验数据库里的 JSON；不认识的键、空内容忽略。
func ParseStoredTemplates(v map[string]any) StoredPromptTemplates {
	out := StoredPromptTemplates{}
	for _, k := range PromptKeys {
		if raw, ok := v[k]; ok {
			if s, ok := raw.(string); ok && strings.TrimSpace(s) != "" {
				out[k] = s
			}
		}
	}
	return out
}

// ResolvedTemplate 当前生效的要求模板。
type ResolvedTemplate struct {
	Text   string
	Custom bool
}

// ResolveTemplate 有自定义用自定义，否则用默认。
func ResolveTemplate(key PromptKey, stored StoredPromptTemplates) ResolvedTemplate {
	if custom, ok := stored[key]; ok && custom != "" {
		return ResolvedTemplate{Text: custom, Custom: true}
	}
	return ResolvedTemplate{Text: DefaultPromptTemplates[key]}
}

// MergeStoredTemplates 把本次提交合并到已保存的内容上（调用前已逐项通过校验）；
// 和默认一样的项视为没改，不存。
func MergeStoredTemplates(stored, updates StoredPromptTemplates) StoredPromptTemplates {
	next := StoredPromptTemplates{}
	for k, v := range stored {
		next[k] = v
	}
	for _, k := range PromptKeys {
		text, ok := updates[k]
		if !ok {
			continue
		}
		if text == DefaultPromptTemplates[k] {
			delete(next, k)
		} else {
			next[k] = text
		}
	}
	return next
}

// PromptItem GET /settings/ai/prompts 里的一项。
type PromptItem struct {
	Key         PromptKey `json:"key"`
	Current     string    `json:"current"`
	IsDefault   bool      `json:"isDefault"`
	DefaultText string    `json:"defaultText"`
}

// PromptsView GET /settings/ai/prompts 的视图。
type PromptsView struct {
	Example PromptItem `json:"example"`
	Passage PromptItem `json:"passage"`
}

// ToPromptsView 构造视图。
func ToPromptsView(stored StoredPromptTemplates) PromptsView {
	item := func(key PromptKey) PromptItem {
		r := ResolveTemplate(key, stored)
		return PromptItem{Key: key, Current: r.Text, IsDefault: !r.Custom, DefaultText: DefaultPromptTemplates[key]}
	}
	return PromptsView{Example: item(PromptExample), Passage: item(PromptPassage)}
}

// PromptWord 拼提示词用到的单词字段。
type PromptWord struct {
	Spelling     string
	Definition   string
	PartOfSpeech string // 空串表示无
}

// BuildExamplePrompt 例句的可见提示词：要求模板 + 单词列表。
func BuildExamplePrompt(template string, words []PromptWord) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(template))
	b.WriteString("\n\n请为下面 ")
	b.WriteString(strconv.Itoa(len(words)))
	b.WriteString(" 个词各写一个例句：")
	for i, w := range words {
		b.WriteString("\n")
		b.WriteString(strconv.Itoa(i + 1))
		b.WriteString(". ")
		b.WriteString(w.Spelling)
		if w.PartOfSpeech != "" {
			b.WriteString(" (")
			b.WriteString(w.PartOfSpeech)
			b.WriteString(")")
		}
		b.WriteString(" —— ")
		b.WriteString(w.Definition)
	}
	return b.String()
}

// BuildPassagePrompt 短文的可见提示词：要求模板 + 目标单词 + 主题。
func BuildPassagePrompt(template string, words []PromptWord, topic string) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(template))
	b.WriteString("\n\n目标单词（")
	b.WriteString(strconv.Itoa(len(words)))
	b.WriteString(" 个）：")
	for i, w := range words {
		b.WriteString("\n")
		b.WriteString(strconv.Itoa(i + 1))
		b.WriteString(". ")
		b.WriteString(w.Spelling)
		b.WriteString(" —— ")
		b.WriteString(w.Definition)
	}
	if t := strings.TrimSpace(topic); t != "" {
		b.WriteString("\n\n主题倾向：")
		b.WriteString(t)
	}
	return b.String()
}

// Messages 发给模型的两条消息。
type Messages struct {
	System string
	User   string
}

// BuildMessages 系统消息是隐藏的输出格式，用户消息是可见提示词（原样）。
func BuildMessages(key PromptKey, visiblePrompt string) Messages {
	return Messages{System: OutputFormats[key], User: visiblePrompt}
}
