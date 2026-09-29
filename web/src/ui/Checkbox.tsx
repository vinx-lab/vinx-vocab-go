import type { ComponentChildren, JSX } from "preact";
import { cx } from "./util";

export type CheckboxChange = (e: { target: { checked: boolean; value?: unknown } }) => void;
export interface CheckboxProps {
  checked?: boolean;
  indeterminate?: boolean;
  disabled?: boolean;
  value?: unknown;
  onChange?: CheckboxChange;
  children?: ComponentChildren;
  className?: string;
  style?: JSX.CSSProperties;
  id?: string;
  "aria-label"?: string;
}

export function Checkbox({ checked, indeterminate, disabled, value, onChange, children, className, style, id, ...rest }: CheckboxProps) {
  return (
    <label class={cx("ant-checkbox-wrapper", checked && "ant-checkbox-wrapper-checked", disabled && "ant-checkbox-wrapper-disabled", className)} style={style}>
      <span class={cx("ant-checkbox", indeterminate && "ant-checkbox-indeterminate", "ant-wave-target", checked && "ant-checkbox-checked", disabled && "ant-checkbox-disabled")}>
        <input
          id={id}
          aria-label={rest["aria-label"]}
          class="ant-checkbox-input"
          type="checkbox"
          checked={!!checked}
          disabled={disabled}
          aria-checked={indeterminate ? "mixed" : undefined}
          onChange={(e) => onChange?.({ target: { checked: (e.currentTarget as HTMLInputElement).checked, value } })}
          onClick={(e) => e.stopPropagation()}
        />
        <span class="ant-checkbox-inner" />
      </span>
      {children !== undefined && children !== null && <span class="ant-checkbox-label">{children}</span>}
    </label>
  );
}

type Opt = string | { label: ComponentChildren; value: any; disabled?: boolean };
function Group({ options = [], value = [], onChange, disabled, style, className }: { options?: Opt[]; value?: any[]; onChange?: (v: any[]) => void; disabled?: boolean; style?: JSX.CSSProperties; className?: string }) {
  const opts = options.map((o) => (typeof o === "string" ? { label: o, value: o } : o));
  return (
    <div class={cx("ant-checkbox-group", className)} style={style}>
      {opts.map((o) => (
        <Checkbox
          className="ant-checkbox-group-item"
          checked={value.includes(o.value)}
          disabled={disabled || o.disabled}
          value={o.value}
          onChange={(e) => {
            const next = e.target.checked ? [...value, o.value] : value.filter((v) => v !== o.value);
            // 保持选项顺序
            onChange?.(opts.map((x) => x.value).filter((v) => next.includes(v)));
          }}
        >
          {o.label}
        </Checkbox>
      ))}
    </div>
  );
}
Checkbox.Group = Group;
