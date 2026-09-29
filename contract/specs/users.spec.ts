/**
 * 用户管理（班级版，旧 apps/api/src/routes/users.ts）：
 * 教师可建学生账号、看自己建的与本班学生、重置自己建的学生密码；管理员建任意角色、改角色、重置任意密码。
 *
 * 「系统至少保留一个管理员」需要库里恰好一个管理员，共享库上不稳定，由 Go 单测覆盖（internal/api/users_test.go）。
 */
import { describe, it, expect, beforeAll } from "vitest";
import { anon, login, STAMP, type Client } from "../lib/client";
import { ready } from "../lib/areas";
import { expectShape } from "../lib/shape";

let seq = 0;
const uniq = (tag: string) => `u-${tag}-${STAMP}-${seq++}@vinx.test`;

describe.runIf(ready("users"))("用户管理（班级版）", () => {
  let admin: Client;
  beforeAll(async () => {
    admin = await login("admin@vinx.test");
  });

  it("建号：默认学生；教师只能建学生；校验与重复账号", async () => {
    const email = uniq("create");
    const res = await admin.post("/users", { email: `  ${email.toUpperCase()} `, name: " 新同学 ", password: "dev123456" });
    expect(res.status).toBe(200);
    expect(res.body.data).toEqual({ id: expect.any(String), email, name: "新同学", role: "student" });
    expectShape("user-create", res.body);
    expect((await anon().post("/auth/login", { email, password: "dev123456" })).status).toBe(200);

    const dup = await admin.post("/users", { email, name: "x", password: "dev123456" });
    expect(dup.status).toBe(409);
    expect(dup.body.error).toMatchObject({ code: "EMAIL_EXISTS", message: "账号已存在" });

    const teacherEmail = uniq("t");
    expect((await admin.post("/users", { email: teacherEmail, name: "老师", password: "dev123456", role: "teacher" })).body.data.role).toBe("teacher");
    const teacher = await login(teacherEmail);
    const denied = await teacher.post("/users", { email: uniq("x"), name: "x", password: "dev123456", role: "teacher" });
    expect(denied.status).toBe(403);
    expect(denied.body.error).toMatchObject({ code: "FORBIDDEN", message: "只有管理员可以创建教师或管理员账号" });
    expect((await teacher.post("/users", { email: uniq("s"), name: "x", password: "dev123456", role: "student" })).status).toBe(200);

    const bad = async (body: object) => (await admin.post("/users", body)).body.error;
    expect(await bad({})).toMatchObject({ code: "VALIDATION", details: { email: "Required", name: "Required", password: "Required" } });
    expect((await bad({ email: " a ", name: " ", password: "" })).details).toEqual({ email: "账号至少 2 位", name: "姓名不能为空", password: "密码不能为空" });
    expect((await bad({ email: "x".repeat(101), name: "n".repeat(51), password: "p" })).details).toEqual({
      email: "String must contain at most 100 character(s)",
      name: "String must contain at most 50 character(s)",
    });
    expect((await bad({ email: uniq("r"), name: "x", password: "dev123456", role: "boss" })).details).toEqual({
      role: "Invalid enum value. Expected 'student' | 'teacher' | 'admin', received 'boss'",
    });
    const weak = await bad({ email: uniq("w"), name: "x", password: "12345" });
    expect(weak).toMatchObject({ code: "VALIDATION", message: "密码长度至少 6 位" });
    expect(weak.details).toBeUndefined();

    const student = await login("student@vinx.test");
    expect((await student.post("/users", { email: uniq("s"), name: "x", password: "dev123456" })).status).toBe(403);
    expect((await anon().post("/users", {})).status).toBe(401);
  });

  it("列表：管理员看全部并可搜索、分页；教师只看自己建的与本班学生", async () => {
    const teacherEmail = uniq("lt");
    await admin.post("/users", { email: teacherEmail, name: "列表老师", password: "dev123456", role: "teacher" });
    const teacher = await login(teacherEmail);
    const tag = `lst${STAMP}`;
    const mine = await teacher.post("/users", { email: `${tag}-a@vinx.test`, name: `甲${tag}`, password: "dev123456" });
    const cls = (await teacher.post("/classes", { name: `列表班 ${STAMP}` })).body.data;
    const joined = await anon().post("/auth/signup", { email: `${tag}-b@vinx.test`, password: "dev123456", name: `乙${tag}`, inviteCode: cls.inviteCode });
    expect(joined.status).toBe(201);
    await admin.post("/users", { email: `${tag}-c@vinx.test`, name: `丙${tag}`, password: "dev123456" });

    const tl = await teacher.get("/users");
    expect(tl.status).toBe(200);
    expect(tl.body.data).toMatchObject({ total: 2, page: 1, limit: 50 });
    // 新建的在前
    expect(tl.body.data.items.map((u: { email: string }) => u.email)).toEqual([`${tag}-b@vinx.test`, `${tag}-a@vinx.test`]);
    expect(tl.body.data.items.map((u: { managedByMe: boolean }) => u.managedByMe)).toEqual([false, true]);
    expect(tl.body.data.items[1]).toEqual({
      id: mine.body.data.id,
      email: `${tag}-a@vinx.test`,
      name: `甲${tag}`,
      role: "student",
      currentGrade: null,
      createdAt: expect.any(String),
      createdById: expect.any(String),
      managedByMe: true,
    });
    expect(tl.body.data.items[0].createdById).toBeNull();
    expectShape("user-list", tl.body);

    // 管理员：搜索（姓名或账号，不区分大小写），managedByMe 恒为真
    const found = await admin.get(`/users?q=${encodeURIComponent(` ${tag.toUpperCase()} `)}`);
    expect(found.body.data.total).toBe(3);
    expect(found.body.data.items.every((u: { managedByMe: boolean }) => u.managedByMe)).toBe(true);
    const byName = await admin.get(`/users?q=${encodeURIComponent(`丙${tag}`)}`);
    expect(byName.body.data.items.map((u: { email: string }) => u.email)).toEqual([`${tag}-c@vinx.test`]);
    const paged = await admin.get(`/users?q=${tag}&page=2&limit=2`);
    expect(paged.body.data).toMatchObject({ total: 3, page: 2, limit: 2 });
    expect(paged.body.data.items).toHaveLength(1);
    expect(paged.body.data.items[0].email).toBe(`${tag}-a@vinx.test`);
    // 空搜索词等于不搜
    const all = await admin.get("/users?q=%20&limit=500");
    expect(all.body.data.items.length).toBeGreaterThan(3);
    // 教师搜索也受范围限制
    const tq = await teacher.get(`/users?q=${tag}`);
    expect(tq.body.data.total).toBe(2);

    const bad = async (qs: string) => (await admin.get(`/users?${qs}`)).body.error;
    expect((await bad("limit=0")).details).toEqual({ limit: "Number must be greater than or equal to 1" });
    expect((await bad("limit=501")).details).toEqual({ limit: "Number must be less than or equal to 500" });
    expect((await bad("page=0")).details).toEqual({ page: "Number must be greater than or equal to 1" });
    expect((await bad("page=abc")).details).toEqual({ page: "Expected number, received nan" });
    expect((await bad("limit=1.5")).details).toEqual({ limit: "Expected integer, received float" });
  });

  it("改角色：仅管理员；不存在 404；校验", async () => {
    const email = uniq("role");
    const u = (await admin.post("/users", { email, name: "改角色", password: "dev123456" })).body.data;
    const res = await admin.patch(`/users/${u.id}`, { role: "teacher" });
    expect(res.status).toBe(200);
    expect(res.body.data).toEqual({ id: u.id, role: "teacher" });
    expectShape("user-role", res.body);
    // 新角色在重新登录后生效
    const relogin = await login(email);
    expect((await relogin.get("/auth/me")).body.data.role).toBe("teacher");
    // 老师没有 system 能力
    const denied = await relogin.patch(`/users/${u.id}`, { role: "admin" });
    expect(denied.status).toBe(403);
    expect(denied.body.error.message).toBe("无权访问");

    const nf = await admin.patch("/users/no-such-user", { role: "student" });
    expect(nf.status).toBe(404);
    expect(nf.body.error).toMatchObject({ code: "NOT_FOUND", message: "用户不存在" });
    expect((await admin.patch(`/users/${u.id}`, {})).body.error.details).toEqual({ role: "Required" });
    expect((await admin.patch(`/users/${u.id}`, { role: "root" })).status).toBe(400);
  });

  it("重置密码：管理员任意账号；老师仅限自己创建的学生", async () => {
    const teacherEmail = uniq("rt");
    await admin.post("/users", { email: teacherEmail, name: "重置老师", password: "dev123456", role: "teacher" });
    const teacher = await login(teacherEmail);
    const mineEmail = uniq("rm");
    const mine = (await teacher.post("/users", { email: mineEmail, name: "我的学生", password: "dev123456" })).body.data;
    const othersEmail = uniq("ro");
    const others = (await admin.post("/users", { email: othersEmail, name: "别人的学生", password: "dev123456" })).body.data;

    const ok = await teacher.post(`/users/${mine.id}/reset-password`, { newPassword: "changed1" });
    expect(ok.status).toBe(200);
    expect(ok.body.data).toBeUndefined();
    expect((await anon().post("/auth/login", { email: mineEmail, password: "changed1" })).status).toBe(200);

    const denied = await teacher.post(`/users/${others.id}/reset-password`, { newPassword: "changed1" });
    expect(denied.status).toBe(403);
    expect(denied.body.error).toMatchObject({ code: "FORBIDDEN", message: "只能重置自己创建的学生账号的密码" });
    // 老师自己建的账号被改成老师后也不能重置
    await admin.patch(`/users/${mine.id}`, { role: "teacher" });
    expect((await teacher.post(`/users/${mine.id}/reset-password`, { newPassword: "changed2" })).status).toBe(403);

    expect((await admin.post(`/users/${others.id}/reset-password`, { newPassword: "byadmin1" })).status).toBe(200);
    expect((await anon().post("/auth/login", { email: othersEmail, password: "byadmin1" })).status).toBe(200);

    const nf = await admin.post("/users/no-such-user/reset-password", { newPassword: "abcdef" });
    expect(nf.status).toBe(404);
    expect(nf.body.error.message).toBe("用户不存在");
    // 校验先于查找
    expect((await admin.post("/users/no-such-user/reset-password", {})).body.error.details).toEqual({ newPassword: "Required" });
    expect((await admin.post(`/users/${others.id}/reset-password`, { newPassword: "" })).body.error.details).toEqual({ newPassword: "新密码不能为空" });
    const weak = await admin.post(`/users/no-such-user/reset-password`, { newPassword: "123" });
    expect(weak.status).toBe(400);
    expect(weak.body.error.message).toBe("密码长度至少 6 位");

    const student = await login("student@vinx.test");
    expect((await student.post(`/users/${others.id}/reset-password`, { newPassword: "abcdef" })).status).toBe(403);
  });
});
