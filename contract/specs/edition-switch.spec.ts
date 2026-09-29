/**
 * 版本选择与运行时切换（Go 版新增，spec 0001 §2「版本」；oracle 只能在启动时用 VINX_EDITION 指定，没有这些接口）：
 * - GET /config 的 editionLocked / needsSetup；
 * - POST /setup/edition：仅首次运行（needsSetup）可用，无需登录；
 * - PUT /settings/edition：管理员双向切换，下一个请求即生效。降级只隐藏、不删数据：班级 / 邀请码 / 布置计划 /
 *   用户管理接口 404，注册关闭，已有账号仍能登录且只看自己的数据；升级回班级版原样恢复。
 *
 * 只在 Go 上跑（goOnly）。被测实例须未锁定版本（没设 VINX_EDITION）且已 seed（有账号，所以 needsSetup 为假）。
 * 用例会把实例切到个人版，结束时（包括失败时）切回班级版。「全新安装走向导」由 Go 单测覆盖（internal/api/edition_test.go）。
 */
import { describe, it, expect, beforeAll, afterAll } from "vitest";
import { anon, login, STAMP, type Client } from "../lib/client";
import { goOnly } from "../lib/areas";

describe.runIf(goOnly)("版本切换（Go）", () => {
  let admin: Client;
  let teacher: Client;
  let student: Client;
  let studentId = "";
  let classId = "";
  let classPlanId = "";
  let teacherBookId = "";
  let unitId = "";
  const teacherEmail = `es-t-${STAMP}@vinx.test`;
  const studentEmail = `es-s-${STAMP}@vinx.test`;

  beforeAll(async () => {
    admin = await login("admin@vinx.test");
    const cfg = (await anon().get("/config")).body.data;
    if (cfg.editionLocked || cfg.edition !== "school") throw new Error("被测实例须为未锁定的班级版");
    expect((await admin.post("/users", { email: teacherEmail, name: "切换老师", password: "dev123456", role: "teacher" })).status).toBe(200);
    teacher = await login(teacherEmail);
    const cls = await teacher.post("/classes", { name: `切换班 ${STAMP}` });
    classId = cls.body.data.id;
    student = anon();
    const signup = await student.post("/auth/signup", { email: studentEmail, password: "dev123456", name: "切换学生", inviteCode: cls.body.data.inviteCode });
    expect(signup.status).toBe(201);
    studentId = signup.body.data.user.id;
    const book = await teacher.post("/books", { name: `切换词书 ${STAMP}` });
    teacherBookId = book.body.data.id;
    const unit = await teacher.post(`/books/${teacherBookId}/units`, { name: "U1" });
    unitId = unit.body.data.id;
    for (let i = 0; i < 3; i++) await teacher.post(`/units/${unit.body.data.id}/words`, { spelling: `sw${STAMP}${i}`, definition: `释义${i}` });
    const plan = await teacher.post("/plans", { name: `切换计划 ${STAMP}`, newPerDay: 3, modes: ["recognition"], unitIds: [unit.body.data.id], targets: { classIds: [classId], userIds: [] } });
    expect(plan.status).toBe(200);
    classPlanId = plan.body.data.id;
  });

  afterAll(async () => {
    // 无论成败都切回班级版，免得影响其他用例
    if (admin) await admin.put("/settings/edition", { edition: "school" });
  });

  it("/config：未锁定、已有账号不进向导；首次运行接口已关闭", async () => {
    const cfg = (await anon().get("/config")).body.data;
    expect(cfg).toMatchObject({ edition: "school", editionLocked: false, needsSetup: false });
    const setup = await anon().post("/setup/edition", { edition: "personal" });
    expect(setup.status).toBe(403);
    expect(setup.body.error).toMatchObject({ code: "FORBIDDEN", message: "已完成初始设置，如需切换版本请由管理员在系统设置里操作" });
    const bad = await anon().post("/setup/edition", { edition: "enterprise" });
    expect(bad.status).toBe(400);
    expect(bad.body.error.details).toEqual({ edition: "Invalid enum value. Expected 'personal' | 'school', received 'enterprise'" });
  });

  it("PUT /settings/edition：仅管理员；校验", async () => {
    expect((await anon().put("/settings/edition", { edition: "personal" })).status).toBe(401);
    const t = await teacher.put("/settings/edition", { edition: "personal" });
    expect(t.status).toBe(403);
    expect(t.body.error).toMatchObject({ code: "FORBIDDEN", message: "无权访问" });
    expect((await admin.put("/settings/edition", {})).body.error.details).toEqual({ edition: "Required" });
    expect((await admin.put("/settings/edition", { edition: null })).body.error.details).toEqual({ edition: "Expected 'personal' | 'school', received null" });
    // 切到同一个版本也可以（保存选择）
    const same = await admin.put("/settings/edition", { edition: "school" });
    expect(same.status).toBe(200);
    expect(same.body.data).toMatchObject({ edition: "school", editionLocked: false, needsSetup: false, signupEnabled: true });
  });

  it("降级为个人版：隐藏班级 / 用户管理并关闭注册，数据不删、只看自己的；升级原样恢复", async () => {
    // 降级前：老师能看本班学生的记录，管理员能看到老师的计划与词书
    expect((await teacher.get(`/records/summary?userId=${studentId}`)).status).toBe(200);
    const adminPlansBefore = (await admin.get("/plans")).body.data.items.map((p: { id: string }) => p.id);
    expect(adminPlansBefore).toContain(classPlanId);

    const down = await admin.put("/settings/edition", { edition: "personal" });
    expect(down.status).toBe(200);
    expect(down.body.data).toMatchObject({
      edition: "personal",
      features: { classes: false, assignToOthers: false, multiUser: false, studentRecords: false },
      signupEnabled: false,
      editionLocked: false,
      needsSetup: false,
    });
    expect((await anon().get("/config")).body.data.edition).toBe("personal");

    // 班级、邀请码、用户管理接口不存在
    for (const [c, method, path] of [
      [teacher, "GET", "/classes"],
      [teacher, "GET", `/classes/${classId}`],
      [admin, "GET", "/users"],
      [admin, "POST", "/users"],
      [student, "GET", "/me/classes"],
      [student, "POST", "/classes/join"],
    ] as const) {
      const r = await c.call(method, path, method === "POST" ? {} : undefined);
      expect(r.status, `${method} ${path}`).toBe(404);
      expect(r.body.error).toMatchObject({ code: "NOT_FOUND", message: "接口不存在" });
    }
    // 注册关闭
    const signup = await anon().post("/auth/signup", { email: `es-new-${STAMP}@vinx.test`, password: "dev123456" });
    expect(signup.status).toBe(403);
    expect(signup.body.error).toMatchObject({ code: "FORBIDDEN", message: "个人版只允许一个账号，请直接登录" });

    // 已有账号仍能登录
    const t2 = await login(teacherEmail);
    expect((await t2.get("/auth/me")).body.data.email).toBe(teacherEmail);
    const s2 = await login(studentEmail);
    // 学生的学习不受影响：被安排的计划仍在今日
    const today = await s2.get("/today");
    expect(today.status).toBe(200);
    expect(today.body.data.plans.map((p: { planId: string }) => p.planId)).toContain(classPlanId);

    // 只看自己的数据：老师 / 管理员都不能看别人的记录
    for (const c of [teacher, admin]) {
      const r = await c.get(`/records/summary?userId=${studentId}`);
      expect(r.status).toBe(403);
      expect(r.body.error).toMatchObject({ code: "FORBIDDEN", message: "无权访问" });
    }
    expect((await teacher.get("/records/summary")).status).toBe(200);
    // 不能给他人安排计划
    const assign = await teacher.post("/plans", { name: `x ${STAMP}`, unitIds: [unitId], targets: { classIds: [classId], userIds: [] } });
    expect(assign.status).toBe(403);
    expect(assign.body.error.message).toBe("没有为他人安排计划的权限");
    // 管理员也一样（用系统词书的单元：老师的私有词书对个人版的管理员不可见）
    const sysBook = (await admin.get("/books")).body.data.items.find((b: { name: string; isSystem: boolean }) => b.name === "七年级下册" && b.isSystem);
    const sysUnit = (await admin.get(`/books/${sysBook.id}`)).body.data.units[0].id;
    const assignUser = await admin.post("/plans", { name: `y ${STAMP}`, unitIds: [sysUnit], targets: { classIds: [], userIds: [studentId] } });
    expect(assignUser.status).toBe(403);
    expect(assignUser.body.error.message).toBe("没有为他人安排计划的权限");
    // 管理员不再看到别人的计划与私有词书
    const adminPlans = (await admin.get("/plans")).body.data.items.map((p: { id: string }) => p.id);
    expect(adminPlans).not.toContain(classPlanId);
    expect((await admin.get(`/plans/${classPlanId}`)).status).toBe(404);
    const adminBooks = (await admin.get("/books")).body.data.items.map((b: { id: string }) => b.id);
    expect(adminBooks).not.toContain(teacherBookId);
    // 老师仍能管理自己创建的计划；计划进度只含自己
    expect((await teacher.get(`/plans/${classPlanId}`)).status).toBe(200);
    const progress = await teacher.get(`/plans/${classPlanId}/progress`);
    expect(progress.status).toBe(200);
    expect(progress.body.data.items.map((i: { userId: string }) => i.userId)).not.toContain(studentId);

    // 升级回班级版：原样恢复
    const up = await admin.put("/settings/edition", { edition: "school" });
    expect(up.status).toBe(200);
    expect(up.body.data).toMatchObject({ edition: "school", features: { classes: true, multiUser: true }, signupEnabled: true });
    const detail = await teacher.get(`/classes/${classId}`);
    expect(detail.status).toBe(200);
    expect(detail.body.data.members.map((m: { id: string }) => m.id)).toEqual([studentId]);
    expect(detail.body.data.plans.map((p: { id: string }) => p.id)).toEqual([classPlanId]);
    expect((await teacher.get(`/records/summary?userId=${studentId}`)).status).toBe(200);
    expect((await admin.get("/plans")).body.data.items.map((p: { id: string }) => p.id)).toContain(classPlanId);
    expect((await student.get("/me/classes")).body.data.items.map((c: { id: string }) => c.id)).toEqual([classId]);
  });
});
