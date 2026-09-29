/** 遮罩类：confirm（命令式确认框）、Drawer、message、useApp；Modal 组件在 Modal.tsx（只在懒加载页面里用到） */
import { h, render, type ComponentChildren, type JSX } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import { Button, type ButtonProps } from "./Button";
import { CheckCircleFilled, CloseCircleFilled, CloseOutlined, ExclamationCircleFilled, InfoCircleFilled, LoadingOutlined } from "./icons";
import { FocusTrap, useLayer } from "./layers";
import { cx, nextZ, Portal, useUid } from "./util";

/** 打开期间锁住页面滚动（多个弹窗叠加时计数） */
let locks = 0;
export function useScrollLock(on: boolean) {
  useEffect(() => {
    if (!on) return;
    if (locks++ === 0) document.body.style.overflow = "hidden";
    return () => {
      if (--locks === 0) document.body.style.overflow = "";
    };
  }, [on]);
}
export interface ConfirmConfig {
  title?: ComponentChildren;
  content?: ComponentChildren;
  okText?: ComponentChildren;
  cancelText?: ComponentChildren;
  okButtonProps?: Partial<ButtonProps>;
  cancelButtonProps?: Partial<ButtonProps>;
  okType?: ButtonProps["type"];
  onOk?: () => unknown;
  onCancel?: () => void;
  icon?: ComponentChildren;
  width?: number;
  type?: "confirm" | "info" | "success" | "error" | "warning";
  /** 与 antd 5 一致：默认聚焦「确定」（回车即确认）；"cancel" 聚焦取消，null 不自动聚焦 */
  autoFocusButton?: "ok" | "cancel" | null;
}

function ConfirmDialog({ cfg, close }: { cfg: ConfirmConfig; close: () => void }) {
  const [loading, setLoading] = useState(false);
  const type = cfg.type ?? "confirm";
  const titleId = useUid("confirm");
  const [z] = useState(nextZ);
  useScrollLock(true);
  useLayer(true, () => {
    cfg.onCancel?.();
    close();
  });
  const btns = useRef<HTMLDivElement>(null);
  const autoFocusButton = cfg.autoFocusButton === null ? null : (cfg.autoFocusButton ?? "ok");
  useEffect(() => {
    if (!autoFocusButton) return;
    // antd ActionButton：挂载后在下一个宏任务里 focus（晚于弹层把焦点移进对话框）
    const t = setTimeout(() => {
      const list = btns.current?.querySelectorAll<HTMLButtonElement>("button");
      if (!list?.length) return;
      (autoFocusButton === "cancel" && type === "confirm" ? list[0] : list[list.length - 1]).focus();
    });
    return () => clearTimeout(t);
  }, []);
  const I = type === "success" ? CheckCircleFilled : type === "error" ? CloseCircleFilled : type === "info" ? InfoCircleFilled : ExclamationCircleFilled;
  const ok = async () => {
    const r = cfg.onOk?.();
    if (r && typeof (r as Promise<unknown>).then === "function") {
      setLoading(true);
      try {
        await r;
      } catch {
        setLoading(false);
        return;
      }
    }
    close();
  };
  return (
    <div class="ant-modal-root">
      <div class="ant-modal-mask" style={{ zIndex: z }} />
      <div tabIndex={-1} class="ant-modal-wrap" style={{ zIndex: z }}>
        <FocusTrap active>
        <div role="dialog" aria-labelledby={titleId} aria-modal="true" class={cx("ant-modal", "ant-modal-confirm", `ant-modal-confirm-${type}`)} style={{ width: cfg.width ?? 416 }}>
          <div tabIndex={0} data-focus-start style={{ outline: "none" }}>
            <div class="ant-modal-content">
              <div class="ant-modal-body">
                <div class="ant-modal-confirm-body-wrapper">
                  <div class={cx("ant-modal-confirm-body", cfg.title && "ant-modal-confirm-body-has-title")}>
                    {cfg.icon ?? <I aria-label={type === "success" ? "check-circle" : type === "error" ? "close-circle" : type === "info" ? "info-circle" : "exclamation-circle"} />}
                    <div class="ant-modal-confirm-paragraph">
                      {cfg.title && (
                        <span class="ant-modal-confirm-title" id={titleId}>
                          {cfg.title}
                        </span>
                      )}
                      {cfg.content && <div class="ant-modal-confirm-content">{cfg.content}</div>}
                    </div>
                  </div>
                  <div class="ant-modal-confirm-btns" ref={btns}>
                    {type === "confirm" && (
                      <Button
                        {...cfg.cancelButtonProps}
                        onClick={() => {
                          cfg.onCancel?.();
                          close();
                        }}
                      >
                        {cfg.cancelText ?? "取消"}
                      </Button>
                    )}
                    <Button type={cfg.okType ?? "primary"} loading={loading} {...cfg.okButtonProps} onClick={ok}>
                      {cfg.okText ?? (type === "confirm" ? "确定" : "知道了")}
                    </Button>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>
        </FocusTrap>
      </div>
    </div>
  );
}

/** 命令式确认框：modal.confirm({...})，返回 { destroy } */
export function confirm(cfg: ConfirmConfig) {
  const el = document.createElement("div");
  document.body.appendChild(el);
  const destroy = () => {
    render(null, el);
    el.remove();
  };
  render(h(ConfirmDialog, { cfg, close: destroy }), el);
  return { destroy };
}
export const info = (c: ConfirmConfig) => confirm({ ...c, type: "info" });
export const success = (c: ConfirmConfig) => confirm({ ...c, type: "success" });
export const error = (c: ConfirmConfig) => confirm({ ...c, type: "error" });
export const warning = (c: ConfirmConfig) => confirm({ ...c, type: "warning" });

export interface DrawerProps {
  open?: boolean;
  onClose?: (e?: Event) => void;
  placement?: "left" | "right" | "top" | "bottom";
  width?: number | string;
  height?: number | string;
  title?: ComponentChildren;
  extra?: ComponentChildren;
  footer?: ComponentChildren;
  closable?: boolean;
  maskClosable?: boolean;
  destroyOnHidden?: boolean;
  styles?: { body?: JSX.CSSProperties; header?: JSX.CSSProperties };
  className?: string;
  children?: ComponentChildren;
}

export function Drawer({ open = false, onClose, placement = "right", width = 378, height = 378, title, extra, footer, closable = true, maskClosable = true, destroyOnHidden, styles, className, children }: DrawerProps) {
  const titleId = useUid("drawer");
  const [z, setZ] = useState(0);
  const [everOpen, setEverOpen] = useState(false);
  const [lastOpen, setLastOpen] = useState(false);
  if (open !== lastOpen) {
    setLastOpen(open);
    if (open) {
      setZ(nextZ());
      setEverOpen(true);
    }
  }
  useScrollLock(open);
  useLayer(open, () => onClose?.());
  // 与 antd 一致：关闭后保留内容（隐藏），destroyOnHidden 时卸载
  if (!open && (!everOpen || destroyOnHidden)) return null;
  const horizontal = placement === "left" || placement === "right";
  return (
    <Portal>
      <div class={cx("ant-drawer", `ant-drawer-${placement}`, open && "ant-drawer-open", className)} tabIndex={-1} style={{ zIndex: z, ...(open ? {} : { display: "none" }) }}>
        <div class="ant-drawer-mask" onClick={(e) => maskClosable && onClose?.(e)} />
        <div class="ant-drawer-content-wrapper" style={horizontal ? { width } : { height }}>
          <div class="ant-drawer-content" role="dialog" aria-modal="true" aria-labelledby={title ? titleId : undefined}>
            <FocusTrap active={open}>
              <div tabIndex={0} data-focus-start style={{ width: 0, height: 0, overflow: "hidden", outline: "none", position: "absolute" }} />
              {(title || extra) && (
                <div class="ant-drawer-header" style={styles?.header}>
                  <div class="ant-drawer-header-title">
                    {closable && (
                      <button type="button" class="ant-drawer-close" aria-label="关闭" onClick={(e) => onClose?.(e)}>
                        <CloseOutlined />
                      </button>
                    )}
                    {title && (
                      <div class="ant-drawer-title" id={titleId}>
                        {title}
                      </div>
                    )}
                  </div>
                  {extra && <div class="ant-drawer-extra">{extra}</div>}
                </div>
              )}
              <div class="ant-drawer-body" style={styles?.body}>
                {children}
              </div>
              {footer && <div class="ant-drawer-footer">{footer}</div>}
            </FocusTrap>
          </div>
        </div>
      </div>
    </Portal>
  );
}

// ---------------- message ----------------
type MsgType = "success" | "error" | "info" | "warning" | "loading";
interface Notice {
  id: number;
  key?: string;
  type: MsgType;
  content: ComponentChildren;
}
let notices: Notice[] = [];
let msgRoot: HTMLDivElement | null = null;
let nid = 0;
const MSG_ICON = { success: CheckCircleFilled, error: CloseCircleFilled, info: InfoCircleFilled, warning: ExclamationCircleFilled, loading: LoadingOutlined };
const MSG_LABEL = { success: "check-circle", error: "close-circle", info: "info-circle", warning: "exclamation-circle", loading: "loading" };

function paintMessages() {
  if (!msgRoot) {
    msgRoot = document.createElement("div");
    document.body.appendChild(msgRoot);
  }
  render(
    notices.length ? (
      <div class="ant-message ant-message-top" style={{ left: "50%", transform: "translateX(-50%)", top: 8, zIndex: 2010 }}>
        {notices.map((n) => {
          const I = MSG_ICON[n.type];
          return (
            <div class="ant-message-notice-wrapper" key={n.id}>
              <div class={cx("ant-message-notice", `ant-message-notice-${n.type}`)}>
                <div class="ant-message-notice-content">
                  <div class={cx("ant-message-custom-content", `ant-message-${n.type}`)}>
                    <I aria-label={MSG_LABEL[n.type]} />
                    <span>{n.content}</span>
                  </div>
                </div>
              </div>
            </div>
          );
        })}
      </div>
    ) : null,
    msgRoot,
  );
}

export interface MessageArgs {
  content: ComponentChildren;
  type?: MsgType;
  duration?: number;
  key?: string;
}
function open(args: MessageArgs): (() => void) & Promise<void> {
  const type = args.type ?? "info";
  if (args.key) notices = notices.filter((n) => n.key !== args.key);
  const n: Notice = { id: ++nid, key: args.key, type, content: args.content };
  notices = [...notices, n];
  paintMessages();
  const close = () => {
    notices = notices.filter((x) => x !== n);
    paintMessages();
  };
  const duration = args.duration ?? 3;
  let resolve!: () => void;
  const done = new Promise<void>((r) => (resolve = r));
  if (duration > 0)
    setTimeout(() => {
      close();
      resolve();
    }, duration * 1000);
  return Object.assign(close, { then: done.then.bind(done), catch: done.catch.bind(done), finally: done.finally.bind(done) }) as never;
}
const typed = (type: MsgType) => (content: ComponentChildren | MessageArgs, duration?: number) =>
  content && typeof content === "object" && "content" in (content as object) ? open({ ...(content as MessageArgs), type }) : open({ content: content as ComponentChildren, duration, type });

export const message = {
  open,
  success: typed("success"),
  error: typed("error"),
  info: typed("info"),
  warning: typed("warning"),
  loading: typed("loading"),
  destroy: (key?: string) => {
    notices = key ? notices.filter((n) => n.key !== key) : [];
    paintMessages();
  },
};

export const modal = { confirm, info, success, error, warning };

/** 对应 antd 的 App.useApp()：{ message, modal } */
export function useApp() {
  return { message, modal };
}
