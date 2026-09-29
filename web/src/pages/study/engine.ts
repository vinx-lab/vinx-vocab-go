/**
 * 学习组前端流程（纯函数）：根据快照与已作答记录推导“现在该做什么”，保证刷新/断网后可续做。
 *
 * 阶段：
 * - learn：cards（认识）→ practice（每词 × 每题型各一题）→ consolidate（错题重做直到答对，最多 3 次）→ finish
 * - review / drill：practice → consolidate → finish
 * - test：test（不反馈）→ finish
 */
import { isTestKind } from "@vinx/shared";
import type { Mode, SessionAnswer, SessionItem, SessionKind } from "@/types";

export const MAX_CONSOLIDATE_ATTEMPTS = 3;

export type Stage = "cards" | "practice" | "consolidate" | "test" | "finish";

export interface Question {
  wordId: string;
  mode: Mode;
  phase: "practice" | "consolidate" | "test";
  attempt: number;
}

export interface FlowState {
  stage: Stage;
  /** 当前阶段的全部题目（按固定顺序） */
  queue: Question[];
  /** 当前要做的题；null 表示阶段完成 */
  current: Question | null;
  done: number;
  total: number;
  wrongWordIds: string[];
}

/** 确定性随机：同一组刷新后题序不变 */
function seeded(seed: string) {
  let h = 2166136261;
  for (let i = 0; i < seed.length; i++) h = Math.imul(h ^ seed.charCodeAt(i), 16777619);
  return () => {
    h = (h + 0x6d2b79f5) | 0;
    let t = Math.imul(h ^ (h >>> 15), 1 | h);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

export function seededShuffle<T>(arr: readonly T[], seed: string): T[] {
  const rng = seeded(seed);
  const a = [...arr];
  for (let i = a.length - 1; i > 0; i--) {
    const j = Math.floor(rng() * (i + 1));
    [a[i], a[j]] = [a[j], a[i]];
  }
  return a;
}

/** 某个词实际要做的题型（缺例句的词没有挖空题） */
export function modesOf(item: SessionItem, sessionModes: Mode[]): Mode[] {
  return item.modes ?? sessionModes;
}

/** 第一轮题序：认义 → 拼写 → 挖空（由易到难），每轮内打乱 */
export function firstRoundQueue(sessionId: string, items: SessionItem[], modes: Mode[], phase: "practice" | "test"): Question[] {
  const order: Mode[] = (["recognition", "spelling", "cloze"] as Mode[]).filter((m) => modes.includes(m));
  return order.flatMap((mode) =>
    seededShuffle(items, `${sessionId}:${mode}`)
      .filter((i) => modesOf(i, modes).includes(mode))
      .map((i) => ({ wordId: i.wordId, mode, phase, attempt: 1 })),
  );
}

const key = (a: { wordId: string; mode: string }) => `${a.wordId}:${a.mode}`;

export function deriveFlow(input: {
  sessionId: string;
  kind: SessionKind;
  status: "active" | "completed";
  items: SessionItem[];
  modes: Mode[];
  answers: SessionAnswer[];
  cardsDone: boolean;
}): FlowState {
  const { sessionId, kind, items, modes, answers } = input;
  if (input.status === "completed") return { stage: "finish", queue: [], current: null, done: 0, total: 0, wrongWordIds: [] };

  if (isTestKind(kind)) {
    const queue = firstRoundQueue(sessionId, items, modes, "test");
    const answered = new Set(answers.filter((a) => a.phase === "test").map(key));
    const remaining = queue.filter((q) => !answered.has(key(q)));
    return {
      stage: remaining.length ? "test" : "finish",
      queue,
      current: remaining[0] ?? null,
      done: queue.length - remaining.length,
      total: queue.length,
      wrongWordIds: [],
    };
  }

  const practiceAnswers = answers.filter((a) => a.phase === "practice" && a.attempt === 1);
  if (kind === "learn" && !input.cardsDone && practiceAnswers.length === 0) {
    return { stage: "cards", queue: [], current: null, done: 0, total: items.length, wrongWordIds: [] };
  }

  const practiceQueue = firstRoundQueue(sessionId, items, modes, "practice");
  const answered = new Set(practiceAnswers.map(key));
  const remaining = practiceQueue.filter((q) => !answered.has(key(q)));
  if (remaining.length > 0) {
    return { stage: "practice", queue: practiceQueue, current: remaining[0], done: practiceQueue.length - remaining.length, total: practiceQueue.length, wrongWordIds: [] };
  }

  // 巩固：练习中答错的（词, 题型）逐个重做，直到答对或达到上限
  const wrongPairs = practiceQueue.filter((q) => practiceAnswers.some((a) => key(a) === key(q) && a.correct === false));
  const wrongWordIds = [...new Set(wrongPairs.map((p) => p.wordId))];
  const consolidate = answers.filter((a) => a.phase === "consolidate");
  let resolved = 0;
  let current: Question | null = null;
  for (const pair of wrongPairs) {
    const tries = consolidate.filter((a) => key(a) === key(pair));
    if (tries.some((t) => t.correct) || tries.length >= MAX_CONSOLIDATE_ATTEMPTS) {
      resolved += 1;
      continue;
    }
    if (!current) current = { wordId: pair.wordId, mode: pair.mode, phase: "consolidate", attempt: tries.length + 1 };
  }
  return {
    stage: current ? "consolidate" : "finish",
    queue: wrongPairs.map((p) => ({ ...p, phase: "consolidate" as const })),
    current,
    done: resolved,
    total: wrongPairs.length,
    wrongWordIds,
  };
}

/** 拼写输入归一化（与后端 answer-check 口径一致，仅用于前端即时提示） */
export function normalizeAnswer(s: string): string {
  return s.toLowerCase().replace(/[’‘`]/g, "'").replace(/\.{3}|…/g, "").replace(/\s+/g, "");
}
