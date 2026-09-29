/**
 * 班级版：班级、成员、邀请码、批量建号、班级概览、学生侧入班 / 退班（旧 apps/api/src/school/classes.routes.ts）。
 * K21：老师只能代管自己批量创建的学生账号；自行注册的学生只能凭邀请码入班；教师 / 管理员不能凭邀请码入班。
 *
 * 依赖 seed：teacher@vinx.test（DEMO01 班主任）、admin@vinx.test、student@vinx.test。
 * 每个用例用 STAMP 建自己的老师、班级与学生，不依赖其他用例创建的数据。
 */
import { describe, it, expect, beforeAll } from "vitest";
import { anon, login, STAMP, type Client } from "../lib/client";
import { ready } from "../lib/areas";
import { expectShape } from "../lib/shape";

const CODE_RE = /^[ABCDEFGHJKLMNPQRSTUVWXYZ23456789]{6}$/;

let seq = 0;
async function newTeacher(admin: Client, tag: string) {
  const email = `ct-${tag}-${STAMP}-${seq++}@vinx.test`;
  const r = await admin.post("/users", { email, name: `班级测试老师${tag}`, password: "dev123456", role: "teacher" });
  expect(r.status).toBe(200);
  return { client: await login(email), id: r.body.data.id as string, email };
}

async function newStudentWithCode(inviteCode: string | undefined, tag: string) {
  const c = anon();
  const email = `cs-${tag}-${STAMP}-${seq++}@vinx.test`;
  const r = await c.post("/auth/signup", { email, password: "dev123456", name: `学生${tag}`, ...(inviteCode ? { inviteCode } : {}) });
  expect(r.status).toBe(201);
  return { client: c, id: r.body.data.user.id as string, email };
}

describe.runIf(ready("classes", "users"))("班级（班级版）", () => {
  let admin: Client;
  beforeAll(async () => {
    admin = await login("admin@vinx.test");
  });

  it("建班：返回班级行；校验名称；学生无权", async () => {
    const t = await newTeacher(admin, "create");
    const res = await t.client.post("/classes", { name: `  一班 ${STAMP}  ` });
    expect(res.status).toBe(200);
    const cls = res.body.data;
    expect(cls).toMatchObject({ name: `一班 ${STAMP}`, teacherId: t.id, archived: false });
    expect(cls.inviteCode).toMatch(CODE_RE);
    expectShape("class-create", res.body);

    const empty = await t.client.post("/classes", { name: "   " });
    expect(empty.status).toBe(400);
    expect(empty.body.error).toMatchObject({ code: "VALIDATION", message: "参数校验失败", details: { name: "班级名称不能为空" } });
    const long = await t.client.post("/classes", { name: "x".repeat(41) });
    expect(long.body.error.details).toEqual({ name: "名称过长" });
    const missing = await t.client.post("/classes", {});
    expect(missing.body.error.details).toEqual({ name: "Required" });
    const badArchived = await t.client.post("/classes", { name: "x", archived: "yes" });
    expect(badArchived.body.error.details).toEqual({ archived: "Expected boolean, received string" });

    const student = await login("student@vinx.test");
    const denied = await student.post("/classes", { name: "x" });
    expect(denied.status).toBe(403);
    expect(denied.body.error).toMatchObject({ code: "FORBIDDEN", message: "无权访问" });
    expect((await student.get("/classes")).status).toBe(403);
    expect((await anon().get("/classes")).status).toBe(401);
  });

  it("列表：老师只看自己的班，未归档在前、新建在前；管理员看全部", async () => {
    const t = await newTeacher(admin, "list");
    const a = (await t.client.post("/classes", { name: `A ${STAMP}` })).body.data;
    const b = (await t.client.post("/classes", { name: `B ${STAMP}` })).body.data;
    const c = (await t.client.post("/classes", { name: `C ${STAMP}` })).body.data;
    const arch = await t.client.patch(`/classes/${b.id}`, { archived: true });
    expect(arch.status).toBe(200);
    expect(arch.body.data).toMatchObject({ id: b.id, archived: true, name: `B ${STAMP}` });
    expectShape("class-update", arch.body);

    const list = await t.client.get("/classes");
    expect(list.status).toBe(200);
    expect(list.body.data.total).toBe(3);
    expect(list.body.data.items.map((x: { id: string }) => x.id)).toEqual([c.id, a.id, b.id]);
    const first = list.body.data.items[0];
    expect(first).toMatchObject({ teacherId: t.id, teacherName: "班级测试老师list", memberCount: 0, planCount: 0, archived: false });
    expectShape("class-list", list.body);

    const all = await admin.get("/classes");
    const ids = all.body.data.items.map((x: { id: string }) => x.id);
    expect(ids).toEqual(expect.arrayContaining([a.id, b.id, c.id]));
    expect(all.body.data.total).toBe(all.body.data.items.length);
  });

  it("详情 / 修改 / 删除：只能管理自己的班级；不存在 404", async () => {
    const t = await newTeacher(admin, "detail");
    const other = await newTeacher(admin, "other");
    const cls = (await t.client.post("/classes", { name: `详情 ${STAMP}` })).body.data;
    const s = await newStudentWithCode(cls.inviteCode, "detail");

    const detail = await t.client.get(`/classes/${cls.id}`);
    expect(detail.status).toBe(200);
    expect(detail.body.data).toMatchObject({
      id: cls.id,
      name: `详情 ${STAMP}`,
      inviteCode: cls.inviteCode,
      archived: false,
      teacher: { id: t.id, name: "班级测试老师detail" },
      plans: [],
    });
    expect(detail.body.data.members).toEqual([{ id: s.id, name: "学生detail", email: s.email, joinedAt: expect.any(String), managedByMe: false }]);
    expectShape("class-detail", detail.body);
    // 管理员可代管任何账号
    const byAdmin = await admin.get(`/classes/${cls.id}`);
    expect(byAdmin.body.data.members[0].managedByMe).toBe(true);

    for (const [method, path, body] of [
      ["GET", `/classes/${cls.id}`, undefined],
      ["PATCH", `/classes/${cls.id}`, { name: "x" }],
      ["DELETE", `/classes/${cls.id}`, undefined],
      ["POST", `/classes/${cls.id}/invite-code`, undefined],
      ["GET", `/classes/${cls.id}/overview`, undefined],
      ["POST", `/classes/${cls.id}/members`, { account: s.email }],
      ["DELETE", `/classes/${cls.id}/members/${s.id}`, undefined],
    ] as const) {
      const r = await other.client.call(method, path, body);
      expect(r.status, `${method} ${path}`).toBe(403);
      expect(r.body.error).toMatchObject({ code: "FORBIDDEN", message: "只能管理自己的班级" });
    }
    const missing = await t.client.get("/classes/no-such-class");
    expect(missing.status).toBe(404);
    expect(missing.body.error).toMatchObject({ code: "NOT_FOUND", message: "班级不存在" });
    expect((await t.client.patch("/classes/no-such-class", { name: "x" })).status).toBe(404);

    // 部分更新：空对象也可以；名称校验同建班
    const noop = await t.client.patch(`/classes/${cls.id}`, {});
    expect(noop.status).toBe(200);
    expect(noop.body.data.name).toBe(`详情 ${STAMP}`);
    expect((await t.client.patch(`/classes/${cls.id}`, { name: "" })).body.error.details).toEqual({ name: "班级名称不能为空" });
    const renamed = await t.client.patch(`/classes/${cls.id}`, { name: " 改名 " });
    expect(renamed.body.data.name).toBe("改名");

    const del = await t.client.del(`/classes/${cls.id}`);
    expect(del.status).toBe(200);
    expect(del.body).toMatchObject({ success: true });
    expect(del.body.data).toBeUndefined();
    expect((await t.client.get(`/classes/${cls.id}`)).status).toBe(404);
    // 学生的班级列表里也没有了（级联删除成员）
    const mine = await s.client.get("/me/classes");
    expect(mine.body.data.items).toEqual([]);
  });

  it("换邀请码：旧码失效、新码可用；归档后邀请码无效", async () => {
    const t = await newTeacher(admin, "code");
    const cls = (await t.client.post("/classes", { name: `邀请码 ${STAMP}` })).body.data;
    const rotated = await t.client.post(`/classes/${cls.id}/invite-code`);
    expect(rotated.status).toBe(200);
    expect(rotated.body.data.id).toBe(cls.id);
    expect(rotated.body.data.inviteCode).toMatch(CODE_RE);
    expect(rotated.body.data.inviteCode).not.toBe(cls.inviteCode);
    expectShape("class-invite-code", rotated.body);

    const s = await newStudentWithCode(undefined, "code");
    const old = await s.client.post("/classes/join", { inviteCode: cls.inviteCode });
    expect(old.status).toBe(404);
    expect(old.body.error).toMatchObject({ code: "NOT_FOUND", message: "邀请码无效或班级已归档" });
    // 去空白、不区分大小写
    const ok = await s.client.post("/classes/join", { inviteCode: `  ${rotated.body.data.inviteCode.toLowerCase()} ` });
    expect(ok.status).toBe(200);
    expect(ok.body.data).toEqual({ id: cls.id, name: `邀请码 ${STAMP}` });
    // 重复加入不报错
    expect((await s.client.post("/classes/join", { inviteCode: rotated.body.data.inviteCode })).status).toBe(200);

    await t.client.patch(`/classes/${cls.id}`, { archived: true });
    const s2 = await newStudentWithCode(undefined, "code2");
    const archived = await s2.client.post("/classes/join", { inviteCode: rotated.body.data.inviteCode });
    expect(archived.status).toBe(404);
    // 注册时带归档班级的邀请码也不行
    const signup = await anon().post("/auth/signup", { email: `cs-arch-${STAMP}@vinx.test`, password: "dev123456", inviteCode: rotated.body.data.inviteCode });
    expect(signup.status).toBe(404);
  });

  it("学生侧：凭邀请码入班、查看我的班级、退班；校验与角色限制", async () => {
    const t = await newTeacher(admin, "join");
    const cls = (await t.client.post("/classes", { name: `入班 ${STAMP}` })).body.data;
    const s = await newStudentWithCode(undefined, "join");

    const short = await s.client.post("/classes/join", { inviteCode: " ab " });
    expect(short.status).toBe(400);
    expect(short.body.error.details).toEqual({ inviteCode: "请输入邀请码" });
    expect((await s.client.post("/classes/join", {})).body.error.details).toEqual({ inviteCode: "Required" });

    expect((await s.client.post("/classes/join", { inviteCode: cls.inviteCode })).status).toBe(200);
    const mine = await s.client.get("/me/classes");
    expect(mine.status).toBe(200);
    expect(mine.body.data).toEqual({
      items: [{ id: cls.id, name: `入班 ${STAMP}`, teacherName: "班级测试老师join", archived: false, joinedAt: expect.any(String) }],
      total: 1,
    });
    expectShape("me-classes", mine.body);

    // 老师看到成员；自行注册的学生不可代管
    const detail = await t.client.get(`/classes/${cls.id}`);
    expect(detail.body.data.members.map((m: { id: string; managedByMe: boolean }) => [m.id, m.managedByMe])).toEqual([[s.id, false]]);
    const list = await t.client.get("/classes");
    expect(list.body.data.items[0].memberCount).toBe(1);

    const leave = await s.client.del(`/me/classes/${cls.id}`);
    expect(leave.status).toBe(200);
    expect(leave.body.data).toBeUndefined();
    expect((await s.client.get("/me/classes")).body.data).toEqual({ items: [], total: 0 });
    // 不在班里也返回成功
    expect((await s.client.del(`/me/classes/${cls.id}`)).status).toBe(200);
    expect((await anon().get("/me/classes")).status).toBe(401);

    // 教师 / 管理员不能凭邀请码入班
    for (const c of [t.client, admin]) {
      const r = await c.post("/classes/join", { inviteCode: cls.inviteCode });
      expect(r.status).toBe(403);
      expect(r.body.error).toMatchObject({ code: "FORBIDDEN", message: "教师和管理员账号不能加入班级" });
    }
    // 老师的「我的班级」为空列表（接口对所有登录者开放）
    expect((await t.client.get("/me/classes")).body.data).toEqual({ items: [], total: 0 });
  });

  it("批量建号：账号 = 前缀 + 两位序号，跳过已占用；全部入班；校验", async () => {
    const t = await newTeacher(admin, "batch");
    const cls = (await t.client.post("/classes", { name: `批量 ${STAMP}` })).body.data;
    const prefix = `b${STAMP.slice(-8)}`;
    // 先占用 prefix02
    expect((await t.client.post("/users", { email: `${prefix}02`, name: "占位", password: "dev123456" })).status).toBe(200);

    const res = await t.client.post(`/classes/${cls.id}/members/batch`, { names: [" 张三 ", "李四", "王五"], prefix: `  ${prefix.toUpperCase()} `, password: "abc123" });
    expect(res.status).toBe(200);
    expect(res.body.data).toEqual({
      created: [
        { name: "张三", account: `${prefix}01` },
        { name: "李四", account: `${prefix}03` },
        { name: "王五", account: `${prefix}04` },
      ],
    });
    expectShape("class-batch", res.body);
    const login01 = await anon().post("/auth/login", { email: `${prefix}01`, password: "abc123" });
    expect(login01.status).toBe(200);
    expect(login01.body.data.user).toMatchObject({ role: "student", name: "张三" });

    const detail = await t.client.get(`/classes/${cls.id}`);
    expect(detail.body.data.members.map((m: { email: string; managedByMe: boolean }) => [m.email, m.managedByMe])).toEqual([
      [`${prefix}01`, true],
      [`${prefix}03`, true],
      [`${prefix}04`, true],
    ]);

    const bad = async (body: object) => (await t.client.post(`/classes/${cls.id}/members/batch`, body)).body.error;
    expect(await bad({ names: [], prefix, password: "abc123" })).toMatchObject({ code: "VALIDATION", details: { names: "请至少填写一个姓名" } });
    expect((await bad({ names: Array(101).fill("a"), prefix, password: "abc123" })).details).toEqual({ names: "一次最多 100 人" });
    expect((await bad({ names: ["a", " "], prefix, password: "abc123" })).details).toEqual({ "names.1": "String must contain at least 1 character(s)" });
    expect((await bad({ names: ["x".repeat(31)], prefix, password: "abc123" })).details).toEqual({ "names.0": "String must contain at most 30 character(s)" });
    expect((await bad({ names: ["a"], prefix: "1abc", password: "abc123" })).details).toEqual({ prefix: "账号前缀需以字母开头，2–20 位字母数字" });
    expect((await bad({ names: ["a"], prefix: "a", password: "abc123" })).details).toEqual({ prefix: "账号前缀需以字母开头，2–20 位字母数字" });
    expect((await bad({ names: ["a"], prefix, password: "12345" })).details).toEqual({ password: "初始密码至少 6 位" });
    expect((await bad({})).details).toEqual({ names: "Required", prefix: "Required", password: "Required" });

    // 学生没有 classes 能力
    const s = await login("student@vinx.test");
    expect((await s.post(`/classes/${cls.id}/members/batch`, { names: ["a"], prefix, password: "abc123" })).status).toBe(403);
    // 班级不存在
    const nf = await t.client.post(`/classes/no-such/members/batch`, { names: ["a"], prefix, password: "abc123" });
    expect(nf.status).toBe(404);
    expect(nf.body.error.message).toBe("班级不存在");
  });

  it("按账号添加 / 移出成员：老师只能加自己创建的学生账号（K21）", async () => {
    const t = await newTeacher(admin, "add");
    const other = await newTeacher(admin, "add2");
    const cls = (await t.client.post("/classes", { name: `加人 ${STAMP}` })).body.data;
    const email = `ca-${STAMP}@vinx.test`;
    const created = await t.client.post("/users", { email, name: "代建学生", password: "dev123456" });
    expect(created.status).toBe(200);
    const sid = created.body.data.id;

    const add = await t.client.post(`/classes/${cls.id}/members`, { account: `  ${email.toUpperCase()} ` });
    expect(add.status).toBe(200);
    expect(add.body.data).toEqual({ id: sid, name: "代建学生", email });
    expectShape("class-add-member", add.body);
    // 重复添加不报错
    expect((await t.client.post(`/classes/${cls.id}/members`, { account: email })).status).toBe(200);
    expect((await t.client.get(`/classes/${cls.id}`)).body.data.members).toHaveLength(1);

    // 别的老师的班：拉不进来
    const otherCls = (await other.client.post("/classes", { name: `别人 ${STAMP}` })).body.data;
    const pull = await other.client.post(`/classes/${otherCls.id}/members`, { account: email });
    expect(pull.status).toBe(404);
    expect(pull.body.error).toMatchObject({ code: "NOT_FOUND", message: "找不到你创建的这个学生账号；其他学生请让对方用邀请码加入" });
    const ghost = await t.client.post(`/classes/${cls.id}/members`, { account: "nobody-here" });
    expect(ghost.status).toBe(404);
    expect((await t.client.post(`/classes/${cls.id}/members`, { account: "  " })).body.error.details).toEqual({ account: "请输入账号" });

    // 管理员可以加任何学生，但只能加学生账号
    const adminCls = (await admin.post("/classes", { name: `管理员班 ${STAMP}` })).body.data;
    expect((await admin.post(`/classes/${adminCls.id}/members`, { account: email })).status).toBe(200);
    const notStudent = await admin.post(`/classes/${adminCls.id}/members`, { account: other.email });
    expect(notStudent.status).toBe(400);
    expect(notStudent.body.error).toMatchObject({ code: "VALIDATION", message: "只能添加学生账号" });
    // 老师自己建的老师账号？只有管理员能建老师，这里略

    const rm = await t.client.del(`/classes/${cls.id}/members/${sid}`);
    expect(rm.status).toBe(200);
    expect(rm.body.data).toBeUndefined();
    expect((await t.client.get(`/classes/${cls.id}`)).body.data.members).toEqual([]);
    // 不在班里也返回成功
    expect((await t.client.del(`/classes/${cls.id}/members/${sid}`)).status).toBe(200);
    await admin.del(`/classes/${adminCls.id}`);
  });

  it("班级内重置密码：只能重置自己批量创建的学生；校验", async () => {
    const t = await newTeacher(admin, "reset");
    const cls = (await t.client.post("/classes", { name: `重置 ${STAMP}` })).body.data;
    const prefix = `r${STAMP.slice(-8)}`;
    await t.client.post(`/classes/${cls.id}/members/batch`, { names: ["甲"], prefix, password: "abc123" });
    const self = await newStudentWithCode(cls.inviteCode, "reset");
    const members = (await t.client.get(`/classes/${cls.id}`)).body.data.members as { id: string; email: string }[];
    const mine = members.find((m) => m.email === `${prefix}01`)!;

    const url = (uid: string) => `/classes/${cls.id}/members/${uid}/reset-password`;
    const ok = await t.client.post(url(mine.id), { newPassword: "newpass1" });
    expect(ok.status).toBe(200);
    expect(ok.body.data).toBeUndefined();
    expect((await anon().post("/auth/login", { email: `${prefix}01`, password: "newpass1" })).status).toBe(200);

    const denied = await t.client.post(url(self.id), { newPassword: "hacked1" });
    expect(denied.status).toBe(403);
    expect(denied.body.error.message).toBe("只能重置你批量创建的学生账号的密码，自行注册的学生请联系管理员");
    expect((await t.client.post(url(mine.id), { newPassword: "" })).body.error.details).toEqual({ newPassword: "新密码不能为空" });
    const weak = await t.client.post(url(mine.id), { newPassword: "12345" });
    expect(weak.status).toBe(400);
    expect(weak.body.error).toMatchObject({ code: "VALIDATION", message: "密码长度至少 6 位" });
    const notIn = await t.client.post(url("no-such-user"), { newPassword: "abcdef" });
    expect(notIn.status).toBe(404);
    expect(notIn.body.error.message).toBe("该学生不在班级中");
    // 管理员可以重置自行注册的学生
    expect((await admin.post(url(self.id), { newPassword: "byadmin1" })).status).toBe(200);
    // 学生没有 users 能力
    expect((await self.client.post(url(mine.id), { newPassword: "abcdef" })).status).toBe(403);
  });

  it("班级概览：成员今日状态、汇总、难词、近 14 天活跃", async () => {
    const t = await newTeacher(admin, "ov");
    const cls = (await t.client.post("/classes", { name: `概览 ${STAMP}` })).body.data;
    const empty = await t.client.get(`/classes/${cls.id}/overview`);
    expect(empty.status).toBe(200);
    expect(empty.body.data.class).toEqual({ id: cls.id, name: `概览 ${STAMP}`, inviteCode: cls.inviteCode, memberCount: 0 });
    expect(empty.body.data.students).toEqual([]);
    expect(empty.body.data.hardWords).toEqual([]);
    expect(empty.body.data.activeByDay).toHaveLength(14);
    expect(empty.body.data.summary).toEqual({ doneToday: 0, inProgress: 0, notStarted: 0, noPlan: 0, accuracy7d: expect.anything() });
    expectShape("class-overview-empty", empty.body);

    const s = await newStudentWithCode(cls.inviteCode, "ov");
    const s2 = await newStudentWithCode(cls.inviteCode, "ov2");
    // 给班级布置计划，s 答几道题（答错一题），s2 不动
    const books = await t.client.get("/books");
    const book = books.body.data.items.find((b: { name: string; isSystem: boolean }) => b.name === "七年级下册" && b.isSystem);
    const unitId = (await t.client.get(`/books/${book.id}`)).body.data.units[0].id;
    const plan = await t.client.post("/plans", { name: `概览计划 ${STAMP}`, newPerDay: 3, modes: ["recognition"], unitIds: [unitId], targets: { classIds: [cls.id], userIds: [] } });
    expect(plan.status).toBe(200);
    const started = await s.client.post("/study/sessions", { kind: "learn", planId: plan.body.data.id });
    expect(started.status).toBe(200);
    const items = (await s.client.get(`/study/sessions/${started.body.data.id}`)).body.data.items as { wordId: string; definition: string }[];
    for (const [i, it] of items.entries()) {
      await s.client.post(`/study/sessions/${started.body.data.id}/answers`, { wordId: it.wordId, mode: "recognition", phase: "practice", attempt: 1, answer: i === 0 ? "错的" : it.definition });
    }

    const ov = await t.client.get(`/classes/${cls.id}/overview`);
    expect(ov.status).toBe(200);
    const d = ov.body.data;
    expect(d.class.memberCount).toBe(2);
    expect(d.students.map((x: { userId: string }) => x.userId)).toEqual([s.id, s2.id]);
    const st = d.students[0];
    expect(st).toMatchObject({ name: "学生ov", email: s.email, streak: 0, lastActiveDay: null, learnedWords: 0, activeDays7: 0 });
    expect(st.today).toMatchObject({ status: "in-progress", answers: items.length, newLeft: 3, pendingTests: 0 });
    expect(d.students[1].today.status).toBe("not-started");
    expect(d.summary).toMatchObject({ inProgress: 1, notStarted: 1, doneToday: 0, noPlan: 0 });
    expect(d.summary.accuracy7d).toMatchObject({ total: items.length, correct: items.length - 1 });
    expect(d.hardWords).toEqual([{ wordId: items[0].wordId, wrong: 1, total: 1, rate: 1, spelling: expect.any(String), definition: items[0].definition }]);
    const todayPoint = d.activeByDay.find((p: { day: string }) => p.day === d.day);
    expect(todayPoint).toMatchObject({ activeStudents: 1, answers: items.length });
    expect(d.activeByDay[0].accuracy).toBeNull();
    expectShape("class-overview", ov.body);

    // 详情里能看到班级的计划
    const detail = await t.client.get(`/classes/${cls.id}`);
    expect(detail.body.data.plans).toEqual([{ id: plan.body.data.id, name: `概览计划 ${STAMP}`, kind: "daily", status: "active", newPerDay: 3 }]);
    expect((await t.client.get("/classes")).body.data.items[0].planCount).toBe(1);

    // 老师能看本班学生的记录（students.view），看不了别人的
    expect((await t.client.get(`/records/summary?userId=${s.id}`)).status).toBe(200);
    const other = await newTeacher(admin, "ov-other");
    const r = await other.client.get(`/records/summary?userId=${s.id}`);
    expect(r.status).toBe(403);
    expect(r.body.error.message).toBe("只能查看本班学生");
    // 学生退班后老师不能再看
    await s.client.del(`/me/classes/${cls.id}`);
    expect((await t.client.get(`/records/summary?userId=${s.id}`)).status).toBe(403);
    await s.client.del(`/study/sessions/${started.body.data.id}`);
  });
});
