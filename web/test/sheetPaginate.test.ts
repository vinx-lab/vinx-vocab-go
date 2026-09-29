import { describe, expect, it } from "vitest";
import { paginate, ROWS_PER_PAGE } from "@/pages/sheets/paginate";

const n = (k: number) => Array.from({ length: k }, (_, i) => i);

describe("单词单打印分页", () => {
  it("每页 30 条", () => expect(ROWS_PER_PAGE).toBe(30));
  it("30 条一页", () => expect(paginate(n(30)).map((p) => p.rows.length)).toEqual([30]));
  it("31 条两页，第二页从 30 开始编号", () => {
    const pages = paginate(n(31));
    expect(pages.map((p) => p.rows.length)).toEqual([30, 1]);
    expect(pages[1].start).toBe(30);
  });
  it("60 条两页、61 条三页", () => {
    expect(paginate(n(60)).map((p) => p.rows.length)).toEqual([30, 30]);
    expect(paginate(n(61)).map((p) => p.rows.length)).toEqual([30, 30, 1]);
  });
  it("空列表没有页", () => expect(paginate([])).toEqual([]));
});
