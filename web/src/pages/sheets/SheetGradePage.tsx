import { useState } from "preact/hooks";
import { Link, useParams } from "@/lib/router";
import { useQuery, useQueryClient } from "@/lib/query";
import { useIdentity } from "@/lib/auth";
import { Button, Input, Tag, useApp, ArrowLeftOutlined, CheckCircleFilled, CloseCircleFilled, PrinterOutlined } from "@/ui";
import { api, errorMessage } from "@/lib/api";
import { EmptyBlock, ErrorBlock, Loading, PageHeader } from "@/components/ui";
import type { SheetDetail } from "@/types";
import { fmtDateTime } from "@/pages/records/shared";
import { DICT_TYPE_LABEL, gradePayload, numberItems, regenerateDictationLink, type NumberedItem } from "./dictation";

/** 默写单批改 /sheets/:id/grade */
export function SheetGradePage() {
  const { id = "" } = useParams<{ id: string }>();
  return <SheetGrade sheetId={id} />;
}

/**
 * 逐题显示中文提示和标准答案，默认全部算对，点一下标为「错」（再点一下改回来）；
 * 错题旁可以记下学生实际写的内容。底部显示「对 x / 错 y」，确认后提交，只能提交一次。
 * 已批改时显示成绩单（对错列表）和「用错题再出一份」。
 */
export function SheetGrade({ sheetId }: { sheetId: string }) {
  const qc = useQueryClient();
  const { message, modal } = useApp();
  const { data: identity } = useIdentity();
  const [wrong, setWrong] = useState<Set<number>>(() => new Set());
  const [notes, setNotes] = useState<Record<number, string>>({});
  const key = ["sheets", "detail", sheetId];
  const q = useQuery({ queryKey: key, queryFn: () => api.get<SheetDetail>(`/sheets/${sheetId}`), enabled: !!sheetId });

  const back = (
    <Link to="/sheets">
      <Button icon={<ArrowLeftOutlined />}>单词单</Button>
    </Link>
  );

  if (q.isLoading) return <div className="vx-page"><Loading /></div>;
  if (q.isError || !q.data)
    return (
      <div className="vx-page">
        <PageHeader eyebrow="默写单" title="批改" extra={back} />
        <ErrorBlock error={q.error} onRetry={() => q.refetch()} />
      </div>
    );

  const s = q.data;
  const items = numberItems(s.items ?? []);
  const header = (sub: string) => (
    <PageHeader
      eyebrow={`${s.student.name} 的默写单`}
      title={`默写单 #${s.seq} · ${sub}`}
      extra={
        <span style={{ display: "inline-flex", gap: 8, flexWrap: "wrap" }}>
          {back}
          <Link to={`/sheets/${s.id}/print`} target="_blank">
            <Button icon={<PrinterOutlined />}>打印</Button>
          </Link>
        </span>
      }
    />
  );

  if (s.format !== "dictation")
    return (
      <div className="vx-page">
        <PageHeader eyebrow="单词单" title={`单词单 #${s.seq}`} extra={back} />
        <EmptyBlock title="这是自测单词单，不需要批改" description="自测单词单在「今日」或单词单列表里在线测试。" />
      </div>
    );

  if (s.grading) {
    const isOwner = identity?.id === s.student.id;
    return (
      <div className="vx-page">
        {header("已批改")}
        <GradeReport items={items} sheet={s} regenerate={regenerateDictationLink({ sessionId: s.grading.sessionId, isOwner, userId: s.student.id })} />
      </div>
    );
  }

  const nWrong = items.filter((it) => wrong.has(it.index)).length;
  const toggle = (index: number) =>
    setWrong((w) => {
      const next = new Set(w);
      if (next.has(index)) next.delete(index);
      else next.add(index);
      return next;
    });

  const submit = async () => {
    try {
      const res = await api.post<SheetDetail>(`/sheets/${s.id}/grade`, { results: gradePayload(items, wrong, notes) });
      qc.setQueryData(key, res);
      void qc.invalidateQueries({ queryKey: ["sheets"] });
      void qc.invalidateQueries({ queryKey: ["today"] });
      void qc.invalidateQueries({ queryKey: ["records"] });
      message.success("批改已提交");
    } catch (e) {
      message.error(errorMessage(e));
      // 已经批改过（409）等：重新取一次，显示最新状态
      void q.refetch();
    }
  };

  const confirm = () =>
    modal.confirm({
      title: `提交默写单 #${s.seq} 的批改？`,
      content: `对 ${items.length - nWrong} 题，错 ${nWrong} 题。提交后不能修改；批改错了，可以在下一次默写或检测里纠正。`,
      okText: "确认提交",
      cancelText: "再看看",
      onOk: submit,
    });

  return (
    <div className="vx-page">
      {header("批改")}
      <p style={{ color: "var(--ink-soft)", marginTop: -6 }}>默认全部算对。对照学生写的，点一下标为「错」，再点一下改回来；错题可以记下学生写的内容，方便以后查看。</p>
      {items.length === 0 ? (
        <EmptyBlock title="这份默写单里的词和句子都已被删除" />
      ) : (
        <div style={{ display: "grid", gap: 8 }}>
          {items.map((it) => {
            const isWrong = wrong.has(it.index);
            return (
              <div key={it.index} className={`vx-card vx-grade-item${isWrong ? " is-wrong" : ""}`}>
                <button type="button" className="vx-grade-toggle" aria-label={`第 ${it.no} 题`} aria-pressed={isWrong} onClick={() => toggle(it.index)}>
                  <span className="vx-grade-no">{it.no}</span>
                  <span className="vx-grade-body">
                    <span className="vx-grade-prompt">
                      <Tag bordered={false} style={{ marginRight: 6 }}>{DICT_TYPE_LABEL[it.type]}</Tag>
                      <span className="vx-cn">{it.prompt}</span>
                    </span>
                    <span className="vx-word vx-grade-answer">{it.answer}</span>
                  </span>
                  <span className="vx-grade-mark">
                    {isWrong ? (
                      <>
                        <CloseCircleFilled aria-hidden /> 错
                      </>
                    ) : (
                      <>
                        <CheckCircleFilled aria-hidden /> 对
                      </>
                    )}
                  </span>
                </button>
                {isWrong && (
                  <div style={{ padding: "0 12px 12px 44px" }}>
                    <Input
                      aria-label={`第 ${it.no} 题学生写的`}
                      placeholder="学生写的（可不填）"
                      maxLength={200}
                      value={notes[it.index] ?? ""}
                      autoCapitalize="off"
                      autoCorrect="off"
                      spellCheck={false}
                      onChange={(e) => setNotes((n) => ({ ...n, [it.index]: (e.target as HTMLInputElement).value }))}
                    />
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}
      <div className="vx-grade-bar">
        <span style={{ flex: 1, fontWeight: 600 }}>
          对 {items.length - nWrong} / 错 {nWrong}
        </span>
        <Button type="primary" size="large" disabled={items.length === 0} onClick={confirm}>
          提交批改
        </Button>
      </div>
    </div>
  );
}

/** 成绩单：和检测相同的对错列表，带批改人、自批标记和「用错题再出一份」 */
function GradeReport({ sheet: s, items, regenerate }: { sheet: SheetDetail; items: NumberedItem[]; regenerate: string }) {
  const g = s.grading!;
  const byIndex = new Map(g.results.map((r) => [r.index, r]));
  const wrongCount = g.total - g.correct;
  return (
    <>
      <div className="vx-card" style={{ padding: "16px 18px", display: "flex", alignItems: "center", gap: 16, flexWrap: "wrap", marginBottom: 14 }}>
        <div>
          <div style={{ fontSize: 13, color: "var(--muted)" }}>成绩</div>
          <div className="vx-num" style={{ fontSize: 28, fontWeight: 700 }}>
            {g.correct} / {g.total}
          </div>
        </div>
        <div style={{ flex: 1, minWidth: 180, color: "var(--ink-soft)", fontSize: 13 }}>
          {g.gradedBy.name || "—"} 批改 · {fmtDateTime(g.gradedAt)}
          {g.selfGraded && (
            <Tag color="orange" bordered={false} style={{ marginLeft: 8 }}>
              自批
            </Tag>
          )}
          <div style={{ marginTop: 4 }}>{g.selfGraded ? "学生账号提交的批改，成绩照常计入；老师可以再出一份默写单复核。" : "成绩已计入覆盖进度和复习安排。"}</div>
        </div>
        {wrongCount > 0 && (
          <Link to={regenerate}>
            <Button type="primary">用错题再出一份</Button>
          </Link>
        )}
      </div>
      <div className="vx-card" style={{ padding: "4px 16px" }}>
        {items.map((it, i) => {
          const r = byIndex.get(it.index);
          return (
            <div key={it.index} style={{ padding: "12px 0", borderTop: i ? "1px solid var(--line)" : "none", display: "flex", gap: 10, alignItems: "flex-start" }}>
              <span className="vx-num" style={{ color: "var(--muted)", width: 24, flexShrink: 0 }}>{it.no}</span>
              <div style={{ flex: 1, minWidth: 0 }}>
                <div className="vx-cn" style={{ color: "var(--ink-soft)" }}>
                  <Tag bordered={false} style={{ marginRight: 6 }}>{DICT_TYPE_LABEL[it.type]}</Tag>
                  {it.prompt}
                </div>
                <div className="vx-word" style={{ fontWeight: 600, marginTop: 2 }}>{it.answer}</div>
                {r && !r.correct && r.userAnswer && (
                  <div style={{ fontSize: 13, color: "var(--bad)", marginTop: 2 }}>
                    学生写的：<span className="vx-word">{r.userAnswer}</span>
                  </div>
                )}
              </div>
              <span style={{ flexShrink: 0, color: r ? (r.correct ? "var(--good)" : "var(--bad)") : "var(--muted)", display: "inline-flex", alignItems: "center", gap: 4 }}>
                {r ? r.correct ? <><CheckCircleFilled aria-hidden />对</> : <><CloseCircleFilled aria-hidden />错</> : "未批改"}
              </span>
            </div>
          );
        })}
      </div>
    </>
  );
}
