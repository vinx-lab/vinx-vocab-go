/**
 * 前端能力判定：与后端共用 packages/shared 的角色能力表。
 * 前端判定只决定“显示什么”，真正的授权与数据范围在后端。
 */
import { roleCan, type Capability, type User } from "@vinx/shared";

export function can(identity: Pick<User, "role"> | undefined | null, capability: Capability): boolean {
  return !!identity && roleCan(identity.role, capability);
}

/** refine 资源 → 能力（仅用于 accessControlProvider） */
const RESOURCE_CAPABILITY: Record<string, Capability> = {
  today: "study",
  study: "study",
  plans: "plans",
  books: "books.read",
  records: "study",
  passages: "study",
  classes: "classes",
  students: "students.view",
  users: "users",
  system: "system",
};

export function canAccess(identity: Pick<User, "role"> | undefined | null, resource: string): boolean {
  const cap = RESOURCE_CAPABILITY[resource];
  return !!cap && can(identity, cap);
}

/** 登录后的默认落地页 */
export function homePath(identity: Pick<User, "role"> | undefined | null): string {
  if (can(identity, "study")) return "/today";
  if (can(identity, "classes")) return "/classes";
  if (can(identity, "users")) return "/admin/users";
  return "/profile";
}
