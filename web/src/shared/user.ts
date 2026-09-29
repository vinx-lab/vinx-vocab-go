import type { Capability, Role } from "./access";

/** 外观偏好：跟随系统 / 浅色 / 深色（跟账号保存，默认跟随系统） */
export const THEME_PREFS = ["system", "light", "dark"] as const;
export type ThemePref = (typeof THEME_PREFS)[number];

export function isThemePref(v: unknown): v is ThemePref {
  return typeof v === "string" && (THEME_PREFS as readonly string[]).includes(v);
}

/** 非法或缺失的值一律按「跟随系统」处理 */
export function normalizeThemePref(v: unknown): ThemePref {
  return isThemePref(v) ? v : "system";
}

export interface User {
  id: string;
  email: string;
  name: string;
  role: Role;
  currentGrade?: string | null;
  createdAt?: string;
  /** 外观偏好 */
  theme?: ThemePref;
  /** 当前角色具备的能力（后端按角色下发） */
  capabilities?: Capability[];
}

/** JWT claims（HttpOnly cookie 中的 payload） */
export interface AuthClaims {
  sub: string;
  email: string;
  name: string;
  role: Role;
}

/** 登录 / 注册成功返回 */
export interface LoginResult {
  user: User;
}
