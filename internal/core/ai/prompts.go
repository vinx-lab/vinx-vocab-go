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

// PromptKey 可编辑的默认要求模板：例句 / 短文 / 句型 / 仿写（spec 0005 §2）。
type PromptKey = string

const (
	PromptExample PromptKey = "example"
	PromptPassage PromptKey = "passage"
	PromptPattern PromptKey = "pattern"
	PromptVariant PromptKey = "variant"
)

// PromptKeys 顺序固定（前两项与旧版 AI_PROMPT_KEYS 一致，spec 0005 追加句型、仿写）。
var PromptKeys = []PromptKey{PromptExample, PromptPassage, PromptPattern, PromptVariant}

// PromptLabel 中文名。
var PromptLabel = map[PromptKey]string{PromptExample: "例句", PromptPassage: "短文", PromptPattern: "句型", PromptVariant: "仿写"}

// PromptMaxLength 模板、以及生成前页面上可见提示词的最大长度（去掉首尾空白后，按 JS String.length 计）。
const PromptMaxLength = 6000

// DefaultPromptTemplates 默认要求模板（不含输出格式）。可用占位符 {学段}、{句长}、{短文长度}、{单句上限}，
// 生成时按学段替换（FillPlaceholders）；设置页显示的是带占位符的原文。
var DefaultPromptTemplates = map[PromptKey]string{
	PromptExample: `你是{学段}英语教材的例句编辑。为每个给定单词写一个例句。要求：
1. 句子必须用上该单词（可用词形变化；短语中的 be/do 可以变形）；
2. 长度 {句长}，语法正确，只用{学段}学生认识的常用词；
3. 场景贴近校园与日常生活，语义要体现给定的中文释义；
4. 配一句自然的中文翻译；
5. 不要俚语、文化梗或生僻专有名词。`,

	PromptPassage: `你是{学段}英语阅读材料编辑。用给定的目标单词写一篇短文。要求：
1. 每个目标单词至少出现一次（允许常见词形变化），出现得自然；
2. 总长 {短文长度}，情节连贯、贴近中学生生活；
3. 除目标词外只用{学段}常用词汇，单句不超过 {单句上限}；
4. 给出整篇中文翻译，以及 2 道中文理解问题与参考答案；
5. 内容积极健康，不涉及暴力、政治、宗教等敏感话题。`,

	PromptPattern: `你是{学段}英语教材编辑。根据给定单元的话题和词汇，写出本单元的重点句型。要求：
1. 句型是这个话题下最常用、最值得背下来的表达，覆盖问句和答句；
2. 有固定结构的句型给出骨架（用 ... 表示可替换部分）和一个完整例句；
3. 尽量用上本单元的单词和短语，完整例句长度 {句长}；
4. 只用{学段}学生认识的词汇，中文翻译自然；
5. 不要重复已有的句型。`,

	PromptVariant: `你是{学段}英语老师。照着给定的例句做仿写和变式练习，帮助学生巩固运用。要求：
1. 每个变式必须保留原句的句型结构，只按指定方式改动；
2. 替换内容时优先使用给定的词汇（本单元或学生的目标词汇）；
3. 长度不超过 {单句上限}，只用{学段}学生认识的词汇；
4. 每个变式写明改了什么，并配中文翻译；
5. 同一个例句的几个变式之间不要雷同。`,
}

// OutputFormats 输出格式（系统消息）：程序按这些字段解析回复，不对用户开放。四种生成统一逐句输出（spec 0005 §3）。
// 注意：各格式里的关键字（例句 / 重点句型 / 仿写 / 短文）也被契约测试的假 AI 用来区分生成种类。
var OutputFormats = map[PromptKey]string{
	PromptExample: `按用户消息里的要求为给定单词写例句。输出格式（必须遵守）：
只输出一个 JSON 数组（所有单词放在同一个数组里，每个单词一项）：[{"spelling":"给定单词","en":"英文例句","cn":"中文翻译"}]。不要输出任何其他文字。`,

	PromptPassage: `按用户消息里的要求用目标单词写短文。输出格式（必须遵守）：
只输出 JSON：{"title":"英文标题","titleCn":"中文标题","sentences":[{"en":"一句英文","cn":"这一句的中文翻译","paragraph":0}],"questions":[{"q":"问题","a":"答案"}]}。
短文逐句写进 sentences，按顺序排列，每项只放一句；paragraph 是段落序号（从 0 开始，同一段的句子相同）。不要输出其他文字。`,

	PromptPattern: `按用户消息里的要求写本单元的重点句型。输出格式（必须遵守）：
只输出一个 JSON 数组，每个句型一项：[{"en":"完整的英文例句","cn":"中文翻译","frame":"句型骨架，用 ... 表示可替换部分"}]。
没有固定结构的句型省略 frame。不要输出任何其他文字。`,

	PromptVariant: `按用户消息里的要求做仿写和变式练习。输出格式（必须遵守）：
只输出一个 JSON 数组，每个变式一项：[{"origin":1,"en":"英文变式","cn":"中文翻译","change":"替换","note":"改了什么"}]。
origin 是用户消息里原句的序号（从 1 开始）；change 只能是 替换、转换、扩展、迁移 之一。不要输出任何其他文字。`,
}

// RewriteOutputFormat 重写一句的输出格式（系统消息）。
const RewriteOutputFormat = `按用户消息里的要求重写这一句。输出格式（必须遵守）：
只输出 JSON：{"en":"重写后的英文句子","cn":"中文翻译"}。不要输出其他文字。`

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
	Pattern PromptItem `json:"pattern"`
	Variant PromptItem `json:"variant"`
}

// ToPromptsView 构造视图。
func ToPromptsView(stored StoredPromptTemplates) PromptsView {
	item := func(key PromptKey) PromptItem {
		r := ResolveTemplate(key, stored)
		return PromptItem{Key: key, Current: r.Text, IsDefault: !r.Custom, DefaultText: DefaultPromptTemplates[key]}
	}
	return PromptsView{Example: item(PromptExample), Passage: item(PromptPassage), Pattern: item(PromptPattern), Variant: item(PromptVariant)}
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

func writeWordList(b *strings.Builder, words []PromptWord) {
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
}

// PatternInput 句型生成的输入。
type PatternInput struct {
	UnitName string
	Topic    string
	Count    int
	Words    []PromptWord // 本单元的单词和短语
	Existing []string     // 本单元已有的句型（英文），让 AI 不要重复
}

// BuildPatternPrompt 句型的可见提示词：要求模板（已替换占位符）+ 单元、话题、数量 + 本单元词汇 + 已有句型。
func BuildPatternPrompt(template string, in PatternInput) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(template))
	b.WriteString("\n\n单元：")
	b.WriteString(in.UnitName)
	b.WriteString("\n话题或语法点：")
	b.WriteString(strings.TrimSpace(in.Topic))
	b.WriteString("\n请写 ")
	b.WriteString(strconv.Itoa(in.Count))
	b.WriteString(" 个重点句型。")
	if len(in.Words) > 0 {
		b.WriteString("\n\n本单元的单词和短语（")
		b.WriteString(strconv.Itoa(len(in.Words)))
		b.WriteString(" 个）：")
		writeWordList(&b, in.Words)
	}
	if len(in.Existing) > 0 {
		b.WriteString("\n\n本单元已有的句型（不要重复）：")
		for i, s := range in.Existing {
			b.WriteString("\n")
			b.WriteString(strconv.Itoa(i + 1))
			b.WriteString(". ")
			b.WriteString(s)
		}
	}
	return b.String()
}

// VariantInput 仿写生成的输入。
type VariantInput struct {
	Origins []PastedOrigin // 例句（中文可空，由 AI 补上）
	Modes   []string       // 改造方式（ModeReplace 等）
	PerItem int            // 每个例句几个变式
	Words   []PromptWord   // 替换用的词汇
}

// BuildVariantPrompt 仿写的可见提示词：要求模板（已替换占位符）+ 改造方式 + 例句（带序号）+ 替换用的词汇。
func BuildVariantPrompt(template string, in VariantInput) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(template))
	b.WriteString("\n\n改造方式：")
	for i, m := range in.Modes {
		if i > 0 {
			b.WriteString("、")
		}
		b.WriteString(VariantModeLabel[m])
		b.WriteString("（")
		b.WriteString(variantModeHint[m])
		b.WriteString("）")
	}
	b.WriteString("\n每个例句写 ")
	b.WriteString(strconv.Itoa(in.PerItem))
	b.WriteString(" 个变式。")
	b.WriteString("\n\n例句（")
	b.WriteString(strconv.Itoa(len(in.Origins)))
	b.WriteString(" 句）：")
	for i, o := range in.Origins {
		b.WriteString("\n")
		b.WriteString(strconv.Itoa(i + 1))
		b.WriteString(". ")
		b.WriteString(o.En)
		b.WriteString(" | ")
		if o.Cn != "" {
			b.WriteString(o.Cn)
		} else {
			b.WriteString("（中文请补上）")
		}
	}
	if len(in.Words) > 0 {
		b.WriteString("\n\n替换时优先使用的词汇（")
		b.WriteString(strconv.Itoa(len(in.Words)))
		b.WriteString(" 个）：")
		writeWordList(&b, in.Words)
	}
	return b.String()
}

// RewriteInput 重写一句的输入。
type RewriteInput struct {
	Kind   PromptKey // 这一句属于哪种生成
	Level  Level
	En     string
	Cn     string
	Issues []string // 检查标出的问题
	Target string   // 例句：必须用上的目标词（可空）
	Origin string   // 仿写：原句（可空）
}

// BuildRewritePrompt 重写一句的用户消息：把这句和标出的问题一起发给 AI，只替换这一句（spec 0005 §4）。
func BuildRewritePrompt(in RewriteInput) string {
	p := ParamsOf(in.Level)
	var b strings.Builder
	b.WriteString("你是")
	b.WriteString(p.Name)
	b.WriteString("英语教材编辑。请重写下面这一句（")
	b.WriteString(PromptLabel[in.Kind])
	b.WriteString("），解决标出的问题，其余尽量保持不变。要求：只用")
	b.WriteString(p.Name)
	b.WriteString("学生认识的词汇，长度不超过 ")
	b.WriteString(strconv.Itoa(p.MaxWords))
	b.WriteString(" 词，语法正确，配自然的中文翻译。")
	if in.Target != "" {
		b.WriteString("\n必须用上：")
		b.WriteString(in.Target)
	}
	if in.Origin != "" {
		b.WriteString("\n仿写的原句：")
		b.WriteString(in.Origin)
		b.WriteString("（保留它的句型结构）")
	}
	b.WriteString("\n\n原句：")
	b.WriteString(in.En)
	if in.Cn != "" {
		b.WriteString("\n中文：")
		b.WriteString(in.Cn)
	}
	b.WriteString("\n\n问题：")
	if len(in.Issues) == 0 {
		b.WriteString("\n（没有标出问题，请换一种更自然的写法）")
	}
	for _, s := range in.Issues {
		b.WriteString("\n- ")
		b.WriteString(s)
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
