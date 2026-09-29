import { useState } from "preact/hooks";
import {
  Alert,
  Button,
  CopyOutlined,
  Form,
  Input,
  Modal,
  Popconfirm,
  ReloadOutlined,
  Space,
  Table,
  Typography,
  UserAddOutlined,
  UsergroupAddOutlined,
  useApp,
  type ColumnType,
} from "@/ui";
import { useMutation, useQueryClient } from "@/lib/query";
import { useIdentity } from "@/lib/auth";
import { Link } from "@/lib/router";
import dayjs from "dayjs";
import { api, errorMessage } from "@/lib/api";
import { can } from "@/lib/perms";
import type { ClassDetail } from "@/types";
import { InviteCode, useCopy } from "./shared";

type Member = ClassDetail["members"][number];

interface BatchForm {
  names: string;
  prefix: string;
  password: string;
}

interface BatchResult {
  created: { name: string; account: string }[];
  password: string;
}

/** 班级成员：邀请码 + 成员表 + 添加/批量建号/移出/重置密码 */
export function ClassMembersTab({ detail }: { detail: ClassDetail }) {
  const { message } = useApp();
  const qc = useQueryClient();
  const copy = useCopy();
  const { data: identity } = useIdentity();
  const canManage = can(identity, "classes");
  const canReset = can(identity, "users");
  const canBatch = canManage && can(identity, "users");
  const classId = detail.id;

  const refresh = () => qc.invalidateQueries({ queryKey: ["classes"] });

  // ---- 邀请码 ----
  const resetCode = useMutation({
    mutationFn: () => api.post(`/classes/${classId}/invite-code`),
    onSuccess: async () => {
      message.success("邀请码已重置，旧邀请码失效");
      await refresh();
    },
    onError: (e) => message.error(errorMessage(e)),
  });

  // ---- 移出 ----
  const remove = useMutation({
    mutationFn: (userId: string) => api.del(`/classes/${classId}/members/${userId}`),
    onSuccess: async () => {
      message.success("已移出班级");
      await refresh();
    },
    onError: (e) => message.error(errorMessage(e)),
  });

  // ---- 添加已有学生 ----
  const [addOpen, setAddOpen] = useState(false);
  const [addForm] = Form.useForm<{ account: string }>();
  const add = useMutation({
    mutationFn: (account: string) => api.post<{ name: string }>(`/classes/${classId}/members`, { account }),
    onSuccess: async (u) => {
      message.success(`已添加 ${u.name}`);
      setAddOpen(false);
      await refresh();
    },
    onError: (e) => message.error(errorMessage(e, "添加失败")),
  });

  // ---- 重置密码 ----
  const [resetUser, setResetUser] = useState<Member | null>(null);
  const [resetForm] = Form.useForm<{ newPassword: string }>();
  const resetPwd = useMutation({
    mutationFn: (v: { userId: string; newPassword: string }) => api.post(`/classes/${classId}/members/${v.userId}/reset-password`, { newPassword: v.newPassword }),
    onSuccess: () => {
      message.success("密码已重置");
      setResetUser(null);
    },
    onError: (e) => message.error(errorMessage(e, "重置失败")),
  });

  // ---- 批量建号 ----
  const [batchOpen, setBatchOpen] = useState(false);
  const [batchForm] = Form.useForm<BatchForm>();
  const [batchResult, setBatchResult] = useState<BatchResult | null>(null);
  const batch = useMutation({
    mutationFn: (v: { names: string[]; prefix: string; password: string }) =>
      api.post<{ created: { name: string; account: string }[] }>(`/classes/${classId}/members/batch`, v),
    onSuccess: async (res, v) => {
      setBatchResult({ created: res.created, password: v.password });
      message.success(`已创建 ${res.created.length} 个学生账号`);
      await refresh();
    },
    onError: (e) => message.error(errorMessage(e, "批量创建失败")),
  });
  const closeBatch = () => {
    setBatchOpen(false);
    setBatchResult(null);
  };
  const copyBatch = () => {
    if (!batchResult) return;
    const lines = ["姓名\t账号\t密码", ...batchResult.created.map((c) => `${c.name}\t${c.account}\t${batchResult.password}`)];
    void copy(lines.join("\n"), "账号列表已复制，可直接粘贴到表格");
  };

  const columns: ColumnType<Member>[] = [
    {
      title: "姓名",
      dataIndex: "name",
      render: (_, m) => <Link to={`/classes/${classId}/students/${m.id}`}>{m.name}</Link>,
    },
    { title: "账号", dataIndex: "email", render: (v: string) => <span style={{ fontFamily: "var(--mono)" }}>{v}</span> },
    { title: "加入时间", dataIndex: "joinedAt", width: 120, render: (v: string) => dayjs(v).format("YYYY-MM-DD") },
  ];
  if (canManage || canReset) {
    columns.push({
      title: "操作",
      key: "actions",
      width: 160,
      render: (_, m) => (
        <Space size={4}>
          {canReset && m.managedByMe && (
            <Button
              size="small"
              type="link"
              onClick={() => {
                resetForm.resetFields();
                setResetUser(m);
              }}
            >
              重置密码
            </Button>
          )}
          {canManage && (
            <Popconfirm title={`将 ${m.name} 移出班级？`} description="学习记录会保留，可再次加入。" okText="移出" cancelText="取消" onConfirm={() => remove.mutateAsync(m.id)}>
              <Button size="small" type="link" danger>
                移出
              </Button>
            </Popconfirm>
          )}
        </Space>
      ),
    });
  }

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
      <div className="vx-card" style={{ padding: 16 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 12, flexWrap: "wrap" }}>
          <span style={{ color: "var(--muted)" }}>邀请码</span>
          <InviteCode code={detail.inviteCode} size={24} />
          {canManage && (
            <Popconfirm title="重置邀请码？" description="旧邀请码将立即失效，已加入的学生不受影响。" okText="重置" cancelText="取消" onConfirm={() => resetCode.mutateAsync()}>
              <Button size="small" icon={<ReloadOutlined />} loading={resetCode.isPending}>
                重置邀请码
              </Button>
            </Popconfirm>
          )}
        </div>
        <div style={{ fontSize: 13, color: "var(--ink-soft)", marginTop: 8 }}>学生注册时填写邀请码，或登录后在「个人中心」输入邀请码加入班级。</div>
      </div>

      <div className="vx-card" style={{ padding: 16 }}>
        <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 8, flexWrap: "wrap", marginBottom: 12 }}>
          <h3 className="vx-title" style={{ fontSize: 17 }}>
            成员 <span style={{ fontSize: 13, fontWeight: 400, color: "var(--muted)" }}>{detail.members.length} 人</span>
          </h3>
          {canManage && (
            <Space wrap>
              <Button
                icon={<UserAddOutlined />}
                onClick={() => {
                  addForm.resetFields();
                  setAddOpen(true);
                }}
              >
                添加已有学生
              </Button>
              {canBatch && (
                <Button
                  type="primary"
                  icon={<UsergroupAddOutlined />}
                  onClick={() => {
                    batchForm.resetFields();
                    setBatchResult(null);
                    setBatchOpen(true);
                  }}
                >
                  批量创建学生账号
                </Button>
              )}
            </Space>
          )}
        </div>
        <Table<Member> rowKey="id" size="middle" columns={columns} dataSource={detail.members} pagination={false} scroll={{ x: true }} locale={{ emptyText: "还没有成员" }} />
      </div>

      {/* 添加已有学生 */}
      <Modal title="添加已有学生" open={addOpen} onCancel={() => setAddOpen(false)} onOk={() => addForm.submit()} confirmLoading={add.isPending} okText="添加" cancelText="取消" forceRender>
        <Form form={addForm} layout="vertical" onFinish={(v) => add.mutate(v.account.trim())}>
          <Form.Item name="account" label="学生账号" extra="用于把你批量创建、后来移出的账号重新加回班级；自行注册的学生请让其用邀请码加入" rules={[{ required: true, whitespace: true, message: "请输入账号" }]}>
            <Input placeholder="如 zhangsan@example.com 或 c7301" autoComplete="off" />
          </Form.Item>
        </Form>
      </Modal>

      {/* 重置密码 */}
      <Modal
        title={`重置密码：${resetUser?.name ?? ""}`}
        open={!!resetUser}
        onCancel={() => setResetUser(null)}
        onOk={() => resetForm.submit()}
        confirmLoading={resetPwd.isPending}
        okText="重置"
        cancelText="取消"
        forceRender
      >
        <Form form={resetForm} layout="vertical" onFinish={(v) => resetUser && resetPwd.mutate({ userId: resetUser.id, newPassword: v.newPassword })}>
          <Form.Item name="newPassword" label="新密码" rules={[{ required: true, message: "请输入新密码" }, { min: 6, message: "至少 6 位" }]}>
            <Input.Password autoComplete="new-password" />
          </Form.Item>
        </Form>
      </Modal>

      {/* 批量创建 */}
      <Modal
        title="批量创建学生账号"
        open={batchOpen}
        onCancel={closeBatch}
        width={560}
        forceRender
        footer={
          batchResult ? (
            <Space>
              <Button icon={<CopyOutlined />} onClick={copyBatch}>
                复制全部
              </Button>
              <Button type="primary" onClick={closeBatch}>
                完成
              </Button>
            </Space>
          ) : (
            <Space>
              <Button onClick={closeBatch}>取消</Button>
              <Button type="primary" loading={batch.isPending} onClick={() => batchForm.submit()}>
                创建
              </Button>
            </Space>
          )
        }
      >
        {batchResult && (
          <div>
            <Alert type="success" showIcon style={{ marginBottom: 12 }} message={`已创建 ${batchResult.created.length} 个账号并加入班级`} description="请复制保存并发给学生；关闭后将无法再次查看初始密码。" />
            <Table
              rowKey="account"
              size="small"
              pagination={false}
              scroll={{ y: 320 }}
              dataSource={batchResult.created}
              columns={[
                { title: "姓名", dataIndex: "name" },
                { title: "账号", dataIndex: "account", render: (v: string) => <span style={{ fontFamily: "var(--mono)" }}>{v}</span> },
                { title: "初始密码", key: "pwd", render: () => <span style={{ fontFamily: "var(--mono)" }}>{batchResult.password}</span> },
              ]}
            />
          </div>
        )}
        {/* 表单保持挂载（隐藏），避免 form 实例断开 */}
        <div hidden={!!batchResult}>
          <Form
            form={batchForm}
            layout="vertical"
            onFinish={(v: BatchForm) =>
              batch.mutate({
                names: v.names
                  .split(/\r?\n/)
                  .map((n) => n.trim())
                  .filter(Boolean),
                prefix: v.prefix.trim().toLowerCase(),
                password: v.password,
              })
            }
          >
            <Form.Item
              name="names"
              label="学生姓名（每行一个）"
              rules={[
                { required: true, whitespace: true, message: "请至少填写一个姓名" },
                {
                  validator: (_, value: string | undefined) =>
                    (value ?? "").split(/\r?\n/).filter((n) => n.trim()).length > 100 ? Promise.reject(new Error("一次最多 100 人")) : Promise.resolve(),
                },
              ]}
            >
              <Input.TextArea rows={6} placeholder={"张三\n李四\n王五"} />
            </Form.Item>
            <Form.Item
              name="prefix"
              label="账号前缀"
              extra={<Typography.Text type="secondary">账号 = 前缀 + 两位序号，如前缀 c7301 → c730101、c730102…（已被占用的序号自动跳过）</Typography.Text>}
              rules={[
                { required: true, message: "请输入账号前缀" },
                { pattern: /^[A-Za-z][A-Za-z0-9]{1,19}$/, message: "以字母开头，2–20 位字母或数字" },
              ]}
            >
              <Input placeholder="如 c7301" autoComplete="off" />
            </Form.Item>
            <Form.Item name="password" label="初始密码（所有账号相同）" rules={[{ required: true, message: "请输入初始密码" }, { min: 6, message: "至少 6 位" }]}>
              <Input.Password autoComplete="new-password" />
            </Form.Item>
          </Form>
        </div>
      </Modal>
    </div>
  );
}
