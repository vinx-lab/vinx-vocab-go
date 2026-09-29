/**
 * 由旧 apps/api/tests/security.flow.test.ts 改写为纯 HTTP 黑盒（2026-09-18 独立审查 #1 #3 #5 #6）。
 *
 * 与旧测试的差异：
 * - 旧测试直接调用 startSession / recordAnswer / completeSession 等服务函数，这里改为学生本人走 HTTP；
 * - 「createdById 已写入」改为观察「老师能重置该账号密码」；
 * - 「ReviewLog 条数不变」改为观察重考那一组的 /records/sessions/:id 中每个词的 review 均为 null；
 * - 「StudySession_active_uniq 索引存在」无法经 HTTP 观察，见 contract/README.md「未覆盖」（Go 端由 store 单测覆盖）。
 *
 * 另含 Go 版的 Origin 检查（oracle 以开发模式运行、不做 Origin 检查，这部分只在 Go 上跑）。
 */
import { describe, it, expect, beforeAll } from "vitest";
import { anon, BASE_URL, Client, login, STAMP } from "../lib/client";
import { goOnly, ready } from "../lib/areas";

type Item = { wordId: string; definition: string; options?: string[] };

describe.runIf(ready("users", "classes", "plans", "study", "books"))("安全与一致性回归（需 seed）", () => {
  const emails = { teacher: `st-${STAMP}@vinx.test`, other: `so-${STAMP}@vinx.test`, student: `ss-${STAMP}@vinx.test` };
  let teacher: Client;
  let student: Client;
  let classId = "";
  let inviteCode = "";
  let studentId = "";
  let unitIds: string[] = [];

  beforeAll(async () => {
    const admin = await login("admin@vinx.test");
    for (const email of [emails.teacher, emails.other]) {
      const r = await admin.post("/users", { email, name: "安全测试老师", password: "dev123456", role: "teacher" });
      expect(r.status).toBe(200);
    }
    teacher = await login(emails.teacher);
    const cls = await teacher.post("/classes", { name: `安全班 ${STAMP}` });
    classId = cls.body.data.id;
    inviteCode = cls.body.data.inviteCode;
    student = anon();
    const signup = await student.post("/auth/signup", { email: emails.student, password: "dev123456", name: "自注册学生", inviteCode });
    expect(signup.status).toBe(201);
    studentId = signup.body.data.user.id;
    const books = await teacher.get("/books");
    const book = books.body.data.items.find((b: { name: string; isSystem: boolean }) => b.name === "七年级下册" && b.isSystem);
    const detail = await teacher.get(`/books/${book.id}`);
    unitIds = [detail.body.data.units[0].id];
  });

  it("#1 老师不能拉入或重置自行注册学生的账号；只能管理自己批量创建的账号", async () => {
    const other = await login(emails.other);
    const otherCls = await other.post("/classes", { name: `别人的班 ${STAMP}` });
    const pull = await other.post(`/classes/${otherCls.body.data.id}/members`, { account: emails.student });
    expect(pull.status).toBe(404);
    const reset = await teacher.post(`/classes/${classId}/members/${studentId}/reset-password`, { newPassword: "hacked1" });
    expect(reset.status).toBe(403);
    const detail = await teacher.get(`/classes/${classId}`);
    expect(detail.body.data.members.find((m: { id: string }) => m.id === studentId).managedByMe).toBe(false);

    // 批量创建的账号可以重置（等价于旧测试的 createdById 已写入）
    const prefix = `sec${STAMP.slice(-6)}`;
    const batch = await teacher.post(`/classes/${classId}/members/batch`, { names: ["甲"], prefix, password: "abc123" });
    expect(batch.status).toBe(200);
    const account = batch.body.data.created[0].account;
    expect(account).toBe(`${prefix}01`);
    const members = (await teacher.get(`/classes/${classId}`)).body.data.members as { id: string; managedByMe: boolean; email?: string }[];
    const created = members.find((m) => m.id !== studentId && m.managedByMe);
    expect(created).toBeTruthy();
    expect((await teacher.post(`/classes/${classId}/members/${created!.id}/reset-password`, { newPassword: "newpass1" })).status).toBe(200);
    expect((await anon().post("/auth/login", { email: account, password: "newpass1" })).status).toBe(200);
    // 弱密码不允许批量建号
    expect((await teacher.post(`/classes/${classId}/members/batch`, { names: ["乙"], prefix, password: "12345" })).status).toBe(400);

    // 老师账号不能凭邀请码入班
    expect((await other.post("/classes/join", { inviteCode })).status).toBe(403);
  });

  it("#3 进行中的检测不下发答案与例句，记录接口也不可提前查看", async () => {
    const plan = await teacher.post("/plans", { name: `安全检测 ${STAMP}`, kind: "test", modes: ["recognition"], testSize: 3, unitIds, targets: { classIds: [classId], userIds: [] } });
    expect(plan.status).toBe(200);
    const started = await student.post("/study/sessions", { kind: "test", planId: plan.body.data.id });
    expect(started.status).toBe(200);
    const detail = await student.get(`/study/sessions/${started.body.data.id}`);
    for (const i of detail.body.data.items) {
      expect(i.answer).toBe("");
      expect(i.definition).toBe("");
      expect(i.example).toBeNull();
      expect(i.options.length).toBeGreaterThan(1);
    }
    expect((await student.get(`/records/sessions/${started.body.data.id}`)).status).toBe(400);
    // 交卷，免得影响后面的用例
    expect((await student.post(`/study/sessions/${started.body.data.id}/complete`)).status).toBe(200);
  });

  it("#6 检测重考不再更新记忆；#5 删除计划时已作答的进行中组先结算", async () => {
    const daily = await teacher.post("/plans", { name: `安全每日 ${STAMP}`, newPerDay: 5, modes: ["recognition"], unitIds, targets: { classIds: [classId], userIds: [] } });
    const dailyId = daily.body.data.id;
    const learn = await student.post("/study/sessions", { kind: "learn", planId: dailyId });
    const learnId = learn.body.data.id;
    const items = (await student.get(`/study/sessions/${learnId}`)).body.data.items as Item[];
    for (const it of items) {
      const r = await student.post(`/study/sessions/${learnId}/answers`, { wordId: it.wordId, mode: "recognition", phase: "practice", attempt: 1, answer: it.definition });
      expect(r.status).toBe(200);
    }
    expect((await student.post(`/study/sessions/${learnId}/complete`)).status).toBe(200);

    const testPlan = await teacher.post("/plans", { name: `安全重考 ${STAMP}`, kind: "test", modes: ["recognition"], testSize: 5, testScope: "learned", unitIds, targets: { classIds: [], userIds: [studentId] } });
    const testPlanId = testPlan.body.data.id;
    const takeTest = async () => {
      const s = await student.post("/study/sessions", { kind: "test", planId: testPlanId });
      const id = s.body.data.id;
      const testItems = (await student.get(`/study/sessions/${id}`)).body.data.items as Item[];
      for (const it of testItems) {
        await student.post(`/study/sessions/${id}/answers`, { wordId: it.wordId, mode: "recognition", phase: "test", attempt: 1, answer: it.options?.[0] ?? "" });
      }
      const done = await student.post(`/study/sessions/${id}/complete`);
      expect(done.status).toBe(200);
      return { id, result: done.body.data.result };
    };
    await takeTest();
    const retake = await takeTest();
    expect(retake.result.ratings).toEqual({});
    const rec = await student.get(`/records/sessions/${retake.id}`);
    expect(rec.status).toBe(200);
    for (const w of rec.body.data.words) expect(w.review).toBeNull();

    const today = await student.get("/today");
    const card = today.body.data.plans.find((p: { planId: string }) => p.planId === dailyId);
    expect(card.newLeft).toBe(0);
    expect((await teacher.patch(`/plans/${dailyId}`, { newPerDay: 10 })).status).toBe(200);
    const second = await student.post("/study/sessions", { kind: "learn", planId: dailyId });
    const secondId = second.body.data.id;
    const it0 = ((await student.get(`/study/sessions/${secondId}`)).body.data.items as Item[])[0];
    await student.post(`/study/sessions/${secondId}/answers`, { wordId: it0.wordId, mode: "recognition", phase: "practice", attempt: 1, answer: it0.definition });
    expect((await teacher.del(`/plans/${dailyId}`)).status).toBe(200);
    const settled = await student.get(`/study/sessions/${secondId}`);
    expect(settled.body.data.status).toBe("completed");
    expect(settled.body.data.planId).toBeNull();
  });
});

describe.runIf(ready("users"))("能力守卫", () => {
  it("学生访问用户管理 → 403 无权访问", async () => {
    const s = await login("student@vinx.test");
    const res = await s.get("/users");
    expect(res.status).toBe(403);
    expect(res.body.error).toMatchObject({ code: "FORBIDDEN", message: "无权访问" });
  });
});

describe.runIf(goOnly)("Origin 检查（Go）", () => {
  const host = new URL(BASE_URL).host;

  it("写请求带外站 Origin → 403 非法请求来源（先于认证与业务处理）", async () => {
    const res = await anon().post("/auth/login", { email: "student@vinx.test", password: "dev123456" }, { headers: { origin: "http://evil.example" } });
    expect(res.status).toBe(403);
    expect(res.body.error).toMatchObject({ code: "FORBIDDEN", message: "非法请求来源" });
    expect(typeof res.body.error.requestId).toBe("string");
    expect(res.setCookie).toEqual([]);
    const out = await anon().post("/auth/logout", {}, { headers: { origin: "http://evil.example" } });
    expect(out.status).toBe(403);
  });

  it("无 Origin、同主机 Origin 放行；读请求不检查", async () => {
    expect((await anon().post("/auth/login", { email: "student@vinx.test", password: "dev123456" })).status).toBe(200);
    const same = await anon().post("/auth/login", { email: "student@vinx.test", password: "dev123456" }, { headers: { origin: `http://${host}` } });
    expect(same.status).toBe(200);
    expect((await anon().get("/health", { headers: { origin: "http://evil.example" } })).status).toBe(200);
  });

  it("不存在的接口仍是 404（Origin 检查只作用于已匹配的路由）", async () => {
    const res = await anon().post("/no-such-route", {}, { headers: { origin: "http://evil.example" } });
    expect(res.status).toBe(404);
  });
});
