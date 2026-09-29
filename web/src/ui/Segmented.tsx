import type { ComponentChildren, JSX } from "preact";
import { useUid, cx } from "./util";

type Opt<T> = T | { label: ComponentChildren; value: T; disabled?: boolean; icon?: ComponentChildren; title?: string };
export interface SegmentedProps<T extends string | number> {
  options: Opt<T>[];
  value?: T;
  onChange?: (value: T) => void;
  block?: boolean;
  size?: "small" | "middle" | "large";
  disabled?: boolean;
  style?: JSX.CSSProperties;
  className?: string;
  "aria-label"?: string;
}

export function Segmented<T extends string | number>({ options, value, onChange, block, size, disabled, style, className, ...rest }: SegmentedProps<T>) {
  const name = useUid("seg");
  const opts = options.map((o) => (typeof o === "object" && o !== null ? o : { label: String(o), value: o as T }));
  return (
    <div role="radiogroup" aria-label={rest["aria-label"] ?? "segmented control"} tabIndex={0} aria-orientation="horizontal" class={cx("ant-segmented", block && "ant-segmented-block", size === "small" && "ant-segmented-sm", size === "large" && "ant-segmented-lg", disabled && "ant-segmented-disabled", className)} style={style}>
      <div class="ant-segmented-group">
        {opts.map((o) => {
          const selected = o.value === value;
          const dis = disabled || o.disabled;
          return (
            <label class={cx("ant-segmented-item", selected && "ant-segmented-item-selected", dis && "ant-segmented-item-disabled")}>
              <input name={name} class="ant-segmented-item-input" type="radio" checked={selected} disabled={dis} onChange={() => !dis && onChange?.(o.value)} />
              <div class="ant-segmented-item-label" title={o.title ?? (typeof o.label === "string" ? o.label : undefined)}>
                {o.icon ? (
                  <>
                    <span class="ant-segmented-item-icon">{o.icon}</span>
                    <span>{o.label}</span>
                  </>
                ) : (
                  o.label
                )}
              </div>
            </label>
          );
        })}
      </div>
    </div>
  );
}
