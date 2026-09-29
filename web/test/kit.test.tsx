/** 组件库冒烟：每个组件都能渲染出 antd 同名 class，主要交互不报错 */
import { describe, expect, it, vi } from "vitest";
import { render } from "preact";
import { act } from "preact/test-utils";
import dayjs from "dayjs";
import * as UI from "@/ui";

const mount = async (vnode: preact.VNode) => {
  const el = document.createElement("div");
  document.body.appendChild(el);
  await act(() => render(vnode, el));
  return el;
};
const tick = () => act(async () => { await new Promise((r) => setTimeout(r, 0)); });

describe("组件库冒烟", () => {
  it("展示类组件", async () => {
    const el = await mount(
      <div>
        <UI.Card title="卡" extra="x">体</UI.Card>
        <UI.Tag color="green" bordered={false}>绿</UI.Tag>
        <UI.Alert type="warning" showIcon message="m" description="d" />
        <UI.Spin tip="加载中"><div /></UI.Spin>
        <UI.Empty image={UI.Empty.PRESENTED_IMAGE_SIMPLE} description="空" />
        <UI.Result status="warning" title="加载失败" />
        <UI.Progress percent={30} size="small" format={() => "3/10"} />
        <UI.Steps current={1} items={[{ title: "一" }, { title: "二" }]} />
        <UI.Timeline items={[{ children: "a" }]} />
        <UI.Collapse defaultActiveKey={["a"]} items={[{ key: "a", label: "甲", children: "A" }]} />
        <UI.Tabs items={[{ key: "a", label: "甲", children: "A" }, { key: "b", label: "乙", children: "B" }]} />
        <UI.Switch checked checkedChildren="读" />
        <UI.Segmented options={["a", "b"]} value="a" />
        <UI.Avatar size={28}>V</UI.Avatar>
        <UI.Space wrap><span>a</span></UI.Space>
        <UI.Divider>文</UI.Divider>
        <UI.Typography.Text type="secondary">次</UI.Typography.Text>
      </div>,
    );
    for (const c of ["ant-card-head-title", "ant-tag-green", "ant-alert-warning", "ant-spin-text", "ant-empty-normal", "ant-result-warning", "ant-progress-text", "ant-steps-item-process", "ant-timeline-item-last", "ant-collapse-content-box", "ant-tabs-tab-active", "ant-switch-checked", "ant-segmented-item-selected", "ant-avatar-string", "ant-space-item", "ant-divider-inner-text", "ant-typography-secondary"])
      expect(el.querySelector(`.${c}`), c).toBeTruthy();
    // Tabs 切换
    await act(() => (el.querySelectorAll(".ant-tabs-tab-btn")[1] as HTMLElement).click());
    expect(el.querySelector(".ant-tabs-tab-active")!.textContent).toBe("乙");
  });

  it("Select 打开、选择、多选", async () => {
    const onChange = vi.fn();
    const el = await mount(<UI.Select options={[{ value: "a", label: "甲" }, { value: "b", label: "乙" }]} onChange={onChange} placeholder="请选择" />);
    expect(el.querySelector(".ant-select-selection-placeholder")!.textContent).toBe("请选择");
    await act(() => { el.querySelector(".ant-select")!.dispatchEvent(new MouseEvent("mousedown", { bubbles: true })); });
    await tick();
    const opts = document.querySelectorAll(".ant-select-item-option");
    expect(opts.length).toBe(2);
    await act(() => (opts[1] as HTMLElement).click());
    expect(onChange).toHaveBeenCalledWith("b", { value: "b", label: "乙" });
    render(null, el);
  });

  it("Table 排序、分页、多选", async () => {
    const data = Array.from({ length: 25 }, (_, i) => ({ id: String(i), n: i }));
    const onSel = vi.fn();
    const el = await mount(<UI.Table rowKey="id" columns={[{ title: "数", dataIndex: "n", sorter: (a: { n: number }, b: { n: number }) => a.n - b.n }]} dataSource={data} rowSelection={{ selectedRowKeys: [], onChange: onSel }} />);
    expect(el.querySelectorAll("tbody tr").length).toBe(10);
    expect(el.querySelectorAll(".ant-pagination-item").length).toBe(3);
    await act(() => (el.querySelector("th.ant-table-column-has-sorters") as HTMLElement).click());
    await act(() => (el.querySelector("th.ant-table-column-has-sorters") as HTMLElement).click());
    expect(el.querySelector("tbody tr td:last-child")!.textContent).toBe("24");
    await act(() => (el.querySelector("thead input") as HTMLInputElement).click());
    expect(onSel.mock.calls[0][0].length).toBe(10);
    await act(() => (el.querySelector(".ant-pagination-item-3") as HTMLElement).click());
    expect(el.querySelectorAll("tbody tr").length).toBe(5);
    const empty = await mount(<UI.Table columns={[{ title: "a", dataIndex: "a" }]} dataSource={[]} locale={{ emptyText: "还没有成员" }} />);
    expect(empty.textContent).toContain("还没有成员");
  });

  it("Modal、confirm、message、Dropdown、DatePicker", async () => {
    const onOk = vi.fn();
    const el = await mount(<UI.Modal open title="标题" onOk={onOk}>内容</UI.Modal>);
    await tick();
    expect(document.querySelector("[role=dialog] .ant-modal-title")!.textContent).toBe("标题");
    await act(() => (document.querySelector(".ant-modal-footer .ant-btn-primary") as HTMLElement).click());
    expect(onOk).toHaveBeenCalled();
    render(null, el);

    const ok = vi.fn();
    UI.modal.confirm({ title: "删除？", onOk: ok });
    await tick();
    await act(() => (document.querySelector(".ant-modal-confirm-btns .ant-btn-primary") as HTMLElement).click());
    await tick();
    expect(ok).toHaveBeenCalled();
    expect(document.querySelector(".ant-modal-confirm")).toBeNull();

    UI.message.success("已保存");
    expect(document.querySelector(".ant-message-success")!.textContent).toContain("已保存");

    const click = vi.fn();
    const dd = await mount(<UI.Dropdown trigger={["click"]} menu={{ items: [{ key: "a", label: "甲" }, { type: "divider" }, { key: "b", label: "退出登录", danger: true }], onClick: click }}><button>开</button></UI.Dropdown>);
    await tick();
    await act(() => dd.querySelector("button")!.click());
    await tick();
    const items = document.querySelectorAll(".ant-dropdown-menu-item");
    expect(items.length).toBe(2);
    await act(() => (items[1] as HTMLElement).click());
    expect(click).toHaveBeenCalledWith(expect.objectContaining({ key: "b" }));

    const pick = vi.fn();
    const dp = await mount(<UI.DatePicker value={dayjs("2026-09-28")} onChange={pick} />);
    await act(() => { dp.querySelector(".ant-picker")!.dispatchEvent(new MouseEvent("mousedown", { bubbles: true })); });
    await tick();
    await act(() => (document.querySelector('td[title="2026-09-30"]') as HTMLElement).click());
    expect(pick.mock.calls[0][1]).toBe("2026-09-30");
  });
});
