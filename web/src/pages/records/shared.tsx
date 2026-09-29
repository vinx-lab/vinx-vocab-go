import type { ComponentChildren, JSX } from "preact";
import { Tag } from "@/ui";
import { CheckCircleFilled, CloseCircleFilled } from "@/ui";
import dayjs from "dayjs";
import type { Mode } from "@/types";
import { MODE_LABEL } from "@/types";

/** 给 URL 追加 ?userId=（查看他人记录时） */
export function withUser(path: string, userId?: string): string {
  if (!userId) return path;
  return `${path}${path.includes("?") ? "&" : "?"}userId=${encodeURIComponent(userId)}`;
}

export const PHASE_LABEL: Record<string, string> = { practice: "练习", consolidate: "巩固", test: "检测" };

export function fmtDate(v: string | null | undefined): string {
  return v ? dayjs(v).format("M月D日") : "—";
}

export function fmtDateTime(v: string | null | undefined): string {
  return v ? dayjs(v).format("M月D日 HH:mm") : "—";
}

export function fmtTime(v: string | null | undefined): string {
  return v ? dayjs(v).format("HH:mm") : "—";
}

/** dayKey（YYYY-MM-DD）→ 9月18日 周五 */
export function fmtDay(day: string): string {
  const d = dayjs(day);
  return `${d.format("M月D日")} 周${"日一二三四五六".charAt(d.day())}`;
}

/** 作答对错标记：图标 + 文字，不只靠颜色 */
export function ResultMark({ correct, style }: { correct: boolean | null; style?: JSX.CSSProperties }) {
  if (correct === null) return <span style={{ color: "var(--muted)", ...style }}>未判</span>;
  return correct ? (
    <span style={{ color: "var(--good)", display: "inline-flex", alignItems: "center", gap: 4, ...style }}>
      <CheckCircleFilled aria-hidden />对
    </span>
  ) : (
    <span style={{ color: "var(--bad)", display: "inline-flex", alignItems: "center", gap: 4, ...style }}>
      <CloseCircleFilled aria-hidden />错
    </span>
  );
}

/** 单题作答小条：题型 · 对错 · 错误答案（点了「不会」只显示「不会」） · 提示 */
export function AnswerChip({
  mode,
  correct,
  userAnswer,
  hintUsed,
  dontKnow,
  prefix,
}: {
  mode: Mode;
  correct: boolean | null;
  userAnswer: string | null;
  hintUsed: boolean;
  dontKnow?: boolean;
  prefix?: ComponentChildren;
}) {
  return (
    <span
      style={{
        display: "inline-flex",
        alignItems: "center",
        gap: 6,
        padding: "2px 10px",
        borderRadius: 999,
        fontSize: 12,
        background: correct === false ? "var(--bad-soft)" : correct ? "var(--good-soft)" : "var(--paper-deep)",
        border: "1px solid var(--line)",
        whiteSpace: "nowrap",
      }}
    >
      {prefix}
      <span style={{ color: "var(--ink-soft)" }}>{MODE_LABEL[mode] ?? mode}</span>
      <ResultMark correct={correct} />
      {dontKnow ? (
        <span style={{ color: "var(--ink-soft)", fontWeight: 600 }}>不会</span>
      ) : correct === false && userAnswer ? (
        <span style={{ color: "var(--ink-soft)" }}>
          答 <span className="vx-word">{userAnswer}</span>
        </span>
      ) : null}
      {hintUsed && (
        <Tag bordered={false} style={{ margin: 0, fontSize: 11, lineHeight: "16px" }}>
          提示
        </Tag>
      )}
    </span>
  );
}
