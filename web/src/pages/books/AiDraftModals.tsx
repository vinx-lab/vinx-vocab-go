/**
 * AI 生成句型与仿写（spec 0005 §5、§6）：预览（输入、学段、可见提示词）→ 后台生成 → 草稿逐句编辑 / 删除 / 重写一句 → 保存。
 * 草稿只在服务端任务结果里（内存，1 小时），页面关掉或服务重启后丢失；保存时把（编辑过的）句子提交给
 * POST /ai/units/:id/patterns/save、POST /ai/variants/save，新建一篇句型清单或追加到已有的。
 */
import { useEffect, useRef, useState } from "preact/hooks";
import { Alert, Button, Checkbox, Input, Modal, Radio, Segmented, Space, Tag, Tooltip, Upload, useApp } from "@/ui";
import { DeleteOutlined, ReloadOutlined, ThunderboltOutlined, UploadOutlined } from "@/ui";
import {
  AI_JOB_TTL_MS,
  AI_LEVELS,
  AI_LEVEL_HINT,
  AI_LEVEL_LABEL,
  AI_PROMPT_MAX_LENGTH,
  PATTERN_COUNT_DEFAULT,
  PATTERN_COUNT_MAX,
  PATTERN_COUNT_MIN,
  VARIANT_MODES,
  VARIANT_MODE_HINT,
  VARIANT_MODE_LABEL,
  VARIANT_ORIGIN_LIMIT,
  VARIANT_PASTED_MAX_BYTES,
  VARIANT_PER_ITEM_DEFAULT,
  VARIANT_PER_ITEM_MAX,
  VARIANT_PER_ITEM_MIN,
  type AiJobStarted,
  type AiLevel,
  type AiPatternDraft,
  type AiPatternDraftItem,
  type AiPatternPreview,
  type AiRewriteResult,
  type AiVariantDraft,
  type AiVariantDraftItem,
  type AiVariantPreview,
  type SentenceChecks,
  type VariantMode,
} from "@vinx/shared";
import { api, errorMessage } from "@/lib/api";
import { useAiJob } from "@/lib/useAiJob";
import { applyRewrite, countPastedOrigins, draftProblem, patternSaveBody, rewriteBody, utf8Bytes, variantSaveBody, withKeys, type Keyed } from "@/lib/aiDraft";
import { AiJobProgress, AiPromptPreview } from "@/components/AiPromptPreview";
import { AiChecks, countFlagged } from "@/components/AiChecks";
import type { UnitText } from "@/types";

const TTL_HOURS = Math.round(AI_JOB_TTL_MS / 3_600_000);

// ---------------------------------------------------------------------------
// 共用小件
// ---------------------------------------------------------------------------

/** 学段：只对这一次生成有效 */
export function LevelField({ value, onChange, disabled }: { value: AiLevel | undefined; onChange: (v: AiLevel) => void; disabled?: boolean }) {
  return (
    <div style={{ display: "flex", flexWrap: "wrap", alignItems: "center", gap: 8, marginBottom: 12 }} data-testid="ai-level">
      <span style={{ color: "var(--ink-soft)", fontSize: 13 }}>学段</span>
      <Radio.Group
        optionType="button"
        size="small"
        value={value}
        disabled={disabled}
        onChange={(e) => onChange(e.target.value as AiLevel)}
        options={AI_LEVELS.map((l) => ({ value: l, label: AI_LEVEL_LABEL[l] }))}
      />
      {value && <span style={{ color: "var(--muted)", fontSize: 12 }}>{AI_LEVEL_HINT[value]}（只对这一次生成有效）</span>}
    </div>
  );
}

/** 保存到哪里：新建一篇（标题可改）或追加到本单元已有的句型清单 */
function SaveTargetField({ lists, target, onTarget, title, onTitle }: { lists: UnitText[]; target: string; onTarget: (v: string) => void; title: string; onTitle: (v: string) => void }) {
  return (
    <div data-testid="save-target" style={{ marginTop: 12, paddingTop: 12, borderTop: "1px solid var(--line)" }}>
      <div style={{ color: "var(--ink-soft)", fontSize: 13, marginBottom: 6 }}>保存到</div>
      <Radio.Group
        value={target}
        onChange={(e) => onTarget(String(e.target.value))}
        options={[{ value: "", label: "新建一篇句型清单" }, ...lists.map((t) => ({ value: t.id, label: `追加到「${t.title}」` }))]}
      />
      {target === "" && <Input style={{ marginTop: 8 }} value={title} maxLength={100} onChange={(e) => onTitle(e.target.value)} prefix={<span style={{ color: "var(--muted)" }}>标题</span>} aria-label="标题" />}
    </div>
  );
}

function DraftNotice() {
  return (
    <Alert
      type="info"
      showIcon
      style={{ marginBottom: 12 }}
      message={`草稿只暂存在服务器上 ${TTL_HOURS} 小时，关掉窗口、超时或服务重启后会丢失，请及时保存。`}
      description="标黄的是超纲、太长或结构偏离，只做提示；可以直接改、删掉，或点「重写」让 AI 只重写这一句。"
    />
  );
}

/** 草稿的一行：英文、中文（句型另有骨架；仿写另有说明）、检查标记、重写 / 删除 */
function DraftRow({
  n,
  row,
  frame,
  extra,
  rewriting,
  onPatch,
  onRewrite,
  onRemove,
}: {
  n: number;
  row: { en: string; cn: string; checks: SentenceChecks };
  frame?: { value: string; onChange: (v: string) => void };
  extra?: preact.ComponentChildren;
  rewriting: boolean;
  onPatch: (p: { en?: string; cn?: string }) => void;
  onRewrite: () => void;
  onRemove: () => void;
}) {
  return (
    <div className="vx-draft-row" style={{ display: "flex", gap: 8, padding: "8px 0", borderTop: "1px dashed var(--line)" }}>
      <div className="vx-num" style={{ color: "var(--muted)", fontSize: 12, paddingTop: 6, minWidth: 18, textAlign: "right" }}>
        {n}
      </div>
      <div style={{ flex: 1, minWidth: 0 }}>
        {extra}
        {frame && <Input size="small" value={frame.value} placeholder="句型骨架（可选）" maxLength={1000} style={{ marginBottom: 4 }} onChange={(e) => frame.onChange(e.target.value)} aria-label={`第 ${n} 句骨架`} />}
        <Input className="vx-word" value={row.en} placeholder="英文" maxLength={1000} disabled={rewriting} onChange={(e) => onPatch({ en: e.target.value })} aria-label={`第 ${n} 句英文`} />
        <Input value={row.cn} placeholder="中文" maxLength={1000} disabled={rewriting} style={{ marginTop: 4 }} onChange={(e) => onPatch({ cn: e.target.value })} aria-label={`第 ${n} 句中文`} />
        <AiChecks checks={row.checks} />
      </div>
      <Space direction="vertical" size={2}>
        <Tooltip title="把这句和标出的问题发给 AI，只重写这一句">
          <Button size="small" icon={<ReloadOutlined />} loading={rewriting} onClick={onRewrite} aria-label={`重写第 ${n} 句`} />
        </Tooltip>
        <Button size="small" danger icon={<DeleteOutlined />} disabled={rewriting} onClick={onRemove} aria-label={`删除第 ${n} 句`} />
      </Space>
    </div>
  );
}

/**
 * 草稿的状态与操作：逐句改、删、重写一句（同步调用，最长 60 秒）、保存。
 * kind 决定重写时的类型；save 由调用方给出请求。
 */
function useDraft<T extends { en: string; cn: string; checks: SentenceChecks; originEn?: string }>(kind: "pattern" | "variant", unitId: string) {
  const { message } = useApp();
  const [rows, setRows] = useState<Keyed<T>[] | null>(null);
  const [level, setLevel] = useState<AiLevel>("junior");
  const [rewriting, setRewriting] = useState<string[]>([]);
  const [saving, setSaving] = useState(false);

  const patch = (key: string, p: Partial<T>) => setRows((prev) => prev && prev.map((r) => (r.key === key ? { ...r, ...p } : r)));
  const remove = (key: string) => setRows((prev) => prev && prev.filter((r) => r.key !== key));
  const rewrite = async (row: Keyed<T>) => {
    if (!row.en.trim()) return void message.warning("先填好英文再重写");
    setRewriting((xs) => [...xs, row.key]);
    try {
      const r = await api.post<AiRewriteResult>("/ai/sentences/rewrite", rewriteBody(kind, row, level, unitId));
      setRows((prev) => prev && prev.map((x) => (x.key === row.key ? applyRewrite(x, r) : x)));
      message.success("已重写这一句");
    } catch (e) {
      message.error(errorMessage(e, "重写失败"));
    } finally {
      setRewriting((xs) => xs.filter((k) => k !== row.key));
    }
  };
  const save = async (send: (rows: Keyed<T>[]) => Promise<unknown>, onSaved: () => void) => {
    if (!rows) return;
    const problem = draftProblem(rows);
    if (problem) return void message.warning(problem);
    setSaving(true);
    try {
      await send(rows);
      message.success(`已保存 ${rows.length} 句`);
      onSaved();
    } catch (e) {
      message.error(errorMessage(e, "保存失败"));
    } finally {
      setSaving(false);
    }
  };
  return { rows, setRows, level, setLevel, rewriting, saving, patch, remove, rewrite, save };
}

/** 关窗前确认：有没保存的草稿时提示会丢失 */
function useGuardedClose(hasDraft: boolean, onClose: () => void) {
  const { modal } = useApp();
  return () => {
    if (!hasDraft) return onClose();
    modal.confirm({
      title: "放弃这份草稿？",
      content: "草稿还没保存，关掉后无法找回。",
      okText: "放弃",
      okButtonProps: { danger: true },
      cancelText: "继续编辑",
      onOk: onClose,
    });
  };
}

/** 提示词被手改过时，学段 / 话题变了先确认再覆盖 */
function usePrompt() {
  const { modal } = useApp();
  const [prompt, setPrompt] = useState("");
  const [basePrompt, setBasePrompt] = useState("");
  const ref = useRef({ prompt, basePrompt });
  ref.current = { prompt, basePrompt };
  const apply = (next: string) => {
    const { prompt: cur, basePrompt: base } = ref.current;
    if (cur === base || cur === next) {
      setPrompt(next);
      setBasePrompt(next);
      return;
    }
    modal.confirm({
      title: "替换你改过的提示词？",
      content: "输入或学段变了，提示词已重新拼好。替换后，你在编辑框里的修改会丢失。",
      okText: "替换",
      cancelText: "保留我的修改",
      onOk: () => {
        setPrompt(next);
        setBasePrompt(next);
      },
      onCancel: () => setBasePrompt(next),
    });
  };
  const usable = !!prompt.trim() && prompt.trim().length <= AI_PROMPT_MAX_LENGTH;
  return { prompt, setPrompt, basePrompt, apply, usable };
}

// ---------------------------------------------------------------------------
// 句型
// ---------------------------------------------------------------------------

type PatternRowT = AiPatternDraftItem;

export function AiPatternsModal({ unitId, texts, onClose, onSaved }: { unitId: string; texts: UnitText[]; onClose: () => void; onSaved: () => void }) {
  const [topic, setTopic] = useState("");
  const [count, setCount] = useState(PATTERN_COUNT_DEFAULT);
  const [level, setLevel] = useState<AiLevel | undefined>(undefined);
  const [preview, setPreview] = useState<AiPatternPreview | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const p = usePrompt();
  const job = useAiJob<AiPatternDraft>();
  const draft = useDraft<PatternRowT>("pattern", unitId);
  const [target, setTarget] = useState("");
  const [title, setTitle] = useState("");
  const seq = useRef(0);
  const previewed = useRef<string | null>(null);
  const keyOf = (t: string, c: number, l: AiLevel | undefined) => JSON.stringify([t.trim(), c, l ?? null]);

  const load = async (body: { topic?: string; count: number; level?: AiLevel }) => {
    const my = ++seq.current;
    setLoading(true);
    try {
      const r = await api.post<AiPatternPreview>(`/ai/units/${unitId}/patterns/preview`, body);
      if (my !== seq.current) return;
      setLoadError(null);
      setPreview(r);
      // 话题从单元名预填；学段默认用词书的
      const nextTopic = body.topic ?? r.topic;
      if (body.topic === undefined) setTopic(r.topic);
      setLevel(r.level);
      previewed.current = keyOf(nextTopic, r.count, r.level);
      p.apply(r.prompt);
    } catch (e) {
      if (my === seq.current) setLoadError(errorMessage(e, "预览失败"));
    } finally {
      if (my === seq.current) setLoading(false);
    }
  };

  useEffect(() => {
    void load({ count: PATTERN_COUNT_DEFAULT });
    // 只在打开时取一次；之后随话题、数量、学段变化
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [unitId]);

  useEffect(() => {
    if (previewed.current === null || previewed.current === keyOf(topic, count, level) || !topic.trim()) return;
    const timer = setTimeout(() => void load({ topic: topic.trim(), count, level }), 400);
    return () => clearTimeout(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [topic, count, level]);

  const generate = () =>
    job.start(
      () => api.post<AiJobStarted>(`/ai/units/${unitId}/patterns`, { topic: topic.trim(), count, level, prompt: p.prompt }),
      (r) => {
        draft.setRows(withKeys(r.items));
        draft.setLevel(r.level);
        setTitle(r.title);
      },
    );

  const rows = draft.rows;
  const close = useGuardedClose(!!rows && rows.length > 0, onClose);
  const lists = texts.filter((t) => t.kind === "list");
  const save = () =>
    draft.save(
      (rs) => api.post(`/ai/units/${unitId}/patterns/save`, patternSaveBody(rs, { textId: target || undefined, title })),
      () => {
        onSaved();
        onClose();
      },
    );

  const footer = rows ? (
    <Space wrap style={{ justifyContent: "flex-end" }}>
      <Button onClick={close}>放弃草稿</Button>
      <Button type="primary" data-testid="ai-save" loading={draft.saving} disabled={rows.length === 0 || draft.rewriting.length > 0 || (!target && !title.trim())} onClick={save}>
        保存（{rows.length} 句）
      </Button>
    </Space>
  ) : (
    <Space wrap style={{ justifyContent: "flex-end" }}>
      <Button onClick={onClose}>{job.running ? "关闭（草稿会丢失）" : "取消"}</Button>
      <Button type="primary" data-testid="ai-generate" icon={<ThunderboltOutlined />} loading={job.running} disabled={loading || !topic.trim() || !p.usable} onClick={generate}>
        {job.running ? "生成中…" : "生成"}
      </Button>
    </Space>
  );

  return (
    <Modal open width={760} title="AI 生成句型" onCancel={close} footer={footer} maskClosable={false} destroyOnHidden>
      {rows ? (
        <div data-testid="ai-pattern-draft">
          <DraftNotice />
          {countFlagged(rows) > 0 && <div style={{ color: "var(--accent)", fontSize: 12, marginBottom: 6 }}>{countFlagged(rows)} 句带标记</div>}
          <div style={{ maxHeight: "50vh", overflowY: "auto" }}>
            {rows.map((r, i) => (
              <DraftRow
                key={r.key}
                n={i + 1}
                row={r}
                frame={{ value: r.frame ?? "", onChange: (v) => draft.patch(r.key, { frame: v }) }}
                rewriting={draft.rewriting.includes(r.key)}
                onPatch={(x) => draft.patch(r.key, x)}
                onRewrite={() => void draft.rewrite(r)}
                onRemove={() => draft.remove(r.key)}
              />
            ))}
          </div>
          <SaveTargetField lists={lists} target={target} onTarget={setTarget} title={title} onTitle={setTitle} />
        </div>
      ) : loadError && !preview ? (
        <Alert type="error" showIcon message={loadError} action={<Button size="small" onClick={() => void load({ count })}>重试</Button>} />
      ) : (
        <>
          <Input
            value={topic}
            maxLength={100}
            disabled={job.running}
            onChange={(e) => setTopic(e.target.value)}
            placeholder="例如：学习方法；by doing 结构"
            prefix={<span style={{ color: "var(--muted)" }}>话题或语法点</span>}
            status={!loading && !topic.trim() ? "error" : undefined}
            aria-label="话题或语法点"
            style={{ marginBottom: 10 }}
          />
          <div style={{ display: "flex", flexWrap: "wrap", alignItems: "center", gap: 8, marginBottom: 10 }}>
            <span style={{ color: "var(--ink-soft)", fontSize: 13 }}>生成几句</span>
            <Segmented
              size="small"
              value={count}
              disabled={job.running}
              onChange={(v) => setCount(Number(v))}
              options={Array.from({ length: PATTERN_COUNT_MAX - PATTERN_COUNT_MIN + 1 }, (_, i) => PATTERN_COUNT_MIN + i)}
            />
          </div>
          <LevelField value={level} onChange={setLevel} disabled={job.running} />
          {preview && preview.existing.length > 0 && (
            <div style={{ color: "var(--muted)", fontSize: 12, marginBottom: 8 }}>本单元已有 {preview.existing.length} 句句型，会让 AI 不要重复。</div>
          )}
          {loadError && <Alert type="warning" showIcon style={{ marginBottom: 10 }} message={loadError} />}
          <AiPromptPreview
            words={preview?.words ?? []}
            prompt={p.prompt}
            onPromptChange={p.setPrompt}
            onRestore={() => p.setPrompt(p.basePrompt)}
            dirty={p.prompt !== p.basePrompt}
            loading={loading}
            disabled={job.running}
            emptyText="本单元还没有单词"
          />
          <AiJobProgress state={job.state} />
        </>
      )}
    </Modal>
  );
}

// ---------------------------------------------------------------------------
// 仿写
// ---------------------------------------------------------------------------

type VariantInputs = { unitId: string; sentenceIds?: string[]; pasted?: string; modes: VariantMode[]; perItem: number; vocab: "unit" | "target" };

export function AiVariantsModal({
  unitId,
  texts,
  initialSentenceIds = [],
  onClose,
  onSaved,
}: {
  unitId: string;
  texts: UnitText[];
  initialSentenceIds?: string[];
  onClose: () => void;
  onSaved: () => void;
}) {
  const { message } = useApp();
  // 第 1 步：例句来源与改造方式
  const [ids, setIds] = useState<string[]>(initialSentenceIds);
  const [pasted, setPasted] = useState("");
  const [modes, setModes] = useState<VariantMode[]>(["replace"]);
  const [perItem, setPerItem] = useState(VARIANT_PER_ITEM_DEFAULT);
  const [vocab, setVocab] = useState<"unit" | "target">("unit");
  // 第 2 步：预览（学段、提示词）
  const [inputs, setInputs] = useState<VariantInputs | null>(null);
  const [preview, setPreview] = useState<AiVariantPreview | null>(null);
  const [level, setLevel] = useState<AiLevel | undefined>(undefined);
  const [loading, setLoading] = useState(false);
  const p = usePrompt();
  const job = useAiJob<AiVariantDraft>();
  const draft = useDraft<AiVariantDraftItem>("variant", unitId);
  const [origins, setOrigins] = useState<AiVariantDraft["origins"]>([]);
  const [target, setTarget] = useState("");
  const [title, setTitle] = useState("");
  const seq = useRef(0);

  const pastedCount = countPastedOrigins(pasted);
  const total = ids.length + pastedCount;
  const pastedTooBig = utf8Bytes(pasted) > VARIANT_PASTED_MAX_BYTES;
  const sourceProblem =
    total === 0 ? "请勾选或粘贴至少一个例句" : total > VARIANT_ORIGIN_LIMIT ? `一次最多 ${VARIANT_ORIGIN_LIMIT} 个例句（现在 ${total} 个）` : pastedTooBig ? "粘贴或上传的内容不能超过 20KB" : modes.length === 0 ? "请至少选择一种改造方式" : null;

  const loadPreview = async (body: VariantInputs, lv?: AiLevel) => {
    const my = ++seq.current;
    setLoading(true);
    try {
      const r = await api.post<AiVariantPreview>("/ai/variants/preview", lv ? { ...body, level: lv } : body);
      if (my !== seq.current) return;
      setInputs(body);
      setPreview(r);
      setLevel(r.level);
      p.apply(r.prompt);
    } catch (e) {
      if (my === seq.current) message.error(errorMessage(e, "预览失败"));
    } finally {
      if (my === seq.current) setLoading(false);
    }
  };

  const next = () => {
    if (sourceProblem) return void message.warning(sourceProblem);
    const body: VariantInputs = { unitId, ...(ids.length ? { sentenceIds: ids } : {}), ...(pasted.trim() ? { pasted } : {}), modes, perItem, vocab };
    void loadPreview(body);
  };

  const changeLevel = (lv: AiLevel) => {
    setLevel(lv);
    if (inputs) void loadPreview(inputs, lv);
  };

  const generate = () =>
    inputs &&
    job.start(
      () => api.post<AiJobStarted>("/ai/variants", { ...inputs, level, prompt: p.prompt }),
      (r) => {
        draft.setRows(withKeys(r.items));
        draft.setLevel(r.level);
        setOrigins(r.origins);
        setTitle(r.title);
      },
    );

  const upload = (file: File) => {
    if (file.size > VARIANT_PASTED_MAX_BYTES) {
      message.error("文件不能超过 20KB");
      return false;
    }
    file
      .text()
      .then((t) => setPasted(t))
      .catch(() => message.error("读取文件失败"));
    return false;
  };

  const rows = draft.rows;
  const close = useGuardedClose(!!rows && rows.length > 0, onClose);
  const lists = texts.filter((t) => t.kind === "list");
  const save = () =>
    draft.save(
      (rs) => api.post("/ai/variants/save", variantSaveBody(unitId, rs, { textId: target || undefined, title })),
      () => {
        onSaved();
        onClose();
      },
    );

  const step: "source" | "preview" | "draft" = rows ? "draft" : inputs && preview ? "preview" : "source";

  const footer =
    step === "draft" ? (
      <Space wrap style={{ justifyContent: "flex-end" }}>
        <Button onClick={close}>放弃草稿</Button>
        <Button type="primary" data-testid="ai-save" loading={draft.saving} disabled={rows!.length === 0 || draft.rewriting.length > 0 || (!target && !title.trim())} onClick={save}>
          保存（{rows!.length} 句）
        </Button>
      </Space>
    ) : step === "preview" ? (
      <Space wrap style={{ justifyContent: "flex-end" }}>
        <Button disabled={job.running} onClick={() => setInputs(null)}>
          上一步
        </Button>
        <Button onClick={onClose}>{job.running ? "关闭（草稿会丢失）" : "取消"}</Button>
        <Button type="primary" data-testid="ai-generate" icon={<ThunderboltOutlined />} loading={job.running} disabled={loading || !p.usable} onClick={generate}>
          {job.running ? "生成中…" : "生成"}
        </Button>
      </Space>
    ) : (
      <Space wrap style={{ justifyContent: "flex-end" }}>
        <Button onClick={onClose}>取消</Button>
        <Button type="primary" loading={loading} disabled={!!sourceProblem} onClick={next}>
          下一步
        </Button>
      </Space>
    );

  const allSentences = texts.flatMap((t) => t.sentences.map((s) => ({ s, text: t })));

  return (
    <Modal open width={760} title="AI 仿写" onCancel={close} footer={footer} maskClosable={false} destroyOnHidden>
      {step === "draft" ? (
        <div data-testid="ai-variant-draft">
          <DraftNotice />
          <div style={{ maxHeight: "50vh", overflowY: "auto" }}>
            {origins.map((o, oi) => {
              const mine = rows!.filter((r) => r.origin === oi + 1);
              if (mine.length === 0) return null;
              return (
                <section key={oi} style={{ marginBottom: 10 }}>
                  <div className="vx-word" style={{ fontWeight: 600, fontSize: 14, marginTop: 6 }}>
                    原句 {oi + 1}：{o.en}
                  </div>
                  {mine.map((r) => {
                    const n = rows!.indexOf(r) + 1;
                    return (
                      <DraftRow
                        key={r.key}
                        n={n}
                        row={r}
                        extra={
                          <div style={{ display: "flex", gap: 6, alignItems: "center", marginBottom: 4 }}>
                            {r.change && (
                              <Tag bordered={false} color="blue" style={{ marginInlineEnd: 0 }}>
                                {VARIANT_MODE_LABEL[r.change]}
                              </Tag>
                            )}
                            <Input size="small" value={r.note} placeholder="改了什么（可选）" maxLength={200} onChange={(e) => draft.patch(r.key, { note: e.target.value })} aria-label={`第 ${n} 句说明`} />
                          </div>
                        }
                        rewriting={draft.rewriting.includes(r.key)}
                        onPatch={(x) => draft.patch(r.key, x)}
                        onRewrite={() => void draft.rewrite(r)}
                        onRemove={() => draft.remove(r.key)}
                      />
                    );
                  })}
                </section>
              );
            })}
          </div>
          <SaveTargetField lists={lists} target={target} onTarget={setTarget} title={title} onTitle={setTitle} />
        </div>
      ) : step === "preview" ? (
        <>
          <div style={{ marginBottom: 10 }}>
            <div style={{ color: "var(--ink-soft)", fontSize: 13, marginBottom: 4 }}>这次的例句（{preview!.origins.length}）</div>
            <ol style={{ margin: 0, paddingLeft: 22 }}>
              {preview!.origins.map((o, i) => (
                <li key={i} className="vx-word" style={{ fontSize: 14 }}>
                  {o.en}
                  {o.cn && <span className="vx-cn" style={{ color: "var(--muted)", marginLeft: 8, fontSize: 12 }}>{o.cn}</span>}
                </li>
              ))}
            </ol>
          </div>
          <LevelField value={level} onChange={changeLevel} disabled={job.running} />
          <AiPromptPreview
            words={preview!.words}
            prompt={p.prompt}
            onPromptChange={p.setPrompt}
            onRestore={() => p.setPrompt(p.basePrompt)}
            dirty={p.prompt !== p.basePrompt}
            loading={loading}
            disabled={job.running}
            emptyText="没有替换用的词汇"
          />
          <AiJobProgress state={job.state} />
        </>
      ) : (
        <>
          <div style={{ color: "var(--ink-soft)", fontSize: 13, marginBottom: 6 }}>
            例句：勾选本单元已有的句子，或粘贴、上传（共 1～{VARIANT_ORIGIN_LIMIT} 句）
          </div>
          {allSentences.length > 0 && (
            <div data-testid="variant-sentences" style={{ maxHeight: 200, overflowY: "auto", border: "1px solid var(--line)", borderRadius: 8, padding: "6px 10px", marginBottom: 10 }}>
              {texts
                .filter((t) => t.sentences.length > 0)
                .map((t) => (
                  <div key={t.id} style={{ marginBottom: 6 }}>
                    <div style={{ color: "var(--muted)", fontSize: 12 }}>{t.title}</div>
                    {t.sentences.map((s) => (
                      <div key={s.id}>
                        <Checkbox
                          checked={ids.includes(s.id)}
                          onChange={(e) => setIds((prev) => (e.target.checked ? [...prev, s.id] : prev.filter((x) => x !== s.id)))}
                          aria-label={`选择 ${s.en}`}
                        >
                          <span className="vx-word">{s.en}</span>
                        </Checkbox>
                      </div>
                    ))}
                  </div>
                ))}
            </div>
          )}
          <Input.TextArea
            value={pasted}
            onChange={(e) => setPasted(e.target.value)}
            autoSize={{ minRows: 3, maxRows: 8 }}
            placeholder={"每行一句：英文 | 中文，或者只写英文（中文由 AI 补上）\nI find making word cards useful. | 我发现做单词卡很有用。"}
            aria-label="粘贴例句"
            status={pastedTooBig ? "error" : undefined}
            style={{ fontSize: 13 }}
          />
          <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginTop: 6, marginBottom: 12, gap: 8, flexWrap: "wrap" }}>
            <Upload accept=".txt,text/plain" showUploadList={false} beforeUpload={upload}>
              <Button size="small" icon={<UploadOutlined />}>
                上传 .txt（20KB 以内）
              </Button>
            </Upload>
            <span style={{ color: total > VARIANT_ORIGIN_LIMIT ? "var(--bad)" : "var(--muted)", fontSize: 12 }}>
              已选 {ids.length} 句，粘贴 {pastedCount} 句
            </span>
          </div>

          <div style={{ color: "var(--ink-soft)", fontSize: 13, marginBottom: 4 }}>改造方式（可多选）</div>
          <div style={{ display: "flex", flexDirection: "column", gap: 2, marginBottom: 12 }}>
            {VARIANT_MODES.map((m) => (
              <Checkbox
                key={m}
                checked={modes.includes(m)}
                onChange={(e) => setModes((prev) => VARIANT_MODES.filter((x) => (x === m ? e.target.checked : prev.includes(x))))}
                aria-label={`改造方式 ${VARIANT_MODE_LABEL[m]}`}
              >
                {VARIANT_MODE_LABEL[m]}
                <span style={{ color: "var(--muted)", fontSize: 12, marginLeft: 6 }}>{VARIANT_MODE_HINT[m]}</span>
              </Checkbox>
            ))}
          </div>

          <div style={{ display: "flex", flexWrap: "wrap", alignItems: "center", gap: 8, marginBottom: 12 }}>
            <span style={{ color: "var(--ink-soft)", fontSize: 13 }}>每个例句写几个变式</span>
            <Segmented
              size="small"
              value={perItem}
              onChange={(v) => setPerItem(Number(v))}
              options={Array.from({ length: VARIANT_PER_ITEM_MAX - VARIANT_PER_ITEM_MIN + 1 }, (_, i) => VARIANT_PER_ITEM_MIN + i)}
            />
          </div>
          <div style={{ display: "flex", flexWrap: "wrap", alignItems: "center", gap: 8 }}>
            <span style={{ color: "var(--ink-soft)", fontSize: 13 }}>替换用的词汇</span>
            <Radio.Group
              value={vocab}
              onChange={(e) => setVocab(e.target.value as "unit" | "target")}
              options={[
                { value: "unit", label: "本单元的词" },
                { value: "target", label: "班级目标词书的词" },
              ]}
            />
          </div>
          {sourceProblem && total > 0 && <div style={{ color: "var(--accent)", fontSize: 12, marginTop: 8 }}>{sourceProblem}</div>}
        </>
      )}
    </Modal>
  );
}
