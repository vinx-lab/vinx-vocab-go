/**
 * 内容模型 词 ← 句 ← 篇（spec 0004，只在 Go 上跑）：单元的篇（/units/:id/texts、/texts/:id）的增删改查与排序、
 * 词书导入与单元内粘贴导入带句型 / 课文、/words/:id/sentences、/sentences/analyze，以及权限：
 * 别人的单元内容不能改（403），看不到的词书 404；班级学生只读老师词书里的篇。
 *
 * 词库拼写全局唯一（K16）：句子里只用本用例造的词和一个词库里不存在的词，关联、超纲、词库外的断言才稳定。
 */
import { beforeAll, describe, expect, it } from "vitest";
import { anon, login, type Client } from "../lib/client";
import { goOnly } from "../lib/areas";
import { importBook, newClassWithStudent, newTeacher, uniq, type Account, type BuiltBook } from "../lib/fixtures";

interface SentenceView {
  id: string;
  en: string;
  cn: string;
  frame: string | null;
  paragraph: number;
  source: string;
  words: { wordId: string; position: number; form: string }[];
}

interface TextView {
  id: string;
  unitId: string;
  kind: string;
  title: string;
  titleCn: string | null;
  sortOrder: number;
  sentences: SentenceView[];
}

describe.runIf(goOnly)("句与篇：单元的篇、导入、词 → 句、分析", () => {
  let teacher: Account, other: Account, student: Account;
  let book: BuiltBook;
  let u1 = "", u2 = "";
  const w: Record<"a" | "b" | "c" | "d", string> = { a: "", b: "", c: "", d: "" };
  let nonsense = "";

  beforeAll(async () => {
    const admin = await login("admin@vinx.test");
    teacher = await newTeacher(admin, "sent");
    other = await newTeacher(admin, "sent2");
    student = (await newClassWithStudent(teacher.client, "句子")).student;
    const tag = uniq();
    w.a = `vxsalpha${tag}`;
    w.b = `vxsbravo${tag}`;
    w.c = `vxscharlie${tag}`;
    w.d = `vxsdelta${tag}`;
    nonsense = `zqxvnope${tag}`;
    book = await importBook(teacher.client, "句子词书", [
      {
        name: "Unit 1",
        entries: [
          { spelling: w.a, definition: "甲", partOfSpeech: "n." },
          { spelling: w.b, definition: "乙", partOfSpeech: "v." },
        ],
        texts: [
          { title: "重点句型", kind: "list", sentences: [{ en: `${w.a} ${w.b} it.`, cn: "甲乙它。", frame: `${w.a} ... it.` }] },
          {
            title: "Story",
            titleCn: "故事",
            kind: "text",
            sentences: [
              { en: `The ${w.a} is here.`, cn: "甲在这里。" },
              { en: `We ${w.b} again.`, cn: "我们又乙了。", paragraph: 1 },
            ],
          },
        ],
      },
      {
        name: "Unit 2",
        entries: [
          { spelling: w.c, definition: "丙" },
          { spelling: w.d, definition: "丁" },
        ],
      },
    ]);
    u1 = book.units[0].id;
    u2 = book.units[1].id;
  });

  it("词书导入带句型 / 课文：单元里按顺序列出篇，句子带骨架、段落、关联到的词", async () => {
    const r = await teacher.client.get(`/units/${u1}/texts`);
    expect(r.status).toBe(200);
    const items = r.body.data.items as TextView[];
    expect(items.map((t) => [t.kind, t.title, t.titleCn])).toEqual([
      ["list", "重点句型", null],
      ["text", "Story", "故事"],
    ]);
    const pattern = items[0].sentences[0];
    expect(pattern).toMatchObject({ en: `${w.a} ${w.b} it.`, cn: "甲乙它。", frame: `${w.a} ... it.`, source: "import" });
    expect(pattern.words.filter((x) => [book.wordId(w.a), book.wordId(w.b)].includes(x.wordId))).toEqual([
      { wordId: book.wordId(w.a), position: 0, form: w.a },
      { wordId: book.wordId(w.b), position: 1, form: w.b },
    ]);
    expect(items[1].sentences.map((s) => s.paragraph)).toEqual([0, 1]);
    // 单元 2 没有篇
    expect((await teacher.client.get(`/units/${u2}/texts`)).body.data.items).toEqual([]);
  });

  it("增删改查与排序：新建、改标题、整体替换句子（带 id 的保留）、排序、删除", async () => {
    const created = await teacher.client.post(`/units/${u2}/texts`, {
      title: "句型 2",
      kind: "list",
      sentences: [
        { en: `${w.c} first.`, cn: "丙第一。" },
        { en: `${w.d} second.`, cn: "丁第二。" },
      ],
    });
    expect(created.status, created.text).toBe(200);
    const t1 = created.body.data as TextView;
    expect(t1).toMatchObject({ unitId: u2, kind: "list", title: "句型 2", titleCn: null, sortOrder: 0 });
    expect(t1.sentences.map((s) => s.source)).toEqual(["manual", "manual"]);

    const t2 = (await teacher.client.post(`/units/${u2}/texts`, { title: "课文 2", sentences: [{ en: `${w.c} story.`, cn: "丙的故事。" }] })).body.data as TextView;
    expect(t2).toMatchObject({ kind: "text", sortOrder: 1 });

    const patched = await teacher.client.patch(`/texts/${t1.id}`, { title: "改过的句型", titleCn: "句型" });
    expect(patched.status).toBe(200);
    expect(patched.body.data).toMatchObject({ title: "改过的句型", titleCn: "句型", kind: "list" });

    const keep = t1.sentences[1];
    const replaced = await teacher.client.put(`/texts/${t1.id}/sentences`, {
      sentences: [
        { en: `${w.d} new.`, cn: "丁新的。" },
        { id: keep.id, en: `${w.d} second again.`, cn: "丁又第二。" },
      ],
    });
    expect(replaced.status, replaced.text).toBe(200);
    const rs = replaced.body.data.sentences as SentenceView[];
    expect(rs.map((s) => s.en)).toEqual([`${w.d} new.`, `${w.d} second again.`]);
    expect(rs[1].id).toBe(keep.id);
    expect(rs[1].words.some((x) => x.wordId === book.wordId(w.d))).toBe(true);
    // 别的篇里的句子 id 不能借用
    expect((await teacher.client.put(`/texts/${t1.id}/sentences`, { sentences: [{ id: t2.sentences[0].id, en: "Hijack.", cn: "劫持。" }] })).status).toBe(400);

    const order = await teacher.client.patch(`/units/${u2}/texts/order`, { textIds: [t2.id, t1.id] });
    expect(order.status).toBe(200);
    expect(order.body.data).toEqual({ ordered: 2 });
    expect(((await teacher.client.get(`/units/${u2}/texts`)).body.data.items as TextView[]).map((t) => t.id)).toEqual([t2.id, t1.id]);
    expect((await teacher.client.patch(`/units/${u2}/texts/order`, { textIds: [t1.id] })).status).toBe(400);

    expect((await teacher.client.del(`/texts/${t2.id}`)).status).toBe(200);
    const gone = await teacher.client.patch(`/texts/${t2.id}`, { title: "x" });
    expect(gone.status).toBe(404);
    expect(gone.body.error).toMatchObject({ code: "NOT_FOUND", message: "内容不存在" });
    expect(((await teacher.client.get(`/units/${u2}/texts`)).body.data.items as TextView[]).map((t) => t.id)).toEqual([t1.id]);
  });

  it("校验：标题必填、英文不能为空、kind 只能是 text / list；单元不存在 404", async () => {
    const bad = await teacher.client.post(`/units/${u2}/texts`, { kind: "poem", sentences: [{ en: "", cn: "x" }] });
    expect(bad.status).toBe(400);
    expect(bad.body.error.details).toMatchObject({ title: "Required", "sentences.0.en": "英文不能为空" });
    expect(bad.body.error.details.kind).toBeDefined();
    const missing = await teacher.client.post(`/units/no-such-unit/texts`, { title: "x", sentences: [] });
    expect(missing.status).toBe(404);
    expect(missing.body.error.message).toBe("单元不存在");
  });

  it("单元内粘贴导入：预览给出篇、句子、超纲词与统计；确认导入写入", async () => {
    const text = `[句型]\n${w.c} ... ${w.a}. | 丙……甲。 | ${w.c} likes ${w.a}.\n[课文]\nTitle: Pasted | 粘贴\n${w.c} one. | 丙一。\n\n${w.d} two. | 丁二。\n`;
    const pv = await teacher.client.post(`/units/${u1}/texts/import/preview`, { text });
    expect(pv.status, pv.text).toBe(200);
    const texts = pv.body.data.texts as { kind: string; title: string; sentences: { en: string; frame?: string | null; paragraph?: number; outOfScope: { spelling: string }[] }[] }[];
    expect(texts.map((t) => t.kind)).toEqual(["list", "text"]);
    expect(texts[0].sentences[0]).toMatchObject({ en: `${w.c} likes ${w.a}.`, frame: `${w.c} ... ${w.a}.` });
    // 单元 1 之前的词只有 a、b：c 在单元 2 → 超纲
    expect(texts[0].sentences[0].outOfScope.map((x) => x.spelling)).toContain(w.c);
    expect(texts[0].sentences[0].outOfScope.map((x) => x.spelling)).not.toContain(w.a);
    expect(texts[1]).toMatchObject({ title: "Pasted" });
    expect(pv.body.data.stats).toMatchObject({ texts: 2, sentences: 3, error: 0 });

    const before = ((await teacher.client.get(`/units/${u1}/texts`)).body.data.items as TextView[]).length;
    const imported = await teacher.client.post(`/units/${u1}/texts/import`, { texts: pv.body.data.texts });
    expect(imported.status, imported.text).toBe(200);
    expect(imported.body.data).toMatchObject({ texts: 2, sentences: 3 });
    const after = (await teacher.client.get(`/units/${u1}/texts`)).body.data.items as TextView[];
    expect(after).toHaveLength(before + 2);
    expect(after.at(-1)).toMatchObject({ title: "Pasted", titleCn: "粘贴", kind: "text" });
    expect(after.at(-1)!.sentences.map((s) => s.paragraph)).toEqual([0, 1]);
  });

  it("权限：班级学生只读；别的老师看不到（404）、不能改（403）；学生不能改；未登录 401", async () => {
    const texts = (await student.client.get(`/units/${u1}/texts`)).body.data.items as TextView[];
    expect(texts.length).toBeGreaterThanOrEqual(2);
    const textId = texts[0].id;

    const invisible = await other.client.get(`/units/${u1}/texts`);
    expect(invisible.status).toBe(404);
    expect(invisible.body.error.message).toBe("单元不存在");
    expect((await other.client.post(`/units/${u1}/texts`, { title: "x", sentences: [] })).status).toBe(403);
    expect((await other.client.patch(`/texts/${textId}`, { title: "x" })).status).toBe(403);
    expect((await other.client.put(`/texts/${textId}/sentences`, { sentences: [] })).status).toBe(403);
    expect((await other.client.del(`/texts/${textId}`)).status).toBe(403);
    expect((await other.client.post(`/units/${u1}/texts/import/preview`, { text: "[句型]\nA. | 甲。" })).status).toBe(403);

    expect((await student.client.post(`/units/${u1}/texts`, { title: "x", sentences: [] })).status).toBe(403);
    expect((await student.client.patch(`/texts/${textId}`, { title: "x" })).status).toBe(403);
    expect((await student.client.del(`/texts/${textId}`)).status).toBe(403);
    expect((await student.client.post("/sentences/analyze", { en: "Hi." })).status).toBe(403);

    expect((await anon().get(`/units/${u1}/texts`)).status).toBe(401);
  });

  it("/words/:id/sentences：按来源分组（例句、句型、课文、我的短文），带出处；看不到的词 404", async () => {
    const patch = await teacher.client.patch(`/words/${book.wordId(w.a)}`, { example: `My ${w.a} is new.`, exampleCn: "我的甲是新的。" });
    expect(patch.status, patch.text).toBe(200);

    const r = await student.client.get(`/words/${book.wordId(w.a)}/sentences`);
    expect(r.status, r.text).toBe(200);
    const d = r.body.data as Record<"examples" | "patterns" | "texts" | "passages", { en: string; from?: Record<string, unknown> }[]>;
    expect(d.examples.map((s) => s.en)).toEqual([`My ${w.a} is new.`]);
    expect(d.patterns.map((s) => s.en)).toContain(`${w.a} ${w.b} it.`);
    expect(d.texts.map((s) => s.en)).toContain(`The ${w.a} is here.`);
    expect(d.passages).toEqual([]);
    const from = d.patterns.find((s) => s.en === `${w.a} ${w.b} it.`)!.from!;
    expect(from).toMatchObject({ unitId: u1, bookId: book.bookId, title: "重点句型" });

    const hidden = await other.client.get(`/words/${book.wordId(w.a)}/sentences`);
    expect(hidden.status).toBe(404);
    expect(hidden.body.error.message).toBe("单词不存在");
    expect((await student.client.get(`/words/no-such-word/sentences`)).status).toBe(404);
  });

  it("/sentences/analyze：分词、关联到的词、超纲词（带 unitId 时按本册到当前单元为止）、词库外的词", async () => {
    const r = await teacher.client.post("/sentences/analyze", { en: `${w.a} ${w.c} ${nonsense}.`, unitId: u1 });
    expect(r.status, r.text).toBe(200);
    const d = r.body.data as { tokens: unknown[]; words: { spelling: string }[]; outOfScope: { spelling: string }[]; unknown: { text: string }[] };
    expect(d.words.map((x) => x.spelling)).toEqual([w.a, w.c]);
    expect(d.outOfScope.map((x) => x.spelling)).toEqual([w.c]);
    expect(d.unknown.map((x) => x.text)).toEqual([nonsense]);
    expect(d.tokens).toHaveLength(3);

    // 单元 2 之前包含 c
    const u2r = await teacher.client.post("/sentences/analyze", { en: `${w.a} ${w.c}.`, unitId: u2 });
    expect(u2r.body.data.outOfScope).toEqual([]);

    expect((await teacher.client.post("/sentences/analyze", { en: "" })).status).toBe(400);
    expect((await other.client.post("/sentences/analyze", { en: "Hi.", unitId: u1 })).status).toBe(404);
  });
});
