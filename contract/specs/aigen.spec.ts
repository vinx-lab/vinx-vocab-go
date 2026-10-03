/**
 * AI 生成：学段、句型、仿写、重写一句、逐句短文（spec 0005，以及 spec 0004 的 /passages/:id 逐句字段；只在 Go 上跑）。
 *
 * 用测试里起的假 OpenAI 兼容服务（lib/fake-ai.ts 的 generationReply 按请求种类给出合法的逐句 JSON），不依赖真实 AI。
 * 覆盖：提示词设置扩展到 4 项；词书学段；句型 / 仿写的预览、生成（草稿带每句检查）、保存成单元的篇；
 * 学生在单元页看到保存的句型；重写一句；短文按句保存；新接口的权限（学生不能生成句型或仿写，别人的词书 403，
 * 系统词书只有管理员，未登录 401）。
 * 结束前一律 DELETE /settings/ai 与 pattern / variant 两个提示词模板，不影响其他用例。
 */
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { anon, login, type Client } from "../lib/client";
import { goOnly } from "../lib/areas";
import { generationKind, generationReply, startFakeAi, type FakeAi } from "../lib/fake-ai";
import { importBook, newClassWithStudent, newTeacher, uniq, waitJob, type Account, type BuiltBook } from "../lib/fixtures";

const KEY = "sk-contract-aigen-secret-4242";

interface Checks {
  error: boolean;
  warning: boolean;
  issues: string[];
  outOfScope: string[];
  missingTargets: string[];
  tooLong: boolean;
  words: number;
  maxWords: number;
  structureDeviates: boolean;
  similarity: number | null;
}

describe.runIf(goOnly)("AI 生成：学段、句型、仿写、逐句短文", () => {
  let fake: FakeAi;
  let admin: Client;
  let teacher: Account, other: Account, student: Account;
  let book: BuiltBook;
  let unitId = "";
  let patternTextId = "";
  let patternSentenceId = "";
  const sp: string[] = [];

  beforeAll(async () => {
    admin = await login("admin@vinx.test");
    fake = await startFakeAi();
    fake.set({ reply: generationReply });
    const ai = await admin.put("/settings/ai", { provider: "openai", baseUrl: fake.url, apiKey: KEY, model: "fake-model", timeoutSec: 10 });
    expect(ai.status, ai.text).toBe(200);
    teacher = await newTeacher(admin, "aigen");
    other = await newTeacher(admin, "aigen2");
    student = (await newClassWithStudent(teacher.client, "生成")).student;
    const tag = uniq();
    for (const s of ["alpha", "bravo", "charlie", "delta"]) sp.push(`vxgen${s}${tag}`);
    book = await importBook(teacher.client, "生成词书", [{ name: "Unit 1", entries: sp.map((s, i) => ({ spelling: s, definition: `生成义${i}`, partOfSpeech: "n." })) }]);
    unitId = book.units[0].id;
  });

  afterAll(async () => {
    await admin.del("/settings/ai");
    await admin.del("/settings/ai/prompts/pattern");
    await admin.del("/settings/ai/prompts/variant");
    await fake?.close();
  });

  it("提示词设置扩展到 4 项（example、passage、pattern、variant），默认模板保留学段占位符；可改、可恢复默认", async () => {
    await admin.del("/settings/ai/prompts/pattern");
    await admin.del("/settings/ai/prompts/variant");
    const r = await admin.get("/settings/ai/prompts");
    expect(r.status).toBe(200);
    expect(Object.keys(r.body.data).sort()).toEqual(["example", "passage", "pattern", "variant"]);
    for (const k of ["example", "passage", "pattern", "variant"]) {
      expect(r.body.data[k]).toMatchObject({ key: k, isDefault: true });
      expect(r.body.data[k].current).toBe(r.body.data[k].defaultText);
      expect(r.body.data[k].defaultText).toContain("{学段}");
    }
    const put = await admin.put("/settings/ai/prompts", { pattern: "自定义句型模板", variant: "自定义仿写模板" });
    expect(put.status).toBe(200);
    expect(put.body.data.pattern).toMatchObject({ current: "自定义句型模板", isDefault: false });
    expect(put.body.data.variant).toMatchObject({ current: "自定义仿写模板", isDefault: false });
    const del = await admin.del("/settings/ai/prompts/variant");
    expect(del.status).toBe(200);
    expect(del.body.data.variant.isDefault).toBe(true);
    expect(del.body.data.pattern.isDefault).toBe(false);
    expect((await admin.del("/settings/ai/prompts/pattern")).body.data.pattern.isDefault).toBe(true);
    expect((await admin.put("/settings/ai/prompts", { variant: "   " })).status).toBe(400);
    expect((await admin.del("/settings/ai/prompts/bogus")).status).toBe(400);
    expect((await teacher.client.put("/settings/ai/prompts", { pattern: "x" })).status).toBe(403);
  });

  it("词书学段：PATCH /books/:id 的 level，列表与详情都带；非法值 400；别人的词书 403", async () => {
    expect((await teacher.client.get(`/books/${book.bookId}`)).body.data.level).toBeNull();
    const p = await teacher.client.patch(`/books/${book.bookId}`, { level: "primary" });
    expect(p.status, p.text).toBe(200);
    expect(p.body.data.level).toBe("primary");
    expect((await teacher.client.get(`/books/${book.bookId}`)).body.data.level).toBe("primary");
    const list = await teacher.client.get("/books");
    expect(list.body.data.items.find((b: { id: string }) => b.id === book.bookId).level).toBe("primary");
    expect((await teacher.client.patch(`/books/${book.bookId}`, { level: "senior" })).status).toBe(400);
    expect((await other.client.patch(`/books/${book.bookId}`, { level: "exam" })).status).toBe(403);
    expect((await teacher.client.patch(`/books/${book.bookId}`, { level: "junior" })).body.data.level).toBe("junior");
  });

  it("句型：预览按词书学段替换占位符；生成得到带检查的草稿（不写库）；保存成单元的句型清单；学生在单元页看到", async () => {
    const pv = await teacher.client.post(`/ai/units/${unitId}/patterns/preview`, { topic: "学习方法", count: 6 });
    expect(pv.status, pv.text).toBe(200);
    expect(pv.body.data).toMatchObject({ level: "junior", topic: "学习方法", count: 6 });
    expect(pv.body.data.prompt).toContain("你是初中英语教材编辑");
    expect(pv.body.data.prompt).toContain("话题或语法点：学习方法");
    expect(pv.body.data.prompt).not.toMatch(/\{[^{}\n]+\}/);
    expect(pv.body.data.words).toHaveLength(4);
    // 临时改学段
    const exam = await teacher.client.post(`/ai/units/${unitId}/patterns/preview`, { topic: "x", level: "exam" });
    expect(exam.body.data.level).toBe("exam");
    // 话题默认取单元名，数量默认 8
    expect((await teacher.client.post(`/ai/units/${unitId}/patterns/preview`, {})).body.data).toMatchObject({ topic: "Unit 1", count: 8 });
    for (const bad of [{ topic: "x", count: 3 }, { topic: "x", count: 13 }, { topic: "x", level: "senior" }]) {
      expect((await teacher.client.post(`/ai/units/${unitId}/patterns`, bad)).status).toBe(400);
    }

    const textsBefore = (await teacher.client.get(`/units/${unitId}/texts`)).body.data.items.length;
    fake.requests.length = 0;
    const started = await teacher.client.post(`/ai/units/${unitId}/patterns`, { topic: "学习方法", count: 4 });
    expect(started.status, started.text).toBe(200);
    const job = await waitJob(teacher.client, started.body.data.jobId);
    expect(job, JSON.stringify(job)).toMatchObject({ kind: "patterns", status: "done" });
    expect(generationKind(fake.requests[0])).toBe("pattern");
    expect(job.result).toMatchObject({ unitId, level: "junior", title: "Unit 1 重点句型" });
    const items = job.result.items as { en: string; cn: string; frame?: string | null; checks: Checks }[];
    expect(items.map((i) => i.en)).toEqual(["How do you learn English?", "I learn English by reading."]);
    expect(items[1].frame).toBe("I learn ... by doing ...");
    for (const it of items) {
      expect(typeof it.checks.warning).toBe("boolean");
      expect(Array.isArray(it.checks.outOfScope)).toBe(true);
      expect(it.checks.maxWords).toBe(20);
    }
    // 草稿不落库
    expect((await teacher.client.get(`/units/${unitId}/texts`)).body.data.items).toHaveLength(textsBefore);

    // 编辑草稿后保存：新建一篇 list，默认标题
    const saved = await teacher.client.post(`/ai/units/${unitId}/patterns/save`, {
      sentences: [
        { en: `I like the ${sp[0]}.`, cn: "我喜欢甲。", frame: "I like ..." },
        { en: items[1].en, cn: items[1].cn, frame: items[1].frame },
      ],
    });
    expect(saved.status, saved.text).toBe(200);
    expect(saved.body.data).toMatchObject({ kind: "list", title: "Unit 1 重点句型", unitId });
    const ss = saved.body.data.sentences as { id: string; en: string; source: string; frame: string | null; words: { wordId: string }[] }[];
    expect(ss.map((s) => s.source)).toEqual(["ai", "ai"]);
    expect(ss[0]).toMatchObject({ en: `I like the ${sp[0]}.`, frame: "I like ..." });
    expect(ss[0].words.some((w) => w.wordId === book.wordId(sp[0]))).toBe(true);
    patternTextId = saved.body.data.id;
    patternSentenceId = ss[0].id;

    // 追加到已有清单（标题不变）；课文篇、不存在的篇 400；空句子 400
    const app = await teacher.client.post(`/ai/units/${unitId}/patterns/save`, { textId: patternTextId, title: "忽略", sentences: [{ en: "Is it useful?", cn: "它有用吗？" }] });
    expect(app.status).toBe(200);
    expect(app.body.data).toMatchObject({ id: patternTextId, title: "Unit 1 重点句型" });
    expect(app.body.data.sentences).toHaveLength(3);
    expect((await teacher.client.post(`/ai/units/${unitId}/patterns/save`, { sentences: [] })).status).toBe(400);
    expect((await teacher.client.post(`/ai/units/${unitId}/patterns/save`, { textId: "nope", sentences: [{ en: "a", cn: "b" }] })).status).toBe(400);
    const txt = await teacher.client.post(`/units/${unitId}/texts`, { title: "课文", kind: "text", sentences: [{ en: "Hi.", cn: "嗨。" }] });
    expect((await teacher.client.post(`/ai/units/${unitId}/patterns/save`, { textId: txt.body.data.id, sentences: [{ en: "a", cn: "b" }] })).status).toBe(400);

    // 学生在单元页看到
    const seen = (await student.client.get(`/units/${unitId}/texts`)).body.data.items as { id: string; sentences: { en: string }[] }[];
    expect(seen.find((t) => t.id === patternTextId)?.sentences.map((s) => s.en)).toEqual([`I like the ${sp[0]}.`, items[1].en, "Is it useful?"]);
  });

  it("仿写：预览列出原句（已有句子 + 粘贴）；生成的变式带原句与检查；保存成「句型仿写」并记录原句与改动", async () => {
    const body = { unitId, sentenceIds: [patternSentenceId], pasted: "It is fun. | 很有趣。\nI am here.", modes: ["replace", "transform"], perItem: 2 };
    const pv = await teacher.client.post("/ai/variants/preview", body);
    expect(pv.status, pv.text).toBe(200);
    expect(pv.body.data.level).toBe("junior");
    expect(pv.body.data.prompt).toContain("你是初中英语老师");
    expect(pv.body.data.prompt).toContain("每个例句写 2 个变式");
    const origins = pv.body.data.origins as { id: string | null; en: string }[];
    expect(origins.map((o) => o.en)).toEqual([`I like the ${sp[0]}.`, "It is fun.", "I am here."]);
    expect(origins[0].id).toBe(patternSentenceId);
    expect(origins[1].id).toBeNull();
    for (const bad of [
      { unitId, pasted: "a", modes: [] },
      { unitId, pasted: "a", modes: ["bogus"] },
      { unitId, modes: ["replace"] },
      { unitId, pasted: "a", modes: ["replace"], perItem: 6 },
      { pasted: "a", modes: ["replace"] },
    ]) {
      expect((await teacher.client.post("/ai/variants/preview", bad)).status).toBe(400);
    }
    expect((await teacher.client.post("/ai/variants/preview", { unitId, sentenceIds: ["nope"], modes: ["replace"] })).status).toBe(404);

    fake.requests.length = 0;
    const started = await teacher.client.post("/ai/variants", body);
    expect(started.status, started.text).toBe(200);
    const job = await waitJob(teacher.client, started.body.data.jobId);
    expect(job, JSON.stringify(job)).toMatchObject({ kind: "variants", status: "done" });
    expect(generationKind(fake.requests[0])).toBe("variant");
    expect(job.result).toMatchObject({ unitId, title: "Unit 1 句型仿写" });
    const items = job.result.items as { origin: number; originId: string | null; originEn: string; change: string; note: string; checks: Checks }[];
    expect(items).toHaveLength(3);
    expect(items[0]).toMatchObject({ origin: 1, originId: patternSentenceId, originEn: `I like the ${sp[0]}.`, change: "replace", note: "换了动名词" });
    expect(items[1]).toMatchObject({ origin: 2, originId: null, originEn: "It is fun." });
    expect(typeof items[0].checks.structureDeviates).toBe("boolean");

    const saved = await teacher.client.post("/ai/variants/save", {
      unitId,
      sentences: [
        { en: `I like the ${sp[1]}.`, cn: "我喜欢乙。", originId: patternSentenceId, variantNote: "换了名词", change: "replace" },
        { en: `Do you like the ${sp[0]}?`, cn: "你喜欢甲吗？", originId: patternSentenceId, variantNote: "改成疑问句", change: "transform" },
      ],
    });
    expect(saved.status, saved.text).toBe(200);
    expect(saved.body.data).toMatchObject({ kind: "list", title: "Unit 1 句型仿写" });
    expect(saved.body.data.sentences.map((s: { source: string }) => s.source)).toEqual(["variant", "variant"]);
    expect((await teacher.client.post("/ai/variants/save", { unitId, sentences: [{ en: "a", cn: "b", originId: "nope" }] })).status).toBe(400);
    expect((await teacher.client.post("/ai/variants/save", { unitId, sentences: [{ en: "a", cn: "b", change: "bogus" }] })).status).toBe(400);
  });

  it("重写一句：同步返回新句子与检查；学生不能重写句型；别人的单元 403", async () => {
    const r = await teacher.client.post("/ai/sentences/rewrite", { en: "I like giraffes.", cn: "我喜欢长颈鹿。", issues: ["超纲词：giraffes"], level: "primary", kind: "pattern", unitId });
    expect(r.status, r.text).toBe(200);
    expect(r.body.data).toMatchObject({ en: "I like reading books.", cn: "我喜欢读书。", level: "primary" });
    expect(r.body.data.checks.maxWords).toBe(12);
    expect((await teacher.client.post("/ai/sentences/rewrite", { en: "", kind: "pattern" })).status).toBe(400);
    expect((await teacher.client.post("/ai/sentences/rewrite", { en: "x", kind: "bogus" })).status).toBe(400);
    expect((await student.client.post("/ai/sentences/rewrite", { en: "x", kind: "pattern" })).status).toBe(403);
    expect((await other.client.post("/ai/sentences/rewrite", { en: "x", kind: "pattern", unitId })).status).toBe(403);
    expect((await anon().post("/ai/sentences/rewrite", { en: "x", kind: "pattern" })).status).toBe(401);
  });

  it("权限：学生不能生成句型或仿写；别人的词书 403；未登录 401；系统词书只有管理员能生成", async () => {
    const calls: [string, unknown][] = [
      [`/ai/units/${unitId}/patterns/preview`, { topic: "x" }],
      [`/ai/units/${unitId}/patterns`, { topic: "x" }],
      [`/ai/units/${unitId}/patterns/save`, { sentences: [{ en: "a", cn: "b" }] }],
      ["/ai/variants/preview", { unitId, pasted: "I like it.", modes: ["replace"] }],
      ["/ai/variants", { unitId, pasted: "I like it.", modes: ["replace"] }],
      ["/ai/variants/save", { unitId, sentences: [{ en: "a", cn: "b" }] }],
    ];
    for (const [path, body] of calls) {
      expect((await student.client.post(path, body)).status, `学生 ${path}`).toBe(403);
      expect((await other.client.post(path, body)).status, `别人的词书 ${path}`).toBe(403);
      expect((await anon().post(path, body)).status, `未登录 ${path}`).toBe(401);
    }
    const sys = (await teacher.client.get("/books")).body.data.items.find((b: { isSystem: boolean }) => b.isSystem);
    const sysUnit = (await teacher.client.get(`/books/${sys.id}`)).body.data.units[0].id as string;
    expect((await teacher.client.post(`/ai/units/${sysUnit}/patterns/preview`, { topic: "x" })).status).toBe(403);
    const adminPv = await admin.post(`/ai/units/${sysUnit}/patterns/preview`, { topic: "x" });
    expect(adminPv.status).toBe(200);
    expect(["junior", "exam"]).toContain(adminPv.body.data.level);
  });

  it("短文按句保存：生成结果与详情都带逐句（段落、关联到的词），任务结果带每句检查", async () => {
    const ids = sp.slice(0, 3).map((s) => book.wordId(s));
    const pv = await student.client.post("/ai/passages/preview", { wordIds: ids });
    expect(pv.status, pv.text).toBe(200);
    expect(["primary", "junior", "exam"]).toContain(pv.body.data.level);
    const started = await student.client.post("/passages/generate", { wordIds: ids });
    expect(started.status, started.text).toBe(200);
    const job = await waitJob(student.client, started.body.data.jobId);
    expect(job, JSON.stringify(job)).toMatchObject({ kind: "passage", status: "done" });
    const resultSentences = job.result.sentences as { en: string; paragraph: number; checks: Checks }[];
    expect(resultSentences.map((s) => s.paragraph)).toEqual([0, 0, 1]);
    expect(typeof resultSentences[0].checks.warning).toBe("boolean");

    const detail = await student.client.get(`/passages/${job.result.id}`);
    expect(detail.status).toBe(200);
    const sents = detail.body.data.sentences as { id: string; en: string; cn: string; paragraph: number; words: { wordId: string; position: number; form: string }[] }[];
    expect(sents.map((s) => [s.en, s.cn, s.paragraph])).toEqual([
      ["I get up early.", "我起得很早。", 0],
      ["Then I read a book.", "然后我读书。", 0],
      ["At night I go to bed.", "晚上我睡觉。", 1],
    ]);
    for (const s of sents) {
      expect(typeof s.id).toBe("string");
      expect(Array.isArray(s.words)).toBe(true);
    }
    // 整段正文由句子拼出来
    expect(detail.body.data.passage).toContain("I get up early.");
    expect((await teacher.client.get(`/passages/${job.result.id}`)).status).toBe(404);
    expect((await student.client.del(`/passages/${job.result.id}`)).status).toBe(200);
  });
});
