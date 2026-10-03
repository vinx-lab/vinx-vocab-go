/** 目标词书编辑（spec 0003）：上下移动排序后保存整体顺序；只读时不出现编辑按钮 */
import { afterEach, describe, expect, it, vi } from "vitest";
import { render } from "preact";
import { act } from "preact/test-utils";
import { CoverageBar, TargetBooksEditor } from "@/pages/coverage/components";
import { clearCache } from "@/lib/query";

const tick = () => act(async () => { await new Promise((r) => setTimeout(r, 30)); });

afterEach(() => {
  vi.unstubAllGlobals();
  clearCache();
});

function stubBooks() {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response(JSON.stringify({ success: true, data: { items: [{ id: "a", name: "七上", wordCount: 300 }, { id: "b", name: "七下", wordCount: 320 }], total: 2 } }), { status: 200 })),
  );
}

describe("TargetBooksEditor", () => {
  it("下移后保存，按新顺序提交 bookIds；未改动时保存不可点", async () => {
    stubBooks();
    const onSave = vi.fn();
    const el = document.createElement("div");
    document.body.appendChild(el);
    await act(() => render(<TargetBooksEditor value={[{ id: "a", name: "七上" }, { id: "b", name: "七下" }]} onSave={onSave} />, el));
    await tick();
    // 两个汉字的按钮文字中间会插空格（与 antd 一致）
    const saveBtn = () => [...el.querySelectorAll("button")].find((b) => /保\s*存/.test(b.textContent ?? "")) as HTMLButtonElement;
    expect(saveBtn().disabled).toBe(true);
    await act(() => (el.querySelector('[aria-label="下移 七上"]') as HTMLButtonElement).click());
    expect(saveBtn().disabled).toBe(false);
    await act(() => saveBtn().click());
    expect(onSave).toHaveBeenCalledWith(["b", "a"]);
    await act(() => render(null, el));
    el.remove();
  });

  it("只读：列出词书，没有移动、移除和保存", async () => {
    const el = document.createElement("div");
    await act(() => render(<TargetBooksEditor readOnly value={[{ id: "a", name: "七上" }]} />, el));
    expect(el.textContent).toContain("七上");
    expect(el.querySelector('[aria-label="下移 七上"]')).toBeNull();
    expect([...el.querySelectorAll("button")].some((b) => /保\s*存/.test(b.textContent ?? ""))).toBe(false);
    await act(() => render(null, el));
  });
});

describe("CoverageBar", () => {
  it("按会了 / 要学 / 未测分段，数字写在无障碍标签里", async () => {
    const el = document.createElement("div");
    await act(() => render(<CoverageBar counts={{ known: 2000, learning: 800, untested: 900 }} />, el));
    expect(el.querySelector('[role="img"]')!.getAttribute("aria-label")).toBe("会了 2000，要学 800，未测 900");
    expect(el.querySelectorAll('[role="img"] > div').length).toBe(3);
    await act(() => render(null, el));
  });
});
