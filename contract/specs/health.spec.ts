/**
 * 由旧 apps/api/tests/health.test.ts 改写：健康检查、404 包络，以及 Fastify 的请求体解析边界。
 */
import { describe, it, expect } from "vitest";
import { anon } from "../lib/client";
import { expectShape } from "../lib/shape";

describe("health 冒烟", () => {
  it("GET /health 返回成功包络与 timestamp", async () => {
    const res = await anon().get("/health");
    expect(res.status).toBe(200);
    expect(res.headers.get("content-type")).toMatch(/^application\/json/);
    expect(res.body.success).toBe(true);
    expect(res.body.data).toEqual({ status: "ok" });
    expect(res.body.timestamp).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$/);
    expectShape("health", res.body);
  });

  it("未知路由返回统一错误包络（含 requestId）", async () => {
    const res = await anon().get("/no-such-route");
    expect(res.status).toBe(404);
    expect(res.body.success).toBe(false);
    expect(res.body.error.code).toBe("NOT_FOUND");
    expect(res.body.error.message).toBe("接口不存在");
    expect(typeof res.body.error.requestId).toBe("string");
    expect(typeof res.body.timestamp).toBe("string");
    expectShape("not-found", res.body);
  });

  it("路径存在但方法不对、多一个斜杠，同样是 404 包络", async () => {
    for (const r of [await anon().del("/health"), await anon().get("/health/"), await anon().post("/health", {})]) {
      expect(r.status).toBe(404);
      expect(r.body.error.code).toBe("NOT_FOUND");
    }
  });

  it("HEAD /health 可用", async () => {
    const res = await anon().call("HEAD", "/health");
    expect(res.status).toBe(200);
  });
});

describe("请求体解析（Fastify 语义）", () => {
  it("content-type 为 JSON 但请求体为空 → 400 VALIDATION（先于路由匹配与认证）", async () => {
    for (const path of ["/auth/login", "/auth/logout", "/no-such-route"]) {
      const res = await anon().call("POST", path, undefined, { headers: { "content-type": "application/json" } });
      expect(res.status).toBe(400);
      expect(res.body.error.code).toBe("VALIDATION");
      expect(res.body.error.message).toBe("Body cannot be empty when content-type is set to 'application/json'");
    }
  });

  it("非法 JSON → 400 VALIDATION", async () => {
    const res = await anon().call("POST", "/auth/login", undefined, { raw: "{bad", headers: { "content-type": "application/json" } });
    expect(res.status).toBe(400);
    expect(res.body.error.code).toBe("VALIDATION");
    expect(res.body.error.message).toBe("Body is not valid JSON but content-type is set to 'application/json'");
  });

  it("不支持的 content-type → 415", async () => {
    const res = await anon().call("POST", "/auth/login", undefined, { raw: "x=1", headers: { "content-type": "application/x-www-form-urlencoded" } });
    expect(res.status).toBe(415);
    expect(res.body.error).toMatchObject({ code: "VALIDATION", message: "Unsupported Media Type" });
  });

  it("text/plain 按字符串处理 → 字段校验失败", async () => {
    const res = await anon().call("POST", "/auth/login", undefined, { raw: "x", headers: { "content-type": "text/plain" } });
    expect(res.status).toBe(400);
    expect(res.body.error.details).toEqual({ body: "Expected object, received string" });
  });

  it("JSON 数组、null、类型不符的字段 → zod 风格的 details", async () => {
    const arr = await anon().call("POST", "/auth/login", undefined, { raw: "[]", headers: { "content-type": "application/json" } });
    expect(arr.status).toBe(400);
    expect(arr.body.error.details).toEqual({ body: "Expected object, received array" });

    const nul = await anon().call("POST", "/auth/login", undefined, { raw: "null", headers: { "content-type": "application/json" } });
    expect(nul.body.error.details).toEqual({ email: "Required", password: "Required" });

    const typed = await anon().post("/auth/login", { email: 5 });
    expect(typed.status).toBe(400);
    expect(typed.body.error).toMatchObject({
      code: "VALIDATION",
      message: "参数校验失败",
      details: { email: "Expected string, received number", password: "Required" },
    });
    expectShape("validation-error", typed.body);
  });

  it("GET 带 JSON content-type 不解析请求体", async () => {
    const res = await anon().call("GET", "/health", undefined, { headers: { "content-type": "application/json" } });
    expect(res.status).toBe(200);
  });
});
