/** 目标词书与覆盖进度（spec 0003）的前端纯函数 */
import type { CoverageStatus, TargetSheetStatus } from "@/types";

export const COVERAGE_LABEL: Record<CoverageStatus, string> = { known: "会了", learning: "要学", untested: "未测" };

/** 三段进度条的颜色：会了（好）/ 要学（强调）/ 未测（浅色） */
export const COVERAGE_COLOR: Record<CoverageStatus, string> = { known: "var(--good)", learning: "var(--accent)", untested: "var(--line-strong)" };

/** 词表里状态的标签颜色 */
export const COVERAGE_TAG: Record<CoverageStatus, string> = { known: "green", learning: "orange", untested: "default" };

/** 已测 / 应测；没有目标词（或没有目标）时为 null */
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
