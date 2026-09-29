import type { JSX } from "preact";
import { cx } from "./util";

/** [aria-label, viewBox 是否为 0 0 1024 1024, ...path d]（数据来自 @ant-design/icons-svg，MIT） */
export type IconDef = readonly [string, 0 | 1, ...string[]];

export interface IconProps {
  className?: string;
  style?: JSX.CSSProperties;
  spin?: boolean;
  onClick?: (e: MouseEvent) => void;
  "aria-label"?: string;
  "aria-hidden"?: boolean;
}

/** 与 @ant-design/icons 相同的 DOM：<span role="img" class="anticon anticon-xxx"><svg …/></span> */
export function Icon({ def, className, style, spin, onClick, ...rest }: IconProps & { def: IconDef }) {
  const [name, full, ...paths] = def;
  return (
    <span role="img" aria-label={rest["aria-label"] ?? name} class={cx("anticon", `anticon-${name}`, (spin || name === "loading") && "anticon-spin", className)} style={style} onClick={onClick} aria-hidden={rest["aria-hidden"]}>
      <svg viewBox={full ? "0 0 1024 1024" : "64 64 896 896"} focusable="false" data-icon={name} width="1em" height="1em" fill="currentColor" aria-hidden="true">
        {paths.map((d) => (
          <path d={d} />
        ))}
      </svg>
    </span>
  );
}

export const mk = (def: IconDef) => (p: IconProps) => <Icon def={def} {...p} />;
