/** 展示类小组件：Avatar、Tag、Spin、Empty、Result（首屏会用到的）；Alert、Space、Divider、Typography、Timeline、Progress 在 Feedback.tsx */
import { toChildArray, type ComponentChildren, type JSX } from "preact";
import { useState } from "preact/hooks";
import { CheckCircleFilled, CloseCircleFilled, CloseOutlined, ExclamationCircleFilled, WarningFilled } from "./icons";
import { cx } from "./util";

type Style = JSX.CSSProperties;

export function Avatar({ size = "default", icon, style, className, children, shape = "circle" }: { size?: number | "small" | "default" | "large"; icon?: ComponentChildren; style?: Style; className?: string; children?: ComponentChildren; shape?: "circle" | "square" }) {
  const s: Style = typeof size === "number" ? { width: size, height: size, fontSize: icon ? size / 2 : 18 } : {};
  const kids = toChildArray(children);
  const isText = kids.every((k) => typeof k === "string" || typeof k === "number");
  return (
    <span class={cx("ant-avatar", size === "small" && "ant-avatar-sm", size === "large" && "ant-avatar-lg", `ant-avatar-${shape}`, icon && "ant-avatar-icon", className)} style={{ ...s, ...style }}>
      {icon ?? (isText ? <span class="ant-avatar-string" style={{ transform: "scale(1)" }}>{children}</span> : children)}
    </span>
  );
}

const PRESET = /^(magenta|red|volcano|orange|gold|lime|green|cyan|blue|geekblue|purple|success|processing|error|warning|default)$/;
export function Tag({ color, bordered = true, closable, onClose, closeIcon, icon, style, className, children, ...rest }: { color?: string; bordered?: boolean; closable?: boolean; onClose?: (e: MouseEvent) => void; closeIcon?: ComponentChildren; icon?: ComponentChildren; style?: Style; className?: string; children?: ComponentChildren; "data-testid"?: string; onClick?: (e: MouseEvent) => void }) {
  const [open, setOpen] = useState(true);
  if (!open) return null;
  const preset = color && PRESET.test(color) && color !== "default";
  const custom = color && !PRESET.test(color);
  return (
    <span class={cx("ant-tag", preset && `ant-tag-${color}`, custom && "ant-tag-has-color", !bordered && "ant-tag-borderless", className)} style={{ ...(custom ? { backgroundColor: color } : {}), ...style }} {...rest}>
      {icon}
      {icon ? <span>{children}</span> : children}
      {closable && (
        <span
          class="ant-tag-close-icon"
          onClick={(e) => {
            e.stopPropagation();
            onClose?.(e);
            if (!e.defaultPrevented) setOpen(false);
          }}
        >
          {closeIcon ?? <CloseOutlined />}
        </span>
      )}
    </span>
  );
}

export function Spin({ size, tip, spinning = true, style, className, children, indicator }: { size?: "small" | "default" | "large"; tip?: ComponentChildren; spinning?: boolean; style?: Style; className?: string; children?: ComponentChildren; indicator?: ComponentChildren }) {
  const spin = spinning ? (
    <div class={cx("ant-spin", size === "small" && "ant-spin-sm", size === "large" && "ant-spin-lg", "ant-spin-spinning", tip && children !== undefined && "ant-spin-show-text", children === undefined && className)} aria-live="polite" aria-busy="true" style={children === undefined ? style : undefined}>
      {indicator ? (
        <span class="ant-spin-dot">{indicator}</span>
      ) : (
        <span class="ant-spin-dot-holder">
          <span class="ant-spin-dot ant-spin-dot-spin">
            <i class="ant-spin-dot-item" />
            <i class="ant-spin-dot-item" />
            <i class="ant-spin-dot-item" />
            <i class="ant-spin-dot-item" />
          </span>
        </span>
      )}
      {tip && children !== undefined && <div class="ant-spin-text">{tip}</div>}
    </div>
  ) : null;
  if (children === undefined) return spin;
  return (
    <div class={cx("ant-spin-nested-loading", className)} style={style}>
      {spin && <div>{spin}</div>}
      <div class={cx("ant-spin-container", spinning && "ant-spin-blur")}>{children}</div>
    </div>
  );
}

/** antd 的两种空状态插画；简版颜色随主题（值取自 antd 按 token 计算的结果） */
const SIMPLE = (
  <svg width="64" height="41" viewBox="0 0 64 41" xmlns="http://www.w3.org/2000/svg">
    <title>暂无数据</title>
    <g transform="translate(0 1)" fill="none" fill-rule="evenodd">
      <ellipse class="vx-empty-shadow" cx="32" cy="33" rx="32" ry="7" />
      <g fill-rule="nonzero" class="vx-empty-stroke">
        <path d="M55 12.76L44.854 1.258C44.367.474 43.656 0 42.907 0H21.093c-.749 0-1.46.474-1.947 1.257L9 12.761V22h46v-9.24z" />
        <path class="vx-empty-fill" d="M41.613 15.931c0-1.605.994-2.93 2.227-2.931H55v18.137C55 33.26 53.68 35 52.05 35h-40.1C10.32 35 9 33.259 9 31.137V13h11.16c1.233 0 2.227 1.323 2.227 2.928v.022c0 1.605 1.005 2.901 2.237 2.901h14.752c1.232 0 2.237-1.308 2.237-2.913v-.007z" />
      </g>
    </g>
  </svg>
);
const DEFAULT_IMG = (
  <svg width="184" height="152" viewBox="0 0 184 152" xmlns="http://www.w3.org/2000/svg">
    <title>暂无数据</title>
    <g fill="none" fill-rule="evenodd">
      <g transform="translate(24 31.67)">
        <ellipse fill-opacity=".8" fill="#F5F5F7" cx="67.797" cy="106.89" rx="67.797" ry="12.668" />
        <path d="M122.034 69.674L98.109 40.229c-1.148-1.386-2.826-2.225-4.593-2.225h-51.44c-1.766 0-3.444.839-4.592 2.225L13.56 69.674v15.383h108.475V69.674z" fill="#AEB8C2" />
        <path d="M101.537 86.214L80.63 61.102c-1.001-1.207-2.507-1.867-4.048-1.867H31.724c-1.54 0-3.047.66-4.048 1.867L6.769 86.214v13.792h94.768V86.214z" fill="#DCE0E6" transform="translate(13.56)" />
        <path d="M33.83 0h67.933a4 4 0 0 1 4 4v93.344a4 4 0 0 1-4 4H33.83a4 4 0 0 1-4-4V4a4 4 0 0 1 4-4z" fill="#F5F5F7" />
        <path d="M42.678 9.953h50.237a2 2 0 0 1 2 2V36.91a2 2 0 0 1-2 2H42.678a2 2 0 0 1-2-2V11.953a2 2 0 0 1 2-2zM42.94 49.767h49.713a2.262 2.262 0 1 1 0 4.524H42.94a2.262 2.262 0 0 1 0-4.524zM42.94 61.53h49.713a2.262 2.262 0 1 1 0 4.525H42.94a2.262 2.262 0 0 1 0-4.525zM121.813 105.032c-.775 3.071-3.497 5.36-6.735 5.36H20.515c-3.238 0-5.96-2.29-6.734-5.36a7.309 7.309 0 0 1-.222-1.79V69.675h26.318c2.907 0 5.25 2.448 5.25 5.42v.04c0 2.971 2.37 5.37 5.277 5.37h34.785c2.907 0 5.277-2.421 5.277-5.393V75.1c0-2.972 2.343-5.426 5.25-5.426h26.318v33.569c0 .617-.077 1.216-.221 1.789z" fill="#DCE0E6" />
      </g>
      <path d="M149.121 33.292l-6.83 2.65a1 1 0 0 1-1.317-1.23l1.937-6.207c-2.589-2.944-4.109-6.534-4.109-10.408C138.802 8.102 148.92 0 161.402 0 173.881 0 184 8.102 184 18.097c0 9.995-10.118 18.097-22.599 18.097-4.528 0-8.744-1.066-12.28-2.902z" fill="#DCE0E6" />
      <g transform="translate(149.65 15.383)" fill="#FFF">
        <ellipse cx="20.654" cy="3.167" rx="2.849" ry="2.815" />
        <path d="M5.698 5.63H0L2.898.704zM9.259.704h4.985V5.63H9.259z" />
      </g>
    </g>
  </svg>
);
const PRESENTED_IMAGE_SIMPLE = "simple";
const PRESENTED_IMAGE_DEFAULT = "default";
export function Empty({ image = PRESENTED_IMAGE_DEFAULT, description, style, className, children, imageStyle }: { image?: ComponentChildren; description?: ComponentChildren | false; style?: Style; className?: string; children?: ComponentChildren; imageStyle?: Style }) {
  const simple = image === PRESENTED_IMAGE_SIMPLE;
  const img = simple ? SIMPLE : image === PRESENTED_IMAGE_DEFAULT ? DEFAULT_IMG : image;
  return (
    <div class={cx("ant-empty", simple && "ant-empty-normal", className)} style={style}>
      <div class="ant-empty-image" style={imageStyle}>
        {img}
      </div>
      {description !== false && <div class="ant-empty-description">{description ?? "暂无数据"}</div>}
      {children && <div class="ant-empty-footer">{children}</div>}
    </div>
  );
}
Empty.PRESENTED_IMAGE_SIMPLE = PRESENTED_IMAGE_SIMPLE;
Empty.PRESENTED_IMAGE_DEFAULT = PRESENTED_IMAGE_DEFAULT;

const RESULT_ICON = { success: CheckCircleFilled, error: CloseCircleFilled, info: ExclamationCircleFilled, warning: WarningFilled };
export function Result({ status = "info", title, subTitle, extra, icon, style, className, children }: { status?: "success" | "error" | "info" | "warning"; title?: ComponentChildren; subTitle?: ComponentChildren; extra?: ComponentChildren; icon?: ComponentChildren; style?: Style; className?: string; children?: ComponentChildren }) {
  const I = RESULT_ICON[status];
  return (
    <div class={cx("ant-result", `ant-result-${status}`, className)} style={style}>
      <div class="ant-result-icon">{icon ?? <I aria-label={({ success: "check-circle", error: "close-circle", info: "exclamation-circle", warning: "warning" } as const)[status]} />}</div>
      {title && <div class="ant-result-title">{title}</div>}
      {subTitle && <div class="ant-result-subtitle">{subTitle}</div>}
      {extra && <div class="ant-result-extra">{extra}</div>}
      {children && <div class="ant-result-content">{children}</div>}
    </div>
  );
}
