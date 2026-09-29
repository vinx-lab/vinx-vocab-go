/**
 * 统一响应包络
 */
export interface ApiErrorShape {
  /** 业务错误码，如 "USER_NOT_FOUND" */
  code: string;
  /** 用户友好的错误信息 */
  message: string;
  /** 可选：字段级错误详情（zod 校验失败映射） */
  details?: Record<string, unknown>;
  /** 可选：请求追踪 ID */
  requestId?: string;
}

export interface ApiResponse<T = unknown> {
  /** 请求是否成功 */
  success: boolean;
  /** 成功时返回业务数据 */
  data?: T;
  /** 失败时返回错误信息 */
  error?: ApiErrorShape;
  /** 可选：响应时间戳（ISO 字符串） */
  timestamp?: string;
}

/**
 * 列表分页数据结构
 */
export interface PaginatedData<T> {
  items: T[];
  total: number;
  page?: number;
  limit?: number;
}
