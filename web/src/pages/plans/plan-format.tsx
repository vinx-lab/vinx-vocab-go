import { Tag } from "@/ui";
import type { Mode, Plan, PlanKind, PlanStatus } from "@/types";
import { MODE_LABEL, PLAN_STATUS_LABEL } from "@/types";

/** 计划页共用的文案与小组件 */

export const PLAN_KIND_LABEL: Record<PlanKind, string> = { daily: "每日学习", test: "检测" };

const STATUS_COLOR: Record<PlanStatus, string> = { active: "green", paused: "gold", archived: "default" };

export function PlanKindTag({ kind }: { kind: PlanKind }) {
  return (
    <Tag color={kind === "daily" ? "cyan" : "geekblue"} bordered={false}>
      {PLAN_KIND_LABEL[kind]}
    </Tag>
  );
}

export function PlanStatusTag({ status }: { status: PlanStatus }) {
  return (
    <Tag color={STATUS_COLOR[status]} bordered={false}>
      {PLAN_STATUS_LABEL[status]}
    </Tag>
  );
}

export function modesText(modes: Mode[]): string {
  return modes.map((m) => MODE_LABEL[m]).join(" + ") || "—";
}

export function paceText(plan: Pick<Plan, "kind" | "newPerDay" | "reviewPerDay" | "testSize">): string {
  return plan.kind === "daily" ? `每天 ${plan.newPerDay} 新词 · 复习上限 ${plan.reviewPerDay}` : `检测 ${plan.testSize} 词`;
}

/** 单元按词书分组（保持原顺序） */
export function groupUnitsByBook(units: Plan["units"]): { bookId: string; bookName: string; units: Plan["units"] }[] {
  const groups: { bookId: string; bookName: string; units: Plan["units"] }[] = [];
  for (const u of units) {
    let g = groups.find((x) => x.bookId === u.bookId);
    if (!g) {
      g = { bookId: u.bookId, bookName: u.bookName, units: [] };
      groups.push(g);
    }
    g.units.push(u);
  }
  return groups;
}

/** 单元名摘要：多于 max 个时折叠为“A、B 等 N 个单元” */
export function unitNamesText(names: string[], max = 3): string {
  if (names.length <= max) return names.join("、");
  return `${names.slice(0, max).join("、")} 等 ${names.length} 个单元`;
}

export function dateRangeText(startDate: string | null, endDate: string | null): string {
  if (!startDate && !endDate) return "立即开始 · 长期有效";
  return `${startDate ?? "立即开始"} 至 ${endDate ?? "长期"}`;
}

/** spec 0008 的计划标记：「班级未开放自主安排，暂停中」「不在目标词书内」 */
export function PlanTargetTags({ plan }: { plan: Pick<Plan, "selfPlanPaused" | "outsideTarget" | "status"> }) {
  return (
    <>
      {plan.selfPlanPaused && plan.status === "active" && (
        <span title="所在班级不允许学生自主安排计划，这份自建计划暂停；老师重新允许后自动恢复">
<Tag color="orange" bordered={false}>
          班级未开放自主安排，暂停中
        </Tag>
</span>
      )}
      {plan.outsideTarget && (
        <span title="计划里有单元所在的词书不在你的目标词书内，这部分进度不计入目标">
<Tag bordered={false}>
          不在目标词书内
        </Tag>
</span>
      )}
    </>
  );
}
