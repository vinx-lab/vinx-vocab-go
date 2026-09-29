/**
 * 契约测试的 HTTP 客户端：纯黑盒，只经 HTTP 访问被测实例。
 *
 * BASE_URL：
 * - oracle（旧 Node 后端，无 /api 前缀）：http://localhost:4100
 * - Go 单文件（API 挂在 /api 下）：http://localhost:3200/api
 *
 * 目标解析（BASE_URL / CONTRACT_BASE_URL / TARGET 三个变量怎么合并、冲突时怎么办）统一在 lib/target.ts，
 * 这里不再重复判断逻辑，只从已经过 vitest.config.ts 解析并校验过的 process.env 读一遍（worker 进程里
 * CONTRACT_BASE_URL / TARGET 已经是一致的，这里再跑一遍 resolveTarget 是防御性的，不会改变结果）。
 * `global-setup.ts` 会在任何用例跑之前，用 X-Vinx-Vocab 响应头再做一次活体探测，防住"配置自洽但连错实例"。
 *
 * 每个 Client 自带一个简易 cookie jar（只关心 vinx_token 这类 name=value，按 Max-Age=0 删除）。
 */
import { resolveTarget } from "./target";

const resolved = resolveTarget();
export const BASE_URL = resolved.baseUrl;

/** 被测实现：oracle | go。 */
export const TARGET: "oracle" | "go" = resolved.target;

export const DEMO_PASSWORD = "dev123456";

export type Method = "GET" | "POST" | "PUT" | "PATCH" | "DELETE" | "HEAD";

export interface CallResult<T = any> {
  status: number;
  body: T;
  headers: Headers;
  /** 原始 Set-Cookie 行 */
  setCookie: string[];
  text: string;
}

export interface CallOptions {
  headers?: Record<string, string>;
  /** 原样发送的请求体（不做 JSON 序列化，也不自动加 content-type） */
  raw?: string;
}

export class Client {
  readonly jar = new Map<string, string>();

  constructor(readonly baseUrl = BASE_URL) {}

  /** 当前 cookie 头 */
  cookieHeader(): string {
    return [...this.jar.entries()].map(([k, v]) => `${k}=${v}`).join("; ");
  }

  private absorb(setCookie: string[]) {
    for (const line of setCookie) {
      const [pair, ...attrs] = line.split(";").map((s) => s.trim());
      const eq = pair.indexOf("=");
      if (eq < 0) continue;
      const name = pair.slice(0, eq);
      const value = pair.slice(eq + 1);
      const expired = attrs.some((a) => /^max-age=0$/i.test(a) || /^expires=thu, 01 jan 1970/i.test(a));
      if (expired || value === "") this.jar.delete(name);
      else this.jar.set(name, value);
    }
  }

  async call<T = any>(method: Method, path: string, body?: unknown, opts: CallOptions = {}): Promise<CallResult<T>> {
    const headers: Record<string, string> = { ...(opts.headers ?? {}) };
    const cookie = this.cookieHeader();
    if (cookie && !("cookie" in headers)) headers.cookie = cookie;
    let payload: string | undefined;
    if (opts.raw !== undefined) {
      payload = opts.raw;
    } else if (body !== undefined) {
      payload = JSON.stringify(body);
      headers["content-type"] ??= "application/json";
    }
    const res = await fetch(`${this.baseUrl}${path}`, { method, headers, body: payload, redirect: "manual" });
    const setCookie = res.headers.getSetCookie();
    this.absorb(setCookie);
    const text = await res.text();
    let parsed: unknown = text;
    try {
      parsed = text ? JSON.parse(text) : undefined;
    } catch {
      /* 非 JSON 响应（如音频）保留原文 */
    }
    return { status: res.status, body: parsed as T, headers: res.headers, setCookie, text };
  }

  get<T = any>(path: string, opts?: CallOptions) {
    return this.call<T>("GET", path, undefined, opts);
  }
  post<T = any>(path: string, body?: unknown, opts?: CallOptions) {
    return this.call<T>("POST", path, body, opts);
  }
  put<T = any>(path: string, body?: unknown, opts?: CallOptions) {
    return this.call<T>("PUT", path, body, opts);
  }
  patch<T = any>(path: string, body?: unknown, opts?: CallOptions) {
    return this.call<T>("PATCH", path, body, opts);
  }
  del<T = any>(path: string, opts?: CallOptions) {
    return this.call<T>("DELETE", path, undefined, opts);
  }
}

/** 未登录的客户端 */
export function anon(baseUrl = BASE_URL): Client {
  return new Client(baseUrl);
}

/** 登录并返回带 cookie 的客户端；登录失败直接抛错 */
export async function login(email: string, password = DEMO_PASSWORD, baseUrl = BASE_URL): Promise<Client> {
  const c = new Client(baseUrl);
  const res = await c.post("/auth/login", { email, password });
  if (res.status !== 200) throw new Error(`登录失败 ${email}: ${res.status} ${res.text}`);
  return c;
}

/** 便捷形式：call(client, method, url, body) → { status, body } */
export async function call<T = any>(client: Client, method: Method, url: string, body?: unknown) {
  const r = await client.call<T>(method, url, body);
  return { status: r.status, body: r.body };
}

/** 每次运行唯一的后缀，避免与之前运行留下的数据冲突 */
export const STAMP = `${Date.now()}${Math.floor(Math.random() * 1000)}`;
