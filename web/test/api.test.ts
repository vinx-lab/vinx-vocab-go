import { afterEach, describe, expect, it, vi } from "vitest";
import { api, fallbackMessage, type ApiError } from "@/lib/api";

const res = (status: number, body: unknown, json = true) => ({ ok: status < 400, status, json: json ? async () => body : async () => { throw new Error("not json"); } });

describe("api 包络", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("成功时解包 data，带 /api 前缀与查询参数，跳过空值", async () => {
    const f = vi.fn().mockResolvedValue(res(200, { success: true, data: { a: 1 } }));
    vi.stubGlobal("fetch", f);
    expect(await api.get("/units/u1/words", { page: 2, q: undefined, limit: 20 })).toEqual({ a: 1 });
    expect(f.mock.calls[0][0]).toBe("/api/units/u1/words?page=2&limit=20");
    expect(f.mock.calls[0][1].credentials).toBe("include");
  });

  it("post 没有 body 时发 {}（与旧版 axios 一致），GET 不带 Content-Type", async () => {
    const f = vi.fn().mockResolvedValue(res(200, { success: true, data: null }));
    vi.stubGlobal("fetch", f);
    await api.post("/x");
    expect(f.mock.calls[0][1].body).toBe("{}");
    await api.get("/y");
    expect(f.mock.calls[1][1].headers["Content-Type"]).toBeUndefined();
  });

  it("包络错误透传 message/code/statusCode/requestId/details", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(res(401, { success: false, error: { code: "INVALID_CREDENTIALS", message: "账号或密码错误", requestId: "r1", details: { x: 1 } } })));
    const e = (await api.post("/auth/login", {}).catch((x) => x)) as ApiError;
    expect(e.message).toBe("账号或密码错误");
    expect([e.statusCode, e.code, e.requestId, e.errors]).toEqual([401, "INVALID_CREDENTIALS", "r1", { x: 1 }]);
  });

  it("网关返回非 JSON 时给出网关错误说明", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(res(504, null, false)));
    const e = (await api.get("/x").catch((x) => x)) as ApiError;
    expect(e.message).toBe("网关错误（504）：请求等待时间过长或服务不可用");
    expect(e.statusCode).toBe(504);
  });

  it("fallbackMessage", () => {
    expect(fallbackMessage(undefined, false)).toBe("请求失败");
    expect(fallbackMessage(500, true)).toBe("请求失败（500）");
    expect(fallbackMessage(502, true)).toContain("网关错误（502）");
  });
});
