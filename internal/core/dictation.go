package core

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// 默写单（spec 0006）：单词单的一种格式。题目是「看中文写英文」，纸面手写后由家长或老师批改录入。
// 本文件只放纯规则：题型、出题切分、句子状态、批改结果校验、卷面提示。

// 单词单格式。
const (
	SheetFormatSelftest  = "selftest"
	SheetFormatDictation = "dictation"
)

// SheetFormats 单词单格式全集（接口校验用）。
var SheetFormats = []string{SheetFormatSelftest, SheetFormatDictation}

// DictationMode 默写单批改写入 Answer 的 mode。
const DictationMode = "dictation"

// 默写单题型。
const (
	DictWord      = "word"
	DictPhrase    = "phrase"
	DictSentence  = "sentence"
	DictFrame     = "frame"
	DictTransform = "transform"
)

// DictationItemTypes 题型全集（顺序即卷面顺序）。
var DictationItemTypes = []string{DictWord, DictPhrase, DictSentence, DictFrame, DictTransform}

// 每份默写单各题型的默认数量与上限。
const (
	DictationWordDefault     = 20
	DictationPhraseDefault   = 10
	DictationSentenceDefault = 8
	DictationWordMax         = SheetPerMax
	DictationPhraseMax       = SheetPerMax
	DictationSentenceMax     = 20
)

// IsSentenceItem 句子类题（sentence / frame / transform），写入 SentenceAnswer，不进 FSRS。
func IsSentenceItem(t string) bool {
	return t == DictSentence || t == DictFrame || t == DictTransform
}

// DictationSection 卷面分区：1 单词、2 短语、3 句子、4 仿写与转换；未知题型为 0。
func DictationSection(t string) int {
	switch t {
	case DictWord:
		return 1
	case DictPhrase:
		return 2
	case DictSentence:
		return 3
	case DictFrame, DictTransform:
		return 4
	}
	return 0
}

// DictationItem 默写单的一道题（WordSheet.items 的一项）。
type DictationItem struct {
	Type       string `json:"type"`
	WordID     string `json:"wordId,omitempty"`
	SentenceID string `json:"sentenceId,omitempty"`
}

// key 去重键：同一个词（单词 / 短语）或同一个句子在一份单子里只出一次。
func (i DictationItem) key() string {
	if IsSentenceItem(i.Type) {
		return "s:" + i.SentenceID
	}
	return "w:" + i.WordID
}

// DedupeDictationItems 按顺序去重（同一个词或句子只保留第一次出现）。
func DedupeDictationItems(items []DictationItem) []DictationItem {
	seen := make(map[string]bool, len(items))
	out := make([]DictationItem, 0, len(items))
	for _, it := range items {
		k := it.key()
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, it)
	}
	return out
}

// DictationLimits 每份各题型的数量上限。
type DictationLimits struct {
	Words, Phrases, Sentences int
}

// splitEven 把 n 个元素按顺序连续切成 k 份，前面的份多一个。
func splitEven[T any](xs []T, k int) [][]T {
	out := make([][]T, k)
	start := 0
	for i := 0; i < k; i++ {
		size := len(xs) / k
		if i < len(xs)%k {
			size++
		}
		out[i] = xs[start : start+size]
		start += size
	}
	return out
}

// SplitDictation 把已按「从难到易」排好的题切成多份默写单：去重后单词、短语、句子三组各自按顺序
// 平均分到各份（第 1 份最难；前面的份多一题），每份内按卷面分区排序。份数不超过三组里最多的题数。
// 某组超过 份数 × 每份上限 时报错，不静默丢题。
func SplitDictation(items []DictationItem, copies int, lim DictationLimits) ([][]DictationItem, error) {
	items = DedupeDictationItems(items)
	var words, phrases, sentences []DictationItem
	for _, it := range items {
		switch {
		case it.Type == DictWord:
			words = append(words, it)
		case it.Type == DictPhrase:
			phrases = append(phrases, it)
		case IsSentenceItem(it.Type):
			sentences = append(sentences, it)
		default:
			return nil, fmt.Errorf("未知题型 %s", it.Type)
		}
	}
	check := func(label string, n, per int) error {
		if n > copies*per {
			return fmt.Errorf("%s %d 题，超过 %d 份 × %d 题", label, n, copies, per)
		}
		return nil
	}
	if err := check("单词", len(words), lim.Words); err != nil {
		return nil, err
	}
	if err := check("短语", len(phrases), lim.Phrases); err != nil {
		return nil, err
	}
	if err := check("句子", len(sentences), lim.Sentences); err != nil {
		return nil, err
	}
	k := min(copies, max(len(words), len(phrases), len(sentences)))
	if k == 0 {
		return [][]DictationItem{}, nil
	}
	ws, ps, ss := splitEven(words, k), splitEven(phrases, k), splitEven(sentences, k)
	out := make([][]DictationItem, k)
	for i := 0; i < k; i++ {
		sheet := make([]DictationItem, 0, len(ws[i])+len(ps[i])+len(ss[i]))
		sheet = append(sheet, ws[i]...)
		sheet = append(sheet, ps[i]...)
		sheet = append(sheet, ss[i]...)
		sort.SliceStable(sheet, func(a, b int) bool { return DictationSection(sheet[a].Type) < DictationSection(sheet[b].Type) })
		out[i] = sheet
	}
	return out, nil
}

// ------------------------------------------------------------------
// 句子状态
// ------------------------------------------------------------------

// SentenceAnswerFact 一次句子作答（SentenceAnswer 的一行）。
type SentenceAnswerFact struct {
	Correct bool
	At      time.Time
}

// SentenceStatus 句子的状态：没有作答为未测（untested），最近一次错为要学（learning），最近一次对为会了（known）。
// 同一时刻的多条以切片里靠后的为准。句子不进 FSRS。
func SentenceStatus(answers []SentenceAnswerFact) CoverageStatus {
	if len(answers) == 0 {
		return CoverageUntested
	}
	last := answers[0]
	for _, a := range answers[1:] {
		if !a.At.Before(last.At) {
			last = a
		}
	}
	if last.Correct {
		return CoverageKnown
	}
	return CoverageLearning
}

// SentenceCandidate 出题时的一个候选句子。
type SentenceCandidate struct {
	ID     string
	Status CoverageStatus
}

// OrderDictationSentences 句子出题顺序：要学的优先，其次没测过的，最后会了的；同一档内保持来源顺序；去重。
func OrderDictationSentences(cands []SentenceCandidate) []string {
	rank := map[CoverageStatus]int{CoverageLearning: 0, CoverageUntested: 1, CoverageKnown: 2}
	seen := map[string]bool{}
	list := make([]SentenceCandidate, 0, len(cands))
	for _, c := range cands {
		if seen[c.ID] {
			continue
		}
		seen[c.ID] = true
		list = append(list, c)
	}
	sort.SliceStable(list, func(i, j int) bool { return rank[list[i].Status] < rank[list[j].Status] })
	out := make([]string, len(list))
	for i, c := range list {
		out[i] = c.ID
	}
	return out
}

// ------------------------------------------------------------------
// 句子题型
// ------------------------------------------------------------------

// variantTransformLabel 仿写句 variantNote 里「转换」的中文名（与 ai.VariantModeLabel 一致）。
const variantTransformLabel = "转换"

// IsTransformVariant 改动方式为「转换」的仿写句：source = variant、有原句、variantNote 以「转换」开头。
func IsTransformVariant(source string, variantNote, originID *string) bool {
	return source == "variant" && originID != nil && *originID != "" && variantNote != nil &&
		strings.HasPrefix(strings.TrimSpace(*variantNote), variantTransformLabel)
}

// DictationSentenceType 句子默认出成的题型：转换仿写句 → transform；带骨架 → frame；其余 → sentence。
func DictationSentenceType(source string, frame, variantNote, originID *string) string {
	if IsTransformVariant(source, variantNote, originID) {
		return DictTransform
	}
	if frame != nil && strings.TrimSpace(*frame) != "" {
		return DictFrame
	}
	return DictSentence
}

// DictationSentenceTypeAllowed 句子能否出成题型 t：任何句子都能出成 sentence；frame 要有骨架；transform 要是转换仿写句。
func DictationSentenceTypeAllowed(t, source string, frame, variantNote, originID *string) bool {
	switch t {
	case DictSentence:
		return true
	case DictFrame:
		return frame != nil && strings.TrimSpace(*frame) != ""
	case DictTransform:
		return IsTransformVariant(source, variantNote, originID)
	}
	return false
}

// TransformRequirement 转换题的要求：variantNote「转换：改成一般疑问句」→「改成一般疑问句」；只有「转换」时给通用要求。
func TransformRequirement(variantNote *string) string {
	if variantNote != nil {
		s := strings.TrimSpace(*variantNote)
		s = strings.TrimSpace(strings.TrimPrefix(s, variantTransformLabel))
		s = strings.TrimSpace(strings.TrimLeft(s, "：:"))
		if s != "" {
			return s
		}
	}
	return "按要求转换句式"
}

// FrameBlank 骨架里的可替换部分（「...」「…」）换成书写横线「___」。
func FrameBlank(frame string) string {
	s := strings.ReplaceAll(frame, "...", "___")
	return strings.ReplaceAll(s, "…", "___")
}

// DictationPromptInput 生成卷面提示需要的字段。
type DictationPromptInput struct {
	Type         string
	PartOfSpeech *string // 单词
	Definition   string  // 单词 / 短语
	Cn           string  // 句子中文
	Frame        *string // 仿写骨架
	OriginEn     string  // 转换题的原句
	VariantNote  *string // 转换题的要求
}

// DictationPrompt 卷面左侧的中文提示（spec 0006 第 2 节的表）：
// 单词「n. 句子」；短语「大声朗读」；句子为中文句子；仿写「I find ___ useful.（我发现做笔记很有用）」；
// 转换「原句（改成一般疑问句）」。
func DictationPrompt(in DictationPromptInput) string {
	switch in.Type {
	case DictWord:
		if in.PartOfSpeech != nil && strings.TrimSpace(*in.PartOfSpeech) != "" {
			return strings.TrimSpace(*in.PartOfSpeech) + " " + in.Definition
		}
		return in.Definition
	case DictPhrase:
		return in.Definition
	case DictFrame:
		frame := ""
		if in.Frame != nil {
			frame = FrameBlank(strings.TrimSpace(*in.Frame))
		}
		if in.Cn == "" {
			return frame
		}
		return frame + "（" + in.Cn + "）"
	case DictTransform:
		return in.OriginEn + "（" + TransformRequirement(in.VariantNote) + "）"
	default:
		return in.Cn
	}
}

// ------------------------------------------------------------------
// 批改
// ------------------------------------------------------------------

// GradeResult 一道题的批改结果；Index 是题目在 items 里的下标（从 0 开始）。
type GradeResult struct {
	Index      int
	Correct    bool
	UserAnswer *string
}

// ValidateGradeResults 批改结果必须恰好覆盖要批改的全部题目（indexes：题目下标，每个一次；
// 词或句子已被删除的题不在其中）；返回按下标排好的结果。
func ValidateGradeResults(indexes []int, results []GradeResult) ([]GradeResult, error) {
	want := make(map[int]bool, len(indexes))
	for _, i := range indexes {
		want[i] = true
	}
	seen := make(map[int]bool, len(results))
	for _, r := range results {
		if !want[r.Index] {
			return nil, fmt.Errorf("题号 %d 超出范围", r.Index)
		}
		if seen[r.Index] {
			return nil, fmt.Errorf("题号 %d 重复", r.Index)
		}
		seen[r.Index] = true
	}
	if len(results) != len(want) {
		return nil, fmt.Errorf("需要批改全部 %d 道题", len(want))
	}
	out := append([]GradeResult{}, results...)
	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })
	return out, nil
}

// IsSelfGraded 学生本人的账号提交的批改标记为「自批」（家长通常用孩子的账号）。
func IsSelfGraded(graderID, studentID string) bool {
	return graderID == studentID
}
