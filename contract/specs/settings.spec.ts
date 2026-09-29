/**
 * 系统设置 · AI（旧 apps/api/tests/settings.flow.test.ts 改写为纯 HTTP 黑盒）：权限、优先级（数据库
 * 优先于环境变量）、Key 不外泄（响应体不含明文）、测试连接不保存、保存后 /config 的 ai 立即变化。
 *
 * 两个目标（oracle 4100、Go）在启动时 AI_BASE_URL / AI_API_KEY 都是空的，所以恢复为环境变量后
 * source 应为 "env"、enabled 为 false —— 用这一点在测试结束时验证已经把设置还原干净。
 * 用例结束前一律 DELETE /settings/ai，不影响其他并发跑的 agent。
 */
import { afterAll, describe, expect, it } from "vitest";
import { anon, login } from "../lib/client";
import { ready } from "../lib/areas";
import { closedPortUrl, startFakeAi, type FakeAi } from "../lib/fake-ai";

const KEY = "sk-contract-settings-secret-7788";

describe.runIf(ready("settings"))("系统设置 · AI", () => {
  let fake: FakeAi;

  afterAll(async () => {
    const admin = await login("admin@vinx.test");
    await admin.del("/settings/ai");
    await fake?.close();
  });

  it("非管理员 403，未登录 401", async () => {
    const teacher = await login("teacher@vinx.test");
    const student = await login("student@vinx.test");
    expect((await teacher.get("/settings/ai")).status).toBe(403);
    expect((await student.put("/settings/ai", { provider: "off" })).status).toBe(403);
    expect((await student.del("/settings/ai")).status).toBe(403);
    expect((await student.post("/settings/ai/test", { provider: "openai", baseUrl: "http://127.0.0.1:1/v1" })).status).toBe(403);
    expect((await anon().get("/settings/ai")).status).toBe(401);
  });

  it("没有保存过时来源是环境变量", async () => {
    // provider 具体是什么取决于被测实例启动时 AI_PROVIDER 的字面值（oracle 的 .env 与 Go 契约实例的
    // 启动参数可能不同：AI_PROVIDER 不是 "auto" 时 selected = AI_PROVIDER 本身，即使没配 baseUrl/Key
    // 也照样显示该值，只是 enabled 为假）；这里只断言两边一致的部分：来源与「未配置时不可用」。
    const admin = await login("admin@vinx.test");
    await admin.del("/settings/ai"); // 清干净，防止前面用例的残留
    const r = await admin.get("/settings/ai");
    expect(r.status).toBe(200);
    expect(r.body.data).toMatchObject({ source: "env", enabled: false, apiKey: { set: false, last4: null } });
  });

  it("测试连接：用填写的值，不保存；连不上时返回失败原因且不含 Key", async () => {
    fake = await startFakeAi();
    const admin = await login("admin@vinx.test");

    const ok = await admin.post("/settings/ai/test", { provider: "openai", baseUrl: fake.url, apiKey: KEY, model: "fake-model", timeoutSec: 10 });
    expect(ok.status).toBe(200);
    expect(ok.body.data.ok).toBe(true);
    expect(typeof ok.body.data.ms).toBe("number");
    expect(fake.requests).toHaveLength(1);
    expect(fake.requests[0].auth).toBe(`Bearer ${KEY}`);
    expect(fake.requests[0].model).toBe("fake-model");
    // 不保存
    expect((await admin.get("/settings/ai")).body.data.source).toBe("env");

    const closed = await closedPortUrl();
    const bad = await admin.post("/settings/ai/test", { provider: "openai", baseUrl: closed, apiKey: KEY, model: "m", timeoutSec: 5 });
    expect(bad.status).toBe(200);
    expect(bad.body.data.ok).toBe(false);
    expect(bad.body.data.error).toBeTruthy();
    expect(bad.text).not.toContain(KEY);
  });

  it("保存后 /config 的 ai 立即变化；GET 不返回完整 Key；Key 留空沿用原来的", async () => {
    const admin = await login("admin@vinx.test");

    const off = await admin.put("/settings/ai", { provider: "off" });
    expect(off.status).toBe(200);
    expect(off.body.data).toMatchObject({ source: "db", provider: "off", enabled: false });
    expect((await admin.get("/config")).body.data.ai).toBe(false);

    const saved = await admin.put("/settings/ai", { provider: "openai", baseUrl: fake.url, apiKey: KEY, model: "fake-model", timeoutSec: 30 });
    expect(saved.status).toBe(200);
    expect(saved.text).not.toContain(KEY);
    expect(saved.body.data).toMatchObject({
      source: "db",
      provider: "openai",
      baseUrl: fake.url,
      model: "fake-model",
      timeoutSec: 30,
      enabled: true,
      apiKey: { set: true, last4: "7788" },
    });
    expect((await admin.get("/config")).body.data.ai).toBe(true);

    const got = await admin.get("/settings/ai");
    expect(got.text).not.toContain(KEY);

    fake.requests.length = 0;
    const kept = await admin.put("/settings/ai", { provider: "openai", baseUrl: fake.url, apiKey: "", model: "fake-model-2", timeoutSec: 30 });
    expect(kept.status).toBe(200);
    expect(kept.body.data.apiKey).toEqual({ set: true, last4: "7788" });
    const test = await admin.post("/settings/ai/test", { provider: "openai", baseUrl: fake.url, model: "fake-model-2" });
    expect(test.body.data.ok).toBe(true);
    expect(fake.requests[0]?.auth).toBe(`Bearer ${KEY}`);
  });

  it("恢复为环境变量后回到环境变量的配置", async () => {
    const admin = await login("admin@vinx.test");
    const r = await admin.del("/settings/ai");
    expect(r.status).toBe(200);
    expect(r.body.data.source).toBe("env");
    expect((await admin.get("/config")).body.data.ai).toBe(r.body.data.enabled);
  });

  it("参数校验：OpenAI 兼容要接口地址，Claude 要 Key，超时范围", async () => {
    const admin = await login("admin@vinx.test");
    const noBase = await admin.put("/settings/ai", { provider: "openai", baseUrl: "" });
    expect(noBase.status).toBe(400);
    expect(noBase.body.error.code).toBe("VALIDATION");
    const noKey = await admin.put("/settings/ai", { provider: "anthropic", clearApiKey: true });
    expect(noKey.status).toBe(400);
    const badTimeout = await admin.put("/settings/ai", { provider: "off", timeoutSec: 1 });
    expect(badTimeout.status).toBe(400);
    await admin.del("/settings/ai");
  });
});
