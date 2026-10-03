import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "@/lib/api";
import { TAB_TOKEN_KEY, getTabToken, isTabSession, setTabToken, userStorage } from "@/lib/tabSession";
import { check, impersonate, login, logout, restoreBrowserSession } from "@/lib/auth";
import { THEME_STORAGE_KEY, readCachedTheme } from "@/lib/theme";
import { homePath } from "@/lib/perms";

const ok = (data: unknown) => ({ ok: true, status: 200, json: async () => ({ success: true, data }) });
const user = (id: string, theme = "system", role = "student") => ({ id, email: `${id}@x.test`, name: id, role, currentGrade: null, theme, createdAt: "", capabilities: role === "student" ? ["study"] : ["classes"] });

describe("开发模式：仅本标签页令牌", () => {
  beforeEach(() => {
    localStorage.clear();
    sessionStorage.clear();
  });
  afterEach(() => vi.unstubAllGlobals());

  it("有本标签页令牌时带 Bearer 头，没有时不带", async () => {
    const f = vi.fn().mockResolvedValue(ok(null));
    vi.stubGlobal("fetch", f);
    await api.get("/x");
    expect(f.mock.calls[0][1].headers.Authorization).toBeUndefined();
    setTabToken("tok-1");
    expect(sessionStorage.getItem(TAB_TOKEN_KEY)).toBe("tok-1");
    expect(isTabSession()).toBe(true);
    await api.post("/y");
    expect(f.mock.calls[1][1].headers.Authorization).toBe("Bearer tok-1");
    setTabToken(null);
    expect(getTabToken()).toBeNull();
    await api.get("/z");
    expect(f.mock.calls[2][1].headers.Authorization).toBeUndefined();
  });

  it("仅本标签页模式下身份快照与外观缓存存到 sessionStorage", async () => {
    localStorage.setItem(THEME_STORAGE_KEY, "light");
    expect(userStorage()).toBe(localStorage);
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(ok({ user: user("t1", "dark", "teacher"), token: "tok-t" })));
    const to = await impersonate("t1", "tab");
    expect(to).toBe(homePath({ role: "teacher" }));
    expect(getTabToken()).toBe("tok-t");
    expect(userStorage()).toBe(sessionStorage);
    expect(JSON.parse(sessionStorage.getItem("vinx_user")!).id).toBe("t1");
    expect(localStorage.getItem("vinx_user")).toBeNull();
    expect(sessionStorage.getItem(THEME_STORAGE_KEY)).toBe("dark");
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe("light");
    expect(readCachedTheme()).toBe("dark");
  });

  it("仅本标签页模式下退出登录：只清本标签页令牌，不调用 /auth/logout，回到浏览器账号", async () => {
    setTabToken("tok-s");
    sessionStorage.setItem("vinx_user", JSON.stringify(user("s1")));
    const f = vi.fn().mockResolvedValue(ok(user("browser-admin", "system", "admin")));
    vi.stubGlobal("fetch", f);
    await logout();
    expect(getTabToken()).toBeNull();
    expect(sessionStorage.getItem("vinx_user")).toBeNull();
    const urls = f.mock.calls.map((c) => c[0]);
    expect(urls).not.toContain("/api/auth/logout");
    expect(urls).toContain("/api/auth/me");
    expect(f.mock.calls.every((c) => c[1].headers.Authorization === undefined)).toBe(true);
  });

  it("恢复为浏览器账号：清掉本标签页令牌与缓存，按 Cookie 重新校验", async () => {
    setTabToken("tok-s");
    sessionStorage.setItem(THEME_STORAGE_KEY, "dark");
    const f = vi.fn().mockResolvedValue(ok(user("b1")));
    vi.stubGlobal("fetch", f);
    const to = await restoreBrowserSession();
    expect(to).toBe("/today");
    expect(getTabToken()).toBeNull();
    expect(sessionStorage.getItem(THEME_STORAGE_KEY)).toBeNull();
    expect(f.mock.calls[0][0]).toBe("/api/auth/me");
  });

  it("本标签页令牌失效（401）时回到 Cookie 上的账号；正常登录退出本标签页模式", async () => {
    setTabToken("tok-expired");
    const f = vi
      .fn()
      .mockResolvedValueOnce({ ok: false, status: 401, json: async () => ({ success: false, error: { code: "VALIDATION", message: "expired" } }) })
      .mockResolvedValueOnce(ok(user("cookie-user")));
    vi.stubGlobal("fetch", f);
    expect(await check()).toBe(true);
    expect(getTabToken()).toBeNull();
    expect(f.mock.calls[0][1].headers.Authorization).toBe("Bearer tok-expired");
    expect(f.mock.calls[1][1].headers.Authorization).toBeUndefined();

    setTabToken("tok-x");
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(ok({ user: user("me") })));
    await login("me@x.test", "pw");
    expect(getTabToken()).toBeNull();
    expect(JSON.parse(localStorage.getItem("vinx_user")!).id).toBe("me");
  });

  it("整个浏览器：清掉本标签页令牌，身份写回 localStorage", async () => {
    setTabToken("tok-old");
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(ok({ user: user("s2") })));
    await impersonate("s2", "browser");
    expect(getTabToken()).toBeNull();
    expect(JSON.parse(localStorage.getItem("vinx_user")!).id).toBe("s2");
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(ok(user("s2"))));
    expect(await check()).toBe(true);
  });
});
