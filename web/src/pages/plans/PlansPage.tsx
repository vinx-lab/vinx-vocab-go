import { useState } from "preact/hooks";
import { useNavigate } from "@/lib/router";
import { useQuery } from "@/lib/query";
import { useIdentity } from "@/lib/auth";
import { Button, Col, Row, Segmented, Select, Tag, PlusOutlined, TeamOutlined, UserOutlined } from "@/ui";
import { api } from "@/lib/api";
import { can } from "@/lib/perms";
import type { Paged, Plan, PlanStatus } from "@/types";
import { EmptyBlock, ErrorBlock, Loading, PageHeader } from "@/components/ui";
import { PlanKindTag, PlanStatusTag, groupUnitsByBook, modesText, paceText, unitNamesText } from "./plan-format";

type Scope = "all" | "mine" | "created";

export function PlansPage() {
  const navigate = useNavigate();
  const { data: identity } = useIdentity();
  const [scope, setScope] = useState<Scope>("all");
  const [status, setStatus] = useState<PlanStatus | undefined>(undefined);

  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ["plans", "list", scope, status ?? "any"],
    queryFn: () => api.get<Paged<Plan>>("/plans", { scope, ...(status ? { status } : {}) }),
  });

  const canCreate = can(identity, "plans");
  const filtered = scope !== "all" || !!status;

  return (
    <div className="vx-page">
      <PageHeader
        eyebrow="Plans"
        title="学习计划"
        extra={
          canCreate && (
            <Button type="primary" icon={<PlusOutlined />} onClick={() => navigate("/plans/new")}>
              新建计划
            </Button>
          )
        }
      >
        计划决定每天学哪些词、学多少、用什么题型练。
      </PageHeader>

      <div style={{ display: "flex", flexWrap: "wrap", gap: 12, marginBottom: 16, alignItems: "center" }}>
        <Segmented<Scope>
          value={scope}
          onChange={setScope}
          options={[
            { label: "全部", value: "all" },
            { label: "安排给我的", value: "mine" },
            { label: "我创建的", value: "created" },
          ]}
        />
        <Select<PlanStatus>
          allowClear
          placeholder="全部状态"
          style={{ width: 130 }}
          value={status}
          onChange={(v) => setStatus(v)}
          options={[
            { label: "进行中", value: "active" },
            { label: "已暂停", value: "paused" },
            { label: "已归档", value: "archived" },
          ]}
        />
      </div>

      {isLoading ? (
        <Loading />
      ) : error ? (
        <ErrorBlock error={error} onRetry={() => refetch()} />
      ) : !data || data.items.length === 0 ? (
        filtered ? (
          <EmptyBlock title="没有符合条件的计划" description="换个筛选条件看看。" />
        ) : (
          <EmptyBlock
            title="还没有学习计划"
            description="一份计划 = 学哪些单元 + 每天学多少 + 用什么题型。建好后，每天打开“今日”就能按计划学新词、复习到期词。"
            action={
              canCreate && (
                <Button type="primary" icon={<PlusOutlined />} onClick={() => navigate("/plans/new")}>
                  建第一个计划
                </Button>
              )
            }
          />
        )
      ) : (
        <Row gutter={[16, 16]}>
          {data.items.map((plan, i) => (
            <Col key={plan.id} xs={24} md={12}>
              <PlanCard plan={plan} delay={i} onOpen={() => navigate(`/plans/${plan.id}`)} />
            </Col>
          ))}
        </Row>
      )}
    </div>
  );
}

function PlanCard({ plan, delay, onOpen }: { plan: Plan; delay: number; onOpen: () => void }) {
  const groups = groupUnitsByBook(plan.units);
  const source = plan.isSelfPlan ? "自己安排" : `${plan.creator.name} 安排`;
  return (
    <div
      className="vx-card vx-rise"
      role="button"
      tabIndex={0}
      onClick={onOpen}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onOpen();
        }
      }}
      style={{
        padding: "16px 18px",
        cursor: "pointer",
        height: "100%",
        animationDelay: `${Math.min(delay, 8) * 40}ms`,
        opacity: plan.status === "archived" ? 0.72 : 1,
        borderLeft: `4px solid ${plan.kind === "daily" ? "var(--primary)" : "var(--accent)"}`,
      }}
    >
      <div style={{ display: "flex", alignItems: "flex-start", justifyContent: "space-between", gap: 8 }}>
        <h3 className="vx-title" style={{ fontSize: 18, lineHeight: 1.35, wordBreak: "break-word" }}>
          {plan.name}
        </h3>
        <div style={{ flexShrink: 0 }}>
          <PlanKindTag kind={plan.kind} />
          <PlanStatusTag status={plan.status} />
        </div>
      </div>
      <div style={{ color: "var(--muted)", fontSize: 12, marginTop: 2 }}>{source}</div>

      <div style={{ marginTop: 10, color: "var(--ink-soft)", fontSize: 13, lineHeight: 1.7 }}>
        {groups.map((g) => (
          <div key={g.bookId} style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
            <span className="vx-cn" style={{ color: "var(--ink)" }}>
              {g.bookName}
            </span>
            {" · "}
            {unitNamesText(g.units.map((u) => u.name))}
          </div>
        ))}
      </div>

      <div style={{ display: "flex", alignItems: "baseline", gap: 16, marginTop: 10, flexWrap: "wrap" }}>
        <span>
          <span className="vx-num" style={{ fontSize: 22, fontWeight: 600 }}>
            {plan.wordCount}
          </span>
          <span style={{ color: "var(--muted)", fontSize: 12, marginLeft: 3 }}>词</span>
        </span>
        <span style={{ fontSize: 13, color: "var(--ink-soft)" }}>{paceText(plan)}</span>
        <span style={{ fontSize: 13, color: "var(--muted)" }}>{modesText(plan.modes)}</span>
      </div>

      {!plan.isSelfPlan && plan.targets.length > 0 && (
        <div style={{ marginTop: 10, display: "flex", flexWrap: "wrap", gap: 4 }}>
          {plan.targets.slice(0, 6).map((t) => (
            <Tag key={`${t.type}-${t.id}`} icon={t.type === "class" ? <TeamOutlined /> : <UserOutlined />} style={{ marginInlineEnd: 0 }}>
              {t.name}
            </Tag>
          ))}
          {plan.targets.length > 6 && <Tag style={{ marginInlineEnd: 0 }}>+{plan.targets.length - 6}</Tag>}
        </div>
      )}
    </div>
  );
}
