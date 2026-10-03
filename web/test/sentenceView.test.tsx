/** spec 0004 组件：词 → 句（最多 5 条、展开全部、目标词高亮）、课文逐句阅读、单元句型 / 课文只读与编辑入口 */
import { afterEach, describe, expect, it, vi } from "vitest";
import { render } from "preact";
import { act } from "preact/test-utils";
import { clearCache } from "@/lib/query";
import { TextReader, WordSentencesBlock } from "@/components/SentenceView";
import { UnitTextsPanel } from "@/pages/books/UnitTexts";
import type { Sentence, UnitText, WordSentenceItem, WordSentences } from "@/types";

const tick = () => act(async () => { await new Promise((r) => setTimeout(r, 20)); });
const mount = async (vnode: preact.VNode) => {
  const el = document.createElement("div");
  document.body.appendChild(el);
  await act(() => render(vnode, el));
  return el;
};
const ok = (data: unknown) => ({ ok: true, status: 200, json: async () => ({ success: true, data }) });

const s = (id: string, en: string, cn: string, paragraph = 0, words: Sentence["words"] = []): Sentence => ({ id, en, cn, frame: null, source: "import", paragraph, words });

afterEach(() => {
  vi.unstubAllGlobals();
  clearCache();
  document.body.innerHTML = "";
});

describe("WordSentencesBlock", () => {
  it("最多显示 5 条，目标词高亮，可以展开全部", async () => {
    const it = (id: string, type: WordSentenceItem["from"]["type"]): WordSentenceItem => ({
      ...s(id, `He went home ${id}.`, `他回家了 ${id}。`, 0, [{ wordId: "go", position: 1, form: "went" }]),
      from: type === "unitText" ? { type, bookId: "b1", bookName: "七上", unitName: "Unit 1", title: "课文" } : { type, wordId: "go", spelling: "go" },
    });
    const data: WordSentences = { examples: [it("e1", "example")], patterns: [], texts: ["t1", "t2", "t3", "t4", "t5", "t6"].map((x) => it(x, "unitText")), passages: [] };
    const f = vi.fn().mockResolvedValue(ok(data));
    vi.stubGlobal("fetch", f);
    const el = await mount(<WordSentencesBlock wordId="go" />);
    await tick();
    expect(f.mock.calls[0][0]).toBe("/api/words/go/sentences");
    expect(el.querySelectorAll(".vx-example-en").length).toBe(5);
    expect(el.querySelector(".vx-sentence-target")!.textContent).toBe("went");
    expect(el.textContent).toContain("七上 · Unit 1 · 课文");
    const more = [...el.querySelectorAll("button")].find((b) => b.textContent?.includes("展开全部 7 句"))!;
    await act(() => more.click());
    expect(el.querySelectorAll(".vx-example-en").length).toBe(7);
  });
});

describe("TextReader", () => {
  it("按段落显示，点一句显示这一句的译文；整篇译文开关", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(ok({ id: "w", spelling: "w", type: "word", phonetic: null, partOfSpeech: null, definition: "释义" })));
    const el = await mount(<TextReader sentences={[s("a", "One.", "一。"), s("b", "Two.", "二。"), s("c", "Three.", "三。", 1)]} />);
    expect(el.querySelectorAll("p.vx-word").length).toBe(2);
    expect(el.textContent).not.toContain("二。");
    await act(() => (el.querySelectorAll(".vx-text-sentence")[1] as HTMLElement).click());
    expect(el.querySelector(".vx-sentence-gloss")!.textContent).toContain("二。");
    expect(el.textContent).not.toContain("三。");
    await act(() => (el.querySelector(".ant-switch") as HTMLElement).click());
    expect(el.textContent).toContain("三。");
  });
});

const text = (id: string, kind: UnitText["kind"], sentences: Sentence[]): UnitText => ({ id, unitId: "u1", kind, title: `T-${id}`, titleCn: null, sortOrder: 0, createdAt: "", updatedAt: "", sentences });

describe("UnitTextsPanel", () => {
  const texts = [text("l1", "list", [{ ...s("x", "I find making cards useful.", "我发现做卡片很有用。"), frame: "I find ... useful." }]), text("t1", "text", [s("y", "Hello.", "你好。")])];

  it("只读：只显示这一类的篇，没有编辑入口", async () => {
    const el = await mount(<UnitTextsPanel unitId="u1" kind="list" editable={false} texts={texts} />);
    expect(el.querySelectorAll(".vx-unit-text").length).toBe(1);
    expect(el.textContent).toContain("I find ... useful.");
    expect(el.textContent).toContain("我发现做卡片很有用。");
    expect(el.textContent).not.toContain("粘贴导入");
    expect(el.textContent).not.toContain("编辑");
  });

  it("可编辑：下移时提交整个单元的顺序", async () => {
    const f = vi.fn().mockResolvedValue(ok({ ordered: 3 }));
    vi.stubGlobal("fetch", f);
    const all = [text("t1", "text", []), text("l1", "list", []), text("t2", "text", [])];
    const el = await mount(<UnitTextsPanel unitId="u1" kind="text" editable texts={all} />);
    expect(el.textContent).toContain("粘贴导入课文");
    await act(() => (el.querySelector('[aria-label="下移 T-t1"]') as HTMLElement).click());
    await tick();
    const [url, init] = f.mock.calls[0];
    expect(url).toBe("/api/units/u1/texts/order");
    expect(JSON.parse(init.body)).toEqual({ textIds: ["t2", "l1", "t1"] });
  });
});
