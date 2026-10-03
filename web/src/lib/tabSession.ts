/**
 * 开发模式「仅本标签页」登录（spec 0002）：
 * 令牌放在 sessionStorage（每个标签页各自独立，刷新、前进后退都还在，关闭标签页即消失），
 * api.ts 发请求时有它就加 Authorization: Bearer，后端先认 Bearer 再认 Cookie。
 * 这种模式下身份快照 vinx_user 与外观缓存 vinx_theme 也存到 sessionStorage，不影响其他标签页。
 */
export const TAB_TOKEN_KEY = "vinx_tab_token";

export function getTabToken(): string | null {
  try {
    return sessionStorage.getItem(TAB_TOKEN_KEY);
  } catch {
    return null;
  }
}

export function setTabToken(token: string | null) {
  try {
    if (token) sessionStorage.setItem(TAB_TOKEN_KEY, token);
    else sessionStorage.removeItem(TAB_TOKEN_KEY);
  } catch {
    /* sessionStorage 不可用：仅本标签页模式不可用，回落到 Cookie */
  }
}

export function isTabSession(): boolean {
  return !!getTabToken();
}

/** 身份快照与外观缓存所在的存储：仅本标签页模式下用 sessionStorage，否则 localStorage */
export function userStorage(): Storage {
  return isTabSession() ? sessionStorage : localStorage;
}
