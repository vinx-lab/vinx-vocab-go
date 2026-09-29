/**
 * 角色与能力（前后端共用的唯一事实来源）。
 *
 * 不用「权限点 + 角色权限关联表」那套 RBAC：小系统里三种角色够用，
 * 能力表写死在代码里，改一次两端同时生效，不需要数据库配置和管理界面。
 * 真正的数据范围（本人 / 本班 / 全部）仍由后端 services/access.ts 判定。
 */

export type Role = "student" | "teacher" | "admin";

export const ROLES: Role[] = ["student", "teacher", "admin"];

export const ROLE_LABEL: Record<Role, string> = {
  student: "学生",
  teacher: "教师",
  admin: "管理员",
};

/** 能力（粗粒度，按“能不能做这类事”划分） */
export type Capability =
  | "study" // 今日 / 学习 / 自己的记录
  | "plans" // 学习计划（自己的）
  | "plans.assign" // 给班级或学生安排计划
  | "books.read" // 浏览词书
  | "books.edit" // 建词书 / 导入 / 编辑词条（含 AI 例句、发音缓存）
  | "classes" // 班级与成员管理
  | "students.view" // 查看学生的学习记录
  | "users" // 用户管理（建号 / 改角色 / 重置密码）
  | "system"; // 系统词书、系统设置

const STUDENT: Capability[] = ["study", "plans", "books.read"];
const TEACHER: Capability[] = [...STUDENT, "plans.assign", "books.edit", "classes", "students.view", "users"];
const ADMIN: Capability[] = [...TEACHER, "system"];

export const ROLE_CAPABILITIES: Record<Role, Capability[]> = {
  student: STUDENT,
  teacher: TEACHER,
  admin: ADMIN,
};

export function isRole(value: unknown): value is Role {
  return typeof value === "string" && (ROLES as string[]).includes(value);
}

/** 角色是否具备某能力 */
export function roleCan(role: string | undefined | null, capability: Capability): boolean {
  if (!isRole(role)) return false;
  return ROLE_CAPABILITIES[role].includes(capability);
}

/** 某角色的能力清单（登录后下发给前端） */
export function capabilitiesOf(role: string | undefined | null): Capability[] {
  return isRole(role) ? [...ROLE_CAPABILITIES[role]] : [];
}
