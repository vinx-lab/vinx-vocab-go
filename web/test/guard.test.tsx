/** 路由守卫随 /config 变化：版本切到个人版后，班级页被挡回首页（M-1 裁定保留 feature 守卫） */
import { describe, expect, it, vi } from "vitest";
import { render } from "preact";
import { act } from "preact/test-utils";
import { App } from "@/App";
import { ThemeProvider } from "@/components/ThemeProvider";
import { setQueryData } from "@/lib/query";

// preact 的 act 只在回调结束时冲刷副作用，所以分多次 act 推进
const flush = async () => {
  for (let i = 0; i < 15; i++) await act(async () => { await new Promise((r) => setTimeout(r, 10)); });
};
const school = { edition: "school", features: { classes: true, assignToOthers: true, multiUser: true, studentRecords: true }, ai: false, audio: false, signupEnabled: true };

describe("路由守卫", () => {
  it("班级版可进 /classes；/config 变为个人版后回到首页", async () => {
    vi.stubGlobal("fetch", vi.fn(async (url: string) => {
      const data = url.startsWith("/api/auth/me") ? { id: "t1", email: "teacher@vinx.test", name: "王老师", role: "teacher", theme: "light" } : url.startsWith("/api/config") ? school : {};
      return { ok: true, status: 200, json: async () => ({ success: true, data }) };
    }));
    history.replaceState(null, "", "/classes");
    const el = document.createElement("div");
    document.body.appendChild(el);
    await act(() => render(<ThemeProvider><App /></ThemeProvider>, el));
    await flush();
    expect(location.pathname).toBe("/classes");
    expect(el.textContent).toContain("班级");
    await act(() => setQueryData(["config"], { ...school, edition: "personal", features: { classes: false, assignToOthers: false, multiUser: false, studentRecords: false } }));
    await flush();
    // 教师有 study 能力，首页是 /today；侧栏也不再有「班级」
    expect(location.pathname).toBe("/today");
    expect(el.textContent).not.toContain("教学");
    await act(() => render(null, el));
    vi.unstubAllGlobals();
  });
});
