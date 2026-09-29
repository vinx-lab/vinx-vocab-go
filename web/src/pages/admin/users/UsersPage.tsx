import { useState } from "preact/hooks";
import { Button, Form, Input, KeyOutlined, Modal, PlusOutlined, Select, Table, Tag, UserSwitchOutlined, useApp, type ColumnType } from "@/ui";
import { useMutation, useQuery, useQueryClient } from "@/lib/query";
import { useIdentity } from "@/lib/auth";
import { ROLE_LABEL, ROLES, type Role } from "@vinx/shared";
import { api, errorMessage } from "@/lib/api";
import { can } from "@/lib/perms";
import { ErrorBlock, Loading, PageHeader } from "@/components/ui";
import type { Paged } from "@/types";

interface UserItem {
  id: string;
  email: string;
  name: string;
  role: Role;
  currentGrade?: string | null;
  createdAt: string;
  managedByMe: boolean;
}

const ROLE_COLOR: Record<Role, string> = { admin: "red", teacher: "blue", student: "green" };

/** 用户管理：教师可建学生账号并重置自己创建的账号密码；管理员可建任意角色并改角色 */
export function UsersPage() {
  const { message, modal } = useApp();
  const { data: identity } = useIdentity();
  const qc = useQueryClient();
  const isAdmin = identity?.role === "admin";
  const [keyword, setKeyword] = useState("");
  const [creating, setCreating] = useState(false);
  const [resetUser, setResetUser] = useState<UserItem | null>(null);
  const [createForm] = Form.useForm();
  const [resetForm] = Form.useForm<{ newPassword: string }>();

  const list = useQuery({
    queryKey: ["users", keyword],
    queryFn: () => api.get<Paged<UserItem>>("/users", { q: keyword || undefined, page: 1, limit: 200 }),
  });

  const create = useMutation({
    mutationFn: (values: { email: string; name: string; password: string; role: Role }) => api.post("/users", values),
    onSuccess: () => {
      message.success("账号已创建");
      setCreating(false);
      createForm.resetFields();
      qc.invalidateQueries({ queryKey: ["users"] });
    },
    onError: (e) => message.error(errorMessage(e)),
  });

  const resetPassword = useMutation({
    mutationFn: (values: { id: string; newPassword: string }) => api.post(`/users/${values.id}/reset-password`, { newPassword: values.newPassword }),
    onSuccess: () => {
      message.success("密码已重置");
      setResetUser(null);
      resetForm.resetFields();
    },
    onError: (e) => message.error(errorMessage(e)),
  });

  const changeRole = useMutation({
    mutationFn: (values: { id: string; role: Role }) => api.patch(`/users/${values.id}`, { role: values.role }),
    onSuccess: () => {
      message.success("角色已更新");
      qc.invalidateQueries({ queryKey: ["users"] });
    },
    onError: (e) => message.error(errorMessage(e)),
  });

  const columns: ColumnType<UserItem>[] = [
    { title: "姓名", dataIndex: "name", render: (v: string, u) => <span>{v}{u.id === identity?.id && <Tag style={{ marginLeft: 6 }}>我</Tag>}</span> },
    { title: "账号", dataIndex: "email", responsive: ["sm"] },
    {
      title: "角色",
      dataIndex: "role",
      width: 140,
      render: (role: Role, u) =>
        isAdmin ? (
          <Select
            size="small"
            value={role}
            style={{ width: 100 }}
            options={ROLES.map((r) => ({ value: r, label: ROLE_LABEL[r] }))}
            onChange={(next) =>
              modal.confirm({
                title: `把「${u.name}」改为${ROLE_LABEL[next as Role]}？`,
                content: "角色决定可用功能：学生只能学习，教师可管理班级，管理员可管理系统词书与用户。",
                okText: "确定",
                cancelText: "取消",
                onOk: () => changeRole.mutateAsync({ id: u.id, role: next as Role }),
              })
            }
          />
        ) : (
          <Tag color={ROLE_COLOR[role]} bordered={false}>
            {ROLE_LABEL[role]}
          </Tag>
        ),
    },
    { title: "创建时间", dataIndex: "createdAt", responsive: ["md"], render: (v: string) => new Date(v).toLocaleDateString("zh-CN") },
    {
      title: "操作",
      key: "actions",
      width: 110,
      render: (_, u) =>
        u.managedByMe ? (
          <Button size="small" type="link" icon={<KeyOutlined />} onClick={() => setResetUser(u)}>
            重置密码
          </Button>
        ) : (
          <span style={{ color: "var(--muted)", fontSize: 12 }}>—</span>
        ),
    },
  ];

  return (
    <div className="vx-page">
      <PageHeader
        eyebrow="Users"
        title="用户"
        extra={
          can(identity, "users") && (
            <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreating(true)}>
              新建账号
            </Button>
          )
        }
      >
        {isAdmin ? "管理员可创建任意角色并调整角色。" : "你可以创建学生账号，并重置自己创建的账号密码。"}
      </PageHeader>

      <div className="vx-card" style={{ padding: 16 }}>
        <Input.Search allowClear placeholder="搜索姓名或账号" style={{ maxWidth: 280, marginBottom: 12 }} onSearch={(v) => setKeyword(v.trim())} />
        {list.isLoading ? (
          <Loading />
        ) : list.isError ? (
          <ErrorBlock error={list.error} onRetry={() => list.refetch()} />
        ) : (
          <Table<UserItem>
            rowKey="id"
            size="middle"
            columns={columns}
            dataSource={list.data?.items ?? []}
            pagination={{ pageSize: 20, hideOnSinglePage: true }}
            scroll={{ x: true }}
          />
        )}
      </div>

      <Modal
        title="新建账号"
        open={creating}
        onCancel={() => setCreating(false)}
        onOk={() => createForm.submit()}
        confirmLoading={create.isPending}
        okText="创建"
        cancelText="取消"
        destroyOnHidden
      >
        <Form form={createForm} layout="vertical" initialValues={{ role: "student" }} onFinish={(v: any) => create.mutate(v)}>
          <Form.Item name="name" label="姓名" rules={[{ required: true, whitespace: true, message: "请输入姓名" }]}>
            <Input maxLength={50} />
          </Form.Item>
          <Form.Item name="email" label="账号" extra="可以是邮箱，也可以是学号等自定义账号" rules={[{ required: true, whitespace: true, message: "请输入账号" }]}>
            <Input maxLength={100} />
          </Form.Item>
          <Form.Item name="password" label="初始密码" rules={[{ required: true, min: 6, message: "至少 6 位" }]}>
            <Input.Password />
          </Form.Item>
          <Form.Item name="role" label="角色">
            <Select options={(isAdmin ? ROLES : (["student"] as Role[])).map((r) => ({ value: r, label: ROLE_LABEL[r] }))} />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title={`重置密码：${resetUser?.name ?? ""}`}
        open={!!resetUser}
        onCancel={() => setResetUser(null)}
        onOk={() => resetForm.submit()}
        confirmLoading={resetPassword.isPending}
        okText="重置"
        cancelText="取消"
        destroyOnHidden
      >
        <Form form={resetForm} layout="vertical" onFinish={(v) => resetUser && resetPassword.mutate({ id: resetUser.id, newPassword: v.newPassword })}>
          <Form.Item name="newPassword" label="新密码" rules={[{ required: true, min: 6, message: "至少 6 位" }]}>
            <Input.Password prefix={<UserSwitchOutlined style={{ color: "var(--muted)" }} />} />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
}
