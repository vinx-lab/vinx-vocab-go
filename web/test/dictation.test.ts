/** 默写单（spec 0006）前端的纯函数：切分预告、书写区分档、版式分页、批改提交、链接与标签 */
import { describe, expect, it } from "vitest";
import {
  answerPages,
  defaultLineStyle,
  dictationScore,
  gradeLink,
  gradePayload,
  layoutQuestionPages,
  numberItems,
  parseLineStyle,
  printLinkWithLines,
  regenerateDictationLink,
  selfGradedText,
  sentenceSourcesOf,
  sheetStatusLabel,
  splitDictation,
  todaySheetTitle,
  writeTier,
  type NumberedItem,
} from "@/pages/sheets/dictation";
import type { DictItemType, DictItemView } from "@/types";

const LIM = { words: 20, phrases: 10, sentences: 8 };

function refs(type: DictItemType, n: number, prefix = type) {
  const sentence = type === "sentence" || type === "frame" || type === "transform";
  return Array.from({ length: n }, (_, i) => (sentence ? { type, sentenceId: `${prefix}${i}` } : { type, wordId: `${prefix}${i}` }));
}

describe("splitDictation（与后端 core.SplitDictation 同口径）", () => {
  it("三组各自平均分到各份，前面的份多一题，份内按卷面分区排序", () => {
    const items = [...refs("sentence", 3), ...refs("word", 5), ...refs("phrase", 2)];
    const r = splitDictation(items, 2, LIM);
    if ("error" in r) throw new Error(r.error);
    expect(r.groups.map((g) => g.map((x) => x.type[0]).join(""))).toEqual(["wwwpss", "wwps"]);
  });

  it("份数不超过三组里最多的题数；没有题时为空", () => {
    const r = splitDictation(refs("word", 2), 5, LIM);
    expect("groups" in r && r.groups.length).toBe(2);
    const empty = splitDictation([], 3, LIM);
    expect("groups" in empty && empty.groups).toEqual([]);
  });

  it("同一个词或句子只出一次（按顺序去重）", () => {
    const r = splitDictation([{ type: "word", wordId: "a" }, { type: "word", wordId: "a" }, { type: "sentence", sentenceId: "a" }], 1, LIM);
    expect("groups" in r && r.groups[0].length).toBe(2);
  });

  it("某组超过 份数 × 每份上限 时报错，不静默丢题", () => {
    const r = splitDictation(refs("sentence", 17), 2, LIM);
    expect(r).toEqual({ error: "句子 17 题，超过 2 份 × 8 题" });
    const w = splitDictation(refs("word", 21), 1, LIM);
    expect("error" in w && w.error).toBe("单词 21 题，超过 1 份 × 20 题");
  });
});

describe("sentenceSourcesOf（生成页的句子来源 → 接口参数）", () => {
  it("单元的篇可多选，加上没记住的句子和一篇 AI 短文", () => {
    expect(sentenceSourcesOf({ textIds: ["t1", "t2"], learning: true, passageId: "p1" })).toEqual([
      { kind: "text", textId: "t1" },
      { kind: "text", textId: "t2" },
      { kind: "learning" },
      { kind: "passage", passageId: "p1" },
    ]);
    expect(sentenceSourcesOf({ textIds: [], learning: false })).toEqual([]);
  });
  it("用错题再出一份：只用那次批改的错句", () => {
    expect(sentenceSourcesOf({ from: "g1", textIds: ["t1"], learning: true })).toEqual([{ kind: "session", sessionId: "g1" }]);
  });
});

describe("书写线与书写区分档", () => {
  it("小学学段默认四线三格，其他学段（含未设置）默认横线", () => {
    expect(defaultLineStyle("primary")).toBe("fourline");
    expect(defaultLineStyle("junior")).toBe("line");
    expect(defaultLineStyle(null)).toBe("line");
    expect(parseLineStyle("fourline")).toBe("fourline");
    expect(parseLineStyle("line")).toBe("line");
    expect(parseLineStyle("x")).toBeUndefined();
    expect(parseLineStyle(null)).toBeUndefined();
  });

  it.each([
    ["word", "apple", "line", "word"],
    ["word", "responsibility", "line", "phrase"],
    ["word", "a very long hyphenated-word entry", "line", "line"],
    ["phrase", "read aloud", "line", "phrase"],
    ["phrase", "take part in the school activities", "line", "line"],
    ["sentence", "I find taking notes useful.", "line", "line"],
    ["sentence", "My grandmother usually reads newspapers in the garden after breakfast.", "line", "double"],
    ["sentence", "We often play football together after school.", "line", "line"],
    ["sentence", "We often play football together after school.", "fourline", "double"],
    ["transform", "Do you like apples?", "fourline", "line"],
  ] as const)("%s「%s」%s → %s", (type, answer, style, tier) => {
    expect(writeTier(type, answer, style)).toBe(tier);
  });
});

function view(type: DictItemType, i: number, answer = `${type}${i}`, prompt = `提示${i}`): DictItemView {
  const section = { word: 1, phrase: 2, sentence: 3, frame: 4, transform: 4 }[type];
  return { index: i, type, section, prompt, answer };
}

function sheet(nWords: number, nPhrases: number, nSentences: number, sentence = "I find taking notes useful."): DictItemView[] {
  const out: DictItemView[] = [];
  let i = 0;
  for (let k = 0; k < nSentences; k++) out.push(view("sentence", i++, sentence, "我发现记笔记很有用。"));
  for (let k = 0; k < nWords; k++) out.push(view("word", i++, "apple", "n. 苹果"));
  for (let k = 0; k < nPhrases; k++) out.push(view("phrase", i++, "read aloud", "大声朗读"));
  return out;
}

describe("numberItems", () => {
  it("按卷面分区稳定排序后连续编号（题号从 1 开始）", () => {
    const n = numberItems(sheet(2, 1, 1));
    expect(n.map((x) => `${x.no}${x.type[0]}`)).toEqual(["1w", "2w", "3p", "4s"]);
    expect(n[3].index).toBe(0);
  });
});

describe("layoutQuestionPages（题目页分页，按高度预算，防止 overflow:hidden 截掉题目）", () => {
  const blocks = (pages: ReturnType<typeof layoutQuestionPages>) => pages.flatMap((p) => p.blocks);
  const numbers = (pages: ReturnType<typeof layoutQuestionPages>) =>
    blocks(pages).flatMap((b) => (b.kind === "row" ? b.items.map((x) => x.no) : b.kind === "sentence" ? [b.item.no] : []));

  it("默认题量（单词 20、短语 10、句子 8）横线一页排下；分区标题只显示有题的部分", () => {
    const pages = layoutQuestionPages(numberItems(sheet(20, 10, 8)), "line");
    expect(pages).toHaveLength(1);
    expect(blocks(pages).filter((b) => b.kind === "title").map((b) => b.kind === "title" && b.title)).toEqual(["一、单词", "二、短语", "三、句子"]);
    expect(numbers(pages)).toEqual(Array.from({ length: 38 }, (_, i) => i + 1));
  });

  it("单词、短语两题一行（左右两栏）；太长的占整行", () => {
    const items = numberItems([view("word", 0, "apple"), view("word", 1, "a very long hyphenated-word entry"), view("word", 2, "pear"), view("word", 3, "plum")]);
    const rows = blocks(layoutQuestionPages(items, "line")).filter((b) => b.kind === "row");
    expect(rows.map((b) => b.kind === "row" && b.items.map((x) => x.no))).toEqual([[1], [2], [3, 4]]);
    expect(rows.map((b) => b.kind === "row" && b.full)).toEqual([false, true, false]);
  });

  it("四线三格行距加大：同样的题量分成多页，续页的分区标题标「续」，每一题都在且只出现一次", () => {
    const items = numberItems(sheet(20, 10, 8));
    const pages = layoutQuestionPages(items, "fourline");
    expect(pages.length).toBeGreaterThan(1);
    expect(numbers(pages)).toEqual(Array.from({ length: 38 }, (_, i) => i + 1));
    const conts = blocks(pages).filter((b) => b.kind === "title" && b.cont);
    expect(conts.length).toBeGreaterThan(0);
    for (const p of pages) {
      expect(p.blocks[0].kind).toBe("title"); // 每页从分区标题开始
      expect(p.blocks[p.blocks.length - 1].kind).not.toBe("title"); // 标题不落在页尾
      expect(p.height).toBeLessThanOrEqual(p.budget);
    }
  });

  it("长句占两行书写区", () => {
    const long = "My grandmother usually reads newspapers in the garden after breakfast.";
    const b = blocks(layoutQuestionPages(numberItems([view("sentence", 0, long)]), "line")).find((x) => x.kind === "sentence");
    expect(b && b.kind === "sentence" && b.tier).toBe("double");
  });
});

describe("answerPages（答案页两栏，按估算高度分页）", () => {
  it("默认题量一页；题很多、句子很长时分页且不丢题", () => {
    expect(answerPages(numberItems(sheet(20, 10, 8)))).toHaveLength(1);
    const long = "My grandmother usually reads newspapers in the garden after breakfast every single morning of the week.";
    const many: NumberedItem[] = numberItems(sheet(30, 30, 20, long).concat(sheet(0, 0, 60, long).map((v, i) => ({ ...v, index: 100 + i }))));
    const pages = answerPages(many);
    expect(pages.length).toBeGreaterThan(1);
    expect(pages.flat().length).toBe(many.length);
  });
});

describe("gradePayload（批改提交：覆盖明细里的全部题目下标）", () => {
  it("默认全对；标错的题 correct=false 并带上记下的内容（去掉首尾空白，空的不带）", () => {
    const items = [{ index: 0 }, { index: 2 }, { index: 5 }];
    expect(gradePayload(items, new Set([2, 5]), { 2: "  aple ", 5: "  ", 0: "忽略：对的题不带" })).toEqual([
      { index: 0, correct: true },
      { index: 2, correct: false, userAnswer: "aple" },
      { index: 5, correct: false },
    ]);
  });
});

describe("链接与标签", () => {
  it("批改页链接", () => {
    expect(gradeLink("s1")).toBe("/sheets/s1/grade");
  });

  it("用错题再出一份：默写单格式，老师代学生出时带 userId", () => {
    expect(regenerateDictationLink({ sessionId: "g1", isOwner: true, userId: "stu" })).toBe("/sheets/new?format=dictation&from=g1");
    expect(regenerateDictationLink({ sessionId: "g1", isOwner: false, userId: "stu" })).toBe("/sheets/new?format=dictation&from=g1&userId=stu");
  });

  it("生成后打印：带上线型（横线不带参数）", () => {
    expect(printLinkWithLines("/sheets/a/print", "fourline")).toBe("/sheets/a/print?lines=fourline");
    expect(printLinkWithLines("/sheets/print?ids=a,b", "fourline")).toBe("/sheets/print?ids=a,b&lines=fourline");
    expect(printLinkWithLines("/sheets/a/print", "line")).toBe("/sheets/a/print");
  });

  it("列表状态：默写单显示待批改 / 已批改，自测表照旧", () => {
    expect(sheetStatusLabel("dictation", "pending")).toBe("待批改");
    expect(sheetStatusLabel("dictation", "tested")).toBe("已批改");
    expect(sheetStatusLabel("selftest", "pending")).toBe("待测试");
    expect(sheetStatusLabel(undefined, "testing")).toBe("测试中");
  });

  it("默写成绩：词的首答加上句子题", () => {
    expect(dictationScore({ correctFirst: 18, totalFirst: 20, sentences: { total: 8, correct: 5, wrongSentenceIds: [] } })).toEqual({ correct: 23, total: 28 });
    expect(dictationScore({ correctFirst: 3, totalFirst: 4 })).toEqual({ correct: 3, total: 4 });
  });

  it("今日页入口：默写单显示待批改与题数，不显示测试", () => {
    expect(todaySheetTitle({ seq: 4, wordCount: 25, format: "dictation", itemCount: 33 })).toBe("默写单 #4 · 待批改 · 33 题");
    expect(todaySheetTitle({ seq: 2, wordCount: 30, format: "selftest", itemCount: 30 })).toBe("单词单 #2 · 30 词");
    expect(todaySheetTitle({ seq: 2, wordCount: 30 })).toBe("单词单 #2 · 30 词");
  });

  it("班级概览的自批比例：没有已测或没有自批时不显示", () => {
    expect(selfGradedText({ selfGraded: 3, selfGradedRatio: 0.25 })).toBe("自批 25%");
    expect(selfGradedText({ selfGraded: 0, selfGradedRatio: 0 })).toBeNull();
    expect(selfGradedText({ selfGradedRatio: null })).toBeNull();
    expect(selfGradedText(null)).toBeNull();
  });
});
