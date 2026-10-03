/** spec 0005 前端纯函数：检查标记的颜色、草稿保存请求体、重写一句的替换、粘贴例句计数与字节数 */
import { describe, expect, it } from "vitest";
import {
  applyRewrite,
  checkTone,
  countPastedOrigins,
  draftProblem,
  patternSaveBody,
  rewriteBody,
  utf8Bytes,
  variantSaveBody,
  withKeys,
} from "@/lib/aiDraft";
import type { SentenceChecks } from "@vinx/shared";

const checks = (p: Partial<SentenceChecks> = {}): SentenceChecks => ({
  words: 6,
  maxWords: 20,
  missingTargets: [],
  outOfScope: [],
  tooLong: false,
  similarity: null,
  structureDeviates: false,
  warning: false,
  error: false,
  issues: [],
  ...p,
});

describe("checkTone", () => {
  it.each([
    ["没有检查结果", undefined, "ok"],
    ["全部通过", checks(), "ok"],
    ["超纲标黄", checks({ warning: true, outOfScope: ["giraffe"] }), "warning"],
    ["目标词没用上标红（优先于黄）", checks({ warning: true, error: true }), "error"],
  ] as const)("%s", (_name, c, want) => {
    expect(checkTone(c)).toBe(want);
  });
});

describe("patternSaveBody", () => {
  const rows = withKeys([
    { en: "  I find making word cards useful. ", cn: " 我发现做单词卡很有用。", frame: " I find ... useful. ", checks: checks() },
    { en: "How do you learn English?", cn: "你怎样学英语？", frame: null, checks: checks() },
    { en: "By reading aloud.", cn: "通过朗读。", frame: "  ", checks: checks() },
  ]);

  it("新建：去掉首尾空白，空骨架不提交，带标题", () => {
    expect(patternSaveBody(rows, { title: " Unit 1 重点句型 " })).toEqual({
      title: "Unit 1 重点句型",
      sentences: [
        { en: "I find making word cards useful.", cn: "我发现做单词卡很有用。", frame: "I find ... useful." },
        { en: "How do you learn English?", cn: "你怎样学英语？" },
        { en: "By reading aloud.", cn: "通过朗读。" },
      ],
    });
  });

  it("追加到已有清单：带 textId，不带标题", () => {
    const body = patternSaveBody(rows.slice(1, 2), { textId: "t9", title: "忽略" });
    expect(body).toEqual({ textId: "t9", sentences: [{ en: "How do you learn English?", cn: "你怎样学英语？" }] });
  });
});

describe("variantSaveBody", () => {
  const rows = withKeys([
    { origin: 1, originId: "s1", originEn: "I find making word cards useful.", en: "I find reading aloud helpful.", cn: "我发现朗读很有帮助。", change: "replace" as const, note: "换了做法和形容词", checks: checks() },
    { origin: 2, originId: null, originEn: "He likes music.", en: "Does he like music?", cn: "他喜欢音乐吗？", change: "" as const, note: "", checks: checks() },
  ]);

  it("改造方式与说明分开提交（服务端拼成「替换：…」），粘贴的例句不带 originId", () => {
    expect(variantSaveBody("u1", rows, {})).toEqual({
      unitId: "u1",
      sentences: [
        { en: "I find reading aloud helpful.", cn: "我发现朗读很有帮助。", originId: "s1", variantNote: "换了做法和形容词", change: "replace" },
        { en: "Does he like music?", cn: "他喜欢音乐吗？" },
      ],
    });
  });

  it("追加时带 textId", () => {
    expect(variantSaveBody("u1", rows.slice(0, 1), { textId: "t2" }).textId).toBe("t2");
  });
});

describe("draftProblem", () => {
  it("没有句子、英文或中文为空时给出提示；都填好时为 null", () => {
    expect(draftProblem([])).toBe("至少保留一句");
    expect(draftProblem(withKeys([{ en: "Hi.", cn: " " }]))).toBe("第 1 句的英文和中文都不能为空");
    expect(draftProblem(withKeys([{ en: "Hi.", cn: "你好。" }, { en: "", cn: "再见。" }]))).toBe("第 2 句的英文和中文都不能为空");
    expect(draftProblem(withKeys([{ en: "Hi.", cn: "你好。" }]))).toBeNull();
  });
});

describe("applyRewrite / rewriteBody", () => {
  const row = withKeys([{ en: "I find making word cards extremely useful for memorizing.", cn: "旧", frame: "I find ... useful.", checks: checks({ warning: true, outOfScope: ["memorizing"], issues: ["超纲词：memorizing"] }) }])[0];

  it("只替换英文、中文和检查结果；返回没有骨架时保留原骨架", () => {
    const next = applyRewrite(row, { en: "I find word cards useful.", cn: "新", level: "junior", checks: checks() });
    expect(next).toEqual({ ...row, en: "I find word cards useful.", cn: "新", checks: checks() });
    expect(applyRewrite(row, { en: "x", cn: "y", frame: "I think ... useful.", level: "junior", checks: checks() }).frame).toBe("I think ... useful.");
  });

  it("把这句和标出的问题一起发出；仿写带原句", () => {
    expect(rewriteBody("pattern", row, "junior", "u1")).toEqual({
      kind: "pattern",
      en: row.en,
      cn: "旧",
      issues: ["超纲词：memorizing"],
      level: "junior",
      unitId: "u1",
    });
    const v = withKeys([{ origin: 1, originId: null, originEn: "He likes music.", en: "He likes art.", cn: "他喜欢美术。", change: "replace" as const, note: "", checks: checks() }])[0];
    expect(rewriteBody("variant", v, "primary", "u1")).toMatchObject({ kind: "variant", origin: "He likes music.", level: "primary" });
  });
});

describe("粘贴例句", () => {
  it("每行一句，没有英文的行不算（与服务端解析一致）", () => {
    expect(countPastedOrigins("I like it. | 我喜欢。\n\n  | 只有中文\nHe runs fast.\r\n")).toBe(2);
    expect(countPastedOrigins("")).toBe(0);
  });

  it("按 UTF-8 字节计算大小（中文 3 字节）", () => {
    expect(utf8Bytes("ab")).toBe(2);
    expect(utf8Bytes("中文")).toBe(6);
  });
});
