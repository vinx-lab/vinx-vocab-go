/**
 * AI 生成前预览与后台任务（K41，spec 0006）。
 *
 * - 预览：`POST /ai/passages/preview`、`POST /ai/units/:id/examples/preview`、`POST /ai/words/:id/example/preview`
 *   返回这次要用的单词和默认的可见提示词（要求模板 + 单词列表 + 主题），不调用 AI。
 * - 生成：`POST /passages/generate`、`POST /ai/units/:id/examples`、`POST /ai/words/:id/example`
 *   请求体带 `wordIds` 和 `prompt`（可见提示词），立即返回 `{ jobId }`，生成在服务端后台进行。
 * - 进度：`GET /ai/jobs/:id`，只有发起人能查；完成后保留 1 小时，api 重启后丢失。
 */

/** 预览里的单词 */
export interface AiPreviewWord {
  id: string;
  spelling: string;
  definition: string;
}

/** 预览结果：单词 + 默认的可见提示词 */
export interface AiPreview {
  words: AiPreviewWord[];
  prompt: string;
  /** 这次生成用的学段（spec 0005） */
  level?: AiLevel;
}

// ------------------------------------------------------------------
// 学段与生成后检查（spec 0005）
// ------------------------------------------------------------------

/** 学段：小学 / 初中 / 中考冲刺 */
export type AiLevel = "primary" | "junior" | "exam";

export const AI_LEVELS: AiLevel[] = ["primary", "junior", "exam"];

export const AI_LEVEL_LABEL: Record<AiLevel, string> = { primary: "小学", junior: "初中", exam: "中考冲刺" };

/** 各学段的默认参数（与后端 core/ai/levels.go 一致，页面说明用） */
export const AI_LEVEL_HINT: Record<AiLevel, string> = {
  primary: "句长 4～10 词，短文 50～80 词，单句不超过 12 词",
  junior: "句长 6～14 词，短文 80～140 词，单句不超过 20 词",
  exam: "句长 8～20 词，短文 120～200 词，单句不超过 25 词",
};

export const isAiLevel = (v: unknown): v is AiLevel => typeof v === "string" && (AI_LEVELS as string[]).includes(v);

/**
 * 一句话的检查结果。error（标红）= 目标词没用上；warning（标黄）= 超纲、太长、结构偏离。
 * issues 是给人看、也给「重写这一句」用的问题说明。只做提示，不拦截。
 */
export interface SentenceChecks {
  words: number;
  maxWords: number;
  missingTargets: string[];
  outOfScope: string[];
  tooLong: boolean;
  /** 仿写：和原句的结构相似度（其他为 null） */
  similarity: number | null;
  structureDeviates: boolean;
  warning: boolean;
  error: boolean;
  issues: string[];
}

/** 仿写的改造方式 */
export type VariantMode = "replace" | "transform" | "expand" | "transfer";

export const VARIANT_MODES: VariantMode[] = ["replace", "transform", "expand", "transfer"];

export const VARIANT_MODE_LABEL: Record<VariantMode, string> = { replace: "替换", transform: "转换", expand: "扩展", transfer: "迁移" };

export const VARIANT_MODE_HINT: Record<VariantMode, string> = {
  replace: "保留句型，换内容",
  transform: "肯定 ↔ 否定 ↔ 疑问，时态，主动 ↔ 被动",
  expand: "加原因、时间、地点等成分",
  transfer: "同一个句型换一个话题场景",
};

/** 句型数量、仿写的例句数与每句变式数、粘贴 / 上传上限（与后端一致） */
export const PATTERN_COUNT_MIN = 4;
export const PATTERN_COUNT_MAX = 12;
export const PATTERN_COUNT_DEFAULT = 8;
export const VARIANT_ORIGIN_LIMIT = 10;
export const VARIANT_PER_ITEM_MIN = 1;
export const VARIANT_PER_ITEM_MAX = 5;
export const VARIANT_PER_ITEM_DEFAULT = 3;
export const VARIANT_PASTED_MAX_BYTES = 20 * 1024;

/** POST /ai/units/:id/patterns/preview */
export interface AiPatternPreview {
  unitId: string;
  unitName: string;
  level: AiLevel;
  topic: string;
  count: number;
  words: AiPreviewWord[];
  /** 本单元已有的句型（让 AI 不要重复） */
  existing: string[];
  prompt: string;
}

/** 句型草稿的一句 */
export interface AiPatternDraftItem {
  en: string;
  cn: string;
  frame: string | null;
  checks: SentenceChecks;
}

/** 句型任务的结果（草稿只在服务端内存里，1 小时后或服务重启后丢失） */
export interface AiPatternDraft {
  unitId: string;
  level: AiLevel;
  /** 保存时的默认标题 */
  title: string;
  model: string;
  items: AiPatternDraftItem[];
}

/** 仿写的例句：已有句子带 id，粘贴的 id 为 null */
export interface AiVariantOrigin {
  id: string | null;
  en: string;
  cn: string;
}

/** POST /ai/variants/preview */
export interface AiVariantPreview {
  unitId: string;
  level: AiLevel;
  origins: AiVariantOrigin[];
  modes: VariantMode[];
  perItem: number;
  vocab: "unit" | "target";
  words: AiPreviewWord[];
  prompt: string;
}

/** 仿写草稿的一句：origin 是例句序号（从 1 开始） */
export interface AiVariantDraftItem {
  origin: number;
  originId: string | null;
  originEn: string;
  en: string;
  cn: string;
  /** 认不出时为空串 */
  change: VariantMode | "";
  note: string;
  checks: SentenceChecks;
}

export interface AiVariantDraft {
  unitId: string;
  level: AiLevel;
  title: string;
  model: string;
  origins: AiVariantOrigin[];
  items: AiVariantDraftItem[];
}

/** POST /ai/sentences/rewrite 的结果（不写库） */
export interface AiRewriteResult {
  en: string;
  cn: string;
  frame?: string;
  level: AiLevel;
  checks: SentenceChecks;
}

/** 单元补例句预览额外返回：本单元缺例句的词数 */
export interface AiUnitExamplesPreview extends AiPreview {
  remaining: number;
}

export type AiJobKind = "passage" | "examples" | "example" | "patterns" | "variants";
export type AiJobStatus = "running" | "done" | "failed";

/** 生成接口的返回 */
export interface AiJobStarted {
  jobId: string;
}

/** GET /ai/jobs/:id */
export interface AiJobView<T = unknown> {
  id: string;
  kind: AiJobKind;
  status: AiJobStatus;
  elapsedMs: number;
  result?: T;
  error?: string;
}

/** 短文任务的结果：已保存的短文 id（页面据此打开短文） */
export interface AiPassageJobResult {
  id: string;
  title: string;
  missingWords: string[];
  /** spec 0005：这次用的学段、保存下来的逐句结构与每句的检查结果 */
  level?: AiLevel;
  sentences?: { en: string; cn: string; paragraph: number; checks: SentenceChecks }[];
}

/** 例句任务的结果 */
export interface AiExamplesJobResult {
  items: { wordId: string; spelling: string; example: string; exampleCn: string; clozeReady: boolean; checks?: SentenceChecks }[];
  failed: string[];
  /** 单元里还缺例句的词数（单个词的任务没有） */
  remaining?: number;
  level?: AiLevel;
}

/** 前端轮询间隔 */
export const AI_JOB_POLL_MS = 2000;
/** 同一个账号同时进行中的任务上限 */
export const AI_JOB_MAX_RUNNING = 2;
/** 任务完成后保留多久 */
export const AI_JOB_TTL_MS = 60 * 60 * 1000;
