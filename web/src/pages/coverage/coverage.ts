/** 目标词书与覆盖进度（spec 0003）的前端纯函数 */
import type { CoverageCounts, CoverageStatus, TargetSheetStatus, WordStage } from "@/types";

/** 三档（单词单来源等仍按三档）：记住了 / 没记住 / 未接触 */
export const COVERAGE_LABEL: Record<CoverageStatus, string> = { known: "记住了", learning: "没记住", untested: "未接触" };

/** 五级词状态（spec 0009） */
export const STAGE_LABEL: Record<WordStage, string> = { untested: "未接触", missed: "没记住", fresh: "刚记住", consolidating: "巩固中", mastered: "已掌握" };

/** 进度条与图例的顺序：从牢到不牢，最后是未接触 */
export const STAGE_ORDER: WordStage[] = ["mastered", "consolidating", "fresh", "missed", "untested"];

/** 进度条颜色：记住的三档用记忆分布同一组由深到浅的颜色，没记住用强调色，未接触浅色 */
export const STAGE_COLOR: Record<WordStage, string> = {
  mastered: "var(--mastery-3)",
  consolidating: "var(--mastery-2)",
  fresh: "var(--mastery-1)",
  missed: "var(--accent)",
  untested: "var(--line-strong)",
};

/** 词表里状态的标签颜色 */
export const STAGE_TAG: Record<WordStage, string> = { mastered: "green", consolidating: "cyan", fresh: "blue", missed: "orange", untested: "default" };

/** 某一级的词数（没记住即计数里的 learning） */
export function stageCount(c: Pick<CoverageCounts, "untested" | "learning" | "fresh" | "consolidating" | "mastered">, s: WordStage): number {
  if (s === "missed") return c.learning;
  return c[s];
}

/** 已接触 / 应测；没有目标词（或没有目标）时为 null */
export function testedRate(c: { target: number; tested: number } | null | undefined): number | null {
  if (!c || c.target <= 0) return null;
  return c.tested / c.target;
}

/** 概览「目标覆盖」列的排序值：没有目标的排在 0% 之前 */
export function coverageRateOrder(c: { target: number; tested: number } | null | undefined): number {
  return testedRate(c) ?? -1;
}

/** 生成页预选目标来源：/sheets/new?target=untested|learning[&userId=][&bookId=] */
export function targetSheetLink(status: TargetSheetStatus, opts: { userId?: string; bookId?: string } = {}): string {
  const sp = new URLSearchParams({ target: status });
  if (opts.userId) sp.set("userId", opts.userId);
  if (opts.bookId) sp.set("bookId", opts.bookId);
  return `/sheets/new?${sp.toString()}`;
}

export function parseTargetStatus(raw: string | null | undefined): TargetSheetStatus | null {
  return raw === "untested" || raw === "learning" ? raw : null;
}

/** 把 from 位置的一项移到 to；越界或不动时原样返回（新数组） */
export function moveItem<T>(list: readonly T[], from: number, to: number): T[] {
  const out = [...list];
  if (from === to || from < 0 || to < 0 || from >= out.length || to >= out.length) return out;
  const [x] = out.splice(from, 1);
  out.splice(to, 0, x);
  return out;
}
