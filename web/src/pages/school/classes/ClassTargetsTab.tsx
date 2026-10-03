import { useApp } from "@/ui";
import { useMutation, useQuery, useQueryClient } from "@/lib/query";
import { api, errorMessage } from "@/lib/api";
import type { Paged, TargetBook } from "@/types";
import { ErrorBlock, Loading } from "@/components/ui";
import { TargetBooksEditor } from "@/pages/coverage/components";

/** 班级目标词书的查询键（概览上方的提示与本标签页共用） */
export const classTargetsKey = (classId: string) => ["classes", classId, "target-books"] as const;

export function useClassTargets(classId: string) {
  return useQuery({ queryKey: classTargetsKey(classId), queryFn: () => api.get<Paged<TargetBook>>(`/classes/${classId}/target-books`) });
}

/** 班级详情「目标词书」（spec 0003）：这一阶段要求覆盖的词书，学生的今日目标进度与记录页按它统计 */
export function ClassTargetsTab({ classId, editable }: { classId: string; editable: boolean }) {
  const { message } = useApp();
  const qc = useQueryClient();
  const q = useClassTargets(classId);
  const save = useMutation({
    mutationFn: (bookIds: string[]) => api.put<Paged<TargetBook>>(`/classes/${classId}/target-books`, { bookIds }),
    onSuccess: async () => {
      message.success("目标词书已保存");
      await qc.invalidateQueries({ queryKey: ["classes", classId] });
    },
    onError: (e) => message.error(errorMessage(e, "保存失败")),
  });

  if (q.isLoading) return <Loading />;
  if (q.error || !q.data) return <ErrorBlock error={q.error} onRetry={() => q.refetch()} />;

  return (
    <div className="vx-card" style={{ padding: 16, maxWidth: 720 }}>
      <h3 className="vx-title" style={{ fontSize: 17, marginBottom: 4 }}>
        目标词书
      </h3>
      <div style={{ fontSize: 13, color: "var(--muted)", marginBottom: 12 }}>
        这一阶段要求覆盖的词书。学生的「今日」目标进度、学习记录和本班概览的「目标覆盖」按这些词书统计（各书的词去重后计入）。顺序就是学生看到的顺序。
      </div>
      <TargetBooksEditor value={q.data.items} readOnly={!editable} saving={save.isPending} onSave={(ids) => save.mutate(ids)} />
    </div>
  );
}
