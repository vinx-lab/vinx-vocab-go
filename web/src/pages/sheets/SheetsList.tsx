import { useState } from "preact/hooks";
import { Link, useNavigate } from "@/lib/router";
import { useQuery, useQueryClient } from "@/lib/query";
import { useIdentity } from "@/lib/auth";
import { Button, Checkbox, Popconfirm, Tag, useApp } from "@/ui";
import { api, errorMessage } from "@/lib/api";
import { EmptyBlock, ErrorBlock, Loading } from "@/components/ui";
import type { Paged, SheetListItem } from "@/types";
import { printLink } from "./links";
import { gradeLink, sheetStatus } from "./dictation";
import { preloadStudyPage } from "@/pages/study/lazy";

export async function startSheetTest(sheetId: string): Promise<string> {
  const res = await api.post<{ id: string }>("/study/sessions", { kind: "sheet", sheetId });
  return res.id;
}


/** 单词单列表：学生看自己的；老师传 userId 看某个学生的。勾选几份可合并打印 */
export function SheetsList({ userId }: { userId?: string }) {
  const { data: identity } = useIdentity();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const { message } = useApp();
  const [busy, setBusy] = useState<string | null>(null);
  const [picked, setPicked] = useState<string[]>([]);
  const isSelf = !userId || userId === identity?.id;
  const q = useQuery({
    queryKey: ["sheets", userId ?? "me"],
    queryFn: () => api.get<Paged<SheetListItem>>("/sheets", { page: 1, limit: 50, ...(userId ? { userId } : {}) }),
  });

  const test = async (s: SheetListItem) => {
    setBusy(s.id);
    try {
      const [sid] = await Promise.all([startSheetTest(s.id), preloadStudyPage()]);
      navigate(`/study/${sid}`);
    } catch (e) {
      message.info(errorMessage(e));
    } finally {
      setBusy(null);
    }
  };

  const remove = async (s: SheetListItem) => {
    try {
      await api.del(`/sheets/${s.id}`);
      await qc.invalidateQueries({ queryKey: ["sheets"] });
      await qc.invalidateQueries({ queryKey: ["today"] });
    } catch (e) {
      message.error(errorMessage(e));
    }
  };

  if (q.isLoading) return <Loading />;
  if (q.isError || !q.data) return <ErrorBlock error={q.error} onRetry={() => q.refetch()} />;
  if (q.data.items.length === 0)
    return (
      <EmptyBlock
        title="还没有单词单"
        description="系统会挑出最近容易错、容易忘的词，打印成一张纸带去学校看，回家再测。"
        action={
          <Link to={userId ? `/sheets/new?userId=${userId}` : "/sheets/new"}>
            <Button type="primary">生成单词单</Button>
          </Link>
        }
      />
    );

  const toggle = (id: string, on: boolean) => setPicked((p) => (on ? [...p, id] : p.filter((x) => x !== id)));

  return (
    <div style={{ display: "grid", gap: 10 }}>
      <div style={{ display: "flex", alignItems: "center", gap: 12, flexWrap: "wrap" }}>
        <span style={{ color: "var(--muted)", fontSize: 13, flex: 1 }}>勾选几份可以合并打印（每份一页，适合双面打印）</span>
        {picked.length > 0 && <Button type="link" onClick={() => setPicked([])}>取消勾选</Button>}
        <Link to={printLink(picked)} target="_blank" aria-disabled={picked.length === 0} onClick={(e) => picked.length === 0 && e.preventDefault()}>
          <Button disabled={picked.length === 0}>合并打印所选{picked.length > 0 ? `（${picked.length}）` : ""}</Button>
        </Link>
      </div>
      {q.data.items.map((s) => {
        const dict = s.format === "dictation";
        const st = sheetStatus(s.format, s.status);
        // 默写单不能在线测试：待批改 → 批改（学生本人和能查看该学生的老师都可以），已批改 → 成绩单
        const actions = dict ? (
          <Link to={gradeLink(s.id)}>{s.status === "tested" ? <Button type="link">查看成绩</Button> : <Button type="primary">批改</Button>}</Link>
        ) : (
          <>
            {isSelf && s.status !== "tested" && (
              <Button type="primary" loading={busy === s.id} onClick={() => test(s)}>
                {s.status === "testing" ? "继续测试" : "开始测试"}
              </Button>
            )}
            {isSelf && s.status === "tested" && (
              <Button loading={busy === s.id} onClick={() => test(s)}>
                再测一次（练习）
              </Button>
            )}
            {s.firstResult && (
              <Link to={`/study/${s.firstResult.sessionId}`}>
                <Button type="link">查看成绩</Button>
              </Link>
            )}
          </>
        );
        return (
        <div key={s.id} className="vx-card" style={{ padding: "14px 16px", display: "flex", alignItems: "center", gap: 12, flexWrap: "wrap" }}>
          <Checkbox aria-label={`选择单词单 #${s.seq}`} checked={picked.includes(s.id)} onChange={(e) => toggle(s.id, e.target.checked)} />
          <div style={{ flex: 1, minWidth: 200 }}>
            <div style={{ fontWeight: 600 }}>
              {dict ? "默写单" : "单词单"} #{s.seq} <Tag bordered={false}>{dict ? "默写" : "自测"}</Tag>
              <Tag color={st.color} bordered={false}>{st.label}</Tag>
              {dict && s.selfGraded && (
                <Tag color="orange" bordered={false}>
                  自批
                </Tag>
              )}
            </div>
            <div style={{ color: "var(--muted)", fontSize: 13 }}>
              {new Date(s.createdAt).toLocaleDateString()} · {dict ? `${s.itemCount ?? s.wordCount} 题` : `${s.wordCount} 词`} · {s.creatorName} 生成
              {s.firstResult && ` · ${dict ? "成绩" : "首次成绩"} ${s.firstResult.correct}/${s.firstResult.total}`}
            </div>
          </div>
          <Link to={`/sheets/${s.id}/print`} target="_blank">
            <Button>打印</Button>
          </Link>
          {actions}
          {s.status === "pending" && (
            <Popconfirm title={dict ? "删除这份默写单？" : "删除这张单词单？"} onConfirm={() => remove(s)}>
              <Button type="text" danger>
                删除
              </Button>
            </Popconfirm>
          )}
        </div>
        );
      })}
    </div>
  );
}
