/** 展示类小组件（只在懒加载页面里用到，单独成模块以便跟着页面分块）：Alert、Space、Divider、Typography、Timeline、Progress */
import { toChildArray, type ComponentChildren, type JSX } from "preact";
import { useState } from "preact/hooks";
import { CheckCircleFilled, CloseCircleFilled, CloseOutlined, ExclamationCircleFilled, InfoCircleFilled } from "./icons";
import { cx } from "./util";

type Style = JSX.CSSProperties;

const ALERT_ICON = { success: CheckCircleFilled, info: InfoCircleFilled, warning: ExclamationCircleFilled, error: CloseCircleFilled };
export function Alert({ type = "info", showIcon, message, description, action, closable, onClose, style, className, banner, icon }: { type?: "success" | "info" | "warning" | "error"; showIcon?: boolean; message?: ComponentChildren; description?: ComponentChildren; action?: ComponentChildren; closable?: boolean; onClose?: () => void; style?: Style; className?: string; banner?: boolean; icon?: ComponentChildren }) {
  const [closed, setClosed] = useState(false);
  if (closed) return null;
  const I = ALERT_ICON[type];
  const withIcon = showIcon ?? banner;
  return (
    <div data-show="true" class={cx("ant-alert", `ant-alert-${type}`, description && "ant-alert-with-description", !withIcon && "ant-alert-no-icon", banner && "ant-alert-banner", className)} role="alert" style={style}>
      {withIcon && (icon ? <span class="ant-alert-icon">{icon}</span> : <I className="ant-alert-icon" aria-label={({ success: "check-circle", info: "info-circle", warning: "exclamation-circle", error: "close-circle" } as const)[type]} />)}
      <div class="ant-alert-content">
        {message !== undefined && <div class="ant-alert-message">{message}</div>}
        {description && <div class="ant-alert-description">{description}</div>}
      </div>
      {action && <div class="ant-alert-action">{action}</div>}
      {closable && (
        <button
          type="button"
          class="ant-alert-close-icon"
          tabIndex={0}
          onClick={() => {
            setClosed(true);
            onClose?.();
          }}
        >
          <CloseOutlined />
        </button>
      )}
    </div>
  );
}

const GAP = { small: 8, middle: 16, large: 24 } as const;
export function Space({ direction = "horizontal", size = "small", wrap, align, style, className, children }: { direction?: "horizontal" | "vertical"; size?: "small" | "middle" | "large" | number | [number, number]; wrap?: boolean; align?: "start" | "end" | "center" | "baseline"; style?: Style; className?: string; children?: ComponentChildren }) {
  const items = toChildArray(children).filter((c) => c !== null && c !== undefined && (c as unknown) !== false && c !== "");
  const named = typeof size === "string";
  const gapStyle: Style = named ? {} : Array.isArray(size) ? { columnGap: size[0], rowGap: size[1] } : { gap: size };
  const al = align ?? (direction === "horizontal" ? "center" : undefined);
  return (
    <div
      class={cx("ant-space", `ant-space-${direction}`, al && `ant-space-align-${al}`, named && `ant-space-gap-row-${size}`, named && `ant-space-gap-col-${size}`, className)}
      style={{ ...(wrap ? { flexWrap: "wrap" } : {}), ...gapStyle, ...style }}
    >
      {items.map((c) => (
        <div class="ant-space-item">{c}</div>
      ))}
    </div>
  );
}
export const spaceGap = GAP;

export function Divider({ type = "horizontal", dashed, orientation, plain, style, className, children }: { type?: "horizontal" | "vertical"; dashed?: boolean; orientation?: "left" | "right" | "center"; plain?: boolean; style?: Style; className?: string; children?: ComponentChildren }) {
  const withText = children !== undefined && type === "horizontal";
  const pos = orientation === "left" ? "start" : orientation === "right" ? "end" : "center";
  return (
    <div class={cx("ant-divider", `ant-divider-${type}`, withText && "ant-divider-with-text", withText && `ant-divider-with-text-${pos}`, dashed && "ant-divider-dashed", plain && "ant-divider-plain", className)} role="separator" style={style}>
      {withText && <span class="ant-divider-inner-text">{children}</span>}
    </div>
  );
}

function Text({ type, strong, code, style, className, children }: { type?: "secondary" | "success" | "warning" | "danger"; strong?: boolean; code?: boolean; style?: Style; className?: string; children?: ComponentChildren }) {
  let body: ComponentChildren = children;
  if (code) body = <code>{body}</code>;
  if (strong) body = <strong>{body}</strong>;
  return (
    <span class={cx("ant-typography", type && `ant-typography-${type}`, className)} style={style}>
      {body}
    </span>
  );
}
function Paragraph({ type, style, className, children }: { type?: "secondary" | "success" | "warning" | "danger"; style?: Style; className?: string; children?: ComponentChildren }) {
  return (
    <div class={cx("ant-typography", type && `ant-typography-${type}`, className)} style={style}>
      {children}
    </div>
  );
}
function Title({ level = 1, style, className, children }: { level?: 1 | 2 | 3 | 4 | 5; style?: Style; className?: string; children?: ComponentChildren }) {
  const H = `h${level}` as "h1";
  return (
    <H class={cx("ant-typography", className)} style={style}>
      {children}
    </H>
  );
}
export const Typography = { Text, Paragraph, Title };

export function Timeline({ items = [] }: { items?: { key?: string | number; children?: ComponentChildren; color?: string; dot?: ComponentChildren; label?: ComponentChildren }[] }) {
  return (
    <ol class="ant-timeline">
      {items.map((it, i) => {
        const c = it.color ?? "blue";
        const preset = /^(blue|red|green|gray)$/.test(c);
        return (
          <li key={it.key ?? i} class={cx("ant-timeline-item", i === items.length - 1 && "ant-timeline-item-last")}>
            <div class="ant-timeline-item-tail" />
            <div class={cx("ant-timeline-item-head", it.dot && "ant-timeline-item-head-custom", preset && `ant-timeline-item-head-${c}`)} style={preset ? undefined : { borderColor: c, color: c }}>
              {it.dot}
            </div>
            <div class="ant-timeline-item-content">{it.children}</div>
          </li>
        );
      })}
    </ol>
  );
}

export function Progress({ percent = 0, size = "default", showInfo = true, format, status, strokeColor, trailColor, style, className }: { percent?: number; size?: "small" | "default"; showInfo?: boolean; format?: (p: number) => ComponentChildren; status?: "normal" | "success" | "exception" | "active"; strokeColor?: string; trailColor?: string; style?: Style; className?: string }) {
  const p = Math.max(0, Math.min(100, percent));
  const st = status ?? (p >= 100 ? "success" : "normal");
  const text = format ? format(p) : st === "exception" ? <CloseCircleFilled aria-label="close-circle" /> : st === "success" ? <CheckCircleFilled aria-label="check-circle" /> : `${Math.round(p)}%`;
  const h = size === "small" ? 6 : 8;
  return (
    <div class={cx("ant-progress", `ant-progress-status-${st}`, "ant-progress-line", "ant-progress-line-align-end", "ant-progress-line-position-outer", showInfo && "ant-progress-show-info", `ant-progress-${size}`, className)} role="progressbar" aria-valuenow={p} aria-valuemin={0} aria-valuemax={100} style={style}>
      <div class="ant-progress-outer" style={{ width: "100%" }}>
        <div class="ant-progress-inner" style={trailColor ? { backgroundColor: trailColor } : undefined}>
          <div class="ant-progress-bg ant-progress-bg-outer" style={{ width: `${p}%`, height: h, "--progress-percent": p / 100, ...(strokeColor ? { background: strokeColor } : {}) } as Style} />
        </div>
        {showInfo && (
          <span class="ant-progress-text ant-progress-text-end ant-progress-text-outer" title={typeof text === "string" ? text : undefined}>
            {text}
          </span>
        )}
      </div>
    </div>
  );
}

