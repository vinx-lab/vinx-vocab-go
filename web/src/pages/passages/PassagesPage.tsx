import { useEffect, useMemo, useRef, useState } from "preact/hooks";
import { Link, useNavigate } from "@/lib/router";
import { useQuery, useQueryClient } from "@/lib/query";
import { useIdentity } from "@/lib/auth";
import { Alert, useApp, Button, Card, Input, List, Segmented, Select, Spin, Tag } from "@/ui";
import { ReadOutlined, SettingOutlined, ThunderboltOutlined } from "@/ui";
import { api, errorMessage } from "@/lib/api";
import { can } from "@/lib/perms";
import { useAiStatus } from "@/lib/useAiStatus";
import { useAiJob } from "@/lib/useAiJob";
import { AiJobProgress, AiPromptPreview } from "@/components/AiPromptPreview";
import { EmptyBlock, ErrorBlock, Loading, PageHeader } from "@/components/ui";
import type { MemoryWord, Paged, PassageListItem } from "@/types";
import { AI_PROMPT_MAX_LENGTH, type AiJobStarted, type AiPassageJobResult, type AiPreview, type AiPreviewWord, type User } from "@vinx/shared";

type Source = "due" | "difficult" | "recent" | "manual";

const SOURCE_LABEL: Record<Source, string> = { due: "今天到期", difficult: "易错难词", recent: "最近学过", manual: "自己选词" };
const SOURCE_HINT: Record<Source, string> = {
  due: "用今天该复习的词写一篇短文，读完顺手复习一遍。",
  difficult: "专挑反复答错、容易忘的词，放进一个故事里加深印象。",
  recent: "用最近两周新学的词巩固。",
  manual: "自己挑几个词（3–15 个），生成专属短文。",
};

/**
 * AI 巩固短文：用学过 / 易忘 / 指定的单词生成短文。
 * 生成前预览单词和可见提示词（可去掉单词、可改提示词，只管这一次）；生成走后台任务，每 2 秒查一次进度（K41）。
 */
export function PassagesPage() {
  const { ai, status, isLoading: aiLoading } = useAiStatus();
  const { data: identity } = useIdentity();
  const canConfigureAi = can(identity, "system");
  const { message, modal } = useApp();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [source, setSource] = useState<Source>("due");
  const [count, setCount] = useState(8);
  const [topic, setTopic] = useState("");
  const [manualIds, setManualIds] = useState<string[]>([]);
  const [keyword, setKeyword] = useState("");

  const list = useQuery({ queryKey: ["passages"], queryFn: () => api.get<Paged<PassageListItem>>("/passages", { page: 1, limit: 20 }) });

  const words = useQuery({
    queryKey: ["records", "words", "passage-picker", keyword],
    queryFn: () => api.get<Paged<MemoryWord>>("/records/words", { filter: "all", q: keyword || undefined, page: 1, limit: 50 }),
    enabled: source === "manual",
  });

  // ---- 生成前预览（K41）：选好来源 / 词数 / 单词 / 主题后自动取单词和默认提示词 ----
  const [picked, setPicked] = useState<AiPreviewWord[]>([]);
  const [excluded, setExcluded] = useState<string[]>([]);
  const [prompt, setPrompt] = useState("");
  const [basePrompt, setBasePrompt] = useState("");
  const [previewing, setPreviewing] = useState(false);
  const [previewError, setPreviewError] = useState<string | null>(null);
  const promptRef = useRef({ prompt, basePrompt });
  promptRef.current = { prompt, basePrompt };
  const seq = useRef(0);
  const lastPick = useRef<string | null>(null);
  const job = useAiJob<AiPassageJobResult>();

  const pickKey = JSON.stringify({ source, count, manualIds: source === "manual" ? manualIds : [] });
  const shown = useMemo(() => picked.filter((w) => !excluded.includes(w.id)), [picked, excluded]);

  /** 拿到新拼的提示词：编辑框里有手改的内容时先确认再覆盖 */
  const applyPrompt = (next: string) => {
    const { prompt: cur, basePrompt: base } = promptRef.current;
    if (cur === base || cur === next) {
      setPrompt(next);
      setBasePrompt(next);
      return;
    }
    modal.confirm({
      title: "替换你改过的提示词？",
      content: "单词或主题变了，提示词已按新的选择重新拼好。替换后，你在编辑框里的修改会丢失。",
      okText: "替换",
      cancelText: "保留我的修改",
      onOk: () => {
        setPrompt(next);
        setBasePrompt(next);
      },
      onCancel: () => setBasePrompt(next),
    });
  };

  useEffect(() => {
    if (!ai) return;
    const repick = lastPick.current !== pickKey;
    const my = ++seq.current;
    const timer = setTimeout(async () => {
      if (repick && source === "manual" && manualIds.length === 0) {
        lastPick.current = pickKey;
        setPicked([]);
        setExcluded([]);
        return;
      }
      setPreviewing(true);
      try {
        const body = repick
          ? { source, count, topic: topic.trim() || undefined, ...(source === "manual" ? { wordIds: manualIds } : {}) }
          : { source, count, topic: topic.trim() || undefined, wordIds: shown.map((w) => w.id) };
        const r = await api.post<AiPreview>("/ai/passages/preview", body);
        if (my !== seq.current) return;
        setPreviewError(null);
        if (repick) {
          lastPick.current = pickKey;
          setPicked(r.words);
          setExcluded([]);
        }
        applyPrompt(r.prompt);
      } catch (e) {
        if (my === seq.current) setPreviewError(errorMessage(e, "预览失败"));
      } finally {
        if (my === seq.current) setPreviewing(false);
      }
    }, 400);
    return () => clearTimeout(timer);
    // shown 由 picked / excluded 决定，已在依赖里
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ai, pickKey, topic, excluded]);

  const removeWord = (id: string) => {
    if (source === "manual") setManualIds((ids) => ids.filter((x) => x !== id));
    else setExcluded((xs) => [...xs, id]);
  };

  const generate = () =>
    job.start(
      () =>
        api.post<AiJobStarted>("/passages/generate", {
          source,
          count,
          topic: topic.trim() || undefined,
          wordIds: shown.map((w) => w.id),
          prompt,
        }),
      (r) => {
        qc.invalidateQueries({ queryKey: ["passages"] });
        message.success(r.missingWords.length ? `短文已生成（没用上：${r.missingWords.join("、")}）` : "短文已生成");
        navigate(`/passages/${r.id}`);
      },
    );

  const options = useMemo(
    () => (words.data?.items ?? []).map((w) => ({ value: w.wordId, label: `${w.spelling} — ${w.definition}` })),
    [words.data],
  );

  const tooFew = shown.length < 3;
  const pendingPick = lastPick.current !== pickKey;

  if (aiLoading) return <div className="vx-page"><Loading /></div>;

  return (
    <div className="vx-page">
      <PageHeader eyebrow="AI 巩固" title="短文巩固">
        把学过或容易忘的单词放进一篇短文里，读一遍比单独背更容易记住。
      </PageHeader>

      {!ai ? (
        <EmptyBlock
          title="还没有配置 AI 服务"
          description={
            canConfigureAi
              ? "在「系统设置」里填好 AI 接口（本机 Ollama、vLLM，或 DeepSeek 等 OpenAI 兼容服务，也可以用 Claude），保存后立即可用。"
              : "请管理员在「系统设置」里配置 AI 接口，配置好后这里就能用了。"
          }
          action={
            canConfigureAi && (
              <Link to="/admin/settings">
                <Button type="primary" icon={<SettingOutlined />}>
                  去系统设置
                </Button>
              </Link>
            )
          }
        />
      ) : (
        <Card className="vx-rise" style={{ marginBottom: 20 }} title={<><ThunderboltOutlined /> 生成一篇</>}>
          <Segmented
            block
            value={source}
            onChange={(v) => setSource(v as Source)}
            options={(["due", "difficult", "recent", "manual"] as Source[]).map((s) => ({ label: SOURCE_LABEL[s], value: s }))}
          />
          <div style={{ color: "var(--muted)", fontSize: 13, margin: "10px 0 14px" }}>{SOURCE_HINT[source]}</div>

          {source === "manual" ? (
            <Select
              mode="multiple"
              style={{ width: "100%" }}
              value={manualIds}
              onChange={(v) => setManualIds(v.slice(0, 15))}
              onSearch={setKeyword}
              filterOption={false}
              notFoundContent={words.isFetching ? <Spin size="small" /> : "没有匹配的单词"}
              placeholder="搜索并选择 3–15 个学过的单词"
              options={options}
              maxTagCount="responsive"
            />
          ) : (
            <div style={{ display: "flex", alignItems: "center", gap: 10, flexWrap: "wrap" }}>
              <span style={{ color: "var(--ink-soft)" }}>用几个词：</span>
              <Segmented value={count} onChange={(v) => setCount(Number(v))} options={[5, 8, 12, 15]} />
            </div>
          )}

          <Input
            style={{ marginTop: 12 }}
            value={topic}
            onChange={(e) => setTopic(e.target.value)}
            placeholder="主题（可选），例如：校园运动会、周末旅行"
            maxLength={60}
          />

          <div style={{ marginTop: 16 }}>
            <AiPromptPreview
              words={shown}
              onRemoveWord={job.running ? undefined : removeWord}
              prompt={prompt}
              onPromptChange={setPrompt}
              onRestore={() => setPrompt(basePrompt)}
              dirty={prompt !== basePrompt}
              loading={previewing}
              disabled={job.running}
              emptyText={source === "manual" ? "先在上面选几个单词" : "这个来源暂时没有单词，换一个来源试试"}
            />
            {previewError && <Alert type="warning" showIcon style={{ marginTop: 10 }} message={previewError} />}
            {!previewing && !pendingPick && tooFew && shown.length > 0 && (
              <div style={{ color: "var(--accent)", fontSize: 12, marginTop: 8 }}>至少需要 3 个单词{source === "manual" ? "" : "，先去学几个词再来"}</div>
            )}
          </div>

          <Button
            type="primary"
            size="large"
            block
            style={{ marginTop: 14 }}
            loading={job.running}
            disabled={tooFew || !prompt.trim() || prompt.trim().length > AI_PROMPT_MAX_LENGTH || previewing || pendingPick}
            onClick={generate}
          >
            {job.running ? "生成中…" : "生成短文"}
          </Button>
          <AiJobProgress state={job.state} />
          {job.running && (
            <div style={{ color: "var(--muted)", fontSize: 12, marginTop: 6, textAlign: "center" }}>
              模型 {status?.model} 正在写作。可以先离开，生成完会保存在「我的短文」里
            </div>
          )}
        </Card>
      )}

      {list.isLoading ? (
        <Loading />
      ) : list.isError ? (
        <ErrorBlock error={list.error} onRetry={() => list.refetch()} />
      ) : (list.data?.items.length ?? 0) === 0 ? (
        ai ? <EmptyBlock title="还没有生成过短文" description="选一个来源，生成第一篇吧。" /> : null
      ) : (
        <Card title={<><ReadOutlined /> 我的短文</>} styles={{ body: { padding: 0 } }}>
          <List
            dataSource={list.data?.items ?? []}
            renderItem={(p) => (
              <List.Item style={{ padding: "14px 18px" }}>
                <List.Item.Meta
                  title={
                    <Link to={`/passages/${p.id}`} className="vx-word" style={{ fontSize: 16 }}>
                      {p.title}
                    </Link>
                  }
                  description={
                    <span style={{ color: "var(--muted)" }}>
                      {p.titleCn} · {p.wordCount} 个目标词 · {new Date(p.createdAt).toLocaleDateString("zh-CN")}
                    </span>
                  }
                />
                <Tag bordered={false}>阅读</Tag>
              </List.Item>
            )}
          />
        </Card>
      )}
    </div>
  );
}
