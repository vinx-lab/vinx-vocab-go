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
}

/** 单元补例句预览额外返回：本单元缺例句的词数 */
export interface AiUnitExamplesPreview extends AiPreview {
  remaining: number;
}

export type AiJobKind = "passage" | "examples" | "example";
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
}

/** 例句任务的结果 */
export interface AiExamplesJobResult {
  items: { wordId: string; spelling: string; example: string; exampleCn: string; clozeReady: boolean }[];
  failed: string[];
  /** 单元里还缺例句的词数（单个词的任务没有） */
  remaining?: number;
}

/** 前端轮询间隔 */
export const AI_JOB_POLL_MS = 2000;
/** 同一个账号同时进行中的任务上限 */
export const AI_JOB_MAX_RUNNING = 2;
/** 任务完成后保留多久 */
export const AI_JOB_TTL_MS = 60 * 60 * 1000;
