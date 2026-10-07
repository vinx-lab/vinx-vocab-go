package core

import (
	"sort"
	"time"

	"github.com/vinx-lab/vinx-vocab-go/internal/core/fsrs"
)

// 作答证据与词状态（spec 0009）：结算、覆盖进度、学习记录、历史补算共用这里的定义。
//
//   - 证据：已完成的学习组里，一个词在练习阶段（检测类组为检测阶段）的第一次作答；多个题型全对才算对。
//   - 不算证据：巩固阶段与第二次及以后的作答、进行中的组、同一张单词单再次交卷的组（看过答案的原题），
//     以及同一个词同一学习日里完成时间不是最早的那一条（K19 同口径）。检测计划每次随机抽题，不再有重测。
//   - 状态：未接触 / 没记住 / 刚记住 / 巩固中 / 已掌握，另有「待复查」「可能忘了」两个标记。

// EvidenceSession 一个已完成的学习组。
type EvidenceSession struct {
	ID   string
	Kind string
	// SheetID 单词单测试的单子（判定同一张单子的重测）；单子已删除时为 nil，按正式算。
	SheetID *string
	// Day 完成的学习日；At 完成时间。
	Day string
	At  time.Time
	// SelfGraded 学生账号自批的默写单（spec 0006）。
	SelfGraded bool
}

// EvidenceAnswer 一次作答。
type EvidenceAnswer struct {
	SessionID string
	WordID    string
	Mode      string
	Phase     string
	Attempt   int
	Correct   bool
	HintUsed  bool
}

// Evidence 一个词的一条证据。
type Evidence struct {
	SessionID  string
	Kind       string
	Day        string
	At         time.Time
	Correct    bool
	SelfGraded bool
	// Attempts 这一组里这个词各题型的首次作答（推导评分用）。
	Attempts []FirstAttempt
}

// FirstPhase 某类学习组里算首次作答的阶段：检测类组是检测阶段，其余是练习阶段。
func FirstPhase(kind string) string {
	if IsTestKind(kind) {
		return "test"
	}
	return "practice"
}

// SheetRetakes 同一张单子再次交卷的单词单测试组（按完成时间，最早的那组是正式的，其余是重测）。
func SheetRetakes(sessions []EvidenceSession) map[string]bool {
	list := sortedSessions(sessions)
	out := map[string]bool{}
	seen := map[string]bool{}
	for _, s := range list {
		if s.Kind != "sheet" || s.SheetID == nil {
			continue
		}
		if seen[*s.SheetID] {
			out[s.ID] = true
			continue
		}
		seen[*s.SheetID] = true
	}
	return out
}

func sortedSessions(sessions []EvidenceSession) []EvidenceSession {
	list := append([]EvidenceSession(nil), sessions...)
	sort.SliceStable(list, func(i, j int) bool { return list[i].At.Before(list[j].At) })
	return list
}

// BuildEvidence 按上面的规则从已完成的组与作答整理出每个词的证据（wordId → 按完成时间升序）。
// 作答所在的组不在 sessions 里（进行中、已删除）的忽略。
func BuildEvidence(sessions []EvidenceSession, answers []EvidenceAnswer) map[string][]Evidence {
	retake := SheetRetakes(sessions)
	byID := map[string]EvidenceSession{}
	for _, s := range sessions {
		byID[s.ID] = s
	}
	// 每组每词的首次作答，保持作答顺序
	type key struct{ session, word string }
	attempts := map[key][]FirstAttempt{}
	wrong := map[key]bool{}
	wordsOf := map[string][]string{}
	for _, a := range answers {
		s, ok := byID[a.SessionID]
		if !ok || retake[s.ID] || a.Attempt != 1 || a.Phase != FirstPhase(s.Kind) {
			continue
		}
		k := key{a.SessionID, a.WordID}
		if _, seen := attempts[k]; !seen {
			wordsOf[a.SessionID] = append(wordsOf[a.SessionID], a.WordID)
		}
		attempts[k] = append(attempts[k], FirstAttempt{Mode: a.Mode, Correct: a.Correct, HintUsed: a.HintUsed})
		if !a.Correct {
			wrong[k] = true
		}
	}
	out := map[string][]Evidence{}
	taken := map[string]bool{} // wordId + 学习日
	for _, s := range sortedSessions(sessions) {
		for _, w := range wordsOf[s.ID] {
			dk := w + "\x00" + s.Day
			if taken[dk] {
				continue
			}
			taken[dk] = true
			k := key{s.ID, w}
			out[w] = append(out[w], Evidence{SessionID: s.ID, Kind: s.Kind, Day: s.Day, At: s.At, Correct: !wrong[k], SelfGraded: s.SelfGraded, Attempts: attempts[k]})
		}
	}
	return out
}

// 词状态。
const (
	StageUntested      = "untested"
	StageMissed        = "missed"
	StageFresh         = "fresh"
	StageConsolidating = "consolidating"
	StageMastered      = "mastered"
)

// StageLabel 状态中文名。
var StageLabel = map[string]string{
	StageUntested: "未接触", StageMissed: "没记住", StageFresh: "刚记住", StageConsolidating: "巩固中", StageMastered: "已掌握",
}

// StageMemory 判定状态用到的记忆字段（没学过为 nil）。
type StageMemory struct {
	Stability  float64
	Due        time.Time
	LastReview *time.Time
}

// WordStageInfo 一个词的状态与标记。
type WordStageInfo struct {
	Stage string
	// Due 待复查：已到期（due 早于今天结束）。Forgetting 可能忘了：回忆概率低于 ForgettingThreshold。
	// 只在有记忆、且不是「没记住」时可能为 true；Forgetting 为 true 时 Due 也为 true。
	Due        bool
	Forgetting bool
	// SelfGraded 决定状态的最近一条证据来自自批的默写单。
	SelfGraded bool
}

// ForgettingThreshold 「可能忘了」的回忆概率阈值。
const ForgettingThreshold = 0.7

// Retrievability 记忆在 now 时的回忆概率（没有复习时间时为 0）。
func Retrievability(m StageMemory, now time.Time) float64 {
	return memoryScheduler.Retrievability(fsrs.Card{Stability: m.Stability, LastReview: m.LastReview}, now)
}

// WordStage 词状态：最近一条证据答错为「没记住」；否则有记忆按稳定度分层（没有记忆、但最近一次答对为「刚记住」）；
// 既没有证据也没有记忆为「未接触」。ev 须按时间升序（BuildEvidence 的输出）。
func WordStage(ev []Evidence, mem *StageMemory, now, dayEnd time.Time) WordStageInfo {
	var last *Evidence
	if len(ev) > 0 {
		last = &ev[len(ev)-1]
	}
	out := WordStageInfo{}
	if last != nil {
		out.SelfGraded = last.SelfGraded
	}
	switch {
	case last == nil && mem == nil:
		out.Stage = StageUntested
		return out
	case last != nil && !last.Correct:
		out.Stage = StageMissed
		return out
	case mem == nil:
		out.Stage = StageFresh
		return out
	}
	switch MasteryLevel(mem.Stability) {
	case "mastered":
		out.Stage = StageMastered
	case "consolidating":
		out.Stage = StageConsolidating
	default:
		out.Stage = StageFresh
	}
	if mem.Due.Before(dayEnd) {
		out.Due = true
		out.Forgetting = mem.LastReview != nil && Retrievability(*mem, now) < ForgettingThreshold
	}
	return out
}

// StageLevel 学习记录里的分层键（历史命名：「刚记住」是 learning）。
func StageLevel(stage string) string {
	if stage == StageFresh {
		return "learning"
	}
	return stage
}

// CoverageStatus 覆盖进度的三档（接口兼容）：untested | learning（没记住）| known（刚记住、巩固中、已掌握）。
type CoverageStatus = string

const (
	CoverageUntested CoverageStatus = "untested"
	CoverageLearning CoverageStatus = "learning"
	CoverageKnown    CoverageStatus = "known"
)

// CoverageStatuses 全部覆盖状态（接口校验用）。
var CoverageStatuses = []string{CoverageUntested, CoverageLearning, CoverageKnown}

// CoverageWordFilters 目标词列表可用的筛选：三档覆盖状态，或五级状态里的另外四个（spec 0009）。
var CoverageWordFilters = []string{CoverageUntested, CoverageLearning, CoverageKnown, StageMissed, StageFresh, StageConsolidating, StageMastered}

// CoverageStatusOf 五级状态对应的三档。
func CoverageStatusOf(stage string) CoverageStatus {
	switch stage {
	case StageUntested, "":
		return CoverageUntested
	case StageMissed:
		return CoverageLearning
	default:
		return CoverageKnown
	}
}

// MemoryAction 结算（及补算）时对一个词的记忆做什么。
type MemoryAction int

const (
	MemoryNone MemoryAction = iota
	// MemoryCreateLearn 新学建卡（封顶到下个学习日）。
	MemoryCreateLearn
	// MemoryCreate 非新学组里第一次见就答对（评分「记得」）：建卡，不占每日计划的新词额度。
	MemoryCreate
	// MemoryUpdate 按评分更新已有记忆。
	MemoryUpdate
)

// MemoryActionFor 一个词这一组该怎么动记忆（调用方先保证这是该词当天的证据、且当天还没更新过记忆）：
// 新学组只给没有记忆的词建卡；其余组有记忆就更新，没有记忆时只有答对（Good）才建卡，答错留在新词池等新学。
func MemoryActionFor(kind string, hasMemory bool, rating Rating) MemoryAction {
	if rating == 0 {
		return MemoryNone
	}
	if kind == "learn" {
		if hasMemory {
			return MemoryNone
		}
		return MemoryCreateLearn
	}
	if hasMemory {
		return MemoryUpdate
	}
	if rating == RatingGood {
		return MemoryCreate
	}
	return MemoryNone
}

// CountSelfGraded 目标词（按书去重）里已接触、且决定状态的最近一条证据是自批的词数（班级概览「目标覆盖」旁的自批比例 = 它 / 已测）。
func CountSelfGraded(stages map[string]WordStageInfo, books []CoverageBook) int {
	n := 0
	for _, w := range CoverageWordOrder(books) {
		st := stages[w]
		if st.Stage != "" && st.Stage != StageUntested && st.SelfGraded {
			n++
		}
	}
	return n
}

// CoverageCounts 一组目标词的数字：Tested = Known + Learning，Target = Tested + Untested；
// Known = Fresh + Consolidating + Mastered；Learning 是「没记住」；Due 是带「待复查」标记的词数（spec 0009 新增）。
type CoverageCounts struct {
	Target        int `json:"target"`
	Tested        int `json:"tested"`
	Known         int `json:"known"`
	Learning      int `json:"learning"`
	Untested      int `json:"untested"`
	Fresh         int `json:"fresh"`
	Consolidating int `json:"consolidating"`
	Mastered      int `json:"mastered"`
	Due           int `json:"due"`
}

func (c *CoverageCounts) add(s WordStageInfo) {
	c.Target++
	switch s.Stage {
	case StageMissed:
		c.Learning++
		c.Tested++
	case StageFresh, StageConsolidating, StageMastered:
		c.Known++
		c.Tested++
		switch s.Stage {
		case StageFresh:
			c.Fresh++
		case StageConsolidating:
			c.Consolidating++
		default:
			c.Mastered++
		}
	default:
		c.Untested++
	}
	if s.Due {
		c.Due++
	}
}

// CoverageBook 一本目标词书的词（按书内顺序，可含重复）。
type CoverageBook struct {
	BookID  string
	Name    string
	WordIDs []string
}

// CoverageBookCounts 每本书的数字。
type CoverageBookCounts struct {
	BookID string `json:"bookId"`
	Name   string `json:"name"`
	CoverageCounts
}

// SummarizeCoverage 汇总每本书与目标合计：书内按 wordId 去重；合计跨书去重。
// stages 里没有的词算未接触。
func SummarizeCoverage(stages map[string]WordStageInfo, books []CoverageBook) (CoverageCounts, []CoverageBookCounts) {
	var total CoverageCounts
	per := make([]CoverageBookCounts, 0, len(books))
	all := map[string]bool{}
	for _, b := range books {
		row := CoverageBookCounts{BookID: b.BookID, Name: b.Name}
		seen := map[string]bool{}
		for _, w := range b.WordIDs {
			if seen[w] {
				continue
			}
			seen[w] = true
			st := stages[w]
			row.add(st)
			if !all[w] {
				all[w] = true
				total.add(st)
			}
		}
		per = append(per, row)
	}
	return total, per
}

// CoverageWordOrder 目标词按书的顺序、书内顺序展开并去重。
func CoverageWordOrder(books []CoverageBook) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, b := range books {
		for _, w := range b.WordIDs {
			if !seen[w] {
				seen[w] = true
				out = append(out, w)
			}
		}
	}
	return out
}
