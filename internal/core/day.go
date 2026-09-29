package core

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// 学习日工具（纯函数，对应旧 lib/day.ts）。学习日 = APP_TIMEZONE 时区下的自然日，键格式 YYYY-MM-DD。

// DefaultTimezone 默认学习日时区。
const DefaultTimezone = "Asia/Shanghai"

// DayKeyOf 某时刻所属学习日。
func DayKeyOf(t time.Time, loc *time.Location) string {
	return t.In(loc).Format("2006-01-02")
}

// parseDayKey 宽松解析（与 JS Date.UTC 一样允许日溢出，如 02-30 → 03-02）。
func parseDayKey(dayKey string) (y, m, d int) {
	fmt.Sscanf(dayKey, "%d-%d-%d", &y, &m, &d)
	return
}

// DayRange 学习日 [start, end) 的 UTC 时刻。
func DayRange(dayKey string, loc *time.Location) (start, end time.Time) {
	y, m, d := parseDayKey(dayKey)
	start = time.Date(y, time.Month(m), d, 0, 0, 0, 0, loc).UTC()
	end = time.Date(y, time.Month(m), d+1, 0, 0, 0, 0, loc).UTC()
	return
}

// AddDays dayKey 加减天数。
func AddDays(dayKey string, n int) string {
	y, m, d := parseDayKey(dayKey)
	return time.Date(y, time.Month(m), d+n, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
}

// DiffDays 两个 dayKey 相差天数（b - a）。
func DiffDays(a, b string) int {
	ya, ma, da := parseDayKey(a)
	yb, mb, db := parseDayKey(b)
	ta := time.Date(ya, time.Month(ma), da, 0, 0, 0, 0, time.UTC)
	tb := time.Date(yb, time.Month(mb), db, 0, 0, 0, 0, time.UTC)
	return int(tb.Sub(ta).Hours() / 24)
}

// LastNDays 最近 n 天的 dayKey 列表（含 today，升序）。
func LastNDays(today string, n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, AddDays(today, i-n+1))
	}
	return out
}

// ComputeStreak 连续学习天数：今天学了从今天往回数；今天没学从昨天往回数。
func ComputeStreak(activeDays []string, today string) int {
	set := make(map[string]bool, len(activeDays))
	for _, d := range activeDays {
		set[d] = true
	}
	cursor := today
	if !set[today] {
		cursor = AddDays(today, -1)
	}
	n := 0
	for set[cursor] {
		n++
		cursor = AddDays(cursor, -1)
	}
	return n
}

var dayKeyRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// IsValidDayKey 格式为 YYYY-MM-DD 且 JS Date.parse 能解析（月 1–12、日 1–31，不校验当月天数，与旧版一致）。
func IsValidDayKey(s string) bool {
	if !dayKeyRe.MatchString(s) {
		return false
	}
	m, _ := strconv.Atoi(s[5:7])
	d, _ := strconv.Atoi(s[8:10])
	return m >= 1 && m <= 12 && d >= 1 && d <= 31
}
