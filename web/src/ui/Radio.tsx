import { createContext, type ComponentChildren, type JSX } from "preact";
import { useContext } from "preact/hooks";
import { cx, useUid } from "./util";

type RadioEvent<T> = { target: { value: T; checked: boolean } };
interface GroupCtxValue {
  name: string;
  value: unknown;
  onChange: (v: unknown) => void;
  button: boolean;
  disabled?: boolean;
  size?: string;
}
const GroupCtx = createContext<GroupCtxValue | null>(null);

export interface RadioProps {
  value?: unknown;
  checked?: boolean;
  disabled?: boolean;
  onChange?: (e: RadioEvent<unknown>) => void;
  children?: ComponentChildren;
  className?: string;
  style?: JSX.CSSProperties;
}

function RadioBase({ value, checked, disabled, onChange, children, className, style, button }: RadioProps & { button?: boolean }) {
  const g = useContext(GroupCtx);
  const isBtn = button || g?.button;
  const p = isBtn ? "ant-radio-button" : "ant-radio";
  const on = g ? g.value === value : !!checked;
  const dis = disabled || g?.disabled;
  return (
    <label class={cx(`${p}-wrapper`, on && `${p}-wrapper-checked`, dis && `${p}-wrapper-disabled`, isBtn && g?.size === "small" && "ant-radio-button-wrapper-sm", className)} style={style}>
      <span class={cx(p, !isBtn && "ant-wave-target", on && `${p}-checked`, dis && `${p}-disabled`)}>
        <input
          name={g?.name}
          class={`${p}-input`}
          type="radio"
          value={String(value ?? "")}
          checked={on}
          disabled={dis}
          onChange={() => {
            if (g) g.onChange(value);
            else onChange?.({ target: { value, checked: true } });
          }}
        />
        <span class={`${p}-inner`} />
      </span>
      {children !== undefined && <span class={`${p}-label`}>{children}</span>}
    </label>
  );
}

export function Radio(props: RadioProps) {
  return <RadioBase {...props} />;
}
Radio.Button = (props: RadioProps) => <RadioBase {...props} button />;

type Opt = string | number | { label: ComponentChildren; value: unknown; disabled?: boolean };
export interface RadioGroupProps {
  value?: unknown;
  onChange?: (e: RadioEvent<any>) => void;
  options?: Opt[];
  optionType?: "default" | "button";
  buttonStyle?: "outline" | "solid";
  disabled?: boolean;
  size?: "small" | "middle" | "large";
  children?: ComponentChildren;
  style?: JSX.CSSProperties;
  className?: string;
  id?: string;
  "aria-label"?: string;
}

function Group({ value, onChange, options, optionType, buttonStyle = "outline", disabled, size, children, style, className, id, ...rest }: RadioGroupProps) {
  const name = useUid("radio");
  const ctx: GroupCtxValue = { name, value, onChange: (v) => onChange?.({ target: { value: v, checked: true } }), button: optionType === "button", disabled, size };
  return (
    <GroupCtx.Provider value={ctx}>
      <div id={id} role="radiogroup" aria-label={rest["aria-label"]} class={cx("ant-radio-group", `ant-radio-group-${buttonStyle}`, size === "large" && "ant-radio-group-large", size === "small" && "ant-radio-group-small", className)} style={style}>
        {options
          ? options.map((o) => {
              const opt = typeof o === "object" ? o : { label: String(o), value: o };
              return (
                <RadioBase value={opt.value} disabled={opt.disabled}>
                  {opt.label}
                </RadioBase>
              );
            })
          : children}
      </div>
    </GroupCtx.Provider>
  );
}
Radio.Group = Group;
