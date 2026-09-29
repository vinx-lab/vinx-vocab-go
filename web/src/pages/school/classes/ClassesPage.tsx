import { useState } from "preact/hooks";
import { Button, Form, Input, Modal, PlusOutlined, Tag, TeamOutlined, useApp } from "@/ui";
import { useMutation, useQuery, useQueryClient } from "@/lib/query";
import { useIdentity } from "@/lib/auth";
import { Link, useNavigate } from "@/lib/router";
import { api, errorMessage } from "@/lib/api";
import { can } from "@/lib/perms";
import type { ClassItem, Paged } from "@/types";
import { EmptyBlock, ErrorBlock, Loading, PageHeader } from "@/components/ui";
import { InviteCode } from "./shared";

/** 班级列表 */
export function ClassesPage() {
  const { message } = useApp();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const { data: identity } = useIdentity();
  const isAdmin = identity?.role === "admin";
  const canCreate = can(identity, "classes");

  const [open, setOpen] = useState(false);
  const [form] = Form.useForm<{ name: string }>();

  const query = useQuery({ queryKey: ["classes", "list"], queryFn: () => api.get<Paged<ClassItem>>("/classes") });

  const create = useMutation({
    mutationFn: (name: string) => api.post<{ id: string }>("/classes", { name }),
    onSuccess: async (cls) => {
      message.success("班级已创建");
      setOpen(false);
      form.resetFields();
      await qc.invalidateQueries({ queryKey: ["classes"] });
      navigate(`/classes/${cls.id}`);
    },
    onError: (e) => message.error(errorMessage(e, "创建失败")),
  });

  const createButton = canCreate && (
    <Button type="primary" icon={<PlusOutlined />} onClick={() => setOpen(true)}>
      新建班级
    </Button>
  );

  return (
    <div className="vx-page">
      <PageHeader eyebrow="Classes" title="班级" extra={createButton}>
        学生凭邀请码加入班级；在班级里查看每天的学习进度。
      </PageHeader>

      {query.isLoading ? (
        <Loading />
      ) : query.error || !query.data ? (
        <ErrorBlock error={query.error} onRetry={() => query.refetch()} />
      ) : query.data.items.length === 0 ? (
        <EmptyBlock title="还没有班级" description="新建一个班级，把邀请码发给学生即可加入" action={createButton || undefined} />
      ) : (
        <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(280px, 1fr))", gap: 14 }}>
          {query.data.items.map((c) => (
            <Link
              key={c.id}
              to={`/classes/${c.id}`}
              className="vx-card vx-rise"
              style={{ display: "block", padding: "16px 18px", color: "inherit", opacity: c.archived ? 0.72 : 1 }}
            >
              <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
                <TeamOutlined style={{ color: "var(--primary)" }} aria-hidden />
                <span className="vx-title" style={{ fontSize: 18, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                  {c.name}
                </span>
                {c.archived && (
                  <Tag bordered={false} style={{ marginLeft: "auto" }}>
                    已归档
                  </Tag>
                )}
              </div>
              <div style={{ display: "flex", alignItems: "center", gap: 8, marginTop: 10, fontSize: 13, color: "var(--muted)" }}>
                邀请码 <InviteCode code={c.inviteCode} size={16} />
              </div>
              <div style={{ display: "flex", gap: 16, marginTop: 6, fontSize: 13, color: "var(--ink-soft)", flexWrap: "wrap" }}>
                <span>
                  <span className="vx-num" style={{ fontSize: 18, fontWeight: 600, color: "var(--ink)" }}>{c.memberCount}</span> 人
                </span>
                <span>
                  <span className="vx-num" style={{ fontSize: 18, fontWeight: 600, color: "var(--ink)" }}>{c.planCount}</span> 个计划
                </span>
                {isAdmin && <span style={{ marginLeft: "auto" }}>老师：{c.teacherName}</span>}
              </div>
            </Link>
          ))}
        </div>
      )}

      <Modal
        title="新建班级"
        open={open}
        onCancel={() => setOpen(false)}
        onOk={() => form.submit()}
        confirmLoading={create.isPending}
        okText="创建"
        cancelText="取消"
        destroyOnHidden
      >
        <Form form={form} layout="vertical" onFinish={(v) => create.mutate(v.name.trim())} preserve={false}>
          <Form.Item name="name" label="班级名称" rules={[{ required: true, whitespace: true, message: "请输入班级名称" }, { max: 40, message: "名称过长" }]}>
            <Input placeholder="如：七年级 3 班" autoFocus maxLength={40} />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
}
