/** InputNumber、DatePicker */
import type { JSX } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import dayjs, { type Dayjs } from "dayjs";
import { CalendarOutlined, CloseCircleFilled, DownOutlined, UpOutlined } from "./icons";
import { useLayer } from "./layers";
import { Positioned } from "./Popup";
import { cx, Portal, sizeCls, type Size } from "./util";

export interface InputNumberProps {
  id?: string;
  value?: number | null;
  defaultValue?: number | null;
  onChange?: (v: number | null) => void;
  min?: number;
  max?: number;
  step?: number;
  precision?: number;
  disabled?: boolean;
  size?: Size;
  placeholder?: string;
  status?: "error" | "warning" | "";
  style?: JSX.CSSProperties;
  className?: string;
  addonAfter?: string;
  "aria-label"?: string;
}

const fmt = (v: number | null | undefined, precision?: number) => (v === null || v === undefined || Number.isNaN(v) ? "" : precision !== undefined ? v.toFixed(precision) : String(v));

export function InputNumber({ id, value, defaultValue = null, onChange, min = -Infinity, max = Infinity, step = 1, precision, disabled, size, placeholder, status, style, className, ...rest }: InputNumberProps) {
  const [inner, setInner] = useState<number | null>(defaultValue);
  const cur = value !== undefined ? value : inner;
  const [textVal, setText] = useState(fmt(cur, precision));
  const editing = useRef(false);
  useEffect(() => {
    if (!editing.current) setText(fmt(cur, precision));
  }, [cur, precision]);
  const emit = (v: number | null) => {
    setInner(v);
    onChange?.(v);
  };
  const clamp = (v: number) => {
    let n = Math.min(max, Math.max(min, v));
    if (precision !== undefined) n = Number(n.toFixed(precision));
    return n;
  };
  const stepBy = (d: number) => {
    if (disabled) return;
    const base = cur ?? (min > -Infinity ? min : 0);
    const n = clamp(Number((base + d * step).toFixed(10)));
    setText(fmt(n, precision));
    emit(n);
  };
  return (
    <div class={cx("ant-input-number", sizeCls("ant-input-number", size), "ant-input-number-outlined", disabled && "ant-input-number-disabled", status && `ant-input-number-status-${status}`, className)} style={style}>
      <div class="ant-input-number-handler-wrap">
        <span role="button" aria-label="Increase Value" aria-disabled={cur !== null && cur >= max} class={cx("ant-input-number-handler", "ant-input-number-handler-up", cur !== null && cur >= max && "ant-input-number-handler-up-disabled")} onMouseDown={(e) => { e.preventDefault(); stepBy(1); }}>
          <UpOutlined className="ant-input-number-handler-up-inner" />
        </span>
        <span role="button" aria-label="Decrease Value" aria-disabled={cur !== null && cur <= min} class={cx("ant-input-number-handler", "ant-input-number-handler-down", cur !== null && cur <= min && "ant-input-number-handler-down-disabled")} onMouseDown={(e) => { e.preventDefault(); stepBy(-1); }}>
          <DownOutlined className="ant-input-number-handler-down-inner" />
        </span>
      </div>
      <div class="ant-input-number-input-wrap">
        <input
          id={id}
          autoComplete="off"
          role="spinbutton"
          aria-valuemin={Number.isFinite(min) ? min : undefined}
          aria-valuemax={Number.isFinite(max) ? max : undefined}
          aria-valuenow={cur ?? undefined}
          aria-label={rest["aria-label"]}
          step={step}
          inputMode="decimal"
          class="ant-input-number-input"
          value={textVal}
          placeholder={placeholder}
          disabled={disabled}
          onFocus={() => (editing.current = true)}
          onInput={(e) => {
            const t = (e.currentTarget as HTMLInputElement).value;
            setText(t);
            if (t.trim() === "") return emit(null);
            const n = Number(t);
            if (!Number.isNaN(n) && n >= min && n <= max) emit(precision !== undefined ? Number(n.toFixed(precision)) : n);
          }}
          onBlur={() => {
            editing.current = false;
            if (textVal.trim() === "") {
              setText("");
              return;
            }
            const n = Number(textVal);
            const next = Number.isNaN(n) ? cur : clamp(n);
            setText(fmt(next, precision));
            if (next !== cur) emit(next);
          }}
          onKeyDown={(e) => {
            if (e.key === "ArrowUp") {
              e.preventDefault();
              stepBy(1);
            } else if (e.key === "ArrowDown") {
              e.preventDefault();
              stepBy(-1);
            }
          }}
        />
      </div>
    </div>
  );
}

export interface DatePickerProps {
  id?: string;
  value?: Dayjs | null;
  onChange?: (d: Dayjs | null, s: string) => void;
  placeholder?: string;
  disabledDate?: (d: Dayjs) => boolean;
  disabled?: boolean;
  allowClear?: boolean;
  format?: string;
  size?: Size;
  status?: "error" | "warning" | "";
  style?: JSX.CSSProperties;
  className?: string;
}

const WEEK = ["一", "二", "三", "四", "五", "六", "日"];

/** antd DatePicker（日期面板，zh-cn：周一开头） */
export function DatePicker({ id, value, onChange, placeholder = "请选择日期", disabledDate, disabled, allowClear = true, format = "YYYY-MM-DD", size, status, style, className }: DatePickerProps) {
  const [open, setOpen] = useState(false);
  const [view, setView] = useState<Dayjs>(() => (value ?? dayjs()).startOf("month"));
  const root = useRef<HTMLDivElement>(null);
  const popup = useRef<HTMLDivElement | null>(null);
  const [hover, setHover] = useState(false);
  useEffect(() => {
    if (open) setView((value ?? dayjs()).startOf("month"));
  }, [open]);
  useEffect(() => {
    if (!open) return;
    const onDown = (e: Event) => {
      const t = e.target as Node;
      if (root.current?.contains(t) || popup.current?.contains(t)) return;
      setOpen(false);
    };
    document.addEventListener("mousedown", onDown, true);
    return () => document.removeEventListener("mousedown", onDown, true);
  }, [open]);
  useLayer(open, () => setOpen(false));
  const pick = (d: Dayjs | null) => {
    onChange?.(d, d ? d.format(format) : "");
    setOpen(false);
  };
  // 周一开头：本月 1 号往前退到周一
  const first = view.startOf("month");
  const start = first.subtract((first.day() + 6) % 7, "day");
  const today = dayjs();
  const weeks = Array.from({ length: 6 }, (_, w) => Array.from({ length: 7 }, (_, d) => start.add(w * 7 + d, "day")));
  const todayDisabled = disabledDate?.(today) ?? false;
  return (
    <div
      ref={root}
      class={cx("ant-picker", sizeCls("ant-picker", size), "ant-picker-outlined", open && "ant-picker-focused", disabled && "ant-picker-disabled", status && `ant-picker-status-${status}`, className)}
      style={style}
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
      onMouseDown={(e) => {
        if (disabled) return;
        if ((e.target as HTMLElement).tagName !== "INPUT") e.preventDefault();
        root.current?.querySelector("input")?.focus();
        setOpen(true);
      }}
    >
      <div class="ant-picker-input">
        <input id={id} aria-invalid={status === "error"} autoComplete="off" size={12} placeholder={placeholder} value={value ? value.format(format) : ""} readOnly disabled={disabled} />
        <span class="ant-picker-suffix">
          <CalendarOutlined />
        </span>
        {allowClear && value && !disabled && hover && (
          <span
            class="ant-picker-clear"
            role="button"
            onMouseDown={(e) => {
              e.stopPropagation();
              e.preventDefault();
              pick(null);
            }}
          >
            <CloseCircleFilled />
          </span>
        )}
      </div>
      {open && root.current && (
        <Portal>
          <Positioned anchor={root.current} placement="bottomLeft" gap={4} prefix="ant-picker-dropdown" popupRef={(el) => (popup.current = el)}>
            <div tabIndex={-1} class="ant-picker-panel-container ant-picker-date-panel-container" onMouseDown={(e) => e.preventDefault()}>
              <div class="ant-picker-panel-layout">
                <div>
                  <div tabIndex={0} class="ant-picker-panel">
                    <div class="ant-picker-date-panel">
                      <div class="ant-picker-header">
                        <button type="button" aria-label="上一年" tabIndex={-1} class="ant-picker-header-super-prev-btn" onClick={() => setView(view.subtract(1, "year"))}>
                          <span class="ant-picker-super-prev-icon" />
                        </button>
                        <button type="button" aria-label="上个月" tabIndex={-1} class="ant-picker-header-prev-btn" onClick={() => setView(view.subtract(1, "month"))}>
                          <span class="ant-picker-prev-icon" />
                        </button>
                        <div class="ant-picker-header-view">
                          <button type="button" tabIndex={-1} class="ant-picker-year-btn">
                            {view.year()}年
                          </button>
                          <button type="button" tabIndex={-1} class="ant-picker-month-btn">
                            {view.month() + 1}月
                          </button>
                        </div>
                        <button type="button" aria-label="下个月" tabIndex={-1} class="ant-picker-header-next-btn" onClick={() => setView(view.add(1, "month"))}>
                          <span class="ant-picker-next-icon" />
                        </button>
                        <button type="button" aria-label="下一年" tabIndex={-1} class="ant-picker-header-super-next-btn" onClick={() => setView(view.add(1, "year"))}>
                          <span class="ant-picker-super-next-icon" />
                        </button>
                      </div>
                      <div class="ant-picker-body">
                        <table class="ant-picker-content">
                          <thead>
                            <tr>
                              {WEEK.map((w) => (
                                <th>{w}</th>
                              ))}
                            </tr>
                          </thead>
                          <tbody>
                            {weeks.map((row) => (
                              <tr>
                                {row.map((d) => {
                                  const dis = disabledDate?.(d) ?? false;
                                  const inView = d.month() === view.month();
                                  return (
                                    <td
                                      title={d.format("YYYY-MM-DD")}
                                      class={cx("ant-picker-cell", inView && "ant-picker-cell-in-view", d.isSame(today, "day") && "ant-picker-cell-today", value && d.isSame(value, "day") && "ant-picker-cell-selected", dis && "ant-picker-cell-disabled")}
                                      onClick={() => !dis && pick(d.startOf("day"))}
                                    >
                                      <div class="ant-picker-cell-inner">{d.date()}</div>
                                    </td>
                                  );
                                })}
                              </tr>
                            ))}
                          </tbody>
                        </table>
                      </div>
                      <div class="ant-picker-footer">
                        <ul class="ant-picker-ranges">
                          <li class="ant-picker-now">
                            <a class={cx("ant-picker-now-btn", todayDisabled && "ant-picker-now-btn-disabled")} aria-disabled={todayDisabled} onClick={() => !todayDisabled && pick(today.startOf("day"))}>
                              今天
                            </a>
                          </li>
                        </ul>
                      </div>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </Positioned>
        </Portal>
      )}
    </div>
  );
}
