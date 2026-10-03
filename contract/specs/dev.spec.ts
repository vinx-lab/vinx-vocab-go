/**
 * 开发模式免密切换（spec 0002，只在 Go 上跑）。
 *
 * - BASE_URL 指向的实例按常规启动（不加 --dev）：/dev/* 与未知路径一样 404，/config 的 dev 为 false。
 * - 设了 DEV_BASE_URL 时，另外对该实例（以 `serve --dev` 启动、seed-demo 数据）验证：
 *   /config 的 dev 为 true；/dev/users 无需登录、按角色再按邮箱排序；
 *   scope=tab 只返回令牌、不写 Cookie，令牌用 Bearer 头能访问 /auth/me；scope=browser 与正常登录一样写 Cookie；
 *   外站 Origin 的切换请求同样被 Origin 检查拦下。
 */
import { describe, expect, it } from "vitest";
import { anon, login } from "../lib/client";
import { goOnly } from "../lib/areas";
import { expectShape } from "../lib/shape";

const DEV_BASE_URL = process.env.DEV_BASE_URL;

describe.runIf(goOnly)("开发模式：未加 --dev 的实例", () => {
  it("/config 的 dev 为 false", async () => {
    const res = await anon().get("/config");
    expect(res.status).toBe(200);
    expect(res.body.data.dev).toBe(false);
  });

  it("/dev/* 不注册：与未知路径相同的 404 包络", async () => {
    const admin = await login("admin@vinx.test");
    const student = await login("student@vinx.test");
    const me = (await student.get("/auth/me")).body.data as { id: string };
    for (const r of [
      await anon().get("/dev/users"),
      await admin.get("/dev/users"),
      await anon().post("/dev/impersonate", { userId: me.id, scope: "tab" }),
      await admin.post("/dev/impersonate", { userId: me.id, scope: "browser" }),
    ]) {
      expect(r.status).toBe(404);
      expect(r.body.error).toMatchObject({ code: "NOT_FOUND", message: "接口不存在" });
      expect(r.setCookie).toEqual([]);
      expectShape("not-found", r.body);
    }
  });
});

describe.runIf(goOnly && !!DEV_BASE_URL)("开发模式：--dev 实例（DEV_BASE_URL）", () => {
  const base = DEV_BASE_URL!;

  async function devUsers() {
    const res = await anon(base).get("/dev/users");
    expect(res.status).toBe(200);
    return res.body.data as { id: string; email: string; name: string; role: string; classNames: string[] }[];
  }

  it("/config 的 dev 为 true", async () => {
    const res = await anon(base).get("/config");
    expect(res.status).toBe(200);
    expect(res.body.data.dev).toBe(true);
  });

  it("/dev/users 无需登录：管理员 → 老师 → 学生，同角色按邮箱；带所在班级名", async () => {
    const users = await devUsers();
    const rank: Record<string, number> = { admin: 0, teacher: 1, student: 2 };
    for (let i = 1; i < users.length; i++) {
      const a = users[i - 1];
      const b = users[i];
      expect(rank[a.role] < rank[b.role] || (rank[a.role] === rank[b.role] && a.email <= b.email), `${a.email} 应排在 ${b.email} 前`).toBe(true);
    }
    const teacher = users.find((u) => u.email === "teacher@vinx.test")!;
    expect(teacher).toMatchObject({ role: "teacher", name: expect.any(String) });
    expect(Array.isArray(teacher.classNames)).toBe(true);
    expect(users.find((u) => u.email === "student@vinx.test")?.classNames.length).toBeGreaterThan(0);
    expect(users.find((u) => u.email === "admin@vinx.test")?.classNames).toEqual([]);
  });

  it("scope=tab：只返回令牌、不写 Cookie；令牌用 Bearer 头访问 /auth/me", async () => {
    const student = (await devUsers()).find((u) => u.email === "student@vinx.test")!;
    const c = anon(base);
    const res = await c.post("/dev/impersonate", { userId: student.id, scope: "tab" });
    expect(res.status).toBe(200);
    expect(res.setCookie).toEqual([]);
    expect(res.body.data.user).toMatchObject({ id: student.id, email: "student@vinx.test", role: "student" });
    expect(typeof res.body.data.token).toBe("string");
    expect(res.body.data.token.length).toBeGreaterThan(20);

    // 同一个客户端没有拿到 Cookie：不带头时仍是未登录
    expect((await c.get("/auth/me")).status).toBe(401);
    const me = await anon(base).get("/auth/me", { headers: { authorization: `Bearer ${res.body.data.token}` } });
    expect(me.status).toBe(200);
    expect(JSON.stringify(me.body.data)).toContain("student@vinx.test");

    // Bearer 优先于 Cookie：浏览器 Cookie 是管理员，本标签页令牌是学生
    const admin = await login("admin@vinx.test", undefined, base);
    const both = await admin.get("/auth/me", { headers: { authorization: `Bearer ${res.body.data.token}` } });
    expect(both.status).toBe(200);
    expect(JSON.stringify(both.body.data)).toContain("student@vinx.test");
    expect(JSON.stringify((await admin.get("/auth/me")).body.data)).toContain("admin@vinx.test");
  });

  it("scope=browser：与正常登录一样写 Cookie，不返回令牌", async () => {
    const teacher = (await devUsers()).find((u) => u.email === "teacher@vinx.test")!;
    const c = anon(base);
    const res = await c.post("/dev/impersonate", { userId: teacher.id, scope: "browser" });
    expect(res.status).toBe(200);
    expect(res.setCookie.some((l) => /^vinx_token=[^;]+/.test(l) && /httponly/i.test(l))).toBe(true);
    expect(res.body.data.token).toBeUndefined();
    expect(res.body.data.user).toMatchObject({ id: teacher.id, role: "teacher" });
    const me = await c.get("/auth/me");
    expect(me.status).toBe(200);
    expect(JSON.stringify(me.body.data)).toContain("teacher@vinx.test");
  });

  it("校验：缺参数 400；scope 非法 400；用户不存在 404；外站 Origin 403", async () => {
    const c = anon(base);
    const missing = await c.post("/dev/impersonate", {});
    expect(missing.status).toBe(400);
    expect(missing.body.error.details).toMatchObject({ userId: "Required", scope: "Required" });
    const student = (await devUsers()).find((u) => u.email === "student@vinx.test")!;
    const badScope = await c.post("/dev/impersonate", { userId: student.id, scope: "window" });
    expect(badScope.status).toBe(400);
    expect(badScope.body.error.details.scope).toBe("scope 只能是 tab 或 browser");
    const nobody = await c.post("/dev/impersonate", { userId: "no-such-user", scope: "tab" });
    expect(nobody.status).toBe(404);
    expect(nobody.body.error.message).toBe("用户不存在");
    const foreign = await c.post("/dev/impersonate", { userId: student.id, scope: "browser" }, { headers: { origin: "http://evil.example" } });
    expect(foreign.status).toBe(403);
    expect(foreign.setCookie).toEqual([]);
  });
});
