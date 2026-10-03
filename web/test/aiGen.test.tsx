/**
 * spec 0005 前端：检查标记、系统设置提示词 4 项、句型生成（预览 → 后台任务 → 草稿编辑 / 重写一句 → 保存）、
 * 仿写（勾选句子 + 粘贴 + 改造方式 → 预览请求体 → 保存请求体）、词书学段设置。
 */
import { afterEach, describe, expect, it, vi } from "vitest";
import { render } from "preact";
import { act } from "preact/test-utils";
import { clearCache } from "@/lib/query";
import { AiChecks } from "@/components/AiChecks";
import { AiPromptsCard } from "@/pages/admin/settings/AiPromptsCard";
import { AiPatternsModal, AiVariantsModal } from "@/pages/books/AiDraftModals";
import { BookInfoModal } from "@/pages/books/BookDetailPage";
import type { AiPatternDraft, AiPatternPreview, AiVariantDraft, AiVariantPreview, SentenceChecks } from "@vinx/shared";
import type { Sentence, UnitText } from "@/types";

const wait = (ms: number) => act(async () => { await new Promise((r) => setTimeout(r, ms)); });
const mounted: HTMLElement[] = [];
const mount = async (vnode: preact.VNode) => {
  const el = document.createElement("div");
  document.body.appendChild(el);
  mounted.push(el);
  await act(() => render(vnode, el));
  return el;
};
const ok = (data: unknown) => new Response(JSON.stringify({ success: true, data }), { status: 200 });

type Route = { method: string; url: string; reply: (body: any) => unknown };
/** 按方法 + 路径返回数据；记录每次请求的请求体 */
function stubApi(routes: Route[]) {
  const calls: { method: string; url: string; body: any }[] = [];
  const f = vi.fn(async (url: string, init?: RequestInit) => {
    const method = init?.method ?? "GET";
    const body = init?.body ? JSON.parse(String(init.body)) : undefined;
    calls.push({ method, url, body });
    const r = routes.find((x) => x.method === method && x.url === url);
    if (!r) return new Response(JSON.stringify({ success: false, error: { message: `no route ${method} ${url}` } }), { status: 404 });
    return ok(r.reply(body));
  });
  vi.stubGlobal("fetch", f);
  return calls;
}

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

const byText = (root: ParentNode, re: RegExp) => [...root.querySelectorAll("button")].find((b) => re.test(b.textContent ?? "")) as HTMLButtonElement;
/** 按标签文字找单选项的 input */
const radio = (root: ParentNode, label: string) =>
  [...root.querySelectorAll("label")].find((l) => l.querySelector('input[type="radio"]') && l.textContent?.trim() === label)!.querySelector("input") as HTMLInputElement;
const typeInto = async (input: HTMLInputElement | HTMLTextAreaElement, value: string) => {
  input.value = value;
  await act(() => { input.dispatchEvent(new Event("input", { bubbles: true })); });
};

afterEach(async () => {
  for (const el of mounted.splice(0)) await act(() => render(null, el));
  vi.unstubAllGlobals();
  clearCache();
  document.body.innerHTML = "";
});

describe("AiChecks", () => {
  it("通过时不显示；目标词没用上标红，超纲 / 太长 / 结构偏离标黄并列出", async () => {
    const el = await mount(<AiChecks checks={checks()} />);
    expect(el.querySelector('[data-testid="ai-checks"]')).toBeNull();
    await act(() =>
      render(
        <AiChecks
          checks={checks({ error: true, warning: true, missingTargets: ["useful"], outOfScope: ["giraffe", "zebra"], tooLong: true, words: 23, similarity: 0.4, structureDeviates: true })}
        />,
        el,
      ),
    );
    const box = el.querySelector('[data-testid="ai-checks"]')!;
    expect(box.getAttribute("data-tone")).toBe("error");
    expect(box.textContent).toContain("没用上：useful");
    expect(box.textContent).toContain("超纲：giraffe、zebra");
    expect(box.textContent).toContain("太长 23/20 词");
    expect(box.textContent).toContain("结构偏离 0.40");
    expect(box.querySelectorAll(".ant-tag-red").length).toBe(1);
    expect(box.querySelectorAll(".ant-tag-gold").length).toBe(3);
  });
});

describe("AiPromptsCard", () => {
  it("显示例句、短文、句型、仿写 4 项，并说明可用的占位符", async () => {
    const item = (key: string) => ({ key, current: `模板 ${key}`, isDefault: true, defaultText: `默认 ${key}` });
    stubApi([{ method: "GET", url: "/api/settings/ai/prompts", reply: () => ({ example: item("example"), passage: item("passage"), pattern: item("pattern"), variant: item("variant") }) }]);
    const el = await mount(<AiPromptsCard />);
    await wait(30);
    const tabs = [...el.querySelectorAll(".ant-tabs-tab")].map((t) => t.textContent);
    expect(tabs).toEqual(["例句", "短文", "句型", "仿写"]);
    expect(el.textContent).toContain("{学段}");
    expect(el.textContent).toContain("{单句上限}");
  });
});

const sentence = (id: string, en: string, cn: string, frame: string | null = null): Sentence => ({ id, en, cn, frame, source: "import", paragraph: 0, words: [] });
const text = (id: string, kind: UnitText["kind"], title: string, sentences: Sentence[]): UnitText => ({ id, unitId: "u1", kind, title, titleCn: null, sortOrder: 0, createdAt: "", updatedAt: "", sentences });

describe("AiPatternsModal", () => {
  const preview: AiPatternPreview = {
    unitId: "u1",
    unitName: "Unit 1",
    level: "junior",
    topic: "Unit 1",
    count: 8,
    words: [{ id: "w1", spelling: "useful", definition: "有用的" }],
    existing: [],
    prompt: "你是初中英语教材编辑。……",
  };
  const draft: AiPatternDraft = {
    unitId: "u1",
    level: "junior",
    title: "Unit 1 重点句型",
    model: "fake",
    items: [
      { en: "I find making word cards useful.", cn: "我发现做单词卡很有用。", frame: "I find ... useful.", checks: checks() },
      { en: "How do you memorize giraffe words?", cn: "你怎么记长颈鹿的词？", frame: null, checks: checks({ warning: true, outOfScope: ["giraffe"], issues: ["超纲词：giraffe"] }) },
      { en: "By reading aloud.", cn: "通过朗读。", frame: null, checks: checks() },
    ],
  };

  it("预览 → 生成 → 草稿逐句编辑、删除、重写一句 → 保存为新的句型清单", async () => {
    const calls = stubApi([
      { method: "POST", url: "/api/ai/units/u1/patterns/preview", reply: () => preview },
      { method: "POST", url: "/api/ai/units/u1/patterns", reply: () => ({ jobId: "j1" }) },
      { method: "GET", url: "/api/ai/jobs/j1", reply: () => ({ id: "j1", kind: "patterns", status: "done", elapsedMs: 1200, result: draft }) },
      { method: "POST", url: "/api/ai/sentences/rewrite", reply: () => ({ en: "How do you remember new words?", cn: "你怎么记新单词？", level: "junior", checks: checks() }) },
      { method: "POST", url: "/api/ai/units/u1/patterns/save", reply: () => ({ id: "t-new" }) },
    ]);
    const onSaved = vi.fn();
    const onClose = vi.fn();
    await mount(<AiPatternsModal unitId="u1" texts={[]} onClose={onClose} onSaved={onSaved} />);
    await wait(30);
    const body = document.body;
    expect(calls[0]).toMatchObject({ method: "POST", url: "/api/ai/units/u1/patterns/preview" });
    // 话题从预览预填
    expect((body.querySelector('[aria-label="话题或语法点"]') as HTMLInputElement).value).toBe("Unit 1");
    expect((body.querySelector('[aria-label="提示词"]') as HTMLTextAreaElement).value).toBe(preview.prompt);

    await act(() => (body.querySelector('[data-testid="ai-generate"]') as HTMLButtonElement).click());
    await wait(30);
    const gen = calls.find((c) => c.url === "/api/ai/units/u1/patterns")!;
    expect(gen.body).toMatchObject({ topic: "Unit 1", count: 8, level: "junior", prompt: preview.prompt });

    // 等一次轮询（2 秒）
    await wait(2200);
    expect(body.querySelectorAll(".vx-draft-row").length).toBe(3);
    expect(body.textContent).toContain("1 小时");
    expect(body.textContent).toContain("超纲：giraffe");

    // 编辑第 1 句中文、删除第 3 句、重写第 2 句
    await typeInto(body.querySelector('[aria-label="第 1 句中文"]') as HTMLInputElement, "我觉得做单词卡很有用。");
    await act(() => (body.querySelector('[aria-label="删除第 3 句"]') as HTMLButtonElement).click());
    expect(body.querySelectorAll(".vx-draft-row").length).toBe(2);
    await act(() => (body.querySelector('[aria-label="重写第 2 句"]') as HTMLButtonElement).click());
    await wait(30);
    const rw = calls.find((c) => c.url === "/api/ai/sentences/rewrite")!;
    expect(rw.body).toEqual({ kind: "pattern", en: "How do you memorize giraffe words?", cn: "你怎么记长颈鹿的词？", issues: ["超纲词：giraffe"], level: "junior", unitId: "u1" });
    expect((body.querySelector('[aria-label="第 2 句英文"]') as HTMLInputElement).value).toBe("How do you remember new words?");
    expect(body.textContent).not.toContain("超纲：giraffe");

    await act(() => (body.querySelector('[data-testid="ai-save"]') as HTMLButtonElement).click());
    await wait(30);
    const save = calls.find((c) => c.url === "/api/ai/units/u1/patterns/save")!;
    expect(save.body).toEqual({
      title: "Unit 1 重点句型",
      sentences: [
        { en: "I find making word cards useful.", cn: "我觉得做单词卡很有用。", frame: "I find ... useful." },
        { en: "How do you remember new words?", cn: "你怎么记新单词？" },
      ],
    });
    expect(onSaved).toHaveBeenCalled();
    expect(onClose).toHaveBeenCalled();
  }, 10_000);
});

describe("AiVariantsModal", () => {
  const texts = [
    text("l1", "list", "重点句型", [sentence("s1", "I find making word cards useful.", "我发现做单词卡很有用。", "I find ... useful.")]),
    text("t1", "text", "课文", [sentence("s2", "He likes music.", "他喜欢音乐。")]),
  ];
  const preview: AiVariantPreview = {
    unitId: "u1",
    level: "junior",
    origins: [
      { id: "s1", en: "I find making word cards useful.", cn: "我发现做单词卡很有用。" },
      { id: null, en: "She is tall.", cn: "" },
    ],
    modes: ["replace", "transform"],
    perItem: 3,
    vocab: "unit",
    words: [],
    prompt: "你是初中英语老师。……",
  };
  const draft: AiVariantDraft = {
    unitId: "u1",
    level: "junior",
    title: "Unit 1 句型仿写",
    model: "fake",
    origins: preview.origins,
    items: [
      { origin: 1, originId: "s1", originEn: "I find making word cards useful.", en: "I find reading aloud helpful.", cn: "我发现朗读很有帮助。", change: "replace", note: "换了做法", checks: checks({ similarity: 1 }) },
      { origin: 2, originId: null, originEn: "She is tall.", en: "Is she tall?", cn: "她高吗？", change: "transform", note: "改成疑问句", checks: checks({ similarity: 1 }) },
    ],
  };

  it("勾选的句子 + 粘贴的例句 + 改造方式进预览请求；草稿按例句分组，保存到已有清单", async () => {
    const calls = stubApi([
      { method: "POST", url: "/api/ai/variants/preview", reply: () => preview },
      { method: "POST", url: "/api/ai/variants", reply: () => ({ jobId: "j2" }) },
      { method: "GET", url: "/api/ai/jobs/j2", reply: () => ({ id: "j2", kind: "variants", status: "done", elapsedMs: 900, result: draft }) },
      { method: "POST", url: "/api/ai/variants/save", reply: () => ({ id: "l1" }) },
    ]);
    const onSaved = vi.fn();
    await mount(<AiVariantsModal unitId="u1" texts={texts} initialSentenceIds={["s1"]} onClose={() => undefined} onSaved={onSaved} />);
    const body = document.body;
    // 选择已有句子：s1 已勾选
    expect((body.querySelector('[aria-label="选择 I find making word cards useful."]') as HTMLInputElement).checked).toBe(true);
    await typeInto(body.querySelector('[aria-label="粘贴例句"]') as HTMLTextAreaElement, "She is tall.");
    // 勾选「转换」（默认只选了「替换」）
    await act(() => (body.querySelector('[aria-label="改造方式 转换"]') as HTMLInputElement).click());
    await act(() => byText(body, /下一步/).click());
    await wait(30);
    const pv = calls.find((c) => c.url === "/api/ai/variants/preview")!;
    expect(pv.body).toEqual({ unitId: "u1", sentenceIds: ["s1"], pasted: "She is tall.", modes: ["replace", "transform"], perItem: 3, vocab: "unit" });
    expect((body.querySelector('[aria-label="提示词"]') as HTMLTextAreaElement).value).toBe(preview.prompt);

    await act(() => (body.querySelector('[data-testid="ai-generate"]') as HTMLButtonElement).click());
    await wait(2200);
    const gen = calls.find((c) => c.url === "/api/ai/variants")!;
    expect(gen.body).toMatchObject({ unitId: "u1", sentenceIds: ["s1"], pasted: "She is tall.", modes: ["replace", "transform"], level: "junior", prompt: preview.prompt });
    expect(body.querySelectorAll(".vx-draft-row").length).toBe(2);
    expect(body.textContent).toContain("原句 1：I find making word cards useful.");
    expect(body.textContent).toContain("原句 2：She is tall.");

    // 追加到已有的句型清单
    const target = body.querySelector('[data-testid="save-target"]')!;
    await act(() => radio(target, "追加到「重点句型」").click());
    await act(() => (body.querySelector('[data-testid="ai-save"]') as HTMLButtonElement).click());
    await wait(30);
    const save = calls.find((c) => c.url === "/api/ai/variants/save")!;
    expect(save.body).toEqual({
      unitId: "u1",
      textId: "l1",
      sentences: [
        { en: "I find reading aloud helpful.", cn: "我发现朗读很有帮助。", originId: "s1", variantNote: "换了做法", change: "replace" },
        { en: "Is she tall?", cn: "她高吗？", variantNote: "改成疑问句", change: "transform" },
      ],
    });
    expect(onSaved).toHaveBeenCalled();
  }, 10_000);
});

describe("BookInfoModal", () => {
  it("学段可以设置和清空，随名称、描述一起提交", async () => {
    const calls = stubApi([{ method: "PATCH", url: "/api/books/b1", reply: () => ({ id: "b1" }) }]);
    const book = { id: "b1", name: "七上", description: null, isSystem: false, ownerName: "T", canEdit: true, level: "junior", units: [] };
    await mount(<BookInfoModal book={book} onClose={() => undefined} />);
    const body = document.body;
    await act(() => radio(body.querySelector('[data-testid="book-level"]')!, "小学").click());
    await act(() => byText(body, /保\s*存/).click());
    await wait(30);
    const patches = () => calls.filter((c) => c.method === "PATCH");
    expect(patches()[0].body).toEqual({ name: "七上", description: null, level: "primary" });
    await act(() => radio(body.querySelector('[data-testid="book-level"]')!, "不设置").click());
    await act(() => byText(body, /保\s*存/).click());
    await wait(30);
    expect(patches()[1].body).toEqual({ name: "七上", description: null, level: null });
  });
});
