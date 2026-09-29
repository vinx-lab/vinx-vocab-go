// Package audioplan 发音预缓存的待抓清单（纯函数，对应旧 lib/audio-plan.ts）：按读音文本去重、
// 跳过已缓存、顺序固定。读音文本与缓存文件名的规则也在这里，internal/service/audio.go 点播时共用。
package audioplan

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

var (
	parenRe        = regexp.MustCompile(`[（(][^)）]*[)）]`)
	slashRe        = regexp.MustCompile(`/+[^/]*/+`)
	bracketRe      = regexp.MustCompile(`\[[^\]]*\]`)
	trailingOpenRe = regexp.MustCompile(`[（(].*$`)
	wsRe           = regexp.MustCompile(`\s+`)
)

// Target 发音使用的目标文本：去掉等号别名、括号注释，以及词库里残留的音标与半截括号
// （flowerbed //ˈflaʊəbed//、between [bɪˈtwiːn]、go by （、burn (-ed, -ed）。
func Target(spelling string) string {
	s := strings.SplitN(spelling, "=", 2)[0]
	s = parenRe.ReplaceAllString(s, "")
	s = slashRe.ReplaceAllString(s, "")
	s = bracketRe.ReplaceAllString(s, "")
	s = trailingOpenRe.ReplaceAllString(s, "")
	s = wsRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// FileName 缓存文件名：读音文本小写后取 sha1，所以大小写不同的词共用一个文件。
func FileName(text string) string {
	sum := sha1.Sum([]byte(strings.ToLower(text)))
	return hex.EncodeToString(sum[:]) + ".mp3"
}

// IsSuspect 读音文本里仍有斜线、方括号或括号：发给发音源会报错或把残留一起读出来。
func IsSuspect(text string) bool {
	return strings.ContainsAny(text, "/[]（(）)")
}

// uriUnreserved JS encodeURIComponent 保留不转义的字符集：A-Z a-z 0-9 - _ . ! ~ * ' ( )
// （评审 I2：url.QueryEscape 把空格编成 "+"、且会转义 ! * ' ( )，与 encodeURIComponent 不同，
// 自建音源把 {word} 放在路径段里时会因为 "+" 被当成字面加号而抓不到音频）。
var uriUnreserved [128]bool

func init() {
	for _, r := range "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_.!~*'()" {
		uriUnreserved[r] = true
	}
}

// EncodeURIComponent 与 JS `encodeURIComponent` 等价（逐字节 UTF-8 编码非保留字符为大写十六进制
// %XX；不做 UTF-16 代理对特殊处理，业务用到的发音文本不含 astral 平面字符，够用）。
func EncodeURIComponent(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r < 128 && uriUnreserved[r] {
			b.WriteRune(r)
			continue
		}
		var buf [utf8.UTFMax]byte
		n := utf8.EncodeRune(buf[:], r)
		for _, by := range buf[:n] {
			fmt.Fprintf(&b, "%%%02X", by)
		}
	}
	return b.String()
}

// Word 待规划的一个词。
type Word struct {
	ID        string
	Spelling  string
	AudioFile string // 空串表示未记录
}

// Item 一个待抓 / 已缓存目标文件。
type Item struct {
	// Text 请求发音源用的文本（同一文件的第一个词的写法）。
	Text    string   `json:"text"`
	File    string   `json:"file"`
	WordIDs []string `json:"wordIds"`
}

// Plan 发音预缓存计划。
type Plan struct {
	// Total 词总数。
	Total int `json:"total"`
	// Empty 去掉括号与别名后读音文本为空的词，不抓。
	Empty []string `json:"empty"`
	// Todo 需要去抓的文件，按文件名排序。
	Todo []Item `json:"todo"`
	// Suspect Target() 清理后仍残留斜线、方括号或括号的（兜底），不抓，交人工处理。
	Suspect []Item `json:"suspect"`
	// CachedFiles 已缓存的文件数。
	CachedFiles int `json:"cachedFiles"`
	// CachedWords 已缓存文件覆盖的词数。
	CachedWords int `json:"cachedWords"`
	// Unlinked 文件已缓存但 Word.audioFile 没记上的词，按 file 分组。
	Unlinked []Item `json:"unlinked"`
	// Shared 读音文本相同、共用一个文件的组（≥2 个词），按文件名排序。
	Shared []Item `json:"shared"`
}

type group struct {
	text    string
	file    string
	wordIDs []string
	linked  map[string]bool
}

// Plan 规划待抓清单。cached 为已缓存（且体积合格）的文件名集合。
func PlanAudioFetch(words []Word, cached map[string]bool) Plan {
	sorted := append([]Word{}, words...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Spelling != sorted[j].Spelling {
			return sorted[i].Spelling < sorted[j].Spelling
		}
		return sorted[i].ID < sorted[j].ID
	})

	groups := map[string]*group{}
	var order []string
	var empty []string
	for _, w := range sorted {
		text := Target(w.Spelling)
		if text == "" {
			empty = append(empty, w.ID)
			continue
		}
		file := FileName(text)
		g, ok := groups[file]
		if !ok {
			g = &group{text: text, file: file, linked: map[string]bool{}}
			groups[file] = g
			order = append(order, file)
		}
		g.wordIDs = append(g.wordIDs, w.ID)
		if w.AudioFile == file {
			g.linked[w.ID] = true
		}
	}

	all := make([]*group, 0, len(order))
	for _, f := range order {
		all = append(all, groups[f])
	}
	sort.Slice(all, func(i, j int) bool { return all[i].file < all[j].file })

	strip := func(g *group) Item { return Item{Text: g.text, File: g.file, WordIDs: g.wordIDs} }

	var todo, suspect, done, shared []*group
	for _, g := range all {
		if cached[g.file] {
			done = append(done, g)
			continue
		}
		if IsSuspect(g.text) {
			suspect = append(suspect, g)
		} else {
			todo = append(todo, g)
		}
	}
	for _, g := range all {
		if len(g.wordIDs) > 1 {
			shared = append(shared, g)
		}
	}

	toItems := func(gs []*group) []Item {
		out := make([]Item, 0, len(gs))
		for _, g := range gs {
			out = append(out, strip(g))
		}
		return out
	}

	cachedWords := 0
	var unlinked []Item
	for _, g := range done {
		cachedWords += len(g.wordIDs)
		var missing []string
		for _, id := range g.wordIDs {
			if !g.linked[id] {
				missing = append(missing, id)
			}
		}
		if len(missing) > 0 {
			unlinked = append(unlinked, Item{Text: g.text, File: g.file, WordIDs: missing})
		}
	}

	return Plan{
		Total:       len(words),
		Empty:       nonNil(empty),
		Todo:        toItems(todo),
		Suspect:     toItems(suspect),
		CachedFiles: len(done),
		CachedWords: cachedWords,
		Unlinked:    nonNilItems(unlinked),
		Shared:      toItems(shared),
	}
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func nonNilItems(s []Item) []Item {
	if s == nil {
		return []Item{}
	}
	return s
}
