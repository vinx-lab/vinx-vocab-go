/**
 * 业务请求封装：自动加 /api 前缀、带 cookie、解包 { success, data } 包络。
 * 失败抛出带 statusCode / code / requestId / errors 的 Error（与旧版 httpClient 拦截器一致）。
 */
export interface ApiError extends Error {
  statusCode: number;
  code?: string;
  requestId?: string;
  errors?: unknown;
}

/** 网关（反向代理）在后端之前出错时返回的非 JSON 状态码 */
const GATEWAY_STATUS = new Set([502, 503, 504]);

/** 响应不是后端包络时的说明：网关错误给出状态码和原因，其余带上状态码 */
export function fallbackMessage(status: number | undefined, hasResponse: boolean): string {
  if (!hasResponse || !status) return "请求失败";
  if (GATEWAY_STATUS.has(status)) return `网关错误（${status}）：请求等待时间过长或服务不可用`;
  return `请求失败（${status}）`;
}

function makeError(message: string, statusCode: number, env?: { code?: string; requestId?: string; details?: unknown }): ApiError {
  return Object.assign(new Error(message), { statusCode, code: env?.code, requestId: env?.requestId, errors: env?.details });
}

function qs(params?: Record<string, unknown>): string {
  if (!params) return "";
  const sp = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (v === undefined || v === null) continue;
    if (Array.isArray(v)) v.forEach((x) => sp.append(`${k}[]`, String(x)));
    else sp.append(k, String(v));
  }
  const s = sp.toString();
  return s ? `?${s}` : "";
}

/** 底层请求：返回解包后的 data */
export async function request<T>(method: string, url: string, body?: unknown, params?: Record<string, unknown>): Promise<T> {
  const headers: Record<string, string> = {};
  let payload: BodyInit | undefined;
  if (body !== undefined) {
    headers["Content-Type"] = "application/json";
    payload = JSON.stringify(body);
  }
  let res: Response;
  try {
    res = await fetch(`/api${url}${qs(params)}`, { method, headers, body: payload, credentials: "include" });
  } catch {
    throw makeError(fallbackMessage(undefined, false), 500);
  }
  let json: { success?: boolean; data?: T; error?: { message?: string; code?: string; requestId?: string; details?: unknown } } | undefined;
  try {
    json = await res.json();
  } catch {
    json = undefined;
  }
  if (!res.ok || !json || json.success === false) {
    const env = json?.error?.message ? json.error : undefined;
    throw makeError(env?.message ?? fallbackMessage(res.status, true), res.status, env);
  }
  return json.data as T;
}

export const api = {
  get: <T>(url: string, params?: Record<string, unknown>) => request<T>("GET", url, undefined, params),
  // 与旧版 axios 行为一致：post/patch/put 没有 body 时发 {}
  post: <T>(url: string, body?: unknown) => request<T>("POST", url, body ?? {}),
  patch: <T>(url: string, body?: unknown) => request<T>("PATCH", url, body ?? {}),
  put: <T>(url: string, body?: unknown) => request<T>("PUT", url, body ?? {}),
  del: <T>(url: string) => request<T>("DELETE", url),
};

export function errorMessage(e: unknown, fallback = "操作失败"): string {
  return (e as Error)?.message || fallback;
}
