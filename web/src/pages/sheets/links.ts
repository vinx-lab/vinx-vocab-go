/** 「用错词再出一张」：老师从学生成绩页进入时，单子要生成给该学生，而不是老师自己 */
export function regenerateLink(session: { id: string; isOwner: boolean; userId: string }): string {
  return `/sheets/new?from=${session.id}${session.isOwner ? "" : `&userId=${session.userId}`}`;
}

/** 合并打印页：多份单子按顺序每份一页 */
export function printLink(ids: readonly string[]): string {
  return `/sheets/print?ids=${ids.join(",")}`;
}

export function parsePrintIds(raw: string | null): string[] {
  return [...new Set((raw ?? "").split(",").map((s) => s.trim()).filter(Boolean))];
}

/** 每份词数预告，与后端 lib/sheet-batch.ts splitSheets 同口径；超出 份数 × 每份词数 返回 null */
export function batchSizes(total: number, copies: number, perSheet: number): number[] | null {
  if (total > copies * perSheet) return null;
  const k = Math.min(copies, total);
  return Array.from({ length: k }, (_, i) => Math.floor(total / k) + (i < total % k ? 1 : 0));
}
