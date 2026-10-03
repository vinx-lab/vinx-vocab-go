/** spec 0004 前端纯函数：分词与高亮、分段、导入预览转请求体、单元内篇排序、编辑行与句子互转、词 → 句截断 */
import { describe, expect, it } from "vitest";
import {
  editRowsFromSentences,
  editRowsToInputs,
  groupParagraphs,
  highlightSegments,
  limitWordSentences,
  mergeKindOrder,
  moveItem,
  previewTextsPayload,
  tokenize,
} from "@/lib/sentences";
import type { PreviewSentence, Sentence, WordSentenceItem, WordSentences } from "@/types";

describe("tokenize（与后端 lemma.Tokenize 同口径）", () => {
  it("按空格和标点切开，保留词内撇号与连字符", () => {
    expect(tokenize("I don't like T-shirts, do you?").map((t) => t.text)).toEqual(["I", "don't", "like", "T-shirts", "do", "you"]);
    expect(tokenize("It's six o’clock.").map((t) => t.text)).toEqual(["It's", "six", "o’clock"]);
  });
  it("复数所有格 students' 带上撇号；句首引号不算词", () => {
    expect(tokenize("'The students' books' 3 times").map((t) => t.text)).toEqual(["The", "students'", "books'", "3", "times"]);
  });
  it("给出原文中的起止位置与序号", () => {
    const toks = tokenize("Hi, Tom!");
    expect(toks).toEqual([
      { text: "Hi", index: 0, start: 0, end: 2 },
      { text: "Tom", index: 1, start: 4, end: 7 },
    ]);
  });
  it("空串或纯标点没有词", () => {
    expect(tokenize("")).toEqual([]);
    expect(tokenize("... !?")).toEqual([]);
  });
});

describe("highlightSegments", () => {
  it("单词按 position 定位，原文的标点与空格保留", () => {
    const segs = highlightSegments("He went to school.", [{ wordId: "go", position: 1, form: "went" }], "go");
    expect(segs).toEqual([
      { text: "He " },
      { text: "went", wordId: "go", target: true },
      { text: " to school." },
    ]);
    expect(segs.map((s) => s.text).join("")).toBe("He went to school.");
  });
  it("短语按 form 的词数覆盖多个词；非目标词也标出 wordId", () => {
    const segs = highlightSegments("She is good at maths.", [
      { wordId: "bga", position: 1, form: "is good at" },
      { wordId: "maths", position: 4, form: "maths" },
    ]);
    expect(segs).toEqual([
      { text: "She " },
      { text: "is good at", wordId: "bga", target: false },
      { text: " " },
      { text: "maths", wordId: "maths", target: false },
      { text: "." },
    ]);
  });
  it("重叠或越界的关联跳过，按位置排序", () => {
    const segs = highlightSegments("I like it", [
      { wordId: "it", position: 2, form: "it" },
      { wordId: "x", position: 9, form: "x" },
      { wordId: "like-it", position: 1, form: "like it" },
    ]);
    expect(segs).toEqual([{ text: "I " }, { text: "like it", wordId: "like-it", target: false }]);
  });
  it("没有关联时整句一段", () => {
    expect(highlightSegments("Hello!", [])).toEqual([{ text: "Hello!" }]);
  });
});

describe("groupParagraphs", () => {
  it("相邻的同一段号归为一段", () => {
    const s = (id: string, paragraph: number) => ({ id, paragraph });
    expect(groupParagraphs([s("a", 0), s("b", 0), s("c", 1), s("d", 3)]).map((g) => g.map((x) => x.id))).toEqual([["a", "b"], ["c"], ["d"]]);
    expect(groupParagraphs([])).toEqual([]);
  });
});

const ps = (en: string, cn: string, extra: Partial<PreviewSentence & { include: boolean }> = {}): PreviewSentence & { include: boolean } => ({
  en,
  cn,
  frame: "",
  paragraph: 0,
  status: "ok",
  issues: [],
  raw: en,
  line: 1,
  words: [],
  outOfScope: [],
  unknown: [],
  include: true,
  ...extra,
});

describe("previewTextsPayload", () => {
  it("只带勾选且中英文都有的句子；空骨架省略；没有句子的篇去掉；标题为空时用默认标题", () => {
    const out = previewTextsPayload([
      {
        kind: "list",
        title: " ",
        titleCn: "",
        line: 1,
        sentences: [
          ps("How do you learn?", "你怎么学？"),
          ps("I find making cards useful.", "我发现……很有用。", { frame: "I find ... useful." }),
          ps("bad", "", { status: "error", include: false }),
          ps("  ", "空英文"),
        ],
      },
      { kind: "text", title: "Story", titleCn: "故事", line: 5, sentences: [ps("A.", "甲。", { include: false })] },
      { kind: "text", title: " My Day ", titleCn: " 我的一天 ", line: 9, sentences: [ps(" One. ", " 一。 "), ps("Two.", "二。", { paragraph: 1 })] },
    ]);
    expect(out).toEqual([
      {
        kind: "list",
        title: "重点句型",
        titleCn: null,
        sentences: [
          { en: "How do you learn?", cn: "你怎么学？", paragraph: 0 },
          { en: "I find making cards useful.", cn: "我发现……很有用。", frame: "I find ... useful.", paragraph: 0 },
        ],
      },
      {
        kind: "text",
        title: "My Day",
        titleCn: "我的一天",
        sentences: [
          { en: "One.", cn: "一。", paragraph: 0 },
          { en: "Two.", cn: "二。", paragraph: 1 },
        ],
      },
    ]);
  });
});

describe("mergeKindOrder", () => {
  it("只重排一种类型，另一种保持原位，结果包含全部篇", () => {
    const all = [
      { id: "l1", kind: "list" },
      { id: "t1", kind: "text" },
      { id: "l2", kind: "list" },
      { id: "t2", kind: "text" },
    ];
    expect(mergeKindOrder(all, "text", ["t2", "t1"])).toEqual(["l1", "t2", "l2", "t1"]);
    expect(mergeKindOrder(all, "list", ["l2", "l1"])).toEqual(["l2", "t1", "l1", "t2"]);
  });
});

describe("moveItem", () => {
  it("把一项移到目标位置，越界不变", () => {
    expect(moveItem(["a", "b", "c"], 0, 2)).toEqual(["b", "c", "a"]);
    expect(moveItem(["a", "b", "c"], 2, 0)).toEqual(["c", "a", "b"]);
    expect(moveItem(["a", "b", "c"], 1, 5)).toEqual(["a", "b", "c"]);
  });
});

const sent = (id: string, paragraph: number, frame: string | null = null): Sentence => ({ id, en: `${id} en`, cn: `${id} 中`, frame, source: "manual", paragraph, words: [] });

describe("编辑行与句子互转", () => {
  it("段号变化处标记「另起一段」，保存时重新编号；已有句子带 id，新行不带", () => {
    const rows = editRowsFromSentences([sent("a", 0), sent("b", 0), sent("c", 2, "c ...")]);
    expect(rows.map((r) => [r.id, r.newParagraph, r.frame])).toEqual([
      ["a", false, ""],
      ["b", false, ""],
      ["c", true, "c ..."],
    ]);
    rows.push({ key: "n1", en: " New. ", cn: " 新。 ", frame: "", newParagraph: true });
    expect(editRowsToInputs(rows, "text")).toEqual([
      { id: "a", en: "a en", cn: "a 中", frame: null, paragraph: 0 },
      { id: "b", en: "b en", cn: "b 中", frame: null, paragraph: 0 },
      { id: "c", en: "c en", cn: "c 中", frame: "c ...", paragraph: 1 },
      { en: "New.", cn: "新。", frame: null, paragraph: 2 },
    ]);
  });
  it("句型清单不分段，段号都是 0；第一行的「另起一段」不起作用", () => {
    const rows = [
      { key: "1", en: "A", cn: "甲", frame: "", newParagraph: true },
      { key: "2", en: "B", cn: "乙", frame: "B ...", newParagraph: true },
    ];
    expect(editRowsToInputs(rows, "list").map((s) => s.paragraph)).toEqual([0, 0]);
    expect(editRowsToInputs(rows, "text").map((s) => s.paragraph)).toEqual([0, 1]);
  });
});

const item = (id: string, type: WordSentenceItem["from"]["type"]): WordSentenceItem => ({ ...sent(id, 0), from: { type } });

describe("limitWordSentences", () => {
  const ws: WordSentences = {
    examples: [item("e1", "example")],
    patterns: [item("p1", "unitText"), item("p2", "unitText")],
    texts: [item("t1", "unitText"), item("t2", "unitText"), item("t3", "unitText")],
    passages: [item("m1", "passage")],
  };
  it("按 例句 → 课本句型 → 课文 → 我的短文 依次取，最多 limit 条，空组不出现", () => {
    const r = limitWordSentences(ws, 5);
    expect(r.total).toBe(7);
    expect(r.groups.map((g) => [g.label, g.items.map((x) => x.id)])).toEqual([
      ["例句", ["e1"]],
      ["课本句型", ["p1", "p2"]],
      ["课文", ["t1", "t2"]],
    ]);
  });
  it("limit 为 null 时全部显示", () => {
    const r = limitWordSentences({ ...ws, examples: [] }, null);
    expect(r.groups.map((g) => g.label)).toEqual(["课本句型", "课文", "我的短文"]);
    expect(r.groups.reduce((n, g) => n + g.items.length, 0)).toBe(6);
  });
});
