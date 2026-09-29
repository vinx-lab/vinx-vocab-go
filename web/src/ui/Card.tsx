import type { ComponentChildren, JSX } from "preact";
import { Spin } from "./Display";
import { cx } from "./util";

export interface CardProps {
  title?: ComponentChildren;
  extra?: ComponentChildren;
  size?: "default" | "small";
  bordered?: boolean;
  loading?: boolean;
  hoverable?: boolean;
  className?: string;
  style?: JSX.CSSProperties;
  styles?: { header?: JSX.CSSProperties; body?: JSX.CSSProperties };
  onClick?: (e: MouseEvent) => void;
  children?: ComponentChildren;
}

export function Card({ title, extra, size, bordered = true, loading, hoverable, className, style, styles, onClick, children }: CardProps) {
  return (
    <div class={cx("ant-card", bordered && "ant-card-bordered", hoverable && "ant-card-hoverable", size === "small" && "ant-card-small", className)} style={style} onClick={onClick}>
      {(title || extra) && (
        <div class="ant-card-head" style={styles?.header}>
          <div class="ant-card-head-wrapper">
            {title && <div class="ant-card-head-title">{title}</div>}
            {extra && <div class="ant-card-extra">{extra}</div>}
          </div>
        </div>
      )}
      <div class="ant-card-body" style={styles?.body}>
        {loading ? <Spin /> : children}
      </div>
    </div>
  );
}
