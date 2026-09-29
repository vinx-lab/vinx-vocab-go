import type { ApiErrorShape } from "./api-response";

/**
 * 业务错误码全集
 * 对齐 handoff/docs/02-api-contract.md 的错误码集合，另增 INVALID_EMAIL。
 */
export type ErrorCode =
  | "UNAUTHORIZED"
  | "FORBIDDEN"
  | "VALIDATION"
  | "NOT_FOUND"
  | "INVALID_JSON"
  | "MISSING_FIELDS"
  | "INVALID_EMAIL"
  | "EMAIL_EXISTS"
  | "INVALID_ACTION"
  | "INVALID_STATUS"
  | "DUPLICATE"
  | "DUPLICATE_NAME"
  | "NO_DATA"
  | "TOO_MANY_REQUESTS"
  | "SERVER"
  | "UNKNOWN";

/** 错误码 → 建议 HTTP 状态映射 */
export const ERROR_HTTP_STATUS: Record<ErrorCode, number> = {
  UNAUTHORIZED: 401,
  FORBIDDEN: 403,
  VALIDATION: 400,
  NOT_FOUND: 404,
  INVALID_JSON: 400,
  MISSING_FIELDS: 400,
  INVALID_EMAIL: 400,
  EMAIL_EXISTS: 409,
  INVALID_ACTION: 400,
  INVALID_STATUS: 400,
  DUPLICATE: 409,
  DUPLICATE_NAME: 409,
  NO_DATA: 404,
  TOO_MANY_REQUESTS: 429,
  SERVER: 500,
  UNKNOWN: 500,
};

/** 后端业务异常：携带业务错误码、HTTP 状态与可选详情 */
export class ApiError extends Error {
  readonly code: ErrorCode;
  readonly statusCode: number;
  readonly details?: Record<string, unknown>;
  readonly requestId?: string;

  constructor(
    code: ErrorCode,
    message: string,
    options?: {
      statusCode?: number;
      details?: Record<string, unknown>;
      requestId?: string;
    },
  ) {
    super(message);
    this.name = "ApiError";
    this.code = code;
    this.statusCode = options?.statusCode ?? ERROR_HTTP_STATUS[code];
    this.details = options?.details;
    this.requestId = options?.requestId;
  }

  /** 转成包络 error 形状 */
  toShape(): ApiErrorShape {
    return {
      code: this.code,
      message: this.message,
      details: this.details,
      requestId: this.requestId,
    };
  }
}
