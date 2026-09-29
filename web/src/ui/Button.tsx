import type { ComponentChildren, JSX } from "preact";
import { toChildArray } from "preact";
import { LoadingOutlined } from "./icons";
import { cx, insertSpace, sizeCls, type Size } from "./util";

export interface ButtonProps {
  type?: "primary" | "default" | "dashed" | "text" | "link";
  size?: Size;
  htmlType?: "button" | "submit" | "reset";
  icon?: ComponentChildren;
  loading?: boolean;
  disabled?: boolean;
  danger?: boolean;
  ghost?: boolean;
  block?: boolean;
  shape?: "default" | "circle" | "round";
  href?: string;
  className?: string;
  style?: JSX.CSSProperties;
  onClick?: (e: MouseEvent) => void;
  children?: ComponentChildren;
  title?: string;
  form?: string;
  "aria-label"?: string;
  "data-testid"?: string;
}

/** antd 5 Button：同样的 class 组合，样式来自 antd.css */
export function Button({ type = "default", size, htmlType = "button", icon, loading, disabled, danger, ghost, block, shape, href, className, style, onClick, children, ...rest }: ButtonProps) {
  const kids = toChildArray(children);
  const iconOnly = kids.length === 0 && (icon || loading);
  const color = danger ? "dangerous" : type === "primary" ? "primary" : type === "link" ? "link" : "default";
  const variant = type === "primary" ? "solid" : type === "default" ? "outlined" : type;
  const cls = cx(
    "ant-btn",
    shape && shape !== "default" && `ant-btn-${shape}`,
    `ant-btn-${type}`,
    danger && "ant-btn-dangerous",
    `ant-btn-color-${color}`,
    `ant-btn-variant-${variant}`,
    sizeCls("ant-btn", size),
    iconOnly && "ant-btn-icon-only",
    ghost && "ant-btn-background-ghost",
    loading && "ant-btn-loading",
    block && "ant-btn-block",
    className,
  );
  const iconNode = loading ? (
    <span class="ant-btn-icon ant-btn-loading-icon">
      <LoadingOutlined />
    </span>
  ) : icon ? (
    <span class="ant-btn-icon">{icon}</span>
  ) : null;
  // antd：只有一个文本子节点、无图标、非 text/link 时，两个汉字中间加空格
  const spaced = kids.length === 1 && typeof kids[0] === "string" && !icon && !loading && variant !== "text" && variant !== "link";
  // 与 antd 一致：相邻的文本子节点合并进同一个 <span>（拆成多个 span 会让无障碍名称在相邻文本间插入空格，
  // 例如「合并打印所选」+「（2）」会被读成「合并打印所选 （2）」）；元素子节点原样放进按钮，不再包一层 span
  // （包一层会破坏页面给按钮设的 flex 布局，例如今日页「学新词」大按钮）。
  const body: ComponentChildren[] = [];
  let text: (string | number)[] = [];
  const flush = () => {
    if (text.length) body.push(<span>{spaced ? insertSpace(String(text[0])) : text.join("")}</span>);
    text = [];
  };
  for (const k of kids) {
    if (typeof k === "string" || typeof k === "number") text.push(k);
    else {
      flush();
      body.push(k);
    }
  }
  flush();
  const click = (e: MouseEvent) => {
    if (loading || disabled) {
      e.preventDefault();
      return;
    }
    onClick?.(e);
  };
  if (href) {
    return (
      <a class={cx(cls, disabled && "ant-btn-disabled")} href={disabled ? undefined : href} style={style} onClick={click} {...rest}>
        {iconNode}
        {body}
      </a>
    );
  }
  return (
    <button type={htmlType} class={cls} style={style} disabled={disabled} onClick={click} {...rest}>
      {iconNode}
      {body}
    </button>
  );
}
