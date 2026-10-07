package core

import (
	"math"
	"time"

	"github.com/vinx-lab/vinx-vocab-go/internal/core/fsrs"
)

// 记忆排程（FSRS）+ 自动评分规则（对应旧 lib/scheduler.ts）。设计见 docs/decisions.md K5、K7–K10。
//
// 学生不自评：一组学习结束后，服务端根据每个词在练习阶段的“首次作答”推导评分。

// Rating 评分（1 again / 2 hard / 3 good / 4 easy）。
type Rating = fsrs.Rating

const (
	RatingAgain = fsrs.Again
	RatingHard  = fsrs.Hard
	RatingGood  = fsrs.Good
	RatingEasy  = fsrs.Easy
)

// RatingName 结算结果 ratings 里的键名（旧 RATING_NAME）。
var RatingName = map[Rating]string{RatingAgain: "again", RatingHard: "hard", RatingGood: "good", RatingEasy: "easy"}

// 与旧版相同的参数：保留率 0.9、最长间隔 365 天、无 fuzz、按“天”排程（学生一天学一次，不需要分钟级学习步）。
var memoryScheduler = fsrs.New(fsrs.Params{RequestRetention: 0.9, MaximumInterval: 365, W: fsrs.DefaultW})

// MemoryCard 数据库 MemoryState 中的 FSRS 字段。
type MemoryCard struct {
	Due           time.Time
	Stability     float64
	Difficulty    float64
	ElapsedDays   int
	ScheduledDays int
	LearningSteps int
	Reps          int
	Lapses        int
	State         int
	LastReview    *time.Time
}

func toFSRS(m MemoryCard) fsrs.Card {
	return fsrs.Card{Due: m.Due, Stability: m.Stability, Difficulty: m.Difficulty, ElapsedDays: m.ElapsedDays,
		ScheduledDays: m.ScheduledDays, LearningSteps: m.LearningSteps, Reps: m.Reps, Lapses: m.Lapses,
		State: fsrs.State(m.State), LastReview: m.LastReview}
}

func fromFSRS(c fsrs.Card) MemoryCard {
	return MemoryCard{Due: c.Due, Stability: c.Stability, Difficulty: c.Difficulty, ElapsedDays: c.ElapsedDays,
		ScheduledDays: c.ScheduledDays, LearningSteps: c.LearningSteps, Reps: c.Reps, Lapses: c.Lapses,
		State: int(c.State), LastReview: c.LastReview}
}

// NewMemoryCard 新卡片（尚未复习）。
func NewMemoryCard(now time.Time) MemoryCard {
	return MemoryCard{Due: now}
}

// ApplyRating 按评分推进一次记忆状态。
// 时钟回拨 / 跨设备时间不一致时，不允许“早于上次复习”的复习时刻（FSRS 会拒绝负间隔）。
func ApplyRating(card MemoryCard, rating Rating, now time.Time) MemoryCard {
	at := now
	if card.LastReview != nil && now.Before(*card.LastReview) {
		at = *card.LastReview
	}
	return fromFSRS(memoryScheduler.Next(toFSRS(card), at, rating))
}

// CapDue 把到期时间封顶到 cap（新学当天的词保证下个学习日一定复习）。
func CapDue(card MemoryCard, cap, now time.Time) MemoryCard {
	if !card.Due.After(cap) {
		return card
	}
	card.Due = cap
	days := float64(cap.Sub(now).Milliseconds()) / 86_400_000
	card.ScheduledDays = int(math.Max(0, jsRoundF(days)))
	return card
}

// jsRoundF 与 JS Math.round 相同（.5 取向 +∞）。
func jsRoundF(x float64) float64 {
	f := math.Floor(x)
	if x-f >= 0.5 {
		return f + 1
	}
	return f
}

// FirstAttempt 词在一组中的首次作答汇总（每个题型一条）。
type FirstAttempt struct {
	Mode     string
	Correct  bool
	HintUsed bool
}

// DeriveRating 自动评分：
//   - learn（当天刚看过答案，弱证据）：全对 → Hard；有错 → Again
//   - review、drill（spec 0009：错词强化按复习的规则）：全对且无提示 → Good；部分对或用了提示 → Hard；全错 → Again
//   - test / sheet：全对 → Good；否则 → Again
//
// 没有任何首次作答（未练完）→ 0（不结算）。返回 0 表示 null。
func DeriveRating(kind string, attempts []FirstAttempt, requiredModes []string) Rating {
	byMode := map[string]FirstAttempt{}
	for _, a := range attempts {
		byMode[a.Mode] = a // 与 new Map(entries) 一样后者覆盖前者
	}
	if len(requiredModes) == 0 {
		return 0
	}
	list := make([]FirstAttempt, 0, len(requiredModes))
	for _, m := range requiredModes {
		a, ok := byMode[m]
		if !ok {
			return 0
		}
		list = append(list, a)
	}
	correct := 0
	hint := false
	for _, a := range list {
		if a.Correct {
			correct++
		}
		if a.HintUsed {
			hint = true
		}
	}
	allCorrect := correct == len(list)
	switch kind {
	case "learn":
		if allCorrect {
			return RatingHard
		}
		return RatingAgain
	case "test", "sheet":
		if allCorrect {
			return RatingGood
		}
		return RatingAgain
	case "review", "drill":
		if correct == 0 {
			return RatingAgain
		}
		if allCorrect && !hint {
			return RatingGood
		}
		return RatingHard
	}
	return 0 // 旧版 switch 落空返回 undefined（调用方视为不结算）
}

// MasteryLevel 记忆分层：stability 天数阈值（docs/product.md §6）。
func MasteryLevel(stability float64) string {
	if stability >= 21 {
		return "mastered"
	}
	if stability >= 7 {
		return "consolidating"
	}
	return "learning"
}

// MasteryLabel 分层中文名（spec 0009：learning 显示为「刚记住」，另有最近一次答错的「没记住」）。
var MasteryLabel = map[string]string{"missed": "没记住", "learning": "刚记住", "consolidating": "巩固中", "mastered": "已掌握"}
