import { useState } from "preact/hooks";
import { Alert, Button, Col, Row, Table, Tag, type ColumnType } from "@/ui";
import { coverageRateOrder, testedRate } from "@/pages/coverage/coverage";
import { useClassTargets } from "./ClassTargetsTab";
import { useQuery } from "@/lib/query";
import { Link } from "@/lib/router";
import { api } from "@/lib/api";
import type { ClassOverview, StudentTodayStatus } from "@/types";
import { EmptyBlock, ErrorBlock, Loading, StatTile, percent } from "@/components/ui";
import { SERIES } from "@/components/charts";

type StudentRow = ClassOverview["students"][number];

const STATUS_META: Record<StudentTodayStatus, { label: string; color: string; order: number }> = {
  "not-started": { label: "未开始", color: "orange", order: 0 },
  "in-progress": { label: "进行中", color: "blue", order: 1 },
  done: { label: "已完成", color: "green", order: 2 },
  "no-plan": { label: "无计划", color: "default", order: 3 },
};

/** 班级今日概览 */
export function ClassOverviewTab({ classId, onSetTargets }: { classId: string; onSetTargets?: () => void }) {
  const query = useQuery({
    queryKey: ["classes", classId, "overview"],
    queryFn: () => api.get<ClassOverview>(`/classes/${classId}/overview`),
  });
  const targets = useClassTargets(classId);

  if (query.isLoading) return <Loading />;
  if (query.error || !query.data) return <ErrorBlock error={query.error} onRetry={() => query.refetch()} />;
  const o = query.data;
  const memberCount = o.class.memberCount;

  const columns: ColumnType<StudentRow>[] = [
    {
      title: "姓名",
      dataIndex: "name",
      fixed: "left",
      width: 110,
      sorter: (a, b) => a.name.localeCompare(b.name, "zh-CN"),
      render: (_, r) => <Link to={`/classes/${classId}/students/${r.userId}`}>{r.name}</Link>,
    },
    {
      title: "今日状态",
      key: "status",
      width: 100,
      defaultSortOrder: "ascend",
      sorter: (a, b) => STATUS_META[a.today.status].order - STATUS_META[b.today.status].order,
      render: (_, r) => (
        <Tag color={STATUS_META[r.today.status].color} bordered={false}>
          {STATUS_META[r.today.status].label}
        </Tag>
      ),
    },
    {
      title: "今日新学",
      key: "new",
      width: 100,
      sorter: (a, b) => a.today.newDone - b.today.newDone,
      render: (_, r) => <DoneLeft done={r.today.newDone} left={r.today.newLeft} />,
    },
    {
      title: "今日复习",
      key: "review",
      width: 100,
      sorter: (a, b) => a.today.reviewDone - b.today.reviewDone,
      render: (_, r) => <DoneLeft done={r.today.reviewDone} left={r.today.reviewLeft} />,
    },
    { title: "连续天数", dataIndex: "streak", width: 90, sorter: (a, b) => a.streak - b.streak, render: (v: number) => `${v} 天` },
    {
      title: "近 7 天活跃",
      dataIndex: "activeDays7",
      width: 100,
      sorter: (a, b) => a.activeDays7 - b.activeDays7,
      render: (v: number) => `${v}/7 天`,
    },
    {
      title: "正确率",
      key: "acc",
      width: 90,
      sorter: (a, b) => (a.accuracy7d.rate ?? -1) - (b.accuracy7d.rate ?? -1),
      render: (_, r) => <span title={`近 7 天首答 ${r.accuracy7d.correct}/${r.accuracy7d.total} 题`}>{percent(r.accuracy7d.rate)}</span>,
    },
    {
      title: "目标覆盖",
      key: "coverage",
      width: 100,
      sorter: (a, b) => coverageRateOrder(a.coverage) - coverageRateOrder(b.coverage),
      render: (_, r) =>
        r.coverage ? (
          <span title={`已测 ${r.coverage.tested}/${r.coverage.target} 词`}>{percent(testedRate(r.coverage))}</span>
        ) : (
          <span style={{ color: "var(--muted)" }}>—</span>
        ),
    },
    {
      title: "要学",
      key: "learning",
      width: 80,
      sorter: (a, b) => (a.coverage?.learning ?? -1) - (b.coverage?.learning ?? -1),
      render: (_, r) => (r.coverage ? <span className="vx-num">{r.coverage.learning}</span> : <span style={{ color: "var(--muted)" }}>—</span>),
    },
    { title: "已学词", dataIndex: "learnedWords", width: 80, sorter: (a, b) => a.learnedWords - b.learnedWords },
    {
      title: "最近学习",
      dataIndex: "lastActiveDay",
      width: 110,
      sorter: (a, b) => (a.lastActiveDay ?? "").localeCompare(b.lastActiveDay ?? ""),
      render: (v: string | null) => v ?? <span style={{ color: "var(--muted)" }}>从未</span>,
    },
  ];

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
      {targets.data && targets.data.items.length === 0 && (
        <Alert
          type="info"
          showIcon
          message="还没有设置目标词书"
          description="设置后，学生的今日页会出现目标进度，这里会显示每个学生的目标覆盖和要学的词数。"
          action={
            onSetTargets && (
              <Button size="small" type="primary" onClick={onSetTargets}>
                设置目标词书
              </Button>
            )
          }
        />
      )}
      <Row gutter={[12, 12]}>
        <Col xs={12} sm={8} md={4}>
          <StatTile label="今日完成" value={`${o.summary.doneToday}/${memberCount}`} suffix="人" tone="primary" />
        </Col>
        <Col xs={12} sm={8} md={5}>
          <StatTile label="学习中" value={o.summary.inProgress} suffix="人" />
        </Col>
        <Col xs={12} sm={8} md={5}>
          <StatTile label="未开始" value={o.summary.notStarted} suffix="人" tone="accent" />
        </Col>
        <Col xs={12} sm={8} md={5}>
          <StatTile label="无计划" value={o.summary.noPlan} suffix="人" />
        </Col>
        <Col xs={24} sm={16} md={5}>
          <StatTile
            label="近 7 天首答正确率"
            value={percent(o.summary.accuracy7d.rate)}
            hint={`${o.summary.accuracy7d.correct}/${o.summary.accuracy7d.total} 题`}
          />
        </Col>
      </Row>

      <div className="vx-card" style={{ padding: 16 }}>
        <h3 className="vx-title" style={{ fontSize: 17, marginBottom: 12 }}>
          学生今日进度 <span style={{ fontSize: 13, fontWeight: 400, color: "var(--muted)" }}>{o.day}</span>
        </h3>
        {o.students.length === 0 ? (
          <EmptyBlock title="班级还没有学生" description="在「成员」页把邀请码发给学生，或批量创建学生账号" />
        ) : (
          <Table<StudentRow> rowKey="userId" size="middle" columns={columns} dataSource={o.students} pagination={false} scroll={{ x: 1060 }} />
        )}
      </div>

      <Row gutter={[16, 16]}>
        <Col xs={24} md={12}>
          <div className="vx-card" style={{ padding: 16, height: "100%" }}>
            <h3 className="vx-title" style={{ fontSize: 17, marginBottom: 4 }}>
              班级难词榜
            </h3>
            <div style={{ fontSize: 12, color: "var(--muted)", marginBottom: 8 }}>近 14 天全班答错最多的词</div>
            {o.hardWords.length === 0 ? (
              <div style={{ color: "var(--muted)", padding: "16px 0" }}>暂无错题</div>
            ) : (
              o.hardWords.map((w, i) => (
                <div key={w.wordId} style={{ display: "flex", alignItems: "center", gap: 10, padding: "8px 0", borderTop: i ? "1px solid var(--line)" : "none" }}>
                  <span className="vx-num" style={{ width: 20, color: "var(--muted)" }}>
                    {i + 1}
                  </span>
                  <div style={{ flex: 1, minWidth: 0 }}>
                    <div className="vx-word" style={{ fontWeight: 600 }}>
                      {w.spelling}
                    </div>
                    <div className="vx-cn" style={{ fontSize: 12, color: "var(--ink-soft)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                      {w.definition}
                    </div>
                  </div>
                  <div style={{ textAlign: "right", flexShrink: 0 }}>
                    <div style={{ color: "var(--bad)", fontWeight: 600 }}>{percent(w.rate)}</div>
                    <div style={{ fontSize: 12, color: "var(--muted)" }}>
                      错 {w.wrong}/{w.total} 次
                    </div>
                  </div>
                </div>
              ))
            )}
          </div>
        </Col>
        <Col xs={24} md={12}>
          <div className="vx-card" style={{ padding: 16, height: "100%" }}>
            <h3 className="vx-title" style={{ fontSize: 17, marginBottom: 4 }}>
              近 14 天活跃人数
            </h3>
            <div style={{ fontSize: 12, color: "var(--muted)", marginBottom: 8 }}>当天有作答的学生数（共 {memberCount} 人）</div>
            <ActiveChart data={o.activeByDay} max={memberCount} />
          </div>
        </Col>
      </Row>
    </div>
  );
}

function DoneLeft({ done, left }: { done: number; left: number }) {
  return (
    <span>
      <span className="vx-num" style={{ fontWeight: 600 }}>{done}</span>
      {left > 0 && <span style={{ color: "var(--muted)", fontSize: 12 }}> 剩 {left}</span>}
    </span>
  );
}

/** 单序列柱图：规格同 charts.tsx（柱宽 ≤ 24、顶端 4px 圆角、细网格、悬停提示） */
function ActiveChart({ data, max }: { data: ClassOverview["activeByDay"]; max: number }) {
  const [hover, setHover] = useState<number | null>(null);
  const width = 420;
  const height = 150;
  const pad = { top: 10, right: 6, bottom: 22, left: 26 };
  const innerW = width - pad.left - pad.right;
  const innerH = height - pad.top - pad.bottom;
  const top = Math.max(1, max, ...data.map((d) => d.activeStudents));
  const band = innerW / Math.max(1, data.length);
  const barW = Math.min(24, band * 0.62);
  const y = (v: number) => pad.top + innerH - (v / top) * innerH;
  const h = hover !== null ? data[hover] : null;

  if (data.length === 0) return <div style={{ color: "var(--muted)" }}>暂无数据</div>;

  return (
    <div>
      <svg viewBox={`0 0 ${width} ${height}`} width="100%" role="img" aria-label="近 14 天每日活跃学生数" onMouseLeave={() => setHover(null)} style={{ display: "block" }}>
        {[0, top].map((t) => (
          <g key={t}>
            <line x1={pad.left} x2={width - pad.right} y1={y(t)} y2={y(t)} style={{ stroke: "var(--chart-grid)" }} strokeWidth={1} />
            <text x={pad.left - 6} y={y(t) + 4} textAnchor="end" fontSize={11} style={{ fill: "var(--chart-axis)" }}>
              {t}
            </text>
          </g>
        ))}
        {data.map((d, i) => {
          const cx = pad.left + band * i + band / 2;
          const x = cx - barW / 2;
          const yt = y(d.activeStudents);
          const bh = y(0) - yt;
          const r = Math.min(4, barW / 2, bh);
          const [, m, dd] = d.day.split("-");
          return (
            <g key={d.day} onMouseEnter={() => setHover(i)}>
              <rect x={pad.left + band * i} y={pad.top} width={band} height={innerH} style={{ fill: hover === i ? "var(--chart-hover)" : "transparent" }} />
              {bh > 0 && (
                <path d={`M${x},${yt + bh} V${yt + r} Q${x},${yt} ${x + r},${yt} H${x + barW - r} Q${x + barW},${yt} ${x + barW},${yt + r} V${yt + bh} Z`} style={{ fill: SERIES.reviewedWords }} />
              )}
              {i % 2 === 0 && (
                <text x={cx} y={height - 6} textAnchor="middle" fontSize={10} style={{ fill: "var(--chart-axis)" }}>
                  {`${Number(m)}/${Number(dd)}`}
                </text>
              )}
            </g>
          );
        })}
        <line x1={pad.left} x2={width - pad.right} y1={y(0)} y2={y(0)} style={{ stroke: "var(--line-strong)" }} strokeWidth={1} />
      </svg>
      <div role="status" style={{ fontSize: 12, color: "var(--ink-soft)", minHeight: 18, marginTop: 4 }}>
        {h ? `${h.day}：${h.activeStudents} 人活跃 · ${h.answers} 题 · 首答正确率 ${percent(h.accuracy)}` : "悬停柱子查看当天详情"}
      </div>
    </div>
  );
}
