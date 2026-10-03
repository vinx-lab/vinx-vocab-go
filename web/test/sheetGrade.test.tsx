/** 默写单批改页（spec 0006）：默认全对、点一下标错再点改回、错题可记下学生写的内容、确认后提交、提交后显示成绩单 */
import { afterEach, describe, expect, it, vi } from "vitest";
import { render } from "preact";
import { act } from "preact/test-utils";
import { clearCache } from "@/lib/query";
import { SheetGrade } from "@/pages/sheets/SheetGradePage";
import type { SheetDetail } from "@/types";

const wait = (ms = 30) => act(async () => { await new Promise((r) => setTimeout(r, ms)); });
const mounted: HTMLElement[] = [];
const mount = async (vnode: preact.VNode) => {
  const el = document.createElement("div");
  document.body.appendChild(el);
  mounted.push(el);
  await act(() => render(vnode, el));
  return el;
};
const ok = (data: unknown) => new Response(JSON.stringify({ success: true, data }), { status: 200 });

afterEach(async () => {
  for (const el of mounted.splice(0)) await act(() => render(null, el));
  vi.unstubAllGlobals();
  clearCache();
  document.body.innerHTML = "";
});

const base: SheetDetail = {
  id: "sh1",
  seq: 3,
  createdAt: "2026-10-01T00:00:00Z",
  student: { id: "stu", name: "小明" },
  modes: [],
  words: [],
  format: "dictation",
  // index 1 的词已被删除，不在明细里：提交时也不能带上它
  items: [
    { index: 0, type: "word", section: 1, wordId: "w1", prompt: "n. 苹果", answer: "apple" },
    { index: 2, type: "phrase", section: 2, wordId: "w2", prompt: "大声朗读", answer: "read aloud" },
    { index: 3, type: "sentence", section: 3, sentenceId: "s1", prompt: "我发现记笔记很有用。", answer: "I find taking notes useful.", cn: "我发现记笔记很有用。" },
  ],
  grading: null,
};

const graded: SheetDetail = {
  ...base,
  grading: {
    sessionId: "g1",
    gradedAt: "2026-10-02T10:00:00Z",
    gradedBy: { id: "stu", name: "小明" },
    selfGraded: true,
    correct: 1,
    total: 3,
    results: [
      { index: 0, correct: true, userAnswer: null },
      { index: 2, correct: false, userAnswer: "read loud" },
      { index: 3, correct: false, userAnswer: null },
    ],
  },
};

function stub(detail: SheetDetail, after: SheetDetail) {
  const calls: { method: string; url: string; body: any }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      calls.push({ method, url, body: init?.body ? JSON.parse(String(init.body)) : undefined });
      if (method === "GET" && url === "/api/sheets/sh1") return ok(calls.some((c) => c.method === "POST") ? after : detail);
      if (method === "POST" && url === "/api/sheets/sh1/grade") return ok(after);
      return new Response(JSON.stringify({ success: false, error: { message: "no route" } }), { status: 404 });
    }),
  );
  return calls;
}

const toggle = (root: ParentNode, no: number) => root.querySelector(`[aria-label="第 ${no} 题"]`) as HTMLButtonElement;
const buttonByText = (root: ParentNode, re: RegExp) => [...root.querySelectorAll("button")].find((b) => re.test(b.textContent ?? "")) as HTMLButtonElement;

describe("SheetGrade", () => {
  it("默认全对；点一下标错、再点改回；错题可记下内容；确认后按明细下标提交", async () => {
    const calls = stub(base, graded);
    const el = await mount(<SheetGrade sheetId="sh1" />);
    await wait();
    expect(el.textContent).toContain("默写单 #3");
    expect(el.textContent).toContain("对 3 / 错 0");
    // 题号按卷面连续编排；提示与标准答案都显示
    expect(el.textContent).toContain("n. 苹果");
    expect(el.textContent).toContain("I find taking notes useful.");
    expect(toggle(el, 1).getAttribute("aria-pressed")).toBe("false");

    await act(() => toggle(el, 2).click());
    await act(() => toggle(el, 3).click());
    await act(() => toggle(el, 1).click());
    await act(() => toggle(el, 1).click()); // 改回对
    expect(toggle(el, 2).getAttribute("aria-pressed")).toBe("true");
    expect(el.textContent).toContain("对 1 / 错 2");

    const note = el.querySelector('[aria-label="第 2 题学生写的"]') as HTMLInputElement;
    expect(note).not.toBeNull();
    expect(el.querySelector('[aria-label="第 1 题学生写的"]')).toBeNull(); // 对的题不记
    note.value = " read loud ";
    await act(() => { note.dispatchEvent(new Event("input", { bubbles: true })); });

    await act(() => buttonByText(el, /提交批改/).click());
    await wait();
    // 确认弹窗：只能提交一次
    expect(document.body.textContent).toContain("提交后不能修改");
    expect(calls.some((c) => c.method === "POST")).toBe(false);
    await act(() => buttonByText(document.body, /确认提交/).click());
    await wait(60);

    const post = calls.find((c) => c.method === "POST")!;
    expect(post.url).toBe("/api/sheets/sh1/grade");
    expect(post.body).toEqual({
      results: [
        { index: 0, correct: true },
        { index: 2, correct: false, userAnswer: "read loud" },
        { index: 3, correct: false },
      ],
    });
    // 提交后显示成绩单
    expect(el.textContent).toContain("1 / 3");
    expect(el.querySelector('a[href^="/sheets/new?format=dictation&from=g1"]')).not.toBeNull();
  });

  it("已批改：直接显示成绩单（对错列表、自批标记、记下的内容），没有提交按钮", async () => {
    stub(graded, graded);
    const el = await mount(<SheetGrade sheetId="sh1" />);
    await wait();
    expect(el.textContent).toContain("已批改");
    expect(el.textContent).toContain("自批");
    expect(el.textContent).toContain("read loud");
    expect(buttonByText(el, /提交批改/)).toBeUndefined();
    expect(el.querySelector('a[href="/sheets/new?format=dictation&from=g1&userId=stu"]')).not.toBeNull();
  });
});
