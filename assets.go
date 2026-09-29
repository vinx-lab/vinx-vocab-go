// Package vinxvocab 只用来嵌入仓库根目录下的静态资源（go:embed 不能引用上级目录）。
package vinxvocab

import "embed"

// VocabFS 内置词表：data/vocab/*.txt（首次启动自动导入为系统词书）。
//
//go:embed data/vocab/*.txt
var VocabFS embed.FS
