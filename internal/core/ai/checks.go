package ai

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"unicode"

	"github.com/vinx-lab/vinx-vocab-go/internal/core/lemma"
)

// 生成后的自动检查（spec 0005 §4）：目标词有没有用上、超纲词、长度超过学段的单句上限、仿写的结构偏离。
// 只做提示，不拦截。分词与关联用 0004 的 lemma 匹配器（由 service 装配），这里只放纯规则。

// StructureThreshold 结构相似度低于它就标黄（先这样定，演练后再调）。
const StructureThreshold = 0.6

// ---------------------------------------------------------------------------
// 结构相似度
// ---------------------------------------------------------------------------

var (
	posParenRe = regexp.MustCompile(`[（(][^)）]*[)）]`)
	contentPOS = map[string]bool{"n": true, "v": true, "vt": true, "vi": true, "adj": true, "a": true, "adv": true, "ad": true}
)

// IsContentPOS 词性是否实词（n. / v. / adj. / adv.，含 vt. vi. a. ad. 等写法）。词条可能列了多个词性
// （如 in: prep. … ad.），只看第一个，免得介词、代词因为兼有副词、形容词用法被当成实词。
func IsContentPOS(pos string) bool {
	pos = strings.TrimSpace(posParenRe.ReplaceAllString(pos, " "))
	fields := strings.Fields(pos)
	if len(fields) == 0 {
		return false
	}
	parts := strings.FieldsFunc(strings.ToLower(fields[0]), func(r rune) bool { return r == '/' || r == '&' || r == ',' || r == '，' })
	if len(parts) == 0 {
		return false
	}
	return contentPOS[strings.TrimRight(parts[0], ".")]
}

// StructWord 句中一个词在结构比较里的分类依据。
type StructWord struct {
	Norm     string // 小写、撇号统一
	Matched  bool   // 关联到了词库
	InPhrase bool   // 被短语覆盖（短语覆盖到的词算实词）
	POS      string // 关联到的词条的词性
}

// StructWordsOf 把 lemma 的分析结果转成 StructWord。entry 返回词条的词性，以及这个词条是否短语
// （短语即使只有一个字面词、只覆盖一个位置，也算实词）。
func StructWordsOf(a lemma.Analysis, entry func(wordID string) (pos string, phrase bool)) []StructWord {
	out := make([]StructWord, len(a.Tokens))
	for i, t := range a.Tokens {
		out[i] = StructWord{Norm: t.Norm}
	}
	for _, m := range a.Matches {
		pos, phrase := entry(m.WordID)
		if len(m.Covered) > 1 {
			phrase = true
		}
		for _, c := range m.Covered {
			if c < 0 || c >= len(out) {
				continue
			}
			out[c].Matched = true
			out[c].InPhrase = phrase
			out[c].POS = pos
		}
	}
	return out
}

// be / do / have 的各种形式与人称代词各归一类（肯定 → 疑问、换主语这类正常变式不应被判为偏离）。
var (
	classBe   = "<be>"
	classDo   = "<do>"
	classHave = "<have>"
	classPron = "<pron>"
	wordClass = map[string][]string{}
)

func init() {
	add := func(class string, words ...string) {
		for _, w := range words {
			wordClass[w] = []string{class}
		}
	}
	add(classBe, "be", "am", "is", "are", "was", "were", "been", "being", "isn't", "aren't", "wasn't", "weren't", "ain't")
	add(classDo, "do", "does", "did", "done", "doing", "don't", "doesn't", "didn't")
	add(classHave, "have", "has", "had", "having", "haven't", "hasn't", "hadn't")
	add(classPron, "i", "me", "my", "mine", "myself", "you", "your", "yours", "yourself", "yourselves",
		"he", "him", "his", "himself", "she", "her", "hers", "herself", "it", "its", "itself",
		"we", "us", "our", "ours", "ourselves", "they", "them", "their", "theirs", "themselves")
	// 代词与 be / have 的缩写
	for _, p := range []string{"i", "you", "he", "she", "it", "we", "they"} {
		wordClass[p+"'m"] = []string{classPron, classBe}
		wordClass[p+"'re"] = []string{classPron, classBe}
		wordClass[p+"'s"] = []string{classPron, classBe}
		wordClass[p+"'ve"] = []string{classPron, classHave}
		wordClass[p+"'ll"] = []string{classPron, "will"}
		wordClass[p+"'d"] = []string{classPron, "would"}
	}
}

// FunctionSequence 去掉实词后剩下的虚词序列：短语覆盖到的词、关联到实词词条的词、词库外的词都去掉；
// be / do / have 的各种形式与人称代词（不在短语里时）先归类再保留。
// 词条没有填词性时按实词处理：老师自建的词书常不填词性，而词表里的词绝大多数是实词；虚词（介词、
// 连词、冠词、代词）在内置的中考词表里都带词性。
func FunctionSequence(words []StructWord) []string {
	out := []string{}
	for _, w := range words {
		if w.InPhrase {
			continue
		}
		if cls, ok := wordClass[w.Norm]; ok {
			out = append(out, cls...)
			continue
		}
		if !w.Matched || strings.TrimSpace(w.POS) == "" || IsContentPOS(w.POS) {
			continue
		}
		out = append(out, w.Norm)
	}
	return out
}

// StructureSimilarity 虚词序列的最长公共子序列长度 ÷ 原句虚词数。原句没有虚词时无法判断，按 1（不标）。
func StructureSimilarity(origin, variant []string) float64 {
	if len(origin) == 0 {
		return 1
	}
	prev := make([]int, len(variant)+1)
	cur := make([]int, len(variant)+1)
	for i := 1; i <= len(origin); i++ {
		for j := 1; j <= len(variant); j++ {
			if origin[i-1] == variant[j-1] {
				cur[j] = prev[j-1] + 1
			} else {
				cur[j] = max(prev[j], cur[j-1])
			}
		}
		prev, cur = cur, prev
	}
	return float64(prev[len(variant)]) / float64(len(origin))
}

// ---------------------------------------------------------------------------
// 一句的检查结果
// ---------------------------------------------------------------------------

func hasLetter(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

// CountWords 句子的词数（含字母的词；纯数字不算）。
func CountWords(tokens []lemma.Token) int {
	n := 0
	for _, t := range tokens {
		if hasLetter(t.Text) {
			n++
		}
	}
	return n
}

// TargetUse 一个目标词有没有用上。
type TargetUse struct {
	Spelling string
	Used     bool
}

// CheckInput 一句话的检查输入（由 service 分词、关联后填好）。
type CheckInput struct {
	Words      int         // 词数
	MaxWords   int         // 学段的单句上限
	Targets    []TargetUse // 例句、短文：要用上的目标词（其他生成为空）
	OutOfScope []string    // 超纲词（关联到词库、但不在已知词集合里的，写法取句中的形式）
	Similarity *float64    // 仿写：和原句的结构相似度（其他生成为 nil）
}

// SentenceChecks 一句话的检查结果，附在草稿 / 任务结果上。Error（标红）= 目标词没用上；
// Warning（标黄）= 超纲、太长、结构偏离。Issues 是给人看、也给「重写这一句」用的问题说明。
type SentenceChecks struct {
	Words             int      `json:"words"`
	MaxWords          int      `json:"maxWords"`
	MissingTargets    []string `json:"missingTargets"`
	OutOfScope        []string `json:"outOfScope"`
	TooLong           bool     `json:"tooLong"`
	Similarity        *float64 `json:"similarity"`
	StructureDeviates bool     `json:"structureDeviates"`
	Warning           bool     `json:"warning"`
	Error             bool     `json:"error"`
	Issues            []string `json:"issues"`
}

// BuildChecks 汇总一句话的检查结果。
func BuildChecks(in CheckInput) SentenceChecks {
	c := SentenceChecks{Words: in.Words, MaxWords: in.MaxWords, MissingTargets: []string{}, OutOfScope: []string{}, Issues: []string{}}
	for _, t := range in.Targets {
		if !t.Used {
			c.MissingTargets = append(c.MissingTargets, t.Spelling)
		}
	}
	if len(c.MissingTargets) > 0 {
		c.Error = true
		c.Issues = append(c.Issues, "没有用上目标词："+strings.Join(c.MissingTargets, "、"))
	}
	c.OutOfScope = append(c.OutOfScope, in.OutOfScope...)
	if len(c.OutOfScope) > 0 {
		c.Warning = true
		c.Issues = append(c.Issues, "超纲词："+strings.Join(c.OutOfScope, "、"))
	}
	if in.MaxWords > 0 && in.Words > in.MaxWords {
		c.TooLong, c.Warning = true, true
		c.Issues = append(c.Issues, fmt.Sprintf("句子太长：%d 词，上限 %d 词", in.Words, in.MaxWords))
	}
	if in.Similarity != nil {
		s := math.Round(*in.Similarity*100) / 100
		c.Similarity = &s
		if *in.Similarity < StructureThreshold {
			c.StructureDeviates, c.Warning = true, true
			c.Issues = append(c.Issues, fmt.Sprintf("和原句的句型结构偏离（相似度 %.2f）", s))
		}
	}
	return c
}
