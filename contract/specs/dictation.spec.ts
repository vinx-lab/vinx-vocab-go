/**
 * 默写单（spec 0006，只在 Go 上跑）：出题（单词、短语、句子、仿写、转换；词与句子的来源）、明细（分区、提示、答案、原句）、
 * 不能在线测试、今日页待批改 / 已批改、批改（必须全部题目、学生本人提交为自批、只能提交一次 409）、
 * 列表 / 学习记录 / 成绩单带格式与自批标记、错题再出一份、老师批改不是自批，以及权限（别人的学生 403）。
 *
 * 句子来自单元的篇（spec 0004）：句型清单（带骨架的句子、普通句子）、课文，以及用 /ai/variants/save 存下的
 * 「转换」变式（spec 0005，保存草稿不调用 AI）。
 */
import { beforeAll, describe, expect, it } from "vitest";
import { login, type Client } from "../lib/client";
import { goOnly } from "../lib/areas";
import { importBook, newClassWithStudent, newStudent, newTeacher, uniq, type Account, type BuiltBook } from "../lib/fixtures";

interface DictItem {
  index: number;
  section: number;
  type: string;
  wordId?: string;
  sentenceId?: string;
  prompt: string;
  answer: string;
  origin?: { en: string } | null;
}

describe.runIf(goOnly)("默写单：出题、明细、批改、权限", () => {
  let teacher: Account, other: Account, student: Account, outsider: Account;
  let book: BuiltBook;
  let unitId = "";
  let word = "", phrase = "";
  const sent: Record<"frame" | "plain" | "text" | "transform", string> = { frame: "", plain: "", text: "", transform: "" };
  let listId = "", textId = "";
  let sheetId = "";
  let sessionId = "";
  let detailItems: DictItem[] = [];

  beforeAll(async () => {
    const admin = await login("admin@vinx.test");
    teacher = await newTeacher(admin, "dict");
    other = await newTeacher(admin, "dict2");
    student = (await newClassWithStudent(teacher.client, "默写")).student;
    outsider = await newStudent("dict-out");
    const tag = uniq();
    word = `vxdword${tag}`;
    phrase = `vxdpick${tag} up`;
    book = await importBook(teacher.client, "默写词书", [
      {
        name: "Unit 1",
        entries: [
          { spelling: word, definition: "默写词", partOfSpeech: "n." },
          { spelling: phrase, definition: "捡起", type: "phrase" },
        ],
        texts: [
          {
            title: "重点句型",
            kind: "list",
            sentences: [
              { en: `I find the ${word} useful.`, cn: "我发现这个词很有用。", frame: "I find ... useful." },
              { en: `I can ${phrase} the ${word}.`, cn: "我能捡起这个词。" },
            ],
          },
          { title: "课文", kind: "text", sentences: [{ en: `The ${word} is red.`, cn: "这个词是红色的。" }] },
        ],
      },
    ]);
    unitId = book.units[0].id;
    const texts = (await teacher.client.get(`/units/${unitId}/texts`)).body.data.items as { id: string; kind: string; sentences: { id: string; en: string }[] }[];
    const list = texts.find((t) => t.kind === "list")!;
    const text = texts.find((t) => t.kind === "text")!;
    listId = list.id;
    textId = text.id;
    sent.frame = list.sentences[0].id;
    sent.plain = list.sentences[1].id;
    sent.text = text.sentences[0].id;
    const v = await teacher.client.post("/ai/variants/save", {
      unitId,
      textId: listId,
      sentences: [{ en: `Can you ${phrase} the ${word}?`, cn: "你能捡起这个词吗？", originId: sent.plain, variantNote: "改成一般疑问句", change: "transform" }],
    });
    expect(v.status, v.text).toBe(200);
    sent.transform = (v.body.data.sentences as { id: string; en: string }[]).find((s) => s.en.startsWith("Can you"))!.id;
  });

  it("预览：词按单词 / 短语分题型，句子来自篇（句型、仿写、转换、课文）；可以不要短语、限句子数；来源列表带单元的篇", async () => {
    const pv = await student.client.post("/sheets/preview", {
      format: "dictation",
      source: { kind: "unit", unitId },
      sentenceSources: [
        { kind: "text", textId: listId },
        { kind: "text", textId: textId },
      ],
    });
    expect(pv.status, pv.text).toBe(200);
    const items = pv.body.data.items as { wordId: string; type: string }[];
    expect(items.map((i) => [i.wordId, i.type]).sort()).toEqual(
      [
        [book.wordId(word), "word"],
        [book.wordId(phrase), "phrase"],
      ].sort(),
    );
    const sentences = pv.body.data.sentences as { sentenceId: string; type: string; status: string; prompt: string; answer: string }[];
    expect(Object.fromEntries(sentences.map((s) => [s.sentenceId, s.type]))).toEqual({
      [sent.frame]: "frame",
      [sent.plain]: "sentence",
      [sent.transform]: "transform",
      [sent.text]: "sentence",
    });
    expect(sentences.every((s) => s.status === "untested")).toBe(true);
    const frame = sentences.find((s) => s.sentenceId === sent.frame)!;
    expect(frame.prompt).toBe("I find ___ useful.（我发现这个词很有用。）");
    const transform = sentences.find((s) => s.sentenceId === sent.transform)!;
    expect(transform).toMatchObject({ prompt: `I can ${phrase} the ${word}.（改成一般疑问句）`, answer: `Can you ${phrase} the ${word}?` });

    const limited = await student.client.post("/sheets/preview", {
      format: "dictation",
      source: { kind: "unit", unitId },
      includePhrases: false,
      sentenceCount: 2,
      sentenceSources: [{ kind: "text", textId: listId }],
    });
    expect(limited.body.data.items.map((i: { wordId: string }) => i.wordId)).toEqual([book.wordId(word)]);
    expect(limited.body.data.sentences).toHaveLength(2);

    const bad = await student.client.post("/sheets/preview", { format: "x", sentenceSources: [{ kind: "text" }, { kind: "bad" }], sentenceCount: 21 });
    expect(bad.status).toBe(400);
    expect(bad.body.error.details).toMatchObject({ "sentenceSources.0.textId": "Required" });
    for (const k of ["format", "sentenceSources.1.kind", "sentenceCount"]) expect(bad.body.error.details[k], k).toBeDefined();

    // 看不到的词书里的篇 404
    expect((await outsider.client.post("/sheets/preview", { format: "dictation", sentenceSources: [{ kind: "text", textId: listId }] })).status).toBe(404);

    const src = await student.client.get(`/sheets/sources?unitId=${unitId}`);
    expect(src.status).toBe(200);
    expect(src.body.data.learningSentences).toBe(0);
    const texts = src.body.data.texts as { id: string; kind: string; sentenceCount: number }[];
    expect(texts.map((t) => t.id)).toEqual([listId, textId]);
    expect(texts[0]).toMatchObject({ kind: "list", sentenceCount: 3 });
    expect((await student.client.get("/sheets/sources")).body.data.texts).toBeUndefined();
  });

  it("出题与明细：按分区排好（单词、短语、句子、仿写与转换），带提示、答案与原句；不能在线测试；今日页显示待批改", async () => {
    const bad = await student.client.post("/sheets", { format: "dictation", items: [{ type: "frame", sentenceId: sent.plain }] });
    expect(bad.status).toBe(400);
    expect((await student.client.post("/sheets", { format: "dictation", items: [] })).status).toBe(400);
    expect((await student.client.post("/sheets", { format: "dictation", items: [{ type: "word" }] })).status).toBe(400);
    // 别的老师不能给这个学生出题
    expect((await other.client.post("/sheets", { userId: student.id, format: "dictation", items: [{ type: "word", wordId: book.wordId(word) }] })).status).toBe(403);

    const created = await student.client.post("/sheets", {
      format: "dictation",
      items: [
        { type: "sentence", sentenceId: sent.plain },
        { type: "word", wordId: book.wordId(word) },
        { type: "phrase", wordId: book.wordId(phrase) },
        { type: "frame", sentenceId: sent.frame },
        { type: "transform", sentenceId: sent.transform },
      ],
    });
    expect(created.status, created.text).toBe(200);
    sheetId = created.body.data.id;

    const detail = await student.client.get(`/sheets/${sheetId}`);
    expect(detail.status).toBe(200);
    expect(detail.body.data).toMatchObject({ format: "dictation", grading: null });
    detailItems = detail.body.data.items;
    expect(detailItems.map((i) => i.type)).toEqual(["word", "phrase", "sentence", "frame", "transform"]);
    expect(detailItems.map((i) => i.index)).toEqual([0, 1, 2, 3, 4]);
    expect(detailItems[0]).toMatchObject({ section: 1, prompt: "n. 默写词", answer: word, wordId: book.wordId(word) });
    expect(detailItems[1]).toMatchObject({ prompt: "捡起", answer: phrase });
    expect(detailItems[2]).toMatchObject({ prompt: "我能捡起这个词。", answer: `I can ${phrase} the ${word}.`, sentenceId: sent.plain });
    expect(detailItems[4].origin).toMatchObject({ en: `I can ${phrase} the ${word}.` });

    const start = await student.client.post("/study/sessions", { kind: "sheet", sheetId });
    expect(start.status).toBe(400);
    expect(start.body.error).toMatchObject({ code: "INVALID_ACTION", message: "默写单不能在线测试，请打印后批改" });
    const today = await student.client.get("/today");
    expect(today.body.data.sheet).toMatchObject({ id: sheetId, format: "dictation", itemCount: 5 });
    expect(today.body.data.gradedSheets).toEqual([]);
  });

  it("权限：本班老师能看；其他学生、别班老师看不到也不能批改；批改要包含全部题目", async () => {
    expect((await teacher.client.get(`/sheets/${sheetId}`)).status).toBe(200);
    expect((await outsider.client.get(`/sheets/${sheetId}`)).status).toBe(403);
    expect((await other.client.get(`/sheets/${sheetId}`)).status).toBe(403);
    expect((await outsider.client.post(`/sheets/${sheetId}/grade`, { results: [{ index: 0, correct: true }] })).status).toBe(403);
    expect((await other.client.post(`/sheets/${sheetId}/grade`, { results: detailItems.map((i) => ({ index: i.index, correct: true })) })).status).toBe(403);
    const partial = await student.client.post(`/sheets/${sheetId}/grade`, { results: [{ index: 0, correct: true }] });
    expect(partial.status).toBe(400);
    expect(partial.body.error).toMatchObject({ code: "VALIDATION", message: "需要批改全部 5 道题" });
    expect((await student.client.post(`/sheets/${sheetId}/grade`, { results: [{ index: "a" }] })).status).toBe(400);
  });

  it("学生本人批改（自批）：标两道错题后成绩单、今日页已批改、列表、记录都带自批标记；只能提交一次；批改过的不能删", async () => {
    const wrong = new Set([detailItems[0].index, detailItems[3].index]); // 单词、仿写各错一道
    const graded = await student.client.post(`/sheets/${sheetId}/grade`, {
      results: detailItems.map((i) => ({ index: i.index, correct: !wrong.has(i.index), ...(wrong.has(i.index) ? { userAnswer: "写错了" } : {}) })),
    });
    expect(graded.status, graded.text).toBe(200);
    const g = graded.body.data.grading;
    expect(g).toMatchObject({ selfGraded: true, gradedBy: { id: student.id }, correct: 3, total: 5 });
    expect(g.results).toHaveLength(5);
    expect(g.results[0]).toMatchObject({ correct: false, userAnswer: "写错了" });
    expect(g.results[1].userAnswer ?? null).toBeNull();
    sessionId = g.sessionId;

    const today = await student.client.get("/today");
    expect(today.body.data.sheet?.id).not.toBe(sheetId);
    expect(today.body.data.gradedSheets).toHaveLength(1);
    expect(today.body.data.gradedSheets[0]).toMatchObject({ id: sheetId, itemCount: 5, sessionId, correct: 3, total: 5, selfGraded: true });

    const again = await student.client.post(`/sheets/${sheetId}/grade`, { results: detailItems.map((i) => ({ index: i.index, correct: true })) });
    expect(again.status).toBe(409);
    expect(again.body.error).toMatchObject({ code: "INVALID_STATUS", message: "这份默写单已经批改过" });
    expect((await student.client.del(`/sheets/${sheetId}`)).status).toBe(400);

    // 老师看到自批标记：明细、列表、学习记录
    expect((await teacher.client.get(`/sheets/${sheetId}`)).body.data.grading.selfGraded).toBe(true);
    const list = await teacher.client.get(`/sheets?userId=${student.id}`);
    const row = list.body.data.items.find((i: { id: string }) => i.id === sheetId);
    expect(row).toMatchObject({ format: "dictation", status: "tested", selfGraded: true, itemCount: 5, firstResult: { sessionId, correct: 3, total: 5 } });
    const rec = await teacher.client.get(`/records/sessions/${sessionId}`);
    expect(rec.status).toBe(200);
    expect(rec.body.data).toMatchObject({ format: "dictation", selfGraded: true });
    expect(rec.body.data.planName).toMatch(/^默写单 #\d+$/);
    const recSents = rec.body.data.sentences as { sentenceId: string; correct: boolean; userAnswer: string | null }[];
    expect(recSents).toHaveLength(3);
    expect(recSents.find((s) => s.sentenceId === sent.frame)).toMatchObject({ correct: false, userAnswer: "写错了" });
    const sessions = await teacher.client.get(`/records/sessions?userId=${student.id}`);
    expect(sessions.body.data.items.find((s: { id: string }) => s.id === sessionId)).toMatchObject({ format: "dictation", selfGraded: true });
    // 词的批改是正式测试作答：在单词历史里出现 dictation 题型
    const hist = await student.client.get(`/records/words/${book.wordId(word)}`);
    expect(JSON.stringify(hist.body.data)).toContain("dictation");
  });

  it("错题再出一份：词与句子都用这次批改的错题；要学的句子计数 +1", async () => {
    expect((await student.client.get("/sheets/sources")).body.data.learningSentences).toBe(1);
    const pv = await student.client.post("/sheets/preview", {
      format: "dictation",
      source: { kind: "session", sessionId },
      sentenceSources: [{ kind: "session", sessionId }],
    });
    expect(pv.status, pv.text).toBe(200);
    expect(pv.body.data.items.map((i: { wordId: string }) => i.wordId)).toEqual([book.wordId(word)]);
    expect(pv.body.data.sentences.map((s: { sentenceId: string; status: string }) => [s.sentenceId, s.status])).toEqual([[sent.frame, "learning"]]);
    const learning = await student.client.post("/sheets/preview", { format: "dictation", includeWords: false, includePhrases: false, sentenceSources: [{ kind: "learning" }] });
    expect(learning.body.data.sentences.map((s: { sentenceId: string }) => s.sentenceId)).toEqual([sent.frame]);
    // 别人的学习组不能用
    expect((await outsider.client.post("/sheets/preview", { format: "dictation", sentenceSources: [{ kind: "session", sessionId }] })).status).toBe(403);

    const created = await student.client.post("/sheets", {
      format: "dictation",
      items: [...pv.body.data.items.map((i: { type: string; wordId: string }) => ({ type: i.type, wordId: i.wordId })), ...pv.body.data.sentences.map((s: { type: string; sentenceId: string }) => ({ type: s.type, sentenceId: s.sentenceId }))],
    });
    expect(created.status, created.text).toBe(200);
    const d = await student.client.get(`/sheets/${created.body.data.id}`);
    expect(d.body.data.items.map((i: DictItem) => i.type)).toEqual(["word", "frame"]);
  });

  it("老师给学生出题并批改：不是自批；自测单不需要批改", async () => {
    const created = await teacher.client.post("/sheets", { userId: student.id, format: "dictation", items: [{ type: "phrase", wordId: book.wordId(phrase) }] });
    expect(created.status, created.text).toBe(200);
    const id = created.body.data.id as string;
    const g = await teacher.client.post(`/sheets/${id}/grade`, { results: [{ index: 0, correct: true }] });
    expect(g.status, g.text).toBe(200);
    expect(g.body.data.grading).toMatchObject({ selfGraded: false, gradedBy: { id: teacher.id }, correct: 1, total: 1 });

    const selftest = await student.client.post("/sheets", { wordIds: [book.wordId(word)] });
    expect(selftest.status).toBe(200);
    const st = await student.client.post(`/sheets/${selftest.body.data.id}/grade`, { results: [{ index: 0, correct: true }] });
    expect(st.status).toBe(400);
    expect(st.body.error).toMatchObject({ code: "INVALID_ACTION", message: "只有默写单需要批改" });
    const std = await student.client.get(`/sheets/${selftest.body.data.id}`);
    expect(std.body.data).toMatchObject({ format: "selftest", items: [], grading: null });
  });
});

/** 学生本人自批计入班级概览的目标覆盖（spec 0006 §5）；同一学习日老师再出一份复核不计（spec 0009 同日只认第一次作答），隔天复核后比例下降由 Go 单测覆盖 */
describe.runIf(goOnly)("默写单：班级概览的自批比例", () => {
  let teacher: Account, student: Account;
  let classId = "";
  let book: BuiltBook;
  const sp: string[] = [];

  beforeAll(async () => {
    const admin = await login("admin@vinx.test");
    teacher = await newTeacher(admin, "dictcov");
    const cls = await newClassWithStudent(teacher.client, "自批");
    classId = cls.classId;
    student = cls.student;
    const tag = uniq();
    sp.push(`vxdcalpha${tag}`, `vxdcbravo${tag}`);
    book = await importBook(teacher.client, "自批词书", [{ name: "Unit 1", entries: sp.map((s, i) => ({ spelling: s, definition: `自批义${i}` })) }]);
    expect((await teacher.client.put(`/classes/${classId}/target-books`, { bookIds: [book.bookId] })).status).toBe(200);
  });

  async function coverageOf(c: Client = teacher.client) {
    const r = await c.get(`/classes/${classId}/overview`);
    expect(r.status).toBe(200);
    return (r.body.data.students as { userId: string; coverage: Record<string, unknown> }[]).find((s) => s.userId === student.id)!.coverage;
  }

  async function dictate(by: Client, userId: string | undefined, spellings: string[], correct: (s: string) => boolean) {
    const created = await by.post("/sheets", { ...(userId ? { userId } : {}), format: "dictation", items: spellings.map((s) => ({ type: "word", wordId: book.wordId(s) })) });
    expect(created.status, created.text).toBe(200);
    const d = await by.get(`/sheets/${created.body.data.id}`);
    const g = await by.post(`/sheets/${created.body.data.id}/grade`, {
      results: (d.body.data.items as DictItem[]).map((i) => ({ index: i.index, correct: correct(spellings.find((s) => book.wordId(s) === i.wordId)!) })),
    });
    expect(g.status, g.text).toBe(200);
  }

  it("自批的词数与比例：学生自批后 100%，老师当天复核不改变", async () => {
    expect(await coverageOf()).toEqual({ target: 2, tested: 0, learning: 0, selfGraded: 0, selfGradedRatio: null });
    await dictate(student.client, undefined, sp, (s) => s === sp[0]);
    expect(await coverageOf()).toEqual({ target: 2, tested: 2, learning: 1, selfGraded: 2, selfGradedRatio: 1 });
    // 覆盖进度里答错的词变成「没记住」
    const words = await student.client.get("/records/coverage/words?status=learning");
    expect(words.body.data.items.map((w: { wordId: string }) => w.wordId)).toEqual([book.wordId(sp[1])]);
    await dictate(teacher.client, student.id, [sp[0]], () => true);
    expect(await coverageOf()).toEqual({ target: 2, tested: 2, learning: 1, selfGraded: 2, selfGradedRatio: 1 });
  });
});
