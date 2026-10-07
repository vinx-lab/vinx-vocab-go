// Package fsrs 是 ts-fsrs 5.4.2（FSRS-6）长期排程路径的逐行移植，只覆盖旧版用到的配置：
// enable_short_term = false（LongTermScheduler）、enable_fuzz = false、默认权重。
//
// 为什么不用 go-fsrs/v4：它省略了 ts-fsrs 每一步的 roundTo(x, 8)，也没有 elapsed_days / learning_steps
// 字段，对照数据（testdata/fixtures.json，由 contract/scripts/fsrs-fixtures.mjs 用 ts-fsrs 生成）里
// 有到期时间与稳定度不一致的步骤。这里按 ts-fsrs 的运算顺序实现，浮点结果与 JS 一致。
//
// 与 JS 的数值差异处理：
//   - Math.round 在 .5 时取向 +∞ 的整数（Go math.Round 远离 0），用 jsRound；
//   - 显式 float64() 转换阻止编译器在部分架构上把乘加融合成 FMA（Go 规范允许融合，JS 不会）。
package fsrs

import (
	"math"
	"time"
)

// State 卡片状态（与 ts-fsrs State 数值一致）。
type State int

const (
	StateNew        State = 0
	StateLearning   State = 1
	StateReview     State = 2
	StateRelearning State = 3
)

// Rating 评分（与 ts-fsrs Rating 数值一致）。Manual 不支持。
type Rating int

const (
	Again Rating = 1
	Hard  Rating = 2
	Good  Rating = 3
	Easy  Rating = 4
)

// Card 与 ts-fsrs Card 字段一一对应。LastReview 为 nil 表示 undefined。
type Card struct {
	Due           time.Time
	Stability     float64
	Difficulty    float64
	ElapsedDays   int
	ScheduledDays int
	LearningSteps int
	Reps          int
	Lapses        int
	State         State
	LastReview    *time.Time
}

// Params 排程参数（ts-fsrs generatorParameters 的子集）。
type Params struct {
	RequestRetention float64
	MaximumInterval  int
	W                [21]float64
}

// DefaultW ts-fsrs 5.4.2 default_w（FSRS-6）。
var DefaultW = [21]float64{
	0.212, 1.2931, 2.3065, 8.2956, 6.4133, 0.8334, 3.0194, 1e-3, 1.8722, 0.1666, 0.796,
	1.4835, 0.0614, 0.2629, 1.6483, 0.6014, 1.8729, 0.5425, 0.0912, 0.0658, 0.1542,
}

const (
	sMin = 1e-3
	sMax = 36500.0
)

// Scheduler 长期排程器（ts-fsrs fsrs(params) 且 enable_short_term=false）。
type Scheduler struct {
	p                Params
	intervalModifier float64
	decay, factor    float64
}

// New 按参数创建排程器。默认权重都在 ts-fsrs 的 clamp 范围内，clipParameters 不改变任何值。
func New(p Params) *Scheduler {
	s := &Scheduler{p: p}
	s.decay, s.factor = computeDecayFactor(p.W[20])
	s.intervalModifier = roundTo(float64(math.Pow(p.RequestRetention, 1/s.decay)-1)/s.factor, 8)
	return s
}

// jsRound 与 JS Math.round 相同：取最接近的整数，恰好 .5 时取向 +∞ 的那个。
func jsRound(x float64) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return x
	}
	f := math.Floor(x)
	if x-f >= 0.5 {
		return f + 1
	}
	return f
}

// roundTo ts-fsrs roundTo(num, decimals)：Math.round(num * 10^d) / 10^d。
func roundTo(num float64, decimals int) float64 {
	factor := math.Pow(10, float64(decimals))
	return jsRound(float64(num*factor)) / factor
}

func clamp(v, lo, hi float64) float64 { return math.Min(math.Max(v, lo), hi) }

func computeDecayFactor(w20 float64) (decay, factor float64) {
	decay = -w20
	factor = math.Exp(float64(math.Pow(decay, -1)*math.Log(0.9))) - 1
	return decay, roundTo(factor, 8)
}

// forgettingCurve ts-fsrs forgetting_curve(w, elapsed_days, stability)。
func (s *Scheduler) forgettingCurve(elapsedDays int, stability float64) float64 {
	return roundTo(math.Pow(1+float64(s.factor*float64(elapsedDays))/stability, s.decay), 8)
}

func (s *Scheduler) initStability(g Rating) float64 { return math.Max(s.p.W[g-1], 0.1) }

func (s *Scheduler) initDifficulty(g Rating) float64 {
	w := s.p.W
	d := w[4] - math.Exp(float64(float64(g-1)*w[5])) + 1
	return roundTo(d, 8)
}

func (s *Scheduler) nextInterval(stability float64) int {
	ivl := math.Min(math.Max(1, jsRound(float64(stability*s.intervalModifier))), float64(s.p.MaximumInterval))
	// apply_fuzz：未启用 fuzz 时 Math.round(ivl)
	return int(jsRound(ivl))
}

func (s *Scheduler) linearDamping(deltaD, oldD float64) float64 {
	return roundTo(float64(deltaD*(10-oldD))/9, 8)
}

func (s *Scheduler) meanReversion(init, current float64) float64 {
	w := s.p.W
	return roundTo(float64(w[7]*init)+float64((1-w[7])*current), 8)
}

func (s *Scheduler) nextDifficulty(d float64, g Rating) float64 {
	deltaD := -s.p.W[6] * float64(g-3)
	nextD := d + s.linearDamping(deltaD, d)
	return clamp(s.meanReversion(s.initDifficulty(Easy), nextD), 1, 10)
}

func (s *Scheduler) nextRecallStability(d, st, r float64, g Rating) float64 {
	w := s.p.W
	hardPenalty := 1.0
	if g == Hard {
		hardPenalty = w[15]
	}
	easyBound := 1.0
	if g == Easy {
		easyBound = w[16]
	}
	// JS 从左到右：exp(w8) * (11-d) * s^-w9 * (exp((1-r)*w10) - 1) * hard * easy
	inc := math.Exp(w[8])
	inc = inc * (11 - d)
	inc = inc * math.Pow(st, -w[9])
	inc = inc * (math.Exp(float64((1-r)*w[10])) - 1)
	inc = inc * hardPenalty
	inc = float64(inc * easyBound) // 显式转换：阻止与下面的 1+inc 融合
	return roundTo(clamp(st*float64(1+inc), sMin, 36500), 8)
}

func (s *Scheduler) nextForgetStability(d, st, r float64) float64 {
	w := s.p.W
	v := w[11] * math.Pow(d, -w[12])
	v = v * (math.Pow(st+1, w[13]) - 1)
	v = v * math.Exp(float64((1-r)*w[14]))
	return roundTo(clamp(v, sMin, 36500), 8)
}

// nextState ts-fsrs FSRSAlgorithm.next_state（enable_short_term=false）。
func (s *Scheduler) nextState(d, st float64, t int, g Rating, r float64) (float64, float64) {
	if d == 0 && st == 0 {
		return clamp(s.initDifficulty(g), 1, 10), s.initStability(g)
	}
	var newS float64
	if g == Again {
		sAfterFail := s.nextForgetStability(d, st, r)
		// w17 = w18 = 0（未启用短期）：next_s_min = s / exp(0) = s
		nextSMin := st / math.Exp(0)
		newS = clamp(roundTo(nextSMin, 8), sMin, sAfterFail)
	} else {
		newS = s.nextRecallStability(d, st, r, g)
	}
	return s.nextDifficulty(d, g), newS
}

// dateDiffInDays ts-fsrs dateDiffInDays：按 UTC 日历日期相差的天数。
func dateDiffInDays(last, cur time.Time) int {
	l := last.UTC()
	c := cur.UTC()
	u1 := time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, time.UTC)
	u2 := time.Date(c.Year(), c.Month(), c.Day(), 0, 0, 0, 0, time.UTC)
	return int(math.Floor(float64(u2.Sub(u1).Milliseconds()) / 864e5))
}

// Next ts-fsrs fsrs.next(card, now, grade).card（长期排程）。
func (s *Scheduler) Next(card Card, now time.Time, g Rating) Card {
	last := card
	cur := card
	interval := 0
	if cur.State != StateNew && cur.LastReview != nil {
		interval = dateDiffInDays(*cur.LastReview, now)
	}
	rt := now
	cur.LastReview = &rt
	cur.ElapsedDays = interval
	cur.Reps++

	if last.State == StateNew {
		cur.ScheduledDays = 0
		cur.ElapsedDays = 0
		return s.schedule(cur, g, 0, 0, now, false)
	}
	// Learning / Relearning / Review 在长期排程下同一路径（learningState → reviewState）
	r := s.forgettingCurve(interval, cur.Stability)
	return s.schedule(cur, g, interval, r, now, true)
}

// schedule 计算四个评分的结果（间隔互相约束），返回 g 对应的卡片。
func (s *Scheduler) schedule(cur Card, g Rating, t int, r float64, now time.Time, review bool) Card {
	var cards [5]Card
	for _, gr := range []Rating{Again, Hard, Good, Easy} {
		c := cur
		c.Difficulty, c.Stability = s.nextState(cur.Difficulty, cur.Stability, t, gr, r)
		cards[gr] = c
	}
	again := s.nextInterval(cards[Again].Stability)
	hard := s.nextInterval(cards[Hard].Stability)
	good := s.nextInterval(cards[Good].Stability)
	easy := s.nextInterval(cards[Easy].Stability)
	again = min(again, hard)
	hard = max(hard, again+1)
	good = max(good, hard+1)
	easy = max(easy, good+1)
	ivls := [5]int{0, again, hard, good, easy}
	c := cards[g]
	c.ScheduledDays = ivls[g]
	c.Due = now.Add(time.Duration(ivls[g]) * 24 * time.Hour)
	c.State = StateReview
	c.LearningSteps = 0
	if review && g == Again {
		c.Lapses++
	}
	return c
}

// Retrievability 卡片在 now 时的回忆概率（ts-fsrs get_retrievability）；从没复习过（没有 lastReview 或稳定度为 0）返回 0。
func (s *Scheduler) Retrievability(card Card, now time.Time) float64 {
	if card.LastReview == nil || card.Stability <= 0 {
		return 0
	}
	return s.forgettingCurve(max(0, dateDiffInDays(*card.LastReview, now)), card.Stability)
}
