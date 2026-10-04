import { useState } from "preact/hooks";
import { Link, useNavigate } from "@/lib/router";
import { useQuery, useQueryClient } from "@/lib/query";
import { useIdentity } from "@/lib/auth";
import { Alert, Button, Col, Row, Tag, useApp } from "@/ui";
import { CheckCircleFilled, EditOutlined, FireFilled, PlayCircleFilled, PlusOutlined, PrinterOutlined, ThunderboltOutlined } from "@/ui";
import { gradeLink, todaySheetTitle } from "@/pages/sheets/dictation";
import { api, errorMessage } from "@/lib/api";
import { can } from "@/lib/perms";
import { SELF_PLAN_CLOSED_TEXT, useMyTargets } from "@/lib/targets";
import { EmptyBlock, ErrorBlock, Loading, PageHeader, StatTile } from "@/components/ui";
import { ProgressRing } from "@/components/charts";
import type { CoverageData, SessionKind, TodayData, TodayPlanCard } from "@/types";
import { coverageLine } from "@/pages/coverage/components";
import { targetSheetLink } from "@/pages/coverage/coverage";
import { preloadStudyPage } from "@/pages/study/lazy";
import { MODE_LABEL } from "@/types";

/** 开始 / 继续单词单测试（与旧版 pages/sheets/SheetsList.tsx 的 startSheetTest 相同；单词单页面迁移后可改为从那里引用） */
async function startSheetTest(sheetId: string): Promise<string> {
  const res = await api.post<{ id: string }>("/study/sessions", { kind: "sheet", sheetId });
  return res.id;
}

function greeting() {
  const h = new Date().getHours();
  if (h < 6) return "夜深了";
  if (h < 11) return "早上好";
  if (h < 14) return "中午好";
  if (h < 18) return "下午好";
  return "晚上好";
}

function formatDay(day: string) {
  const [y, m, d] = day.split("-").map(Number);
  const week = "日一二三四五六"[new Date(y, m - 1, d).getDay()];
  return `${m} 月 ${d} 日 · 星期${week}`;
}

/** 今日：按计划实时算出的学习队列 */
export function TodayPage() {
  const { data: identity } = useIdentity();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const { message } = useApp();
  const [starting, setStarting] = useState<string | null>(null);
  const q = useQuery({ queryKey: ["today"], queryFn: () => api.get<TodayData>("/today"), refetchOnWindowFocus: true });
  // spec 0008：班级未开放自主安排时，「自己安排计划」不可用（能给别人布置的老师、管理员不受影响）
  const targets = useMyTargets(can(identity, "plans") && !can(identity, "plans.assign"));
  const selfClosed = !can(identity, "plans.assign") && targets.data?.canEditOwn === false;

  const start = async (kind: SessionKind, planId: string | null) => {
    const key = `${kind}:${planId}`;
    setStarting(key);
    try {
      const [res] = await Promise.all([api.post<{ id: string }>("/study/sessions", { kind, planId }), preloadStudyPage()]);
      await qc.invalidateQueries({ queryKey: ["today"] });
      navigate(`/study/${res.id}`);
    } catch (e) {
      message.info(errorMessage(e));
      q.refetch();
    } finally {
      setStarting(null);
    }
  };

  const startSheet = async (sheetId: string) => {
    setStarting(`sheet:${sheetId}`);
    try {
      const [sid] = await Promise.all([startSheetTest(sheetId), preloadStudyPage()]);
      await qc.invalidateQueries({ queryKey: ["today"] });
      navigate(`/study/${sid}`);
    } catch (e) {
      message.info(errorMessage(e));
      q.refetch();
    } finally {
      setStarting(null);
    }
  };

  if (q.isLoading) return <div className="vx-page"><Loading /></div>;
  if (q.isError || !q.data) return <div className="vx-page"><ErrorBlock error={q.error} onRetry={() => q.refetch()} /></div>;
  const t = q.data;
  const outside = new Set(t.outsideTargetPlanIds ?? []);
  const pending = t.plans.filter((p) => !p.doneToday);
  const done = t.plans.filter((p) => p.doneToday);
  const allDone = t.plans.length > 0 && pending.length === 0;

  return (
    <div className="vx-page">
      <PageHeader
        eyebrow={formatDay(t.day)}
        title={`${greeting()}，${identity?.name ?? "同学"}`}
        extra={
          can(identity, "plans") &&
          (selfClosed ? (
            <Button icon={<PlusOutlined />} disabled title={SELF_PLAN_CLOSED_TEXT}>
              自己安排计划
            </Button>
          ) : (
            <Link to="/plans/new">
              <Button icon={<PlusOutlined />}>自己安排计划</Button>
            </Link>
          ))
        }
      >
        {t.plans.length === 0
          ? "还没有生效的学习计划。"
          : allDone
            ? "今天的任务都完成了，明天见！"
            : `今天还有 ${t.totals.newLeft} 个新词、${t.totals.reviewLeft} 个复习词${t.totals.pendingTests ? `、${t.totals.pendingTests} 个检测` : ""}。`}
      </PageHeader>

      <Row gutter={[12, 12]} style={{ marginBottom: 20 }} className="vx-rise">
        <Col xs={12} sm={6}>
          <StatTile label="连续学习" value={<span>{t.streak > 0 && <FireFilled style={{ color: "var(--accent)", fontSize: 22, marginRight: 4 }} />}{t.streak}</span>} suffix="天" tone={t.streak > 0 ? "accent" : "ink"} />
        </Col>
        <Col xs={12} sm={6}>
          <StatTile label="今日新学" value={t.stats.newWords} suffix="词" />
        </Col>
        <Col xs={12} sm={6}>
          <StatTile label="今日复习" value={t.stats.reviewedWords} suffix="词" />
        </Col>
        <Col xs={12} sm={6}>
          <StatTile label="累计已学" value={t.learnedWords} suffix="词" tone="primary" hint={`今天作答 ${t.stats.answers} 题 · ${t.stats.minutes} 分钟`} />
        </Col>
      </Row>

      <TargetProgressCard />

      {(t.pausedSelfPlans ?? 0) > 0 && (
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 14 }}
          message={`你自己安排的 ${t.pausedSelfPlans} 份计划暂停中`}
          description="所在班级未开放自主安排计划，自建的计划暂时不出现在今日任务里；老师重新允许后自动恢复，之前的学习记录都保留。"
        />
      )}

      {t.plans.length === 0 ? (
        <EmptyBlock
          title="今天没有学习任务"
          description={
            can(identity, "plans") && !selfClosed
              ? "老师布置的计划会自动出现在这里；你也可以从词书里挑几个单元，给自己安排一个计划。"
              : "老师布置的计划会自动出现在这里。"
          }
          action={
            <div style={{ display: "flex", gap: 8, justifyContent: "center", flexWrap: "wrap" }}>
              {can(identity, "plans") && !selfClosed && (
                <Link to="/plans/new">
                  <Button type="primary">给自己安排计划</Button>
                </Link>
              )}
              <Link to="/profile">
                <Button>输入班级邀请码</Button>
              </Link>
            </div>
          }
        />
      ) : (
        <div style={{ display: "grid", gap: 14 }}>
          {pending.map((p, i) => (
            <PlanCard key={p.planId} plan={p} index={i} starting={starting} onStart={start} outsideTarget={outside.has(p.planId)} />
          ))}
          {done.length > 0 && (
            <>
              <div className="vx-eyebrow" style={{ marginTop: 8 }}>
                <CheckCircleFilled style={{ color: "var(--good)" }} /> 今日已完成
              </div>
              {done.map((p, i) => (
                <PlanCard key={p.planId} plan={p} index={i} starting={starting} onStart={start} outsideTarget={outside.has(p.planId)} />
              ))}
            </>
          )}
        </div>
      )}

      {t.sheet && t.sheet.format === "dictation" && (
        <div className="vx-card vx-rise" style={{ marginTop: 20, padding: "16px 18px", display: "flex", alignItems: "center", gap: 14, flexWrap: "wrap" }}>
          <EditOutlined style={{ fontSize: 24, color: "var(--primary)" }} />
          <div style={{ flex: 1, minWidth: 180 }}>
            <div style={{ fontWeight: 600 }}>
              {todaySheetTitle(t.sheet)}
              {t.sheet.remaining > 0 && `（还有 ${t.sheet.remaining} 份待完成）`}
            </div>
            <div style={{ color: "var(--muted)", fontSize: 13 }}>纸上默写完了吗？对照答案页逐题批改，错的词和句子会进「要学」。</div>
          </div>
          <Link to={gradeLink(t.sheet.id)}>
            <Button type="primary">批改</Button>
          </Link>
        </div>
      )}

      {(t.gradedSheets ?? []).map((g) => (
        <div key={g.id} className="vx-card vx-rise" style={{ marginTop: 20, padding: "16px 18px", display: "flex", alignItems: "center", gap: 14, flexWrap: "wrap" }}>
          <CheckCircleFilled style={{ fontSize: 24, color: "var(--good)" }} />
          <div style={{ flex: 1, minWidth: 180 }}>
            <div style={{ fontWeight: 600 }}>
              默写单 #{g.seq} · 已批改
              {g.selfGraded && (
                <Tag color="orange" bordered={false} style={{ marginLeft: 8 }}>
                  自批
                </Tag>
              )}
            </div>
            <div style={{ color: "var(--muted)", fontSize: 13 }}>
              成绩 {g.correct}/{g.total}
              {g.correct < g.total ? "，错的词和句子已经进了「要学」。" : "，全对！"}
            </div>
          </div>
          <Link to={gradeLink(g.id)}>
            <Button>查看成绩</Button>
          </Link>
        </div>
      ))}

      {t.sheet && t.sheet.format !== "dictation" && (
        <div className="vx-card vx-rise" style={{ marginTop: 20, padding: "16px 18px", display: "flex", alignItems: "center", gap: 14, flexWrap: "wrap" }}>
          <PrinterOutlined style={{ fontSize: 24, color: "var(--primary)" }} />
          <div style={{ flex: 1, minWidth: 180 }}>
            <div style={{ fontWeight: 600 }}>
              单词单 #{t.sheet.seq} · {t.sheet.wordCount} 词{t.sheet.remaining > 0 && `（还有 ${t.sheet.remaining} 份待测）`}
            </div>
            <div style={{ color: "var(--muted)", fontSize: 13 }}>纸上看过了吗？测一遍，答错的词会优先进下一张。</div>
          </div>
          <Button type="primary" loading={starting === `sheet:${t.sheet.id}`} onClick={() => startSheet(t.sheet!.id)}>
            {t.sheet.activeSessionId ? "继续测试" : "开始测试"}
          </Button>
        </div>
      )}

      {t.drillAvailable > 0 && (
        <div className="vx-card vx-rise" style={{ marginTop: 20, padding: "16px 18px", display: "flex", alignItems: "center", gap: 14, flexWrap: "wrap" }}>
          <ThunderboltOutlined style={{ fontSize: 24, color: "var(--accent)" }} />
          <div style={{ flex: 1, minWidth: 180 }}>
            <div style={{ fontWeight: 600 }}>错词强化</div>
            <div style={{ color: "var(--muted)", fontSize: 13 }}>最近答错或遗忘过的 {t.drillAvailable} 个词，随时加练，不影响复习安排。</div>
          </div>
          <Button loading={starting === "drill:null"} onClick={() => start("drill", null)}>
            开始加练
          </Button>
        </div>
      )}
    </div>
  );
}

/** 目标进度（spec 0003）：已测 / 应测的进度环，会了 · 要学 · 未测，一键出单词单；没有目标时不显示 */
function TargetProgressCard() {
  const q = useQuery({ queryKey: ["records", "me", "coverage"], queryFn: () => api.get<CoverageData>("/records/coverage"), refetchOnWindowFocus: true });
  const c = q.data;
  if (!c || c.books.length === 0) return null;
  const { total } = c;
  return (
    <div className="vx-card vx-rise" style={{ padding: 18, marginBottom: 14 }} aria-label="目标进度">
      <div style={{ display: "flex", gap: 14, alignItems: "center" }}>
        <ProgressRing value={total.tested} total={total.target} label={`已测 ${total.tested}/${total.target}`} />
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ display: "flex", alignItems: "baseline", gap: 8, flexWrap: "wrap" }}>
            <Link to="/records" className="vx-title" style={{ fontSize: 18 }}>
              目标进度
            </Link>
            <span style={{ color: "var(--ink-soft)", fontSize: 14 }}>
              已测 <b className="vx-num">{total.tested}</b> / {total.target}
            </span>
          </div>
          <div style={{ color: "var(--muted)", fontSize: 13, marginTop: 4 }}>{coverageLine(total)}</div>
        </div>
      </div>
      <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(160px, 1fr))", gap: 10, marginTop: 14 }}>
        <Link to={targetSheetLink("untested")} style={total.untested === 0 ? { pointerEvents: "none" } : undefined}>
          <Button block type="primary" disabled={total.untested === 0}>
            测未测的词
          </Button>
        </Link>
        <Link to={targetSheetLink("learning")} style={total.learning === 0 ? { pointerEvents: "none" } : undefined}>
          <Button block disabled={total.learning === 0}>
            练要学的词
          </Button>
        </Link>
      </div>
    </div>
  );
}

function PlanCard({
  plan: p,
  index,
  starting,
  onStart,
  outsideTarget,
}: {
  plan: TodayPlanCard;
  index: number;
  starting: string | null;
  onStart: (k: SessionKind, planId: string) => void;
  /** 计划有单元所在的书不在我的目标词书内（spec 0008） */
  outsideTarget?: boolean;
}) {
  const active = (kind: SessionKind) => p.activeSessions.find((s) => s.kind === kind);
  const loading = (kind: SessionKind) => starting === `${kind}:${p.planId}`;

  const actions: { kind: SessionKind; label: string; sub: string; primary: boolean; disabled: boolean }[] = [];
  if (p.kind === "daily") {
    const learn = active("learn");
    actions.push({
      kind: "learn",
      label: learn ? "继续学新词" : "学新词",
      sub: learn ? `这一组 ${learn.total} 词` : p.newLeft > 0 ? `还剩 ${p.newLeft} 个 · 每组最多 10 个` : p.newAvailable === 0 ? "范围内的词都学过了" : `今天 ${p.newDoneToday}/${p.newPerDay} 已完成`,
      primary: !!learn || p.newLeft > 0,
      disabled: !learn && p.newLeft === 0,
    });
    const review = active("review");
    actions.push({
      kind: "review",
      label: review ? "继续复习" : "复习",
      sub: review ? `这一组 ${review.total} 词` : p.reviewLeft > 0 ? `${p.reviewLeft} 个到期` : p.reviewDoneToday > 0 ? `今天已复习 ${p.reviewDoneToday} 个` : "暂无到期词",
      primary: !!review || (p.reviewLeft > 0 && p.newLeft === 0),
      disabled: !review && p.reviewLeft === 0,
    });
  } else {
    const test = active("test");
    actions.push({
      kind: "test",
      label: test ? "继续检测" : p.testResult ? "再测一次" : "开始检测",
      sub: test ? `共 ${test.total} 词` : p.testResult ? `上次 ${p.testResult.correct}/${p.testResult.total} 题正确` : `随机抽 ${p.testSize} 词，交卷后看成绩`,
      primary: !!test || !p.testResult,
      disabled: false,
    });
  }

  return (
    <div className="vx-card vx-rise" style={{ padding: 18, animationDelay: `${index * 60}ms`, opacity: p.doneToday ? 0.86 : 1 }}>
      <div style={{ display: "flex", gap: 14, alignItems: "flex-start" }}>
        <ProgressRing value={p.learnedWords} total={p.totalWords} label={`已学 ${p.learnedWords}/${p.totalWords}`} />
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
            <Link to={`/plans/${p.planId}`} className="vx-title" style={{ fontSize: 18 }}>
              {p.name}
            </Link>
            {p.kind === "test" && <Tag color="geekblue" bordered={false}>检测</Tag>}
            {p.doneToday && <Tag color="green" bordered={false}>今日完成</Tag>}
            {outsideTarget && (
              <span title="计划里有单元所在的词书不在你的目标词书内，这部分进度不计入目标">
<Tag bordered={false}>
                不在目标词书内
              </Tag>
</span>
            )}
          </div>
          <div style={{ color: "var(--muted)", fontSize: 13, marginTop: 4 }}>
            {p.source === "assigned" ? `${p.creatorName} 安排` : "自己安排"} · 已学 {p.learnedWords}/{p.totalWords} 词 · {p.modes.map((m) => MODE_LABEL[m]).join(" + ")}
          </div>
        </div>
      </div>
      <div style={{ display: "grid", gridTemplateColumns: `repeat(auto-fit, minmax(200px, 1fr))`, gap: 10, marginTop: 14 }}>
        {actions.map((a) => (
          <Button
            key={a.kind}
            type={a.primary ? "primary" : "default"}
            size="large"
            disabled={a.disabled}
            loading={loading(a.kind)}
            onClick={() => onStart(a.kind, p.planId)}
            style={{ height: "auto", padding: "10px 16px", textAlign: "left", display: "flex", alignItems: "center", gap: 10 }}
          >
            {!a.disabled && <PlayCircleFilled style={{ fontSize: 22 }} />}
            <span style={{ display: "flex", flexDirection: "column", lineHeight: 1.35, minWidth: 0 }}>
              <span style={{ fontWeight: 600 }}>{a.label}</span>
              <span style={{ fontSize: 12, opacity: 0.85, whiteSpace: "normal" }}>{a.sub}</span>
            </span>
          </Button>
        ))}
      </div>
    </div>
  );
}
