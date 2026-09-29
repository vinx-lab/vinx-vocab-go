#!/usr/bin/env node
/**
 * 生成 FSRS 排程对照数据：internal/core/fsrs/testdata/fixtures.json。
 *
 * 用旧仓库依赖里的 ts-fsrs（5.4.2），按旧 apps/api/src/lib/scheduler.ts 的参数与封装
 * （applyRating 的「时钟回拨取上次复习时刻」、capDue 封顶到期时间）生成随机作答序列及每一步的卡片结果。
 * Go 测试逐条比对（浮点误差 ≤ 1e-6，时间完全相等）。
 *
 * 用法（在 Go 仓库根目录）：
 *   node contract/scripts/fsrs-fixtures.mjs [旧仓库目录，默认 ../vinx-vocab]
 */
import { createRequire } from "node:module";
import { mkdirSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
const oldRepo = resolve(root, process.argv[2] ?? process.env.OLD_REPO ?? "../vinx-vocab");
const require = createRequire(join(oldRepo, "apps", "api", "package.json"));
const tsfsrs = require("ts-fsrs");
const pkg = { version: tsfsrs.FSRSVersion.match(/^v([\d.]+)/)[1] };
if (pkg.version !== "5.4.2") throw new Error(`期望 ts-fsrs 5.4.2，实际 ${tsfsrs.FSRSVersion}`);
const { createEmptyCard, fsrs, generatorParameters } = tsfsrs;

// ---- 与旧 lib/scheduler.ts 完全相同的参数与封装 ----
const scheduler = fsrs(
  generatorParameters({ request_retention: 0.9, maximum_interval: 365, enable_fuzz: false, enable_short_term: false }),
);
const toCard = (m) => ({
  due: m.due, stability: m.stability, difficulty: m.difficulty, elapsed_days: m.elapsedDays, scheduled_days: m.scheduledDays,
  learning_steps: m.learningSteps, reps: m.reps, lapses: m.lapses, state: m.state, last_review: m.lastReview ?? undefined,
});
const fromCard = (c) => ({
  due: c.due, stability: c.stability, difficulty: c.difficulty, elapsedDays: c.elapsed_days, scheduledDays: c.scheduled_days,
  learningSteps: c.learning_steps, reps: c.reps, lapses: c.lapses, state: c.state, lastReview: c.last_review ?? null,
});
const newMemoryCard = (now) => fromCard(createEmptyCard(now));
function applyRating(card, rating, now) {
  const at = card.lastReview && now < card.lastReview ? card.lastReview : now;
  return fromCard(scheduler.next(toCard(card), at, rating).card);
}
function capDue(card, cap, now) {
  if (card.due <= cap) return card;
  return { ...card, due: cap, scheduledDays: Math.max(0, Math.round((cap.getTime() - now.getTime()) / 86_400_000)) };
}

// ---- 确定性随机 ----
function mulberry32(seed) {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}
const rng = mulberry32(20260928);
const int = (lo, hi) => lo + Math.floor(rng() * (hi - lo + 1));
const pick = (xs) => xs[Math.floor(rng() * xs.length)];

const MIN = 60_000;
const HOUR = 3_600_000;
const DAY = 86_400_000;
const SH_OFFSET = 8 * HOUR; // Asia/Shanghai 无夏令时

/** 下一个上海学习日 00:00 的 UTC 时刻（新学词 capDue 用） */
function nextShanghaiDayStart(now) {
  const local = now.getTime() + SH_OFFSET;
  return new Date(Math.floor(local / DAY) * DAY + DAY - SH_OFFSET);
}

function gap() {
  const kind = pick(["same", "minutes", "hours", "day", "day", "days", "days", "weeks", "long", "back", "utcEdge"]);
  switch (kind) {
    case "same": return { kind, ms: 0 };
    case "minutes": return { kind, ms: int(1, 180) * MIN + int(0, 59_999) };
    case "hours": return { kind, ms: int(3, 20) * HOUR + int(0, HOUR - 1) };
    case "day": return { kind, ms: DAY + int(-6, 6) * HOUR + int(0, HOUR - 1) };
    case "days": return { kind, ms: int(2, 12) * DAY + int(0, DAY - 1) };
    case "weeks": return { kind, ms: int(14, 90) * DAY + int(0, DAY - 1) };
    case "long": return { kind, ms: int(120, 900) * DAY + int(0, DAY - 1) };
    case "back": return { kind, ms: -(int(1, 72) * HOUR + int(0, HOUR - 1)) };
    // 跨 UTC 零点（上海 08:00）前后几分钟：elapsed_days 按 UTC 日期差计算
    default: return { kind, ms: null };
  }
}

function cardJSON(c) {
  return {
    due: c.due.toISOString(), stability: c.stability, difficulty: c.difficulty, elapsedDays: c.elapsedDays,
    scheduledDays: c.scheduledDays, learningSteps: c.learningSteps, reps: c.reps, lapses: c.lapses, state: c.state,
    lastReview: c.lastReview ? c.lastReview.toISOString() : null,
  };
}

const sequences = [];
const COUNT = 320;
for (let n = 0; n < COUNT; n++) {
  const t0 = new Date(Date.UTC(2024, 0, 1) + Math.floor(rng() * 3 * 365 * DAY));
  let card;
  let initial;
  if (rng() < 0.82) {
    card = newMemoryCard(t0);
    initial = { type: "new" };
  } else {
    // 旧数据里可能出现的非新卡（含 Learning / Relearning 状态，长期排程下与 Review 同路径）
    const lastReview = new Date(t0.getTime() - int(0, 60) * DAY - int(0, DAY - 1));
    const scheduledDays = int(0, 40);
    card = {
      due: new Date(lastReview.getTime() + scheduledDays * DAY),
      stability: Number((0.05 + rng() * 150).toFixed(int(2, 8))),
      difficulty: Number((1 + rng() * 9).toFixed(int(2, 8))),
      elapsedDays: int(0, 30), scheduledDays, learningSteps: int(0, 2), reps: int(1, 30), lapses: int(0, 6),
      state: pick([1, 2, 2, 3]), lastReview,
    };
    initial = { type: "card", card: cardJSON(card) };
  }
  const steps = [];
  let now = t0;
  const len = int(1, 16);
  for (let i = 0; i < len; i++) {
    if (i > 0 || initial.type === "card") {
      const g = gap();
      if (g.ms === null) {
        // 下一个 UTC 零点前后 ±5 分钟
        const nextUtcMidnight = Math.floor(now.getTime() / DAY) * DAY + DAY;
        now = new Date(nextUtcMidnight + int(-300_000, 300_000));
      } else {
        now = new Date(now.getTime() + g.ms);
      }
    }
    const r = rng();
    const rating = r < 0.3 ? 1 : r < 0.55 ? 2 : r < 0.92 ? 3 : 4;
    const wasNew = card.state === 0;
    card = applyRating(card, rating, now);
    const step = { now: now.toISOString(), rating, card: cardJSON(card) };
    if (wasNew && rng() < 0.7) {
      const cap = nextShanghaiDayStart(now);
      card = capDue(card, cap, now);
      step.cap = cap.toISOString();
      step.capped = cardJSON(card);
    }
    steps.push(step);
  }
  // initial 为 null 表示从 newMemoryCard(start) 开始
  sequences.push({ start: t0.toISOString(), initial: initial.type === "new" ? null : initial.card, steps });
}

const out = {
  generator: "contract/scripts/fsrs-fixtures.mjs",
  tsFsrs: pkg.version,
  params: { request_retention: 0.9, maximum_interval: 365, enable_fuzz: false, enable_short_term: false },
  sequences,
};
const file = join(root, "internal", "core", "fsrs", "testdata", "fixtures.json");
mkdirSync(dirname(file), { recursive: true });
// 每条序列一行，便于 diff
const body = JSON.stringify({ ...out, sequences: [] }).replace('"sequences":[]', `"sequences":[\n${sequences.map((q) => JSON.stringify(q)).join(",\n")}\n]`);
writeFileSync(file, body + "\n");
console.log(`写入 ${file}：${sequences.length} 条序列，${sequences.reduce((s, q) => s + q.steps.length, 0)} 步`);
