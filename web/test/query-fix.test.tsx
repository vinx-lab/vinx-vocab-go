/** Fix round 1：在途时 invalidate、clearCache 与 GC、默认值与旧版一致 */
import { afterEach, describe, expect, it, vi } from "vitest";
import { render } from "preact";
import { act } from "preact/test-utils";
import { clearCache, invalidate, useApi } from "@/lib/query";

const flush = () => act(async () => { for (let i = 0; i < 5; i++) await new Promise((r) => setTimeout(r, 0)); });
function deferred<T>() {
  let resolve!: (v: T) => void;
  const p = new Promise<T>((r) => (resolve = r));
  return { p, resolve };
}

afterEach(() => {
  clearCache();
  vi.useRealTimers();
});

describe("I-1 在途请求时 invalidate", () => {
  it("作废在途请求并重新请求，旧响应不覆盖、不清除过期标记", async () => {
    const first = deferred<string>();
    const second = deferred<string>();
    const fn = vi.fn().mockReturnValueOnce(first.p).mockReturnValueOnce(second.p);
    let data: unknown;
    function A() {
      data = useApi(["books", "b1"], fn).data;
      return null;
    }
    const el = document.createElement("div");
    await act(() => render(<A />, el));
    expect(fn).toHaveBeenCalledTimes(1);
    // 删除单元的 mutation 成功后立刻失效：此时首个请求还在途
    let done = false;
    const inv = invalidate(["books"]).then(() => (done = true));
    expect(fn).toHaveBeenCalledTimes(2);
    second.resolve("删除后");
    await flush();
    first.resolve("删除前"); // 旧请求后到
    await flush();
    await inv;
    expect(done).toBe(true);
    expect(data).toBe("删除后");
    render(null, el);
  });

  it("refetch() 也强制重新请求", async () => {
    const fn = vi.fn().mockResolvedValueOnce(1).mockResolvedValueOnce(2);
    let s: ReturnType<typeof useApi<number>> | undefined;
    function A() {
      s = useApi(["r"], fn);
      return null;
    }
    const el = document.createElement("div");
    await act(() => render(<A />, el));
    await act(() => s!.refetch());
    await flush();
    expect(fn).toHaveBeenCalledTimes(2);
    expect(s!.data).toBe(2);
    render(null, el);
  });
});

describe("I-2 clearCache 与 GC 定时器", () => {
  it("旧组件卸载留下的 GC 定时器不会删掉换账号后的新条目", async () => {
    vi.useFakeTimers();
    const fnA = vi.fn().mockResolvedValue("A 的今日");
    function Old() {
      useApi(["today"], fnA);
      return null;
    }
    const el = document.createElement("div");
    await act(() => render(<Old />, el));
    await act(async () => { await vi.advanceTimersByTimeAsync(0); });
    // 学生 A 退出：清缓存、页面卸载（旧条目挂上 5 分钟 GC）
    clearCache();
    await act(() => render(null, el));
    // 学生 B 登录，打开今日
    const fnB = vi.fn().mockResolvedValueOnce("B 的今日").mockResolvedValueOnce("B 练习后");
    let data: unknown;
    function New() {
      data = useApi(["today"], fnB).data;
      return null;
    }
    await act(() => render(<New />, el));
    await act(async () => { await vi.advanceTimersByTimeAsync(0); });
    expect(data).toBe("B 的今日");
    await act(async () => { await vi.advanceTimersByTimeAsync(6 * 60_000); });
    // 新条目还在缓存里：失效后能刷新
    await act(async () => { await invalidate(["today"]); });
    expect(data).toBe("B 练习后");
    render(null, el);
  });

  it("clearCache 时仍挂载的组件改用新条目重新请求，不再显示上一个账号的数据", async () => {
    const fn = vi.fn().mockResolvedValueOnce("旧账号").mockResolvedValueOnce("新账号");
    let data: unknown;
    function A() {
      data = useApi(["me"], fn, { keepPrevious: false }).data;
      return null;
    }
    const el = document.createElement("div");
    await act(() => render(<A />, el));
    await flush();
    expect(data).toBe("旧账号");
    await act(() => clearCache());
    await flush();
    expect(fn).toHaveBeenCalledTimes(2);
    expect(data).toBe("新账号");
    render(null, el);
  });

  it("check() 失败（401）清空缓存", async () => {
    const { check } = await import("@/lib/auth");
    const { setQueryData, getQueryData } = await import("@/lib/query");
    setQueryData(["today"], "上个账号");
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: false, status: 401, json: async () => ({ success: false, error: { code: "UNAUTHORIZED", message: "未登录" } }) }));
    expect(await check()).toBe(false);
    expect(getQueryData(["today"])).toBeUndefined();
    vi.unstubAllGlobals();
  });
});

describe("I-3 默认值与旧版（refine 注入的 QueryClient）一致", () => {
  it("换 key 时保留上一个 key 的数据（分页不闪空）", async () => {
    const pages: Record<number, ReturnType<typeof deferred<string>>> = { 1: deferred(), 2: deferred() };
    let s: ReturnType<typeof useApi<string>> | undefined;
    function List({ page }: { page: number }) {
      s = useApi(["list", page], () => pages[page].p);
      return null;
    }
    const el = document.createElement("div");
    await act(() => render(<List page={1} />, el));
    pages[1].resolve("第 1 页");
    await flush();
    await act(() => render(<List page={2} />, el));
    expect(s!.data).toBe("第 1 页");
    expect(s!.isPlaceholderData).toBe(true);
    expect(s!.isLoading).toBe(false);
    expect(s!.isFetching).toBe(true);
    pages[2].resolve("第 2 页");
    await flush();
    expect(s!.data).toBe("第 2 页");
    expect(s!.isPlaceholderData).toBe(false);
    render(null, el);
  });

  it("keepPrevious:false 时换 key 先变空", async () => {
    let s: ReturnType<typeof useApi<string>> | undefined;
    function List({ page }: { page: number }) {
      s = useApi(["np", page], () => new Promise<string>(() => {}), { keepPrevious: false, initialData: page === 1 ? "一" : undefined });
      return null;
    }
    const el = document.createElement("div");
    await act(() => render(<List page={1} />, el));
    await act(() => render(<List page={2} />, el));
    expect(s!.data).toBeUndefined();
    render(null, el);
  });

  it("默认不在窗口聚焦时重取；显式开启才重取", async () => {
    const fn = vi.fn().mockResolvedValue(1);
    const on = vi.fn().mockResolvedValue(1);
    function A() {
      useApi(["focus-off"], fn);
      useApi(["focus-on"], on, { refetchOnWindowFocus: true });
      return null;
    }
    const el = document.createElement("div");
    await act(() => render(<A />, el));
    await flush();
    await new Promise((r) => setTimeout(r, 5));
    await act(() => { window.dispatchEvent(new Event("focus")); });
    await flush();
    expect(fn).toHaveBeenCalledTimes(1);
    expect(on).toHaveBeenCalledTimes(2);
    render(null, el);
  });
});

describe("New-1 clearCache 后不把上一个账号的数据当占位", () => {
  it("默认选项（keepPrevious=true）下，挂载中的组件在 clearCache 后到新数据返回前不暴露旧数据", async () => {
    const next = deferred<string>();
    const fn = vi.fn().mockResolvedValueOnce("A 的数据").mockReturnValueOnce(next.p);
    let s: ReturnType<typeof useApi<string>> | undefined;
    const seen: unknown[] = [];
    function A() {
      s = useApi(["today"], fn);
      seen.push(s.data);
      return null;
    }
    const el = document.createElement("div");
    await act(() => render(<A />, el));
    await flush();
    expect(s!.data).toBe("A 的数据");
    seen.length = 0;
    await act(() => clearCache());
    await flush();
    expect(fn).toHaveBeenCalledTimes(2);
    expect(s!.data).toBeUndefined();
    expect(s!.isPlaceholderData).toBe(false);
    expect(s!.isLoading).toBe(true);
    expect(seen).not.toContain("A 的数据");
    next.resolve("B 的数据");
    await flush();
    expect(s!.data).toBe("B 的数据");
    render(null, el);
  });

  it("clearCache 后换 key 也不带出旧数据", async () => {
    const fn = vi.fn((k: number) => (k === 1 ? Promise.resolve("旧") : new Promise<string>(() => {})));
    let s: ReturnType<typeof useApi<string>> | undefined;
    function L({ k }: { k: number }) {
      s = useApi(["k", k], () => fn(k));
      return null;
    }
    const el = document.createElement("div");
    await act(() => render(<L k={1} />, el));
    await flush();
    expect(s!.data).toBe("旧");
    clearCache();
    await act(() => render(<L k={2} />, el));
    expect(s!.data).toBeUndefined();
    render(null, el);
  });
});

describe("New-2 与本 hook 无关的 removeQueries 不影响占位数据", () => {
  it("翻页等待中，removeQueries(其他前缀) + 重渲染后仍显示上一页，也不触发额外请求", async () => {
    const p2 = deferred<string>();
    const fn = vi.fn((page: number) => (page === 1 ? Promise.resolve("第 1 页") : p2.p));
    let s: ReturnType<typeof useApi<string>> | undefined;
    function List({ page, n }: { page: number; n: number }) {
      s = useApi(["words", page], () => fn(page));
      return <span>{n}</span>;
    }
    const el = document.createElement("div");
    await act(() => render(<List page={1} n={0} />, el));
    await flush();
    await act(() => render(<List page={2} n={0} />, el));
    expect(s!.data).toBe("第 1 页");
    expect(s!.isPlaceholderData).toBe(true);
    const { removeQueries } = await import("@/lib/query");
    await act(() => removeQueries(["classes"]));
    await act(() => render(<List page={2} n={1} />, el));
    await flush();
    expect(s!.data).toBe("第 1 页");
    expect(s!.isPlaceholderData).toBe(true);
    expect(fn).toHaveBeenCalledTimes(2);
    p2.resolve("第 2 页");
    await flush();
    expect(s!.data).toBe("第 2 页");
    render(null, el);
  });

  it("removeQueries 命中本 hook 的来源条目时丢弃占位", async () => {
    const fn = vi.fn((page: number) => (page === 1 ? Promise.resolve("第 1 页") : new Promise<string>(() => {})));
    let s: ReturnType<typeof useApi<string>> | undefined;
    function List({ page }: { page: number }) {
      s = useApi(["w2", page], () => fn(page));
      return null;
    }
    const el = document.createElement("div");
    await act(() => render(<List page={1} />, el));
    await flush();
    await act(() => render(<List page={2} />, el));
    expect(s!.data).toBe("第 1 页");
    const { removeQueries } = await import("@/lib/query");
    await act(() => removeQueries(["w2", 1]));
    // 本 hook 已不订阅第 1 页条目，下次渲染时丢弃占位
    await act(() => render(<List page={2} />, el));
    expect(s!.data).toBeUndefined();
    expect(s!.isPlaceholderData).toBe(false);
    render(null, el);
  });
});
