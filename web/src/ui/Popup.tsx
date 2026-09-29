/** 弹层：定位与触发（Trigger），Tooltip、Dropdown、Menu；Popover、Popconfirm 在 Popconfirm.tsx */
import { cloneElement, isValidElement, type ComponentChildren, type JSX, type VNode } from "preact";
import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";
import { useLayer } from "./layers";
import { cx, nextZ, Portal } from "./util";

export type Placement = "top" | "topLeft" | "topRight" | "bottom" | "bottomLeft" | "bottomRight";

interface PositionedProps {
  anchor: HTMLElement;
  placement: Placement;
  /** 触发元素与弹层的间距 */
  gap: number;
  /** 例：ant-dropdown → ant-dropdown-placement-bottomLeft */
  prefix: string;
  className?: string;
  style?: JSX.CSSProperties;
  /** 宽度与触发元素一致（Select） */
  matchWidth?: boolean;
  /** 最小宽度为触发元素宽度（Dropdown） */
  minWidth?: boolean;
  popupRef?: (el: HTMLDivElement | null) => void;
  onMouseEnter?: () => void;
  onMouseLeave?: () => void;
  children: ComponentChildren;
}

/** 绝对定位到触发元素旁边；空间不够时上下翻转、左右对齐方式切换并夹在视口内 */
export function Positioned({ anchor, placement, gap, prefix, className, style, matchWidth, minWidth, popupRef, onMouseEnter, onMouseLeave, children }: PositionedProps) {
  const ref = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState<{ left: number; top: number; placement: Placement; arrowX: number } | null>(null);
  const [z] = useState(nextZ);
  useLayoutEffect(() => {
    const compute = () => {
      const el = ref.current;
      if (!el || !anchor.isConnected) return;
      const r = anchor.getBoundingClientRect();
      const w = el.offsetWidth;
      const h = el.offsetHeight;
      const vw = document.documentElement.clientWidth;
      const vh = window.innerHeight;
      let v: "top" | "bottom" = placement.startsWith("top") ? "top" : "bottom";
      const align = placement.replace(/^(top|bottom)/, "") as "" | "Left" | "Right";
      const below = r.bottom + gap;
      const above = r.top - gap - h;
      if (v === "bottom" && below + h > vh && above >= 0) v = "top";
      else if (v === "top" && above < 0 && below + h <= vh) v = "bottom";
      let a = align;
      let x = a === "Left" ? r.left : a === "Right" ? r.right - w : r.left + r.width / 2 - w / 2;
      if (a === "Left" && x + w > vw && r.right - w >= 0) {
        a = "Right";
        x = r.right - w;
      } else if (a === "Right" && x < 0 && r.left + w <= vw) {
        a = "Left";
        x = r.left;
      }
      x = Math.max(0, Math.min(x, vw - w));
      const y = v === "bottom" ? below : above;
      const next = { left: Math.round(x + window.scrollX), top: v === "bottom" ? Math.floor(y + window.scrollY) : Math.ceil(y + window.scrollY), placement: (v + a) as Placement, arrowX: r.left + r.width / 2 - x };
      setPos((p) => (p && p.left === next.left && p.top === next.top && p.placement === next.placement ? p : next));
    };
    compute();
    const ro = typeof ResizeObserver !== "undefined" ? new ResizeObserver(compute) : null;
    if (ref.current) ro?.observe(ref.current);
    window.addEventListener("scroll", compute, true);
    window.addEventListener("resize", compute);
    return () => {
      ro?.disconnect();
      window.removeEventListener("scroll", compute, true);
      window.removeEventListener("resize", compute);
    };
  }, [anchor, placement, gap]);
  const aw = anchor.getBoundingClientRect().width;
  return (
    <div
      ref={(el) => {
        ref.current = el;
        popupRef?.(el);
      }}
      class={cx(prefix, `${prefix}-placement-${pos?.placement ?? placement}`, className)}
      style={{
        position: "absolute",
        left: pos?.left ?? 0,
        top: pos?.top ?? 0,
        zIndex: z,
        boxSizing: "border-box",
        visibility: pos ? undefined : "hidden",
        "--arrow-x": `${pos?.arrowX ?? 0}px`,
        ...(matchWidth ? { width: aw } : minWidth ? { minWidth: aw } : {}),
        ...(style as object),
      }}
      onMouseEnter={onMouseEnter}
      onMouseLeave={onMouseLeave}
    >
      {children}
    </div>
  );
}

export type TriggerAction = "hover" | "click" | "focus";

/**
 * 包住触发元素（display:contents，不影响布局），管理打开状态。
 * 受控：传 open + onOpenChange；非受控：内部状态。
 */
export function useTrigger({ open, onOpenChange, trigger, disabled, esc = true }: { open?: boolean; onOpenChange?: (o: boolean) => void; trigger: TriggerAction[]; disabled?: boolean; /** 是否登记为弹层（ESC 关闭）；Tooltip 不登记 */ esc?: boolean }) {
  const [inner, setInner] = useState(false);
  const isOpen = !disabled && (open ?? inner);
  const wrap = useRef<HTMLSpanElement>(null);
  const popup = useRef<HTMLDivElement | null>(null);
  const [anchor, setAnchor] = useState<HTMLElement | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout>>();
  const set = (o: boolean) => {
    if (disabled && o) return;
    clearTimeout(timer.current);
    if (open === undefined) setInner(o);
    onOpenChange?.(o);
  };
  const setLater = (o: boolean, ms: number) => {
    clearTimeout(timer.current);
    timer.current = setTimeout(() => set(o), ms);
  };
  useLayoutEffect(() => {
    const el = (wrap.current?.firstElementChild as HTMLElement | null) ?? null;
    if (el !== anchor) setAnchor(el);
  });
  useEffect(() => {
    if (!anchor) return;
    const offs: (() => void)[] = [];
    const on = (type: string, fn: (e: Event) => void) => {
      anchor.addEventListener(type, fn);
      offs.push(() => anchor.removeEventListener(type, fn));
    };
    if (trigger.includes("hover")) {
      on("mouseenter", () => setLater(true, 100));
      on("mouseleave", () => setLater(false, 100));
    }
    if (trigger.includes("focus")) {
      on("focusin", () => set(true));
      on("focusout", () => set(false));
    }
    if (trigger.includes("click")) on("click", () => set(!isOpenRef.current));
    return () => offs.forEach((f) => f());
  }, [anchor, trigger.join()]);
  const isOpenRef = useRef(isOpen);
  isOpenRef.current = isOpen;
  // 点外面关闭
  useEffect(() => {
    if (!isOpen) return;
    const onDown = (e: Event) => {
      const t = e.target as Node;
      if (anchor?.contains(t) || popup.current?.contains(t)) return;
      set(false);
    };
    document.addEventListener("mousedown", onDown, true);
    document.addEventListener("touchstart", onDown, true);
    return () => {
      document.removeEventListener("mousedown", onDown, true);
      document.removeEventListener("touchstart", onDown, true);
    };
  }, [isOpen, anchor]);
  // ESC 只关最上层
  useLayer(esc && isOpen, () => set(false));
  useEffect(() => () => clearTimeout(timer.current), []);
  return {
    isOpen,
    set,
    anchor,
    wrapProps: { ref: wrap, style: { display: "contents" } },
    popupRef: (el: HTMLDivElement | null) => (popup.current = el),
    hoverKeep: trigger.includes("hover") ? { onMouseEnter: () => clearTimeout(timer.current), onMouseLeave: () => setLater(false, 100) } : {},
  };
}

export interface TooltipProps {
  title?: ComponentChildren;
  placement?: Placement;
  open?: boolean;
  onOpenChange?: (o: boolean) => void;
  children: ComponentChildren;
}

export function Tooltip({ title, placement = "top", open, onOpenChange, children }: TooltipProps) {
  const t = useTrigger({ open, onOpenChange, trigger: ["hover", "focus"], disabled: title === undefined || title === null || title === "", esc: false });
  return (
    <>
      <span {...t.wrapProps}>{children}</span>
      {t.isOpen && t.anchor && (
        <Portal>
          <Positioned anchor={t.anchor} placement={placement} gap={12} prefix="ant-tooltip" popupRef={t.popupRef} {...t.hoverKeep}>
            <div class="ant-tooltip-arrow" style={placement === "top" || placement === "bottom" ? { left: "var(--arrow-x)" } : undefined} />
            <div class="ant-tooltip-content">
              <div class="ant-tooltip-inner" role="tooltip">
                {title}
              </div>
            </div>
          </Positioned>
        </Portal>
      )}
    </>
  );
}

export interface MenuItem {
  key?: string;
  label?: ComponentChildren;
  icon?: ComponentChildren;
  danger?: boolean;
  disabled?: boolean;
  type?: "divider" | "group";
  children?: MenuItem[];
  title?: string;
}

function renderItems(p: string, items: (MenuItem | null | false | undefined)[], selected: string[], onClick: (key: string, e: MouseEvent) => void, inline: boolean): ComponentChildren {
  return items.filter(Boolean).map((raw) => {
    const it = raw as MenuItem;
    if (it.type === "divider") return <li role="separator" class={`${p}-item-divider`} />;
    if (it.type === "group")
      return (
        <li role="presentation" class={`${p}-item-group`}>
          <div role="presentation" class={`${p}-item-group-title`} title={typeof it.label === "string" ? it.label : undefined}>
            {it.label}
          </div>
          <ul role="group" class={`${p}-item-group-list`}>
            {renderItems(p, it.children ?? [], selected, onClick, inline)}
          </ul>
        </li>
      );
    const sel = it.key !== undefined && selected.includes(it.key);
    return (
      <li
        class={cx(`${p}-item`, sel && `${p}-item-selected`, it.danger && `${p}-item-danger`, it.disabled && `${p}-item-disabled`, !it.icon && `${p}-item-only-child`)}
        role="menuitem"
        tabIndex={it.disabled ? undefined : -1}
        aria-disabled={it.disabled || undefined}
        data-menu-id={it.key}
        style={inline ? { paddingLeft: 24 } : undefined}
        onClick={(e) => !it.disabled && it.key !== undefined && onClick(it.key, e)}
      >
        {it.icon && withCls(it.icon, `${p}-item-icon`)}
        <span class={`${p}-title-content`}>{it.label}</span>
      </li>
    );
  });
}
/** 图标元素加上 xxx-item-icon class（与 antd 一致，样式依赖它） */
function withCls(icon: ComponentChildren, cls: string): ComponentChildren {
  if (isValidElement(icon)) {
    const v = icon as VNode<{ className?: string }>;
    return cloneElement(v, { className: cx(v.props.className, cls) });
  }
  return icon;
}

export interface MenuProps {
  items: (MenuItem | null | false | undefined)[];
  mode?: "inline" | "vertical";
  selectedKeys?: string[];
  onClick?: (info: { key: string; domEvent: MouseEvent }) => void;
  className?: string;
  style?: JSX.CSSProperties;
}

/** 侧栏菜单（antd Menu inline 模式） */
export function Menu({ items, mode = "inline", selectedKeys = [], onClick, className, style }: MenuProps) {
  return (
    <ul class={cx("ant-menu", "ant-menu-root", `ant-menu-${mode}`, "ant-menu-light", className)} role="menu" tabIndex={0} data-menu-list="true" style={style}>
      {renderItems("ant-menu", items, selectedKeys, (key, domEvent) => onClick?.({ key, domEvent }), mode === "inline")}
    </ul>
  );
}

export interface DropdownProps {
  menu: { items: (MenuItem | null | false | undefined)[]; onClick?: (info: { key: string; domEvent: MouseEvent }) => void; selectedKeys?: string[] };
  trigger?: ("click" | "hover")[];
  placement?: Placement;
  open?: boolean;
  onOpenChange?: (o: boolean) => void;
  disabled?: boolean;
  children: ComponentChildren;
}

export function Dropdown({ menu, trigger = ["hover"], placement = "bottomLeft", open, onOpenChange, disabled, children }: DropdownProps) {
  const t = useTrigger({ open, onOpenChange, trigger, disabled });
  return (
    <>
      <span {...t.wrapProps}>{children}</span>
      {t.isOpen && t.anchor && (
        <Portal>
          <Positioned anchor={t.anchor} placement={placement} gap={4} prefix="ant-dropdown" minWidth popupRef={t.popupRef} {...t.hoverKeep}>
            <ul class="ant-dropdown-menu ant-dropdown-menu-root ant-dropdown-menu-vertical ant-dropdown-menu-light" role="menu" tabIndex={0} data-menu-list="true">
              {renderItems("ant-dropdown-menu", menu.items, menu.selectedKeys ?? [], (key, domEvent) => {
                t.set(false);
                menu.onClick?.({ key, domEvent });
              }, false)}
            </ul>
          </Positioned>
        </Portal>
      )}
    </>
  );
}
