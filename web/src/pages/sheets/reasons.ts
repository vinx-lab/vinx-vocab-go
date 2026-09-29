import type { SheetReason } from "@/types";

export function reasonLabel(r: SheetReason): string {
  switch (r.kind) {
    case "retest": return "上次测错";
    case "wrong": return `错 ${r.count} 次`;
    case "lapse": return `遗忘 ${r.count} 次`;
    case "learning": return "还在学";
    case "consolidating": return "巩固中";
    case "dueSoon": return "快到期";
    case "sessionWrong": return "那次答错";
    case "unlearned": return "没学过";
  }
}

export const REASON_COLOR: Record<SheetReason["kind"], string> = {
  retest: "magenta",
  wrong: "red",
  lapse: "orange",
  learning: "gold",
  consolidating: "blue",
  dueSoon: "cyan",
  sessionWrong: "magenta",
  unlearned: "default",
};
