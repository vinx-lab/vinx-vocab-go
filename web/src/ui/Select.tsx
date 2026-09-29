import type { ComponentChildren, JSX } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import { Empty } from "./Display";
import { CheckOutlined, CloseCircleFilled, CloseOutlined, DownOutlined, LoadingOutlined, SearchOutlined } from "./icons";
import { useLayer } from "./layers";
import { Positioned } from "./Popup";
import { cx, Portal, sizeCls, type Size } from "./util";

export interface SelectOption<V = any> {
  value: V;
  label?: ComponentChildren;
  disabled?: boolean;
  title?: string;
}

export interface SelectProps<V = any> {
  id?: string;
  value?: V | V[] | null;
  defaultValue?: V | V[];
  onChange?: (value: any, option?: any) => void;
  onSelect?: (value: V, option: SelectOption<V>) => void;
  onSearch?: (text: string) => void;
  options?: SelectOption<V>[];
  mode?: "multiple" | "tags";
  placeholder?: ComponentChildren;
  showSearch?: boolean;
  optionFilterProp?: "label" | "value";
  filterOption?: boolean | ((input: string, option: SelectOption<V>) => boolean);
  allowClear?: boolean;
  loading?: boolean;
  disabled?: boolean;
  size?: Size;
  status?: "error" | "warning" | "";
  notFoundContent?: ComponentChildren;
  maxTagCount?: number | "responsive";
  open?: boolean;
  onDropdownVisibleChange?: (open: boolean) => void;
  onOpenChange?: (open: boolean) => void;
  style?: JSX.CSSProperties;
  className?: string;
  popupMatchSelectWidth?: boolean;
  "aria-label"?: string;
}

const text = (o: SelectOption) => (typeof o.label === "string" || typeof o.label === "number" ? String(o.label) : String(o.value));

/** antd Select：单选 / 多选、可搜索、受控 open；下拉挂在 body 下 */
export function Select<V = any>(props: SelectProps<V>) {
  const { id, options = [], mode, placeholder, showSearch, optionFilterProp = "value", filterOption = true, allowClear, loading, disabled, size, status, notFoundContent, maxTagCount, style, className } = props;
  const multiple = mode === "multiple" || mode === "tags";
  const [innerVal, setInnerVal] = useState(props.defaultValue);
  const value = props.value !== undefined ? props.value : innerVal;
  const values: V[] = multiple ? ((value as V[] | null) ?? []) : value === null || value === undefined ? [] : [value as V];
  const [innerOpen, setInnerOpen] = useState(false);
  const open = !disabled && (props.open ?? innerOpen);
  const [search, setSearch] = useState("");
  const [focused, setFocused] = useState(false);
  const [active, setActive] = useState(0);
  const root = useRef<HTMLDivElement>(null);
  const input = useRef<HTMLInputElement>(null);
  const popup = useRef<HTMLDivElement | null>(null);
  const searchable = showSearch || multiple;

  const setOpen = (o: boolean) => {
    if (o === open) return;
    if (props.open === undefined) setInnerOpen(o);
    props.onDropdownVisibleChange?.(o);
    props.onOpenChange?.(o);
    if (!o && search) {
      setSearch("");
      props.onSearch?.("");
    }
  };

  const filtered =
    searchable && search && filterOption !== false
      ? options.filter((o) =>
          typeof filterOption === "function"
            ? filterOption(search, o)
            : (optionFilterProp === "label" ? text(o) : String(o.value)).toLowerCase().includes(search.toLowerCase()),
        )
      : options;

  useEffect(() => {
    const i = filtered.findIndex((o) => values.includes(o.value));
    setActive(i >= 0 ? i : 0);
  }, [open, search]);

  useLayer(open, () => setOpen(false));

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

  const choose = (o: SelectOption<V>) => {
    if (o.disabled) return;
    props.onSelect?.(o.value, o);
    if (multiple) {
      const next = values.includes(o.value) ? values.filter((v) => v !== o.value) : [...values, o.value];
      setInnerVal(next as never);
      props.onChange?.(next, options.filter((x) => next.includes(x.value)));
      if (search) {
        setSearch("");
        props.onSearch?.("");
      }
    } else {
      setInnerVal(o.value as never);
      props.onChange?.(o.value, o);
      setOpen(false);
      setSearch("");
    }
  };
  const clear = (e: MouseEvent) => {
    e.stopPropagation();
    const next = multiple ? [] : undefined;
    setInnerVal(next as never);
    props.onChange?.(next, multiple ? [] : undefined);
  };

  const onKey = (e: KeyboardEvent) => {
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      if (!open) return setOpen(true);
      const d = e.key === "ArrowDown" ? 1 : -1;
      setActive((a) => (filtered.length ? (a + d + filtered.length) % filtered.length : 0));
    } else if (e.key === "Enter") {
      e.preventDefault();
      if (!open) return setOpen(true);
      const o = filtered[active];
      if (o) choose(o);
    }
    else if (e.key === "Backspace" && multiple && !search && values.length) {
      const next = values.slice(0, -1);
      setInnerVal(next as never);
      props.onChange?.(next);
    }
  };

  const selectedOpt = !multiple && values.length ? options.find((o) => o.value === values[0]) : undefined;
  const showClear = allowClear && !disabled && values.length > 0;
  const searchInput = (
    <input
      ref={input}
      id={id}
      type="search"
      autoComplete="off"
      class="ant-select-selection-search-input"
      role="combobox"
      aria-expanded={open}
      aria-haspopup="listbox"
      aria-autocomplete="list"
      aria-label={props["aria-label"]}
      readOnly={!searchable}
      disabled={disabled}
      value={search}
      style={searchable ? undefined : { opacity: 0 }}
      onInput={(e) => {
        const v = (e.currentTarget as HTMLInputElement).value;
        setSearch(v);
        props.onSearch?.(v);
        setOpen(true);
      }}
      onKeyDown={onKey}
      onFocus={() => setFocused(true)}
      onBlur={() => setFocused(false)}
    />
  );

  const tags = multiple ? (typeof maxTagCount === "number" ? values.slice(0, maxTagCount) : values) : [];
  const rest = multiple && typeof maxTagCount === "number" ? values.length - maxTagCount : 0;

  return (
    <div
      ref={root}
      class={cx(
        "ant-select",
        sizeCls("ant-select", size),
        "ant-select-outlined",
        status && `ant-select-status-${status}`,
        multiple ? "ant-select-multiple" : "ant-select-single",
        showClear && "ant-select-allow-clear",
        "ant-select-show-arrow",
        disabled && "ant-select-disabled",
        loading && "ant-select-loading",
        open && "ant-select-open",
        (focused || open) && "ant-select-focused",
        searchable && "ant-select-show-search",
        className,
      )}
      style={style}
      onMouseDown={(e) => {
        if (disabled) return;
        if ((e.target as HTMLElement).closest(".ant-select-selection-item-remove,.ant-select-clear")) return;
        if (e.target !== input.current) e.preventDefault();
        input.current?.focus();
        setOpen(!open || (searchable && e.target === input.current) ? true : false);
      }}
    >
      <div class="ant-select-selector">
        <span class="ant-select-selection-wrap">
          {multiple ? (
            <div class="ant-select-selection-overflow">
              {tags.map((v) => {
                const o = options.find((x) => x.value === v);
                return (
                  <div class="ant-select-selection-overflow-item" style={{ opacity: 1 }}>
                    <span title={o ? text(o) : String(v)} class="ant-select-selection-item">
                      <span class="ant-select-selection-item-content">{o?.label ?? String(v)}</span>
                      {!disabled && (
                        <span
                          class="ant-select-selection-item-remove"
                          aria-hidden="true"
                          onClick={(e) => {
                            e.stopPropagation();
                            const next = values.filter((x) => x !== v);
                            setInnerVal(next as never);
                            props.onChange?.(next);
                          }}
                        >
                          <CloseOutlined />
                        </span>
                      )}
                    </span>
                  </div>
                );
              })}
              {rest > 0 && (
                <div class="ant-select-selection-overflow-item ant-select-selection-overflow-item-rest" style={{ opacity: 1 }}>
                  <span class="ant-select-selection-item">
                    <span class="ant-select-selection-item-content">+ {rest} ...</span>
                  </span>
                </div>
              )}
              <div class="ant-select-selection-overflow-item ant-select-selection-overflow-item-suffix" style={{ opacity: 1 }}>
                <div class="ant-select-selection-search" style={{ width: search ? `${search.length + 1}ch` : 4 }}>
                  {searchInput}
                  <span class="ant-select-selection-search-mirror" aria-hidden="true">
                    {search || " "}
                  </span>
                </div>
              </div>
              {values.length === 0 && !search && placeholder && <span class="ant-select-selection-placeholder">{placeholder}</span>}
            </div>
          ) : (
            <>
              <span class="ant-select-selection-search">{searchInput}</span>
              {selectedOpt || (values.length && !selectedOpt) ? (
                <span class="ant-select-selection-item" title={selectedOpt ? text(selectedOpt) : String(values[0])} style={search ? { visibility: "hidden" } : undefined}>
                  {selectedOpt?.label ?? String(values[0])}
                </span>
              ) : (
                !search && placeholder && <span class="ant-select-selection-placeholder">{placeholder}</span>
              )}
            </>
          )}
        </span>
      </div>
      <span class={cx("ant-select-arrow", loading && "ant-select-arrow-loading")} aria-hidden="true" style={{ userSelect: "none" }}>
        {loading ? <LoadingOutlined /> : open && showSearch ? <SearchOutlined className="ant-select-suffix" /> : <DownOutlined className="ant-select-suffix" />}
      </span>
      {showClear && (
        <span class="ant-select-clear" aria-hidden="true" style={{ userSelect: "none" }} onMouseDown={clear}>
          <CloseCircleFilled />
        </span>
      )}
      {open && root.current && (
        <Portal>
          <Positioned anchor={root.current} placement="bottomLeft" gap={4} prefix="ant-select-dropdown" matchWidth={props.popupMatchSelectWidth !== false} popupRef={(el) => (popup.current = el)}>
            <div role="listbox" onMouseDown={(e) => e.preventDefault()}>
              {filtered.length === 0 ? (
                <div class="ant-select-item-empty">{notFoundContent !== undefined ? notFoundContent : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} />}</div>
              ) : (
                <div style={{ maxHeight: 256, overflowY: "auto", overflowAnchor: "none" }}>
                  {filtered.map((o, i) => {
                    const sel = values.includes(o.value);
                    return (
                      <div
                        role="option"
                        aria-selected={sel}
                        title={o.title ?? text(o)}
                        class={cx("ant-select-item", "ant-select-item-option", i === active && "ant-select-item-option-active", sel && "ant-select-item-option-selected", o.disabled && "ant-select-item-option-disabled")}
                        onMouseEnter={() => setActive(i)}
                        onClick={() => choose(o)}
                      >
                        <div class="ant-select-item-option-content">{o.label ?? String(o.value)}</div>
                        {multiple && sel && (
                          <span class="ant-select-item-option-state" aria-hidden="true">
                            <CheckOutlined />
                          </span>
                        )}
                      </div>
                    );
                  })}
                </div>
              )}
            </div>
          </Positioned>
        </Portal>
      )}
    </div>
  );
}
