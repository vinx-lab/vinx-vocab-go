/**
 * 弹层栈：ESC 只关最上层（Modal / confirm / Drawer / Popconfirm / Popover / Dropdown / Select / DatePicker），
 * 与 antd 一致——在 Modal 里打开的确认框或下拉，按 ESC 只关它自己。
 * 焦点：Modal / confirm / Drawer 打开时把焦点移进去，Tab 在里面循环，关闭后还给打开前的元素。
 */
import type { ComponentChildren } from "preact";
import { useLayoutEffect, useRef } from "preact/hooks";

interface Layer {
  onEsc: () => void;
}
const stack: Layer[] = [];
let installed = false;

function install() {
  if (installed || typeof document === "undefined") return;
  installed = true;
  document.addEventListener(
    "keydown",
    (e) => {
      if (e.key !== "Escape" || e.isComposing) return;
      const top = stack[stack.length - 1];
      if (!top) return;
      e.stopPropagation();
      top.onEsc();
    },
    true,
  );
}

/** 打开期间登记为一层；ESC 时只有最上层的 onEsc 被调用（传 undefined 表示吞掉 ESC 但不关闭） */
export function useLayer(open: boolean, onEsc?: () => void) {
  const ref = useRef(onEsc);
  ref.current = onEsc;
  // layout effect：打开后立即登记，紧接着的 ESC 也交给它
  useLayoutEffect(() => {
    if (!open) return;
    install();
    const layer: Layer = { onEsc: () => ref.current?.() };
    stack.push(layer);
    return () => {
      const i = stack.lastIndexOf(layer);
      if (i >= 0) stack.splice(i, 1);
    };
  }, [open]);
}

/** 当前层数（测试用） */
export const layerCount = () => stack.length;

const FOCUSABLE = 'a[href],button:not([disabled]),input:not([disabled]):not([type="hidden"]),select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"])';

/**
 * 焦点圈：active 变为 true 时记下当前焦点、把焦点移到 [data-focus-start]（没有则第一个可聚焦元素）；
 * Tab / Shift+Tab 在圈内循环；active 变 false 或卸载时把焦点还回去。
 */
export function FocusTrap({ active, children }: { active: boolean; children: ComponentChildren }) {
  const box = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    if (!active) return;
    const before = document.activeElement as HTMLElement | null;
    const el = box.current;
    const start = el?.querySelector<HTMLElement>("[data-focus-start]") ?? el?.querySelector<HTMLElement>(FOCUSABLE);
    start?.focus({ preventScroll: true });
    return () => {
      if (before && before.isConnected && typeof before.focus === "function") before.focus({ preventScroll: true });
    };
  }, [active]);
  return (
    <div
      ref={box}
      style={{ display: "contents" }}
      onKeyDown={(e) => {
        if (!active || e.key !== "Tab" || !box.current) return;
        const items = [...box.current.querySelectorAll<HTMLElement>(FOCUSABLE)].filter((x) => x.getClientRects().length > 0 && !x.hasAttribute("data-focus-start"));
        if (!items.length) {
          e.preventDefault();
          return;
        }
        const first = items[0];
        const last = items[items.length - 1];
        const cur = document.activeElement;
        if (e.shiftKey && (cur === first || !box.current.contains(cur) || (cur as HTMLElement)?.hasAttribute?.("data-focus-start"))) {
          e.preventDefault();
          last.focus();
        } else if (!e.shiftKey && cur === last) {
          e.preventDefault();
          first.focus();
        }
      }}
    >
      {children}
    </div>
  );
}
