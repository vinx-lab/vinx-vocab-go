import { describe, expect, it, vi } from "vitest";
import { render } from "preact";
import { act } from "preact/test-utils";
import { Button, Form, FormInstance, Input } from "@/ui";
import { pageList } from "@/ui/Table";

const tick = () => act(async () => { await new Promise((r) => setTimeout(r, 0)); });

describe("Button", () => {
  it("两个汉字中间加空格（与 antd 一致），class 组合一致", () => {
    const el = document.createElement("div");
    render(<Button type="primary" size="large" block>登录</Button>, el);
    const b = el.querySelector("button")!;
    expect(b.textContent).toBe("登 录");
    expect(b.className).toBe("ant-btn ant-btn-primary ant-btn-color-primary ant-btn-variant-solid ant-btn-lg ant-btn-block");
  });
});

describe("Form 校验", () => {
  it("必填、最小长度、依赖字段的自定义校验；通过后 onFinish 拿到值", async () => {
    const onFinish = vi.fn();
    const form = new FormInstance();
    const el = document.createElement("div");
    await act(() =>
      render(
        <Form form={form} layout="vertical" onFinish={onFinish} initialValues={{ name: "小明" }}>
          <Form.Item name="name" label="姓名" rules={[{ required: true, message: "请输入姓名" }]}><Input /></Form.Item>
          <Form.Item name="pwd" label="新密码" rules={[{ required: true, min: 6, message: "至少 6 位" }]}><Input.Password /></Form.Item>
          <Form.Item name="pwd2" label="确认" dependencies={["pwd"]} rules={[({ getFieldValue }) => ({ validator: (_, v) => (!v || v === getFieldValue("pwd") ? Promise.resolve() : Promise.reject(new Error("两次输入不一致"))) })]}><Input.Password /></Form.Item>
        </Form>,
        el,
      ),
    );
    expect((el.querySelector("#name") as HTMLInputElement).value).toBe("小明");
    expect(el.querySelector("label[for=name]")!.className).toContain("ant-form-item-required");
    await act(() => form.submit());
    await tick();
    expect(onFinish).not.toHaveBeenCalled();
    expect(el.textContent).toContain("至少 6 位");
    form.setFieldsValue({ pwd: "123456", pwd2: "654321" });
    await act(() => form.submit());
    await tick();
    expect(el.textContent).toContain("两次输入不一致");
    expect(el.querySelector(".ant-form-item-has-error #pwd2")).toBeTruthy();
    form.setFieldsValue({ pwd2: "123456" });
    await act(() => form.submit());
    await tick();
    expect(onFinish).toHaveBeenCalledWith({ name: "小明", pwd: "123456", pwd2: "123456" });
    await act(() => form.resetFields());
    expect(form.getFieldValue("pwd")).toBeUndefined();
    expect(form.getFieldValue("name")).toBe("小明");
    render(null, el);
  });

  it("输入触发字段更新与校验", async () => {
    const form = new FormInstance();
    const el = document.createElement("div");
    await act(() => render(<Form form={form}><Form.Item name="email" label="邮箱" rules={[{ type: "email", message: "请输入有效邮箱" }]}><Input /></Form.Item></Form>, el));
    const input = el.querySelector("input")!;
    input.value = "abc";
    await act(() => { input.dispatchEvent(new Event("input", { bubbles: true })); });
    await tick();
    expect(form.getFieldValue("email")).toBe("abc");
    expect(el.textContent).toContain("请输入有效邮箱");
    render(null, el);
  });
});

describe("分页页码", () => {
  it("与 rc-pagination 一致的省略规则", () => {
    expect(pageList(1, 5)).toEqual([1, 2, 3, 4, 5]);
    expect(pageList(3, 20)).toEqual([1, 2, 3, 4, 5, "next5", 20]);
    expect(pageList(10, 20)).toEqual([1, "prev5", 8, 9, 10, 11, 12, "next5", 20]);
    expect(pageList(19, 20)).toEqual([1, "prev5", 16, 17, 18, 19, 20]);
  });
});
