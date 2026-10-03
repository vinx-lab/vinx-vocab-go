/**
 * 单元页面的「句型」「课文」标签（spec 0004 §9）：
 * - 句型（list）逐条显示英文、中文、句型骨架；课文（text）按段落显示，点一句看译文和逐词释义。
 * - 有编辑权限时可以新建、编辑（逐条改、拖动排序、删除）、删除、调整顺序、粘贴导入；编辑框下方实时显示超纲词和词库外的词。
 * - 学生只读，可以点读。
 */
import { useEffect, useRef, useState } from "preact/hooks";
import { useMutation, useQueryClient } from "@/lib/query";
import { Alert, Button, Checkbox, Empty, Input, Modal, Popconfirm, Space, Tag, Tooltip, useApp } from "@/ui";
import { DownOutlined, UpOutlined, DeleteOutlined, EditOutlined, HolderOutlined, ImportOutlined, PlusOutlined } from "@/ui";
import { api, errorMessage } from "@/lib/api";
import { DEFAULT_TITLE, KIND_LABEL, editRowsFromSentences, editRowsToInputs, mergeKindOrder, moveItem, previewTextsPayload, type EditPreviewText, type EditRow } from "@/lib/sentences";
import type { Sentence, SentenceAnalysis, TextKind, TextsPreview, UnitText } from "@/types";
import { SentenceGloss, SentenceText, TextReader } from "@/components/SentenceView";
import { speak } from "@/components/ui";
import { ScopeHints, TEXTS_SAMPLE, TextsPreviewList, toEditTexts } from "./TextsPreview";

const NEW_LABEL: Record<TextKind, string> = { list: "新建句型清单", text: "新建课文" };

export function UnitTextsPanel({ unitId, kind, editable, texts }: { unitId: string; kind: TextKind; editable: boolean; texts: UnitText[] }) {
  const { message } = useApp();
  const queryClient = useQueryClient();
  const [editing, setEditing] = useState<UnitText | "new" | null>(null);
  const [importing, setImporting] = useState(false);
  const mine = texts.filter((t) => t.kind === kind);

  const refresh = () => queryClient.invalidateQueries({ queryKey: ["books", "unitTexts", unitId] });

  const remove = useMutation({
    mutationFn: (id: string) => api.del(`/texts/${id}`),
    onSuccess: () => {
      refresh();
      message.success("已删除");
    },
    onError: (e) => message.error(errorMessage(e)),
  });

  const reorder = useMutation({
    mutationFn: (textIds: string[]) => api.patch(`/units/${unitId}/texts/order`, { textIds }),
    onSuccess: refresh,
    onError: (e) => {
      message.error(errorMessage(e));
      refresh();
    },
  });

  const move = (from: number, to: number) => {
    const ids = moveItem(
      mine.map((t) => t.id),
      from,
      to,
    );
    reorder.mutate(mergeKindOrder(texts, kind, ids));
  };

  return (
    <div className="vx-unit-texts" data-kind={kind}>
      {editable && (
        <Space wrap style={{ marginBottom: 12 }}>
          <Button type="primary" ghost icon={<PlusOutlined />} onClick={() => setEditing("new")}>
            {NEW_LABEL[kind]}
          </Button>
          <Button icon={<ImportOutlined />} onClick={() => setImporting(true)}>
            粘贴导入{KIND_LABEL[kind]}
          </Button>
        </Space>
      )}
      {mine.length === 0 ? (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={`这个单元还没有${KIND_LABEL[kind]}`} />
      ) : (
        mine.map((t, i) => (
          <section key={t.id} className="vx-unit-text" style={{ borderTop: i ? "1px solid var(--line)" : undefined, padding: "12px 0" }}>
            <div style={{ display: "flex", gap: 8, alignItems: "flex-start", justifyContent: "space-between", flexWrap: "wrap", marginBottom: 8 }}>
              <div style={{ minWidth: 0 }}>
                <h3 className="vx-word" style={{ margin: 0, fontSize: 18, fontWeight: 600 }}>
                  {t.title}
                </h3>
                {t.titleCn && <div className="vx-cn" style={{ color: "var(--muted)", fontSize: 13 }}>{t.titleCn}</div>}
              </div>
              {editable && (
                <Space size={4}>
                  <Tooltip title="上移">
                    <Button size="small" icon={<UpOutlined />} aria-label={`上移 ${t.title}`} disabled={i === 0 || reorder.isPending} onClick={() => move(i, i - 1)} />
                  </Tooltip>
                  <Tooltip title="下移">
                    <Button size="small" icon={<DownOutlined />} aria-label={`下移 ${t.title}`} disabled={i === mine.length - 1 || reorder.isPending} onClick={() => move(i, i + 1)} />
                  </Tooltip>
                  <Button size="small" icon={<EditOutlined />} onClick={() => setEditing(t)}>
                    编辑
                  </Button>
                  <Popconfirm title={`删除「${t.title}」？`} description="只被这一篇用到的句子会一起删除。" okText="删除" okButtonProps={{ danger: true }} cancelText="取消" onConfirm={() => remove.mutateAsync(t.id).catch(() => undefined)}>
                    <Button size="small" danger icon={<DeleteOutlined />} aria-label={`删除 ${t.title}`} />
                  </Popconfirm>
                </Space>
              )}
            </div>
            {t.kind === "list" ? <PatternList sentences={t.sentences} /> : <TextReader sentences={t.sentences} />}
          </section>
        ))
      )}
      {editing && <TextEditorModal unitId={unitId} kind={kind} text={editing === "new" ? null : editing} onClose={() => setEditing(null)} onSaved={refresh} />}
      {importing && <PasteImportModal unitId={unitId} kind={kind} onClose={() => setImporting(false)} onDone={refresh} />}
    </div>
  );
}

/** 句型清单：一条一条显示，中文常显；点一条看逐词释义 */
function PatternList({ sentences }: { sentences: Sentence[] }) {
  const [open, setOpen] = useState<string | null>(null);
  if (sentences.length === 0) return <div style={{ color: "var(--muted)" }}>还没有句子</div>;
  return (
    <ol style={{ margin: 0, paddingLeft: 22 }}>
      {sentences.map((s) => (
        <li key={s.id} style={{ padding: "6px 0" }}>
          <div role="button" tabIndex={0} style={{ cursor: "pointer" }} onClick={() => setOpen((o) => (o === s.id ? null : s.id))} onKeyDown={(e) => e.key === "Enter" && setOpen((o) => (o === s.id ? null : s.id))}>
            {s.frame && (
              <div style={{ fontSize: 13, color: "var(--accent)", fontFamily: "var(--serif-en)" }}>
                <Tag bordered={false} color="gold" style={{ marginRight: 6 }}>
                  句型
                </Tag>
                {s.frame}
              </div>
            )}
            <div className="vx-example-en" style={{ fontSize: 17 }}>
              <SentenceText en={s.en} words={s.words} onWord={(id, text) => speak(text, id)} />
            </div>
            <div className="vx-example-cn is-compact" style={{ color: "var(--ink-soft)" }}>
              {s.cn}
            </div>
          </div>
          {open === s.id && <SentenceGloss sentence={s} />}
        </li>
      ))}
    </ol>
  );
}

let rowSeq = 0;
const newRow = (): EditRow => ({ key: `new-${++rowSeq}`, en: "", cn: "", frame: "", newParagraph: false });

/** 新建或编辑一篇：标题 + 句子逐条编辑（拖动 / 上下移排序、删除），英文框下方实时检查超纲词 */
function TextEditorModal({ unitId, kind, text, onClose, onSaved }: { unitId: string; kind: TextKind; text: UnitText | null; onClose: () => void; onSaved: () => void }) {
  const { message } = useApp();
  const [title, setTitle] = useState(text?.title ?? DEFAULT_TITLE[kind]);
  const [titleCn, setTitleCn] = useState(text?.titleCn ?? "");
  const [rows, setRows] = useState<EditRow[]>(() => (text && text.sentences.length ? editRowsFromSentences(text.sentences) : [newRow()]));
  const [focus, setFocus] = useState<string | null>(null);
  const [dragKey, setDragKey] = useState<string | null>(null);
  const [tried, setTried] = useState(false);
  const realKind = text?.kind ?? kind;

  const patchRow = (key: string, patch: Partial<EditRow>) => setRows((prev) => prev.map((r) => (r.key === key ? { ...r, ...patch } : r)));

  const save = useMutation({
    mutationFn: async () => {
      const sentences = editRowsToInputs(rows, realKind);
      const t = title.trim();
      const tc = titleCn.trim() || null;
      if (!text) {
        await api.post(`/units/${unitId}/texts`, { title: t, titleCn: tc, kind: realKind, sentences });
        return;
      }
      if (t !== text.title || tc !== (text.titleCn ?? null)) await api.patch(`/texts/${text.id}`, { title: t, titleCn: tc });
      await api.put(`/texts/${text.id}/sentences`, { sentences });
    },
    onSuccess: () => {
      message.success("已保存");
      onSaved();
      onClose();
    },
    onError: (e) => message.error(errorMessage(e)),
  });

  const submit = () => {
    setTried(true);
    if (!title.trim()) return message.warning("请填写标题");
    if (rows.some((r) => !r.en.trim() || !r.cn.trim())) return message.warning("每一句都要有英文和中文，空行请删除");
    save.mutate();
  };

  const dropOn = (key: string) => {
    if (!dragKey || dragKey === key) return;
    setRows((prev) =>
      moveItem(
        prev,
        prev.findIndex((r) => r.key === dragKey),
        prev.findIndex((r) => r.key === key),
      ),
    );
    setDragKey(null);
  };

  return (
    <Modal
      open
      width={760}
      title={text ? `编辑${KIND_LABEL[realKind]}：${text.title}` : NEW_LABEL[kind]}
      okText="保存"
      cancelText="取消"
      confirmLoading={save.isPending}
      onOk={submit}
      onCancel={onClose}
      maskClosable={false}
      destroyOnHidden
    >
      <div style={{ display: "flex", gap: 8, flexWrap: "wrap", marginBottom: 12 }}>
        <Input value={title} maxLength={100} placeholder="标题" status={tried && !title.trim() ? "error" : undefined} onChange={(e) => setTitle(e.target.value)} prefix={<span style={{ color: "var(--muted)" }}>标题</span>} style={{ flex: "1 1 240px" }} />
        <Input value={titleCn} maxLength={100} placeholder="可选" onChange={(e) => setTitleCn(e.target.value)} prefix={<span style={{ color: "var(--muted)" }}>中文标题</span>} style={{ flex: "1 1 200px" }} />
      </div>
      {realKind === "list" && <Alert type="info" showIcon style={{ marginBottom: 10 }} message="句型骨架可选，如 I find ... useful.；英文填完整例句，默写时考完整句。" />}
      <div style={{ maxHeight: "55vh", overflowY: "auto" }}>
        {rows.map((r, i) => (
          <div
            key={r.key}
            className="vx-sentence-row"
            onDragOver={(e) => e.preventDefault()}
            onDrop={() => dropOn(r.key)}
            style={{
              display: "flex",
              gap: 8,
              padding: "8px 0",
              borderTop: realKind === "text" && i > 0 && r.newParagraph ? "2px solid var(--line)" : "1px dashed var(--line)",
              opacity: dragKey === r.key ? 0.5 : 1,
            }}
          >
            <div draggable onDragStart={() => setDragKey(r.key)} onDragEnd={() => setDragKey(null)} style={{ cursor: "grab", paddingTop: 6, color: "var(--muted)" }} aria-label="拖动排序">
              <HolderOutlined />
              <div className="vx-num" style={{ fontSize: 11, textAlign: "center" }}>
                {i + 1}
              </div>
            </div>
            <div style={{ flex: 1, minWidth: 0 }}>
              {realKind === "list" && <Input size="small" value={r.frame} placeholder="句型骨架（可选）" maxLength={1000} style={{ marginBottom: 4 }} onChange={(e) => patchRow(r.key, { frame: e.target.value })} />}
              <Input
                className="vx-word"
                value={r.en}
                placeholder="英文"
                maxLength={1000}
                status={tried && !r.en.trim() ? "error" : undefined}
                onChange={(e) => patchRow(r.key, { en: e.target.value })}
                onFocus={() => setFocus(r.key)}
                aria-label={`第 ${i + 1} 句英文`}
              />
              <Input value={r.cn} placeholder="中文" maxLength={1000} style={{ marginTop: 4 }} status={tried && !r.cn.trim() ? "error" : undefined} onChange={(e) => patchRow(r.key, { cn: e.target.value })} aria-label={`第 ${i + 1} 句中文`} />
              {focus === r.key && <LiveAnalysis unitId={unitId} en={r.en} />}
              {realKind === "text" && i > 0 && (
                <Checkbox checked={r.newParagraph} onChange={(e) => patchRow(r.key, { newParagraph: e.target.checked })} style={{ marginTop: 4, fontSize: 12 }}>
                  从这一句起另起一段
                </Checkbox>
              )}
            </div>
            <Space direction="vertical" size={2}>
              <Button size="small" icon={<UpOutlined />} aria-label="上移" disabled={i === 0} onClick={() => setRows((p) => moveItem(p, i, i - 1))} />
              <Button size="small" icon={<DownOutlined />} aria-label="下移" disabled={i === rows.length - 1} onClick={() => setRows((p) => moveItem(p, i, i + 1))} />
              <Button size="small" danger icon={<DeleteOutlined />} aria-label="删除这一句" onClick={() => setRows((p) => p.filter((x) => x.key !== r.key))} />
            </Space>
          </div>
        ))}
      </div>
      <Button
        type="dashed"
        block
        icon={<PlusOutlined />}
        style={{ marginTop: 8 }}
        onClick={() => {
          const row = newRow();
          setRows((p) => [...p, row]);
          setFocus(row.key);
        }}
      >
        添加一句
      </Button>
    </Modal>
  );
}

/** 编辑框下方的实时检查：关联到的词、超纲词、词库外的词（停顿 400ms 后请求） */
function LiveAnalysis({ unitId, en }: { unitId: string; en: string }) {
  const [res, setRes] = useState<SentenceAnalysis | null>(null);
  const [failed, setFailed] = useState(false);
  const seq = useRef(0);
  useEffect(() => {
    const text = en.trim();
    const my = ++seq.current;
    if (!text) {
      setRes(null);
      return;
    }
    const timer = setTimeout(() => {
      api
        .post<SentenceAnalysis>("/sentences/analyze", { en: text, unitId })
        .then((r) => {
          if (my !== seq.current) return;
          setRes(r);
          setFailed(false);
        })
        .catch(() => {
          if (my === seq.current) setFailed(true);
        });
    }, 400);
    return () => clearTimeout(timer);
  }, [en, unitId]);
  if (failed) return <div style={{ fontSize: 12, color: "var(--muted)", marginTop: 4 }}>检查失败</div>;
  if (!res) return null;
  return (
    <div className="vx-live-analysis" style={{ fontSize: 12, color: "var(--muted)", marginTop: 4 }}>
      关联到 {res.words.length} 个词
      {res.outOfScope.length === 0 && res.unknown.length === 0 && <span style={{ color: "var(--good)" }}> · 没有超纲词</span>}
      <ScopeHints outOfScope={res.outOfScope} unknown={res.unknown.map((t) => t.text)} />
    </div>
  );
}

/** 单元内粘贴导入：格式与词表导入的 [句型] / [课文] 段相同，只是不需要 Unit 标题；没有段落标记时按当前标签的类型 */
function PasteImportModal({ unitId, kind, onClose, onDone }: { unitId: string; kind: TextKind; onClose: () => void; onDone: () => void }) {
  const { message } = useApp();
  const [text, setText] = useState("");
  const [texts, setTexts] = useState<EditPreviewText[] | null>(null);
  const [stats, setStats] = useState<TextsPreview["stats"] | null>(null);

  const preview = useMutation({
    mutationFn: () => api.post<TextsPreview>(`/units/${unitId}/texts/import/preview`, { text, kind }),
    onSuccess: (r) => {
      if (r.stats.sentences === 0) {
        message.warning("没有解析出任何句子，请检查格式");
        return;
      }
      setTexts(toEditTexts(r.texts));
      setStats(r.stats);
    },
    onError: (e) => message.error(errorMessage(e)),
  });

  const payload = texts ? previewTextsPayload(texts) : [];
  const count = payload.reduce((n, t) => n + t.sentences.length, 0);

  const doImport = useMutation({
    mutationFn: () => api.post<{ texts: number; sentences: number }>(`/units/${unitId}/texts/import`, { texts: payload }),
    onSuccess: (r) => {
      message.success(`已导入 ${r.texts} 篇、${r.sentences} 句`);
      onDone();
      onClose();
    },
    onError: (e) => message.error(errorMessage(e)),
  });

  const footer = texts ? (
    <Space wrap style={{ justifyContent: "flex-end" }}>
      <Button onClick={() => setTexts(null)}>上一步</Button>
      <Button type="primary" disabled={count === 0} loading={doImport.isPending} onClick={() => doImport.mutate()}>
        确认导入（{count} 句）
      </Button>
    </Space>
  ) : (
    <Space wrap style={{ justifyContent: "flex-end" }}>
      <Button onClick={onClose}>取消</Button>
      <Button type="primary" disabled={!text.trim()} loading={preview.isPending} onClick={() => preview.mutate()}>
        解析预览
      </Button>
    </Space>
  );

  return (
    <Modal open width={760} title={`粘贴导入${KIND_LABEL[kind]}`} onCancel={onClose} footer={footer} maskClosable={false} destroyOnHidden>
      {texts ? (
        <>
          {stats && (
            <div style={{ marginBottom: 10, color: "var(--ink-soft)", fontSize: 13 }}>
              {stats.texts} 篇 · {stats.sentences} 句 · 正常 {stats.ok} · 需检查 {stats.warning} · 错误 {stats.error}（错误的句子默认不导入）
            </div>
          )}
          <div style={{ maxHeight: "60vh", overflowY: "auto" }}>
            <TextsPreviewList
              texts={texts}
              onSentence={(ti, si, patch) => setTexts((prev) => prev && prev.map((t, i) => (i !== ti ? t : { ...t, sentences: t.sentences.map((s, j) => (j === si ? { ...s, ...patch } : s)) })))}
              onText={(ti, patch) => setTexts((prev) => prev && prev.map((t, i) => (i === ti ? { ...t, ...patch } : t)))}
            />
          </div>
        </>
      ) : (
        <>
          <Input.TextArea value={text} onChange={(e) => setText(e.target.value)} autoSize={{ minRows: 10, maxRows: 20 }} placeholder={TEXTS_SAMPLE} style={{ fontFamily: "var(--mono)", fontSize: 13 }} />
          <ul style={{ paddingLeft: 18, margin: "10px 0 0", lineHeight: 1.8, color: "var(--ink-soft)", fontSize: 13 }}>
            <li>每行一句：英文 | 中文；句型带「...」时第三列写完整例句</li>
            <li>课文可加一行 Title: 英文标题 | 中文标题；空行表示分段</li>
            <li>没有 [句型] / [课文] 标记时，按{KIND_LABEL[kind]}导入</li>
          </ul>
          <Button type="link" style={{ padding: 0, marginTop: 4 }} onClick={() => setText(TEXTS_SAMPLE)}>
            填入示例
          </Button>
        </>
      )}
    </Modal>
  );
}
