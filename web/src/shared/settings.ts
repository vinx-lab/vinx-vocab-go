/**
 * 系统设置（K38）：只放需要在运行时修改的配置——AI 接口与 AI 提示词的默认要求模板（K41）。
 * 接口：GET / PUT / DELETE /settings/ai，POST /settings/ai/test，
 * GET / PUT /settings/ai/prompts，DELETE /settings/ai/prompts/:key（默认要求模板，K41；都要求 `system` 能力）。
 */

/** 页面上可选的 AI 供应商：OpenAI 兼容 / Claude / 关闭 */
export type AiSettingProvider = "openai" | "anthropic" | "off";

export const AI_SETTING_PROVIDERS: AiSettingProvider[] = ["openai", "anthropic", "off"];

export const AI_SETTING_PROVIDER_LABEL: Record<AiSettingProvider, string> = {
  openai: "OpenAI 兼容",
  anthropic: "Claude",
  off: "关闭",
};

/** 超时范围（秒），与环境变量 AI_TIMEOUT_MS 的 5s–600s 一致 */
export const AI_TIMEOUT_SEC_MIN = 5;
export const AI_TIMEOUT_SEC_MAX = 600;

/** GET /settings/ai：当前生效的配置。API Key 从不返回，只返回是否已设置和末 4 位 */
export interface AiSettingsView {
  /** 生效配置的来源：页面保存的（数据库）/ 环境变量 */
  source: "db" | "env";
  provider: AiSettingProvider;
  baseUrl: string | null;
  model: string;
  timeoutSec: number;
  apiKey: { set: boolean; last4: string | null };
  /** 已保存的 Key 无法解密（更换了 SETTINGS_SECRET），AI 按「未配置 Key」处理 */
  keyUndecryptable: boolean;
  /** AI 当前是否可用 */
  enabled: boolean;
  /** 来源为页面时的保存时间 */
  updatedAt: string | null;
}

/** PUT /settings/ai、POST /settings/ai/test 的请求体 */
export interface AiSettingsInput {
  provider: AiSettingProvider;
  baseUrl?: string | null;
  /** 留空表示沿用当前生效的 Key */
  apiKey?: string;
  /** 清除 Key（例如改用不需要 Key 的本机服务） */
  clearApiKey?: boolean;
  model?: string;
  timeoutSec?: number;
}

/** POST /settings/ai/test 的结果；测试不会保存 */
export interface AiTestResult {
  ok: boolean;
  ms: number;
  error?: string;
}

// ------------------------------------------------------------------
// AI 提示词默认要求模板（K41，部分取代 K40）
// ------------------------------------------------------------------

/** 可编辑的默认要求模板：例句 / 短文 / 句型 / 仿写（spec 0005 从 2 项扩展到 4 项） */
export type AiPromptKey = "example" | "passage" | "pattern" | "variant";

export const AI_PROMPT_KEYS: AiPromptKey[] = ["example", "passage", "pattern", "variant"];

export const AI_PROMPT_LABEL: Record<AiPromptKey, string> = {
  example: "例句",
  passage: "短文",
  pattern: "句型",
  variant: "仿写",
};

/** 生成时在要求模板后面接上的内容（设置页说明用） */
export const AI_PROMPT_APPENDIX: Record<AiPromptKey, string> = {
  example: "这次的单词列表",
  passage: "这次的单词列表和主题",
  pattern: "单元名、话题、生成数量、本单元的单词和已有的句型",
  variant: "改造方式、每句几个变式、例句和替换用的词汇",
};

/** 模板里可以用的占位符（spec 0005 §2），生成时按学段替换成具体值 */
export const AI_PROMPT_PLACEHOLDERS = ["{学段}", "{句长}", "{短文长度}", "{单句上限}"];

/** 模板、以及生成前页面上可见提示词的最大长度（去掉首尾空白后） */
export const AI_PROMPT_MAX_LENGTH = 6000;

/**
 * GET /settings/ai/prompts 里的一项：默认要求模板。
 * 不含输出格式（输出格式由服务端生成时自动加上，不显示、不可改）。
 */
export interface AiPromptItem {
  key: AiPromptKey;
  /** 当前生效的模板（自定义或默认） */
  current: string;
  isDefault: boolean;
  /** 代码里的默认模板，页面对照用 */
  defaultText: string;
}

export type AiPromptsView = Record<AiPromptKey, AiPromptItem>;

/** PUT /settings/ai/prompts：只更新传了的项 */
export type AiPromptsInput = Partial<Record<AiPromptKey, string>>;
