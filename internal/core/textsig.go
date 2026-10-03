package core

import "strings"

// UnitTextSignature 单元的篇在导入去重时的识别键（spec 0004）：类型 + 标题 + 按顺序的英文句子。
// 用同一份带 [句型] / [课文] 的文件重复导入到已有词书时，单元里已有相同识别键的篇不再重复创建；
// 只按标题判定会把两篇都没写标题（默认标题相同）的不同课文误判为重复，所以带上句子内容。
// 标题与句子去掉首尾空白、句内连续空白折叠为一个空格后比较；中文译文、段落不参与比较。
func UnitTextSignature(kind, title string, ens []string) string {
	var b strings.Builder
	b.WriteString(kind)
	b.WriteByte(0)
	b.WriteString(collapseSpace(title))
	for _, en := range ens {
		b.WriteByte(0)
		b.WriteString(collapseSpace(en))
	}
	return b.String()
}

func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
