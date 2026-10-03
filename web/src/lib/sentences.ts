/**
 * 句子与篇（spec 0004）的前端纯函数：分词与高亮、分段、导入预览转请求体、单元内篇排序、编辑行与句子互转、词 → 句截断。
 * 不依赖组件，单测见 test/sentences.test.ts。
 */
import type { PreviewText, PreviewSentence, Sentence, SentenceInput, SentenceWordRef, TextInput, TextKind, WordSentenceItem, WordSentences } from "@/types";

export interface Token {
  text: string;
  /** 第几个词（从 0 开始，与后端 SentenceWord.position 对应） */
  index: number;
  /** 原文中的起止位置（JS 字符串下标） */
  start: number;
  end: number;
}

const isWordChar = (c: string | undefined) => !!c && /[\p{L}\p{N}]/u.test(c);
const isApos = (c: string | undefined) => c === "'" || c === "’";

/**
 * 分词，与后端 lemma.Tokenize 同口径：字母数字连成词；撇号、连字符夹在词中间时算词内（don't、T-shirt），
 * 词尾跟在 s 后面的撇号也算（students'）。其余字符都是分隔符。
 */
export function tokenize(s: string): Token[] {
  const out: Token[] = [];
  const chars = Array.from(s);
  const offs: number[] = [];
  let o = 0;
  for (const c of chars) {
    offs.push(o);
    o += c.length;
  }
  offs.push(o);
  let start = -1;
  const flush = (end: number) => {
    if (start >= 0) out.push({ text: s.slice(offs[start], offs[end]), index: out.length, start: offs[start], end: offs[end] });
    start = -1;
  };
  for (let i = 0; i < chars.length; i++) {
    const c = chars[i];
    if (isWordChar(c)) {
      if (start < 0) start = i;
    } else if (start >= 0 && (isApos(c) || c === "-") && isWordChar(chars[i + 1])) {
      // 词内撇号 / 连字符
    } else if (start >= 0 && isApos(c) && (chars[i - 1] === "s" || chars[i - 1] === "S") && !isWordChar(chars[i + 1])) {
      flush(i + 1); // 复数所有格
    } else {
      flush(i);
    }
  }
  flush(chars.length);
  return out;
}

export interface Segment {
  text: string;
  /** 关联到的词；普通文字没有 */
  wordId?: string;
  /** 是不是当前要突出的词 */
  target?: boolean;
}

/**
 * 按句子的词关联把英文切成片段：关联到的词（短语按 form 的词数覆盖多个词）单独成段，其余原样保留。
 * 重叠或越界的关联跳过（后端「先长后短」，正常不会重叠）。
 */
export function highlightSegments(en: string, words: SentenceWordRef[], targetId?: string): Segment[] {
  const toks = tokenize(en);
  const spans: { start: number; end: number; wordId: string; to: number }[] = [];
  let covered = -1;
  for (const w of [...words].sort((a, b) => a.position - b.position)) {
    const n = Math.max(1, tokenize(w.form).length);
    const first = toks[w.position];
    const last = toks[w.position + n - 1];
    if (!first || !last || w.position <= covered) continue;
    spans.push({ start: first.start, end: last.end, wordId: w.wordId, to: w.position + n - 1 });
    covered = w.position + n - 1;
  }
  const out: Segment[] = [];
  let at = 0;
  for (const sp of spans) {
    if (sp.start > at) out.push({ text: en.slice(at, sp.start) });
    out.push({ text: en.slice(sp.start, sp.end), wordId: sp.wordId, target: sp.wordId === targetId });
    at = sp.end;
  }
  if (at < en.length || out.length === 0) out.push({ text: en.slice(at) });
  return out;
}

/** 相邻的同一段号归为一段（课文按段落显示） */
export function groupParagraphs<T extends { paragraph: number }>(list: T[]): T[][] {
  const out: T[][] = [];
  let prev: number | undefined;
  for (const s of list) {
    if (out.length === 0 || s.paragraph !== prev) out.push([]);
    out[out.length - 1].push(s);
    prev = s.paragraph;
  }
  return out;
}

export const DEFAULT_TITLE: Record<TextKind, string> = { list: "重点句型", text: "课文" };
export const KIND_LABEL: Record<TextKind, string> = { list: "句型", text: "课文" };

export type EditPreviewSentence = PreviewSentence & { include: boolean };
export type EditPreviewText = Omit<PreviewText, "sentences"> & { sentences: EditPreviewSentence[] };

/** 导入预览（可能改过、取消勾选过）→ 请求体里的篇：只带勾选且中英文都有的句子，没有句子的篇去掉 */
export function previewTextsPayload(texts: EditPreviewText[]): TextInput[] {
  const out: TextInput[] = [];
  for (const t of texts) {
    const sentences: SentenceInput[] = [];
    for (const s of t.sentences) {
      const en = s.en.trim();
      const cn = s.cn.trim();
      if (!s.include || !en || !cn) continue;
      const frame = s.frame.trim();
      sentences.push({ en, cn, ...(frame ? { frame } : {}), paragraph: s.paragraph });
    }
    if (sentences.length === 0) continue;
    out.push({ kind: t.kind, title: t.title.trim() || DEFAULT_TITLE[t.kind], titleCn: t.titleCn.trim() || null, sentences });
  }
  return out;
}

/**
 * 单元内只重排一种类型的篇：该类型原来占的位置按新顺序依次填回，另一种保持原位。
 * 后端要求顺序包含本单元的全部篇。
 */
export function mergeKindOrder(all: { id: string; kind: string }[], kind: string, reorderedIds: string[]): string[] {
  let k = 0;
  return all.map((t) => (t.kind === kind ? reorderedIds[k++] ?? t.id : t.id));
}

/** 把 from 位置的一项移到 to 位置；越界时原样返回 */
export function moveItem<T>(list: T[], from: number, to: number): T[] {
  if (from < 0 || from >= list.length || to < 0 || to >= list.length || from === to) return list;
  const next = [...list];
  const [x] = next.splice(from, 1);
  next.splice(to, 0, x);
  return next;
}

/** 句子编辑框里的一行：已有句子带 id；newParagraph 表示从这一句起另起一段（只对课文有效） */
export interface EditRow {
  key: string;
  id?: string;
  en: string;
  cn: string;
  frame: string;
  newParagraph: boolean;
}

export function editRowsFromSentences(sentences: Sentence[]): EditRow[] {
  return sentences.map((s, i) => ({
    key: s.id,
    id: s.id,
    en: s.en,
    cn: s.cn,
    frame: s.frame ?? "",
    newParagraph: i > 0 && s.paragraph !== sentences[i - 1].paragraph,
  }));
}

/** 编辑行 → PUT /texts/:id/sentences 的句子（段号从 0 重新连续编号；句型清单都是 0） */
export function editRowsToInputs(rows: EditRow[], kind: TextKind): SentenceInput[] {
  let paragraph = 0;
  return rows.map((r, i) => {
    if (kind === "text" && i > 0 && r.newParagraph) paragraph++;
    const frame = r.frame.trim();
    return { ...(r.id ? { id: r.id } : {}), en: r.en.trim(), cn: r.cn.trim(), frame: frame || null, paragraph };
  });
}

const WORD_SENTENCE_GROUPS: { key: keyof WordSentences; label: string }[] = [
  { key: "examples", label: "例句" },
  { key: "patterns", label: "课本句型" },
  { key: "texts", label: "课文" },
  { key: "passages", label: "我的短文" },
];

/** 词 → 句：按 例句 → 课本句型 → 课文 → 我的短文 依次取，最多 limit 条（null 不限），空组不出现 */
export function limitWordSentences(ws: WordSentences, limit: number | null): { total: number; groups: { key: keyof WordSentences; label: string; items: WordSentenceItem[] }[] } {
  let left = limit ?? Infinity;
  let total = 0;
  const groups: { key: keyof WordSentences; label: string; items: WordSentenceItem[] }[] = [];
  for (const g of WORD_SENTENCE_GROUPS) {
    const all = ws[g.key] ?? [];
    total += all.length;
    const items = all.slice(0, Math.max(0, left));
    left -= items.length;
    if (items.length > 0) groups.push({ ...g, items });
  }
  return { total, groups };
}
