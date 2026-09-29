/**
 * 由旧 apps/api/tests/sheets.flow.test.ts + tests/lib/{unfamiliar,sheet-batch}.test.ts 改写为纯 HTTP 黑盒。
 * 决策见 docs/decisions.md K29 K30 K33 K34 K35。
 *
 * 与旧测试的差异（黑盒不能直接建 MemoryState / 改时钟）：
 * - 已学词的记忆状态一律走真实的 "learn" 学习组产生（自建词书 + 计划，答对建卡），不直接插库；
 *   因此新建的记忆都是「学习中」层级（stability < 7），旧测试里「已掌握」「巩固中」分层用旧仓库的
 *   internal/core/unfamiliar_test.go（表驱动单测，已覆盖打分公式的全部分支）代替，这里只断言不变量
 *   （不熟的词入选、已在待测单子上的词被排除、include 置顶、按分数排序的相对顺序）。
 * - 同一学习日内，单词单测试首次交卷的词若已在当天的 "learn" 组更新过记忆，K19 会挡住再次更新
 *   （与 FSRS 单测里显式相隔一天的场景不同，这里断言 ratings 为空、settled/unsettled 仍按已交题目计）。
 * - 进行中检测拿不到标准答案：用 /records/words/:wordId（对任意词 id 都能查，不要求已学）取真实拼写与释义。
 * - 班主任/别班老师/外人权限用专门的班级 + 邀请码学生（同 learning.spec 的做法）。
 */
import { describe, it, expect, beforeAll } from "vitest";
import { anon, login, STAMP, type Client } from "../lib/client";
import { ready } from "../lib/areas";
import { expectShape } from "../lib/shape";

interface WordBrief {
  id: string;
  spelling: string;
  definition: string;
}

/** 新建一本词书的一个单元，放 count 个词（拼写全局唯一，用 tag 加序号）；返回按 sortOrder（即插入顺序）排好的词。 */
async function makeUnit(teacher: Client, bookId: string, unitName: string, count: number, tag: string): Promise<{ unitId: string; words: WordBrief[] }> {
  const unit = await teacher.post(`/books/${bookId}/units`, { name: unitName });
  expect(unit.status).toBe(200);
  const unitId = unit.body.data.id as string;
  for (let i = 0; i < count; i++) {
    const r = await teacher.post(`/units/${unitId}/words`, { spelling: `${tag}${i}`, definition: `义${tag}${i}` });
    expect(r.status).toBe(200);
  }
  const list = await teacher.get(`/units/${unitId}/words?limit=${count}`);
  expect(list.body.data.items).toHaveLength(count);
  return { unitId, words: list.body.data.items.map((w: { id: string; spelling: string; definition: string }) => ({ id: w.id, spelling: w.spelling, definition: w.definition })) };
}

/** 答完一组（practice 阶段）：wrongWordIds 里的词第一次认义故意答错（造 wrong14），其余全对。 */
async function learnAll(student: Client, sessionId: string, wrongOnFirstAttempt: ReadonlySet<string> = new Set()) {
  const detail = await student.get(`/study/sessions/${sessionId}`);
  const items = detail.body.data.items as { wordId: string; definition: string; answer: string; modes?: string[] }[];
  for (const item of items) {
    for (const mode of item.modes ?? (detail.body.data.modes as string[])) {
      const bad = mode === "recognition" && wrongOnFirstAttempt.has(item.wordId);
      const answer = mode === "recognition" ? (bad ? "__wrong__" : item.definition) : item.answer;
      const r = await student.post(`/study/sessions/${sessionId}/answers`, { wordId: item.wordId, mode, phase: "practice", attempt: 1, answer });
      expect(r.status).toBe(200);
    }
  }
  return items.map((i) => i.wordId);
}

/** 单词单测试作答（test 阶段）：拿不到标准答案，用 /records/words/:wordId 查真实拼写/释义。wrong 为真时该词认义故意答错。 */
async function answerSheetTest(student: Client, sessionId: string, wrong: (wordId: string) => boolean = () => false) {
  const detail = await student.get(`/study/sessions/${sessionId}`);
  const items = detail.body.data.items as { wordId: string; modes?: string[] }[];
  const modes = detail.body.data.modes as string[];
  let correct = 0;
  let total = 0;
  for (const item of items) {
    const w = (await student.get(`/records/words/${item.wordId}`)).body.data.word as { spelling: string; definition: string };
    for (const mode of item.modes ?? modes) {
      total++;
      const bad = mode === "recognition" && wrong(item.wordId);
      const answer = mode === "recognition" ? (bad ? "__wrong__" : w.definition) : w.spelling;
      const r = await student.post(`/study/sessions/${sessionId}/answers`, { wordId: item.wordId, mode, phase: "test", attempt: 1, answer });
      expect(r.status).toBe(200);
      expect(r.body.data).toEqual({ recorded: true });
      if (!bad) correct++;
    }
  }
  return { items: items.map((i) => i.wordId), correct, total };
}

describe.runIf(ready("study", "plans", "books", "classes", "users", "sheets"))("单词单：校验（无需数据）", () => {
  let student: Client;

  beforeAll(async () => {
    const c = anon();
    const r = await c.post("/auth/signup", { email: `sheet-val-${STAMP}@vinx.test`, password: "dev123456", name: "校验学生" });
    expect(r.status).toBe(201);
    student = c;
  });

  it("预览：count 越界、source 判别式校验、include/wordIds 类型与长度", async () => {
    expect((await student.post("/sheets/preview", { count: 5 })).body.error.details).toEqual({ count: "Number must be greater than or equal to 10" });
    expect((await student.post("/sheets/preview", { count: 211 })).body.error.details).toEqual({ count: "Number must be less than or equal to 210" });
    const def = await student.post("/sheets/preview", {});
    expect(def.status).toBe(200);
    expect(def.body.data).toEqual({ items: [] });
    expectShape("sheets-preview-empty", def.body);

    const badKind = await student.post("/sheets/preview", { count: 10, source: { kind: "bogus" } });
    expect(badKind.body.error.details).toEqual({ "source.kind": "Invalid discriminator value. Expected 'unfamiliar' | 'session' | 'unit' | 'book'" });
    expect((await student.post("/sheets/preview", { count: 10, source: {} })).body.error.details).toEqual({ "source.kind": "Invalid discriminator value. Expected 'unfamiliar' | 'session' | 'unit' | 'book'" });
    expect((await student.post("/sheets/preview", { count: 10, source: null })).body.error.details).toEqual({ source: "Expected object, received null" });
    expect((await student.post("/sheets/preview", { count: 10, source: "x" })).body.error.details).toEqual({ source: "Expected object, received string" });
    expect((await student.post("/sheets/preview", { count: 10, source: { kind: "session" } })).body.error.details).toEqual({ "source.sessionId": "Required" });
    expect((await student.post("/sheets/preview", { count: 10, source: { kind: "unit" } })).body.error.details).toEqual({ "source.unitId": "Required" });
    expect((await student.post("/sheets/preview", { count: 10, source: { kind: "book" } })).body.error.details).toEqual({ "source.bookId": "Required" });
    // 未识别的 kind 专属字段被忽略（zod 对象 schema 默认丢弃多余键）
    expect((await student.post("/sheets/preview", { count: 10, source: { kind: "unit", sessionId: "x" } })).body.error.details).toEqual({ "source.unitId": "Required" });

    expect((await student.post("/sheets/preview", { count: 10, include: "x" })).body.error.details).toEqual({ include: "Expected array, received string" });
    expect((await student.post("/sheets/preview", { count: 10, include: [1, 2] })).body.error.details).toEqual({ "include.0": "Expected string, received number", "include.1": "Expected string, received number" });
    const bigInclude = await student.post("/sheets/preview", { count: 10, include: Array.from({ length: 211 }, (_, i) => `i${i}`) });
    expect(bigInclude.body.error.details).toEqual({ include: "Array must contain at most 210 element(s)" });

    expect((await student.post("/sheets/preview", { count: 10, source: { kind: "unit", unitId: "nope" } })).status).toBe(404);
    expect((await student.post("/sheets/preview", { count: 10, source: { kind: "unit", unitId: "nope" } })).body.error.message).toBe("单元不存在");
    expect((await student.post("/sheets/preview", { count: 10, source: { kind: "book", bookId: "nope" } })).body.error.message).toBe("词书不存在");
    expect((await student.post("/sheets/preview", { count: 10, source: { kind: "session", sessionId: "nope" } })).body.error).toMatchObject({ code: "NOT_FOUND", message: "这次学习记录不存在" });
  });

  it("生成：wordIds/modes 必填与长度、copies/perSheet 范围、词不存在", async () => {
    expect((await student.post("/sheets", {})).body.error.details).toEqual({ wordIds: "Required" });
    expect((await student.post("/sheets", { wordIds: [] })).body.error.details).toEqual({ wordIds: "至少选一个单词" });
    expect((await student.post("/sheets", { wordIds: Array.from({ length: 211 }, (_, i) => `w${i}`) })).body.error.details).toEqual({ wordIds: "Array must contain at most 210 element(s)" });
    expect((await student.post("/sheets", { wordIds: [""] })).body.error.details).toEqual({ "wordIds.0": "String must contain at least 1 character(s)" });
    expect((await student.post("/sheets", { wordIds: ["a"], modes: [] })).body.error.details).toEqual({ modes: "至少选择一种题型" });
    expect((await student.post("/sheets", { wordIds: ["a"], modes: ["x"] })).body.error.details).toEqual({ "modes.0": "Invalid enum value. Expected 'recognition' | 'spelling' | 'cloze', received 'x'" });
    expect((await student.post("/sheets", { wordIds: ["a"], copies: 0 })).body.error.details).toEqual({ copies: "Number must be greater than or equal to 1" });
    expect((await student.post("/sheets", { wordIds: ["a"], copies: 8 })).body.error.details).toEqual({ copies: "Number must be less than or equal to 7" });
    expect((await student.post("/sheets", { wordIds: ["a"], perSheet: 0 })).body.error.details).toEqual({ perSheet: "Number must be greater than or equal to 1" });
    expect((await student.post("/sheets", { wordIds: ["a"], perSheet: 31 })).body.error.details).toEqual({ perSheet: "Number must be less than or equal to 30" });
    const notExist = await student.post("/sheets", { wordIds: ["nope-word"] });
    expect(notExist.status).toBe(400);
    expect(notExist.body.error).toMatchObject({ code: "VALIDATION", message: "有单词不存在" });
    expect((await student.get("/sheets/nope")).body.error).toMatchObject({ code: "NOT_FOUND", message: "单词单不存在" });
    expect((await student.del("/sheets/nope")).body.error).toMatchObject({ code: "NOT_FOUND", message: "单词单不存在" });
  });
});

describe.runIf(ready("study", "plans", "books", "classes", "users", "sheets"))("单词单：预览、生成、测试流、权限", () => {
  const emails = { teacher: `sht-${STAMP}@vinx.test`, other: `sho-${STAMP}@vinx.test`, student: `shs-${STAMP}@vinx.test`, outsider: `shx-${STAMP}@vinx.test` };
  let teacher: Client, otherTeacher: Client, student: Client, outsider: Client;
  let studentId = "";
  let bookId = "";
  let unitId = "";
  let words: WordBrief[] = []; // 22 个词：0-19 会被学掉，20-21 留作「没学过」
  let learnedIds: string[] = [];
  let unlearnedId = "";

  beforeAll(async () => {
    const admin = await login("admin@vinx.test");
    for (const [k, email] of [["teacher", emails.teacher], ["other", emails.other]] as const) {
      expect((await admin.post("/users", { email, name: k, password: "dev123456", role: "teacher" })).status).toBe(200);
    }
    teacher = await login(emails.teacher);
    otherTeacher = await login(emails.other);
    const cls = await teacher.post("/classes", { name: `单词单班 ${STAMP}` });
    expect(cls.status).toBe(200);
    const su = anon();
    const signup = await su.post("/auth/signup", { email: emails.student, password: "dev123456", name: "单词单学生", inviteCode: cls.body.data.inviteCode });
    expect(signup.status).toBe(201);
    studentId = signup.body.data.user.id;
    student = su;
    const ou = anon();
    expect((await ou.post("/auth/signup", { email: emails.outsider, password: "dev123456", name: "外人" })).status).toBe(201);
    outsider = ou;

    const book = await teacher.post("/books", { name: `单词单书 ${STAMP}` });
    bookId = book.body.data.id;
    const u = await makeUnit(teacher, bookId, "U1", 22, `sh${STAMP.slice(-6)}`);
    unitId = u.unitId;
    words = u.words;
    unlearnedId = words[20].id;

    // 建计划（学生自己安排给自己，走 learn 学两组共 20 词）；第 1 组里第 1 个词首次认义故意答错（造 wrong14）
    const plan = await teacher.post("/plans", { name: `单词单计划 ${STAMP}`, newPerDay: 20, reviewPerDay: 0, modes: ["recognition", "spelling"], unitIds: [unitId], targets: { classIds: [], userIds: [studentId] } });
    expect(plan.status).toBe(200);
    const planId = plan.body.data.id;
    for (let round = 0; round < 2; round++) {
      const s = await student.post("/study/sessions", { kind: "learn", planId });
      expect(s.status).toBe(200);
      const wrongSet = round === 0 ? new Set([(await student.get(`/study/sessions/${s.body.data.id}`)).body.data.items[0].wordId]) : new Set<string>();
      const ids = await learnAll(student, s.body.data.id, wrongSet);
      const done = await student.post(`/study/sessions/${s.body.data.id}/complete`);
      expect(done.status).toBe(200);
      learnedIds.push(...ids);
    }
    expect(learnedIds).toHaveLength(20);
  });

  it("预览：不熟的词入选（默认来源），越权数量按 count 截断，含 wrong14 的词分数更高", async () => {
    const r = await student.post("/sheets/preview", { count: 30 });
    expect(r.status).toBe(200);
    const items = r.body.data.items as { wordId: string; score: number; reasons: { kind: string }[] }[];
    expect(items).toHaveLength(20); // 全部 20 个刚学的词都「不熟」（学习中层级）
    expect(items.map((i) => i.wordId).sort()).toEqual([...learnedIds].sort());
    expect(items.every((i) => i.reasons.some((x) => x.kind === "learning"))).toBe(true);
    // 首位答错的词分数最高（wrong ×3 + learning ×2 = 5），排在最前
    const wrongWord = items.find((i) => i.reasons.some((x) => x.kind === "wrong"));
    expect(wrongWord).toBeDefined();
    expect(wrongWord!.score).toBeGreaterThan(items.find((i) => i.wordId !== wrongWord!.wordId)!.score);
    expect(items[0].wordId).toBe(wrongWord!.wordId);
    expectShape("sheets-preview", r.body);

    const capped = await student.post("/sheets/preview", { count: 10 });
    expect(capped.body.data.items).toHaveLength(10);
  });

  it("按单元 / 整本书预览：没学过的词按池顺序排在不熟的词后面（K10 不建卡）", async () => {
    const byUnit = await student.post("/sheets/preview", { count: 30, source: { kind: "unit", unitId } });
    expect(byUnit.status).toBe(200);
    const uItems = byUnit.body.data.items as { wordId: string; reasons: { kind: string }[] }[];
    expect(uItems).toHaveLength(22);
    expect(uItems.slice(0, 20).map((i) => i.wordId).sort()).toEqual([...learnedIds].sort());
    expect(uItems.slice(20).every((i) => i.reasons.length === 1 && i.reasons[0].kind === "unlearned")).toBe(true);

    const byBook = await student.post("/sheets/preview", { count: 30, source: { kind: "book", bookId } });
    expect(byBook.status).toBe(200);
    expect((byBook.body.data.items as unknown[]).length).toBe(22);
    expectShape("sheets-preview-unit", byUnit.body);

    // 单元 / 整本书须操作者可见（学生在这个老师的班上，老师自己的书对班上学生可见）；未越权用例见后面的权限小节
    const teacherView = await teacher.post("/sheets/preview", { userId: studentId, count: 10, source: { kind: "unit", unitId } });
    expect(teacherView.status).toBe(200);
  });

  let sheetA = "";
  let sheetB = "";
  let sheetC = "";
  let sheetD = "";

  it("生成：本人建 #1；并发再建两张编号不撞；老师给本学生建；越权 403", async () => {
    const a = await student.post("/sheets", { wordIds: [...learnedIds.slice(0, 5), unlearnedId] });
    expect(a.status).toBe(200);
    expect(a.body.data.seq).toBe(1);
    sheetA = a.body.data.id;

    const [b, c] = await Promise.all([student.post("/sheets", { wordIds: learnedIds.slice(5, 8) }), student.post("/sheets", { wordIds: learnedIds.slice(8, 10) })]);
    expect([b.status, c.status]).toEqual([200, 200]);
    expect([b.body.data.seq, c.body.data.seq].sort()).toEqual([2, 3]);
    sheetB = b.body.data.id;
    sheetC = c.body.data.id;

    const d = await teacher.post("/sheets", { userId: studentId, wordIds: learnedIds.slice(10, 12) });
    expect(d.status).toBe(200);
    expect(d.body.data.seq).toBe(4);
    sheetD = d.body.data.id;

    expect((await otherTeacher.post("/sheets", { userId: studentId, wordIds: learnedIds.slice(0, 1) })).status).toBe(403);
    expect((await outsider.post("/sheets", { userId: studentId, wordIds: learnedIds.slice(0, 1) })).status).toBe(403);
  });

  it("批量生成：份数 1–7 × 每份 ≤30，按顺序切分、编号连续、越界 400", async () => {
    const pool = learnedIds.slice(12, 20); // 8 个未占用的已学词
    expect((await student.post("/sheets", { wordIds: pool, copies: 3, perSheet: 2 })).status).toBe(400); // 8 > 3*2

    const r = await student.post("/sheets", { wordIds: pool, copies: 4, perSheet: 2 });
    expect(r.status).toBe(200);
    const items = r.body.data.items as { id: string; seq: number }[];
    expect(items.map((x) => x.seq)).toEqual([5, 6, 7, 8]);
    expect(r.body.data).toMatchObject({ id: items[0].id, seq: 5 });
    const details = await Promise.all(items.map((x) => student.get(`/sheets/${x.id}`)));
    expect(details.map((d) => d.body.data.words.map((w: { wordId: string }) => w.wordId))).toEqual([pool.slice(0, 2), pool.slice(2, 4), pool.slice(4, 6), pool.slice(6, 8)]);
  });

  it("预览排除待测单子上的词（K33：只有默认「不熟的词」来源才排除）", async () => {
    const r = await student.post("/sheets/preview", { count: 30 });
    const ids = (r.body.data.items as { wordId: string }[]).map((i) => i.wordId);
    // sheetA..D 与批量 4 份占用的词全部在「待测」状态（都没交过卷），应被排除
    expect(ids).toHaveLength(0);
    // 单元来源不受「待测单子」影响（K33），仍能看到全部已学词
    const byUnit = await student.post("/sheets/preview", { count: 30, source: { kind: "unit", unitId } });
    expect((byUnit.body.data.items as unknown[]).length).toBe(22);
  });

  it("列表与明细：本人、班主任可看；别班老师 403", async () => {
    const mine = await student.get("/sheets?limit=100");
    expect(mine.status).toBe(200);
    expect(mine.body.data.total).toBe(8); // A B C D + 批量 4 份
    const first = mine.body.data.items[0]; // seq 倒序，最新的批量第 4 份在最前
    expect(first).toMatchObject({ seq: 8, wordCount: 2, status: "pending", creatorName: "单词单学生", firstResult: null, activeSessionId: null });
    expect((await teacher.get(`/sheets?userId=${studentId}&limit=100`)).body.data.total).toBe(8);
    expect((await otherTeacher.get(`/sheets?userId=${studentId}`)).status).toBe(403);

    const d = await teacher.get(`/sheets/${sheetA}`);
    expect(d.status).toBe(200);
    expect(d.body.data).toMatchObject({ seq: 1, student: { id: studentId }, modes: ["recognition", "spelling"] });
    expect(d.body.data.words).toHaveLength(6);
    expect(d.body.data.words[0]).toHaveProperty("phonetic");
    expect(d.body.data).not.toHaveProperty("userId");
    expectShape("sheets-detail", d.body);
    expect((await otherTeacher.get(`/sheets/${sheetA}`)).status).toBe(403);
    expectShape("sheets-list", mine.body);
  });

  it("删除：未开测的可删；别人不能删；开过测的不能删", async () => {
    expect((await outsider.del(`/sheets/${sheetC}`)).status).toBe(403);
    expect((await student.del(`/sheets/${sheetC}`)).status).toBe(200);
    expect((await student.get(`/sheets/${sheetC}`)).status).toBe(404);
  });

  let sheetASession = "";

  it("测试流（K30）：只有本人能开测；同时只能测一张；进行中不泄漏答案", async () => {
    expect((await teacher.post("/study/sessions", { kind: "sheet", sheetId: sheetA })).status).toBe(404);
    const start = await student.post("/study/sessions", { kind: "sheet", sheetId: sheetA });
    expect(start.status).toBe(200);
    sheetASession = start.body.data.id;
    expect((await student.post("/study/sessions", { kind: "sheet", sheetId: sheetA })).body.data).toEqual({ id: sheetASession, resumed: true });

    const blocked = await student.post("/study/sessions", { kind: "sheet", sheetId: sheetD });
    expect(blocked.status).toBe(400);
    expect(blocked.body.error.message).toContain("#1");

    const detail = await student.get(`/study/sessions/${sheetASession}`);
    expect(detail.body.data).toMatchObject({ kind: "sheet", sheetId: sheetA, planName: "单词单 #1", userId: studentId });
    expect(detail.body.data.items.every((i: { answer: string }) => i.answer === "")).toBe(true);

    // 今日页只显示这一份；remaining 数其余未测的（B D + 批量 4 份 = 6，C 已删除不算）
    const today = await student.get("/today");
    expect(today.body.data.sheet).toMatchObject({ id: sheetA, activeSessionId: sheetASession, remaining: 6 });
    expectShape("today-with-sheet", today.body);
  });

  it("交卷：K19 挡住同日重复更新记忆；成绩、错词与列表状态正确", async () => {
    // 6 个词：5 已学 + 1 没学过；第一个词故意认义答错
    const wrongId = learnedIds[0];
    const { correct, total } = await answerSheetTest(student, sheetASession, (id) => id === wrongId);
    expect(total).toBe(12); // 6 词 × 2 题型
    expect(correct).toBe(11);

    const summaryBefore = await student.get("/records/summary");

    const done = await student.post(`/study/sessions/${sheetASession}/complete`);
    expect(done.status).toBe(200);
    expect(done.body.data.result).toMatchObject({ words: 6, settled: 6, unsettled: 0, correctFirst: 11, totalFirst: 12 });
    // 同一学习日内这些词已经在 learn 组更新过记忆：K19 挡住单词单测试再次更新
    expect(done.body.data.result.ratings).toEqual({});
    expect(done.body.data.result.wrongWordIds).toEqual([wrongId]);
    expectShape("sheets-complete", done.body);

    // 没学过的词不建卡（K10）
    const hist = await student.get(`/records/words/${unlearnedId}`);
    expect(hist.body.data.memory).toBeNull();

    const item = (await student.get(`/sheets?limit=100`)).body.data.items.find((i: { id: string }) => i.id === sheetA);
    expect(item).toMatchObject({ status: "tested", activeSessionId: null, firstResult: { sessionId: sheetASession, correct: 11, total: 12 } });
    expect((await student.get("/today")).body.data.sheet?.id).not.toBe(sheetA);

    const rec = await student.get("/records/sessions?kind=sheet&limit=100");
    expect(rec.status).toBe(200);
    const recItem = rec.body.data.items.find((s: { id: string }) => s.id === sheetASession);
    expect(recItem).toMatchObject({ kind: "sheet", planName: "单词单 #1" });

    // 进行中检测的作答不进入统计（已在 K19/进行中检测的其它用例覆盖，这里只确认交卷不影响昨天已有的今日统计基线不倒退）
    expect((await student.get("/records/summary")).body.data.today.answers).toBeGreaterThanOrEqual(summaryBefore.body.data.today.answers);
  });

  it("重测只算练习；已测的单子不能删", async () => {
    const again = await student.post("/study/sessions", { kind: "sheet", sheetId: sheetA });
    expect(again.body.data.resumed).toBe(false);
    // 重测进行中要出现在今日页，才能续做
    expect((await student.get("/today")).body.data.sheet?.id).toBe(sheetA);
    const sid = again.body.data.id as string;
    await answerSheetTest(student, sid);
    const done = await student.post(`/study/sessions/${sid}/complete`);
    expect(done.status).toBe(200);
    expect(done.body.data.result.ratings).toEqual({});

    expect((await student.del(`/sheets/${sheetA}`)).status).toBe(400);
    expect((await student.del(`/sheets/${sheetA}`)).body.error).toMatchObject({ code: "INVALID_ACTION", message: "已经开始测试的单词单不能删除" });
  });
});

describe.runIf(ready("study", "plans", "books", "classes", "users", "sheets"))("单词单：错词来源", () => {
  const emails = { teacher: `shs2t-${STAMP}@vinx.test`, other: `shs2o-${STAMP}@vinx.test`, student: `shs2s-${STAMP}@vinx.test`, outsider: `shs2x-${STAMP}@vinx.test` };
  let teacher: Client, otherTeacher: Client, student: Client, outsider: Client;
  let studentId = "";
  let testSessionId = "";
  let wrongIds: string[] = [];
  let rightIds: string[] = [];

  beforeAll(async () => {
    const admin = await login("admin@vinx.test");
    for (const [k, email] of [["teacher", emails.teacher], ["other", emails.other]] as const) {
      expect((await admin.post("/users", { email, name: k, password: "dev123456", role: "teacher" })).status).toBe(200);
    }
    teacher = await login(emails.teacher);
    otherTeacher = await login(emails.other);
    const cls = await teacher.post("/classes", { name: `错词来源班 ${STAMP}` });
    const su = anon();
    const signup = await su.post("/auth/signup", { email: emails.student, password: "dev123456", name: "错词学生", inviteCode: cls.body.data.inviteCode });
    studentId = signup.body.data.user.id;
    student = su;
    const ou = anon();
    await ou.post("/auth/signup", { email: emails.outsider, password: "dev123456" });
    outsider = ou;

    const book = await teacher.post("/books", { name: `错词来源书 ${STAMP}` });
    const u = await makeUnit(teacher, book.body.data.id, "U1", 6, `wr${STAMP.slice(-6)}`);
    const plan = await teacher.post("/plans", { name: `错词计划 ${STAMP}`, newPerDay: 6, reviewPerDay: 0, unitIds: [u.unitId], targets: { classIds: [], userIds: [studentId] } });
    const s = await student.post("/study/sessions", { kind: "learn", planId: plan.body.data.id });
    const learned = await learnAll(student, s.body.data.id);
    await student.post(`/study/sessions/${s.body.data.id}/complete`);

    const sheet = await student.post("/sheets", { wordIds: learned });
    const start = await student.post("/study/sessions", { kind: "sheet", sheetId: sheet.body.data.id });
    testSessionId = start.body.data.id;
    wrongIds = learned.slice(0, 2);
    rightIds = learned.slice(2);
    const { correct, total } = await answerSheetTest(student, testSessionId, (id) => wrongIds.includes(id));
    expect(total).toBe(12);
    expect(correct).toBe(10);
    expect((await student.post(`/study/sessions/${testSessionId}/complete`)).status).toBe(200);
  });

  it("错词来源列表：本人、班主任可看，列出那次测试和错词数（去重按词，不按题型）；别班老师 403", async () => {
    const mine = await student.get("/sheets/sources");
    expect(mine.status).toBe(200);
    const item = mine.body.data.sessions.find((x: { id: string }) => x.id === testSessionId);
    expect(item).toMatchObject({ kind: "sheet", planName: "单词单 #1", wrongCount: 2 });
    expect((await teacher.get(`/sheets/sources?userId=${studentId}`)).body.data.sessions.map((x: { id: string }) => x.id)).toContain(testSessionId);
    expect((await otherTeacher.get(`/sheets/sources?userId=${studentId}`)).status).toBe(403);
    expectShape("sheets-sources", mine.body);
  });

  it("按某次测试的错词预览：只出那次答错的词，保留已掌握/干净的（keepFamiliar）；越权 403", async () => {
    const r = await student.post("/sheets/preview", { count: 30, source: { kind: "session", sessionId: testSessionId } });
    expect(r.status).toBe(200);
    const items = r.body.data.items as { wordId: string; reasons: { kind: string }[] }[];
    expect(items.map((i) => i.wordId).sort()).toEqual([...wrongIds].sort());
    expect(items.every((i) => i.reasons[0].kind === "sessionWrong")).toBe(true);
    expect((await teacher.post("/sheets/preview", { userId: studentId, count: 30, source: { kind: "session", sessionId: testSessionId } })).status).toBe(200);
    expect((await outsider.post("/sheets/preview", { count: 30, source: { kind: "session", sessionId: testSessionId } })).status).toBe(403);
    expect((await otherTeacher.post("/sheets/preview", { count: 30, source: { kind: "session", sessionId: testSessionId } })).status).toBe(403);
  });

  it("确认正确词不在错词来源里", async () => {
    const r = await student.post("/sheets/preview", { count: 30, source: { kind: "session", sessionId: testSessionId } });
    const ids = (r.body.data.items as { wordId: string }[]).map((i) => i.wordId);
    for (const id of rightIds) expect(ids).not.toContain(id);
  });
});
