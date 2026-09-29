import type { ComponentChildren, JSX } from "preact";
import { Button, Empty, Result, Spin, Tag, Tooltip, SoundOutlined } from "@/ui";
import type { MasteryLevel, SessionKind } from "@/types";
import { KIND_LABEL } from "@/types";

type CSSProperties = JSX.CSSProperties;

/** 页面标题区：眉标 + 衬线标题 + 右侧操作 */
export function PageHeader({ eyebrow, title, extra, children }: { eyebrow?: string; title: ComponentChildren; extra?: ComponentChildren; children?: ComponentChildren }) {
  return (
    <div className="vx-rise" style={{ display: "flex", flexWrap: "wrap", alignItems: "flex-end", justifyContent: "space-between", gap: 12, marginBottom: 20 }}>
      <div style={{ minWidth: 0 }}>
        {eyebrow && <div className="vx-eyebrow">{eyebrow}</div>}
        <h1 className="vx-title" style={{ fontSize: 26, lineHeight: 1.25 }}>
          {title}
        </h1>
        {children && <div style={{ color: "var(--ink-soft)", marginTop: 6 }}>{children}</div>}
      </div>
      {extra && <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>{extra}</div>}
    </div>
  );
}

/** 统计小块：大号数字 + 标签 */
export function StatTile({ label, value, suffix, hint, tone = "ink", style }: { label: string; value: ComponentChildren; suffix?: string; hint?: ComponentChildren; tone?: "ink" | "primary" | "accent"; style?: CSSProperties }) {
  const color = tone === "primary" ? "var(--primary)" : tone === "accent" ? "var(--accent)" : "var(--ink)";
  return (
    <div className="vx-card" style={{ padding: "14px 16px", height: "100%", boxSizing: "border-box", ...style }}>
      <div style={{ fontSize: 13, color: "var(--muted)" }}>{label}</div>
      <div style={{ display: "flex", alignItems: "baseline", gap: 4, marginTop: 4 }}>
        <span className="vx-num" style={{ fontSize: 30, fontWeight: 600, color, lineHeight: 1.1 }}>
          {value}
        </span>
        {suffix && <span style={{ color: "var(--muted)", fontSize: 13 }}>{suffix}</span>}
      </div>
      {hint && <div style={{ fontSize: 12, color: "var(--muted)", marginTop: 4 }}>{hint}</div>}
    </div>
  );
}

export function Loading({ tip = "加载中" }: { tip?: string }) {
  return (
    <div style={{ padding: 64, textAlign: "center" }}>
      <Spin tip={tip}>
        <div style={{ height: 40 }} />
      </Spin>
    </div>
  );
}

export function ErrorBlock({ error, onRetry }: { error: unknown; onRetry?: () => void }) {
  return <Result status="warning" title="加载失败" subTitle={(error as Error)?.message ?? "请稍后再试"} extra={onRetry && <Button onClick={onRetry}>重试</Button>} />;
}

export function EmptyBlock({ title, description, action }: { title: string; description?: ComponentChildren; action?: ComponentChildren }) {
  return (
    <div className="vx-card" style={{ padding: "40px 20px", textAlign: "center" }}>
      <Empty
        image={Empty.PRESENTED_IMAGE_SIMPLE}
        description={
          <div>
            <div style={{ fontSize: 16, color: "var(--ink)", fontWeight: 600 }}>{title}</div>
            {description && <div style={{ color: "var(--muted)", marginTop: 6 }}>{description}</div>}
          </div>
        }
      >
        {action}
      </Empty>
    </div>
  );
}

/** 浏览器合成语音（真人音频不可用时的兜底） */
export function speakSynth(text: string) {
  if (typeof window === "undefined" || !("speechSynthesis" in window)) return;
  window.speechSynthesis.cancel();
  const u = new SpeechSynthesisUtterance(text.replace(/[（(][^)）]*[)）]/g, ""));
  u.lang = "en-US";
  u.rate = 0.9;
  window.speechSynthesis.speak(u);
}

/** 确认没有真人音频的词（本次会话内不再重复请求） */
const noAudio = new Set<string>();
let current: HTMLAudioElement | null = null;
let lastKey = "";
let lastAt = 0;

/** 停掉正在播的真人音频与合成语音（切题、重复触发时先清场，避免两个声音叠在一起） */
export function stopSpeaking() {
  if (current) {
    current.pause();
    current = null;
  }
  if (typeof window !== "undefined" && "speechSynthesis" in window) window.speechSynthesis.cancel();
}

/**
 * 朗读单词：优先服务端缓存的真人发音，取不到时才回退浏览器合成语音。
 * 同一个词 400ms 内重复触发只播一次；被新的播放打断（AbortError）不算「没有真人音频」。
 */
export function speak(text: string, wordId?: string) {
  if (typeof window === "undefined") return;
  const key = wordId ?? text;
  const now = Date.now();
  if (key === lastKey && now - lastAt < 400) return;
  lastKey = key;
  lastAt = now;

  stopSpeaking();
  if (!wordId || noAudio.has(wordId)) return speakSynth(text);

  const audio = new Audio(`/api/audio/words/${wordId}`);
  current = audio;
  const fallback = () => {
    if (current !== audio) return; // 已被新的播放请求接管，不再兜底
    noAudio.add(wordId);
    speakSynth(text);
  };
  audio.addEventListener("error", fallback);
  audio.play().catch((e: unknown) => {
    if ((e as Error)?.name === "AbortError") return;
    fallback();
  });
}

export function SpeakButton({ text, wordId, size = "middle" }: { text: string; wordId?: string; size?: "small" | "middle" | "large" }) {
  return (
    <Tooltip title="朗读">
      <Button
        aria-label={`朗读 ${text}`}
        shape="circle"
        size={size}
        icon={<SoundOutlined />}
        onClick={(e) => {
          e.stopPropagation();
          speak(text, wordId);
        }}
      />
    </Tooltip>
  );
}

const LEVEL_COLOR: Record<MasteryLevel, string> = { learning: "orange", consolidating: "cyan", mastered: "green" };
const LEVEL_LABEL: Record<MasteryLevel, string> = { learning: "学习中", consolidating: "巩固中", mastered: "已掌握" };

export function MasteryTag({ level }: { level: MasteryLevel }) {
  return (
    <Tag color={LEVEL_COLOR[level]} bordered={false}>
      {LEVEL_LABEL[level]}
    </Tag>
  );
}

const KIND_COLOR: Record<SessionKind, string> = { learn: "volcano", review: "cyan", test: "geekblue", drill: "gold", sheet: "purple" };

export function KindTag({ kind }: { kind: SessionKind }) {
  return (
    <Tag color={KIND_COLOR[kind]} bordered={false}>
      {KIND_LABEL[kind]}
    </Tag>
  );
}

export function percent(rate: number | null | undefined, digits = 0): string {
  return rate === null || rate === undefined ? "—" : `${(rate * 100).toFixed(digits)}%`;
}

export function formatDuration(ms: number): string {
  const min = Math.round(ms / 60000);
  if (min < 1) return "不到 1 分钟";
  return `${min} 分钟`;
}
