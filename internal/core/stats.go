package core

import "sort"

// 学习统计口径（纯函数，对应旧 lib/stats.ts），设计见 docs/product.md §6。

// MaxAnswerMs 单题计时上限（统计时长用）。
const MaxAnswerMs = 60_000

// AnswerFact 统计用的作答事实。
type AnswerFact struct {
	WordID     string
	Mode       string
	Phase      string
	Attempt    int
	Correct    bool
	DurationMs int
	DayKey     string
	SessionID  string
}

// IsFirstAttempt 是否“首次作答”：练习 / 检测阶段第 1 次（巩固阶段与重试不计入正确率）。
func IsFirstAttempt(phase string, attempt int) bool {
	return (phase == "practice" || phase == "test") && attempt == 1
}

// Accuracy 首答正确率；无数据时 Rate 为 nil（JSON null）。
type Accuracy struct {
	Correct int      `json:"correct"`
	Total   int      `json:"total"`
	Rate    *float64 `json:"rate"`
}

// AccuracyOf 旧 accuracy()。
func AccuracyOf(answers []AnswerFact) Accuracy {
	r := Accuracy{}
	for _, a := range answers {
		if !IsFirstAttempt(a.Phase, a.Attempt) {
			continue
		}
		r.Total++
		if a.Correct {
			r.Correct++
		}
	}
	if r.Total > 0 {
		v := float64(r.Correct) / float64(r.Total)
		r.Rate = &v
	}
	return r
}

func cappedMs(ms int) int {
	if ms < 0 {
		ms = 0
	}
	if ms > MaxAnswerMs {
		ms = MaxAnswerMs
	}
	return ms
}

// roundMinutes Math.round(ms / 60000)。
func roundMinutes(ms int) int { return int(jsRoundF(float64(ms) / 60_000)) }

// ActiveMinutes 主动学习时长（分钟，单题上限 60 秒）。
func ActiveMinutes(answers []AnswerFact) int {
	ms := 0
	for _, a := range answers {
		ms += cappedMs(a.DurationMs)
	}
	return roundMinutes(ms)
}

// DailyPoint 按学习日汇总的一天。
type DailyPoint struct {
	Day           string `json:"day"`
	NewWords      int    `json:"newWords"`
	ReviewedWords int    `json:"reviewedWords"`
	Answers       int    `json:"answers"`
	Correct       int    `json:"correct"`
	FirstAttempts int    `json:"firstAttempts"`
	Minutes       int    `json:"minutes"`
}

// DailySeries 按学习日汇总：新学词数（按 introducedDay）、复习词数（非新学的 ReviewLog）、题数、首答正确、时长。
func DailySeries(today string, days int, answers []AnswerFact, introducedDays, reviewDays []string) []DailyPoint {
	keys := LastNDays(today, days)
	idx := make(map[string]int, len(keys))
	out := make([]DailyPoint, len(keys))
	for i, k := range keys {
		idx[k] = i
		out[i] = DailyPoint{Day: k}
	}
	ms := map[string]int{}
	for _, a := range answers {
		i, ok := idx[a.DayKey]
		if !ok {
			continue
		}
		p := &out[i]
		p.Answers++
		if IsFirstAttempt(a.Phase, a.Attempt) {
			p.FirstAttempts++
			if a.Correct {
				p.Correct++
			}
		}
		ms[a.DayKey] += cappedMs(a.DurationMs)
	}
	for _, d := range introducedDays {
		if i, ok := idx[d]; ok {
			out[i].NewWords++
		}
	}
	for _, d := range reviewDays {
		if i, ok := idx[d]; ok {
			out[i].ReviewedWords++
		}
	}
	for day, v := range ms {
		out[idx[day]].Minutes = roundMinutes(v)
	}
	return out
}

// MasteryDist 记忆分层分布。
type MasteryDist struct {
	Learning      int `json:"learning"`
	Consolidating int `json:"consolidating"`
	Mastered      int `json:"mastered"`
}

// MasteryDistribution 旧 masteryDistribution()。
func MasteryDistribution(stabilities []float64) MasteryDist {
	var r MasteryDist
	for _, s := range stabilities {
		switch MasteryLevel(s) {
		case "mastered":
			r.Mastered++
		case "consolidating":
			r.Consolidating++
		default:
			r.Learning++
		}
	}
	return r
}

// HardWord 难词统计。
type HardWord struct {
	WordID string  `json:"wordId"`
	Wrong  int     `json:"wrong"`
	Total  int     `json:"total"`
	Rate   float64 `json:"rate"`
}

// HardWords 难词：按首答错误次数降序（至少错 1 次），并给出错误率；limit ≤ 0 时取 10。
func HardWords(answers []AnswerFact, limit int) []HardWord {
	if limit <= 0 {
		limit = 10
	}
	order := []string{}
	stat := map[string]*HardWord{}
	for _, a := range answers {
		if !IsFirstAttempt(a.Phase, a.Attempt) {
			continue
		}
		s := stat[a.WordID]
		if s == nil {
			s = &HardWord{WordID: a.WordID}
			stat[a.WordID] = s
			order = append(order, a.WordID)
		}
		s.Total++
		if !a.Correct {
			s.Wrong++
		}
	}
	out := []HardWord{}
	for _, id := range order {
		s := stat[id]
		if s.Wrong > 0 {
			s.Rate = float64(s.Wrong) / float64(s.Total)
			out = append(out, *s)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Wrong != out[j].Wrong {
			return out[i].Wrong > out[j].Wrong
		}
		return out[i].Rate > out[j].Rate
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Round1 Math.round(x * 10) / 10（记录页的稳定度、难度保留一位小数）。
func Round1(x float64) float64 { return jsRoundF(x*10) / 10 }
