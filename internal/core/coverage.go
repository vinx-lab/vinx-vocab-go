package core

import (
	"sort"
	"time"
)

// 目标词书覆盖进度（spec 0003）：每个目标词的覆盖状态由正式测试作答与记忆状态现算，不落库。
//
//   - 正式测试作答：检测（test）与单词单测试（sheet）已交卷的组里，test 阶段的第一次作答（attempt = 1）；
//     同一计划 / 同一张单子再次交卷的组是重测，只算练习（K20 / K29）。
//   - 一个组里同一个词有多个题型时，任一题型答错即算这次答错（与结算的 wrongWordIds 同口径）。
//   - 未测：从来没有正式测试作答；要学：最近一次答错；会了：最近一次答对，或最近一次答错但之后
//     记忆达到「已掌握」（MasteryLevel = mastered，与「我的单词」同一判定，且最近复习晚于那次答错）。

// CoverageStatus 覆盖状态：untested | learning | known。
type CoverageStatus = string

const (
	CoverageUntested CoverageStatus = "untested"
	CoverageLearning CoverageStatus = "learning"
	CoverageKnown    CoverageStatus = "known"
)

// CoverageStatuses 全部覆盖状态（接口校验用）。
var CoverageStatuses = []string{CoverageUntested, CoverageLearning, CoverageKnown}

// TestSessionRef 一个已交卷的学习组（判定重测用）。
type TestSessionRef struct {
	ID   string
	Kind string
	// GroupKey 检测为 planId、单词单为 sheetId；nil 表示计划 / 单子已删除，无法判断是否重测，按正式算。
	GroupKey    *string
	CompletedAt time.Time
}

// FormalTestSessions 正式交卷的组：检测类组里，同一类型同一 GroupKey 最先交卷的那个（按 CompletedAt，
// 同时按输入顺序）；GroupKey 为 nil 的各自算正式。非检测类组不在结果里。
func FormalTestSessions(sessions []TestSessionRef) map[string]bool {
	list := make([]TestSessionRef, 0, len(sessions))
	for _, s := range sessions {
		if IsTestKind(s.Kind) {
			list = append(list, s)
		}
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].CompletedAt.Before(list[j].CompletedAt) })
	out := map[string]bool{}
	seen := map[string]bool{}
	for _, s := range list {
		if s.GroupKey == nil {
			out[s.ID] = true
			continue
		}
		key := s.Kind + "\x00" + *s.GroupKey
		if seen[key] {
			continue
		}
		seen[key] = true
		out[s.ID] = true
	}
	return out
}

// CoverageAnswer 某个词的一次作答。
type CoverageAnswer struct {
	SessionID string
	Kind      string // 学习组类型
	Phase     string
	Attempt   int
	Correct   bool
	// Retake 所在组是重测（见 FormalTestSessions）。
	Retake bool
	// At 这次作答计入的时间（取所在组的交卷时间）。
	At time.Time
}

// CoverageMemory 某个词的记忆状态（没学过为 nil）。
type CoverageMemory struct {
	Stability  float64
	LastReview *time.Time
}

// isFormal 是否正式测试作答。
func (a CoverageAnswer) isFormal() bool {
	return IsTestKind(a.Kind) && a.Phase == "test" && a.Attempt == 1 && !a.Retake
}

// WordCoverage 一个词的覆盖状态。
func WordCoverage(answers []CoverageAnswer, memory *CoverageMemory) CoverageStatus {
	type result struct {
		at      time.Time
		order   int
		correct bool
	}
	bySession := map[string]*result{}
	for i, a := range answers {
		if !a.isFormal() {
			continue
		}
		r := bySession[a.SessionID]
		if r == nil {
			r = &result{at: a.At, order: i, correct: true}
			bySession[a.SessionID] = r
		}
		if a.At.After(r.at) {
			r.at = a.At
		}
		if i > r.order {
			r.order = i
		}
		if !a.Correct {
			r.correct = false
		}
	}
	var last *result
	for _, r := range bySession {
		if last == nil || r.at.After(last.at) || (r.at.Equal(last.at) && r.order > last.order) {
			last = r
		}
	}
	switch {
	case last == nil:
		return CoverageUntested
	case last.correct:
		return CoverageKnown
	case memory != nil && MasteryLevel(memory.Stability) == "mastered" && memory.LastReview != nil && memory.LastReview.After(last.at):
		return CoverageKnown
	default:
		return CoverageLearning
	}
}

// CoverageCounts 一组目标词的数字：Tested = Known + Learning，Target = Tested + Untested。
type CoverageCounts struct {
	Target   int `json:"target"`
	Tested   int `json:"tested"`
	Known    int `json:"known"`
	Learning int `json:"learning"`
	Untested int `json:"untested"`
}

func (c *CoverageCounts) add(s CoverageStatus) {
	c.Target++
	switch s {
	case CoverageKnown:
		c.Known++
		c.Tested++
	case CoverageLearning:
		c.Learning++
		c.Tested++
	default:
		c.Untested++
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
// status 里没有的词算未测。
func SummarizeCoverage(status map[string]CoverageStatus, books []CoverageBook) (CoverageCounts, []CoverageBookCounts) {
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
			st := status[w]
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
