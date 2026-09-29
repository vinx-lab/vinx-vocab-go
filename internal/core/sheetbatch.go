package core

import "fmt"

// 单词单批量生成（纯函数，对应旧 lib/sheet-batch.ts splitSheets）。
// 今日页取下一份的逻辑（旧 pickNextSheet）在 internal/service/records.go 的 NextSheet 里，
// 因为它需要按「已测完 / 有进行中测试」查库，不是纯函数；不要在这里重复定义。

// SplitSheets 把已按「从难到易」排好的词切成多份：去重后按顺序切，第 1 份最难。
// 词不够 份数 × 每份词数 时各份尽量平均（前面的份多一个），份数不超过词数；超过则报错，不静默丢词。
func SplitSheets(wordIDs []string, copies, perSheet int) ([][]string, error) {
	seen := make(map[string]bool, len(wordIDs))
	ids := make([]string, 0, len(wordIDs))
	for _, id := range wordIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) > copies*perSheet {
		return nil, fmt.Errorf("选了 %d 个词，超过 %d 份 × %d 词", len(ids), copies, perSheet)
	}
	k := copies
	if len(ids) < k {
		k = len(ids)
	}
	sheets := make([][]string, 0, k)
	start := 0
	for i := 0; i < k; i++ {
		size := len(ids) / k
		if i < len(ids)%k {
			size++
		}
		sheets = append(sheets, ids[start:start+size])
		start += size
	}
	return sheets, nil
}
