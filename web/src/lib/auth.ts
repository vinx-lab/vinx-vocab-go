/**
 * 登录态（替代 refine authProvider）：
 * - 服务端种 HttpOnly cookie，前端只缓存身份快照 localStorage.vinx_user（不缓存 token）
 * - 开发模式「仅本标签页」例外：令牌与身份快照都在 sessionStorage（见 tabSession.ts）
 * - 受保护区域首次渲染时走一次 /auth/me 校验（cookie 模式只能网络校验）
 * - 外观偏好以账号为准，拿到账号信息后同步到本地缓存
 */
import { useEffect, useState } from "preact/hooks";
import { normalizeThemePref, type User } from "@vinx/shared";
import { api, type ApiError } from "@/lib/api";
import { homePath } from "@/lib/perms";
import { THEME_STORAGE_KEY, readCachedTheme, syncThemeFromAccount, writeCachedTheme } from "@/lib/theme";
import { clearCache } from "@/lib/query";
import { isTabSession, setTabToken, userStorage } from "@/lib/tabSession";

const STORAGE_KEY = "vinx_user";

type AuthStatus = "unknown" | "checking" | "in" | "out";
let status: AuthStatus = "unknown";
let identity: User | null = readCached();
const subs = new Set<() => void>();
const emit = () => subs.forEach((f) => f());

function readCached(): User | null {
  try {
    const v = userStorage().getItem(STORAGE_KEY);
    return v ? (JSON.parse(v) as User) : null;
  } catch {
    return null;
  }
}
function store(user: User | null) {
  identity = user;
  try {
    const s = userStorage();
    if (user) s.setItem(STORAGE_KEY, JSON.stringify(user));
    else s.removeItem(STORAGE_KEY);
  } catch {
    /* 隐私模式写不进去：只影响刷新后的首帧 */
  }
  emit();
}

/** 改了账号字段（如外观）后同步本地身份快照 */
export function patchCachedUser(patch: Partial<User>) {
  if (identity) store({ ...identity, ...patch });
}

/** 网络校验 /auth/me；成功更新身份，失败清空 */
export async function check(): Promise<boolean> {
  // 已登录时后台静默刷新，不让受保护区域闪成空白
  if (status !== "in") {
    status = "checking";
    emit();
  }
  try {
    const user = await api.get<User>("/auth/me");
    status = "in";
    store(user);
    syncThemeFromAccount(user.theme);
    return true;
  } catch (e) {
    // 开发模式「仅本标签页」的令牌失效：退出本标签页模式，回到 Cookie 上的账号再校验一次
    if (isTabSession() && (e as ApiError)?.statusCode === 401) {
      dropTabSession();
      identity = readCached();
      writeCachedTheme(readCachedTheme());
      return check();
    }
    // 登录态失效（如会话过期）：清掉上一个账号的缓存，换账号登录时不会先闪出旧数据
    status = "out";
    store(null);
    clearCache();
    return false;
  }
}

/** 登录；返回登录后的落地页 */
export async function login(email: string, password: string): Promise<string> {
  const data = await api.post<{ user: User }>("/auth/login", { email, password });
  // 正常登录写的是 Cookie：退出本标签页模式，否则本标签页的令牌会盖过新 Cookie
  dropTabSession();
  // 不经退出直接换账号时也清掉上一个账号的缓存
  if (identity && identity.id !== data.user.id) clearCache();
  status = "in";
  store(data.user);
  // 外观以账号为准，并刷新缓存
  syncThemeFromAccount(data.user.theme);
  return homePath(data.user);
}

/** 注册成功后记下身份（服务端已种 cookie） */
export function setSignedIn(user: User) {
  dropTabSession();
  if (identity && identity.id !== user.id) clearCache();
  status = "in";
  store(user);
  syncThemeFromAccount(user.theme);
}

/** 清掉本标签页的令牌与缓存（开发模式「仅本标签页」） */
function dropTabSession() {
  setTabToken(null);
  try {
    sessionStorage.removeItem(STORAGE_KEY);
    sessionStorage.removeItem(THEME_STORAGE_KEY);
  } catch {
    /* 忽略 */
  }
}

/**
 * 开发模式免密切换（spec 0002）；返回切换后的落地页。
 * tab：令牌存本标签页，身份与外观缓存存 sessionStorage；browser：与正常登录一样由服务端种 Cookie，并退出本标签页模式。
 */
export async function impersonate(userId: string, scope: "tab" | "browser"): Promise<string> {
  const data = await api.post<{ user: User; token?: string }>("/dev/impersonate", { userId, scope });
  dropTabSession();
  if (scope === "tab" && data.token) setTabToken(data.token);
  // 与正常换账号登录相同：清掉上一个账号的查询缓存
  clearCache();
  status = "in";
  store(data.user);
  // 切换存储位置后当前缓存可能已是别的值：外观以账号为准，并通知页面
  writeCachedTheme(normalizeThemePref(data.user.theme));
  return homePath(data.user);
}

/** 退出「仅本标签页」模式，回到 Cookie 上的账号；返回落地页（Cookie 未登录时为 /login） */
export async function restoreBrowserSession(): Promise<string> {
  dropTabSession();
  clearCache();
  status = "unknown";
  identity = readCached();
  // 外观先回到浏览器缓存，再由 /auth/me 按账号同步
  writeCachedTheme(readCachedTheme());
  const ok = await check();
  return ok ? homePath(identity) : "/login";
}

export async function logout(): Promise<string> {
  // 仅本标签页模式：只清本标签页的令牌，不调用 /auth/logout（否则 Cookie 上的账号也会被退出）
  if (isTabSession()) return restoreBrowserSession();
  try {
    await api.post("/auth/logout");
  } catch {
    /* 忽略：JWT 无状态，客户端清 storage 即可 */
  }
  // 外观缓存 vinx_theme 不清：同一设备的登录页仍按上次外观显示
  status = "out";
  store(null);
  clearCache();
  return "/login";
}

/** 当前身份（对应旧版 useGetIdentity）；refetch 重新走 /auth/me */
export function useIdentity(): { data: User | undefined; isLoading: boolean; status: AuthStatus; refetch: () => Promise<boolean> } {
  const [, set] = useState(0);
  useEffect(() => {
    const f = () => set((n) => n + 1);
    subs.add(f);
    return () => void subs.delete(f);
  }, []);
  return { data: identity ?? undefined, isLoading: status === "unknown" || status === "checking", status, refetch: check };
}

/** 受保护区域：首次进入时校验一次登录态 */
export function useAuthCheck(): AuthStatus {
  const { status: s } = useIdentity();
  useEffect(() => {
    if (status === "unknown") void check();
  }, []);
  return s;
}
