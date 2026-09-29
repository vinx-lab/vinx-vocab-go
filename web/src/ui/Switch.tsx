import type { ComponentChildren, JSX } from "preact";
import { LoadingOutlined } from "./icons";
import { cx } from "./util";

export interface SwitchProps {
  checked?: boolean;
  onChange?: (checked: boolean, e: MouseEvent) => void;
  size?: "small" | "default";
  disabled?: boolean;
  loading?: boolean;
  checkedChildren?: ComponentChildren;
  unCheckedChildren?: ComponentChildren;
  id?: string;
  className?: string;
  style?: JSX.CSSProperties;
  "aria-label"?: string;
}

export function Switch({ checked, onChange, size, disabled, loading, checkedChildren, unCheckedChildren, id, className, style, ...rest }: SwitchProps) {
  const dis = disabled || loading;
  return (
    <button
      id={id}
      type="button"
      role="switch"
      aria-checked={!!checked}
      aria-label={rest["aria-label"]}
      disabled={dis}
      class={cx("ant-switch", size === "small" && "ant-switch-small", checked && "ant-switch-checked", loading && "ant-switch-loading", dis && "ant-switch-disabled", className)}
      style={style}
      onClick={(e) => !dis && onChange?.(!checked, e)}
    >
      <div class="ant-switch-handle">{loading && <LoadingOutlined className="ant-switch-loading-icon" />}</div>
      <span class="ant-switch-inner">
        <span class="ant-switch-inner-checked">{checkedChildren}</span>
        <span class="ant-switch-inner-unchecked">{unCheckedChildren}</span>
      </span>
    </button>
  );
}
