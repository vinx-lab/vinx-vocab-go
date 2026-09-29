/** 列表类：List、Collapse、Steps、Tabs、Upload */
import { toChildArray, type ComponentChildren, type JSX } from "preact";
import { useLayoutEffect, useRef, useState } from "preact/hooks";
import { Spin, Empty } from "./Display";
import { CheckOutlined, CloseOutlined, RightOutlined } from "./icons";
import { cx, useUid } from "./util";

type Style = JSX.CSSProperties;

export interface ListProps<T> {
  dataSource?: T[];
  renderItem?: (item: T, index: number) => ComponentChildren;
  loading?: boolean;
  bordered?: boolean;
  size?: "small" | "default" | "large";
  split?: boolean;
  header?: ComponentChildren;
  footer?: ComponentChildren;
  locale?: { emptyText?: ComponentChildren };
  style?: Style;
  className?: string;
  rowKey?: string | ((item: T) => string);
}

export function List<T>({ dataSource = [], renderItem, loading, bordered, size, split = true, header, footer, locale, style, className, rowKey }: ListProps<T>) {
  const key = (it: T, i: number) => (typeof rowKey === "function" ? rowKey(it) : rowKey ? String((it as Record<string, unknown>)[rowKey]) : i);
  const body =
    dataSource.length > 0 ? (
      <ul class="ant-list-items">{dataSource.map((it, i) => <ListKey key={key(it, i)}>{renderItem?.(it, i)}</ListKey>)}</ul>
    ) : loading ? (
      <div style={{ minHeight: 53 }} />
    ) : (
      <div class="ant-list-empty-text">{locale?.emptyText ?? <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} />}</div>
    );
  return (
    <div class={cx("ant-list", size === "small" && "ant-list-sm", size === "large" && "ant-list-lg", split && "ant-list-split", bordered && "ant-list-bordered", loading && "ant-list-loading", className)} style={style}>
      {header && <div class="ant-list-header">{header}</div>}
      <Spin spinning={!!loading}>{body}</Spin>
      {footer && <div class="ant-list-footer">{footer}</div>}
    </div>
  );
}
const ListKey = ({ children }: { children: ComponentChildren }) => <>{children}</>;

function ListItem({ actions, extra, style, className, children, onClick }: { actions?: ComponentChildren[]; extra?: ComponentChildren; style?: Style; className?: string; children?: ComponentChildren; onClick?: (e: MouseEvent) => void }) {
  return (
    <li class={cx("ant-list-item", className)} style={style} onClick={onClick}>
      {children}
      {actions && actions.length > 0 && (
        <ul class="ant-list-item-action">
          {actions.map((a, i) => (
            <li>
              {a}
              {i < actions.length - 1 && <em class="ant-list-item-action-split" />}
            </li>
          ))}
        </ul>
      )}
      {extra && <div class="ant-list-item-extra">{extra}</div>}
    </li>
  );
}
function ListItemMeta({ avatar, title, description }: { avatar?: ComponentChildren; title?: ComponentChildren; description?: ComponentChildren }) {
  return (
    <div class="ant-list-item-meta">
      {avatar && <div class="ant-list-item-meta-avatar">{avatar}</div>}
      <div class="ant-list-item-meta-content">
        {title && <h4 class="ant-list-item-meta-title">{title}</h4>}
        {description && <div class="ant-list-item-meta-description">{description}</div>}
      </div>
    </div>
  );
}
ListItem.Meta = ListItemMeta;
List.Item = ListItem;

export interface CollapseItem {
  key: string;
  label: ComponentChildren;
  children?: ComponentChildren;
  extra?: ComponentChildren;
  className?: string;
  style?: Style;
}
export function Collapse({ items = [], defaultActiveKey, activeKey, onChange, ghost, size, bordered = true, accordion, style, className }: { items?: (CollapseItem | null | undefined | false)[]; defaultActiveKey?: string | string[]; activeKey?: string | string[]; onChange?: (keys: string[]) => void; ghost?: boolean; size?: "small" | "middle" | "large"; bordered?: boolean; accordion?: boolean; style?: Style; className?: string }) {
  const toArr = (k?: string | string[]) => (k === undefined ? [] : Array.isArray(k) ? k : [k]);
  const [inner, setInner] = useState<string[]>(toArr(defaultActiveKey));
  const active = activeKey !== undefined ? toArr(activeKey) : inner;
  const toggle = (k: string) => {
    const next = active.includes(k) ? active.filter((x) => x !== k) : accordion ? [k] : [...active, k];
    setInner(next);
    onChange?.(next);
  };
  return (
    <div class={cx("ant-collapse", "ant-collapse-icon-position-start", !bordered && "ant-collapse-borderless", ghost && "ant-collapse-ghost", size === "small" && "ant-collapse-small", size === "large" && "ant-collapse-large", className)} style={style}>
      {items.filter(Boolean).map((raw) => {
        const it = raw as CollapseItem;
        const open = active.includes(it.key);
        return (
          <div key={it.key} class={cx("ant-collapse-item", open && "ant-collapse-item-active", it.className)} style={it.style}>
            <div class="ant-collapse-header" role="button" aria-expanded={open} aria-disabled="false" tabIndex={0} onClick={() => toggle(it.key)} onKeyDown={(e) => (e.key === "Enter" || e.key === " ") && toggle(it.key)}>
              <div class="ant-collapse-expand-icon">
                <RightOutlined className="ant-collapse-arrow" aria-label={open ? "expanded" : "collapsed"} style={open ? { transform: "rotate(90deg)" } : undefined} />
              </div>
              <span class="ant-collapse-header-text">{it.label}</span>
              {it.extra && (
                <div class="ant-collapse-extra" onClick={(e) => e.stopPropagation()}>
                  {it.extra}
                </div>
              )}
            </div>
            {open && (
              <div class="ant-collapse-content ant-collapse-content-active">
                <div class="ant-collapse-content-box">{it.children}</div>
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
}

export function Steps({ current = 0, size, status = "process", items = [], direction = "horizontal", responsive = true, style, className }: { current?: number; size?: "small" | "default"; status?: "wait" | "process" | "finish" | "error"; items?: { title?: ComponentChildren; description?: ComponentChildren }[]; direction?: "horizontal" | "vertical"; responsive?: boolean; style?: Style; className?: string }) {
  // responsive：窄屏（<532px）时 antd 改为竖排
  const narrow = responsive && typeof window !== "undefined" && window.innerWidth < 532;
  const dir = narrow ? "vertical" : direction;
  return (
    <div class={cx("ant-steps", `ant-steps-${dir}`, size === "small" && "ant-steps-small", dir === "horizontal" && "ant-steps-label-horizontal", className)} style={style}>
      {items.map((it, i) => {
        const st = i < current ? "finish" : i === current ? status : "wait";
        return (
          <div class={cx("ant-steps-item", `ant-steps-item-${st}`, i === current && "ant-steps-item-active", i === current - 1 && status === "error" && "ant-steps-next-error")}>
            <div class="ant-steps-item-container">
              <div class="ant-steps-item-tail" />
              <div class="ant-steps-item-icon">
                <span class="ant-steps-icon">{st === "finish" ? <CheckOutlined className="ant-steps-finish-icon" /> : st === "error" ? <CloseOutlined className="ant-steps-error-icon" /> : i + 1}</span>
              </div>
              <div class="ant-steps-item-content">
                <div class="ant-steps-item-title">{it.title}</div>
                {it.description && <div class="ant-steps-item-description">{it.description}</div>}
              </div>
            </div>
          </div>
        );
      })}
    </div>
  );
}

export interface TabItem {
  key: string;
  label: ComponentChildren;
  children?: ComponentChildren;
  disabled?: boolean;
}
export function Tabs({ items = [], activeKey, defaultActiveKey, onChange, destroyOnHidden, style, className, tabBarExtraContent, size }: { items?: TabItem[]; activeKey?: string; defaultActiveKey?: string; onChange?: (key: string) => void; destroyOnHidden?: boolean; style?: Style; className?: string; tabBarExtraContent?: ComponentChildren; size?: "small" | "middle" | "large" }) {
  const id = useUid("tabs");
  const [inner, setInner] = useState(defaultActiveKey ?? items[0]?.key);
  const active = activeKey ?? inner;
  const visited = useRef(new Set<string>());
  if (active) visited.current.add(active);
  const listRef = useRef<HTMLDivElement>(null);
  const [ink, setInk] = useState<Style>({});
  const measure = (el: HTMLDivElement | null) => {
    if (!el) return;
    const tab = el.querySelector<HTMLElement>(".ant-tabs-tab-active");
    if (!tab) return;
    const next = { width: tab.offsetWidth, left: tab.offsetLeft + tab.offsetWidth / 2, transform: "translateX(-50%)" };
    if (ink.width !== next.width || ink.left !== next.left) setInk(next);
  };
  useLayoutEffect(() => measure(listRef.current));
  return (
    <div class={cx("ant-tabs", "ant-tabs-top", size === "small" && "ant-tabs-small", size === "large" && "ant-tabs-large", className)} style={style}>
      <div role="tablist" aria-orientation="horizontal" class="ant-tabs-nav">
        <div class="ant-tabs-nav-wrap">
          <div class="ant-tabs-nav-list" ref={listRef}>
            {items.map((it) => {
              const on = it.key === active;
              return (
                <div data-node-key={it.key} class={cx("ant-tabs-tab", on && "ant-tabs-tab-active", it.disabled && "ant-tabs-tab-disabled")}>
                  <div
                    role="tab"
                    aria-selected={on}
                    class="ant-tabs-tab-btn"
                    tabIndex={on ? 0 : -1}
                    id={`${id}-tab-${it.key}`}
                    aria-controls={`${id}-panel-${it.key}`}
                    aria-disabled={it.disabled || undefined}
                    onClick={() => {
                      if (it.disabled || on) return;
                      setInner(it.key);
                      onChange?.(it.key);
                    }}
                  >
                    {it.label}
                  </div>
                </div>
              );
            })}
            <div class="ant-tabs-ink-bar ant-tabs-ink-bar-animated" style={ink} />
          </div>
        </div>
        {tabBarExtraContent && <div class="ant-tabs-extra-content">{tabBarExtraContent}</div>}
      </div>
      <div class="ant-tabs-content-holder">
        <div class="ant-tabs-content ant-tabs-content-top">
          {items.map((it) => {
            const on = it.key === active;
            if (!on && (destroyOnHidden || !visited.current.has(it.key))) return null;
            return (
              <div role="tabpanel" tabIndex={on ? 0 : -1} aria-hidden={!on} class={cx("ant-tabs-tabpane", on && "ant-tabs-tabpane-active")} id={`${id}-panel-${it.key}`} aria-labelledby={`${id}-tab-${it.key}`} style={on ? undefined : { display: "none" }}>
                {it.children}
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}

/** Upload：只实现「选文件后交给 beforeUpload」这一种用法（旧版就是这样用的，不自动上传） */
export function Upload({ accept, beforeUpload, children, disabled, multiple }: { accept?: string; beforeUpload?: (file: File, list: File[]) => boolean | Promise<unknown> | void; showUploadList?: boolean; maxCount?: number; multiple?: boolean; disabled?: boolean; children?: ComponentChildren }) {
  const ref = useRef<HTMLInputElement>(null);
  return (
    <span class="ant-upload-wrapper">
      <div class={cx("ant-upload", "ant-upload-select", disabled && "ant-upload-disabled")}>
        <span class="ant-upload" role="button" tabIndex={0} onClick={() => !disabled && ref.current?.click()}>
          <input
            ref={ref}
            name="file"
            type="file"
            accept={accept}
            multiple={multiple}
            style={{ display: "none" }}
            onChange={(e) => {
              const input = e.currentTarget as HTMLInputElement;
              const files = Array.from(input.files ?? []);
              for (const f of files) beforeUpload?.(f, files);
              input.value = "";
            }}
          />
          {toChildArray(children)}
        </span>
      </div>
    </span>
  );
}
