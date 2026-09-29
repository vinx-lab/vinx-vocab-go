/** Fix round 1：ESC 只关最上层；Modal 焦点移入、Tab 循环、关闭后还原 */
import { describe, expect, it, vi } from "vitest";
import { render } from "preact";
import { act } from "preact/test-utils";
import { useState } from "preact/hooks";
import { Button, Drawer, Dropdown, Modal, Popconfirm, Select, modal } from "@/ui";
import { layerCount } from "@/ui/layers";

const tick = () => act(async () => { await new Promise((r) => setTimeout(r, 60)); });
const esc = () => act(() => { document.activeElement?.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true })) ?? document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true })); });
const mount = async (v: preact.VNode) => {
  const el = document.createElement("div");
  document.body.appendChild(el);
  await act(() => render(v, el));
  await tick();
  return el;
};

describe("ESC 只关最上层", () => {
  it("Modal 里打开 confirm：第一次 ESC 只关 confirm，第二次才关 Modal", async () => {
    const onCancel = vi.fn();
    const el = await mount(<Modal open title="编辑" onCancel={onCancel}>表单</Modal>);
    modal.confirm({ title: "替换？" });
    await tick();
    expect(layerCount()).toBe(2);
    await esc();
    await tick();
    expect(document.querySelector(".ant-modal-confirm")).toBeNull();
    expect(onCancel).not.toHaveBeenCalled();
    await esc();
    expect(onCancel).toHaveBeenCalledTimes(1);
    await act(() => render(null, el));
    expect(layerCount()).toBe(0);
  });

  it("Modal 里的 Select 下拉：ESC 只收起下拉", async () => {
    const onCancel = vi.fn();
    const el = await mount(
      <Modal open title="t" onCancel={onCancel}>
        <Select options={[{ value: 1, label: "一" }]} />
      </Modal>,
    );
    await act(() => { document.querySelector(".ant-select")!.dispatchEvent(new MouseEvent("mousedown", { bubbles: true })); });
    await tick();
    expect(document.querySelector(".ant-select-dropdown")).toBeTruthy();
    await esc();
    await tick();
    expect(document.querySelector(".ant-select-dropdown")).toBeNull();
    expect(onCancel).not.toHaveBeenCalled();
    await act(() => render(null, el));
  });

  it("Drawer 里的 Popconfirm、Dropdown：逐层关闭", async () => {
    const onClose = vi.fn();
    function T() {
      return (
        <Drawer open onClose={onClose} title="抽屉">
          <Popconfirm title="确定？">
            <Button>删</Button>
          </Popconfirm>
          <Dropdown trigger={["click"]} menu={{ items: [{ key: "a", label: "甲" }] }}>
            <Button>菜单</Button>
          </Dropdown>
        </Drawer>
      );
    }
    const el = await mount(<T />);
    const btns = [...document.querySelectorAll(".ant-drawer button.ant-btn")] as HTMLElement[];
    await act(() => btns[0].click());
    await tick();
    expect(document.querySelector(".ant-popconfirm")).toBeTruthy();
    await esc();
    await tick();
    expect(document.querySelector(".ant-popconfirm")).toBeNull();
    expect(onClose).not.toHaveBeenCalled();
    await act(() => btns[1].click());
    await tick();
    expect(document.querySelector(".ant-dropdown")).toBeTruthy();
    await esc();
    await tick();
    expect(document.querySelector(".ant-dropdown")).toBeNull();
    expect(onClose).not.toHaveBeenCalled();
    await esc();
    expect(onClose).toHaveBeenCalledTimes(1);
    await act(() => render(null, el));
  });
});

describe("Modal 焦点", () => {
  it("打开时焦点移入，Tab 在对话框内循环，关闭后还给触发按钮", async () => {
    function T() {
      const [open, setOpen] = useState(false);
      return (
        <div>
          <button id="trigger" onClick={() => setOpen(true)}>打开</button>
          <button id="behind">背后的按钮</button>
          <Modal open={open} title="标题" onCancel={() => setOpen(false)} onOk={() => setOpen(false)}>
            <input id="inside" />
          </Modal>
        </div>
      );
    }
    const el = await mount(<T />);
    const trigger = document.getElementById("trigger")!;
    trigger.focus();
    await act(() => trigger.click());
    await tick();
    const dialog = document.querySelector("[role=dialog]")!;
    expect(document.activeElement?.outerHTML.slice(0, 80)).toContain("data-focus-start");
    // jsdom 没有布局：getClientRects 为空，这里补上让「可见」判断通过
    const items = [...dialog.querySelectorAll("button, input")] as HTMLElement[];
    items.forEach((x) => (x.getClientRects = () => [{}] as unknown as DOMRectList));
    const last = items[items.length - 1];
    last.focus();
    await act(() => { last.dispatchEvent(new KeyboardEvent("keydown", { key: "Tab", bubbles: true, cancelable: true })); });
    expect(document.activeElement).toBe(items[0]);
    await act(() => { items[0].dispatchEvent(new KeyboardEvent("keydown", { key: "Tab", shiftKey: true, bubbles: true, cancelable: true })); });
    expect(document.activeElement).toBe(last);
    await esc();
    await tick();
    expect(document.activeElement).toBe(trigger);
    await act(() => render(null, el));
  });
});
