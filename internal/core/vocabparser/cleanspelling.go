package vocabparser

import (
	"regexp"
	"strings"
)

// 拼写末尾注释的写法（spec 0007 §1）。考纲词表里有不少条目把变形、说明、音标写在拼写后面，
// 如 `fly (flew, flown)`、`gladness //ˈɡlædnəs//`、`bike = bicycle`，导入后会成为独立的词。
var (
	trailDoubleSlash = regexp.MustCompile(`^(.*?)` + ws + `*//([^/]+)//$`)
	trailBracket     = regexp.MustCompile(`^(.*?)` + ws + `*\[([^\[\]]+)\]$`)
	trailParen       = regexp.MustCompile(`^(.*?)(` + ws + `*)[(（]([^()（）]*)[)）]$`)
	trailOpenParen   = regexp.MustCompile(`^(.*?)` + ws + `*[(（]([^()（）]*)$`)
	optionalSuffix   = regexp.MustCompile(`^[a-z]{1,3}$`)
	equalsSplit      = regexp.MustCompile(`^([^=]*?)` + ws + `*=` + ws + `*(.*)$`)
)

// singleWord 剩下的部分是单个词（不含空白）且含字母。
func singleWord(s string) bool {
	return s != "" && !wsRe.MatchString(s) && letterRe.MatchString(s)
}

// CleanSpelling 拆出拼写末尾的注释，返回规整后的拼写、拆出的音标与注释（都没有时原样返回、后两者为空）：
//
//   - 双斜杠音标 `gladness //ˈɡlædnəs//` → `gladness`，音标 `ˈɡlædnəs`；
//   - 方括号音标 `between [bɪˈtwiːn]` → `between`，音标 `bɪˈtwiːn`；
//   - 末尾括号（半角或全角，前面可以没有空格）`fly (flew, flown)` → `fly`，注释 `flew, flown`；
//   - 等号 `bike = bicycle` → `bike`，注释 `= bicycle`。
//
// 两个补充：紧贴的一到三个小写字母 `toward(s)` 注释写成 `towards`；缺右括号的 `burn (-ed, -ed`
// 也按末尾括号处理。
//
// 括号与等号两条只在剩下的部分是单个词时生效（`look after (sb.)` 这样的短语不动）；
// 剩下的部分不含字母时原样返回。结果是不动点：对返回的拼写再调用一次不会再变。
func CleanSpelling(s string) (spelling, phonetic, note string) {
	spelling = jsTrim(s)
	var notes []string
	for {
		if m := trailDoubleSlash.FindStringSubmatch(spelling); m != nil && letterRe.MatchString(m[1]) {
			if phonetic == "" {
				phonetic = jsTrim(m[2])
			}
			spelling = jsTrim(m[1])
			continue
		}
		if m := trailBracket.FindStringSubmatch(spelling); m != nil && letterRe.MatchString(m[1]) {
			if phonetic == "" {
				phonetic = jsTrim(m[2])
			}
			spelling = jsTrim(m[1])
			continue
		}
		if m := trailParen.FindStringSubmatch(spelling); m != nil && singleWord(jsTrim(m[1])) {
			base, n := jsTrim(m[1]), jsTrim(m[3])
			if m[2] == "" && optionalSuffix.MatchString(n) {
				// 紧贴的可省字母 `toward(s)`、`mountain(s)`：注释写成完整的另一种拼写
				n = base + n
			}
			if n != "" {
				notes = append([]string{n}, notes...)
			}
			spelling = base
			continue
		}
		// 源词表断行留下的半个括号 `burn (-ed, -ed`：同样当作注释
		if m := trailOpenParen.FindStringSubmatch(spelling); m != nil && singleWord(jsTrim(m[1])) {
			if n := jsTrim(m[2]); n != "" {
				notes = append([]string{n}, notes...)
			}
			spelling = jsTrim(m[1])
			continue
		}
		if m := equalsSplit.FindStringSubmatch(spelling); m != nil && singleWord(jsTrim(m[1])) && jsTrim(m[2]) != "" {
			notes = append([]string{"= " + jsTrim(m[2])}, notes...)
			spelling = jsTrim(m[1])
			continue
		}
		break
	}
	return spelling, phonetic, strings.Join(notes, "；")
}

// AppendNote 把拼写里拆出的注释以「（…）」接在释义末尾；注释为空时原样返回。
func AppendNote(definition, note string) string {
	if note == "" {
		return definition
	}
	return definition + "（" + note + "）"
}
