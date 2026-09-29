/**
 * 由旧 apps/api/src/routes/plans.ts 改写为纯 HTTP 黑盒。
 * 依赖 seed 数据：teacher@vinx.test（班主任，班级 DEMO01，seed 已建计划「八上 Unit 1–2 每日背词」
 * 安排给 DEMO01）、student@vinx.test / student2@vinx.test（DEMO01 成员）、admin@vinx.test（不在任何班级）。
 * DEMO01 的班级 id 从 seed 计划的 targets 里读取（A3 阶段 /classes 尚未实现，没有别的黑盒途径拿到它）。
 * K22（删除计划前结算已作答进行中组）的完整路径依赖 /study 系列接口（A4），用 ready("study") 门控，
 * 目前只在 oracle 上跑；Go 侧先由 internal/service 的单测覆盖「未作答组直接删」这条最常见路径以外的分支。
 * 每个用例用 STAMP 生成自己的词书/单元/计划，不依赖其他用例创建的数据。
 */
import { describe, it, expect } from "vitest";
import { anon, login, STAMP } from "../lib/client";
import { ready } from "../lib/areas";
import { expectShape } from "../lib/shape";

async function demoClassId(): Promise<string> {
  // teacher@vinx.test 是 DEMO01 的班主任：seed 计划「八上 Unit 1–2 每日背词」由他创建并安排给 DEMO01，
  // 用 scope=created 只看自己创建的，避免其他契约用例留下的、他不管理的班级干扰。
  const teacher = await login("teacher@vinx.test");
  const list = await teacher.get("/plans?scope=created");
  for (const p of list.body.data.items as Array<{ targets: Array<{ type: string; id: string; name: string }> }>) {
    const cls = p.targets.find((t) => t.type === "class" && t.name === "八年级一班");
    if (cls) return cls.id;
  }
  throw new Error("没有找到 teacher@vinx.test 安排给「八年级一班」的 seed 计划，无法取得 DEMO01 的 id");
}

async function makeUnit(teacher: Awaited<ReturnType<typeof login>>, tag: string, words = 3) {
  const book = await teacher.post("/books", { name: `plan-src-${tag}` });
  const bookId = book.body.data.id as string;
  const unit = await teacher.post(`/books/${bookId}/units`, { name: "U1" });
  const unitId = unit.body.data.id as string;
  for (let i = 0; i < words; i++) {
    await teacher.post(`/units/${unitId}/words`, { spelling: `w${tag}${i}`, definition: `释义${i}` });
  }
  return { bookId, unitId };
}

describe.runIf(ready("plans"))("学习计划", () => {
  it("预览：空数组、正常范围、包含不可见单元", async () => {
    const teacher = await login("teacher@vinx.test");
    const empty = await teacher.post("/plans/preview", { unitIds: [] });
    expect(empty.status).toBe(200);
    expect(empty.body.data).toEqual({ wordCount: 0, units: [] });

    const { unitId } = await makeUnit(teacher, `prev${STAMP}`, 4);
    const preview = await teacher.post("/plans/preview", { unitIds: [unitId] });
    expect(preview.status).toBe(200);
    expect(preview.body.data.wordCount).toBe(4);
    expect(preview.body.data.units).toEqual([{ id: unitId, name: "U1", wordCount: 4 }]);

    // 系统词书之外、他人不可见的单元（用另一个老师的班没有，这里用不存在的 id 模拟「不可用」）
    const bad = await teacher.post("/plans/preview", { unitIds: [unitId, "not-a-real-unit"] });
    expect(bad.status).toBe(400);
    expect(bad.body.error).toMatchObject({ code: "VALIDATION", message: "包含不可用的单元" });
  });

  it("创建：默认安排给自己；校验；学生可建自用计划但不能安排他人", async () => {
    const teacher = await login("teacher@vinx.test");
    const student = await login("student@vinx.test");
    const { unitId } = await makeUnit(teacher, `c${STAMP}`, 2);

    const created = await teacher.post("/plans", { name: `plan-${STAMP}`, unitIds: [unitId] });
    expect(created.status).toBe(200);
    expect(created.body.data).toMatchObject({
      name: `plan-${STAMP}`, kind: "daily", status: "active", newPerDay: 10, reviewPerDay: 50,
      modes: ["recognition", "spelling"], order: "sequential", testSize: 20, testScope: "all",
      startDate: null, endDate: null, wordCount: 2, isSelfPlan: true, targetsMe: true, canEdit: true,
    });
    expect(created.body.data.targets).toEqual([{ type: "user", id: created.body.data.creator.id, name: "王老师", email: "teacher@vinx.test" }]);
    expectShape("plans-detail", created.body);

    // 学生给自己建计划
    const { unitId: sUnit } = await makeUnit(teacher, `s${STAMP}`, 1);
    // 学生没有 books.edit 权限，用老师建好的单元
    const selfPlan = await student.post("/plans", { name: `self-${STAMP}`, unitIds: [sUnit] });
    expect(selfPlan.status).toBe(200);
    expect(selfPlan.body.data.isSelfPlan).toBe(true);

    // 学生不能安排给他人
    const cid = await demoClassId();
    const denyAssign = await student.post("/plans", { name: `deny2-${STAMP}`, unitIds: [sUnit], targets: { classIds: [cid] } });
    expect(denyAssign.status).toBe(403);
    expect(denyAssign.body.error).toMatchObject({ code: "FORBIDDEN", message: "没有为他人安排计划的权限" });

    // 校验：空名称、unitIds 为空数组、modes 为空数组、日期倒挂
    const emptyName = await teacher.post("/plans", { name: "", unitIds: [unitId] });
    expect(emptyName.status).toBe(400);
    expect(emptyName.body.error.details).toEqual({ name: "请填写计划名称" });

    const noUnits = await teacher.post("/plans", { name: `nu-${STAMP}`, unitIds: [] });
    expect(noUnits.status).toBe(400);
    expect(noUnits.body.error.details).toEqual({ unitIds: "请至少选择一个单元" });

    const noModes = await teacher.post("/plans", { name: `nm-${STAMP}`, unitIds: [unitId], modes: [] });
    expect(noModes.status).toBe(400);
    expect(noModes.body.error.details).toEqual({ modes: "至少选择一种题型" });

    const badDates = await teacher.post("/plans", { name: `bd-${STAMP}`, unitIds: [unitId], startDate: "2026-09-20", endDate: "2026-09-01" });
    expect(badDates.status).toBe(400);
    expect(badDates.body.error).toMatchObject({ code: "VALIDATION", message: "结束日期不能早于开始日期" });

    const badDayKey = await teacher.post("/plans", { name: `bk-${STAMP}`, unitIds: [unitId], startDate: "not-a-date" });
    expect(badDayKey.status).toBe(400);
    expect(badDayKey.body.error.details).toEqual({ startDate: "日期格式应为 YYYY-MM-DD" });

    const badKind = await teacher.post("/plans", { name: `x-${STAMP}`, unitIds: [unitId], kind: "nope" });
    expect(badKind.status).toBe(400);
    expect(badKind.body.error.code).toBe("VALIDATION");
  });

  it("目标解析：给班级 / 给本班学生 成功；给非本班班级 / 非本班学生 / 不存在对象 失败", async () => {
    const teacher = await login("teacher@vinx.test");
    const admin = await login("admin@vinx.test");
    const { unitId } = await makeUnit(teacher, `t${STAMP}`, 1);
    const cid = await demoClassId();

    const toClass = await teacher.post("/plans", { name: `to-class-${STAMP}`, unitIds: [unitId], targets: { classIds: [cid] } });
    expect(toClass.status).toBe(200);
    expect(toClass.body.data.targets).toEqual([{ type: "class", id: cid, name: expect.any(String) }]);

    const student = await login("student@vinx.test");
    const studentID = (await student.get("/auth/me")).body.data.id as string;
    const toStudent = await teacher.post("/plans", { name: `to-student-${STAMP}`, unitIds: [unitId], targets: { userIds: [studentID] } });
    expect(toStudent.status).toBe(200);
    expect(toStudent.body.data.targets).toEqual([{ type: "user", id: studentID, name: expect.any(String), email: "student@vinx.test" }]);

    // 非本班班级：随机 id，老师必然不管理它
    const fakeClassId = `not-managed-${STAMP}`;
    const badClass = await teacher.post("/plans", { name: `bad-class-${STAMP}`, unitIds: [unitId], targets: { classIds: [fakeClassId] } });
    expect(badClass.status).toBe(403);
    expect(badClass.body.error).toMatchObject({ code: "FORBIDDEN", message: "只能给自己的班级安排计划" });

    // 非本班学生：admin 不在 teacher 的任何班级里
    const meAdmin = await admin.get("/auth/me");
    const adminId = meAdmin.body.data.id as string;
    const badStudent = await teacher.post("/plans", { name: `bad-student-${STAMP}`, unitIds: [unitId], targets: { userIds: [adminId] } });
    expect(badStudent.status).toBe(403);
    expect(badStudent.body.error).toMatchObject({ code: "FORBIDDEN", message: "只能给本班学生安排计划" });

    // 管理员：不受「本班」限制，可以指定任意存在的班级 / 用户
    const adminToClass = await admin.post("/plans", { name: `admin-class-${STAMP}`, unitIds: [unitId], targets: { classIds: [cid] } });
    expect(adminToClass.status).toBe(200);

    // 不存在的安排对象：管理员不受「本班」限制，能直接命中「安排对象不存在」这条校验
    // （老师给不存在的用户安排会先被「只能给本班学生安排计划」拦下，见上）
    const missingUser = await admin.post("/plans", { name: `missing-${STAMP}`, unitIds: [unitId], targets: { userIds: ["no-such-user"] } });
    expect(missingUser.status).toBe(400);
    expect(missingUser.body.error).toMatchObject({ code: "VALIDATION", message: "安排对象不存在" });
  });

  it("列表：scope=mine/created/all，status 过滤；详情可见性；权限与数据范围", async () => {
    const teacher = await login("teacher@vinx.test");
    const student = await login("student@vinx.test");
    const admin = await login("admin@vinx.test");
    const outsiderEmail = `plan-outsider-${STAMP}@vinx.test`;
    const outsider = anon();
    const su = await outsider.post("/auth/signup", { email: outsiderEmail, password: "123456" });
    expect(su.status).toBe(201);

    const { unitId } = await makeUnit(teacher, `l${STAMP}`, 1);
    const cid = await demoClassId();
    const toClass = await teacher.post("/plans", { name: `list-class-${STAMP}`, unitIds: [unitId], targets: { classIds: [cid] }, status: "paused" });
    expect(toClass.status).toBe(200);
    const planId = toClass.body.data.id as string;

    // 详情：学生（班级成员）可见，targetsMe 为真
    const asStudent = await student.get(`/plans/${planId}`);
    expect(asStudent.status).toBe(200);
    expect(asStudent.body.data.targetsMe).toBe(true);
    expect(asStudent.body.data.canEdit).toBe(false);

    // 不在班上的学生：不可见
    const asOutsider = await outsider.get(`/plans/${planId}`);
    expect(asOutsider.status).toBe(404);
    expect(asOutsider.body.error).toMatchObject({ code: "NOT_FOUND", message: "计划不存在" });

    // 列表：scope=created 只看自己创建
    const created = await teacher.get("/plans?scope=created");
    expect(created.status).toBe(200);
    expect((created.body.data.items as Array<{ id: string }>).some((p) => p.id === planId)).toBe(true);
    const createdByOutsider = await outsider.get("/plans?scope=created");
    expect((createdByOutsider.body.data.items as Array<{ id: string }>).some((p) => p.id === planId)).toBe(false);

    // 列表：scope=mine 只看安排给我的（含班级）
    const mineStudent = await student.get("/plans?scope=mine");
    expect((mineStudent.body.data.items as Array<{ id: string }>).some((p) => p.id === planId)).toBe(true);

    // 列表：status 过滤
    const paused = await teacher.get("/plans?status=paused");
    expect((paused.body.data.items as Array<{ id: string }>).some((p) => p.id === planId)).toBe(true);
    const active = await teacher.get("/plans?status=active");
    expect((active.body.data.items as Array<{ id: string }>).some((p) => p.id === planId)).toBe(false);

    // 列表：classId 过滤
    const byClass = await admin.get(`/plans?classId=${cid}`);
    expect((byClass.body.data.items as Array<{ id: string }>).some((p) => p.id === planId)).toBe(true);

    // 管理员看得到全部；无关老师（这里用 outsider，无 plans.assign 无所谓，用 student 的 scope=all 验证只影响可见性不影响 cap）
    const allByAdmin = await admin.get("/plans");
    expect((allByAdmin.body.data.items as Array<{ id: string }>).some((p) => p.id === planId)).toBe(true);
  });

  it("修改：改名 / 改单元 / 改安排对象 / 日期校验；权限：仅创建者或管理员", async () => {
    const teacher = await login("teacher@vinx.test");
    const admin = await login("admin@vinx.test");
    const { unitId: u1 } = await makeUnit(teacher, `p1-${STAMP}`, 2);
    const { unitId: u2 } = await makeUnit(teacher, `p2-${STAMP}`, 3);

    const created = await teacher.post("/plans", { name: `patch-${STAMP}`, unitIds: [u1] });
    const planId = created.body.data.id as string;
    expect(created.body.data.wordCount).toBe(2);

    const renamed = await teacher.patch(`/plans/${planId}`, { name: `patched-${STAMP}` });
    expect(renamed.status).toBe(200);
    expect(renamed.body.data.name).toBe(`patched-${STAMP}`);

    const changedUnits = await teacher.patch(`/plans/${planId}`, { unitIds: [u2] });
    expect(changedUnits.status).toBe(200);
    expect(changedUnits.body.data.wordCount).toBe(3);
    expect(changedUnits.body.data.units.map((u: { id: string }) => u.id)).toEqual([u2]);

    // 未提供的字段保持不变
    expect(changedUnits.body.data.newPerDay).toBe(10);

    // 日期校验：结合已有值
    const setStart = await teacher.patch(`/plans/${planId}`, { startDate: "2026-09-20" });
    expect(setStart.status).toBe(200);
    const badEnd = await teacher.patch(`/plans/${planId}`, { endDate: "2026-09-01" });
    expect(badEnd.status).toBe(400);
    expect(badEnd.body.error).toMatchObject({ code: "VALIDATION", message: "结束日期不能早于开始日期" });
    // 清空为 null
    const clearStart = await teacher.patch(`/plans/${planId}`, { startDate: null });
    expect(clearStart.status).toBe(200);
    expect(clearStart.body.data.startDate).toBeNull();

    // 空名称校验
    const badName = await teacher.patch(`/plans/${planId}`, { name: "" });
    expect(badName.status).toBe(400);
    expect(badName.body.error.details).toEqual({ name: "请填写计划名称" });

    // 不存在的单元
    const badUnits = await teacher.patch(`/plans/${planId}`, { unitIds: ["not-a-real-unit"] });
    expect(badUnits.status).toBe(400);
    expect(badUnits.body.error).toMatchObject({ code: "VALIDATION", message: "包含不可用的单元" });

    // 权限：不是创建者也不是管理员 → 403（学生自建计划让别的老师改）
    const student = await login("student@vinx.test");
    const selfPlan = await student.post("/plans", { name: `stu-${STAMP}`, unitIds: [u1] });
    const forbiddenPatch = await teacher.patch(`/plans/${selfPlan.body.data.id}`, { name: "改了" });
    expect(forbiddenPatch.status).toBe(403);
    expect(forbiddenPatch.body.error).toMatchObject({ code: "FORBIDDEN", message: "只能修改自己创建的计划" });

    // 管理员可以改任何人的计划
    const adminPatch = await admin.patch(`/plans/${selfPlan.body.data.id}`, { status: "archived" });
    expect(adminPatch.status).toBe(200);
    expect(adminPatch.body.data.status).toBe("archived");

    // 不存在的计划
    const notFound = await teacher.patch(`/plans/does-not-exist`, { name: "x" });
    expect(notFound.status).toBe(404);
    expect(notFound.body.error).toMatchObject({ code: "NOT_FOUND", message: "计划不存在" });
  });

  it("删除：K22 没有进行中组时直接删除；权限：仅创建者或管理员", async () => {
    const teacher = await login("teacher@vinx.test");
    const admin = await login("admin@vinx.test");
    const student = await login("student@vinx.test");
    const { unitId } = await makeUnit(teacher, `d${STAMP}`, 1);

    const own = await teacher.post("/plans", { name: `del-${STAMP}`, unitIds: [unitId] });
    const ownId = own.body.data.id as string;

    // 学生不能删别人的计划
    const forbidden = await student.del(`/plans/${ownId}`);
    expect(forbidden.status).toBe(403);

    const del = await teacher.del(`/plans/${ownId}`);
    expect(del.status).toBe(200);
    expect(del.body).not.toHaveProperty("data");
    expectShape("plans-ok-empty", del.body);
    const gone = await teacher.get(`/plans/${ownId}`);
    expect(gone.status).toBe(404);

    // 管理员可以删任何人的计划
    const another = await teacher.post("/plans", { name: `del2-${STAMP}`, unitIds: [unitId] });
    const del2 = await admin.del(`/plans/${another.body.data.id}`);
    expect(del2.status).toBe(200);
  });

  it("进度：老师查看班级安排的进度；学生只看到自己；无关老师不可见", async () => {
    const teacher = await login("teacher@vinx.test");
    const student = await login("student@vinx.test");
    const student2 = await login("student2@vinx.test");
    const { unitId } = await makeUnit(teacher, `pr${STAMP}`, 5);
    const cid = await demoClassId();

    const plan = await teacher.post("/plans", { name: `progress-${STAMP}`, unitIds: [unitId], targets: { classIds: [cid] } });
    const planId = plan.body.data.id as string;

    const asTeacher = await teacher.get(`/plans/${planId}/progress`);
    expect(asTeacher.status).toBe(200);
    expect(asTeacher.body.data.totalWords).toBe(5);
    expect(asTeacher.body.data.day).toMatch(/^\d{4}-\d{2}-\d{2}$/);
    const ids = (asTeacher.body.data.items as Array<{ userId: string; today: unknown }>).map((i) => i.userId);
    const meStudent = await student.get("/auth/me");
    const meStudent2 = await student2.get("/auth/me");
    expect(ids).toContain(meStudent.body.data.id);
    expect(ids).toContain(meStudent2.body.data.id);
    const item = (asTeacher.body.data.items as Array<{ userId: string; today: { newLeft: number; doneToday: boolean } }>).find(
      (i) => i.userId === meStudent.body.data.id,
    )!;
    expect(item.today).not.toBeNull();
    expect(item.today.newLeft).toBeGreaterThan(0);
    expectShape("plans-progress", asTeacher.body);

    // 学生查看：只看到自己这一条
    const asStudent = await student.get(`/plans/${planId}/progress`);
    expect(asStudent.status).toBe(200);
    const studentIds = (asStudent.body.data.items as Array<{ userId: string }>).map((i) => i.userId);
    expect(studentIds).toEqual([meStudent.body.data.id]);

    // 不相关老师（自建班，本例用没有 students.view 覆盖到这个班的 outsider 学生代替验证 404 不可见，
    // 因为进度接口先做 visiblePlanWhere，不可见的计划整体 404）
    const outsiderEmail = `progress-outsider-${STAMP}@vinx.test`;
    const outsider = anon();
    await outsider.post("/auth/signup", { email: outsiderEmail, password: "123456" });
    const asOutsider = await outsider.get(`/plans/${planId}/progress`);
    expect(asOutsider.status).toBe(404);
  });

  it.skipIf(!ready("study"))("K22：删除计划前，先结算已作答的进行中组，保留学习记录", async () => {
    const teacher = await login("teacher@vinx.test");
    const student = await login("student@vinx.test");
    const { unitId } = await makeUnit(teacher, `k22-${STAMP}`, 3);
    const studentID = (await student.get("/auth/me")).body.data.id as string;
    const plan = await teacher.post("/plans", { name: `k22-${STAMP}`, unitIds: [unitId], targets: { userIds: [studentID] } });
    const planId = plan.body.data.id as string;

    const start = await student.post("/study/sessions", { kind: "learn", planId });
    expect(start.status).toBe(200);
    const sessionId = start.body.data.id as string;
    const detail = await student.get(`/study/sessions/${sessionId}`);
    const firstWordId = detail.body.data.items[0].wordId as string;
    expect(firstWordId).toBeTruthy();
    const answer = await student.post(`/study/sessions/${sessionId}/answers`, {
      wordId: firstWordId, mode: "recognition", phase: "practice", attempt: 1, answer: "随便答一个", durationMs: 1000,
    });
    expect(answer.status).toBe(200);

    const del = await teacher.del(`/plans/${planId}`);
    expect(del.status).toBe(200);

    const after = await student.get(`/study/sessions/${sessionId}`);
    expect(after.status).toBe(200);
    expect(after.body.data.status).toBe("completed");
  });
});
