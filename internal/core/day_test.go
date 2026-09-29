package core

import (
	"reflect"
	"testing"
	"time"
)

// 逐条翻译旧 tests/lib/day.test.ts

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func iso(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

func TestDayKeyOf(t *testing.T) {
	sh := mustLoc(t, DefaultTimezone)
	cases := []struct{ in, want string }{
		{"2026-09-16T15:59:59Z", "2026-09-16"},
		{"2026-09-16T16:00:00Z", "2026-09-17"},
	}
	for _, c := range cases {
		ts, _ := time.Parse(time.RFC3339, c.in)
		if got := DayKeyOf(ts, sh); got != c.want {
			t.Errorf("DayKeyOf(%s) = %s, want %s", c.in, got, c.want)
		}
	}
}

func TestDayRange(t *testing.T) {
	cases := []struct{ day, tz, start, end string }{
		{"2026-09-17", DefaultTimezone, "2026-09-16T16:00:00.000Z", "2026-09-17T16:00:00.000Z"},
		// 夏令时切换日 23 小时
		{"2026-03-08", "America/New_York", "2026-03-08T05:00:00.000Z", "2026-03-09T04:00:00.000Z"},
	}
	for _, c := range cases {
		s, e := DayRange(c.day, mustLoc(t, c.tz))
		if iso(s) != c.start || iso(e) != c.end {
			t.Errorf("DayRange(%s,%s) = %s..%s", c.day, c.tz, iso(s), iso(e))
		}
	}
}

func TestAddDiffDays(t *testing.T) {
	cases := []struct {
		day  string
		n    int
		want string
	}{
		{"2026-12-31", 1, "2027-01-01"},
		{"2026-03-01", -1, "2026-02-28"},
		{"2026-02-30", 0, "2026-03-02"},
	}
	for _, c := range cases {
		if got := AddDays(c.day, c.n); got != c.want {
			t.Errorf("AddDays(%s,%d) = %s, want %s", c.day, c.n, got, c.want)
		}
	}
	if got := DiffDays("2026-09-01", "2026-09-17"); got != 16 {
		t.Errorf("DiffDays = %d", got)
	}
	if got := DiffDays("2026-09-17", "2026-09-01"); got != -16 {
		t.Errorf("DiffDays negative = %d", got)
	}
}

func TestLastNDays(t *testing.T) {
	got := LastNDays("2026-09-17", 3)
	want := []string{"2026-09-15", "2026-09-16", "2026-09-17"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LastNDays = %v", got)
	}
}

func TestComputeStreak(t *testing.T) {
	cases := []struct {
		days []string
		want int
	}{
		{[]string{"2026-09-17", "2026-09-16", "2026-09-14"}, 2},
		{[]string{"2026-09-16", "2026-09-15"}, 2},
		{[]string{"2026-09-15"}, 0},
		{nil, 0},
	}
	for _, c := range cases {
		if got := ComputeStreak(c.days, "2026-09-17"); got != c.want {
			t.Errorf("ComputeStreak(%v) = %d, want %d", c.days, got, c.want)
		}
	}
}

func TestIsValidDayKey(t *testing.T) {
	cases := map[string]bool{
		"2026-09-17": true,
		"2026-9-17":  false,
		"2026-02-30": true, // 与 JS Date.parse 一致：不校验当月天数
		"2026-02-32": false,
		"2026-13-01": false,
		"2026-00-10": false,
	}
	for in, want := range cases {
		if got := IsValidDayKey(in); got != want {
			t.Errorf("IsValidDayKey(%q) = %v, want %v", in, got, want)
		}
	}
}
