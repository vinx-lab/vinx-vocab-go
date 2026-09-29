/**
 * 由旧 apps/api/tests/auth.flow.test.ts 改写为纯 HTTP 黑盒，并补充认证相关的边界用例。
 * 依赖 seed 数据：admin / teacher / student@vinx.test（密码 dev123456），演示班级邀请码 DEMO01。
 */
import { describe, it, expect } from "vitest";
import { anon, Client, login, STAMP } from "../lib/client";
import { ready } from "../lib/areas";
import { expectShape } from "../lib/shape";

const password = "123456";

function cookieLine(setCookie: string[], name = "vinx_token"): string {
  const line = setCookie.find((l) => l.startsWith(`${name}=`));
  if (!line) throw new Error(`响应无 ${name} Set-Cookie`);
  return line;
}

describe("认证链路", () => {
  const email = `flow-${STAMP}@vinx.test`;

  it("signup → login(Set-Cookie HttpOnly) → me → logout → me(401)", async () => {
    const c = anon();
    const signup = await c.post("/auth/signup", { email, password });
    expect(signup.status).toBe(201);
    expect(signup.body.success).toBe(true);
    expect(signup.body.data.user.email).toBe(email);
    expect(typeof signup.body.timestamp).toBe("string");
    expect(signup.setCookie.length).toBeGreaterThan(0);
    expectShape("auth-signup", signup.body);

    const lc = anon();
    const loginRes = await lc.post("/auth/login", { email, password });
    expect(loginRes.status).toBe(200);
    const line = cookieLine(loginRes.setCookie);
    expect(line).toContain("vinx_token=");
    expect(line).toContain("HttpOnly");
    expect(line).toContain("SameSite=Lax");
    expect(line).toContain("Path=/");
    expect(line).toContain("Max-Age=604800");
    // 开发模式 / 内网 http：不带 Secure
    expect(line).not.toContain("Secure");
    expectShape("auth-login", loginRes.body);

    const me = await lc.get("/auth/me");
    expect(me.status).toBe(200);
    expect(me.body.success).toBe(true);
    expect(me.body.data.email).toBe(email);
    expect(me.body.data.capabilities).toEqual(expect.arrayContaining(["study", "plans", "books.read"]));
    expect(me.body.data.capabilities).not.toContain("classes");
    expect(me.body.data.role).toBe("student");
    expect(me.body.data.currentGrade).toBeNull();
    expect(me.body.data.name).toBe(email.split("@")[0]);
    expectShape("auth-me", me.body);

    const logout = await lc.post("/auth/logout");
    expect(logout.status).toBe(200);
    expect(logout.body).toMatchObject({ success: true });
    expect(logout.body).not.toHaveProperty("data");
    const clear = cookieLine(logout.setCookie);
    expect(clear).toMatch(/Max-Age=0|Expires=Thu, 01 Jan 1970/);
    expectShape("ok-empty", logout.body);

    // cookie 已被 jar 删除
    expect(lc.jar.has("vinx_token")).toBe(false);
    const me2 = await lc.get("/auth/me");
    expect(me2.status).toBe(401);
    expect(me2.body.success).toBe(false);
    expect(me2.body.error.code).toBe("UNAUTHORIZED");
    expect(me2.body.error.message).toBe("未登录或登录已失效");
    expectShape("unauthorized", me2.body);
  });

  it("登录失败返回 UNAUTHORIZED（不暴露账号存在性）", async () => {
    const none = await anon().post("/auth/login", { email: "no-such@vinx.test", password: "wrong-password" });
    expect(none.status).toBe(401);
    expect(none.body.error).toMatchObject({ code: "UNAUTHORIZED", message: "账号或密码错误" });
    const wrong = await anon().post("/auth/login", { email: "student@vinx.test", password: "wrong-password" });
    expect(wrong.status).toBe(401);
    expect(wrong.body.error).toMatchObject({ code: "UNAUTHORIZED", message: "账号或密码错误" });
    expect(wrong.setCookie).toEqual([]);
  });

  it("登录账号去首尾空白并转小写", async () => {
    const res = await anon().post("/auth/login", { email: "  Student@VINX.test ", password: "dev123456" });
    expect(res.status).toBe(200);
    expect(res.body.data.user.email).toBe("student@vinx.test");
  });

  it("参数校验失败返回 VALIDATION + 字段级 details", async () => {
    const res = await anon().post("/auth/signup", { email: "not-an-email", password: "" });
    expect(res.status).toBe(400);
    expect(res.body.error.code).toBe("VALIDATION");
    expect(res.body.error.message).toBe("参数校验失败");
    expect(res.body.error.details).toEqual({ email: "邮箱格式不正确", password: "密码不能为空" });
    expect(typeof res.body.error.requestId).toBe("string");

    const empty = await anon().post("/auth/login", { email: "   ", password: "" });
    expect(empty.status).toBe(400);
    expect(empty.body.error.details).toEqual({ email: "账号不能为空", password: "密码不能为空" });

    const missing = await anon().post("/auth/signup", {});
    expect(missing.body.error.details).toEqual({ email: "Required", password: "Required" });

    const longName = await anon().post("/auth/signup", { email: `ln-${STAMP}@vinx.test`, password, name: "x".repeat(51) });
    expect(longName.status).toBe(400);
    expect(longName.body.error.details).toEqual({ name: "String must contain at most 50 character(s)" });
  });

  it("注册：弱密码、重复邮箱（大小写不敏感）、无效邀请码", async () => {
    const weak = await anon().post("/auth/signup", { email: `weak-${STAMP}@vinx.test`, password: "12345" });
    expect(weak.status).toBe(400);
    expect(weak.body.error).toMatchObject({ code: "VALIDATION", message: "密码长度至少 6 位" });
    expect(weak.body.error).not.toHaveProperty("details");

    const dup = await anon().post("/auth/signup", { email: "STUDENT@vinx.test", password });
    expect(dup.status).toBe(409);
    expect(dup.body.error).toMatchObject({ code: "EMAIL_EXISTS", message: "该邮箱已注册" });

    const badCode = await anon().post("/auth/signup", { email: `code-${STAMP}@vinx.test`, password, inviteCode: "ZZZZZZ" });
    expect(badCode.status).toBe(404);
    expect(badCode.body.error).toMatchObject({ code: "NOT_FOUND", message: "邀请码无效或班级已归档" });
    // 邀请码无效时不建号
    expect((await anon().post("/auth/login", { email: `code-${STAMP}@vinx.test`, password })).status).toBe(401);
  });

  it("注册时姓名可选，自定义姓名原样保存", async () => {
    const res = await anon().post("/auth/signup", { email: `named-${STAMP}@vinx.test`, password, name: "小张" });
    expect(res.status).toBe(201);
    expect(res.body.data.user).toMatchObject({ name: "小张", role: "student", theme: "system", currentGrade: null });
    expect(res.body.data.user.createdAt).toMatch(/Z$/);
  });

  it.runIf(ready("classes"))("注册时带邀请码 DEMO01 → 加入演示班级", async () => {
    const c = anon();
    const res = await c.post("/auth/signup", { email: `join-${STAMP}@vinx.test`, password, inviteCode: " demo01 " });
    expect(res.status).toBe(201);
    const mine = await c.get("/me/classes");
    expect(mine.status).toBe(200);
    expect(mine.body.data.items.map((i: { name: string }) => i.name)).toContain("八年级一班");
  });

  it("外观偏好：默认 system，PUT /auth/profile 可单独保存 theme，非法值 400，登录与 /auth/me 都带 theme", async () => {
    const themeEmail = `theme-${STAMP}@vinx.test`;
    const c = anon();
    const signup = await c.post("/auth/signup", { email: themeEmail, password });
    expect(signup.status).toBe(201);
    expect(signup.body.data.user.theme).toBe("system");

    const save = await c.put("/auth/profile", { theme: "dark" });
    expect(save.status).toBe(200);
    expect(save.body).not.toHaveProperty("data");

    const bad = await c.put("/auth/profile", { theme: "sepia" });
    expect(bad.status).toBe(400);
    expect(bad.body.error.code).toBe("VALIDATION");
    expect(bad.body.error.details).toEqual({ theme: "外观只能是 system / light / dark" });

    const me = await c.get("/auth/me");
    expect(me.body.data.theme).toBe("dark");
    expect(me.body.data.name).toBe(themeEmail.split("@")[0]);

    const again = await anon().post("/auth/login", { email: themeEmail, password });
    expect(again.status).toBe(200);
    expect(again.body.data.user.theme).toBe("dark");

    const rename = await c.put("/auth/profile", { name: "换个名字" });
    expect(rename.status).toBe(200);
    const me2 = await c.get("/auth/me");
    expect(me2.body.data).toMatchObject({ name: "换个名字", theme: "dark" });
  });

  it("个人资料：年级可设置与清空（null），姓名校验", async () => {
    const c = anon();
    await c.post("/auth/signup", { email: `grade-${STAMP}@vinx.test`, password });
    expect((await c.put("/auth/profile", { currentGrade: "八年级" })).status).toBe(200);
    expect((await c.get("/auth/me")).body.data.currentGrade).toBe("八年级");
    // 不带 currentGrade 不改
    expect((await c.put("/auth/profile", {})).status).toBe(200);
    expect((await c.get("/auth/me")).body.data.currentGrade).toBe("八年级");
    expect((await c.put("/auth/profile", { currentGrade: null })).status).toBe(200);
    expect((await c.get("/auth/me")).body.data.currentGrade).toBeNull();

    const emptyName = await c.put("/auth/profile", { name: "" });
    expect(emptyName.status).toBe(400);
    expect(emptyName.body.error.details).toEqual({ name: "String must contain at least 1 character(s)" });
    const longName = await c.put("/auth/profile", { name: "名".repeat(51) });
    expect(longName.body.error.details).toEqual({ name: "姓名过长" });
    const longGrade = await c.put("/auth/profile", { currentGrade: "x".repeat(21) });
    expect(longGrade.body.error.details).toEqual({ currentGrade: "String must contain at most 20 character(s)" });

    expect((await anon().put("/auth/profile", { theme: "dark" })).status).toBe(401);
  });

  it("修改密码：两次不一致、弱密码、旧密码错误、成功后新密码可登录", async () => {
    const pwEmail = `pw-${STAMP}@vinx.test`;
    const c = anon();
    await c.post("/auth/signup", { email: pwEmail, password });

    const missing = await c.post("/auth/change-password", {});
    expect(missing.status).toBe(400);
    expect(missing.body.error.details).toEqual({ oldPassword: "Required", newPassword: "Required", confirmPassword: "Required" });

    const blank = await c.post("/auth/change-password", { oldPassword: "", newPassword: "", confirmPassword: "" });
    expect(blank.body.error.details).toEqual({ oldPassword: "旧密码不能为空", newPassword: "新密码不能为空", confirmPassword: "确认密码不能为空" });

    const mismatch = await c.post("/auth/change-password", { oldPassword: password, newPassword: "abcdef", confirmPassword: "abcdeg" });
    expect(mismatch.status).toBe(400);
    expect(mismatch.body.error.message).toBe("两次输入的新密码不一致");

    const weak = await c.post("/auth/change-password", { oldPassword: password, newPassword: "abc", confirmPassword: "abc" });
    expect(weak.body.error.message).toBe("密码长度至少 6 位");

    const wrongOld = await c.post("/auth/change-password", { oldPassword: "nope-nope", newPassword: "abcdef", confirmPassword: "abcdef" });
    expect(wrongOld.status).toBe(400);
    expect(wrongOld.body.error).toMatchObject({ code: "VALIDATION", message: "旧密码不正确" });

    const done = await c.post("/auth/change-password", { oldPassword: password, newPassword: "abcdef", confirmPassword: "abcdef" });
    expect(done.status).toBe(200);
    expect(done.body).not.toHaveProperty("data");

    expect((await anon().post("/auth/login", { email: pwEmail, password })).status).toBe(401);
    expect((await anon().post("/auth/login", { email: pwEmail, password: "abcdef" })).status).toBe(200);

    expect((await anon().post("/auth/change-password", { oldPassword: "a", newPassword: "b", confirmPassword: "b" })).status).toBe(401);
  });

  it("各角色的能力清单", async () => {
    const expected: Record<string, string[]> = {
      "student@vinx.test": ["study", "plans", "books.read"],
      "teacher@vinx.test": ["study", "plans", "books.read", "plans.assign", "books.edit", "classes", "students.view", "users"],
      "admin@vinx.test": ["study", "plans", "books.read", "plans.assign", "books.edit", "classes", "students.view", "users", "system"],
    };
    for (const [email, caps] of Object.entries(expected)) {
      const c = await login(email);
      const me = await c.get("/auth/me");
      expect(me.body.data.capabilities).toEqual(caps);
      expect(me.body.data.email).toBe(email);
    }
  });
});

describe("令牌", () => {
  // 旧后端的实际行为（@fastify/jwt 的错误码不在 error-handler 的映射表里，落入「4xx → VALIDATION」分支）：
  // 令牌无效时 HTTP 401，但 code 是 VALIDATION、message 是英文原文；只有「没带令牌」才是 UNAUTHORIZED。
  // 前端按 HTTP 401 跳登录页，Go 版照原样输出。
  it("篡改或伪造的令牌 → 401（code VALIDATION + @fastify/jwt 原文）", async () => {
    const c = await login("student@vinx.test");
    const token = c.jar.get("vinx_token")!;
    const [h, p] = token.split(".");
    const forged = new Client();
    forged.jar.set("vinx_token", `${h}.${p}.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA`);
    const res = await forged.get("/auth/me");
    expect(res.status).toBe(401);
    expect(res.body.error).toMatchObject({ code: "VALIDATION", message: "Authorization token is invalid: The token signature is invalid." });

    for (const bad of ["not-a-jwt", `${h}.${p}`]) {
      const g = new Client();
      g.jar.set("vinx_token", bad);
      const r = await g.get("/auth/me");
      expect(r.status).toBe(401);
      expect(r.body.error).toMatchObject({ code: "VALIDATION", message: "Authorization token is invalid: The token is malformed." });
    }
    const hdr = new Client();
    hdr.jar.set("vinx_token", "a.b.c");
    const r2 = await hdr.get("/auth/me");
    expect(r2.status).toBe(401);
    expect(r2.body.error.message).toBe("Authorization token is invalid: The token header is not a valid base64url serialized JSON.");

    // 请求头里的坏令牌同样处理；Bearer 格式不对 → 400
    const viaHeader = await anon().get("/auth/me", { headers: { authorization: "Bearer xx" } });
    expect(viaHeader.status).toBe(401);
    expect(viaHeader.body.error.message).toBe("Authorization token is invalid: The token is malformed.");
    const badFormat = await anon().get("/auth/me", { headers: { authorization: "Bearer a b" } });
    expect(badFormat.status).toBe(400);
    expect(badFormat.body.error).toMatchObject({ code: "VALIDATION", message: "Format is Authorization: Bearer [token]" });
    // 非 Bearer 的 Authorization 头被忽略，回落到 cookie（没有 → 未登录）
    const basic = await anon().get("/auth/me", { headers: { authorization: "Basic xx" } });
    expect(basic.status).toBe(401);
    expect(basic.body.error).toMatchObject({ code: "UNAUTHORIZED", message: "未登录或登录已失效" });
  });

  it("Authorization: Bearer 头同样可用", async () => {
    const c = await login("student@vinx.test");
    const token = c.jar.get("vinx_token")!;
    const res = await anon().get("/auth/me", { headers: { authorization: `Bearer ${token}` } });
    expect(res.status).toBe(200);
    expect(res.body.data.email).toBe("student@vinx.test");
  });

  it("令牌载荷：sub / email / name / role，7 天有效", async () => {
    const c = await login("teacher@vinx.test");
    const payload = JSON.parse(Buffer.from(c.jar.get("vinx_token")!.split(".")[1], "base64url").toString());
    expect(Object.keys(payload).sort()).toEqual(["email", "exp", "iat", "name", "role", "sub"]);
    expect(payload).toMatchObject({ email: "teacher@vinx.test", role: "teacher", name: "王老师" });
    expect(payload.exp - payload.iat).toBe(7 * 24 * 3600);
    const header = JSON.parse(Buffer.from(c.jar.get("vinx_token")!.split(".")[0], "base64url").toString());
    expect(header.alg).toBe("HS256");
  });

  it("未登录访问需登录的接口 → 401", async () => {
    for (const [method, path] of [
      ["POST", "/auth/logout"],
      ["GET", "/auth/me"],
      ["POST", "/auth/change-password"],
      ["PUT", "/auth/profile"],
    ] as const) {
      const res = await anon().call(method, path, method === "GET" ? undefined : {});
      expect(res.status, `${method} ${path}`).toBe(401);
      expect(res.body.error.code).toBe("UNAUTHORIZED");
    }
  });
});
