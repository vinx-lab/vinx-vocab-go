/** 弹层：Popover、Popconfirm（只在懒加载页面里用到，单独成模块以便跟着页面分块；定位与触发复用 Popup.tsx） */
import type { ComponentChildren } from "preact";
import { useState } from "preact/hooks";
import { Button, type ButtonProps } from "./Button";
import { ExclamationCircleFilled } from "./icons";
import { Positioned, useTrigger, type Placement, type TriggerAction } from "./Popup";
import { Portal } from "./util";

export interface PopoverProps {
  title?: ComponentChildren;
  content?: ComponentChildren;
  placement?: Placement;
  trigger?: TriggerAction;
  open?: boolean;
  onOpenChange?: (o: boolean) => void;
  disabled?: boolean;
  className?: string;
  children: ComponentChildren;
}

export function Popover({ title, content, placement = "top", trigger = "hover", open, onOpenChange, disabled, className, children }: PopoverProps) {
  const t = useTrigger({ open, onOpenChange, trigger: [trigger], disabled });
  return (
    <>
      <span {...t.wrapProps}>{children}</span>
      {t.isOpen && t.anchor && (
        <Portal>
          <Positioned anchor={t.anchor} placement={placement} gap={12} prefix="ant-popover" className={className} popupRef={t.popupRef} {...t.hoverKeep}>
            <div class="ant-popover-arrow" style={placement === "top" || placement === "bottom" ? { left: "var(--arrow-x)" } : undefined} />
            <div class="ant-popover-content">
              <div class="ant-popover-inner" role="tooltip">
                {title && <div class="ant-popover-title">{title}</div>}
                <div class="ant-popover-inner-content">{typeof content === "function" ? (content as () => ComponentChildren)() : content}</div>
              </div>
            </div>
          </Positioned>
        </Portal>
      )}
    </>
  );
}

export interface PopconfirmProps {
  title: ComponentChildren;
  description?: ComponentChildren;
  onConfirm?: (e?: MouseEvent) => unknown;
  onCancel?: (e?: MouseEvent) => void;
  okText?: ComponentChildren;
  cancelText?: ComponentChildren;
  okButtonProps?: Partial<ButtonProps>;
  cancelButtonProps?: Partial<ButtonProps>;
  okType?: ButtonProps["type"];
  disabled?: boolean;
  placement?: Placement;
  icon?: ComponentChildren;
  children: ComponentChildren;
}

export function Popconfirm({ title, description, onConfirm, onCancel, okText = "确定", cancelText = "取消", okButtonProps, cancelButtonProps, okType = "primary", disabled, placement = "top", icon, children }: PopconfirmProps) {
  const [open, setOpen] = useState(false);
  const [loading, setLoading] = useState(false);
  const content = (
    <div class="ant-popconfirm-inner-content">
      <div class="ant-popconfirm-message">
        <span class="ant-popconfirm-message-icon">{icon ?? <ExclamationCircleFilled aria-label="exclamation-circle" />}</span>
        <div class="ant-popconfirm-message-text">
          <div class="ant-popconfirm-title">{title}</div>
          {description && <div class="ant-popconfirm-description">{description}</div>}
        </div>
      </div>
      <div class="ant-popconfirm-buttons">
        <Button
          size="small"
          {...cancelButtonProps}
          onClick={(e) => {
            setOpen(false);
            onCancel?.(e);
          }}
        >
          {cancelText}
        </Button>
        <Button
          type={okType}
          size="small"
          loading={loading}
          {...okButtonProps}
          onClick={async (e) => {
            const r = onConfirm?.(e);
            if (r && typeof (r as Promise<unknown>).then === "function") {
              setLoading(true);
              try {
                await r;
              } catch {
                // 与 antd 一致：确认失败时保持打开，不向外抛
                setLoading(false);
                return;
              }
              setLoading(false);
            }
            setOpen(false);
          }}
        >
          {okText}
        </Button>
      </div>
    </div>
  );
  return (
    <Popover trigger="click" open={open} onOpenChange={setOpen} disabled={disabled} placement={placement} className="ant-popconfirm" content={content}>
      {children}
    </Popover>
  );
}
