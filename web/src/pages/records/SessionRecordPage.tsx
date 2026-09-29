import { Button, Col, Row, Tag } from "@/ui";
import { ArrowLeftOutlined } from "@/ui";
import { useQuery } from "@/lib/query";
import { useIdentity } from "@/lib/auth";
import { Link, useNavigate, useParams } from "@/lib/router";
import { api } from "@/lib/api";
import type { SessionRecordDetail } from "@/types";
import { MODE_LABEL, RATING_LABEL } from "@/types";
import { EmptyBlock, ErrorBlock, KindTag, Loading, PageHeader, SpeakButton, StatTile, formatDuration, percent } from "@/components/ui";
import { AnswerChip, PHASE_LABEL, fmtDate, fmtDateTime, withUser } from "./shared";

const RATING_KEYS: { key: string; label: string }[] = [
  { key: "again", label: "忘记" },
  { key: "hard", label: "模糊" },
  { key: "good", label: "记得" },
  { key: "easy", label: "熟练" },
];

const PHASE_ORDER = ["practice", "consolidate", "test"];

/** 学习组详情：结果汇总 + 逐词作答 */
export function SessionRecordPage() {
  const { id = "" } = useParams();
  const navigate = useNavigate();
  const { data: identity } = useIdentity();
  const query = useQuery({
    queryKey: ["records", "session", id],
    queryFn: () => api.get<SessionRecordDetail>(`/records/sessions/${id}`),
    enabled: !!id,
  });

  const back = (
    <Button icon={<ArrowLeftOutlined />} onClick={() => navigate(-1)}>
      返回
    </Button>
  );

  if (query.isLoading) return <Loading />;
  if (query.error || !query.data)
    return (
      <div className="vx-page">
        <PageHeader title="学习组详情" extra={back} />
        <ErrorBlock error={query.error} onRetry={() => query.refetch()} />
      </div>
    );

  const s = query.data;
  const r = s.result;
  const isSelf = identity?.id === s.user.id;
  const viewUserId = isSelf ? undefined : s.user.id;
  const durationMs = r?.durationMs ?? (s.completedAt ? new Date(s.completedAt).getTime() - new Date(s.startedAt).getTime() : 0);

  return (
    <div className="vx-page">
      <PageHeader
        eyebrow={isSelf ? "学习组详情" : `${s.user.name} 的学习组`}
        title={
          <span style={{ display: "inline-flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
            <KindTag kind={s.kind} />
            {s.planName ?? "不限计划"}
          </span>
        }
        extra={back}
      >
        {fmtDateTime(s.startedAt)}
        {s.completedAt ? ` — ${fmtDateTime(s.completedAt)}` : ""} · 用时 {formatDuration(durationMs)} · {s.modes.map((m) => MODE_LABEL[m]).join(" + ")}
        {s.status === "active" && (
          <Tag color="blue" bordered={false} style={{ marginLeft: 8 }}>
            进行中
          </Tag>
        )}
      </PageHeader>

      <Row gutter={[12, 12]} style={{ marginBottom: 16 }}>
        <Col xs={12} sm={6}>
          <StatTile label="词数" value={r?.words ?? s.words.length} suffix="词" />
        </Col>
        <Col xs={12} sm={6}>
          <StatTile label="首答正确率" value={percent(r?.accuracy)} tone="primary" hint={r ? `首答 ${r.correctFirst}/${r.totalFirst} 题` : "尚未完成"} />
        </Col>
        <Col xs={12} sm={6}>
          <StatTile label="新学入库" value={r?.newLearned ?? 0} suffix="词" tone="accent" />
        </Col>
        <Col xs={12} sm={6}>
          <div className="vx-card" style={{ padding: "14px 16px", height: "100%" }}>
            <div style={{ fontSize: 13, color: "var(--muted)" }}>评分分布</div>
            <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: "2px 12px", marginTop: 6, fontSize: 13 }}>
              {RATING_KEYS.map((k) => (
                <span key={k.key}>
                  {k.label} <span className="vx-num" style={{ fontWeight: 600 }}>{r?.ratings?.[k.key] ?? 0}</span>
                </span>
              ))}
            </div>
          </div>
        </Col>
      </Row>

      {s.words.length === 0 ? (
        <EmptyBlock title="这一组没有单词" />
      ) : (
        <div className="vx-card" style={{ padding: "4px 16px" }}>
          {s.words.map((w, i) => {
            const phases = PHASE_ORDER.filter((p) => w.answers.some((a) => a.phase === p)).concat(
              [...new Set(w.answers.map((a) => a.phase))].filter((p) => !PHASE_ORDER.includes(p)),
            );
            return (
              <div key={w.wordId} style={{ padding: "14px 0", borderTop: i ? "1px solid var(--line)" : "none" }}>
                <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
                  <Link to={withUser(`/words/${w.wordId}`, viewUserId)} className="vx-word" style={{ fontSize: 20, fontWeight: 600, color: "var(--ink)" }}>
                    {w.spelling}
                  </Link>
                  <SpeakButton text={w.spelling} wordId={w.wordId} size="small" />
                  {w.phonetic && <span style={{ fontSize: 12, color: "var(--muted)" }}>{w.phonetic}</span>}
                  {w.review && (
                    <span style={{ marginLeft: "auto", fontSize: 12, color: "var(--ink-soft)" }}>
                      <Tag bordered={false} color="cyan" style={{ marginRight: 6 }}>
                        {RATING_LABEL[w.review.rating] ?? w.review.rating}
                      </Tag>
                      下次复习 {fmtDate(w.review.dueAfter)}
                    </span>
                  )}
                </div>
                <div className="vx-cn" style={{ color: "var(--ink-soft)", marginTop: 2 }}>
                  {w.partOfSpeech && <span style={{ color: "var(--muted)", marginRight: 6 }}>{w.partOfSpeech}</span>}
                  {w.definition}
                </div>
                {w.answers.length === 0 ? (
                  <div style={{ fontSize: 12, color: "var(--muted)", marginTop: 8 }}>未作答</div>
                ) : (
                  <div style={{ display: "flex", flexDirection: "column", gap: 6, marginTop: 8 }}>
                    {phases.map((p) => (
                      <div key={p} style={{ display: "flex", alignItems: "center", gap: 6, flexWrap: "wrap" }}>
                        <span style={{ fontSize: 12, color: "var(--muted)", width: 32, flexShrink: 0 }}>{PHASE_LABEL[p] ?? p}</span>
                        {w.answers
                          .filter((a) => a.phase === p)
                          .map((a, j) => (
                            <AnswerChip key={j} mode={a.mode} correct={a.correct} userAnswer={a.userAnswer} hintUsed={a.hintUsed} dontKnow={a.dontKnow} />
                          ))}
                      </div>
                    ))}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
