import type { ComponentChildren, JSX, Ref } from "preact";
import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";
import { Button } from "./Button";
import { CloseCircleFilled, EyeInvisibleOutlined, EyeOutlined, SearchOutlined } from "./icons";
import { cx, sizeCls, type Size } from "./util";

/** onChange 收到的是原生 input 事件（e.target.value 可用），与 antd 用法一致 */
export type InputChange = (e: { target: { value: string } } & Partial<Event>) => void;

export interface InputProps {
  id?: string;
  value?: string | null;
  defaultValue?: string;
  onChange?: InputChange;
  onPressEnter?: (e: KeyboardEvent) => void;
  onKeyDown?: (e: KeyboardEvent) => void;
  onBlur?: (e: FocusEvent) => void;
  onFocus?: (e: FocusEvent) => void;
  placeholder?: string;
  maxLength?: number;
  size?: Size;
  prefix?: ComponentChildren;
  suffix?: ComponentChildren;
  allowClear?: boolean;
  showCount?: boolean;
  status?: "error" | "warning" | "";
  disabled?: boolean;
  readOnly?: boolean;
  autoFocus?: boolean;
  autoComplete?: string;
  inputMode?: string;
  autoCorrect?: string;
  autoCapitalize?: string;
  spellCheck?: boolean;
  type?: string;
  name?: string;
  className?: string;
  style?: JSX.CSSProperties;
  inputRef?: Ref<HTMLInputElement>;
  "aria-label"?: string;
  "aria-invalid"?: boolean;
  "aria-required"?: boolean;
  "aria-describedby"?: string;
}

/** 受控/非受控合一：value 为 undefined 时用内部状态；value 从有到无（表单重置）时清空内部状态 */
export function useMerged<T>(value: T | undefined, initial: T): [T, (v: T) => void] {
  const [inner, setInner] = useState<T>(value !== undefined ? value : initial);
  const prev = useRef(value);
  useEffect(() => {
    if (prev.current !== undefined && value === undefined) setInner(initial);
    prev.current = value;
  }, [value]);
  return [value !== undefined ? value : inner, setInner];
}

const fire = (onChange: InputChange | undefined, value: string, e?: Event) => {
  onChange?.(e && (e.target as HTMLInputElement)?.value === value ? (e as never) : ({ target: { value }, type: e?.type ?? "change" } as never));
};

export function Input(props: InputProps & { _wrapperExtra?: string; _suffixExtra?: ComponentChildren }) {
  const { id, value, defaultValue, onChange, onPressEnter, onKeyDown, placeholder, maxLength, size, prefix, suffix, allowClear, showCount, status, disabled, className, style, inputRef, type = "text", _wrapperExtra, _suffixExtra, spellCheck, ...rest } = props;
  const [val, setVal] = useMerged<string>(value === null ? "" : value, defaultValue ?? "");
  const affix = prefix || suffix || allowClear || showCount || _suffixExtra;
  const statusCls = status ? `ant-input-status-${status}` : "";
  const input = (
    <input
      id={id}
      ref={inputRef}
      type={type}
      value={val}
      placeholder={placeholder}
      maxLength={maxLength}
      disabled={disabled}
      spellcheck={spellCheck}
      class={cx("ant-input", sizeCls("ant-input", size), !affix && disabled && "ant-input-disabled", !affix && "ant-input-outlined", !affix && statusCls, !affix && className)}
      style={affix ? undefined : style}
      onInput={(e) => {
        const v = (e.currentTarget as HTMLInputElement).value;
        setVal(v);
        fire(onChange, v, e);
      }}
      onKeyDown={(e) => {
        onKeyDown?.(e);
        if (e.key === "Enter" && !e.isComposing) onPressEnter?.(e);
      }}
      {...(rest as object)}
    />
  );
  if (!affix) return input;
  const hasSuffix = suffix || showCount || _suffixExtra;
  return (
    <span
      class={cx(
        "ant-input-affix-wrapper",
        sizeCls("ant-input-affix-wrapper", size),
        disabled && "ant-input-affix-wrapper-disabled",
        status && `ant-input-affix-wrapper-status-${status}`,
        "ant-input-outlined",
        disabled && "ant-input-disabled",
        statusCls,
        _wrapperExtra,
        className,
      )}
      style={style}
    >
      {prefix && <span class="ant-input-prefix">{prefix}</span>}
      {input}
      {(allowClear || hasSuffix) && (
        <span class="ant-input-suffix">
          {allowClear && (
            <button
              type="button"
              tabIndex={-1}
              class={cx("ant-input-clear-icon", (!val || disabled) && "ant-input-clear-icon-hidden", hasSuffix && "ant-input-clear-icon-has-suffix")}
              onClick={(e) => {
                setVal("");
                fire(onChange, "", { type: "click", target: { value: "" } } as never);
                (e.currentTarget as HTMLElement).parentElement?.parentElement?.querySelector("input")?.focus();
              }}
            >
              <CloseCircleFilled />
            </button>
          )}
          {showCount && <span class={cx("ant-input-show-count-suffix", suffix && "ant-input-show-count-has-suffix")}>{maxLength ? `${val.length} / ${maxLength}` : val.length}</span>}
          {suffix}
          {_suffixExtra}
        </span>
      )}
    </span>
  );
}

function Password(props: InputProps) {
  const [visible, setVisible] = useState(false);
  const Eye = visible ? EyeOutlined : EyeInvisibleOutlined;
  return (
    <Input
      {...props}
      type={visible ? "text" : "password"}
      _wrapperExtra={cx("ant-input-password", props.size === "large" && "ant-input-password-large", props.size === "small" && "ant-input-password-small")}
      _suffixExtra={<Eye className="ant-input-password-icon" onClick={() => !props.disabled && setVisible(!visible)} />}
    />
  );
}

function Search(props: InputProps & { onSearch?: (value: string, e?: Event, info?: { source: "input" | "clear" }) => void; enterButton?: ComponentChildren; loading?: boolean }) {
  const { onSearch, style, className, size, enterButton, loading, ...rest } = props;
  const [val, setVal] = useMerged<string>(props.value === null ? "" : props.value, props.defaultValue ?? "");
  return (
    <span class={cx("ant-input-group-wrapper", sizeCls("ant-input-group-wrapper", size), "ant-input-group-wrapper-outlined", "ant-input-search", sizeCls("ant-input-search", size), className)} style={style}>
      <span class="ant-input-wrapper ant-input-group">
        <Input
          {...rest}
          size={size}
          type="search"
          value={val}
          onChange={(e) => {
            setVal(e.target.value);
            props.onChange?.(e);
            if ((e as { type?: string }).type === "click") onSearch?.("", e as Event, { source: "clear" });
          }}
          onPressEnter={(e) => onSearch?.(val, e, { source: "input" })}
        />
        <span class="ant-input-group-addon">
          <Button className="ant-input-search-button" type={enterButton ? "primary" : "default"} size={size} icon={enterButton ? undefined : <SearchOutlined />} loading={loading} onClick={(e) => onSearch?.(val, e, { source: "input" })}>
            {typeof enterButton === "boolean" ? undefined : enterButton}
          </Button>
        </span>
      </span>
    </span>
  );
}

export interface TextAreaCount {
  show?: boolean;
  max?: number;
  strategy?: (value: string) => number;
}

export interface TextAreaProps extends Omit<InputProps, "prefix" | "suffix" | "allowClear" | "onChange"> {
  rows?: number;
  autoSize?: boolean | { minRows?: number; maxRows?: number };
  /** antd v5 的 count 写法：{ show, max, strategy }，strategy 决定计数方式（如去掉首尾空白） */
  count?: TextAreaCount;
  onChange?: (e: { target: { value: string } }) => void;
  textareaRef?: Ref<HTMLTextAreaElement>;
}

interface AutoSizeStyle {
  height: number;
  minHeight?: number;
  maxHeight?: number;
  overflowY?: "hidden";
}

const SIZING_STYLE = ["letter-spacing", "line-height", "padding-top", "padding-bottom", "font-family", "font-weight", "font-size", "font-variant", "text-rendering", "text-transform", "width", "text-indent", "padding-left", "padding-right", "border-width", "box-sizing", "word-break", "white-space"];
const HIDDEN_STYLE = "min-height:0 !important;max-height:none !important;height:0 !important;visibility:hidden !important;overflow:hidden !important;position:absolute !important;z-index:-1000 !important;top:0 !important;right:0 !important;pointer-events:none !important";
let hidden: HTMLTextAreaElement | undefined;

/** 与 antd（rc-textarea calculateNodeHeight）相同的测量：隐藏的影子 textarea 复制尺寸样式，
 * 空值时按 placeholder 量高；minRows/maxRows 用实测单行高度（scrollHeight）而不是 line-height 换算 */
function calcAutoSize(node: HTMLTextAreaElement, minRows: number | null, maxRows: number | null): AutoSizeStyle {
  if (!hidden) {
    hidden = document.createElement("textarea");
    hidden.setAttribute("tab-index", "-1");
    hidden.setAttribute("aria-hidden", "true");
    hidden.setAttribute("name", "hiddenTextarea");
    document.body.appendChild(hidden);
  }
  const wrap = node.getAttribute("wrap");
  if (wrap) hidden.setAttribute("wrap", wrap);
  else hidden.removeAttribute("wrap");
  const cs = getComputedStyle(node);
  const boxSizing = cs.getPropertyValue("box-sizing");
  const paddingSize = parseFloat(cs.getPropertyValue("padding-bottom")) + parseFloat(cs.getPropertyValue("padding-top"));
  const borderSize = parseFloat(cs.getPropertyValue("border-bottom-width")) + parseFloat(cs.getPropertyValue("border-top-width"));
  hidden.setAttribute("style", `${SIZING_STYLE.map((n) => `${n}:${cs.getPropertyValue(n)}`).join(";")};${HIDDEN_STYLE}`);
  hidden.value = node.value || node.placeholder || "";
  let height = hidden.scrollHeight;
  if (boxSizing === "border-box") height += borderSize;
  else if (boxSizing === "content-box") height -= paddingSize;
  const out: AutoSizeStyle = { height };
  if (minRows !== null || maxRows !== null) {
    hidden.value = " ";
    const single = hidden.scrollHeight - paddingSize;
    if (minRows !== null) {
      let min = single * minRows;
      if (boxSizing === "border-box") min += paddingSize + borderSize;
      height = Math.max(min, height);
      if (min) out.minHeight = min;
    }
    if (maxRows !== null) {
      let max = single * maxRows;
      if (boxSizing === "border-box") max += paddingSize + borderSize;
      if (!(height > max)) out.overflowY = "hidden";
      height = Math.min(max, height);
      if (max) out.maxHeight = max;
    }
  } else {
    out.overflowY = undefined;
  }
  out.height = height;
  return out;
}

function TextArea(props: TextAreaProps) {
  const { id, value, defaultValue, onChange, rows, autoSize, showCount, maxLength, status, disabled, className, style, textareaRef, size, onPressEnter, count, ...rest } = props;
  const showsCount = showCount || count?.show;
  const [val, setVal] = useMerged<string>(value === null ? "" : value, defaultValue ?? "");
  const ref = useRef<HTMLTextAreaElement>(null);
  const [auto, setAuto] = useState<AutoSizeStyle | undefined>();
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el || !autoSize) return;
    const o = typeof autoSize === "object" ? autoSize : {};
    setAuto(calcAutoSize(el, o.minRows ?? null, o.maxRows ?? null));
  }, [val, autoSize]);
  const ta = (
    <textarea
      id={id}
      ref={(el) => {
        ref.current = el;
        if (typeof textareaRef === "function") textareaRef(el);
        else if (textareaRef) (textareaRef as { current: unknown }).current = el;
      }}
      rows={rows}
      value={val}
      maxLength={count ? undefined : maxLength}
      disabled={disabled}
      class={cx("ant-input", sizeCls("ant-input", size), !showsCount && "ant-input-outlined", !showsCount && disabled && "ant-input-disabled", !showsCount && status && `ant-input-status-${status}`, !showsCount && className)}
      style={{ ...(autoSize ? { resize: "none", ...auto } : {}), ...(showsCount ? {} : (style as object)) }}
      onInput={(e) => {
        const v = (e.currentTarget as HTMLTextAreaElement).value;
        setVal(v);
        onChange?.(e as never);
      }}
      onKeyDown={(e) => {
        if (e.key === "Enter" && !e.isComposing) onPressEnter?.(e);
      }}
      {...(rest as object)}
    />
  );
  if (!showsCount) return ta;
  const countMax = count?.max ?? maxLength;
  const n = count?.strategy ? count.strategy(val) : val.length;
  const text = countMax ? `${n} / ${countMax}` : String(n);
  return (
    <span class={cx("ant-input-affix-wrapper", "ant-input-textarea-affix-wrapper", "ant-input-textarea-show-count", "ant-input-show-count", "ant-input-outlined", disabled && "ant-input-affix-wrapper-disabled", status && `ant-input-status-${status}`, className, !!countMax && n > countMax && "ant-input-out-of-range")} data-count={text} style={style}>
      {ta}
      <span class="ant-input-suffix">
        <span class="ant-input-data-count">{text}</span>
      </span>
    </span>
  );
}

Input.Password = Password;
Input.Search = Search;
Input.TextArea = TextArea;
export { TextArea, Password, Search };
