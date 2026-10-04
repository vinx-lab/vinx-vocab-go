import { useState } from "preact/hooks";
import { Switch, useApp } from "@/ui";
import { useMutation, useQueryClient } from "@/lib/query";
import { api, errorMessage } from "@/lib/api";
import type { ClassDetail, SelfPlanImpact } from "@/types";
import { SELF_PLAN_LABEL } from "./selfPlan";

/**
 * 班级设置（spec 0008）：允许学生自主安排计划。
 * 从允许改为不允许之前先弹窗说明影响（涉及几个学生、几份自建计划、几本自加的目标词书）。
 */
export function ClassSettingsTab({ detail, editable }: { detail: ClassDetail; editable: boolean }) {
  const { message, modal } = useApp();
  const qc = useQueryClient();
  const [checking, setChecking] = useState(false);

  const save = useMutation({
    mutationFn: (allowSelfPlan: boolean) => api.patch(`/classes/${detail.id}`, { allowSelfPlan }),
    onSuccess: async (_, allow) => {
      message.success(allow ? "已允许学生自主安排计划" : "已关闭学生自主安排计划");
      await qc.invalidateQueries({ queryKey: ["classes"] });
    },
    onError: (e) => message.error(errorMessage(e, "保存失败")),
  });

  const toggle = async (next: boolean) => {
    if (next) {
      save.mutate(true);
      return;
    }
    setChecking(true);
    let impact: SelfPlanImpact;
    try {
      impact = await api.get<SelfPlanImpact>(`/classes/${detail.id}/self-plan-impact`);
    } catch (e) {
      message.error(errorMessage(e, "影响统计加载失败"));
      return;
    } finally {
      setChecking(false);
    }
    modal.confirm({
      title: "关闭学生自主安排？",
      content: (
        <div style={{ display: "grid", gap: 8 }}>
          {impact.students > 0 ? (
            <div>
              涉及 <b className="vx-num">{impact.students}</b> 名学生：<b className="vx-num">{impact.selfPlans}</b> 份自建计划会暂停，
              <b className="vx-num">{impact.ownBooks}</b> 本自己追加的目标词书不再计入目标。
            </div>
          ) : (
            <div>目前没有学生自建计划或追加目标词书。</div>
          )}
          <div style={{ color: "var(--muted)", fontSize: 13 }}>
            不删除任何数据：重新允许后，暂停的计划自动恢复，追加的目标词书恢复生效。关闭后班级概览的「目标覆盖」只按本班目标统计。
          </div>
        </div>
      ),
      okText: "关闭自主安排",
      okButtonProps: { danger: true },
      cancelText: "取消",
      onOk: () => save.mutateAsync(false).catch(() => undefined),
    });
  };

  return (
    <div className="vx-card" style={{ padding: 16, display: "grid", gap: 12 }}>
      <h3 className="vx-title" style={{ fontSize: 17 }}>
        学习安排
      </h3>
      <div style={{ display: "flex", alignItems: "flex-start", gap: 12 }}>
        <Switch
          checked={detail.allowSelfPlan}
          loading={checking || save.isPending}
          disabled={!editable}
          aria-label="允许学生自主安排计划"
          onChange={(v) => void toggle(v)}
        />
        <div style={{ display: "grid", gap: 4 }}>
          <span style={{ fontWeight: 600 }}>{SELF_PLAN_LABEL(detail.allowSelfPlan)}</span>
          <span style={{ fontSize: 13, color: "var(--muted)" }}>
            {detail.allowSelfPlan
              ? "学生可以在班级目标之外追加目标词书，并从目标词书里自己建计划；班级概览按每个学生自己的目标统计。"
              : "学生只学老师布置的计划，自建的计划暂停；班级概览只按本班目标统计，全班同一个分母。"}
          </span>
        </div>
      </div>
      <div style={{ fontSize: 12, color: "var(--muted)" }}>学生同时在几个班时，任一班不允许就按不允许算。</div>
    </div>
  );
}
