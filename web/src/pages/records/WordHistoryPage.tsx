import type { ComponentChildren } from "preact";
import { Button, Col, Row, Tag, Timeline } from "@/ui";
import { ArrowLeftOutlined } from "@/ui";
import { useQuery } from "@/lib/query";
import { useNavigate, useParams, useSearchParams } from "@/lib/router";
import { api } from "@/lib/api";
import type { WordHistory } from "@/types";
import { RATING_LABEL } from "@/types";
import { ErrorBlock, Loading, MasteryTag, PageHeader, SpeakButton, StatTile } from "@/components/ui";
import { AnswerChip, PHASE_LABEL, fmtDate, fmtDay, fmtTime, withUser } from "./shared";

interface TimelineEntry {
  key: string;
  at: string;
  node: ComponentChildren;
}

/** 单词轨迹：词卡 + 记忆状态 + 复习/作答时间线 */
export function WordHistoryPage() {
  const { wordId = "" } = useParams();
  const [params] = useSearchParams();
  const userId = params.get("userId") ?? undefined;
  const navigate = useNavigate();

  const query = useQuery({
    queryKey: ["records", userId ?? "me", "word", wordId],
    queryFn: () => api.get<WordHistory>(withUser(`/records/words/${wordId}`, userId)),
    enabled: !!wordId,
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
        <PageHeader title="单词轨迹" extra={back} />
        <ErrorBlock error={query.error} onRetry={() => query.refetch()} />
      </div>
    );

  const { word, memory, reviewLogs, answers } = query.data;

  // 时间线：复习评分逐条；作答按天归组；最新在前
  const entries: TimelineEntry[] = reviewLogs.map((l) => ({
    key: `log-${l.id}`,
    at: l.reviewedAt,
    node: (
      <div>
        <div style={{ fontWeight: 600 }}>
          记忆评分：{RATING_LABEL[l.rating] ?? l.rating}
          <span style={{ fontWeight: 400, fontSize: 12, color: "var(--muted)", marginLeft: 8 }}>
            {fmtDay(l.dayKey)} {fmtTime(l.reviewedAt)}
          </span>
        </div>
        <div style={{ fontSize: 13, color: "var(--ink-soft)" }}>
          稳定性 → {l.stabilityAfter.toFixed(1)} 天 · 下次复习 {fmtDate(l.dueAfter)}
        </div>
      </div>
    ),
  }));

  const byDay = new Map<string, WordHistory["answers"]>();
  for (const a of answers) byDay.set(a.dayKey, [...(byDay.get(a.dayKey) ?? []), a]);
  for (const [day, list] of byDay) {
    const correct = list.filter((a) => a.correct).length;
    entries.push({
      key: `day-${day}`,
      at: list[list.length - 1].createdAt,
      node: (
        <div>
          <div style={{ fontWeight: 600 }}>
            {fmtDay(day)} 作答 {list.length} 题
            <span style={{ fontWeight: 400, fontSize: 12, color: "var(--muted)", marginLeft: 8 }}>
              对 {correct} · 错 {list.length - correct}
            </span>
          </div>
          <div style={{ display: "flex", gap: 6, flexWrap: "wrap", marginTop: 6 }}>
            {list.map((a) => (
              <AnswerChip
                key={a.id}
                mode={a.mode}
                correct={a.correct}
                userAnswer={a.userAnswer}
                hintUsed={a.hintUsed}
                dontKnow={a.dontKnow}
                prefix={<span style={{ color: "var(--muted)" }}>{PHASE_LABEL[a.phase] ?? a.phase}</span>}
              />
            ))}
          </div>
        </div>
      ),
    });
  }
  entries.sort((a, b) => new Date(b.at).getTime() - new Date(a.at).getTime());

  return (
    <div className="vx-page">
      <PageHeader eyebrow={userId ? "学生单词轨迹" : "单词轨迹"} title="单词轨迹" extra={back} />

      <div className="vx-card vx-rise" style={{ padding: "24px 24px 20px", marginBottom: 16 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 12, flexWrap: "wrap" }}>
          <span className="vx-word" style={{ fontSize: 40, fontWeight: 600, lineHeight: 1.1, wordBreak: "break-word" }}>
            {word.spelling}
          </span>
          <SpeakButton text={word.spelling} wordId={word.id} />
        </div>
        <div style={{ color: "var(--muted)", marginTop: 4 }}>{[word.phonetic, word.partOfSpeech].filter(Boolean).join("  ")}</div>
        <div className="vx-cn" style={{ fontSize: 18, marginTop: 10 }}>
          {word.definition}
        </div>
        {word.example && (
          <div style={{ marginTop: 12, paddingLeft: 12, borderLeft: "3px solid var(--line)" }}>
            <div className="vx-example-en" style={{ fontStyle: "italic" }}>
              {word.example}
            </div>
            {word.exampleCn && <div className="vx-example-cn" style={{ marginTop: 2 }}>{word.exampleCn}</div>}
          </div>
        )}
      </div>

      {memory ? (
        <Row gutter={[12, 12]} style={{ marginBottom: 16 }}>
          <Col xs={12} sm={6}>
            <StatTile label="记忆状态" value={<MasteryTag level={memory.level} />} hint={`难度 ${memory.difficulty.toFixed(1)} / 10`} />
          </Col>
          <Col xs={12} sm={6}>
            <StatTile label="稳定性" value={memory.stability.toFixed(1)} suffix="天" tone="primary" />
          </Col>
          <Col xs={12} sm={6}>
            <StatTile label="下次复习" value={fmtDate(memory.due)} hint={new Date(memory.due) <= new Date() ? "已到期" : undefined} />
          </Col>
          <Col xs={12} sm={6}>
            <StatTile label="复习 / 遗忘" value={`${memory.reps}/${memory.lapses}`} suffix="次" />
          </Col>
        </Row>
      ) : (
        <div className="vx-card" style={{ padding: 16, marginBottom: 16, color: "var(--ink-soft)" }}>
          <Tag bordered={false}>尚未学习</Tag> 这个词还没有进入记忆库。
        </div>
      )}

      <div className="vx-card" style={{ padding: 16 }}>
        <h3 className="vx-title" style={{ fontSize: 17, marginBottom: 16 }}>
          学习轨迹
        </h3>
        {entries.length === 0 ? (
          <div style={{ color: "var(--muted)" }}>暂无作答或复习记录</div>
        ) : (
          <Timeline items={entries.map((e) => ({ key: e.key, children: e.node }))} />
        )}
      </div>
    </div>
  );
}
