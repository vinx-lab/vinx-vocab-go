/**
 * 班级自主开关与「先定目标词书，再建计划」（spec 0008，只在 Go 上跑）：
 * 建班带或不带 allowSelfPlan、切换、影响统计、我的目标词书的 source / canEditOwn、
 * 超出目标时 400、不允许时学生自建 403、算出来的暂停（今日不出现、列表标记、重新允许后恢复）、
 * 建计划页的约束集合接口、班级概览的两种口径。
 * 每个用例组用 STAMP 建自己的老师、班级、学生与词书，不依赖其他用例。
 */
import { beforeAll, describe, expect, it } from "vitest";
import { login, type Client } from "../lib/client";
import { goOnly } from "../lib/areas";
import { importBook, newClassWithStudent, newStudent, newTeacher, uniq, type Account, type BuiltBook } from "../lib/fixtures";

describe.runIf(goOnly)("班级自主开关与目标优先", () => {
  let admin: Client;
  let teacher: Account, other: Account, student: Account, loner: Account;
  let classId = "";
  let bookA: BuiltBook, bookB: BuiltBook;

  const ids = (xs: { id: string }[]) => xs.map((x) => x.id);

  async function myTargets(c: Client) {
    const r = await c.get("/me/target-books");
    expect(r.status, r.text).toBe(200);
    return r.body.data as { source: string; books: { id: string; source: string; classNames?: string[] }[]; ownBooks: { id: string }[]; canEditOwn: boolean };
  }

  async function todayPlanIds(c: Client) {
    const r = await c.get("/today");
    expect(r.status, r.text).toBe(200);
    return {
      ids: (r.body.data.plans as { planId: string }[]).map((p) => p.planId),
      outside: r.body.data.outsideTargetPlanIds as string[],
      paused: r.body.data.pausedSelfPlans as number,
    };
  }

  async function setAllow(allow: boolean) {
    const r = await teacher.client.patch(`/classes/${classId}`, { allowSelfPlan: allow });
    expect(r.status, r.text).toBe(200);
    expect(r.body.data.allowSelfPlan).toBe(allow);
  }

  beforeAll(async () => {
    admin = await login("admin@vinx.test");
    teacher = await newTeacher(admin, "自主");
    other = await newTeacher(admin, "自主2");
    const cls = await newClassWithStudent(teacher.client, "自主");
    classId = cls.classId;
    student = cls.student;
    loner = await newStudent("自主-loner");
    const tag = uniq();
    bookA = await importBook(teacher.client, "自主甲", [{ name: "Unit 1", entries: ["alpha", "bravo"].map((s, i) => ({ spelling: `vxsp${s}${tag}`, definition: `甲义${i}` })) }]);
    bookB = await importBook(teacher.client, "自主乙", [{ name: "Unit 1", entries: ["charlie", "delta"].map((s, i) => ({ spelling: `vxsp${s}${tag}`, definition: `乙义${i}` })) }]);
  });

  it("建班：allowSelfPlan 可选、缺省为 true；类型错误 400；详情、列表、修改都带这个字段", async () => {
    const dflt = await teacher.client.get(`/classes/${classId}`);
    expect(dflt.status).toBe(200);
    expect(dflt.body.data.allowSelfPlan).toBe(true);

    const closed = await teacher.client.post("/classes", { name: `不允许 ${uniq()}`, allowSelfPlan: false });
    expect(closed.status, closed.text).toBe(200);
    expect(closed.body.data.allowSelfPlan).toBe(false);
    const open = await teacher.client.post("/classes", { name: `允许 ${uniq()}`, allowSelfPlan: true });
    expect(open.body.data.allowSelfPlan).toBe(true);

    const bad = await teacher.client.post("/classes", { name: `坏 ${uniq()}`, allowSelfPlan: "no" });
    expect(bad.status).toBe(400);
    expect(bad.body.error.details).toEqual({ allowSelfPlan: "Expected boolean, received string" });

    const list = await teacher.client.get("/classes");
    const row = (list.body.data.items as { id: string; allowSelfPlan: boolean }[]).find((c) => c.id === closed.body.data.id);
    expect(row?.allowSelfPlan).toBe(false);

    // 只改名不动开关；别班老师不能改
    const renamed = await teacher.client.patch(`/classes/${closed.body.data.id}`, { name: `改名 ${uniq()}` });
    expect(renamed.body.data.allowSelfPlan).toBe(false);
    expect((await other.client.patch(`/classes/${classId}`, { allowSelfPlan: false })).status).toBe(403);
    expect((await other.client.get(`/classes/${classId}/self-plan-impact`)).status).toBe(403);
    expect((await student.client.get(`/classes/${classId}/self-plan-impact`)).status).toBe(403);
  });

  it("目标词书：班级的书锁定（source = class），允许自主时可以追加（source = own）；不允许时自己设的不生效", async () => {
    expect((await teacher.client.put(`/classes/${classId}/target-books`, { bookIds: [bookA.bookId] })).status).toBe(200);
    // 同一本书既在班级目标里又是自己加的：按班级算，显示一次
    const saved = await student.client.put("/me/target-books", { bookIds: [bookB.bookId, bookA.bookId] });
    expect(saved.status, saved.text).toBe(200);
    let t = await myTargets(student.client);
    expect(t.source).toBe("class");
    expect(t.canEditOwn).toBe(true);
    expect(ids(t.books)).toEqual([bookA.bookId, bookB.bookId]);
    expect(t.books.map((b) => b.source)).toEqual(["class", "own"]);
    expect(t.books[0].classNames).toHaveLength(1);
    expect(ids(t.ownBooks)).toEqual([bookB.bookId, bookA.bookId]);
    // 覆盖进度按有效目标算（2 + 2 词）
    const cov = await student.client.get("/records/coverage");
    expect(cov.body.data.total.target).toBe(4);

    await setAllow(false);
    t = await myTargets(student.client);
    expect(t.canEditOwn).toBe(false);
    expect(ids(t.books)).toEqual([bookA.bookId]);
    expect(ids(t.ownBooks)).toEqual([bookB.bookId, bookA.bookId]);
    await setAllow(true);

    // 没有班级：自己的，永远能编辑
    expect((await loner.client.put("/me/target-books", { bookIds: [bookA.bookId] })).status).toBe(400); // 别的老师的私有词书不可见
    const l = await myTargets(loner.client);
    expect(l).toMatchObject({ source: "none", canEditOwn: true, books: [], ownBooks: [] });
  });

  it("建计划必须落在目标里：超出目标 400；只改节奏不检查；目标为空不约束；约束集合接口", async () => {
    // 学生的有效目标 = 甲（班级）+ 乙（自选）
    const okPlan = await student.client.post("/plans", { name: `自建 ${uniq()}`, unitIds: [bookB.units[0].id] });
    expect(okPlan.status, okPlan.text).toBe(200);
    expect(okPlan.body.data).toMatchObject({ selfPlanPaused: false, outsideTarget: false });

    // 班级目标只有甲：给班级布置乙 → 400
    const bad = await teacher.client.post("/plans", { name: `班级 ${uniq()}`, unitIds: [bookB.units[0].id], targets: { classIds: [classId], userIds: [] } });
    expect(bad.status).toBe(400);
    expect(bad.body.error.code).toBe("VALIDATION");
    expect(bad.body.error.message).toMatch(/^所选单元不在目标词书内：《自主乙 /);
    const good = await teacher.client.post("/plans", { name: `班级 ${uniq()}`, unitIds: [bookA.units[0].id], targets: { classIds: [classId], userIds: [] } });
    expect(good.status, good.text).toBe(200);

    // 给学生本人：按他的有效目标（含自选）→ 乙可以
    const toStudent = await teacher.client.post("/plans", { name: `给学生 ${uniq()}`, unitIds: [bookB.units[0].id], targets: { classIds: [], userIds: [student.id] } });
    expect(toStudent.status, toStudent.text).toBe(200);
    // 班级 + 学生：取交集（只有甲）
    const both = await teacher.client.post("/plans", { name: `交集 ${uniq()}`, unitIds: [bookB.units[0].id], targets: { classIds: [classId], userIds: [student.id] } });
    expect(both.status).toBe(400);
    const allowed = await teacher.client.post("/plans/allowed-books", { targets: { classIds: [classId], userIds: [student.id] } });
    expect(allowed.status, allowed.text).toBe(200);
    expect(allowed.body.data.constrained).toBe(true);
    expect(ids(allowed.body.data.books)).toEqual([bookA.bookId]);

    // 老师自己没有目标：不约束
    const self = await teacher.client.post("/plans/allowed-books", {});
    expect(self.body.data).toMatchObject({ constrained: false, books: [], selfPlanAllowed: true });

    // 只改节奏不检查；改单元时新增超出目标的单元才拒绝
    const planId = good.body.data.id as string;
    expect((await teacher.client.patch(`/plans/${planId}`, { newPerDay: 3 })).status).toBe(200);
    expect((await teacher.client.patch(`/plans/${planId}`, { unitIds: [bookA.units[0].id] })).status).toBe(200);
    const addBad = await teacher.client.patch(`/plans/${planId}`, { unitIds: [bookA.units[0].id, bookB.units[0].id] });
    expect(addBad.status).toBe(400);
    expect(addBad.body.error.message).toMatch(/^所选单元不在目标词书内/);
  });

  it("不允许自主：学生自建 403；已有的自建计划算出来的暂停，重新允许后恢复；影响统计", async () => {
    const mine = await student.client.post("/plans", { name: `暂停 ${uniq()}`, newPerDay: 2, modes: ["recognition"], unitIds: [bookA.units[0].id] });
    expect(mine.status, mine.text).toBe(200);
    const planId = mine.body.data.id as string;
    expect((await todayPlanIds(student.client)).ids).toContain(planId);

    const impact = await teacher.client.get(`/classes/${classId}/self-plan-impact`);
    expect(impact.status, impact.text).toBe(200);
    expect(impact.body.data.students).toBe(1);
    expect(impact.body.data.selfPlans).toBeGreaterThanOrEqual(2); // 上一个用例的自建计划 + 这一个
    expect(impact.body.data.ownBooks).toBe(2);

    await setAllow(false);
    const denied = await student.client.post("/plans", { name: `再建 ${uniq()}`, unitIds: [bookA.units[0].id] });
    expect(denied.status).toBe(403);
    expect(denied.body.error).toMatchObject({ code: "FORBIDDEN", message: "班级未开放自主安排计划" });
    expect((await student.client.post("/plans/allowed-books", {})).body.data.selfPlanAllowed).toBe(false);

    const today = await todayPlanIds(student.client);
    expect(today.ids).not.toContain(planId);
    expect(today.paused).toBeGreaterThanOrEqual(1);
    const detail = await student.client.get(`/plans/${planId}`);
    expect(detail.body.data).toMatchObject({ status: "active", selfPlanPaused: true });
    const start = await student.client.post("/study/sessions", { kind: "learn", planId });
    expect(start.status).toBe(403);
    // 已经不允许：再关一次没有影响
    expect((await teacher.client.get(`/classes/${classId}/self-plan-impact`)).body.data).toEqual({ students: 0, selfPlans: 0, ownBooks: 0 });

    await setAllow(true);
    expect((await todayPlanIds(student.client)).ids).toContain(planId);
    expect((await student.client.get(`/plans/${planId}`)).body.data.selfPlanPaused).toBe(false);
  });

  it("不在目标词书内：已有计划照常能学，今日页与列表标记", async () => {
    // 老师给学生布置乙之后，学生把乙从自选里去掉 → 计划仍在，标「不在目标词书内」
    const plan = await teacher.client.post("/plans", { name: `标记 ${uniq()}`, newPerDay: 2, unitIds: [bookB.units[0].id], targets: { classIds: [], userIds: [student.id] } });
    expect(plan.status, plan.text).toBe(200);
    expect((await student.client.put("/me/target-books", { bookIds: [] })).status).toBe(200);
    const today = await todayPlanIds(student.client);
    expect(today.ids).toContain(plan.body.data.id);
    expect(today.outside).toContain(plan.body.data.id);
    const list = await student.client.get("/plans?scope=mine");
    const row = (list.body.data.items as { id: string; outsideTarget: boolean }[]).find((p) => p.id === plan.body.data.id);
    expect(row?.outsideTarget).toBe(true);
  });

  it("班级概览：允许自主按学生自己的目标（含自选），不允许只按本班目标", async () => {
    expect((await student.client.put("/me/target-books", { bookIds: [bookB.bookId] })).status).toBe(200);
    const row = async () => {
      const r = await teacher.client.get(`/classes/${classId}/overview`);
      expect(r.status).toBe(200);
      const s = (r.body.data.students as { userId: string; coverage: { target: number } | null; coverageIncludesOwn: boolean }[]).find((x) => x.userId === student.id)!;
      return { mode: r.body.data.coverageMode as string, target: s.coverage?.target, own: s.coverageIncludesOwn };
    };
    expect(await row()).toEqual({ mode: "student", target: 4, own: true });
    await setAllow(false);
    expect(await row()).toEqual({ mode: "class", target: 2, own: false });
    await setAllow(true);
  });
});
