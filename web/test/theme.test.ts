import { beforeEach, describe, expect, it, vi } from "vitest";
import { normalizeThemePref } from "@vinx/shared";
import { login, logout, check } from "@/lib/auth";
import { THEME_EVENT, THEME_STORAGE_KEY, mergeThemePref, readCachedTheme, resolveTheme, setSavingTheme, syncThemeFromAccount, writeCachedTheme } from "@/lib/theme";

const ok = (data: unknown) => vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => ({ success: true, data }) });

describe("外观：实际主题", () => {
  it("跟随系统时看系统是否为深色", () => {
    expect(resolveTheme("system", true)).toBe("dark");
    expect(resolveTheme("system", false)).toBe("light");
  });
  it("明确选择时不受系统影响", () => {
    expect(resolveTheme("dark", false)).toBe("dark");
    expect(resolveTheme("light", true)).toBe("light");
  });
  it("非法值按跟随系统处理", () => {
    expect(normalizeThemePref("sepia")).toBe("system");
    expect(normalizeThemePref(undefined)).toBe("system");
  });
});

describe("外观：缓存与账号值合并", () => {
  it("账号有合法值时以账号为准，否则缓存，都没有跟随系统", () => {
    expect(mergeThemePref("light", "dark")).toBe("dark");
    expect(mergeThemePref("dark", undefined)).toBe("dark");
    expect(mergeThemePref("bogus", "also-bogus")).toBe("system");
  });
});

describe("外观：缓存读写与登录", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.unstubAllGlobals();
    setSavingTheme(null);
  });

  it("写缓存并通知当前页面", () => {
    const seen: string[] = [];
    const on = (e: Event) => seen.push((e as CustomEvent<string>).detail);
    window.addEventListener(THEME_EVENT, on);
    writeCachedTheme("dark");
    window.removeEventListener(THEME_EVENT, on);
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe("dark");
    expect(seen).toEqual(["dark"]);
  });

  it("账号值覆盖缓存；保存中不被旧的账号值覆盖", () => {
    writeCachedTheme("light");
    syncThemeFromAccount("dark");
    expect(readCachedTheme()).toBe("dark");
    setSavingTheme("light");
    writeCachedTheme("light");
    syncThemeFromAccount("dark");
    expect(readCachedTheme()).toBe("light");
  });

  it("登录后以账号值为准，退出登录不清外观缓存", async () => {
    localStorage.setItem(THEME_STORAGE_KEY, "light");
    vi.stubGlobal("fetch", ok({ user: { id: "u1", email: "s@t", name: "学生", role: "student", theme: "dark" } }));
    expect(await login("s@t", "123456")).toBe("/today");
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe("dark");
    vi.stubGlobal("fetch", ok(undefined));
    await logout();
    expect(localStorage.getItem("vinx_user")).toBeNull();
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe("dark");
  });

  it("/auth/me 返回的账号值刷新缓存", async () => {
    localStorage.setItem(THEME_STORAGE_KEY, "dark");
    vi.stubGlobal("fetch", ok({ id: "u1", email: "s@t", name: "学生", role: "student", theme: "system" }));
    expect(await check()).toBe(true);
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe("system");
  });
});
