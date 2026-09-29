/** 检测类学习组：作答不即时反馈、进行中不下发答案、首次交卷为正式成绩（decisions K10 / K20） */
export function isTestKind(kind: string): boolean {
  return kind === "test" || kind === "sheet";
}
