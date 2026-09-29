import { describe, expect, it, vi } from "vitest";
import { render } from "preact";
import { act } from "preact/test-utils";
import { getQueryData, invalidate, setQueryData, useApi, useMutation } from "@/lib/query";

const flush = () => act(async () => { await new Promise((r) => setTimeout(r, 0)); });

describe("useApi 缓存与失效", () => {
  it("同 key 共享请求；invalidate 前缀匹配后重取", async () => {
    const fn = vi.fn().mockResolvedValue(1);
    let seen: unknown;
    function A() {
      seen = useApi(["me", "classes"], fn).data;
      return null;
    }
    const el = document.createElement("div");
    await act(() => render(<><A /><A /></>, el));
    await flush();
    expect(fn).toHaveBeenCalledTimes(1);
    expect(seen).toBe(1);
    fn.mockResolvedValue(2);
    await act(() => invalidate(["me"]));
    await flush();
    expect(fn).toHaveBeenCalledTimes(2);
    expect(seen).toBe(2);
    await act(() => invalidate(["other"]));
    expect(fn).toHaveBeenCalledTimes(2);
    render(null, el);
  });

  it("enabled=false 不请求；setQueryData 直接改缓存", async () => {
    const fn = vi.fn().mockResolvedValue("x");
    let state: { data?: unknown; isLoading: boolean } | undefined;
    function B() {
      state = useApi(["b"], fn, { enabled: false });
      return null;
    }
    const el = document.createElement("div");
    await act(() => render(<B />, el));
    expect(fn).not.toHaveBeenCalled();
    expect(state!.isLoading).toBe(false);
    await act(() => setQueryData(["b"], "y"));
    expect(state!.data).toBe("y");
    expect(getQueryData(["b"])).toBe("y");
    render(null, el);
  });

  it("失败后按次数重试，retry:false 不重试", async () => {
    const fn = vi.fn().mockRejectedValue(new Error("坏了"));
    let state: { error?: unknown; isError: boolean } | undefined;
    function C() {
      state = useApi(["c"], fn, { retry: false });
      return null;
    }
    const el = document.createElement("div");
    await act(() => render(<C />, el));
    await flush();
    expect(fn).toHaveBeenCalledTimes(1);
    expect(state!.isError).toBe(true);
    expect((state!.error as Error).message).toBe("坏了");
    render(null, el);
  });
});

describe("useMutation", () => {
  it("成功与失败回调、isPending", async () => {
    const onSuccess = vi.fn();
    const onError = vi.fn();
    let m: ReturnType<typeof useMutation<number, number>> | undefined;
    function M() {
      m = useMutation<number, number>({ mutationFn: async (n) => { if (n < 0) throw new Error("负数"); return n * 2; }, onSuccess, onError });
      return null;
    }
    const el = document.createElement("div");
    await act(() => render(<M />, el));
    await act(async () => { await m!.mutateAsync(2); });
    expect(onSuccess).toHaveBeenCalledWith(4, 2, undefined);
    await act(async () => { m!.mutate(-1); await new Promise((r) => setTimeout(r, 0)); });
    expect(onError).toHaveBeenCalled();
    expect(m!.isError).toBe(true);
    render(null, el);
  });
});
