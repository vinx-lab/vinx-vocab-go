/**
 * 与 react-router 常用 API 对应的小封装（基于 preact-iso），方便从旧页面迁移：
 * - <Link to>：渲染普通 <a href>，点击由 preact-iso 的 LocationProvider 拦截做站内跳转
 * - useNavigate()：navigate(path, { replace }) / navigate(-1)
 * - useParams()：当前路由参数
 * - useSearchParams()：[URLSearchParams, setSearchParams]
 */
import type { ComponentChildren, JSX } from "preact";
import { useLocation, useRoute } from "preact-iso";

type AnchorProps = Omit<JSX.AnchorHTMLAttributes<HTMLAnchorElement>, "href"> & { to: string; children?: ComponentChildren };

export function Link({ to, children, ...rest }: AnchorProps) {
  return (
    <a href={to} {...(rest as JSX.HTMLAttributes<HTMLAnchorElement>)}>
      {children}
    </a>
  );
}

export type NavigateFn = (to: string | number, opts?: { replace?: boolean }) => void;

export function useNavigate(): NavigateFn {
  const { route } = useLocation();
  return (to, opts) => {
    if (typeof to === "number") history.go(to);
    else route(to, opts?.replace);
  };
}

export function useParams<T extends Record<string, string> = Record<string, string>>(): Partial<T> {
  return (useRoute().params ?? {}) as Partial<T>;
}

export function useSearchParams(): [URLSearchParams, (next: URLSearchParams | Record<string, string>, opts?: { replace?: boolean }) => void] {
  const { path, query, route } = useLocation();
  const params = new URLSearchParams(query);
  const set = (next: URLSearchParams | Record<string, string>, opts?: { replace?: boolean }) => {
    const qs = new URLSearchParams(next as Record<string, string>).toString();
    route(qs ? `${path}?${qs}` : path, opts?.replace);
  };
  return [params, set];
}
