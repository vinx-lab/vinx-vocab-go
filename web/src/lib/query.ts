/**
 * 极简数据请求缓存（替代 TanStack Query 的常用部分），默认值与旧版一致
 * （旧版 QueryClient 由 refine 创建：refetchOnWindowFocus=false、placeholderData=keepPreviousData，其余为 TanStack 默认）：
 * - useApi(key, fn, opts)：按 key 缓存；挂载时过期则后台重取；失败按 1s/2s/4s 重试 3 次；换 key 时保留上一个 key 的数据
 * - invalidate(prefix)：前缀匹配的缓存标记过期，正在用的立即重取（在途请求作废，与 TanStack cancelRefetch 一致）
 * - useMutation({ mutationFn, onSuccess, onError, onSettled, onMutate })
 * - queryClient：invalidateQueries / setQueryData / getQueryData / removeQueries（方便从旧代码迁移）
 * - clearCache()：退出登录 / 登录态失效时清空，旧定时器与在途请求都不会再写回
 */
import { useCallback, useEffect, useReducer, useRef, useState } from "preact/hooks";

export type QueryKey = readonly unknown[];

interface Entry<T = unknown> {
  key: QueryKey;
  data?: T;
  error?: unknown;
  hasData: boolean;
  fetching: boolean;
  updatedAt: number;
  promise?: Promise<void>;
  /** 每发起一次请求 +1；只有最新一次请求的结果会写回 */
  seq: number;
  fn?: () => Promise<T>;
  retry: number;
  subs: Set<() => void>;
  gcTimer?: ReturnType<typeof setTimeout>;
  invalid: boolean;
  /** 已被 clearCache 移出缓存 */
  dead?: boolean;
}

const cache = new Map<string, Entry>();
const GC_MS = 5 * 60_000;
const hash = (k: QueryKey) => JSON.stringify(k);

function entryOf<T>(key: QueryKey): Entry<T> {
  const h = hash(key);
  let e = cache.get(h) as Entry<T> | undefined;
  if (!e) {
    e = { key, hasData: false, fetching: false, updatedAt: 0, seq: 0, retry: 3, subs: new Set(), invalid: false };
    cache.set(h, e as Entry);
  }
  return e;
}

const notify = (e: Entry) => [...e.subs].forEach((f) => f());

/**
 * 发起请求。force=false 时复用在途请求（挂载、定时重取）；
 * force=true 时作废在途请求重新发（invalidate / refetch），旧请求结果丢弃。
 */
function run<T>(e: Entry<T>, force = false): Promise<void> {
  if (e.promise && !force) return e.promise;
  const fn = e.fn;
  if (!fn || e.dead) return Promise.resolve();
  const my = ++e.seq;
  const current = () => my === e.seq && !e.dead;
  e.fetching = true;
  notify(e as Entry);
  const attempt = async (n: number): Promise<T> => {
    try {
      return await fn();
    } catch (err) {
      if (n >= e.retry || !current()) throw err;
      await new Promise((r) => setTimeout(r, Math.min(1000 * 2 ** n, 30_000)));
      if (!current()) throw err;
      return attempt(n + 1);
    }
  };
  const p: Promise<void> = attempt(0).then(
    (d) => {
      if (!current()) return;
      e.data = d;
      e.hasData = true;
      e.error = undefined;
      e.updatedAt = Date.now();
      e.invalid = false;
    },
    (err) => {
      if (!current()) return;
      e.error = err;
      e.updatedAt = Date.now();
      e.invalid = false;
    },
  );
  const done = p.finally(() => {
    if (!current()) return;
    e.promise = undefined;
    e.fetching = false;
    notify(e as Entry);
  });
  e.promise = done;
  return done;
}

const startsWith = (key: QueryKey, prefix: QueryKey) => prefix.every((p, i) => hash([p]) === hash([key[i]]));

/** 前缀匹配的查询标记过期；有组件在用的立即重取（在途的旧请求作废）。返回的 Promise 在新数据写回后完成 */
export function invalidate(prefix: QueryKey = []): Promise<void> {
  const jobs: Promise<void>[] = [];
  for (const e of cache.values()) {
    if (!startsWith(e.key, prefix)) continue;
    e.invalid = true;
    if (e.subs.size) jobs.push(run(e, true));
  }
  return Promise.all(jobs).then(() => undefined);
}

export function setQueryData<T>(key: QueryKey, updater: T | ((prev: T | undefined) => T | undefined)) {
  const e = entryOf<T>(key);
  const next = typeof updater === "function" ? (updater as (p: T | undefined) => T | undefined)(e.data) : updater;
  e.data = next;
  e.hasData = next !== undefined;
  e.updatedAt = Date.now();
  notify(e as Entry);
}

export function getQueryData<T>(key: QueryKey): T | undefined {
  return cache.get(hash(key))?.data as T | undefined;
}

/** 删掉前缀匹配的缓存（仍在使用的组件会用新条目重新请求） */
export function removeQueries(prefix: QueryKey = []) {
  const hit: Entry[] = [];
  for (const [h, e] of cache)
    if (startsWith(e.key, prefix)) {
      cache.delete(h);
      clearTimeout(e.gcTimer);
      e.dead = true;
      hit.push(e);
    }
  // 通知仍订阅旧条目的组件重渲染：它们会拿到新条目（依赖项里的条目对象变了），重新订阅并请求
  hit.forEach(notify);
}

/** 清空全部缓存（退出登录、登录态失效时用，避免下一个账号看到上一个账号的数据） */
export function clearCache() {
  removeQueries([]);
}

/** 与旧代码 useQueryClient() 对应的对象 */
export const queryClient = {
  invalidateQueries: ({ queryKey = [] }: { queryKey?: QueryKey } = {}) => invalidate(queryKey),
  setQueryData,
  getQueryData,
  removeQueries: ({ queryKey = [] }: { queryKey?: QueryKey } = {}) => removeQueries(queryKey),
};
export const useQueryClient = () => queryClient;

export interface UseApiOptions<T> {
  enabled?: boolean;
  /** 毫秒；默认 0（每次挂载都后台重取） */
  staleTime?: number;
  /** 换 key 时先显示上一个 key 的数据；默认 true（旧版 refine 的全局默认 placeholderData=keepPreviousData） */
  keepPrevious?: boolean;
  /** 重试次数，默认 3；false 不重试 */
  retry?: number | boolean;
  /** 窗口重新获得焦点时重取过期数据；默认 false（旧版 refine 的全局默认） */
  refetchOnWindowFocus?: boolean;
  refetchInterval?: number;
  initialData?: T;
}

export interface ApiState<T> {
  data: T | undefined;
  error: unknown;
  isLoading: boolean;
  isPending: boolean;
  isFetching: boolean;
  isError: boolean;
  isSuccess: boolean;
  /** 显示的是上一个 key 的数据 */
  isPlaceholderData: boolean;
  refetch: () => Promise<void>;
}

export function useApi<T>(key: QueryKey, fn: () => Promise<T>, opts: UseApiOptions<T> = {}): ApiState<T> {
  const { enabled = true, staleTime = 0, keepPrevious = true, retry = 3, refetchOnWindowFocus = false, refetchInterval, initialData } = opts;
  const h = hash(key);
  const [, force] = useReducer((n: number) => n + 1, 0);
  const e = entryOf<T>(key);
  e.fn = fn;
  e.retry = retry === true ? 3 : retry === false ? 0 : retry;
  if (!e.hasData && initialData !== undefined) {
    e.data = initialData;
    e.hasData = true;
  }
  // 上一次拿到的数据及其来源条目；来源条目被 clearCache/removeQueries 移除后不再拿来当占位（避免带出上一个账号的数据），
  // 与本 hook 无关的 removeQueries 不影响
  const prev = useRef<{ v: T; from: Entry<T> } | undefined>(undefined);

  useEffect(() => {
    const sub = () => force(0);
    e.subs.add(sub);
    clearTimeout(e.gcTimer);
    const stale = e.invalid || !e.hasData || Date.now() - e.updatedAt > staleTime;
    if (enabled && stale && !(e.error && !e.invalid && Date.now() - e.updatedAt < 1000)) void run(e);
    return () => {
      e.subs.delete(sub);
      // 只删自己：clearCache 之后同 key 可能已经是新条目
      if (!e.subs.size) e.gcTimer = setTimeout(() => !e.subs.size && cache.get(h) === e && cache.delete(h), GC_MS);
    };
  }, [h, enabled, e]);

  useEffect(() => {
    if (!enabled || !refetchOnWindowFocus) return;
    const onFocus = () => {
      if (document.visibilityState === "visible" && Date.now() - e.updatedAt > staleTime) void run(e);
    };
    window.addEventListener("visibilitychange", onFocus);
    window.addEventListener("focus", onFocus);
    return () => {
      window.removeEventListener("visibilitychange", onFocus);
      window.removeEventListener("focus", onFocus);
    };
  }, [h, enabled, refetchOnWindowFocus, staleTime, e]);

  useEffect(() => {
    if (!enabled || !refetchInterval) return;
    const t = setInterval(() => void run(e), refetchInterval);
    return () => clearInterval(t);
  }, [h, enabled, refetchInterval, e]);

  let data = e.hasData ? e.data : undefined;
  if (prev.current?.from.dead) prev.current = undefined;
  const placeholder = !e.hasData && keepPrevious && prev.current !== undefined;
  if (placeholder) data = prev.current!.v;
  if (e.hasData) prev.current = { v: e.data as T, from: e };
  const pending = !e.hasData && !placeholder;
  return {
    data,
    error: e.error,
    isPending: pending,
    isLoading: pending && enabled && (e.fetching || !e.error),
    isFetching: e.fetching,
    isError: !!e.error && !e.fetching,
    isSuccess: e.hasData || placeholder,
    isPlaceholderData: placeholder,
    refetch: () => run(e, true),
  };
}

/** 对象写法（与 useQuery({ queryKey, queryFn, ... }) 对应，方便迁移旧页面） */
export function useQuery<T>(o: { queryKey: QueryKey; queryFn: () => Promise<T>; placeholderData?: unknown } & UseApiOptions<T>): ApiState<T> {
  return useApi(o.queryKey, o.queryFn, o);
}
/** 与 TanStack 的 keepPreviousData 同名；默认已保留上一次数据，传不传都一样 */
export const keepPreviousData = <T>(prev: T) => prev;

export interface MutationOptions<V, R, C = unknown> {
  mutationFn: (vars: V) => Promise<R>;
  onMutate?: (vars: V) => C | Promise<C>;
  onSuccess?: (data: R, vars: V, ctx: C | undefined) => unknown;
  onError?: (err: unknown, vars: V, ctx: C | undefined) => unknown;
  onSettled?: (data: R | undefined, err: unknown, vars: V, ctx: C | undefined) => unknown;
}
interface CallOpts<V, R> {
  onSuccess?: (data: R, vars: V) => void;
  onError?: (err: unknown, vars: V) => void;
  onSettled?: (data: R | undefined, err: unknown, vars: V) => void;
}

export function useMutation<V = void, R = unknown, C = unknown>(o: MutationOptions<V, R, C>) {
  const [state, setState] = useState<{ isPending: boolean; data?: R; error?: unknown; variables?: V; status: "idle" | "pending" | "success" | "error" }>({ isPending: false, status: "idle" });
  const opts = useRef(o);
  opts.current = o;
  const alive = useRef(true);
  useEffect(() => () => void (alive.current = false), []);
  const mutateAsync = useCallback(async (vars: V, call?: CallOpts<V, R>): Promise<R> => {
    const m = opts.current;
    setState({ isPending: true, status: "pending", variables: vars });
    let ctx: C | undefined;
    try {
      ctx = await m.onMutate?.(vars);
      const data = await m.mutationFn(vars);
      if (alive.current) setState({ isPending: false, status: "success", data, variables: vars });
      await m.onSuccess?.(data, vars, ctx);
      call?.onSuccess?.(data, vars);
      await m.onSettled?.(data, undefined, vars, ctx);
      call?.onSettled?.(data, undefined, vars);
      return data;
    } catch (err) {
      if (alive.current) setState({ isPending: false, status: "error", error: err, variables: vars });
      await m.onError?.(err, vars, ctx);
      call?.onError?.(err, vars);
      await m.onSettled?.(undefined, err, vars, ctx);
      call?.onSettled?.(undefined, err, vars);
      throw err;
    }
  }, []);
  const mutate = useCallback((vars: V, call?: CallOpts<V, R>) => void mutateAsync(vars, call).catch(() => undefined), [mutateAsync]);
  const reset = useCallback(() => setState({ isPending: false, status: "idle" }), []);
  return { ...state, isSuccess: state.status === "success", isError: state.status === "error", isIdle: state.status === "idle", mutate, mutateAsync, reset };
}
