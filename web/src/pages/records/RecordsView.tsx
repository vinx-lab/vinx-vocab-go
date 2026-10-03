import { useState } from "preact/hooks";
import type { ComponentChildren } from "preact";
import { Col, Pagination, Row, Segmented, Select, Tag } from "@/ui";
import { RightOutlined } from "@/ui";
import { keepPreviousData, useQuery } from "@/lib/query";
import { Link } from "@/lib/router";
import { api } from "@/lib/api";
import type { DailyPoint, Paged, RecordsSummary, SessionKind, SessionListItem } from "@/types";
import { KIND_LABEL } from "@/types";
import { EmptyBlock, ErrorBlock, KindTag, Loading, StatTile, percent } from "@/components/ui";
import { DailyWordsChart, MasteryBar } from "@/components/charts";
import { CoverageSection } from "@/pages/coverage/components";
import { fmtDay, fmtTime, withUser } from "./shared";

const PAGE_SIZE = 20;

/**
 * 学习记录视图：学生本人（userId 省略）与老师查看学生（传 userId）共用。
 */
export function RecordsView({ userId }: { userId?: string }) {
  const summary = useQuery({
    queryKey: ["records", userId ?? "me", "summary"],
    queryFn: () => api.get<RecordsSummary>(withUser("/records/summary", userId)),
  });

  if (summary.isLoading) return <Loading />;
  if (summary.error || !summary.data) return <ErrorBlock error={summary.error} onRetry={() => summary.refetch()} />;
  const s = summary.data;

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
      <Row gutter={[12, 12]}>
        <Col xs={12} sm={8} md={4}>
          <StatTile label="连续学习" value={s.streak} suffix="天" tone="accent" hint={`近 30 天学习 ${s.activeDays30} 天`} />
        </Col>
        <Col xs={12} sm={8} md={4}>
          <StatTile label="已学单词" value={s.learnedWords} suffix="词" tone="primary" />
        </Col>
        <Col xs={12} sm={8} md={4}>
          <StatTile label="今日新学 / 复习" value={`${s.today.newWords}/${s.today.reviewedWords}`} suffix="词" hint={`${s.today.answers} 题 · ${s.today.minutes} 分钟`} />
        </Col>
        <Col xs={12} sm={8} md={4}>
          <StatTile label="近 7 天正确率" value={percent(s.accuracy7d.rate)} hint={`首答 ${s.accuracy7d.correct}/${s.accuracy7d.total} 题`} />
        </Col>
        <Col xs={12} sm={8} md={4}>
          <StatTile label="近 7 天学习" value={s.minutes7d} suffix="分钟" />
        </Col>
        <Col xs={12} sm={8} md={4}>
          <StatTile label="今天到期" value={s.dueToday} suffix="词" hint={s.today.reviewLeft ? `待复习 ${s.today.reviewLeft} 词` : undefined} />
        </Col>
      </Row>

      <CoverageSection userId={userId} />

      <div className="vx-card" style={{ padding: 16 }}>
        <SectionTitle title="记忆分布" />
        <MasteryBar mastery={s.mastery} />
        <div style={{ fontSize: 12, color: "var(--muted)", marginTop: 8 }}>
          按记忆稳定性划分：学习中 &lt; 7 天，巩固中 7–21 天，已掌握 ≥ 21 天（稳定性越长，越不容易忘）。
        </div>
      </div>

      <TrendCard userId={userId} />
      <SessionHistory userId={userId} />
    </div>
  );
}

function SectionTitle({ title, extra }: { title: string; extra?: ComponentChildren }) {
  return (
    <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 8, marginBottom: 12, flexWrap: "wrap" }}>
      <h3 className="vx-title" style={{ fontSize: 17 }}>
        {title}
      </h3>
      {extra}
    </div>
  );
}

function TrendCard({ userId }: { userId?: string }) {
  const [days, setDays] = useState<7 | 30>(7);
  const daily = useQuery({
    queryKey: ["records", userId ?? "me", "daily", days],
    queryFn: () => api.get<DailyPoint[]>(withUser(`/records/daily?days=${days}`, userId)),
  });
  return (
    <div className="vx-card" style={{ padding: 16 }}>
      <SectionTitle
        title="学习趋势"
        extra={
          <Segmented<7 | 30>
            size="small"
            value={days}
            onChange={setDays}
            options={[
              { label: "7 天", value: 7 },
              { label: "30 天", value: 30 },
            ]}
          />
        }
      />
      {daily.isLoading ? (
        <Loading />
      ) : daily.error || !daily.data ? (
        <ErrorBlock error={daily.error} onRetry={() => daily.refetch()} />
      ) : (
        <DailyWordsChart data={daily.data} />
      )}
    </div>
  );
}

type KindFilter = SessionKind | "all";

function SessionHistory({ userId }: { userId?: string }) {
  const [page, setPage] = useState(1);
  const [kind, setKind] = useState<KindFilter>("all");
  const sessions = useQuery({
    queryKey: ["records", userId ?? "me", "sessions", kind, page],
    queryFn: () => {
      const qs = `/records/sessions?page=${page}&limit=${PAGE_SIZE}${kind === "all" ? "" : `&kind=${kind}`}`;
      return api.get<Paged<SessionListItem>>(withUser(qs, userId));
    },
    placeholderData: keepPreviousData,
  });

  const kindOptions: { label: string; value: KindFilter }[] = [{ label: "全部", value: "all" }, ...(Object.keys(KIND_LABEL) as SessionKind[]).map((k) => ({ label: KIND_LABEL[k], value: k }))];

  return (
    <div className="vx-card" style={{ padding: 16 }}>
      <SectionTitle
        title="学习历史"
        extra={
          <Select<KindFilter>
            size="small"
            style={{ width: 120 }}
            value={kind}
            options={kindOptions}
            onChange={(v) => {
              setKind(v);
              setPage(1);
            }}
          />
        }
      />
      {sessions.isLoading ? (
        <Loading />
      ) : sessions.error || !sessions.data ? (
        <ErrorBlock error={sessions.error} onRetry={() => sessions.refetch()} />
      ) : sessions.data.items.length === 0 ? (
        <EmptyBlock title="还没有学习记录" description={kind === "all" ? "完成一组学习后会出现在这里" : "该类型暂无记录"} />
      ) : (
        <>
          <div style={{ display: "flex", flexDirection: "column" }}>
            {sessions.data.items.map((it) => (
              <SessionRow key={it.id} item={it} userId={userId} />
            ))}
          </div>
          {sessions.data.total > PAGE_SIZE && (
            <div style={{ display: "flex", justifyContent: "center", marginTop: 12 }}>
              <Pagination size="small" current={page} pageSize={PAGE_SIZE} total={sessions.data.total} onChange={setPage} showSizeChanger={false} />
            </div>
          )}
        </>
      )}
    </div>
  );
}

function SessionRow({ item, userId }: { item: SessionListItem; userId?: string }) {
  const r = item.result;
  return (
    <Link
      to={withUser(`/records/sessions/${item.id}`, userId)}
      style={{ display: "flex", alignItems: "center", gap: 12, padding: "12px 4px", borderTop: "1px solid var(--line)", color: "inherit" }}
    >
      <div style={{ flex: 1, minWidth: 0 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 6, flexWrap: "wrap" }}>
          <KindTag kind={item.kind} />
          <span style={{ fontWeight: 600, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{item.planName ?? "不限计划"}</span>
          {item.status === "active" && (
            <Tag color="blue" bordered={false}>
              进行中
            </Tag>
          )}
        </div>
        <div style={{ fontSize: 12, color: "var(--muted)", marginTop: 4 }}>
          {fmtDay(item.dayKey)} {fmtTime(item.startedAt)} · {item.words} 词 · {item.answers} 题
        </div>
      </div>
      <div style={{ textAlign: "right", flexShrink: 0 }}>
        {r ? (
          <>
            <div className="vx-num" style={{ fontSize: 18, fontWeight: 600 }}>
              {percent(r.accuracy)}
            </div>
            <div style={{ fontSize: 12, color: "var(--muted)" }}>{r.newLearned > 0 ? `新学入库 ${r.newLearned} 词` : "首答正确率"}</div>
          </>
        ) : (
          <span style={{ fontSize: 12, color: "var(--muted)" }}>未完成</span>
        )}
      </div>
      <RightOutlined style={{ color: "var(--muted)", fontSize: 12 }} aria-hidden />
    </Link>
  );
}
