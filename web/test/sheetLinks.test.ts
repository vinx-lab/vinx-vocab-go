import { describe, expect, it } from "vitest";
import { batchSizes, parsePrintIds, printLink, regenerateLink } from "@/pages/sheets/links";

describe("用错词再出一张", () => {
  it("学生本人：生成给自己", () => {
    expect(regenerateLink({ id: "s1", isOwner: true, userId: "stu" })).toBe("/sheets/new?from=s1");
  });
  it("老师看学生成绩：生成给该学生，而不是老师自己", () => {
    expect(regenerateLink({ id: "s1", isOwner: false, userId: "stu" })).toBe("/sheets/new?from=s1&userId=stu");
  });
});

describe("合并打印链接", () => {
  it("printLink 按给定顺序拼 ids；parsePrintIds 去空去重", () => {
    expect(printLink(["a", "b", "c"])).toBe("/sheets/print?ids=a,b,c");
    expect(parsePrintIds("a,,b,a, c ")).toEqual(["a", "b", "c"]);
    expect(parsePrintIds(null)).toEqual([]);
  });
});

describe("batchSizes（与后端 splitSheets 同口径，给生成页预告每份词数）", () => {
  it("够数时每份满额；不够时尽量平均、前面多一个；超出返回 null", () => {
    expect(batchSizes(120, 4, 30)).toEqual([30, 30, 30, 30]);
    expect(batchSizes(100, 4, 30)).toEqual([25, 25, 25, 25]);
    expect(batchSizes(10, 3, 30)).toEqual([4, 3, 3]);
    expect(batchSizes(0, 3, 30)).toEqual([]);
    expect(batchSizes(31, 1, 30)).toBeNull();
  });
});
