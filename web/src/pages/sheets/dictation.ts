/**
 * 默写单（spec 0006）前端的纯函数：生成页的切分预告、打印版式（书写区分档、按高度分页）、批改提交、链接与标签。
 * 切分口径与后端 internal/core/dictation.go 的 SplitDictation 一致；版式的尺寸单位都是毫米。
 */
import type { ClassStudentCoverage, DictItemType, DictItemView, SentenceSource, SessionResult, SheetFormat, SheetListItem, TodaySheet } from "@/types";

// ------------------------------------------------------------------
// 题型与分区
// ------------------------------------------------------------------

export const DICT_TYPE_LABEL: Record<DictItemType, string> = { word: "单词", phrase: "短语", sentence: "句子", frame: "仿写", transform: "转换" };

/** 卷面分区标题（只显示有题目的部分） */
export const SECTION_TITLE: Record<number, string> = { 1: "一、单词", 2: "二、短语", 3: "三、句子", 4: "四、仿写与转换" };

export function isSentenceType(t: string): boolean {
  return t === "sentence" || t === "frame" || t === "transform";
}

export function sectionOf(t: string): number {
  return ({ word: 1, phrase: 2, sentence: 3, frame: 4, transform: 4 } as Record<string, number>)[t] ?? 0;
}

// ------------------------------------------------------------------
// 每份题数与切分（生成页预告）
// ------------------------------------------------------------------

export interface DictLimits {
  words: number;
  phrases: number;
  sentences: number;
}

/** 每份默认 单词 20、短语 10、句子 8 */
export const DICT_DEFAULT_LIMITS: DictLimits = { words: 20, phrases: 10, sentences: 8 };
/** 每份上限（与后端 DictationWordMax / PhraseMax / SentenceMax 一致） */
export const DICT_MAX_LIMITS: DictLimits = { words: 30, phrases: 30, sentences: 20 };

export interface DictItemRef {
  type: DictItemType;
  wordId?: string;
  sentenceId?: string;
}

function dictKey(it: DictItemRef): string {
  return isSentenceType(it.type) ? `s:${it.sentenceId}` : `w:${it.wordId}`;
}

function splitEven<T>(xs: T[], k: number): T[][] {
  const out: T[][] = [];
  let start = 0;
  for (let i = 0; i < k; i++) {
    const size = Math.floor(xs.length / k) + (i < xs.length % k ? 1 : 0);
    out.push(xs.slice(start, start + size));
    start += size;
  }
  return out;
}

/**
 * 把已按「从难到易」排好的题切成多份：去重后单词、短语、句子三组各自按顺序平均分到各份（前面的份多一题），
 * 份内按卷面分区排序；份数不超过三组里最多的题数；某组超过 份数 × 每份上限 时返回错误。
 */
export function splitDictation<T extends DictItemRef>(items: T[], copies: number, lim: DictLimits): { groups: T[][] } | { error: string } {
  const seen = new Set<string>();
  const words: T[] = [];
  const phrases: T[] = [];
  const sentences: T[] = [];
  for (const it of items) {
    const k = dictKey(it);
    if (seen.has(k)) continue;
    seen.add(k);
    if (it.type === "word") words.push(it);
    else if (it.type === "phrase") phrases.push(it);
    else sentences.push(it);
  }
  const checks: [string, number, number][] = [
    ["单词", words.length, lim.words],
    ["短语", phrases.length, lim.phrases],
    ["句子", sentences.length, lim.sentences],
  ];
  for (const [label, n, per] of checks) {
    if (n > copies * per) return { error: `${label} ${n} 题，超过 ${copies} 份 × ${per} 题` };
  }
  const k = Math.min(copies, Math.max(words.length, phrases.length, sentences.length));
  if (k === 0) return { groups: [] };
  const ws = splitEven(words, k);
  const ps = splitEven(phrases, k);
  const ss = splitEven(sentences, k);
  const groups = Array.from({ length: k }, (_, i) =>
    [...ws[i], ...ps[i], ...ss[i]].map((x, j) => ({ x, j })).sort((a, b) => sectionOf(a.x.type) - sectionOf(b.x.type) || a.j - b.j).map((o) => o.x),
  );
  return { groups };
}

/**
 * 生成页选好的句子来源 → 接口的 sentenceSources：用错题再出一份（from）时只用那次批改的错句；
 * 否则依次为单元的篇（可多选）、没记住的句子、学生自己的一篇 AI 短文。
 */
export function sentenceSourcesOf(o: { from?: string; textIds: string[]; learning: boolean; passageId?: string }): SentenceSource[] {
  if (o.from) return [{ kind: "session", sessionId: o.from }];
  return [
    ...o.textIds.map((textId) => ({ kind: "text" as const, textId })),
    ...(o.learning ? [{ kind: "learning" as const }] : []),
    ...(o.passageId ? [{ kind: "passage" as const, passageId: o.passageId }] : []),
  ];
}

// ------------------------------------------------------------------
// 书写线与书写区
// ------------------------------------------------------------------

/** 书写线：横线 / 四线三格 */
export type LineStyle = "line" | "fourline";

export const LINE_STYLE_LABEL: Record<LineStyle, string> = { line: "横线", fourline: "四线三格" };

/** 小学学段（primary）默认四线三格，其他学段默认横线 */
export function defaultLineStyle(level?: string | null): LineStyle {
  return level === "primary" ? "fourline" : "line";
}

export function parseLineStyle(raw: string | null | undefined): LineStyle | undefined {
  return raw === "line" || raw === "fourline" ? raw : undefined;
}

/**
 * 书写区分档：word 半栏短格、phrase 半栏长格、line 占整行、double 占两行。
 * 单词、短语按答案长度升档；句子类占整行，太长时占两行（四线三格字大，阈值更小）。
 */
export type WriteTier = "word" | "phrase" | "line" | "double";

export function writeTier(type: DictItemType, answer: string, style: LineStyle): WriteTier {
  const n = answer.trim().length;
  if (type === "word") return n <= 12 ? "word" : n <= 20 ? "phrase" : "line";
  if (type === "phrase") return n <= 22 ? "phrase" : "line";
  return n <= (style === "fourline" ? 40 : 55) ? "line" : "double";
}

// ------------------------------------------------------------------
// 题号与分页
// ------------------------------------------------------------------

export type NumberedItem = DictItemView & { no: number };

/** 按卷面分区稳定排序后连续编号（从 1 开始） */
export function numberItems(items: DictItemView[]): NumberedItem[] {
  return items
    .map((x, j) => ({ x, j }))
    .sort((a, b) => a.x.section - b.x.section || a.j - b.j)
    .map((o, i) => ({ ...o.x, no: i + 1 }));
}

export type LaidItem = NumberedItem & { tier: WriteTier };

export type QuestionBlock =
  | { kind: "title"; section: number; title: string; cont: boolean }
  /** 单词 / 短语：两题一行（半栏），太长的占整行（full） */
  | { kind: "row"; items: LaidItem[]; full: boolean }
  /** 句子类：提示一行在上，书写区一或两行在下 */
  | { kind: "sentence"; item: LaidItem; tier: WriteTier; promptLines: number };

export interface QuestionPage {
  blocks: QuestionBlock[];
  height: number;
  budget: number;
}

/** A4 297mm，上下内边距各 12mm；页眉 14mm、页脚 6mm */
export const PAGE_BUDGET = 297 - 24 - 14 - 6;
const TITLE_H = 7;
/** 一行书写区的高度：横线 8mm；四线三格行距加大 */
export const ROW_H: Record<LineStyle, number> = { line: 8, fourline: 13 };
const PROMPT_LINE_H = 4.6;
/** 整行宽（约 179mm）能排下的宽度单位：中日文字记 2，其余记 1 */
const PROMPT_UNITS = 90;

function textUnits(s: string): number {
  let n = 0;
  for (const ch of s) n += (ch.codePointAt(0) ?? 0) >= 0x2e80 ? 2 : 1;
  return n;
}

function linesOf(s: string, perLine: number, max: number): number {
  return Math.min(max, Math.max(1, Math.ceil(textUnits(s) / perLine)));
}

function blockHeight(b: QuestionBlock, style: LineStyle): number {
  if (b.kind === "title") return TITLE_H;
  if (b.kind === "row") return ROW_H[style];
  return b.promptLines * PROMPT_LINE_H + 0.6 + (b.tier === "double" ? 2 : 1) * ROW_H[style];
}

/**
 * 题目页分页：先按分区排成块（分区标题、单词短语行、句子），再按高度预算装页。
 * 分区跨页时续页先放「（续）」标题；分区标题不单独落在页尾。
 */
export function layoutQuestionPages(items: NumberedItem[], style: LineStyle, budget = PAGE_BUDGET): QuestionPage[] {
  // 1. 按分区组织
  const sections: { section: number; blocks: QuestionBlock[] }[] = [];
  for (const it of items) {
    let sec = sections[sections.length - 1];
    if (!sec || sec.section !== it.section) {
      sec = { section: it.section, blocks: [] };
      sections.push(sec);
    }
    const laid: LaidItem = { ...it, tier: writeTier(it.type, it.answer, style) };
    if (isSentenceType(it.type)) {
      sec.blocks.push({ kind: "sentence", item: laid, tier: laid.tier, promptLines: linesOf(it.prompt, PROMPT_UNITS, 3) });
      continue;
    }
    const full = laid.tier === "line" || laid.tier === "double";
    const last = sec.blocks[sec.blocks.length - 1];
    if (!full && last && last.kind === "row" && !last.full && last.items.length === 1) last.items.push(laid);
    else sec.blocks.push({ kind: "row", items: [laid], full });
  }

  // 2. 装页
  const pages: QuestionPage[] = [];
  let page: QuestionPage = { blocks: [], height: 0, budget };
  const push = (b: QuestionBlock) => {
    page.blocks.push(b);
    page.height += blockHeight(b, style);
  };
  const newPage = () => {
    if (page.blocks.length) pages.push(page);
    page = { blocks: [], height: 0, budget };
  };
  for (const sec of sections) {
    const title = (cont: boolean): QuestionBlock => ({ kind: "title", section: sec.section, title: `${SECTION_TITLE[sec.section] ?? ""}${cont ? "（续）" : ""}`, cont });
    // 标题和第一块要一起放下，否则换页
    if (page.height + TITLE_H + blockHeight(sec.blocks[0], style) > budget) newPage();
    push(title(false));
    for (const b of sec.blocks) {
      if (page.height + blockHeight(b, style) > budget) {
        newPage();
        push(title(true));
      }
      push(b);
    }
  }
  newPage();
  return pages;
}

/** 答案页：两栏，页眉 14mm（含下边距）；每栏约 44 个宽度单位一行 */
const ANSWER_COL_H = 297 - 24 - 14;
const ANSWER_UNITS = 44;

export function answerHeight(it: NumberedItem): number {
  return linesOf(`${it.no}. ${it.answer}`, ANSWER_UNITS, 6) * 4.2 + 0.8;
}

/** 答案页分页：按估算高度，每页两栏（留 5% 余量给换行误差） */
export function answerPages(items: NumberedItem[]): NumberedItem[][] {
  const cap = ANSWER_COL_H * 2 * 0.95;
  const pages: NumberedItem[][] = [];
  let cur: NumberedItem[] = [];
  let h = 0;
  for (const it of items) {
    const ih = answerHeight(it);
    if (cur.length && h + ih > cap) {
      pages.push(cur);
      cur = [];
      h = 0;
    }
    cur.push(it);
    h += ih;
  }
  if (cur.length) pages.push(cur);
  return pages;
}

// ------------------------------------------------------------------
// 批改
// ------------------------------------------------------------------

export interface GradeItemPayload {
  index: number;
  correct: boolean;
  userAnswer?: string;
}

/** 批改提交：覆盖明细里的全部题目（按 index）；默认全对，标错的题带上记下的内容（可空） */
export function gradePayload(items: { index: number }[], wrong: Set<number>, notes: Record<number, string>): GradeItemPayload[] {
  return items.map(({ index }) => {
    if (!wrong.has(index)) return { index, correct: true };
    const ua = (notes[index] ?? "").trim();
    return ua ? { index, correct: false, userAnswer: ua } : { index, correct: false };
  });
}

// ------------------------------------------------------------------
// 链接与标签
// ------------------------------------------------------------------

export function gradeLink(sheetId: string): string {
  return `/sheets/${sheetId}/grade`;
}

/** 「用错题再出一份」：错的词和句子一起进入新默写单；老师代学生出时带 userId */
export function regenerateDictationLink(s: { sessionId: string; isOwner: boolean; userId: string }): string {
  return `/sheets/new?format=dictation&from=${s.sessionId}${s.isOwner ? "" : `&userId=${s.userId}`}`;
}

/** 打印链接带上线型（横线是默认，不带参数） */
export function printLinkWithLines(link: string, style: LineStyle): string {
  if (style === "line") return link;
  return `${link}${link.includes("?") ? "&" : "?"}lines=${style}`;
}

const SELFTEST_STATUS: Record<SheetListItem["status"], { label: string; color: string }> = {
  pending: { label: "待测试", color: "gold" },
  testing: { label: "测试中", color: "processing" },
  tested: { label: "已测试", color: "green" },
};
const DICTATION_STATUS: Record<SheetListItem["status"], { label: string; color: string }> = {
  pending: { label: "待批改", color: "gold" },
  testing: { label: "待批改", color: "gold" },
  tested: { label: "已批改", color: "green" },
};

export function sheetStatus(format: SheetFormat | undefined, status: SheetListItem["status"]): { label: string; color: string } {
  return (format === "dictation" ? DICTATION_STATUS : SELFTEST_STATUS)[status];
}

export function sheetStatusLabel(format: SheetFormat | undefined, status: SheetListItem["status"]): string {
  return sheetStatus(format, status).label;
}

/** 默写单批改组的成绩：词（首答）加上句子题 */
export function dictationScore(r: Pick<SessionResult, "correctFirst" | "totalFirst" | "sentences">): { correct: number; total: number } {
  return { correct: r.correctFirst + (r.sentences?.correct ?? 0), total: r.totalFirst + (r.sentences?.total ?? 0) };
}

/** 今日页单词单入口的标题：默写单显示「默写单 #N · 待批改」，不显示「测试」 */
export function todaySheetTitle(s: Pick<TodaySheet, "seq" | "wordCount" | "format" | "itemCount">): string {
  if (s.format === "dictation") return `默写单 #${s.seq} · 待批改 · ${s.itemCount ?? s.wordCount} 题`;
  return `单词单 #${s.seq} · ${s.wordCount} 词`;
}

/** 班级概览「目标覆盖」旁的自批比例；没有已测或没有自批时不显示 */
export function selfGradedText(cov: Pick<ClassStudentCoverage, "selfGraded" | "selfGradedRatio"> | null | undefined): string | null {
  const r = cov?.selfGradedRatio;
  if (r == null || !(r > 0)) return null;
  return `自批 ${Math.round(r * 100)}%`;
}
