// Package vinxvocab 只用来嵌入仓库根目录下的静态资源（go:embed 不能引用上级目录）。
package vinxvocab

import "embed"

// VocabFS 内置词表：data/vocab/*.txt（首次启动自动导入为系统词书）。
//
//go:embed data/vocab/*.txt
var VocabFS embed.FS

// IrregularForms 内置不规则变化表（词形还原用，spec 0004 §5）：每行「原形<TAB>变化形式，逗号分隔」。
// 扩展名是 .tsv，不在 VocabFS 里，不会被当成词书导入。
//
//go:embed data/vocab/irregular-forms.tsv
var IrregularForms string
