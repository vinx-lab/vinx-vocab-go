import { isThemePref, type ThemePref } from "@vinx/shared";

/**
 * 外观偏好：跟账号保存，localStorage 只做缓存。
 * index.html 的内联脚本在渲染前按同样规则读缓存、设 data-theme（避免刷新闪白），规则改动时两处一起改。
 */
export const THEME_STORAGE_KEY = "vinx_theme";
/** 同一页面内通知 ThemeProvider 偏好已变化（登录、/auth/me、手动切换） */
export const THEME_EVENT = "vinx-theme-change";

export type ResolvedTheme = "light" | "dark";

export const THEME_LABEL: Record<ThemePref, string> = { system: "跟随系统", light: "浅色", dark: "深色" };

/** 偏好 → 实际主题：跟随系统时看系统是否为深色 */
export function resolveTheme(pref: ThemePref, systemDark: boolean): ResolvedTheme {
  if (pref === "system") return systemDark ? "dark" : "light";
  return pref;
}

/** 缓存与账号值合并：账号上有合法值时以账号为准，否则用缓存，都没有则跟随系统 */
export function mergeThemePref(cached: unknown, account: unknown): ThemePref {
  if (isThemePref(account)) return account;
  if (isThemePref(cached)) return cached;
  return "system";
}

export function readCachedTheme(): ThemePref {
  try {
    return mergeThemePref(localStorage.getItem(THEME_STORAGE_KEY), undefined);
  } catch {
    return "system";
  }
}

/** 写缓存并通知当前页面；退出登录不调用它，缓存保留给登录页 */
export function writeCachedTheme(pref: ThemePref) {
  try {
    localStorage.setItem(THEME_STORAGE_KEY, pref);
  } catch {
    /* 隐私模式等写不进去：只影响刷新后的首帧 */
  }
  window.dispatchEvent(new CustomEvent<ThemePref>(THEME_EVENT, { detail: pref }));
}

/** 正在保存的新偏好：保存期间到达的 /auth/me 可能还是旧值，不能拿它覆盖 */
let savingTheme: ThemePref | null = null;
export function setSavingTheme(pref: ThemePref | null) {
  savingTheme = pref;
}

/** 拿到账号信息后同步：账号值覆盖缓存 */
export function syncThemeFromAccount(account: unknown) {
  if (savingTheme || !isThemePref(account)) return;
  if (readCachedTheme() !== account) writeCachedTheme(account);
}

export function systemPrefersDark(): boolean {
  return typeof window !== "undefined" && typeof window.matchMedia === "function" && window.matchMedia("(prefers-color-scheme: dark)").matches;
}

/** 把实际主题写到 <html data-theme>，styles.css 的色板据此切换 */
export function applyThemeToDocument(theme: ResolvedTheme) {
  const root = document.documentElement;
  if (root.dataset.theme !== theme) root.dataset.theme = theme;
}
