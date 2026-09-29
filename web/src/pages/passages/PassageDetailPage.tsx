import { useMemo, useState } from "preact/hooks";
import { useNavigate, useParams } from "@/lib/router";
import { useMutation, useQuery, useQueryClient } from "@/lib/query";
import { useApp, Button, Card, Popconfirm, Switch, Tag } from "@/ui";
import { ArrowLeftOutlined, DeleteOutlined } from "@/ui";
import { api, errorMessage } from "@/lib/api";
import { ErrorBlock, Loading, PageHeader, SpeakButton, speak } from "@/components/ui";
import type { PassageDetail } from "@/types";

/** 短文阅读：目标词高亮可点读，中文翻译与理解题按需展开 */
export function PassageDetailPage() {
  const { id = "" } = useParams();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const { message } = useApp();
  const [showCn, setShowCn] = useState(false);
  const [showAnswers, setShowAnswers] = useState(false);

  const q = useQuery({ queryKey: ["passages", id], queryFn: () => api.get<PassageDetail>(`/passages/${id}`) });

  const del = useMutation({
    mutationFn: () => api.del(`/passages/${id}`),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["passages"] });
      message.success("已删除");
      navigate("/passages");
    },
    onError: (e) => message.error(errorMessage(e)),
  });

  const paragraphs = useMemo(() => (q.data?.passage ?? "").split(/\n{2,}/).filter(Boolean), [q.data]);
  const cnParagraphs = useMemo(() => (q.data?.passageCn ?? "").split(/\n{2,}/).filter(Boolean), [q.data]);

  if (q.isLoading) return <div className="vx-page"><Loading /></div>;
  if (q.isError || !q.data) return <div className="vx-page"><ErrorBlock error={q.error} onRetry={() => q.refetch()} /></div>;
  const p = q.data;

  /** 把目标词标出来，点一下就朗读 */
  const renderParagraph = (text: string, key: number) => {
    const targets = p.words.map((w) => w.spelling.split(/\s+/)[0]).filter(Boolean);
    if (targets.length === 0) return <p key={key}>{text}</p>;
    const re = new RegExp(`\\b(${targets.map((t) => t.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")).join("|")})(s|es|ed|ing|d)?\\b`, "gi");
    const parts: (string | { word: string; raw: string })[] = [];
    let last = 0;
    for (const m of text.matchAll(re)) {
      const start = m.index ?? 0;
      if (start > last) parts.push(text.slice(last, start));
      parts.push({ word: m[1], raw: m[0] });
      last = start + m[0].length;
    }
    if (last < text.length) parts.push(text.slice(last));
    return (
      <p key={key} className="vx-word" style={{ fontSize: 18, lineHeight: 1.9, margin: "0 0 14px" }}>
        {parts.map((part, i) =>
          typeof part === "string" ? (
            <span key={i}>{part}</span>
          ) : (
            <button
              key={i}
              type="button"
              onClick={() => speak(part.word, p.words.find((w) => w.spelling.toLowerCase().startsWith(part.word.toLowerCase()))?.id)}
              style={{
                border: "none",
                padding: "0 2px",
                background: "var(--primary-soft)",
                borderRadius: 4,
                color: "var(--primary)",
                fontWeight: 600,
                cursor: "pointer",
                fontFamily: "inherit",
                fontSize: "inherit",
              }}
            >
              {part.raw}
            </button>
          ),
        )}
      </p>
    );
  };

  return (
    <div className="vx-page">
      <PageHeader
        eyebrow="AI 巩固短文"
        title={p.title}
        extra={
          <>
            <Button icon={<ArrowLeftOutlined />} onClick={() => navigate("/passages")}>
              返回
            </Button>
            <Popconfirm title="删除这篇短文？" onConfirm={() => del.mutate()} okText="删除" cancelText="取消">
              <Button danger icon={<DeleteOutlined />} loading={del.isPending}>
                删除
              </Button>
            </Popconfirm>
          </>
        }
      >
        {p.titleCn} · {p.words.length} 个目标词 · {new Date(p.createdAt).toLocaleString("zh-CN")}
        {p.model && <span style={{ color: "var(--muted)" }}> · {p.model}</span>}
      </PageHeader>

      <Card
        className="vx-rise"
        style={{ marginBottom: 16 }}
        title="短文"
        extra={
          <span style={{ fontSize: 13 }}>
            中文 <Switch size="small" checked={showCn} onChange={setShowCn} />
          </span>
        }
      >
        {paragraphs.map((t, i) => (
          <div key={i}>
            {renderParagraph(t, i)}
            {showCn && cnParagraphs[i] && (
              <p className="vx-cn" style={{ color: "var(--ink-soft)", margin: "-6px 0 16px", lineHeight: 1.8 }}>
                {cnParagraphs[i]}
              </p>
            )}
          </div>
        ))}
        {showCn && cnParagraphs.length > paragraphs.length && (
          <p className="vx-cn" style={{ color: "var(--ink-soft)" }}>{cnParagraphs.slice(paragraphs.length).join(" ")}</p>
        )}
      </Card>

      {p.questions.length > 0 && (
        <Card
          className="vx-rise"
          style={{ marginBottom: 16 }}
          title="读后理解"
          extra={
            <Button size="small" type="link" onClick={() => setShowAnswers((v) => !v)}>
              {showAnswers ? "隐藏答案" : "显示答案"}
            </Button>
          }
        >
          {p.questions.map((item, i) => (
            <div key={i} style={{ padding: "8px 0", borderTop: i ? "1px dashed var(--line)" : undefined }}>
              <div className="vx-cn" style={{ fontWeight: 600 }}>
                {i + 1}. {item.q}
              </div>
              {showAnswers && <div className="vx-cn" style={{ color: "var(--good)", marginTop: 4 }}>{item.a}</div>}
            </div>
          ))}
        </Card>
      )}

      <Card className="vx-rise" title="目标单词">
        <div style={{ display: "grid", gap: 10 }}>
          {p.words.map((w) => (
            <div key={w.id} style={{ display: "flex", alignItems: "center", gap: 10, flexWrap: "wrap" }}>
              <span className="vx-word" style={{ fontSize: 18, fontWeight: 600, minWidth: 120 }}>
                {w.spelling}
              </span>
              {w.phonetic && <span style={{ color: "var(--muted)" }}>/{w.phonetic}/</span>}
              <span className="vx-cn" style={{ flex: 1, minWidth: 140, color: "var(--ink-soft)" }}>
                {w.definition}
              </span>
              <SpeakButton text={w.spelling} wordId={w.id} size="small" />
            </div>
          ))}
        </div>
        <Tag bordered={false} style={{ marginTop: 14 }}>
          AI 生成内容，仅供巩固练习，如有语言问题请以教材为准
        </Tag>
      </Card>
    </div>
  );
}
