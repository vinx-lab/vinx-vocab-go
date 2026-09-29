import { Button, CopyOutlined, Tooltip, useApp } from "@/ui";

/** 复制文本到剪贴板（带失败兜底提示） */
export function useCopy() {
  const { message } = useApp();
  return async (text: string, ok = "已复制") => {
    try {
      await navigator.clipboard.writeText(text);
      message.success(ok);
    } catch {
      message.error("复制失败，请手动选择复制");
    }
  };
}

/** 邀请码展示：等宽大字 + 复制按钮 */
export function InviteCode({ code, size = 18 }: { code: string; size?: number }) {
  const copy = useCopy();
  return (
    <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
      <span style={{ fontFamily: "var(--mono)", fontSize: size, fontWeight: 600, letterSpacing: "0.12em", color: "var(--primary)" }}>{code}</span>
      <Tooltip title="复制邀请码">
        <Button
          size="small"
          type="text"
          aria-label="复制邀请码"
          icon={<CopyOutlined />}
          onClick={(e: MouseEvent) => {
            e.preventDefault();
            e.stopPropagation();
            void copy(code, "邀请码已复制");
          }}
        />
      </Tooltip>
    </span>
  );
}
