import { createRoot } from "react-dom/client";
import { useEffect } from "react";
import dayjs from "dayjs";
import zhCN from "antd/locale/zh_CN";
import {
  Alert, App, Avatar, Button, Card, Checkbox, Col, Collapse, ConfigProvider, DatePicker, Divider, Drawer, Dropdown, Empty, Form, Input,
  InputNumber, Layout, List, Menu, Modal, Pagination, Popconfirm, Progress, Radio, Result, Row, Segmented, Select, Space, Spin, Steps,
  Switch, Table, Tabs, Tag, Timeline, Tooltip, Typography, Upload,
} from "antd";
import { FireOutlined, UploadOutlined, UserOutlined } from "@ant-design/icons";
import { getTheme } from "@old-theme";

const dark = location.search.includes("dark");

function Sink() {
  const { message, modal } = App.useApp();
  const [form] = Form.useForm();
  useEffect(() => {
    for (const t of ["success", "error", "info", "warning", "loading"] as const) message[t]("消息", 0);
    modal.confirm({ title: "确认？", content: "内容", okButtonProps: { danger: true } });
    form.validateFields().catch(() => {});
  }, []);
  const cols = [
    { title: "名", dataIndex: "a", sorter: (x: any, y: any) => x.a.localeCompare(y.a), fixed: "left" as const, width: 80 },
    { title: "值", dataIndex: "b", defaultSortOrder: "descend" as const, sorter: (x: any, y: any) => x.b - y.b },
  ];
  const data = Array.from({ length: 30 }, (_, i) => ({ key: i, a: "w" + i, b: i }));
  return (
    <Layout>
      <Layout.Sider theme="light"><Menu mode="inline" selectedKeys={["a"]} items={[{ type: "group", key: "g", label: "组", children: [{ key: "a", icon: <FireOutlined />, label: "今日" }, { key: "b", label: "计划" }] }]} /></Layout.Sider>
      <Layout><Layout.Header>h</Layout.Header><Layout.Content>
        <Space wrap>
          {(["primary", "default", "dashed", "text", "link"] as const).map((t) => [false, true].map((d) => ["small", "middle", "large"].map((s) => <Button key={t + d + s} type={t} danger={d} size={s as any}>按钮</Button>)))}
          <Button disabled>禁用</Button><Button type="primary" disabled>禁用</Button><Button loading>加载</Button><Button type="primary" loading>加载</Button>
          <Button shape="circle" icon={<UserOutlined />} /><Button type="text" icon={<UserOutlined />} /><Button ghost type="primary">幽灵</Button><Button block>块</Button>
          <Button size="small" type="link" danger>退出</Button>
        </Space>
        <Form form={form} layout="vertical" initialValues={{ n: "" }}>
          <Form.Item name="req" label="必填" rules={[{ required: true, message: "请输入" }]} extra="说明"><Input /></Form.Item>
          <Form.Item name="n" label="普通"><Input size="large" placeholder="占位" prefix={<UserOutlined />} suffix="后" allowClear showCount maxLength={10} /></Form.Item>
          <Form.Item label="密码"><Input.Password size="large" /></Form.Item>
          <Form.Item label="搜索"><Input.Search allowClear placeholder="搜" /></Form.Item>
          <Form.Item label="文本"><Input.TextArea showCount maxLength={100} rows={3} /></Form.Item>
          <Form.Item label="数字"><InputNumber min={1} max={9} /></Form.Item>
          <Input status="error" disabled value="x" /><Input size="small" /><InputNumber size="small" disabled /><Input.TextArea status="error" autoSize />
        </Form>
        <Form layout="inline"><Form.Item name="x" rules={[{ required: true }]}><Input /></Form.Item><Button>加入</Button></Form>
        <Select open style={{ width: 200 }} value="a" options={[{ value: "a", label: "甲" }, { value: "b", label: "乙" }, { value: "c", label: "丙", disabled: true }]} />
        <Select mode="multiple" style={{ width: 300 }} value={["a", "b"]} options={[{ value: "a", label: "甲" }, { value: "b", label: "乙" }]} />
        <Select showSearch loading placeholder="请选择" size="small" style={{ width: 120 }} options={[]} /><Select disabled placeholder="d" />
        <Select allowClear size="large" value="a" options={[{ value: "a", label: "甲" }]} status="error" />
        <Checkbox checked>选</Checkbox><Checkbox indeterminate /><Checkbox disabled /><Checkbox.Group options={["a", "b"]} value={["a"]} />
        <Radio.Group optionType="button" buttonStyle="solid" value="a" options={[{ value: "a", label: "甲" }, { value: "b", label: "乙" }, { value: "c", label: "丙", disabled: true }]} />
        <Radio.Group value="a" options={[{ value: "a", label: "甲" }, { value: "b", label: "乙" }]} disabled />
        <Radio.Group optionType="button" value="a" options={[{ value: "a", label: "甲" }, { value: "b", label: "乙" }]} />
        <Segmented block value="a" options={[{ label: "登录", value: "a" }, { label: "注册", value: "b" }, { label: "禁", value: "c", disabled: true }]} />
        <Segmented size="small" value="a" options={["a", "b"]} />
        <Switch checked /><Switch size="small" checkedChildren="读" unCheckedChildren="静" /><Switch disabled loading />
        <DatePicker open value={dayjs("2026-09-28")} disabledDate={(d) => d.date() < 5} /><DatePicker placeholder="立即开始" />
        <Modal open title="标题" okText="确定" cancelText="取消">内容</Modal>
        <Popconfirm open title="确定？" description="描述" okText="是" cancelText="否" okButtonProps={{ danger: true }}><Button>pc</Button></Popconfirm>
        <Drawer open placement="left" width={240} closable={false}>抽屉</Drawer>
        <Drawer open placement="right" title="右侧" styles={{ body: { padding: 0 } }}>抽屉</Drawer>
        <Dropdown open menu={{ selectedKeys: ["t"], items: [{ key: "p", icon: <UserOutlined />, label: "个人" }, { type: "divider" }, { type: "group", key: "g", label: "外观", children: [{ key: "t", label: "浅色" }] }, { key: "l", label: "退出", danger: true }, { key: "d", label: "禁", disabled: true }] }}><Button>dd</Button></Dropdown>
        <Tooltip open title="提示"><Button>tt</Button></Tooltip>
        <Table size="middle" columns={cols} dataSource={data} scroll={{ x: 600 }} pagination={{ pageSize: 10 }} />
        <Table size="small" rowSelection={{ selectedRowKeys: [1] }} columns={cols} dataSource={data.slice(0, 3)} pagination={false} scroll={{ y: 200 }} />
        <Table columns={cols} dataSource={[]} loading />
        <Pagination size="small" current={3} total={200} showSizeChanger={false} /><Pagination current={2} total={50} />
        <Tabs items={[{ key: "a", label: "甲", children: "A" }, { key: "b", label: "乙", children: "B" }, { key: "c", label: "丙", disabled: true }]} />
        <Steps current={1} size="small" responsive items={[{ title: "一" }, { title: "二" }, { title: "三" }]} />
        <Steps current={2} status="finish" items={[{ title: "一" }, { title: "二" }, { title: "三" }]} />
        <Steps current={1} status="error" direction="vertical" items={[{ title: "一" }, { title: "二" }]} />
        <Progress percent={30} size="small" format={() => "3/10"} /><Progress percent={100} /><Progress percent={50} showInfo={false} status="exception" /><Progress percent={50} status="active" />
        <Upload showUploadList={false}><Button icon={<UploadOutlined />}>上传</Button></Upload>
        <Upload.Dragger>拖</Upload.Dragger>
        <Card title="卡片" extra={<a>更多</a>}>内容</Card><Card size="small" title="小">x</Card><Card loading />
        <Row gutter={[16, 16]}>{[1, 2, 3, 4, 6, 8, 12, 16, 24].map((s) => <Col key={s} xs={s} sm={s} md={s} lg={s} xl={s} span={s}>c</Col>)}</Row>
        <Row align="middle" justify="space-between" wrap={false}><Col flex="auto">f</Col><Col flex="none" offset={1}>n</Col></Row>
        <List bordered header="头" footer="尾" dataSource={[1, 2]} renderItem={(i) => <List.Item actions={[<a key="x">退出</a>]}><List.Item.Meta avatar={<Avatar>A</Avatar>} title={"t" + i} description="d" /></List.Item>} />
        <List size="small" loading dataSource={[]} locale={{ emptyText: "空" }} renderItem={() => null} />
        <List dataSource={[]} locale={{ emptyText: "还没有" }} renderItem={() => null} />
        {["magenta", "red", "volcano", "orange", "gold", "lime", "green", "cyan", "blue", "geekblue", "purple", "success", "processing", "error", "warning", "default"].map((c) => <span key={c}><Tag color={c}>{c}</Tag><Tag color={c} bordered={false}>{c}</Tag></span>)}
        <Tag>默认</Tag><Tag closable icon={<UserOutlined />}>关</Tag><Tag bordered={false}>无框</Tag><Tag color="#f50">hex</Tag>
        {(["success", "info", "warning", "error"] as const).map((t) => <div key={t}><Alert type={t} showIcon message="m" /><Alert type={t} showIcon message="m" description="d" action={<Button size="small">重试</Button>} closable /><Alert type={t} message="m" /></div>)}
        <Spin /><Spin size="small" /><Spin size="large" /><Spin tip="加载中"><div style={{ height: 40 }} /></Spin>
        <Empty /><Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="空" />
        {(["success", "error", "info", "warning", "404"] as const).map((s) => <Result key={s} status={s} title="加载失败" subTitle="请稍后" extra={<Button>重试</Button>} />)}
        <Space direction="vertical" size="large"><span>a</span></Space><Space size="small" align="center"><span>b</span></Space><Space size={4} wrap><span>c</span></Space>
        <Divider /><Divider>文字</Divider><Divider type="vertical" /><Divider dashed orientation="left">左</Divider>
        <Avatar size={28}>V</Avatar><Avatar size="small" icon={<UserOutlined />} /><Avatar size="large">L</Avatar>
        <Collapse defaultActiveKey={["a"]} items={[{ key: "a", label: "甲", children: "A", extra: <span>x</span> }, { key: "b", label: "乙", children: "B" }]} />
        <Collapse ghost size="small" items={[{ key: "a", label: "甲", children: "A" }]} />
        <Collapse size="small" items={[{ key: "a", label: "甲", children: "A" }]} />
        <Timeline items={[{ children: "a" }, { children: "b", color: "green" }, { children: "c", color: "red" }, { children: "d", color: "gray" }]} />
        <Typography.Text type="secondary">次</Typography.Text><Typography.Text type="danger">危</Typography.Text><Typography.Text type="success">成</Typography.Text><Typography.Text type="warning">警</Typography.Text><Typography.Text strong>粗</Typography.Text><Typography.Text code>code</Typography.Text>
        <Typography.Paragraph>段</Typography.Paragraph><Typography.Title level={4}>题</Typography.Title><Typography.Link>链</Typography.Link>
      </Layout.Content></Layout>
    </Layout>
  );
}

createRoot(document.getElementById("root")!).render(
  <ConfigProvider locale={zhCN} theme={getTheme(dark)}>
    <App><Sink /></App>
  </ConfigProvider>,
);
