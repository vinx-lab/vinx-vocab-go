package core

import (
	"sort"
	"time"
)

// 单词单选词（纯函数，对应旧 lib/unfamiliar.ts）：给已学词按「不熟」程度打分，规则见 docs/product.md「单词单」。
// 选词来源（不熟 / 某次测试错词 / 某个单元）与批量生成（份数 × 每份词数）。

const (
	// SheetMin 单次预览/生成的最少词数。
	SheetMin = 10
	// SheetPerMax 每份最多 30 词，保证一份正好一页 A4。
	SheetPerMax = 30
	// SheetCopiesMax 一次最多生成 7 份（一周）。
	SheetCopiesMax = 7
	// SheetMax 一次选词的总上限 = 份数 × 每份词数。
	SheetMax = SheetPerMax * SheetCopiesMax
	// SheetDefault 预览/生成的默认词数。
	SheetDefault = 30
)

const dueSoonWindow = 3 * 24 * time.Hour

// UnfamiliarReason 选词原因（旧 UnfamiliarReason 可辨识联合；Count 只在 wrong / lapse 时有意义）。
type UnfamiliarReason struct {
	Kind  string `json:"kind"`
	Count int    `json:"count,omitempty"`
}

// UnfamiliarCandidate 打分输入：一个学生已学词的记忆状态 + 近 14 天首次作答答错次数。
type UnfamiliarCandidate struct {
	WordID    string
	Stability float64
	Lapses    int
	Due       time.Time
	// Wrong14 近 14 天练习/检测首次作答答错次数。
	Wrong14 int
}

// UnfamiliarPick 打分结果。
type UnfamiliarPick struct {
	WordID  string             `json:"wordId"`
	Score   int                `json:"score"`
	Reasons []UnfamiliarReason `json:"reasons"`
}

// ScoreUnfamiliar 计分；已掌握且近 14 天没错、也从没遗忘过的词返回 nil（不入选）。
func ScoreUnfamiliar(c UnfamiliarCandidate, now time.Time) *UnfamiliarPick {
	level := MasteryLevel(c.Stability)
	if level == "mastered" && c.Wrong14 == 0 && c.Lapses == 0 {
		return nil
	}
	reasons := []UnfamiliarReason{}
	score := 0
	if c.Wrong14 > 0 {
		score += 3 * c.Wrong14
		reasons = append(reasons, UnfamiliarReason{Kind: "wrong", Count: c.Wrong14})
	}
	if c.Lapses > 0 {
		score += 2 * c.Lapses
		reasons = append(reasons, UnfamiliarReason{Kind: "lapse", Count: c.Lapses})
	}
	if level == "learning" {
		score += 2
		reasons = append(reasons, UnfamiliarReason{Kind: "learning"})
	} else if level == "consolidating" {
		score += 1
		reasons = append(reasons, UnfamiliarReason{Kind: "consolidating"})
	}
	if c.Due.Sub(now) <= dueSoonWindow {
		score += 1
		reasons = append(reasons, UnfamiliarReason{Kind: "dueSoon"})
	}
	return &UnfamiliarPick{WordID: c.WordID, Score: score, Reasons: reasons}
}

// SelectOptions SelectUnfamiliarWords 的参数。
type SelectOptions struct {
	Now   time.Time
	Count int
	// Exclude 排除集合（其他待测单子上的词）；nil 表示不排除。
	Exclude map[string]bool
	// Include 指定要选的词（如上次测错的词）：排最前，带 retest 原因，不受 Exclude 影响。
	Include []string
}

// SelectUnfamiliarWords 选词：Include 的词排最前并标 retest，其余按分数降序、同分先到期补齐到 count。
// 结果总数不超过 SheetMax。
func SelectUnfamiliarWords(candidates []UnfamiliarCandidate, opts SelectOptions) []UnfamiliarPick {
	byID := make(map[string]UnfamiliarCandidate, len(candidates))
	for _, c := range candidates {
		byID[c.WordID] = c
	}

	seen := map[string]bool{}
	included := []string{}
	for _, id := range opts.Include {
		if seen[id] {
			continue
		}
		seen[id] = true
		included = append(included, id)
		if len(included) >= SheetMax {
			break
		}
	}

	head := make([]UnfamiliarPick, 0, len(included))
	taken := map[string]bool{}
	for _, wordID := range included {
		taken[wordID] = true
		reasons := []UnfamiliarReason{{Kind: "retest"}}
		score := 0
		if cand, ok := byID[wordID]; ok {
			if scored := ScoreUnfamiliar(cand, opts.Now); scored != nil {
				reasons = append(reasons, scored.Reasons...)
				score = scored.Score
			}
		}
		head = append(head, UnfamiliarPick{WordID: wordID, Score: score, Reasons: reasons})
	}

	type dueScored struct {
		pick UnfamiliarPick
		due  int64
	}
	rest := []dueScored{}
	for _, c := range candidates {
		if taken[c.WordID] || (opts.Exclude != nil && opts.Exclude[c.WordID]) {
			continue
		}
		p := ScoreUnfamiliar(c, opts.Now)
		if p == nil {
			continue
		}
		rest = append(rest, dueScored{pick: *p, due: c.Due.UnixMilli()})
	}
	sort.SliceStable(rest, func(i, j int) bool {
		if rest[i].pick.Score != rest[j].pick.Score {
			return rest[i].pick.Score > rest[j].pick.Score
		}
		return rest[i].due < rest[j].due
	})

	total := opts.Count
	if len(head) > total {
		total = len(head)
	}
	if total > SheetMax {
		total = SheetMax
	}
	out := append([]UnfamiliarPick{}, head...)
	for _, r := range rest {
		out = append(out, r.pick)
	}
	if len(out) > total {
		out = out[:total]
	}
	return out
}

// PoolOptions SelectFromPool 的参数。
type PoolOptions struct {
	Now   time.Time
	Count int
	// KeepFamiliar true（错词来源）时已掌握且干净的词也保留，否则排除。
	KeepFamiliar bool
	// Tag 加在每个词原因的最前面；nil 表示不加。
	Tag *UnfamiliarReason
}

// SelectFromPool 从限定的词池里选词（某次测试的错词 / 某个单元）：
// 已学的词按不熟分数降序（同分先到期），没学过的词按池内顺序排在后面（只记成绩、不建记忆卡，K10）。
func SelectFromPool(pool []string, candidates []UnfamiliarCandidate, opts PoolOptions) []UnfamiliarPick {
	byID := make(map[string]UnfamiliarCandidate, len(candidates))
	for _, c := range candidates {
		byID[c.WordID] = c
	}
	var headReasons []UnfamiliarReason
	if opts.Tag != nil {
		headReasons = []UnfamiliarReason{*opts.Tag}
	}

	type dueScored struct {
		pick UnfamiliarPick
		due  int64
	}
	var learned []dueScored
	var unlearned []UnfamiliarPick
	seen := map[string]bool{}
	for _, wordID := range pool {
		if seen[wordID] {
			continue
		}
		seen[wordID] = true
		cand, ok := byID[wordID]
		if !ok {
			reasons := append(append([]UnfamiliarReason{}, headReasons...), UnfamiliarReason{Kind: "unlearned"})
			unlearned = append(unlearned, UnfamiliarPick{WordID: wordID, Score: 0, Reasons: reasons})
			continue
		}
		scored := ScoreUnfamiliar(cand, opts.Now)
		if scored == nil && !opts.KeepFamiliar {
			continue
		}
		score := 0
		var extra []UnfamiliarReason
		if scored != nil {
			score = scored.Score
			extra = scored.Reasons
		}
		reasons := append(append([]UnfamiliarReason{}, headReasons...), extra...)
		learned = append(learned, dueScored{pick: UnfamiliarPick{WordID: wordID, Score: score, Reasons: reasons}, due: cand.Due.UnixMilli()})
	}
	sort.SliceStable(learned, func(i, j int) bool {
		if learned[i].pick.Score != learned[j].pick.Score {
			return learned[i].pick.Score > learned[j].pick.Score
		}
		return learned[i].due < learned[j].due
	})

	out := make([]UnfamiliarPick, 0, len(learned)+len(unlearned))
	for _, l := range learned {
		out = append(out, l.pick)
	}
	out = append(out, unlearned...)
	total := opts.Count
	if total > SheetMax {
		total = SheetMax
	}
	if len(out) > total {
		out = out[:total]
	}
	return out
}
