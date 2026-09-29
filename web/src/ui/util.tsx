import { Component, h, render, type ComponentChildren, type JSX } from "preact";
import { useRef } from "preact/hooks";

/** 拼 className，忽略假值 */
export function cx(...parts: unknown[]): string {
  return parts.filter((p) => typeof p === "string" && p).join(" ");
}

let seq = 0;
/** 稳定的组件实例 id（label/aria 关联用） */
export function useUid(prefix = "vx"): string {
  const ref = useRef<string>();
  if (!ref.current) ref.current = `${prefix}-${++seq}`;
  return ref.current;
}

/** antd 的「两个汉字的按钮中间插空格」：「登录」→「登 录」 */
export function insertSpace(child: ComponentChildren): ComponentChildren {
  if (typeof child === "string" && /^[一-龥]{2}$/.test(child)) return `${child[0]} ${child[1]}`;
  return child;
}

export type Style = JSX.CSSProperties | string | undefined;
export type Size = "small" | "middle" | "large";
export const sizeCls = (prefix: string, size?: Size) => (size === "small" ? `${prefix}-sm` : size === "large" ? `${prefix}-lg` : "");

/** 把 children 渲染到 body 下的独立容器，保留上下文（弹层用） */
function ContextBridge(this: { getChildContext?: () => unknown }, props: { context: unknown; children: ComponentChildren }) {
  this.getChildContext = () => props.context;
  return props.children as JSX.Element;
}
export class Portal extends Component<{ children: ComponentChildren }> {
  el?: HTMLDivElement;
  componentDidMount() {
    this.el = document.createElement("div");
    document.body.appendChild(this.el);
    this.paint();
  }
  componentDidUpdate() {
    this.paint();
  }
  componentWillUnmount() {
    if (this.el) {
      render(null, this.el);
      this.el.remove();
    }
  }
  paint() {
    if (this.el) render(h(ContextBridge as never, { context: this.context }, this.props.children), this.el);
  }
  render() {
    return null;
  }
}

/** z-index 递增，后打开的弹层在上面 */
let z = 1050;
export const nextZ = () => ++z;

/** 从 onChange 的参数里取值：事件取 target.value / checked，其他原样 */
export function valueOf(arg: unknown, prop = "value"): unknown {
  if (arg && typeof arg === "object" && "target" in (arg as object)) {
    const t = (arg as { target: Record<string, unknown> }).target;
    return prop === "checked" ? t.checked : t.value;
  }
  return arg;
}

/** 把 style 对象里的数字补 px（preact 已处理），这里只做合并 */
export function mergeStyle(a?: Style, b?: Style): Style {
  if (!a) return b;
  if (!b) return a;
  if (typeof a === "string" || typeof b === "string") return `${styleStr(a)};${styleStr(b)}`;
  return { ...a, ...b };
}
function styleStr(s: Style): string {
  if (!s) return "";
  if (typeof s === "string") return s;
  return Object.entries(s)
    .map(([k, v]) => `${k.replace(/[A-Z]/g, (c) => "-" + c.toLowerCase())}:${typeof v === "number" ? v + "px" : v}`)
    .join(";");
}
