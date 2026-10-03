package ai

import (
	"strconv"
	"strings"
)

// 学段（spec 0005 §1）：决定生成内容的难度。挂在词书上（Book.level，可空）。

// Level 学段。
type Level = string

const (
	LevelPrimary Level = "primary" // 小学
	LevelJunior  Level = "junior"  // 初中
	LevelExam    Level = "exam"    // 中考冲刺
)

// Levels 合法取值，按难度从低到高。
var Levels = []Level{LevelPrimary, LevelJunior, LevelExam}

// DefaultLevel 没有学段可用时的默认值。
const DefaultLevel = LevelJunior

// LevelParams 各学段的默认参数，作为模板占位符的值。
type LevelParams struct {
	Name        string // {学段}
	SentenceLen string // {句长}
	PassageLen  string // {短文长度}
	MaxWords    int    // {单句上限}（词）
}

// LevelTable 各学段的默认参数（spec 0005 §1 的表）。
var LevelTable = map[Level]LevelParams{
	LevelPrimary: {Name: "小学", SentenceLen: "4～10 词", PassageLen: "50～80 词", MaxWords: 12},
	LevelJunior:  {Name: "初中", SentenceLen: "6～14 词", PassageLen: "80～140 词", MaxWords: 20},
	LevelExam:    {Name: "中考", SentenceLen: "8～20 词", PassageLen: "120～200 词", MaxWords: 25},
}

// IsLevel 是否合法学段。
func IsLevel(s string) bool {
	_, ok := LevelTable[s]
	return ok
}

// ParamsOf 学段参数；不认识的按默认学段。
func ParamsOf(level Level) LevelParams {
	if p, ok := LevelTable[level]; ok {
		return p
	}
	return LevelTable[DefaultLevel]
}

// LevelOr 合法时用 p，否则用 def。
func LevelOr(p *string, def Level) Level {
	if p != nil && IsLevel(*p) {
		return *p
	}
	return def
}

func levelRank(l Level) int {
	for i, x := range Levels {
		if x == l {
			return i
		}
	}
	return -1
}

// MaxLevel 一组词书学段里最高的（忽略空值和不认识的）；都没有时用默认学段。学生短文按目标词书推断学段用。
func MaxLevel(levels []*string) Level {
	best := -1
	for _, l := range levels {
		if l == nil {
			continue
		}
		if r := levelRank(*l); r > best {
			best = r
		}
	}
	if best < 0 {
		return DefaultLevel
	}
	return Levels[best]
}

// FillPlaceholders 把模板里的 {学段}、{句长}、{短文长度}、{单句上限} 换成学段的具体值；其他花括号原样保留。
func FillPlaceholders(template string, level Level) string {
	p := ParamsOf(level)
	return strings.NewReplacer(
		"{学段}", p.Name,
		"{句长}", p.SentenceLen,
		"{短文长度}", p.PassageLen,
		"{单句上限}", strconv.Itoa(p.MaxWords)+" 词",
	).Replace(template)
}

// bookNameLevels 内置词书（及从服务器版导入的同名词书）的书名 → 学段。
// 迁移 0004_book_level.sql 里的回填名单与此一致；从服务器版导入后由 service.RepairBookLevels 按此回填。
var bookNameLevels = map[string]Level{
	"七年级上册":  LevelJunior,
	"七年级下册":  LevelJunior,
	"八年级上册":  LevelJunior,
	"八年级下册":  LevelJunior,
	"九年级上册":  LevelJunior,
	"中考核心词汇": LevelExam,
	"中考差集":   LevelExam,
}

// LevelForBookName 按书名给内置词书定学段；不在名单里返回 nil。
func LevelForBookName(name string) *string {
	if l, ok := bookNameLevels[name]; ok {
		return &l
	}
	return nil
}
