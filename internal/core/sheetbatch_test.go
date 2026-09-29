package core

import "testing"

// 逐条翻译自旧 apps/api/tests/lib/sheet-batch.test.ts 的 splitSheets 部分。
// pickNextSheet 部分（今日页取下一份）在 Go 里没有独立的纯函数——它直接写在
// internal/service/records.go 的 NextSheet 里（查库 + 挑选一次完成），对应场景由
// internal/service/sheets_test.go 的 TestNextSheet 用真实 WordSheet / StudySession 行覆盖。

func ids(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "w" + itoa(i)
	}
	return out
}

func sliceEqual(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestSplitSheets(t *testing.T) {
	t.Run("正好够：按顺序切成 份数 份，第 1 份是排在最前（最难）的词", func(t *testing.T) {
		all := ids(120)
		sheets, err := SplitSheets(all, 4, 30)
		if err != nil {
			t.Fatal(err)
		}
		lens := make([]int, len(sheets))
		for i, s := range sheets {
			lens[i] = len(s)
		}
		for _, l := range lens {
			if l != 30 {
				t.Fatalf("lens = %v", lens)
			}
		}
		sliceEqual(t, sheets[0], all[:30])
		sliceEqual(t, sheets[3], all[90:])
	})

	t.Run("去重，不跨份重复", func(t *testing.T) {
		sheets, err := SplitSheets([]string{"a", "b", "a", "c", "b", "d"}, 2, 2)
		if err != nil {
			t.Fatal(err)
		}
		if len(sheets) != 2 {
			t.Fatalf("got %v", sheets)
		}
		sliceEqual(t, sheets[0], []string{"a", "b"})
		sliceEqual(t, sheets[1], []string{"c", "d"})
	})

	t.Run("词不够时各份尽量平均，前面的份多一个；份数不超过词数", func(t *testing.T) {
		check := func(n, copies, perSheet int, want []int) {
			t.Helper()
			sheets, err := SplitSheets(ids(n), copies, perSheet)
			if err != nil {
				t.Fatal(err)
			}
			got := make([]int, len(sheets))
			for i, s := range sheets {
				got[i] = len(s)
			}
			if len(got) != len(want) {
				t.Fatalf("got %v, want %v", got, want)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("got %v, want %v", got, want)
				}
			}
		}
		check(100, 4, 30, []int{25, 25, 25, 25})
		check(10, 3, 30, []int{4, 3, 3})
		check(2, 5, 30, []int{1, 1})
		sheets, err := SplitSheets([]string{}, 3, 30)
		if err != nil {
			t.Fatal(err)
		}
		if len(sheets) != 0 {
			t.Fatalf("got %v", sheets)
		}
	})

	t.Run("超过 份数 × 每份词数 时报错（不静默丢词）", func(t *testing.T) {
		if _, err := SplitSheets(ids(31), 1, 30); err == nil {
			t.Fatal("want error")
		}
	})
}
