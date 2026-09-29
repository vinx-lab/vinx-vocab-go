/** Fix round 1 的 Minor：Form 多条错误与数字文案、useMutation onMutate 抛错、Popconfirm 拒绝不外抛、Drawer 关闭保留内容 */
import { describe, expect, it, vi } from "vitest";
import { render } from "preact";
import { act } from "preact/test-utils";
import { useState } from "preact/hooks";
import { Button, Drawer, Form, FormInstance, Input, InputNumber, Popconfirm } from "@/ui";
import { useMutation } from "@/lib/query";

const tick = () => act(async () => { await new Promise((r) => setTimeout(r, 30)); });

describe("Form", () => {
  it("显示全部失败规则；数字 min/max 用「不能小于/大于」", async () => {
    const form = new FormInstance();
    const el = document.createElement("div");
    await act(() =>
      render(
        <Form form={form} layout="vertical">
          <Form.Item name="code" label="编码" rules={[{ min: 6, message: "至少 6 位" }, { pattern: /^\d+$/, message: "只能是数字" }]}><Input /></Form.Item>
          <Form.Item name="n" label="每日新词" rules={[{ type: "number" as never, min: 1 }]}><InputNumber /></Form.Item>
        </Form>,
        el,
      ),
    );
    form.setFieldsValue({ code: "ab", n: 0 });
    await act(async () => { await form.validateFields().catch(() => undefined); });
    const errs = [...el.querySelectorAll(".ant-form-item-explain-error")].map((x) => x.textContent);
    expect(errs).toEqual(["至少 6 位", "只能是数字", "每日新词不能小于1"]);
    render(null, el);
  });

  it("慢的旧校验结果不覆盖新结果", async () => {
    const form = new FormInstance();
    let slow = true;
    const el = document.createElement("div");
    await act(() =>
      render(
        <Form form={form}>
          <Form.Item name="x" label="x" rules={[{ validator: async (_r, v) => { if (slow) { await new Promise((r) => setTimeout(r, 30)); throw new Error("旧：" + v); } } }]}><Input /></Form.Item>
        </Form>,
        el,
      ),
    );
    form.setFieldsValue({ x: "a" });
    const p1 = form.validateFields().catch(() => undefined);
    slow = false;
    form.setFieldsValue({ x: "b" });
    await act(async () => { await form.validateFields(); await p1; });
    expect(el.querySelector(".ant-form-item-explain-error")).toBeNull();
    render(null, el);
  });
});

describe("useMutation", () => {
  it("onMutate 抛错时不卡在 isPending", async () => {
    let m: ReturnType<typeof useMutation<void, number>> | undefined;
    function M() {
      m = useMutation<void, number>({ mutationFn: async () => 1, onMutate: () => { throw new Error("坏"); } });
      return null;
    }
    const el = document.createElement("div");
    await act(() => render(<M />, el));
    await act(async () => { await m!.mutateAsync().catch(() => undefined); });
    expect(m!.isPending).toBe(false);
    expect(m!.isError).toBe(true);
    render(null, el);
  });
});

describe("Popconfirm", () => {
  it("onConfirm 返回被拒绝的 Promise：保持打开，不产生未处理的拒绝", async () => {
    const unhandled = vi.fn();
    const proc = (globalThis as unknown as { process: { on: (e: string, f: () => void) => void; off: (e: string, f: () => void) => void } }).process;
    proc.on("unhandledRejection", unhandled);
    const el = document.createElement("div");
    document.body.appendChild(el);
    await act(() => render(<Popconfirm title="删？" onConfirm={() => Promise.reject(new Error("失败"))}><Button>删</Button></Popconfirm>, el));
    await tick();
    await act(() => el.querySelector("button")!.click());
    await tick();
    await act(() => (document.querySelector(".ant-popconfirm-buttons .ant-btn-primary") as HTMLElement).click());
    await tick();
    expect(document.querySelector(".ant-popconfirm")).toBeTruthy();
    expect(unhandled).not.toHaveBeenCalled();
    proc.off("unhandledRejection", unhandled);
    await act(() => render(null, el));
  });
});

describe("Drawer", () => {
  it("关闭后保留内容（再次打开状态还在），destroyOnHidden 时卸载", async () => {
    let setOpen!: (o: boolean) => void;
    function T({ destroy }: { destroy?: boolean }) {
      const [open, s] = useState(true);
      setOpen = s;
      return <Drawer open={open} destroyOnHidden={destroy} onClose={() => s(false)}><input id={destroy ? "d" : "k"} /></Drawer>;
    }
    const el = document.createElement("div");
    await act(() => render(<T />, el));
    await tick();
    (document.getElementById("k") as HTMLInputElement).value = "草稿";
    await act(() => setOpen(false));
    await tick();
    expect((document.getElementById("k") as HTMLInputElement).value).toBe("草稿");
    await act(() => render(<T destroy />, el));
    await tick();
    await act(() => setOpen(false));
    await tick();
    expect(document.getElementById("d")).toBeNull();
    await act(() => render(null, el));
  });
});
