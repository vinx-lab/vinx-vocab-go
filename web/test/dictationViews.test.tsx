/** 默写单（spec 0006）的打印版式与列表入口 */
import { afterEach, describe, expect, it, vi } from "vitest";
import { render } from "preact";
import { act } from "preact/test-utils";
import { LocationProvider } from "preact-iso";
import { clearCache } from "@/lib/query";
import { DictationPages } from "@/pages/sheets/SheetPrintPage";
import { SheetsList } from "@/pages/sheets/SheetsList";
import type { SheetDetail, SheetListItem } from "@/types";

const wait = (ms = 40) => act(async () => { await new Promise((r) => setTimeout(r, ms)); });
const mounted: HTMLElement[] = [];
const mount = async (vnode: preact.VNode) => {
  const el = document.createElement("div");
  document.body.appendChild(el);
  mounted.push(el);
  await act(() => render(vnode, el));
  return el;
};

afterEach(async () => {
  for (const el of mounted.splice(0)) await act(() => render(null, el));
  vi.unstubAllGlobals();
  clearCache();
  document.body.innerHTML = "";
});

const sheet: SheetDetail = {
  id: "sh1",
  seq: 7,
  createdAt: "2026-10-01T00:00:00Z",
  student: { id: "stu", name: "小明" },
  modes: [],
  words: [],
  format: "dictation",
  items: [
    { index: 0, type: "sentence", section: 3, sentenceId: "s1", prompt: "我发现记笔记很有用。", answer: "I find taking notes useful.", cn: "我发现记笔记很有用。" },
    { index: 1, type: "word", section: 1, wordId: "w1", prompt: "n. 苹果", answer: "apple" },
    { index: 2, type: "transform", section: 4, sentenceId: "s2", prompt: "I like apples.（改成一般疑问句）", answer: "Do you like apples?" },
  ],
  grading: null,
};

describe("DictationPages", () => {
  it("题目页：页眉、得分栏、按分区连续编号、没有折线；答案页另起一页", async () => {
    const el = await mount(<DictationPages sheet={sheet} style="line" />);
    const pages = el.querySelectorAll("section.vx-sheet-page");
    expect(pages.length).toBe(2);
    const [q, a] = [...pages];
    expect(q.classList.contains("vx-dict-page")).toBe(true);
    expect(q.querySelector(".vx-dict-head")!.textContent).toContain("VinxVocab 默写单 #7");
    expect(q.textContent).toContain("小明");
    expect(q.textContent).toMatch(/得分\s*\/\s*3/);
    expect([...q.querySelectorAll(".vx-dict-title")].map((t) => t.textContent)).toEqual(["一、单词", "三、句子", "四、仿写与转换"]);
    expect([...q.querySelectorAll(".vx-dict-no")].map((t) => t.textContent)).toEqual(["1.", "2.", "3."]);
    // 题目页不出现答案（转换题的原句是提示的一部分）
    expect(q.textContent).not.toContain("I find taking notes useful.");
    expect(q.textContent).not.toContain("Do you like apples?");
    expect(q.textContent).toContain("I like apples.（改成一般疑问句）");
    expect(a.classList.contains("vx-dict-answer-page")).toBe(true);
    expect(a.textContent).toContain("答案（家长 / 老师批改用）");
    expect([...a.querySelectorAll(".vx-dict-answer")].map((x) => x.textContent)).toEqual(["1.apple", "2.I find taking notes useful.", "3.Do you like apples?"]);
  });

  it("四线三格：题目页带四线三格的样式类，行高变量加大", async () => {
    const el = await mount(<DictationPages sheet={sheet} style="fourline" />);
    const q = el.querySelector("section.vx-dict-page") as HTMLElement;
    expect(q.classList.contains("vx-dict-lines-fourline")).toBe(true);
    expect(q.style.getPropertyValue("--dict-row")).toBe("13mm");
  });
});

describe("SheetsList · 默写单", () => {
  it("显示默写标签与待批改 / 已批改、自批；入口是批改和查看成绩，没有在线测试", async () => {
    const items: SheetListItem[] = [
      { id: "a", seq: 2, wordCount: 20, itemCount: 30, createdAt: "2026-10-01T00:00:00Z", creatorName: "老师", status: "pending", firstResult: null, activeSessionId: null, format: "dictation" },
      { id: "b", seq: 1, wordCount: 20, itemCount: 28, createdAt: "2026-09-30T00:00:00Z", creatorName: "老师", status: "tested", firstResult: { sessionId: "g1", correct: 25, total: 28 }, activeSessionId: null, format: "dictation", selfGraded: true },
    ];
    vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ success: true, data: { items, total: 2 } }), { status: 200 })));
    const el = await mount(
      <LocationProvider>
        <SheetsList userId="stu" />
      </LocationProvider>,
    );
    await wait();
    expect(el.textContent).toContain("默写单 #2");
    expect(el.textContent).toContain("待批改");
    expect(el.textContent).toContain("已批改");
    expect(el.textContent).toContain("自批");
    expect(el.textContent).toContain("30 题");
    expect(el.textContent).toContain("成绩 25/28");
    expect(el.textContent).not.toMatch(/开始测试|再测一次/);
    expect(el.querySelector('a[href="/sheets/a/grade"]')!.textContent).toMatch(/批\s*改/);
    expect(el.querySelector('a[href="/sheets/b/grade"]')!.textContent).toContain("查看成绩");
  });
});
