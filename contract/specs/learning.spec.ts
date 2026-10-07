/**
 * 由旧 apps/api/tests/learning.flow.test.ts 改写为纯 HTTP 黑盒（今日 / 学习组 / 结算 / 记录）。
 *
 * 与旧测试的差异：
 * - 旧测试直接调用服务函数、注入「明天」的时间来测复习；黑盒无法改被测实例的时钟，跨学习日的复习、
 *   上海 00:00 前后的学习日边界与 K19（每个词每个学习日最多更新一次）由 Go 的 internal/service 固定时钟单测覆盖。
 * - 班级（/classes）与建号（/users）属于其他模块：这里用新注册的学生给自己建计划（范围取 seed 的系统词书），
 *   老师视角的记录查看用 seed 的 teacher@vinx.test（不是该学生的班主任 → 403）。
 * - 检测组进行中拿不到标准答案（K20）：用「单词历史」接口查到已学词的释义与拼写后作答。
 * - 出题、选词含随机：只断言不变量（选项数、含正确项、无重复、题型），不断言具体抽到哪个词。
 */
import { describe, it, expect, beforeAll } from "vitest";
import { anon, login, STAMP, type Client } from "../lib/client";
import { ready } from "../lib/areas";
import { expectShape } from "../lib/shape";
import { answerAll, newStudent, systemUnitId, type Item } from "../lib/study";

describe.runIf(ready("study", "plans"))("今日与学习组", () => {
  let student: Client;
  let studentId = "";
  let unitId = "";
  let planId = "";

  beforeAll(async () => {
    ({ client: student, id: studentId } = await newStudent("learn"));
    unitId = await systemUnitId(student);
  });

  it("新学生：今日为空；建计划后看到新词额度", async () => {
    const empty = await student.get("/today");
    expect(empty.status).toBe(200);
    expect(empty.body.data).toMatchObject({ plans: [], drillAvailable: 0, sheet: null, streak: 0, learnedWords: 0 });
    expect(empty.body.data.totals).toEqual({ newLeft: 0, reviewLeft: 0, newDoneToday: 0, reviewDoneToday: 0, pendingTests: 0 });
    expectShape("today-empty", empty.body);

    const plan = await student.post("/plans", { name: `学习 ${STAMP}`, newPerDay: 12, reviewPerDay: 30, modes: ["recognition", "spelling"], unitIds: [unitId] });
    expect(plan.status).toBe(200);
    planId = plan.body.data.id;
    const t = await student.get("/today");
    const card = t.body.data.plans.find((p: { planId: string }) => p.planId === planId);
    expect(card).toMatchObject({ newLeft: 12, newDoneToday: 0, reviewLeft: 0, source: "self", doneToday: false, testResult: null, activeSessions: [] });
    expect(t.body.data.totals.newLeft).toBe(12);
    expectShape("today", t.body);
  });

  it("开组校验：kind 枚举、缺计划、计划不存在、检测计划不能学新词", async () => {
    const bad = await student.post("/study/sessions", { kind: "x" });
    expect(bad.status).toBe(400);
    expect(bad.body.error).toMatchObject({ code: "VALIDATION", message: "参数校验失败" });
    expect(bad.body.error.details).toEqual({ kind: "Invalid enum value. Expected 'learn' | 'review' | 'test' | 'drill' | 'sheet', received 'x'" });
    expectShape("study-validation-error", bad.body);

    const missing = await student.post("/study/sessions", {});
    expect(missing.body.error.details).toEqual({ kind: "Required" });

    const noPlan = await student.post("/study/sessions", { kind: "learn" });
    expect(noPlan.status).toBe(400);
    expect(noPlan.body.error).toMatchObject({ code: "VALIDATION", message: "缺少计划" });
    expect(noPlan.body.error.details).toBeUndefined();

    const nullPlan = await student.post("/study/sessions", { kind: "review", planId: null });
    expect(nullPlan.body.error).toMatchObject({ code: "VALIDATION", message: "缺少计划" });

    const typed = await student.post("/study/sessions", { kind: "learn", planId: 5 });
    expect(typed.body.error.details).toEqual({ planId: "Expected string, received number" });

    const gone = await student.post("/study/sessions", { kind: "learn", planId: "nope" });
    expect(gone.status).toBe(404);
    expect(gone.body.error).toMatchObject({ code: "NOT_FOUND", message: "计划不存在、未生效或不属于你" });

    const test = await student.post("/study/sessions", { kind: "test", planId });
    expect(test.status).toBe(400);
    expect(test.body.error).toMatchObject({ code: "INVALID_ACTION", message: "该计划不是检测" });

    const review = await student.post("/study/sessions", { kind: "review", planId });
    expect(review.status).toBe(404);
    expect(review.body.error).toMatchObject({ code: "NO_DATA", message: "今天没有需要复习的词" });

    const drill = await student.post("/study/sessions", { kind: "drill" });
    expect(drill.status).toBe(404);
    expect(drill.body.error).toMatchObject({ code: "NO_DATA", message: "最近没有错词，继续保持！" });

    const sheet = await student.post("/study/sessions", { kind: "sheet" });
    expect(sheet.body.error).toMatchObject({ code: "VALIDATION", message: "缺少单词单" });
    const sheet404 = await student.post("/study/sessions", { kind: "sheet", sheetId: "nope" });
    expect(sheet404.status).toBe(404);
    expect(sheet404.body.error.message).toBe("单词单不存在");
  });

  it("学一组新词：续做同一组、答题幂等、结算后记忆入库", async () => {
    const start = await student.post("/study/sessions", { kind: "learn", planId });
    expect(start.status).toBe(200);
    expect(start.body.data.resumed).toBe(false);
    expect(Object.keys(start.body.data).sort()).toEqual(["id", "resumed"]);
    const sessionId = start.body.data.id;

    const again = await student.post("/study/sessions", { kind: "learn", planId });
    expect(again.body.data).toEqual({ id: sessionId, resumed: true });

    const detail = await student.get(`/study/sessions/${sessionId}`);
    expect(detail.body.data).toMatchObject({ id: sessionId, kind: "learn", status: "active", userId: studentId, planId, sheetId: null, modes: ["recognition", "spelling"], answers: [], progress: null, result: null, completedAt: null, isOwner: true });
    expect(detail.body.data.planName).toBe(`学习 ${STAMP}`);
    expectShape("study-session-learn", detail.body);

    const t = await student.get("/today");
    const card = t.body.data.plans.find((p: { planId: string }) => p.planId === planId);
    expect(card.activeSessions).toEqual([{ id: sessionId, kind: "learn", total: 10 }]);
    expect(card.doneToday).toBe(false);

    let firstWord = "";
    const items = await answerAll(student, sessionId, "practice", (w, m) => {
      if (!firstWord) firstWord = w;
      return w === firstWord && m === "spelling";
    });
    expect(items).toHaveLength(10);
    for (const i of items) {
      expect(i.options).toHaveLength(4);
      expect(i.options).toContain(i.definition);
      expect(new Set(i.options).size).toBe(4);
      expect(i.modes).toEqual(["recognition", "spelling"]);
      expect(i.cloze).toBeNull();
    }

    // 作答接口：正确答案反馈 + 结构
    const first = items[0];
    const dup = await student.post(`/study/sessions/${sessionId}/answers`, { wordId: firstWord, mode: "spelling", phase: "practice", attempt: 1, answer: first.answer });
    // 重试同一作答：按已存结果（答错）返回，不产生重复记录
    expect(dup.body.data).toEqual({ recorded: true, correct: false, expected: first.answer });
    expectShape("study-answer", dup.body);
    const cons = await student.post(`/study/sessions/${sessionId}/answers`, { wordId: firstWord, mode: "spelling", phase: "consolidate", attempt: 1, answer: first.answer, hintUsed: true, durationMs: 700_000 });
    expect(cons.body.data).toEqual({ recorded: true, correct: true, expected: first.answer });

    // 作答校验
    const notIn = await student.post(`/study/sessions/${sessionId}/answers`, { wordId: "nope", mode: "recognition", phase: "practice", attempt: 1, answer: "" });
    expect(notIn.status).toBe(400);
    expect(notIn.body.error).toMatchObject({ code: "VALIDATION", message: "该词不在本组中" });
    const cloze = await student.post(`/study/sessions/${sessionId}/answers`, { wordId: firstWord, mode: "cloze", phase: "practice", attempt: 1, answer: "" });
    expect(cloze.body.error).toMatchObject({ code: "VALIDATION", message: "本组不包含该题型" });
    const phase = await student.post(`/study/sessions/${sessionId}/answers`, { wordId: firstWord, mode: "recognition", phase: "test", attempt: 1, answer: "" });
    expect(phase.body.error).toMatchObject({ code: "VALIDATION", message: "作答阶段不匹配" });
    const body = await student.post(`/study/sessions/${sessionId}/answers`, { attempt: 0, answer: "x".repeat(301), durationMs: -1, hintUsed: "y", mode: "x" });
    expect(body.status).toBe(400);
    expect(body.body.error.details).toEqual({
      wordId: "Required",
      mode: "Invalid enum value. Expected 'recognition' | 'spelling' | 'cloze', received 'x'",
      phase: "Required",
      attempt: "Number must be greater than or equal to 1",
      answer: "String must contain at most 300 character(s)",
      hintUsed: "Expected boolean, received string",
      durationMs: "Number must be greater than or equal to 0",
    });
    const float = await student.post(`/study/sessions/${sessionId}/answers`, { wordId: "a", mode: "spelling", phase: "practice", attempt: 1.5, answer: "" });
    expect(float.body.error.details).toEqual({ attempt: "Expected integer, received float" });
    const big = await student.post(`/study/sessions/${sessionId}/answers`, { wordId: "", mode: "spelling", phase: "practice", attempt: 21, answer: "" });
    expect(big.body.error.details).toEqual({ wordId: "String must contain at least 1 character(s)", attempt: "Number must be less than or equal to 20" });

    // 进度：保存并原样读回；只接受对象
    const prog = await student.patch(`/study/sessions/${sessionId}/progress`, { progress: { step: 3, queue: ["a"] } });
    expect(prog.status).toBe(200);
    expect(prog.body.data).toBeUndefined();
    expectShape("ok-empty", prog.body);
    expect((await student.get(`/study/sessions/${sessionId}`)).body.data.progress).toEqual({ step: 3, queue: ["a"] });
    for (const [progress, msg] of [[undefined, "Required"], [null, "Expected object, received null"], [[1], "Expected object, received array"], ["s", "Expected object, received string"]] as const) {
      const r = await student.patch(`/study/sessions/${sessionId}/progress`, progress === undefined ? {} : { progress });
      expect(r.status).toBe(400);
      expect(r.body.error.details).toEqual({ progress: msg });
    }

    // 已作答不能放弃
    const discard = await student.del(`/study/sessions/${sessionId}`);
    expect(discard.status).toBe(400);
    expect(discard.body.error).toMatchObject({ code: "INVALID_ACTION", message: "已经开始作答，请点“结束本组”保存进度" });

    const done = await student.post(`/study/sessions/${sessionId}/complete`);
    expect(done.status).toBe(200);
    const result = done.body.data.result;
    expect(done.body.data.session).toEqual({ id: sessionId, status: "completed" });
    expect(result).toMatchObject({ words: 10, settled: 10, unsettled: 0, newLearned: 10, totalFirst: 20, correctFirst: 19, accuracy: 0.95 });
    // 时长：所有作答（含巩固）单题封顶 60 秒
    expect(result.durationMs).toBe(20 * 3000 + 60_000); // 巩固那次存 600000（上限 10 分钟），结算按 60 秒封顶
    expect(result.ratings).toEqual({ hard: 9, again: 1 });
    expect(result.wrongWordIds).toEqual([firstWord]);
    expectShape("study-complete", done.body);

    // 幂等完成
    expect((await student.post(`/study/sessions/${sessionId}/complete`)).body.data).toEqual(done.body.data);
    // 结束后不能再答、不能放弃；进度静默忽略
    const late = await student.post(`/study/sessions/${sessionId}/answers`, { wordId: firstWord, mode: "recognition", phase: "consolidate", attempt: 2, answer: "" });
    expect(late.status).toBe(400);
    expect(late.body.error).toMatchObject({ code: "INVALID_STATUS", message: "这一组已经结束" });
    expect((await student.del(`/study/sessions/${sessionId}`)).body.error).toMatchObject({ code: "INVALID_STATUS", message: "这一组已经结束" });
    expect((await student.patch(`/study/sessions/${sessionId}/progress`, { progress: { x: 1 } })).status).toBe(200);

    const completed = await student.get(`/study/sessions/${sessionId}`);
    expect(completed.body.data).toMatchObject({ status: "completed", result, progress: { step: 3, queue: ["a"] } });
    expect(completed.body.data.answers).toHaveLength(21);
    expect(completed.body.data.answers[0]).toEqual(expect.objectContaining({ phase: "practice", attempt: 1, hintUsed: false, dontKnow: false }));
    expectShape("study-session-completed", completed.body);

    const t2 = await student.get("/today");
    const card2 = t2.body.data.plans.find((p: { planId: string }) => p.planId === planId);
    expect(card2).toMatchObject({ newDoneToday: 10, newLeft: 2, learnedWords: 10, activeSessions: [] });
    expect(t2.body.data).toMatchObject({ streak: 1, drillAvailable: 1, learnedWords: 10 });
    expect(t2.body.data.stats).toMatchObject({ newWords: 10, reviewedWords: 0, answers: 21, newLeft: 2, reviewLeft: 0, pendingTests: 0 });
  });

  it("额度内再学一组只取剩余 2 词；额度用完后开组返回 NO_DATA；未作答的组可以放弃", async () => {
    const s = await student.post("/study/sessions", { kind: "learn", planId });
    const detail = await student.get(`/study/sessions/${s.body.data.id}`);
    expect(detail.body.data.items).toHaveLength(2);
    // 未作答：可以放弃，再开是新组
    expect((await student.del(`/study/sessions/${s.body.data.id}`)).status).toBe(200);
    expect((await student.get(`/study/sessions/${s.body.data.id}`)).status).toBe(404);
    const s2 = await student.post("/study/sessions", { kind: "learn", planId });
    expect(s2.body.data.resumed).toBe(false);
    await answerAll(student, s2.body.data.id, "practice");
    const done = await student.post(`/study/sessions/${s2.body.data.id}/complete`);
    expect(done.body.data.result.words).toBe(2);
    const none = await student.post("/study/sessions", { kind: "learn", planId });
    expect(none.status).toBe(404);
    expect(none.body.error).toMatchObject({ code: "NO_DATA", message: "今天的新词已经学完了" });
    const t = await student.get("/today");
    expect(t.body.data.plans.find((p: { planId: string }) => p.planId === planId)).toMatchObject({ newLeft: 0, doneToday: true });
  });

  it("检测计划：进行中不下发答案与对错（进度接口也不泄露）；只更新已学词；首次交卷为正式成绩", async () => {
    const plan = await student.post("/plans", { name: `检测 ${STAMP}`, kind: "test", modes: ["recognition", "spelling"], testSize: 5, testScope: "learned", unitIds: [unitId] });
    expect(plan.status).toBe(200);
    const testPlanId = plan.body.data.id;
    const t0 = await student.get("/today");
    expect(t0.body.data.totals.pendingTests).toBe(1);

    const learnInTest = await student.post("/study/sessions", { kind: "learn", planId: testPlanId });
    expect(learnInTest.body.error).toMatchObject({ code: "INVALID_ACTION", message: "检测计划不能学新词" });

    const s = await student.post("/study/sessions", { kind: "test", planId: testPlanId });
    const sessionId = s.body.data.id;
    const detail = await student.get(`/study/sessions/${sessionId}`);
    const items = detail.body.data.items as (Item & { letters: number; wordsInAnswer: number; example: null; exampleCn: null; isNew: boolean; phonetic: string | null })[];
    expect(items).toHaveLength(5);
    for (const i of items) {
      expect(i.answer).toBe("");
      expect(i.example).toBeNull();
      expect(i.exampleCn).toBeNull();
      expect(i.isNew).toBe(false);
      expect(i.spelling).not.toBe(""); // 有认义题：给英文
      expect(i.definition).not.toBe(""); // 有拼写题：给释义
      expect(i.letters).toBeGreaterThan(0);
      expect(i.wordsInAnswer).toBeGreaterThan(0);
    }
    expectShape("study-session-test-active", detail.body);

    // 查已学词的拼写与释义（单词历史）后作答：前 3 个答对，后 2 个认义答错
    const know = new Map<string, { spelling: string; definition: string }>();
    for (const i of items) {
      const h = await student.get(`/records/words/${i.wordId}`);
      know.set(i.wordId, h.body.data.word);
    }
    for (const [idx, i] of items.entries()) {
      const w = know.get(i.wordId)!;
      const rec = await student.post(`/study/sessions/${sessionId}/answers`, { wordId: i.wordId, mode: "recognition", phase: "test", attempt: 1, answer: idx < 3 ? w.definition : "x" });
      expect(rec.body.data).toEqual({ recorded: true });
      const sp = await student.post(`/study/sessions/${sessionId}/answers`, { wordId: i.wordId, mode: "spelling", phase: "test", attempt: 1, answer: w.spelling });
      expect(sp.body.data).toEqual({ recorded: true });
    }
    const wrongPhase = await student.post(`/study/sessions/${sessionId}/answers`, { wordId: items[0].wordId, mode: "recognition", phase: "practice", attempt: 1, answer: "" });
    expect(wrongPhase.body.error.message).toBe("作答阶段不匹配");

    const prog = await student.patch(`/study/sessions/${sessionId}/progress`, { progress: { i: 5 } });
    expect(prog.body).not.toHaveProperty("data");
    const active = await student.get(`/study/sessions/${sessionId}`);
    expect(active.body.data.answers).toHaveLength(10);
    expect(active.body.data.answers.every((a: { correct: unknown }) => a.correct === null)).toBe(true);
    expect(active.body.data.items.every((i: Item) => i.answer === "")).toBe(true);
    const rec = await student.get(`/records/sessions/${sessionId}`);
    expect(rec.status).toBe(400);
    expect(rec.body.error).toMatchObject({ code: "INVALID_STATUS", message: "检测进行中，交卷后才能查看详情" });
    // 进行中检测的作答不进入统计
    const sumActive = await student.get("/records/summary");
    expect(sumActive.body.data.today.answers).toBe(21 + 4 + 0 + 0 + 0); // 前两组的作答（21 + 第二组 2 词 × 2）

    const done = await student.post(`/study/sessions/${sessionId}/complete`);
    expect(done.body.data.result).toMatchObject({ correctFirst: 8, totalFirst: 10, settled: 5, newLearned: 0 });
    // 今天都学过（今天已更新过记忆）→ K19：检测不再推动记忆
    expect(done.body.data.result.ratings).toEqual({});
    const after = await student.get(`/study/sessions/${sessionId}`);
    expect(after.body.data.answers.filter((a: { correct: boolean }) => a.correct === false)).toHaveLength(2);
    expect(after.body.data.items.every((i: Item) => i.answer !== "")).toBe(true);

    const t = await student.get("/today");
    const card = t.body.data.plans.find((p: { planId: string }) => p.planId === testPlanId);
    expect(card.testResult).toMatchObject({ sessionId, correct: 8, total: 10 });
    expect(card.doneToday).toBe(true);
    expect(t.body.data.totals.pendingTests).toBe(0);

    // 重考：成绩仍是首次交卷
    const retake = await student.post("/study/sessions", { kind: "test", planId: testPlanId });
    expect(retake.body.data.resumed).toBe(false);
    await student.post(`/study/sessions/${retake.body.data.id}/complete`);
    const t2 = await student.get("/today");
    expect(t2.body.data.plans.find((p: { planId: string }) => p.planId === testPlanId).testResult.sessionId).toBe(sessionId);
  });

  it("并发开组：同一学生同一计划同一类型只产生一个进行中的组（K12）", async () => {
    const { client } = await newStudent("conc");
    const plan = await client.post("/plans", { name: `并发 ${STAMP}`, newPerDay: 5, unitIds: [unitId] });
    const results = await Promise.all(Array.from({ length: 6 }, () => client.post("/study/sessions", { kind: "learn", planId: plan.body.data.id })));
    expect(results.every((r) => r.status === 200)).toBe(true);
    const ids = new Set(results.map((r) => r.body.data.id));
    expect(ids.size).toBe(1);
    expect(results.filter((r) => r.body.data.resumed === false)).toHaveLength(1);
    const t = await client.get("/today");
    expect(t.body.data.plans[0].activeSessions).toHaveLength(1);
    const list = await client.get("/records/sessions");
    expect(list.body.data.total).toBe(1);
  });

  it("K22：删除计划前结算已作答的进行中组，记忆推进、记录保留", async () => {
    const { client } = await newStudent("k22");
    const plan = await client.post("/plans", { name: `删除 ${STAMP}`, newPerDay: 3, modes: ["recognition", "spelling"], unitIds: [unitId] });
    const pid = plan.body.data.id;
    const s = await client.post("/study/sessions", { kind: "learn", planId: pid });
    const detail = await client.get(`/study/sessions/${s.body.data.id}`);
    const w = detail.body.data.items[0] as Item;
    await client.post(`/study/sessions/${s.body.data.id}/answers`, { wordId: w.wordId, mode: "recognition", phase: "practice", attempt: 1, answer: w.definition, durationMs: 5000 });
    await client.post(`/study/sessions/${s.body.data.id}/answers`, { wordId: w.wordId, mode: "spelling", phase: "practice", attempt: 1, answer: w.answer, durationMs: 90_000 });
    expect((await client.del(`/plans/${pid}`)).status).toBe(200);

    const after = await client.get(`/study/sessions/${s.body.data.id}`);
    expect(after.status).toBe(200);
    expect(after.body.data).toMatchObject({ status: "completed", planId: null });
    expect(after.body.data.result).toMatchObject({ words: 3, settled: 1, unsettled: 2, newLearned: 1, ratings: { hard: 1 }, durationMs: 65_000 });
    const words = await client.get("/records/words");
    expect(words.body.data.total).toBe(1);
    expect(words.body.data.items[0]).toMatchObject({ wordId: w.wordId, reps: 1, lapses: 0, level: "learning" });
    const hist = await client.get(`/records/words/${w.wordId}`);
    expect(hist.body.data.memory).toMatchObject({ reps: 1, state: 2, introducedPlanId: pid });
    expect(hist.body.data.reviewLogs).toHaveLength(1);
    expect(hist.body.data.reviewLogs[0]).toMatchObject({ rating: 2, stateBefore: 0, sessionId: s.body.data.id });
    const t = await client.get("/today");
    expect(t.body.data).toMatchObject({ plans: [], streak: 1, learnedWords: 1 });
  });

  // spec 0009 起错词强化也更新记忆；这里与新学同一学习日，K19 挡住同日再次更新
  it("错词强化：只练近期错词，同一学习日不再更新记忆（K19）", async () => {
    const before = await student.get("/records/words?limit=100");
    const s = await student.post("/study/sessions", { kind: "drill", planId: "ignored" });
    expect(s.status).toBe(200);
    const d = await student.get(`/study/sessions/${s.body.data.id}`);
    expect(d.body.data).toMatchObject({ kind: "drill", planId: null, planName: "错词强化", modes: ["recognition", "spelling"] });
    expect(d.body.data.items.length).toBeGreaterThanOrEqual(1);
    await answerAll(student, s.body.data.id, "practice");
    const done = await student.post(`/study/sessions/${s.body.data.id}/complete`);
    expect(done.body.data.result.ratings).toEqual({});
    expect(done.body.data.result.unsettled).toBe(0);
    const after = await student.get("/records/words?limit=100");
    expect(after.body.data.items.map((m: { due: string }) => m.due)).toEqual(before.body.data.items.map((m: { due: string }) => m.due));
  });

  it("记录：概况、每日、学习组列表与详情、单词列表与历史（含筛选、分页、校验）", async () => {
    const summary = await student.get("/records/summary");
    expect(summary.status).toBe(200);
    expect(summary.body.data).toMatchObject({ streak: 1, activeDays30: 1, learnedWords: 12, mastery: { missed: 1, learning: 11, consolidating: 0, mastered: 0 }, dueToday: 0 });
    expect(summary.body.data.lastActiveDay).toBe(summary.body.data.day);
    expect(summary.body.data.accuracy30d.total).toBeGreaterThan(0);
    expectShape("records-summary", summary.body);

    const daily = await student.get("/records/daily?days=7");
    expect(daily.body.data).toHaveLength(7);
    expect(daily.body.data.at(-1)).toMatchObject({ newWords: 12, reviewedWords: 0 });
    expect(daily.body.data[0]).toEqual({ day: daily.body.data[0].day, newWords: 0, reviewedWords: 0, answers: 0, correct: 0, firstAttempts: 0, minutes: 0 });
    expectShape("records-daily", daily.body);
    expect((await student.get("/records/daily")).body.data).toHaveLength(30);
    const badDays = await student.get("/records/daily?days=0");
    expect(badDays.body.error.details).toEqual({ days: "Number must be greater than or equal to 1" });
    expect((await student.get("/records/daily?days=abc")).body.error.details).toEqual({ days: "Expected number, received nan" });

    const sessions = await student.get("/records/sessions");
    expect(sessions.body.data).toMatchObject({ page: 1, limit: 20 });
    expect(sessions.body.data.total).toBe(5); // 学 10、学 2（放弃的那组已删除）、检测、重考、错词强化
    const kinds = sessions.body.data.items.map((s: { kind: string }) => s.kind);
    expect(kinds[0]).toBe("drill"); // 按开始时间倒序
    expectShape("records-sessions", sessions.body);
    const tests = await student.get("/records/sessions?kind=test&limit=1&page=2");
    expect(tests.body.data).toMatchObject({ total: 2, page: 2, limit: 1 });
    expect(tests.body.data.items).toHaveLength(1);
    expect((await student.get("/records/sessions?kind=bogus")).body.error.details).toEqual({ kind: "Invalid enum value. Expected 'learn' | 'review' | 'test' | 'drill' | 'sheet', received 'bogus'" });
    expect((await student.get("/records/sessions?limit=101")).body.error.details).toEqual({ limit: "Number must be less than or equal to 100" });

    const learnRow = sessions.body.data.items.find((s: { kind: string; words: number }) => s.kind === "learn" && s.words === 10);
    const sd = await student.get(`/records/sessions/${learnRow.id}`);
    expect(sd.status).toBe(200);
    expect(sd.body.data.words).toHaveLength(10);
    expect(sd.body.data.user.id).toBe(studentId);
    const wrongWord = sd.body.data.result.wrongWordIds[0];
    const ww = sd.body.data.words.find((w: { wordId: string }) => w.wordId === wrongWord);
    expect(ww.review).toMatchObject({ wordId: wrongWord, rating: 1 });
    expect(ww.answers).toHaveLength(3);
    expectShape("records-session-detail", sd.body);
    expect((await student.get("/records/sessions/nope")).status).toBe(404);

    const words = await student.get("/records/words?filter=all&limit=5");
    expect(words.body.data).toMatchObject({ total: 12, page: 1, limit: 5 });
    expect(words.body.data.items).toHaveLength(5);
    expectShape("records-words", words.body);
    expect((await student.get("/records/words?filter=due")).body.data.total).toBe(0);
    // spec 0009：新学时答错的那个词「没记住」，其余「刚记住」（learning）
    expect((await student.get("/records/words?filter=learning")).body.data.total).toBe(11);
    expect((await student.get("/records/words?filter=missed")).body.data.total).toBe(1);
    expect((await student.get("/records/words?filter=mastered")).body.data.total).toBe(0);
    expect((await student.get("/records/words?filter=bad")).body.error.details).toEqual({ filter: "Invalid enum value. Expected 'all' | 'due' | 'difficult' | 'mastered' | 'consolidating' | 'learning' | 'missed', received 'bad'" });
    const first = words.body.data.items[0];
    const byQ = await student.get(`/records/words?q=${encodeURIComponent(` ${first.spelling.toUpperCase()} `)}`);
    expect(byQ.body.data.items.map((w: { wordId: string }) => w.wordId)).toContain(first.wordId);

    const history = await student.get(`/records/words/${first.wordId}`);
    expect(history.body.data.word.id).toBe(first.wordId);
    expect(history.body.data.reviewLogs.length).toBeGreaterThan(0);
    expect(history.body.data.answers.length).toBeGreaterThan(0);
    expectShape("records-word-history", history.body);
    const unknown = await student.get("/records/words/nope");
    expect(unknown.status).toBe(404);
    expect(unknown.body.error.message).toBe("单词不存在");
  });

  it("权限：他人的学习组与记录", async () => {
    const other = (await newStudent("other")).client;
    const teacher = await login("teacher@vinx.test");
    const r1 = await other.get(`/records/summary?userId=${studentId}`);
    expect(r1.status).toBe(403);
    const r2 = await teacher.get(`/records/summary?userId=${studentId}`);
    expect(r2.status).toBe(403);
    expect(r2.body.error).toMatchObject({ code: "FORBIDDEN", message: "只能查看本班学生" });
    const list = await student.get("/records/sessions");
    const sid = list.body.data.items[0].id;
    expect((await other.get(`/study/sessions/${sid}`)).status).toBe(403);
    expect((await other.get(`/records/sessions/${sid}`)).status).toBe(403);
    const ans = await other.post(`/study/sessions/${sid}/answers`, { wordId: "a", mode: "spelling", phase: "practice", attempt: 1, answer: "" });
    expect(ans.status).toBe(404);
    expect(ans.body.error.message).toBe("学习组不存在");
    expect((await other.post(`/study/sessions/${sid}/complete`)).status).toBe(404);
    expect((await other.patch(`/study/sessions/${sid}/progress`, { progress: {} })).status).toBe(404);
    expect((await other.del(`/study/sessions/${sid}`)).status).toBe(404);
    const admin = await login("admin@vinx.test");
    const adminView = await admin.get(`/study/sessions/${sid}`);
    expect(adminView.status).toBe(200);
    expect(adminView.body.data.isOwner).toBe(false);
    expect((await admin.get(`/records/summary?userId=${studentId}`)).body.data.learnedWords).toBe(12);
    expect((await anon().get("/today")).status).toBe(401);
  });
});
