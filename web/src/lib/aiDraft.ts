/**
 * AI 生成草稿（spec 0005）的前端纯函数：检查标记的颜色、句型 / 仿写保存的请求体、重写一句、粘贴例句的计数与大小。
 * 不依赖组件，单测见 test/aiDraft.test.ts。
 */
import type { AiLevel, AiRewriteResult, SentenceChecks, VariantMode } from "@vinx/shared";

export type CheckTone = "error" | "warning" | "ok";

/** 标红（目标词没用上）优先于标黄（超纲、太长、结构偏离） */
export function checkTone(c: SentenceChecks | null | undefined): CheckTone {
  if (!c) return "ok";
  if (c.error) return "error";
  if (c.warning) return "warning";
  return "ok";
}

/** 草稿的一行带一个本地 key（编辑、删除时定位用） */
export type Keyed<T> = T & { key: string };

let keySeq = 0;
export function withKeys<T extends object>(items: T[]): Keyed<T>[] {
  return items.map((it) => ({ ...it, key: `d${++keySeq}` }));
}

export interface SaveTarget {
  /** 追加到本单元已有的句型清单；不带时新建一篇 */
  textId?: string;
  /** 新建时的标题 */
  title?: string;
}

const targetFields = (t: SaveTarget) => (t.textId ? { textId: t.textId } : t.title?.trim() ? { title: t.title.trim() } : {});

export interface PatternRow {
  en: string;
  cn: string;
  frame: string | null;
}

/** POST /ai/units/:id/patterns/save 的请求体 */
export function patternSaveBody(rows: PatternRow[], target: SaveTarget) {
  return {
    ...targetFields(target),
    sentences: rows.map((r) => {
      const frame = (r.frame ?? "").trim();
      return { en: r.en.trim(), cn: r.cn.trim(), ...(frame ? { frame } : {}) };
    }),
  };
}

export interface VariantRow {
  originId: string | null;
  en: string;
  cn: string;
  change: VariantMode | "";
  note: string;
}

/** POST /ai/variants/save 的请求体：改造方式与说明分开提交，服务端拼成「替换：改了什么」 */
export function variantSaveBody(unitId: string, rows: VariantRow[], target: SaveTarget) {
  return {
    unitId,
    ...targetFields(target),
    sentences: rows.map((r) => {
      const note = r.note.trim();
      return {
        en: r.en.trim(),
        cn: r.cn.trim(),
        ...(r.originId ? { originId: r.originId } : {}),
        ...(note ? { variantNote: note } : {}),
        ...(r.change ? { change: r.change } : {}),
      };
    }),
  };
}

/** 保存前的检查：返回给人看的提示，没问题时为 null */
export function draftProblem(rows: { en: string; cn: string }[]): string | null {
  if (rows.length === 0) return "至少保留一句";
  const i = rows.findIndex((r) => !r.en.trim() || !r.cn.trim());
  return i >= 0 ? `第 ${i + 1} 句的英文和中文都不能为空` : null;
}

/** POST /ai/sentences/rewrite 的请求体：这句和标出的问题；仿写带上原句 */
export function rewriteBody(
  kind: "pattern" | "variant",
  row: { en: string; cn: string; checks: SentenceChecks; originEn?: string },
  level: AiLevel,
  unitId: string,
) {
  return {
    kind,
    en: row.en.trim(),
    cn: row.cn.trim(),
    issues: row.checks.issues,
    level,
    unitId,
    ...(kind === "variant" && row.originEn ? { origin: row.originEn } : {}),
  };
}

/** 用重写结果只替换这一句的英文、中文、检查结果；返回带骨架时一并替换 */
export function applyRewrite<T extends { en: string; cn: string; checks: SentenceChecks; frame?: string | null }>(row: T, r: AiRewriteResult): T {
  const next = { ...row, en: r.en, cn: r.cn, checks: r.checks };
  if (r.frame && "frame" in row) next.frame = r.frame;
  return next;
}

/** 粘贴 / 上传的例句数：每行一句，「英文 | 中文」或只写英文；没有英文的行不算（与服务端 ParsePastedOrigins 一致） */
export function countPastedOrigins(text: string): number {
  return text
    .replace(/\r\n/g, "\n")
    .split("\n")
    .filter((line) => line.replace(/｜/g, "|").split("|")[0].trim() !== "").length;
}

/** UTF-8 字节数（服务端按字节限制 20KB） */
export function utf8Bytes(s: string): number {
  return new TextEncoder().encode(s).length;
}
