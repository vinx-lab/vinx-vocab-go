import { useState } from "preact/hooks";
import type { DailyPoint } from "@/types";

/**
 * 图表：色板经 dataviz 校验（浅色 #d9531e 新学 / #00897b 复习，浅色面全部 PASS）；
 * 颜色都取 styles.css 的 CSS 变量，深色主题下自动换成提亮的同色相（--chart-*、--mastery-*）。
 * 规格：柱宽 ≤ 24px、顶端 4px 圆角、堆叠段间 2px 面色间隙、细网格、悬停提示、图例 + 表格视图兜底。
 */
export const SERIES = { newWords: "var(--chart-new)", reviewedWords: "var(--chart-review)" } as const;
const GRID = "var(--chart-grid)";
const SURFACE = "var(--surface)";

function niceMax(v: number): number {
  if (v <= 5) return 5;
  const pow = 10 ** Math.floor(Math.log10(v));
  const n = v / pow;
  const step = n <= 1 ? 1 : n <= 2 ? 2 : n <= 5 ? 5 : 10;
  return step * pow;
}

function shortDay(day: string) {
  const [, m, d] = day.split("-");
  return `${Number(m)}/${Number(d)}`;
}

/** 每日词数：新学 + 复习 堆叠柱 */
export function DailyWordsChart({ data, height = 180 }: { data: DailyPoint[]; height?: number }) {
  const [hover, setHover] = useState<number | null>(null);
  const [showTable, setShowTable] = useState(false);
  const width = 640;
  const pad = { top: 12, right: 8, bottom: 24, left: 32 };
  const innerW = width - pad.left - pad.right;
  const innerH = height - pad.top - pad.bottom;
  const max = niceMax(Math.max(1, ...data.map((d) => d.newWords + d.reviewedWords)));
  const band = innerW / Math.max(1, data.length);
  const barW = Math.min(24, band * 0.62);
  const y = (v: number) => pad.top + innerH - (v / max) * innerH;
  const ticks = [0, max / 2, max];
  const labelEvery = Math.ceil(data.length / 8);
  const h = hover !== null ? data[hover] : null;

  return (
    <div>
      <div style={{ display: "flex", gap: 16, alignItems: "center", fontSize: 12, color: "var(--ink-soft)", marginBottom: 6, flexWrap: "wrap" }}>
        <Legend color={SERIES.newWords} label="新学词" />
        <Legend color={SERIES.reviewedWords} label="复习词" />
        <button
          type="button"
          onClick={() => setShowTable((s) => !s)}
          style={{ marginLeft: "auto", background: "none", border: "none", color: "var(--primary)", cursor: "pointer", fontSize: 12 }}
        >
          {showTable ? "看图表" : "看表格"}
        </button>
      </div>
      {showTable ? (
        <div style={{ maxHeight: height + 40, overflow: "auto" }}>
          <table style={{ width: "100%", fontSize: 13, borderCollapse: "collapse" }}>
            <thead>
              <tr style={{ color: "var(--muted)", textAlign: "right" }}>
                <th style={{ textAlign: "left", padding: 4 }}>日期</th>
                <th style={{ padding: 4 }}>新学</th>
                <th style={{ padding: 4 }}>复习</th>
                <th style={{ padding: 4 }}>题数</th>
                <th style={{ padding: 4 }}>分钟</th>
              </tr>
            </thead>
            <tbody>
              {[...data].reverse().map((d) => (
                <tr key={d.day} style={{ textAlign: "right", borderTop: `1px solid ${GRID}` }}>
                  <td style={{ textAlign: "left", padding: 4 }}>{d.day}</td>
                  <td style={{ padding: 4 }}>{d.newWords}</td>
                  <td style={{ padding: 4 }}>{d.reviewedWords}</td>
                  <td style={{ padding: 4 }}>{d.answers}</td>
                  <td style={{ padding: 4 }}>{d.minutes}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <div style={{ position: "relative" }}>
          <svg viewBox={`0 0 ${width} ${height}`} width="100%" role="img" aria-label="每日新学与复习词数" style={{ display: "block" }} onMouseLeave={() => setHover(null)}>
            {ticks.map((t) => (
              <g key={t}>
                <line x1={pad.left} x2={width - pad.right} y1={y(t)} y2={y(t)} style={{ stroke: GRID }} stroke-width={1} />
                <text x={pad.left - 6} y={y(t) + 4} text-anchor="end" font-size={11} style={{ fill: "var(--chart-axis)" }}>
                  {t}
                </text>
              </g>
            ))}
            {data.map((d, i) => {
              const cx = pad.left + band * i + band / 2;
              const x = cx - barW / 2;
              const reviewTop = y(d.reviewedWords);
              const newTop = y(d.reviewedWords + d.newWords);
              const gap = d.reviewedWords > 0 && d.newWords > 0 ? 2 : 0;
              return (
                <g key={d.day} onMouseEnter={() => setHover(i)}>
                  <rect x={pad.left + band * i} y={pad.top} width={band} height={innerH} style={{ fill: hover === i ? "var(--chart-hover)" : "transparent" }} />
                  {d.reviewedWords > 0 && <TopRoundBar x={x} y={reviewTop} w={barW} h={y(0) - reviewTop} color={SERIES.reviewedWords} round={d.newWords === 0} />}
                  {d.newWords > 0 && <TopRoundBar x={x} y={newTop} w={barW} h={Math.max(0, reviewTop - newTop - gap)} color={SERIES.newWords} round />}
                  {i % labelEvery === 0 && (
                    <text x={cx} y={height - 6} text-anchor="middle" font-size={11} style={{ fill: "var(--chart-axis)" }}>
                      {shortDay(d.day)}
                    </text>
                  )}
                </g>
              );
            })}
            <line x1={pad.left} x2={width - pad.right} y1={y(0)} y2={y(0)} style={{ stroke: "var(--line-strong)" }} stroke-width={1} />
          </svg>
          {h && hover !== null && (
            <div
              role="status"
              style={{
                position: "absolute",
                top: 0,
                left: `${Math.min(78, Math.max(2, ((pad.left + band * hover + band / 2) / width) * 100 - 10))}%`,
                background: SURFACE,
                border: `1px solid ${GRID}`,
                borderRadius: 8,
                padding: "6px 10px",
                fontSize: 12,
                color: "var(--ink)",
                boxShadow: "var(--chart-tooltip-shadow)",
                pointerEvents: "none",
                whiteSpace: "nowrap",
              }}
            >
              <div style={{ fontWeight: 600, marginBottom: 2 }}>{h.day}</div>
              <div><Legend color={SERIES.newWords} label={`新学 ${h.newWords} 词`} /></div>
              <div><Legend color={SERIES.reviewedWords} label={`复习 ${h.reviewedWords} 词`} /></div>
              <div style={{ color: "var(--muted)" }}>
                {h.answers} 题 · {h.firstAttempts ? `首答正确率 ${Math.round((h.correct / h.firstAttempts) * 100)}%` : "无作答"} · {h.minutes} 分钟
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function TopRoundBar({ x, y, w, h, color, round }: { x: number; y: number; w: number; h: number; color: string; round: boolean }) {
  if (h <= 0) return null;
  const r = round ? Math.min(4, w / 2, h) : 0;
  const d = `M${x},${y + h} V${y + r} Q${x},${y} ${x + r},${y} H${x + w - r} Q${x + w},${y} ${x + w},${y + r} V${y + h} Z`;
  return <path d={d} style={{ fill: color }} />;
}

export function Legend({ color, label }: { color: string; label: string }) {
  return (
    <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
      <span aria-hidden style={{ width: 10, height: 10, borderRadius: 3, background: color, display: "inline-block" }} />
      {label}
    </span>
  );
}

/** 记忆分布：单色由浅到深的分段条（学习中 → 巩固中 → 已掌握），带直接标注 */
export const MASTERY_RAMP = { learning: "var(--mastery-1)", consolidating: "var(--mastery-2)", mastered: "var(--mastery-3)" } as const;

export function MasteryBar({ mastery }: { mastery: { learning: number; consolidating: number; mastered: number } }) {
  const total = mastery.learning + mastery.consolidating + mastery.mastered;
  const parts = [
    { key: "learning", label: "学习中", value: mastery.learning },
    { key: "consolidating", label: "巩固中", value: mastery.consolidating },
    { key: "mastered", label: "已掌握", value: mastery.mastered },
  ] as const;
  return (
    <div>
      <div style={{ display: "flex", gap: 2, height: 14, borderRadius: 7, overflow: "hidden", background: "var(--track)" }} role="img" aria-label={parts.map((p) => `${p.label} ${p.value}`).join("，")}>
        {total > 0 &&
          parts.map((p) =>
            p.value > 0 ? <div key={p.key} title={`${p.label} ${p.value} 词`} style={{ flex: p.value, background: MASTERY_RAMP[p.key] }} /> : null,
          )}
      </div>
      <div style={{ display: "flex", gap: 16, marginTop: 8, fontSize: 13, color: "var(--ink-soft)", flexWrap: "wrap" }}>
        {parts.map((p) => (
          <Legend key={p.key} color={MASTERY_RAMP[p.key]} label={`${p.label} ${p.value}`} />
        ))}
      </div>
    </div>
  );
}

/** 进度环 */
export function ProgressRing({ value, total, size = 56, color = "var(--primary)", label }: { value: number; total: number; size?: number; color?: string; label?: string }) {
  const stroke = 6;
  const r = (size - stroke) / 2;
  const c = 2 * Math.PI * r;
  const ratio = total > 0 ? Math.min(1, value / total) : 0;
  return (
    <svg width={size} height={size} role="img" aria-label={label ?? `${value}/${total}`}>
      <circle cx={size / 2} cy={size / 2} r={r} fill="none" style={{ stroke: "var(--track)" }} stroke-width={stroke} />
      <circle
        cx={size / 2}
        cy={size / 2}
        r={r}
        fill="none"
        stroke-width={stroke}
        stroke-linecap="round"
        stroke-dasharray={`${c * ratio} ${c}`}
        transform={`rotate(-90 ${size / 2} ${size / 2})`}
        style={{ stroke: color, transition: "stroke-dasharray .5s ease" }}
      />
      <text x="50%" y="52%" text-anchor="middle" dominant-baseline="middle" font-size={size / 4.2} style={{ fill: "var(--ink)", fontFamily: "var(--serif-en)" }}>
        {total > 0 ? `${Math.round(ratio * 100)}%` : "—"}
      </text>
    </svg>
  );
}
