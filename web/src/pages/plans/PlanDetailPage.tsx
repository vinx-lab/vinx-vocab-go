import type { ComponentChildren } from "preact";
import { useNavigate, useParams } from "@/lib/router";
import { useMutation, useQuery, useQueryClient } from "@/lib/query";
import { Button, Col, Grid, Popconfirm, Progress, Row, Table, Tag, useApp, DeleteOutlined, EditOutlined, InboxOutlined, PauseCircleOutlined, PlayCircleOutlined, TeamOutlined, UserOutlined } from "@/ui";
import { api, errorMessage } from "@/lib/api";
import type { Plan, PlanProgressItem, PlanStatus } from "@/types";
import { ErrorBlock, Loading, PageHeader, StatTile } from "@/components/ui";
import { ProgressRing } from "@/components/charts";
import { PlanKindTag, PlanStatusTag, dateRangeText, groupUnitsByBook, modesText } from "./plan-format";

type ProgressData = { day: string; totalWords: number; items: PlanProgressItem[] };

export function PlanDetailPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { message } = useApp();

  const planQuery = useQuery({ queryKey: ["plans", "detail", id], queryFn: () => api.get<Plan>(`/plans/${id}`), enabled: !!id });
  const progressQuery = useQuery({
    queryKey: ["plans", "progress", id],
    queryFn: () => api.get<ProgressData>(`/plans/${id}/progress`),
    enabled: !!id,
  });

  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: ["plans"] });
    queryClient.invalidateQueries({ queryKey: ["today"] });
  };

  const setStatus = useMutation({
    mutationFn: (status: PlanStatus) => api.patch<Plan>(`/plans/${id}`, { status }),
    onSuccess: (p) => {
      invalidate();
      message.success(p.status === "active" ? "计划已恢复" : p.status === "paused" ? "计划已暂停" : "计划已归档");
    },
    onError: (e) => message.error(errorMessage(e)),
  });

  const remove = useMutation({
    mutationFn: () => api.del(`/plans/${id}`),
    onSuccess: () => {
      invalidate();
      message.success("计划已删除");
      navigate("/plans", { replace: true });
    },
    onError: (e) => message.error(errorMessage(e)),
  });

  if (planQuery.isLoading) return <Loading />;
  if (planQuery.error || !planQuery.data) {
    return (
      <div className="vx-page">
        <ErrorBlock error={planQuery.error ?? new Error("计划不存在")} onRetry={() => planQuery.refetch()} />
      </div>
    );
  }

  const plan = planQuery.data;
  const busy = setStatus.isPending || remove.isPending;

  const actions: ComponentChildren[] = [];
  if (plan.targetsMe && plan.status === "active") {
    actions.push(
      <Button key="study" type="primary" onClick={() => navigate("/today")}>
        去学习
      </Button>,
    );
  }
  if (plan.canEdit) {
    actions.push(
      <Button key="edit" icon={<EditOutlined />} onClick={() => navigate(`/plans/${plan.id}/edit`)}>
        编辑
      </Button>,
    );
    if (plan.status === "active") {
      actions.push(
        <Button key="pause" icon={<PauseCircleOutlined />} disabled={busy} onClick={() => setStatus.mutate("paused")}>
          暂停
        </Button>,
      );
    } else {
      actions.push(
        <Button key="resume" icon={<PlayCircleOutlined />} disabled={busy} onClick={() => setStatus.mutate("active")}>
          {plan.status === "archived" ? "取消归档" : "恢复"}
        </Button>,
      );
    }
    if (plan.status !== "archived") {
      actions.push(
        <Popconfirm key="archive" title="归档这个计划？" description="归档后不再出现在今日任务中，可随时取消归档。" okText="归档" cancelText="取消" onConfirm={() => setStatus.mutate("archived")}>
          <Button icon={<InboxOutlined />} disabled={busy}>
            归档
          </Button>
        </Popconfirm>,
      );
    }
    actions.push(
      <Popconfirm
        key="delete"
        title="删除这个计划？"
        description="计划删除后无法恢复，已产生的学习记录和单词记忆会保留。"
        okText="删除"
        okButtonProps={{ danger: true }}
        cancelText="取消"
        onConfirm={() => remove.mutate()}
      >
        <Button danger icon={<DeleteOutlined />} disabled={busy}>
          删除
        </Button>
      </Popconfirm>,
    );
  }

  const groups = groupUnitsByBook(plan.units);

  return (
    <div className="vx-page">
      <PageHeader
        eyebrow={plan.kind === "daily" ? "Daily plan" : "Test"}
        title={plan.name}
        extra={actions.length ? actions : undefined}
      >
        <PlanKindTag kind={plan.kind} />
        <PlanStatusTag status={plan.status} />
        <span style={{ fontSize: 13 }}>{plan.isSelfPlan ? "自己安排" : `${plan.creator.name} 创建`}</span>
      </PageHeader>

      <Row gutter={[16, 16]}>
        <Col xs={24} md={14}>
          <InfoBlock title="内容">
            <div style={{ marginBottom: 8 }}>
              共{" "}
              <span className="vx-num" style={{ fontSize: 22, fontWeight: 600, color: "var(--primary)" }}>
                {plan.wordCount}
              </span>{" "}
              个去重单词 · {plan.units.length} 个单元
            </div>
            {groups.map((g) => (
              <div key={g.bookId} style={{ marginTop: 8 }}>
                <a className="vx-cn" style={{ fontWeight: 600, color: "var(--ink)" }} onClick={() => navigate(`/books/${g.bookId}`)}>
                  {g.bookName}
                </a>
                <div style={{ display: "flex", flexWrap: "wrap", gap: 4, marginTop: 4 }}>
                  {g.units.map((u) => (
                    <Tag key={u.id} className="vx-word" style={{ marginInlineEnd: 0 }}>
                      {u.name}
                    </Tag>
                  ))}
                </div>
              </div>
            ))}
          </InfoBlock>
        </Col>
        <Col xs={24} md={10}>
          <InfoBlock title="安排">
            <InfoRow label="节奏">
              {plan.kind === "daily" ? `每天新学 ${plan.newPerDay} 词 · 复习上限 ${plan.reviewPerDay} 词 · ${plan.order === "random" ? "随机顺序" : "按单元顺序"}` : `抽 ${plan.testSize} 词 · ${plan.testScope === "learned" ? "只测已学过的" : "范围内全部"}`}
            </InfoRow>
            <InfoRow label="题型">{modesText(plan.modes)}</InfoRow>
            <InfoRow label="对象">
              {plan.isSelfPlan ? (
                "自己"
              ) : (
                <div style={{ display: "flex", flexWrap: "wrap", gap: 4 }}>
                  {plan.targets.map((t) => (
                    <Tag key={`${t.type}-${t.id}`} icon={t.type === "class" ? <TeamOutlined /> : <UserOutlined />} style={{ marginInlineEnd: 0 }}>
                      {t.name}
                    </Tag>
                  ))}
                </div>
              )}
            </InfoRow>
            <InfoRow label="日期">{dateRangeText(plan.startDate, plan.endDate)}</InfoRow>
            <InfoRow label="创建者">{plan.creator.name}</InfoRow>
          </InfoBlock>
        </Col>
      </Row>

      <div style={{ marginTop: 16 }}>
        <ProgressSection plan={plan} query={progressQuery} />
      </div>
    </div>
  );
}

function InfoBlock({ title, children }: { title: string; children: ComponentChildren }) {
  return (
    <div className="vx-card" style={{ padding: "16px 18px", height: "100%" }}>
      <h2 className="vx-title" style={{ fontSize: 17, marginBottom: 10 }}>
        {title}
      </h2>
      {children}
    </div>
  );
}

function InfoRow({ label, children }: { label: string; children: ComponentChildren }) {
  return (
    <div style={{ display: "flex", gap: 12, padding: "6px 0", borderBottom: "1px dashed var(--line)" }}>
      <div style={{ width: 48, flexShrink: 0, color: "var(--muted)", fontSize: 13 }}>{label}</div>
      <div style={{ flex: 1, minWidth: 0, fontSize: 14 }}>{children}</div>
    </div>
  );
}

function ProgressSection({
  plan,
  query,
}: {
  plan: Plan;
  query: { data?: ProgressData; isLoading: boolean; error: unknown; refetch: () => unknown };
}) {
  const screens = Grid.useBreakpoint();
  if (query.isLoading) return <Loading tip="加载进度" />;
  if (query.error) return <ErrorBlock error={query.error} onRetry={() => query.refetch()} />;
  const items = query.data?.items ?? [];
  if (items.length === 0) {
    return (
      <div className="vx-card" style={{ padding: 20, color: "var(--muted)", textAlign: "center" }}>
        还没有学习者（班级暂无学生）。
      </div>
    );
  }

  // 只有自己一个学习者：大号进度环
  if (items.length === 1 && (plan.isSelfPlan || plan.targetsMe)) {
    const me = items[0];
    const t = me.today;
    return (
      <div className="vx-card" style={{ padding: "18px 20px" }}>
        <h2 className="vx-title" style={{ fontSize: 17, marginBottom: 12 }}>
          我的进度
        </h2>
        <Row gutter={[16, 16]} align="middle">
          <Col xs={24} sm={8} style={{ textAlign: "center" }}>
            <ProgressRing value={me.learnedWords} total={me.totalWords} size={132} label={`已学 ${me.learnedWords}/${me.totalWords}`} />
            <div style={{ marginTop: 6, color: "var(--ink-soft)" }}>
              已学 <span className="vx-num">{me.learnedWords}</span> / {me.totalWords} 词
            </div>
          </Col>
          <Col xs={24} sm={16}>
            {plan.kind === "daily" ? (
              <Row gutter={[12, 12]}>
                <Col xs={12}>
                  <StatTile label="今日新学" value={t ? t.newDone : "—"} suffix={t ? `/ ${t.newDone + t.newLeft}` : undefined} tone="accent" />
                </Col>
                <Col xs={12}>
                  <StatTile label="今日复习" value={t ? t.reviewDone : "—"} suffix={t ? `/ ${t.reviewDone + t.reviewLeft}` : undefined} tone="primary" />
                </Col>
                <Col xs={24}>
                  <TodayStatus today={me.today} />
                </Col>
              </Row>
            ) : (
              <StatTile
                label="检测成绩"
                value={t?.testResult ? t.testResult.correct : "未完成"}
                suffix={t?.testResult ? `/ ${t.testResult.total} 题正确` : undefined}
                tone="primary"
              />
            )}
          </Col>
        </Row>
      </div>
    );
  }

  const title = (
    <h2 className="vx-title" style={{ fontSize: 17, marginBottom: 12 }}>
      学习进度 <span style={{ fontSize: 13, color: "var(--muted)", fontWeight: 400 }}>· {query.data?.day} · {items.length} 人</span>
    </h2>
  );

  // 手机：卡片列表
  if (!screens.md) {
    return (
      <div>
        {title}
        <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
          {items.map((it) => (
            <div key={it.userId} className="vx-card" style={{ padding: "12px 14px" }}>
              <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", gap: 8 }}>
                <span style={{ fontWeight: 600 }}>{it.name}</span>
                {plan.kind === "daily" ? <TodayStatus today={it.today} /> : <TestResult today={it.today} />}
              </div>
              <Progress percent={pct(it.learnedWords, it.totalWords)} size="small" format={() => `${it.learnedWords}/${it.totalWords}`} />
              {plan.kind === "daily" && it.today && (
                <div style={{ fontSize: 12, color: "var(--muted)" }}>
                  今日新学 {it.today.newDone}/{it.today.newDone + it.today.newLeft} · 复习 {it.today.reviewDone}/{it.today.reviewDone + it.today.reviewLeft}
                </div>
              )}
            </div>
          ))}
        </div>
      </div>
    );
  }

  return (
    <div className="vx-card" style={{ padding: "16px 18px" }}>
      {title}
      <Table<PlanProgressItem>
        rowKey="userId"
        size="middle"
        dataSource={items}
        pagination={items.length > 20 ? { pageSize: 20 } : false}
        scroll={{ x: true }}
        columns={[
          {
            title: "学生",
            dataIndex: "name",
            render: (_, r) => (
              <div>
                <div style={{ fontWeight: 600 }}>{r.name}</div>
                <div style={{ fontSize: 12, color: "var(--muted)" }}>{r.email}</div>
              </div>
            ),
          },
          {
            title: "已学 / 总词数",
            sorter: (a, b) => pct(a.learnedWords, a.totalWords) - pct(b.learnedWords, b.totalWords),
            render: (_, r) => (
              <div style={{ minWidth: 160 }}>
                <Progress percent={pct(r.learnedWords, r.totalWords)} size="small" format={() => `${r.learnedWords}/${r.totalWords}`} />
              </div>
            ),
          },
          ...(plan.kind === "daily"
            ? [
                {
                  title: "今日新学",
                  render: (_: unknown, r: PlanProgressItem) => (r.today ? `${r.today.newDone}/${r.today.newDone + r.today.newLeft}` : "—"),
                },
                {
                  title: "今日复习",
                  render: (_: unknown, r: PlanProgressItem) => (r.today ? `${r.today.reviewDone}/${r.today.reviewDone + r.today.reviewLeft}` : "—"),
                },
                { title: "今日状态", render: (_: unknown, r: PlanProgressItem) => <TodayStatus today={r.today} /> },
              ]
            : [{ title: "检测成绩", render: (_: unknown, r: PlanProgressItem) => <TestResult today={r.today} /> }]),
        ]}
      />
    </div>
  );
}

function pct(v: number, total: number) {
  return total > 0 ? Math.round((v / total) * 100) : 0;
}

function TodayStatus({ today }: { today: PlanProgressItem["today"] }) {
  if (!today) return <Tag bordered={false}>今日无任务</Tag>;
  return today.doneToday ? (
    <Tag color="green" bordered={false}>
      已完成
    </Tag>
  ) : (
    <Tag color="orange" bordered={false}>
      未完成
    </Tag>
  );
}

function TestResult({ today }: { today: PlanProgressItem["today"] }) {
  const r = today?.testResult;
  if (!r) return <Tag bordered={false}>未交卷</Tag>;
  return (
    <span>
      <span className="vx-num" style={{ fontWeight: 600 }}>
        {r.correct}
      </span>
      /{r.total} 题正确
    </span>
  );
}
