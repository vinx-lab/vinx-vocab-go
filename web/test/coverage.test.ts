import { describe, expect, it } from "vitest";
import { coverageRateOrder, moveItem, parseTargetStatus, targetSheetLink, testedRate } from "@/pages/coverage/coverage";

describe("已测比例", () => {
  it("已测 / 应测；没有目标词时为 null", () => {
    expect(testedRate({ target: 3700, tested: 2800 })).toBeCloseTo(2800 / 3700);
    expect(testedRate({ target: 0, tested: 0 })).toBeNull();
    expect(testedRate(null)).toBeNull();
  });

  it("概览排序：没有目标的排在 0% 之前", () => {
    const rows = [{ coverage: { target: 10, tested: 5, learning: 1 } }, { coverage: null }, { coverage: { target: 10, tested: 0, learning: 0 } }];
    const sorted = [...rows].sort((a, b) => coverageRateOrder(a.coverage) - coverageRateOrder(b.coverage));
    expect(sorted.map((r) => r.coverage?.tested ?? null)).toEqual([null, 0, 5]);
  });
});

describe("目标来源的单词单链接", () => {
  it("本人：只带来源；老师给学生出：带 userId；只出某本书：带 bookId", () => {
    expect(targetSheetLink("untested")).toBe("/sheets/new?target=untested");
    expect(targetSheetLink("learning", { userId: "stu" })).toBe("/sheets/new?target=learning&userId=stu");
    expect(targetSheetLink("untested", { userId: "stu", bookId: "b1" })).toBe("/sheets/new?target=untested&userId=stu&bookId=b1");
  });

  it("解析 target 参数：只认 untested / learning", () => {
    expect(parseTargetStatus("untested")).toBe("untested");
    expect(parseTargetStatus("learning")).toBe("learning");
    expect(parseTargetStatus("known")).toBeNull();
    expect(parseTargetStatus(null)).toBeNull();
  });
});

describe("目标词书排序", () => {
  it("moveItem 把一项移到新位置，越界不变", () => {
    expect(moveItem(["a", "b", "c"], 0, 2)).toEqual(["b", "c", "a"]);
    expect(moveItem(["a", "b", "c"], 2, 0)).toEqual(["c", "a", "b"]);
    expect(moveItem(["a", "b", "c"], 1, 1)).toEqual(["a", "b", "c"]);
    expect(moveItem(["a", "b", "c"], 0, -1)).toEqual(["a", "b", "c"]);
    expect(moveItem(["a", "b", "c"], 2, 3)).toEqual(["a", "b", "c"]);
  });
});
