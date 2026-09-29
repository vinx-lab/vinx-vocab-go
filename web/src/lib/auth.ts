/**
 * 登录态（替代 refine authProvider）：
 * - 服务端种 HttpOnly cookie，前端只缓存身份快照 localStorage.vinx_user（不缓存 token）
 * - 受保护区域首次渲染时走一次 /auth/me 校验（cookie 模式只能网络校验）
 * - 外观偏好以账号为准，拿到账号信息后同步到本地缓存
 */
import { useEffect, useState } from "preact/hooks";
import type { User } from "@vinx/shared";
import { api } from "@/lib/api";
import { homePath } from "@/lib/perms";
import { syncThemeFromAccount } from "@/lib/theme";
import { clearCache } from "@/lib/query";

const STORAGE_KEY = "vinx_user";

type AuthStatus = "unknown" | "checking" | "in" | "out";
let status: AuthStatus = "unknown";
let identity: User | null = readCached();
const subs = new Set<() => void>();
const emit = () => subs.forEach((f) => f());

function readCached(): User | null {
  try {
    const v = localStorage.getItem(STORAGE_KEY);
    return v ? (JSON.parse(v) as User) : null;
  } catch {
    return null;
  }
}
function store(user: User | null) {
  identity = user;
  try {
    if (user) localStorage.setItem(STORAGE_KEY, JSON.stringify(user));
    else localStorage.removeItem(STORAGE_KEY);
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
  } catch {
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
  if (identity && identity.id !== user.id) clearCache();
  status = "in";
  store(user);
  syncThemeFromAccount(user.theme);
}

export async function logout() {
  try {
    await api.post("/auth/logout");
  } catch {
    /* 忽略：JWT 无状态，客户端清 storage 即可 */
  }
  // 外观缓存 vinx_theme 不清：同一设备的登录页仍按上次外观显示
  status = "out";
  store(null);
  clearCache();
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
