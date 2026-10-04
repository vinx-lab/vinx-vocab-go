import { useState } from "preact/hooks";
import {
  ArrowLeftOutlined,
  Button,
  DeleteOutlined,
  EditOutlined,
  Form,
  InboxOutlined,
  Input,
  Modal,
  Popconfirm,
  PlusOutlined,
  RightOutlined,
  Tabs,
  Tag,
  useApp,
} from "@/ui";
import { useMutation, useQuery, useQueryClient } from "@/lib/query";
import { useIdentity } from "@/lib/auth";
import { Link, useNavigate, useParams, useSearchParams } from "@/lib/router";
import { api, errorMessage } from "@/lib/api";
import { can } from "@/lib/perms";
import type { ClassDetail, PlanKind } from "@/types";
import { PLAN_STATUS_LABEL } from "@/types";
import { EmptyBlock, ErrorBlock, Loading, PageHeader } from "@/components/ui";
import { ClassOverviewTab } from "./ClassOverviewTab";
import { ClassMembersTab } from "./ClassMembersTab";
import { ClassTargetsTab } from "./ClassTargetsTab";
import { ClassSettingsTab } from "./ClassSettingsTab";

const PLAN_KIND_LABEL: Record<PlanKind, string> = { daily: "每日学习", test: "检测" };
const TAB_KEYS = ["overview", "members", "plans", "targets", "settings"] as const;
type TabKey = (typeof TAB_KEYS)[number];

/** 班级详情：今日概览 / 成员 / 学习计划 / 目标词书 / 设置 */
export function ClassDetailPage() {
  const { id = "" } = useParams();
  const navigate = useNavigate();
  const { message } = useApp();
  const qc = useQueryClient();
  const { data: identity } = useIdentity();
  const [params, setParams] = useSearchParams();
  const tabParam = params.get("tab") as TabKey | null;
  const tab: TabKey = tabParam && TAB_KEYS.includes(tabParam) ? tabParam : "overview";

  const detail = useQuery({
    queryKey: ["classes", id, "detail"],
    queryFn: () => api.get<ClassDetail>(`/classes/${id}`),
    enabled: !!id,
  });

  const [renameOpen, setRenameOpen] = useState(false);
  const [renameForm] = Form.useForm<{ name: string }>();

  const update = useMutation({
    mutationFn: (body: { name?: string; archived?: boolean }) => api.patch(`/classes/${id}`, body),
    onSuccess: async (_, body) => {
      message.success(body.archived === undefined ? "已重命名" : body.archived ? "班级已归档" : "已取消归档");
      setRenameOpen(false);
      await qc.invalidateQueries({ queryKey: ["classes"] });
    },
    onError: (e) => message.error(errorMessage(e)),
  });

  const remove = useMutation({
    mutationFn: () => api.del(`/classes/${id}`),
    onSuccess: async () => {
      message.success("班级已删除");
      qc.removeQueries({ queryKey: ["classes", id] });
      await qc.invalidateQueries({ queryKey: ["classes", "list"] });
      navigate("/classes", { replace: true });
    },
    onError: (e) => message.error(errorMessage(e, "删除失败")),
  });

  if (detail.isLoading) return <Loading />;
  if (detail.error || !detail.data)
    return (
      <div className="vx-page">
        <PageHeader title="班级详情" extra={<Button onClick={() => navigate("/classes")}>返回班级列表</Button>} />
        <ErrorBlock error={detail.error} onRetry={() => detail.refetch()} />
      </div>
    );

  const c = detail.data;
  const canUpdate = can(identity, "classes");
  const canDelete = can(identity, "classes");

  const actions = (
    <>
      <Button icon={<ArrowLeftOutlined />} onClick={() => navigate("/classes")}>
        班级列表
      </Button>
      {canUpdate && (
        <Button
          icon={<EditOutlined />}
          onClick={() => {
            renameForm.setFieldsValue({ name: c.name });
            setRenameOpen(true);
          }}
        >
          重命名
        </Button>
      )}
      {canUpdate && (
        <Button icon={<InboxOutlined />} loading={update.isPending} onClick={() => update.mutate({ archived: !c.archived })}>
          {c.archived ? "取消归档" : "归档"}
        </Button>
      )}
      {canDelete && (
        <Popconfirm
          title="删除班级？"
          description="成员关系与布置到本班的计划关联会一并删除，学生账号和学习记录保留。"
          okText="删除"
          okButtonProps={{ danger: true }}
          cancelText="取消"
          onConfirm={() => remove.mutateAsync()}
        >
          <Button danger icon={<DeleteOutlined />}>
            删除
          </Button>
        </Popconfirm>
      )}
    </>
  );

  return (
    <div className="vx-page">
      <PageHeader
        eyebrow={`班级 · 老师 ${c.teacher.name}`}
        title={
          <span style={{ display: "inline-flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
            {c.name}
            {c.archived && <Tag bordered={false}>已归档</Tag>}
          </span>
        }
        extra={actions}
      >
        {c.members.length} 名学生 · {c.plans.length} 个计划 · {c.allowSelfPlan ? "允许学生自主安排" : "不允许学生自主安排"}
      </PageHeader>

      <Tabs
        activeKey={tab}
        onChange={(k) => setParams(k === "overview" ? {} : { tab: k as string }, { replace: true })}
        items={[
          { key: "overview", label: "今日概览", children: <ClassOverviewTab classId={id} onSetTargets={canUpdate ? () => setParams({ tab: "targets" }, { replace: true }) : undefined} /> },
          { key: "members", label: `成员 ${c.members.length}`, children: <ClassMembersTab detail={c} /> },
          { key: "plans", label: "学习计划", children: <PlansTab detail={c} canAssign={can(identity, "plans.assign")} /> },
          { key: "targets", label: "目标词书", children: <ClassTargetsTab classId={id} editable={canUpdate} /> },
          { key: "settings", label: "设置", children: <ClassSettingsTab detail={c} editable={canUpdate} /> },
        ]}
      />

      <Modal title="重命名班级" open={renameOpen} onCancel={() => setRenameOpen(false)} onOk={() => renameForm.submit()} confirmLoading={update.isPending} okText="保存" cancelText="取消" forceRender>
        <Form form={renameForm} layout="vertical" onFinish={(v) => update.mutate({ name: v.name.trim() })}>
          <Form.Item name="name" label="班级名称" rules={[{ required: true, whitespace: true, message: "请输入班级名称" }, { max: 40, message: "名称过长" }]}>
            <Input maxLength={40} />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
}

function PlansTab({ detail, canAssign }: { detail: ClassDetail; canAssign: boolean }) {
  const navigate = useNavigate();
  const createBtn = canAssign && (
    <Button type="primary" icon={<PlusOutlined />} onClick={() => navigate(`/plans/new?classId=${detail.id}`)}>
      为本班新建计划
    </Button>
  );
  if (detail.plans.length === 0) {
    return <EmptyBlock title="还没有布置给本班的计划" description="新建计划并选择本班，学生的「今日」就会出现对应任务" action={createBtn || undefined} />;
  }
  return (
    <div className="vx-card" style={{ padding: 16 }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", gap: 8, flexWrap: "wrap", marginBottom: 8 }}>
        <h3 className="vx-title" style={{ fontSize: 17 }}>
          布置给本班的计划
        </h3>
        {createBtn}
      </div>
      {detail.plans.map((p, i) => (
        <Link
          key={p.id}
          to={`/plans/${p.id}`}
          style={{ display: "flex", alignItems: "center", gap: 10, padding: "12px 4px", borderTop: i ? "1px solid var(--line)" : "none", color: "inherit" }}
        >
          <div style={{ flex: 1, minWidth: 0 }}>
            <div style={{ fontWeight: 600 }}>{p.name}</div>
            <div style={{ fontSize: 12, color: "var(--muted)", marginTop: 2 }}>
              {PLAN_KIND_LABEL[p.kind]}
              {p.kind === "daily" ? ` · 每天新学 ${p.newPerDay} 词` : ""}
            </div>
          </div>
          <Tag color={p.status === "active" ? "green" : p.status === "paused" ? "orange" : "default"} bordered={false}>
            {PLAN_STATUS_LABEL[p.status]}
          </Tag>
          <RightOutlined style={{ color: "var(--muted)", fontSize: 12 }} aria-hidden />
        </Link>
      ))}
    </div>
  );
}
