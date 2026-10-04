/** 班级「允许学生自主安排」（spec 0008）的共用文字 */
export const SELF_PLAN_OPTIONS = [
  { value: true, label: "允许：学生可以在班级目标之外追加目标词书，自己建计划" },
  { value: false, label: "不允许：学生只学老师布置的计划，全班按班级目标统一统计" },
];

export const SELF_PLAN_LABEL = (allow: boolean) => (allow ? "允许学生自主安排" : "不允许学生自主安排");
