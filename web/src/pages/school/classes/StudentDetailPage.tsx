import { ArrowLeftOutlined, Button, Tabs } from "@/ui";
import { useQuery } from "@/lib/query";
import { Link, useNavigate, useParams } from "@/lib/router";
import { api } from "@/lib/api";
import type { ClassDetail } from "@/types";
import { EmptyBlock, ErrorBlock, Loading, PageHeader } from "@/components/ui";
import { RecordsView } from "../../records/RecordsView";
import { WordsView } from "../../records/WordsView";
import { SheetsList } from "../../sheets/SheetsList";

/** 老师查看学生：与学生本人的「学习记录」「我的单词」同一套视图 */
export function StudentDetailPage() {
  const { id = "", userId = "" } = useParams();
  const navigate = useNavigate();
  const detail = useQuery({
    queryKey: ["classes", id, "detail"],
    queryFn: () => api.get<ClassDetail>(`/classes/${id}`),
    enabled: !!id,
  });

  const back = (
    <Button icon={<ArrowLeftOutlined />} onClick={() => navigate(`/classes/${id}`)}>
      返回班级
    </Button>
  );

  if (detail.isLoading) return <Loading />;
  if (detail.error || !detail.data)
    return (
      <div className="vx-page">
        <PageHeader title="学生详情" extra={back} />
        <ErrorBlock error={detail.error} onRetry={() => detail.refetch()} />
      </div>
    );

  const student = detail.data.members.find((m) => m.id === userId);
  if (!student)
    return (
      <div className="vx-page">
        <PageHeader eyebrow={detail.data.name} title="学生详情" extra={back} />
        <EmptyBlock title="该学生不在本班" description="可能已被移出班级" />
      </div>
    );

  return (
    <div className="vx-page">
      <PageHeader eyebrow={detail.data.name} title={student.name} extra={back}>
        账号 <span style={{ fontFamily: "var(--mono)" }}>{student.email}</span>
      </PageHeader>
      <Tabs
        destroyOnHidden
        items={[
          { key: "records", label: "学习概况", children: <RecordsView userId={userId} /> },
          { key: "words", label: "单词", children: <WordsView userId={userId} /> },
          {
            key: "sheets",
            label: "单词单",
            children: (
              <>
                <div style={{ marginBottom: 12 }}>
                  <Link to={`/sheets/new?userId=${userId}`}>
                    <Button type="primary">为 {student.name} 生成单词单</Button>
                  </Link>
                </div>
                <SheetsList userId={userId} />
              </>
            ),
          },
        ]}
      />
    </div>
  );
}
