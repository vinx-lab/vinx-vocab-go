/**
 * 由旧 apps/api/tests/edition.test.ts 改写。
 *
 * 旧测试在同一进程里分别以 personal / school 构建实例；黑盒测试只能测「正在运行的实例」：
 * - BASE_URL 指向的实例按它 /config 报告的版本断言；
 * - 设了 PERSONAL_BASE_URL 时，另外对该实例（以 VINX_EDITION=personal 启动）跑个人版断言。
 */
import { describe, it, expect } from "vitest";
import { anon, BASE_URL } from "../lib/client";
import { goOnly, ready } from "../lib/areas";
import { expectShape } from "../lib/shape";

const SCHOOL = { classes: true, assignToOthers: true, multiUser: true, studentRecords: true };
const PERSONAL = { classes: false, assignToOthers: false, multiUser: false, studentRecords: false };

async function editionOf(base: string) {
  const res = await anon(base).get("/config");
  expect(res.status).toBe(200);
  return res.body.data as { edition: string; features: Record<string, boolean>; ai: boolean; audio: boolean; signupEnabled: boolean };
}

describe("版本（edition）", () => {
  it("/config 结构：edition、features、ai、audio、signupEnabled（无需登录）", async () => {
    const res = await anon().get("/config");
    expect(res.status).toBe(200);
    const d = res.body.data;
    expect(["personal", "school"]).toContain(d.edition);
    expect(typeof d.ai).toBe("boolean");
    expect(typeof d.audio).toBe("boolean");
    expect(typeof d.signupEnabled).toBe("boolean");
    expect(d.features).toEqual(d.edition === "school" ? SCHOOL : PERSONAL);
    if (goOnly) {
      // Go 版新增的两个字段（版本锁定、首次运行向导），oracle 没有；比对结构前去掉
      expect(typeof d.editionLocked).toBe("boolean");
      expect(typeof d.needsSetup).toBe("boolean");
      const { editionLocked: _l, needsSetup: _s, ...rest } = d;
      expectShape("config", { ...res.body, data: rest });
    } else {
      expectShape("config", res.body);
    }
  });

  it("班级版：/config 返回全部功能，班级与用户接口存在（未登录 401 而不是 404）", async () => {
    const cfg = await editionOf(BASE_URL);
    if (cfg.edition !== "school") return;
    expect(cfg).toMatchObject({ edition: "school", features: { classes: true, multiUser: true } });
    // 班级版默认开放注册
    expect(cfg.signupEnabled).toBe(true);
    if (ready("classes")) expect((await anon().get("/classes")).status).toBe(401);
    if (ready("users")) expect((await anon().get("/users")).status).toBe(401);
  });

  it("学习相关接口在两个版本都存在", async () => {
    if (ready("study")) expect((await anon().get("/today")).status).toBe(401);
    if (ready("sheets")) expect((await anon().get("/sheets")).status).toBe(401);
    expect((await anon().get("/health")).status).toBe(200);
  });

  const personalBase = process.env.PERSONAL_BASE_URL;
  it.runIf(Boolean(personalBase))("个人版：关闭班级与多用户，相关接口不存在（404）", async () => {
    const base = personalBase!;
    const cfg = await editionOf(base);
    expect(cfg).toMatchObject({ edition: "personal", features: PERSONAL });
    // Go：个人版实例以 VINX_EDITION=personal 启动，版本锁定、不进向导，界面上不能切换
    if (goOnly) {
      expect(cfg).toMatchObject({ editionLocked: true, needsSetup: false });
      expect((await anon(base).post("/setup/edition", { edition: "school" })).body.error).toMatchObject({
        code: "FORBIDDEN",
        message: "版本由 VINX_EDITION 指定，不能在界面上切换",
      });
    }
    for (const path of ["/classes", "/users"]) {
      const res = await anon(base).get(path);
      expect(res.status, path).toBe(404);
      expect(res.body.error.code).toBe("NOT_FOUND");
    }
    if (ready("classes")) expect((await anon(base).post("/classes/join", { inviteCode: "DEMO01" })).status).toBe(404);
    if (ready("study")) expect((await anon(base).get("/today")).status).toBe(401);
    if (ready("sheets")) expect((await anon(base).get("/sheets")).status).toBe(401);
    expect((await anon(base).get("/health")).status).toBe(200);
    expect((await anon(base).get("/auth/me")).status).toBe(401);
    // 个人版已有账号：关闭注册，提示直接登录
    if (!cfg.signupEnabled) {
      const res = await anon(base).post("/auth/signup", { email: `p-${Date.now()}@vinx.test`, password: "123456" });
      expect(res.status).toBe(403);
      expect(res.body.error).toMatchObject({ code: "FORBIDDEN", message: "个人版只允许一个账号，请直接登录" });
    }
  });
});
