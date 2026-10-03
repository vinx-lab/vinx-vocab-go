/** 生成页的默写单格式（spec 0006）：预览请求带格式、各题型数量与句子来源；生成时提交题目列表并跳到打印页 */
import { afterEach, describe, expect, it, vi } from "vitest";
import { render } from "preact";
import { act } from "preact/test-utils";
import { LocationProvider } from "preact-iso";
import { clearCache } from "@/lib/query";
import { SheetNewPage } from "@/pages/sheets/SheetNewPage";

const wait = (ms = 40) => act(async () => { await new Promise((r) => setTimeout(r, ms)); });
const mounted: HTMLElement[] = [];
const ok = (data: unknown) => new Response(JSON.stringify({ success: true, data }), { status: 200 });

afterEach(async () => {
  for (const el of mounted.splice(0)) await act(() => render(null, el));
  vi.unstubAllGlobals();
  clearCache();
  document.body.innerHTML = "";
});

const preview = {
  items: [
    { wordId: "w1", spelling: "apple", phonetic: null, partOfSpeech: "n.", definition: "苹果", score: 3, reasons: [], type: "word" },
    { wordId: "w2", spelling: "read aloud", phonetic: null, partOfSpeech: null, definition: "大声朗读", score: 2, reasons: [], type: "phrase" },
  ],
  sentences: [
    { sentenceId: "s1", type: "sentence", en: "I like it.", cn: "我喜欢它。", frame: null, prompt: "我喜欢它。", answer: "I like it.", status: "learning" },
  ],
};

function stub() {
  const calls: { method: string; path: string; body: any }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      const path = String(url).split("?")[0];
      const body = init?.body ? JSON.parse(String(init.body)) : undefined;
      calls.push({ method, path, body });
      const key = `${method} ${path}`;
      if (key === "GET /api/sheets/sources") return ok({ sessions: [], learningSentences: 2 });
      if (key === "GET /api/books") return ok({ items: [], total: 0 });
      if (key === "GET /api/passages") return ok({ items: [], total: 0 });
      if (key === "GET /api/records/coverage") return ok({ source: "none", total: { target: 0, tested: 0, known: 0, learning: 0, untested: 0 }, books: [] });
      if (key === "POST /api/sheets/preview") return ok(preview);
      if (key === "POST /api/sheets") return ok({ id: "n1", seq: 5, items: [{ id: "n1", seq: 5 }] });
      return new Response(JSON.stringify({ success: false, error: { message: `no route ${key}` } }), { status: 404 });
    }),
  );
  return calls;
}

describe("SheetNewPage · 默写单", () => {
  it("预览带格式与句子来源；生成时提交词和句子题、各题型每份上限，跳到打印页", async () => {
    const calls = stub();
    history.replaceState(null, "", "/sheets/new?format=dictation");
    const el = document.createElement("div");
    document.body.appendChild(el);
    mounted.push(el);
    await act(() =>
      render(
        <LocationProvider>
          <SheetNewPage />
        </LocationProvider>,
        el,
      ),
    );
    await wait();

    const previews = () => calls.filter((c) => c.method === "POST" && c.path === "/api/sheets/preview");
    expect(previews()[0].body).toMatchObject({
      format: "dictation",
      copies: 1,
      includeWords: true,
      includePhrases: true,
      source: { kind: "unfamiliar" },
      sentenceSources: [],
      wordCount: 20,
      phraseCount: 10,
      sentenceCount: 8,
    });

    // 勾选「要学的句子」
    const learning = [...el.querySelectorAll("label")].find((l) => l.textContent?.includes("要学的句子（2）"))!.querySelector("input") as HTMLInputElement;
    await act(() => learning.click());
    await wait();
    expect(previews()[previews().length - 1].body.sentenceSources).toEqual([{ kind: "learning" }]);
    expect(el.textContent).toContain("已选 3 题");
    expect(el.textContent).toContain("我喜欢它。");

    const btn = [...el.querySelectorAll("button")].find((b) => /生成并打印/.test(b.textContent ?? "")) as HTMLButtonElement;
    await act(() => btn.click());
    await wait();
    const create = calls.find((c) => c.method === "POST" && c.path === "/api/sheets")!;
    expect(create.body).toEqual({
      format: "dictation",
      items: [
        { type: "word", wordId: "w1" },
        { type: "phrase", wordId: "w2" },
        { type: "sentence", sentenceId: "s1" },
      ],
      copies: 1,
      wordCount: 20,
      phraseCount: 10,
      sentenceCount: 8,
    });
    expect(location.pathname).toBe("/sheets/n1/print");
  });

  it("只出句子（不含单词、短语）时预览不取词", async () => {
    const calls = stub();
    history.replaceState(null, "", "/sheets/new?format=dictation");
    const el = document.createElement("div");
    document.body.appendChild(el);
    mounted.push(el);
    await act(() => render(<LocationProvider><SheetNewPage /></LocationProvider>, el));
    await wait();
    const box = (text: string) => [...el.querySelectorAll("label")].find((l) => l.textContent?.trim() === text)!.querySelector("input") as HTMLInputElement;
    await act(() => box("单词").click());
    await act(() => box("短语").click());
    await wait();
    // 词、句子都没有来源：不再请求预览
    const n = calls.filter((c) => c.path === "/api/sheets/preview").length;
    const learning = [...el.querySelectorAll("label")].find((l) => l.textContent?.includes("要学的句子"))!.querySelector("input") as HTMLInputElement;
    await act(() => learning.click());
    await wait();
    const last = calls.filter((c) => c.path === "/api/sheets/preview");
    expect(last.length).toBe(n + 1);
    expect(last[last.length - 1].body).toMatchObject({ includeWords: false, includePhrases: false, sentenceSources: [{ kind: "learning" }] });
    expect(last[last.length - 1].body.source).toBeUndefined();
  });
});
