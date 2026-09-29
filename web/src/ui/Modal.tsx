/** 对话框 Modal（与 antd 5 同样的 DOM / class）；Modal.confirm 等静态方法复用 Overlay.tsx 的命令式确认框 */
import type { ComponentChildren, JSX } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import { Button, type ButtonProps } from "./Button";
import { CloseOutlined } from "./icons";
import { FocusTrap, useLayer } from "./layers";
import { confirm, error, info, success, useScrollLock, warning } from "./Overlay";
import { cx, nextZ, Portal, useUid } from "./util";

export interface ModalProps {
  open?: boolean;
  title?: ComponentChildren;
  onOk?: (e?: MouseEvent) => unknown;
  onCancel?: (e?: MouseEvent | KeyboardEvent) => void;
  okText?: ComponentChildren;
  cancelText?: ComponentChildren;
  confirmLoading?: boolean;
  okButtonProps?: Partial<ButtonProps>;
  cancelButtonProps?: Partial<ButtonProps>;
  okType?: ButtonProps["type"];
  /** null 不显示底部；传节点则替换默认按钮 */
  footer?: ComponentChildren | null;
  width?: number | string;
  centered?: boolean;
  closable?: boolean;
  maskClosable?: boolean;
  keyboard?: boolean;
  /** 关闭后卸载内容 */
  destroyOnHidden?: boolean;
  destroyOnClose?: boolean;
  /** 未打开时也渲染（隐藏），让里面的表单实例保持挂载 */
  forceRender?: boolean;
  afterClose?: () => void;
  zIndex?: number;
  className?: string;
  style?: JSX.CSSProperties;
  styles?: { body?: JSX.CSSProperties };
  children?: ComponentChildren;
}

export function Modal({ open = false, title, onOk, onCancel, okText = "确定", cancelText = "取消", confirmLoading, okButtonProps, cancelButtonProps, okType = "primary", footer, width = 520, centered, closable = true, maskClosable = true, keyboard = true, destroyOnHidden, destroyOnClose, forceRender, afterClose, zIndex, className, style, styles, children }: ModalProps) {
  const titleId = useUid("modal");
  const [everOpen, setEverOpen] = useState(open);
  if (open && !everOpen) setEverOpen(true);
  // 层级在打开时分配：后打开的在上面
  const [z, setZ] = useState(0);
  const [lastOpen, setLastOpen] = useState(false);
  if (open !== lastOpen) {
    setLastOpen(open);
    if (open) setZ(zIndex ?? nextZ());
  }
  useScrollLock(open);
  useLayer(open, keyboard ? () => onCancel?.() : undefined);
  const wasOpen = useRef(open);
  useEffect(() => {
    if (wasOpen.current && !open) afterClose?.();
    wasOpen.current = open;
  }, [open]);
  const destroy = destroyOnHidden || destroyOnClose;
  if (!open && (!everOpen || destroy) && !forceRender) return null;
  const foot =
    footer === undefined ? (
      <div class="ant-modal-footer">
        <Button {...cancelButtonProps} onClick={(e) => onCancel?.(e)}>
          {cancelText}
        </Button>
        <Button type={okType} loading={confirmLoading} {...okButtonProps} onClick={(e) => onOk?.(e)}>
          {okText}
        </Button>
      </div>
    ) : footer === null ? null : (
      <div class="ant-modal-footer">{footer}</div>
    );
  return (
    <Portal>
      <div class="ant-modal-root" style={open ? undefined : { display: "none" }}>
        <div class="ant-modal-mask" style={{ zIndex: z }} />
        <div
          tabIndex={-1}
          class={cx("ant-modal-wrap", centered && "ant-modal-centered")}
          style={{ zIndex: z }}
          onMouseDown={(e) => {
            if (maskClosable && e.target === e.currentTarget) onCancel?.(e);
          }}
        >
          <FocusTrap active={open}>
          <div role="dialog" aria-labelledby={title ? titleId : undefined} aria-modal="true" class={cx("ant-modal", className)} style={{ width, ...(style as object) }}>
            <div tabIndex={0} data-focus-start style={{ outline: "none" }}>
              <div class="ant-modal-content">
                {closable && (
                  <button type="button" aria-label="Close" class="ant-modal-close" onClick={(e) => onCancel?.(e)}>
                    <span class="ant-modal-close-x">
                      <CloseOutlined className="ant-modal-close-icon" />
                    </span>
                  </button>
                )}
                {title && (
                  <div class="ant-modal-header">
                    <div class="ant-modal-title" id={titleId}>
                      {title}
                    </div>
                  </div>
                )}
                <div class="ant-modal-body" style={styles?.body}>
                  {(open || everOpen || forceRender) && children}
                </div>
                {foot}
              </div>
            </div>
          </div>
          </FocusTrap>
        </div>
      </div>
    </Portal>
  );
}

Modal.confirm = confirm;
Modal.info = info;
Modal.success = success;
Modal.error = error;
Modal.warning = warning;
