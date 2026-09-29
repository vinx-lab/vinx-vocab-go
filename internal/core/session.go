package core

// IsTestKind 检测类学习组：作答不即时反馈、进行中不下发答案、首次交卷为正式成绩
// （decisions K10 / K20；旧 packages/shared/src/session.ts）。
func IsTestKind(kind string) bool {
	return kind == "test" || kind == "sheet"
}
