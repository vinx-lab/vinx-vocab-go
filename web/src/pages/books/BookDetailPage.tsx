import { useEffect, useRef, useState } from "preact/hooks";
import { useNavigate, useParams } from "@/lib/router";
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@/lib/query";
import { useIdentity } from "@/lib/auth";
import {
  Alert,
  Button,
  Checkbox,
  Col,
  Dropdown,
  Empty,
  Form,
  Grid,
  Input,
  Modal,
  Pagination,
  Popconfirm,
  Radio,
  Row,
  Segmented,
  Select,
  Space,
  Spin,
  Tag,
  Tooltip,
  useApp,
  DeleteOutlined,
  EditOutlined,
  HolderOutlined,
  ImportOutlined,
  MoreOutlined,
  PlusOutlined,
  ScheduleOutlined,
  SoundOutlined,
  ThunderboltOutlined,
} from "@/ui";
import {
  AI_LEVELS,
  AI_LEVEL_HINT,
  AI_LEVEL_LABEL,
  AI_PROMPT_MAX_LENGTH,
  isAiLevel,
  type AiExamplesJobResult,
  type AiJobStarted,
  type AiLevel,
  type AiPreview,
  type AiPreviewWord,
} from "@vinx/shared";
import { AiChecks, countFlagged } from "@/components/AiChecks";
import { LevelField } from "./AiDraftModals";
import { api, errorMessage } from "@/lib/api";
import { can } from "@/lib/perms";
import type { BookDetail, Paged, TextKind, UnitText, UnitWord, UnitWordsData } from "@/types";
import { UnitTextsPanel } from "./UnitTexts";
import { EmptyBlock, ErrorBlock, Loading, PageHeader, SpeakButton } from "@/components/ui";
import { useAiStatus } from "@/lib/useAiStatus";
import { SELF_PLAN_CLOSED_TEXT, myTargetsKey, useMyTargets } from "@/lib/targets";
import { useAiJob } from "@/lib/useAiJob";
import { AiJobProgress, AiPromptPreview } from "@/components/AiPromptPreview";

const PAGE_SIZE = 100;

type WordForm = { spelling: string; phonetic?: string; partOfSpeech?: string; definition: string; example?: string; exampleCn?: string };
type NameForm = { name: string; description?: string };
/** 单元页面的标签：词表 / 句型（list）/ 课文（text） */
type UnitTab = "words" | TextKind;
/** 弹窗状态 */
type Dialog =
  | { type: "book" }
  | { type: "addUnit" }
  | { type: "renameUnit"; unitId: string; name: string }
  | { type: "addWord"; unitId: string }
  | { type: "editWord"; word: UnitWord }
  | null;

export function BookDetailPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { message, modal } = useApp();
  const screens = Grid.useBreakpoint();
  const { data: identity } = useIdentity();

  const [activeUnitId, setActiveUnitId] = useState<string | undefined>(undefined);
  const [selected, setSelected] = useState<string[]>([]);
  const [q, setQ] = useState("");
  const [page, setPage] = useState(1);
  const [dialog, setDialog] = useState<Dialog>(null);
  const { ai, audio } = useAiStatus();
  const [aiTarget, setAiTarget] = useState<AiExamplesTarget | null>(null);
  const [dragId, setDragId] = useState<string | null>(null);

  const bookQuery = useQuery({ queryKey: ["books", "detail", id], queryFn: () => api.get<BookDetail>(`/books/${id}`), enabled: !!id });
  const book = bookQuery.data;

  // 默认选中第一个单元；当前单元被删除时回退
  useEffect(() => {
    if (!book) return;
    if (!activeUnitId || !book.units.some((u) => u.id === activeUnitId)) setActiveUnitId(book.units[0]?.id);
    setSelected((prev) => prev.filter((uid) => book.units.some((u) => u.id === uid)));
  }, [book, activeUnitId]);

  const wordsQuery = useQuery({
    queryKey: ["books", "unitWords", activeUnitId, page, q],
    queryFn: () => api.get<UnitWordsData>(`/units/${activeUnitId}/words`, { page, limit: PAGE_SIZE, ...(q ? { q } : {}) }),
    enabled: !!activeUnitId,
    placeholderData: keepPreviousData,
  });

  // spec 0004：单元的句型 / 课文
  const [tab, setTab] = useState<UnitTab>("words");
  const textsQuery = useQuery({
    queryKey: ["books", "unitTexts", activeUnitId],
    queryFn: () => api.get<Paged<UnitText>>(`/units/${activeUnitId}/texts`),
    enabled: !!activeUnitId,
    keepPrevious: false,
  });

  const refresh = () => queryClient.invalidateQueries({ queryKey: ["books"] });

  // spec 0008：「用所选单元建计划」只对目标词书里的书可用（学生；能给别人布置的老师到建计划页按对象约束）
  const learnerOnly = can(identity, "plans") && !can(identity, "plans.assign");
  const myTargets = useMyTargets(learnerOnly);
  const addToTargets = useMutation({
    mutationFn: (bookId: string) => api.put(`/me/target-books`, { bookIds: [...(myTargets.data?.ownBooks ?? []).map((b) => b.id), bookId] }),
    onSuccess: () => {
      message.success("已加入我的目标词书");
      void queryClient.invalidateQueries({ queryKey: [...myTargetsKey] });
      void queryClient.invalidateQueries({ queryKey: ["records"] });
    },
    onError: (e) => message.error(errorMessage(e, "加入失败")),
  });

  const deleteBook = useMutation({
    mutationFn: () => api.del(`/books/${id}`),
    onSuccess: () => {
      refresh();
      message.success("词书已删除");
      navigate("/books", { replace: true });
    },
    onError: (e) => message.error(errorMessage(e)),
  });

  const deleteUnit = useMutation({
    mutationFn: (unitId: string) => api.del(`/units/${unitId}`),
    onSuccess: () => {
      refresh();
      message.success("单元已删除");
    },
    onError: (e) => message.error(errorMessage(e)),
  });

  const removeWord = useMutation({
    mutationFn: (wordId: string) => api.del(`/units/${activeUnitId}/words/${wordId}`),
    onSuccess: () => {
      refresh();
      message.success("已从本单元移除（词库中的词条保留）");
    },
    onError: (e) => message.error(errorMessage(e)),
  });

  // 单元拖拽排序（整册一次提交顺序）
  const reorder = useMutation({
    mutationFn: (unitIds: string[]) => api.patch(`/books/${id}/units/order`, { unitIds }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["books", "detail", id] }),
    onError: (e) => {
      message.error(errorMessage(e));
      queryClient.invalidateQueries({ queryKey: ["books", "detail", id] });
    },
  });

  /** 拖到目标单元前面 */
  const dropOn = (targetId: string) => {
    if (!dragId || dragId === targetId || !bookQuery.data) return;
    const ids = bookQuery.data.units.map((u) => u.id).filter((x) => x !== dragId);
    const at = ids.indexOf(targetId);
    ids.splice(at < 0 ? ids.length : at, 0, dragId);
    setDragId(null);
    // 乐观更新，失败时由 onError 重新拉取
    queryClient.setQueryData<BookDetail>(["books", "detail", id], (prev) =>
      prev ? { ...prev, units: ids.map((uid) => prev.units.find((u) => u.id === uid)!).filter(Boolean) } : prev,
    );
    reorder.mutate(ids);
  };

  const prefetchAudio = useMutation({
    mutationFn: () => api.post<{ ok: number; failed: number; skipped: number }>(`/audio/units/${activeUnitId}/prefetch`),
    onSuccess: (r) => message.success(`发音缓存完成：新增 ${r.ok}，已有 ${r.skipped}${r.failed ? `，失败 ${r.failed}` : ""}`),
    onError: (e) => message.error(errorMessage(e)),
  });

  if (bookQuery.isLoading) return <Loading />;
  if (bookQuery.error || !book) {
    return (
      <div className="vx-page">
        <ErrorBlock error={bookQuery.error ?? new Error("词书不存在")} onRetry={() => bookQuery.refetch()} />
      </div>
    );
  }

  const canPlan = can(identity, "plans");
  const canImport = can(identity, "books.edit");
  const editable = book.canEdit;
  const activeUnit = book.units.find((u) => u.id === activeUnitId);
  const totalWords = book.units.reduce((s, u) => s + u.wordCount, 0);
  const isMobile = !screens.md;
  /** 标签上的句子数（句型 / 课文） */
  const textCount = (kind: TextKind) => {
    const n = (textsQuery.data?.items ?? []).filter((t) => t.kind === kind).reduce((s, t) => s + t.sentences.length, 0);
    return n ? ` ${n}` : "";
  };

  const toggleSelect = (uid: string, checked: boolean) =>
    setSelected((prev) => {
      const next = checked ? [...prev, uid] : prev.filter((x) => x !== uid);
      // 按单元在书中的顺序排列
      return book.units.map((u) => u.id).filter((x) => next.includes(x));
    });

  const switchUnit = (uid: string) => {
    setActiveUnitId(uid);
    setPage(1);
    setQ("");
  };

  const t = learnerOnly ? myTargets.data : undefined;
  const selfClosed = t?.canEditOwn === false;
  const outsideTarget = !!t && t.books.length > 0 && !t.books.some((b) => b.id === book.id);
  const planBlocked = selfClosed || outsideTarget;
  const headerActions = (
    <>
      {canPlan && (
        <Button
          type="primary"
          icon={<ScheduleOutlined />}
          disabled={selected.length === 0 || planBlocked}
          title={selfClosed ? SELF_PLAN_CLOSED_TEXT : outsideTarget ? "先把这本书加入目标词书" : undefined}
          onClick={() => navigate(`/plans/new?bookId=${book.id}&unitIds=${selected.join(",")}`)}
        >
          用所选单元建计划{selected.length > 0 ? `（${selected.length}）` : ""}
        </Button>
      )}
      {canPlan && outsideTarget && !selfClosed && (
        <Button loading={addToTargets.isPending} onClick={() => addToTargets.mutate(book.id)}>
          加入我的目标
        </Button>
      )}
      {editable && canImport && (
        <Button icon={<ImportOutlined />} onClick={() => navigate(`/books/import?bookId=${book.id}`)}>
          导入到本词书
        </Button>
      )}
      {editable && (
        <Dropdown
          trigger={["click"]}
          menu={{
            items: [
              { key: "rename", icon: <EditOutlined />, label: "编辑名称与描述" },
              { key: "delete", icon: <DeleteOutlined />, danger: true, label: "删除词书" },
            ],
            onClick: ({ key }) => {
              if (key === "rename") setDialog({ type: "book" });
              else if (key === "delete")
                modal.confirm({
                  title: `删除词书「${book.name}」？`,
                  content: "单元与单词关联会一起删除，词库中的词条和学生的学习记录保留。正在被计划使用的词书无法删除。",
                  okText: "删除",
                  okButtonProps: { danger: true },
                  cancelText: "取消",
                  onOk: () => deleteBook.mutateAsync().catch(() => undefined),
                });
            },
          }}
        >
          <Button icon={<MoreOutlined />} aria-label="更多操作" />
        </Dropdown>
      )}
    </>
  );

  return (
    <div className="vx-page">
      <PageHeader eyebrow="Word book" title={book.name} extra={headerActions}>
        <Tag color={book.isSystem ? "cyan" : "volcano"} bordered={false}>
          {book.isSystem ? "系统词书" : `${book.ownerName ?? ""} 的词书`}
        </Tag>
        {isAiLevel(book.level) && (
          <Tag bordered={false} data-testid="book-level-tag">
            {AI_LEVEL_LABEL[book.level]}
          </Tag>
        )}
        {book.units.length} 单元 · {totalWords} 词{book.description ? ` · ${book.description}` : ""}
        {canPlan && planBlocked && (
          <div style={{ marginTop: 4, fontSize: 13, color: "var(--muted)" }} data-testid="book-plan-hint">
            {selfClosed
              ? `${SELF_PLAN_CLOSED_TEXT}，目标词书由老师设置。`
              : "这本书不在你的目标词书里：先把这本书加入目标词书，再用它建计划。"}
          </div>
        )}
      </PageHeader>

      {book.units.length === 0 ? (
        <EmptyBlock
          title="这本词书还没有单元"
          description={editable ? "添加一个单元后录入单词，或直接导入词表。" : undefined}
          action={
            editable && (
              <Space>
                <Button type="primary" icon={<PlusOutlined />} onClick={() => setDialog({ type: "addUnit" })}>
                  添加单元
                </Button>
                {canImport && <Button onClick={() => navigate(`/books/import?bookId=${book.id}`)}>导入词表</Button>}
              </Space>
            )
          }
        />
      ) : (
        <Row gutter={[16, 16]}>
          <Col xs={24} md={8} lg={7}>
            {isMobile ? (
              <div className="vx-card" style={{ padding: 12 }}>
                <Select
                  style={{ width: "100%" }}
                  value={activeUnitId}
                  onChange={switchUnit}
                  options={book.units.map((u) => ({ value: u.id, label: `${u.name}（${u.wordCount} 词）` }))}
                />
                <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginTop: 10 }}>
                  {canPlan && activeUnitId ? (
                    <Checkbox checked={selected.includes(activeUnitId)} onChange={(e) => toggleSelect(activeUnitId, e.target.checked)}>
                      选入计划
                    </Checkbox>
                  ) : (
                    <span />
                  )}
                  {editable && (
                    <Button size="small" icon={<PlusOutlined />} onClick={() => setDialog({ type: "addUnit" })}>
                      添加单元
                    </Button>
                  )}
                </div>
              </div>
            ) : (
              <div className="vx-card" style={{ padding: "8px 0", position: "sticky", top: 16 }}>
                <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", padding: "6px 14px 10px" }}>
                  <span className="vx-eyebrow">Units{editable ? " · 可拖动排序" : ""}</span>
                  {canPlan && (
                    <Checkbox
                      checked={selected.length === book.units.length}
                      indeterminate={selected.length > 0 && selected.length < book.units.length}
                      onChange={(e) => setSelected(e.target.checked ? book.units.map((u) => u.id) : [])}
                    >
                      全选
                    </Checkbox>
                  )}
                </div>
                <div style={{ maxHeight: "calc(100vh - 220px)", overflowY: "auto" }}>
                  {book.units.map((u) => {
                    const active = u.id === activeUnitId;
                    return (
                      <div
                        key={u.id}
                        onClick={() => switchUnit(u.id)}
                        draggable={editable}
                        onDragStart={() => setDragId(u.id)}
                        onDragOver={(e) => editable && e.preventDefault()}
                        onDrop={() => dropOn(u.id)}
                        onDragEnd={() => setDragId(null)}
                        style={{
                          display: "flex",
                          alignItems: "center",
                          gap: 10,
                          padding: "9px 14px",
                          cursor: "pointer",
                          borderLeft: `3px solid ${active ? "var(--accent)" : "transparent"}`,
                          background: dragId === u.id ? "var(--primary-soft)" : active ? "var(--paper-deep)" : undefined,
                          opacity: dragId === u.id ? 0.6 : 1,
                        }}
                      >
                        {editable && (
                          <HolderOutlined
                            aria-label="拖动排序"
                            style={{ color: "var(--muted)", cursor: "grab" }}
                            onClick={(e) => e.stopPropagation()}
                          />
                        )}
                        {canPlan && (
                          <span onClick={(e) => e.stopPropagation()}>
                            <Checkbox checked={selected.includes(u.id)} onChange={(e) => toggleSelect(u.id, e.target.checked)} aria-label={`选择 ${u.name}`} />
                          </span>
                        )}
                        <span className="vx-word" style={{ flex: 1, minWidth: 0, fontWeight: active ? 600 : 400, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                          {u.name}
                        </span>
                        <span className="vx-num" style={{ color: "var(--muted)", fontSize: 12 }}>
                          {u.wordCount}
                        </span>
                      </div>
                    );
                  })}
                </div>
                {editable && (
                  <div style={{ padding: "10px 14px 4px", borderTop: "1px solid var(--line)" }}>
                    <Button block type="dashed" icon={<PlusOutlined />} onClick={() => setDialog({ type: "addUnit" })}>
                      添加单元
                    </Button>
                  </div>
                )}
              </div>
            )}
          </Col>

          <Col xs={24} md={16} lg={17}>
            <div className="vx-card" style={{ padding: "14px 16px" }}>
              <div style={{ display: "flex", flexWrap: "wrap", gap: 10, alignItems: "center", justifyContent: "space-between", marginBottom: 12 }}>
                <div style={{ minWidth: 0 }}>
                  <h2 className="vx-word" style={{ fontSize: 22, margin: 0, fontWeight: 600 }}>
                    {activeUnit?.name}
                  </h2>
                  <div style={{ fontSize: 12, color: "var(--muted)" }}>{wordsQuery.data ? `${wordsQuery.data.total} 词${q ? "（搜索结果）" : ""}` : " "}</div>
                </div>
                <Space wrap>
                  {tab === "words" && (
                  <Input.Search
                    allowClear
                    placeholder="搜索拼写或释义"
                    style={{ width: isMobile ? "100%" : 200 }}
                    onSearch={(v) => {
                      setQ(v.trim());
                      setPage(1);
                    }}
                    key={activeUnitId}
                  />
                  )}
                  {editable && activeUnit && (
                    <>
                      {tab === "words" && (
                        <>
                          <Button type="primary" ghost icon={<PlusOutlined />} onClick={() => setDialog({ type: "addWord", unitId: activeUnit.id })}>
                            添加单词
                          </Button>
                          {ai && (
                            <Button icon={<ThunderboltOutlined />} onClick={() => setAiTarget({ kind: "unit", unitId: activeUnit.id })}>
                              AI 补例句
                            </Button>
                          )}
                          {audio && (
                            <Tooltip title="预先抓取本单元的真人发音，学生端零等待">
                              <Button icon={<SoundOutlined />} loading={prefetchAudio.isPending} onClick={() => prefetchAudio.mutate()}>
                                缓存发音
                              </Button>
                            </Tooltip>
                          )}
                        </>
                      )}
                      <Tooltip title="重命名单元">
                        <Button icon={<EditOutlined />} aria-label="重命名单元" onClick={() => setDialog({ type: "renameUnit", unitId: activeUnit.id, name: activeUnit.name })} />
                      </Tooltip>
                      <Popconfirm
                        title="删除这个单元？"
                        description="单元内的单词关联会删除，词库词条保留。正在被计划使用的单元无法删除。"
                        okText="删除"
                        okButtonProps={{ danger: true }}
                        cancelText="取消"
                        onConfirm={() => deleteUnit.mutate(activeUnit.id)}
                      >
                        <Button danger icon={<DeleteOutlined />} aria-label="删除单元" loading={deleteUnit.isPending} />
                      </Popconfirm>
                    </>
                  )}
                </Space>
              </div>

              <Segmented
                className="vx-unit-tabs"
                value={tab}
                onChange={(v) => setTab(v as UnitTab)}
                style={{ marginBottom: 12 }}
                options={[
                  { value: "words", label: `词表${wordsQuery.data && !q ? ` ${wordsQuery.data.total}` : ""}` },
                  { value: "list", label: `句型${textCount("list")}` },
                  { value: "text", label: `课文${textCount("text")}` },
                ]}
              />

              {tab !== "words" ? (
                textsQuery.isLoading ? (
                  <div style={{ padding: 40, textAlign: "center" }}>
                    <Spin />
                  </div>
                ) : textsQuery.error || !textsQuery.data || !activeUnit ? (
                  <ErrorBlock error={textsQuery.error ?? new Error("加载失败")} onRetry={() => textsQuery.refetch()} />
                ) : (
                  <UnitTextsPanel key={`${activeUnit.id}-${tab}`} unitId={activeUnit.id} kind={tab} editable={editable} ai={ai} texts={textsQuery.data.items} />
                )
              ) : wordsQuery.isLoading ? (
                <div style={{ padding: 40, textAlign: "center" }}>
                  <Spin />
                </div>
              ) : wordsQuery.error ? (
                <ErrorBlock error={wordsQuery.error} onRetry={() => wordsQuery.refetch()} />
              ) : !wordsQuery.data || wordsQuery.data.items.length === 0 ? (
                <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={q ? "没有匹配的单词" : "这个单元还没有单词"} />
              ) : (
                <>
                  <div>
                    {wordsQuery.data.items.map((w) => (
                      <WordRow
                        key={w.id}
                        word={w}
                        editable={editable}
                        aiEnabled={ai}
                        onAi={() => setAiTarget({ kind: "word", word: w })}
                        onEdit={() => setDialog({ type: "editWord", word: w })}
                        onRemove={() => removeWord.mutate(w.id)}
                      />
                    ))}
                  </div>
                  {wordsQuery.data.total > PAGE_SIZE && (
                    <div style={{ textAlign: "center", marginTop: 12 }}>
                      <Pagination current={page} pageSize={PAGE_SIZE} total={wordsQuery.data.total} showSizeChanger={false} onChange={setPage} size="small" />
                    </div>
                  )}
                </>
              )}
            </div>
          </Col>
        </Row>
      )}

      {aiTarget && <AiExamplesModal target={aiTarget} onClose={() => setAiTarget(null)} />}
      {dialog?.type === "book" && <BookInfoModal book={book} onClose={() => setDialog(null)} />}
      {dialog?.type === "addUnit" && (
        <UnitNameModal
          title="添加单元"
          initial=""
          submit={(name) => api.post<{ id: string }>(`/books/${book.id}/units`, { name })}
          onDone={(unit) => {
            if (unit?.id) switchUnit(unit.id);
            setDialog(null);
          }}
          onClose={() => setDialog(null)}
        />
      )}
      {dialog?.type === "renameUnit" && (
        <UnitNameModal
          title="重命名单元"
          initial={dialog.name}
          submit={(name) => api.patch<{ id: string }>(`/units/${dialog.unitId}`, { name })}
          onDone={() => setDialog(null)}
          onClose={() => setDialog(null)}
        />
      )}
      {(dialog?.type === "addWord" || dialog?.type === "editWord") && (
        <WordModal
          unitId={dialog.type === "addWord" ? dialog.unitId : undefined}
          word={dialog.type === "editWord" ? dialog.word : undefined}
          onClose={() => setDialog(null)}
        />
      )}
    </div>
  );
}

function WordRow({
  word,
  editable,
  aiEnabled,
  onAi,
  onEdit,
  onRemove,
}: {
  word: UnitWord;
  editable: boolean;
  aiEnabled: boolean;
  onAi: () => void;
  onEdit: () => void;
  onRemove: () => void;
}) {
  const { modal } = useApp();
  return (
    <div style={{ display: "flex", gap: 12, padding: "12px 4px", borderTop: "1px solid var(--line)", alignItems: "flex-start" }}>
      <div style={{ flex: 1, minWidth: 0 }}>
        <div style={{ display: "flex", flexWrap: "wrap", alignItems: "baseline", columnGap: 10 }}>
          <span className="vx-word" style={{ fontSize: 20, fontWeight: 600, wordBreak: "break-word" }}>
            {word.spelling}
          </span>
          {word.phonetic && <span style={{ color: "var(--muted)", fontSize: 13, fontFamily: "var(--serif-en)" }}>{word.phonetic}</span>}
          {word.type === "phrase" && (
            <Tag bordered={false} style={{ marginInlineEnd: 0 }}>
              词组
            </Tag>
          )}
        </div>
        <div className="vx-cn" style={{ marginTop: 2, fontSize: 15 }}>
          {word.partOfSpeech && <span style={{ color: "var(--primary)", fontStyle: "italic", marginRight: 6, fontFamily: "var(--serif-en)" }}>{word.partOfSpeech}</span>}
          {word.definition}
        </div>
        {word.example ? (
          <div style={{ marginTop: 4 }}>
            <span className="vx-example-en is-compact">{word.example}</span>
            {word.exampleSource === "ai" && (
              <Tag bordered={false} color="gold" style={{ marginLeft: 6 }}>
                AI
              </Tag>
            )}
            {word.exampleCn && <div className="vx-example-cn is-compact" style={{ color: "var(--muted)" }}>{word.exampleCn}</div>}
          </div>
        ) : (
          editable && <div style={{ marginTop: 4, fontSize: 12, color: "var(--muted)" }}>暂无例句（挖空填词题需要例句）</div>
        )}
      </div>
      <Space size={4}>
        <SpeakButton text={word.spelling} wordId={word.id} size="small" />
        {editable && aiEnabled && (
          <Tooltip title={word.example ? "用 AI 重写例句" : "用 AI 生成例句"}>
            <Button size="small" shape="circle" icon={<ThunderboltOutlined />} onClick={onAi} aria-label={`AI 例句 ${word.spelling}`} />
          </Tooltip>
        )}
        {editable && (
          <Dropdown
            trigger={["click"]}
            menu={{
              items: [
                { key: "edit", icon: <EditOutlined />, label: "编辑词条" },
                { key: "remove", icon: <DeleteOutlined />, danger: true, label: "从本单元移除" },
              ],
              onClick: ({ key }) => {
                if (key === "edit") onEdit();
                else if (key === "remove")
                  modal.confirm({
                    title: `从本单元移除「${word.spelling}」？`,
                    content: "只移除本单元中的关联，词库词条和学习记录保留。",
                    okText: "移除",
                    okButtonProps: { danger: true },
                    cancelText: "取消",
                    onOk: onRemove,
                  });
              },
            }}
          >
            <Button size="small" shape="circle" icon={<MoreOutlined />} aria-label={`${word.spelling} 更多操作`} />
          </Dropdown>
        )}
      </Space>
    </div>
  );
}

/** AI 补例句的对象：整单元（每次最多 20 个缺例句的词）或单个词 */
type AiExamplesTarget = { kind: "unit"; unitId: string } | { kind: "word"; word: UnitWord };

/**
 * AI 补例句（K41）：先预览这次的单词和可编辑的提示词，确认后提交后台任务并轮询；
 * 例句在服务端写回词书，关掉弹窗也不影响。
 */
function AiExamplesModal({ target, onClose }: { target: AiExamplesTarget; onClose: () => void }) {
  const { modal } = useApp();
  const queryClient = useQueryClient();
  const base = target.kind === "unit" ? `/ai/units/${target.unitId}/examples` : `/ai/words/${target.word.id}/example`;
  const [words, setWords] = useState<AiPreviewWord[]>([]);
  const [prompt, setPrompt] = useState("");
  const [basePrompt, setBasePrompt] = useState("");
  const [remaining, setRemaining] = useState<number | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const job = useAiJob<AiExamplesJobResult>();
  const seq = useRef(0);
  /** spec 0005：临时改的学段（只对这一次有效）；没改时用预览返回的（词书的学段） */
  const [levelOverride, setLevelOverride] = useState<AiLevel | undefined>(undefined);
  const [previewLevel, setPreviewLevel] = useState<AiLevel | undefined>(undefined);
  const level = levelOverride ?? previewLevel;

  /** 取预览；wordIds 为空表示按缺例句重新挑一批。手改过的提示词先确认再覆盖 */
  const load = async (wordIds?: string[], keepEdits = false, lv: AiLevel | undefined = levelOverride) => {
    const my = ++seq.current;
    setLoading(true);
    try {
      const r = await api.post<AiPreview & { remaining?: number }>(`${base}/preview`, { ...(wordIds ? { wordIds } : {}), ...(lv ? { level: lv } : {}) });
      if (my !== seq.current) return;
      setLoadError(null);
      setWords(r.words);
      if (isAiLevel(r.level)) setPreviewLevel(r.level);
      if (r.remaining !== undefined) setRemaining(r.remaining);
      if (keepEdits && prompt !== basePrompt && prompt !== r.prompt) {
        modal.confirm({
          title: "替换你改过的提示词？",
          content: "单词变了，提示词已按新的单词重新拼好。替换后，你在编辑框里的修改会丢失。",
          okText: "替换",
          cancelText: "保留我的修改",
          onOk: () => {
            setPrompt(r.prompt);
            setBasePrompt(r.prompt);
          },
          onCancel: () => setBasePrompt(r.prompt),
        });
      } else {
        setPrompt(r.prompt);
        setBasePrompt(r.prompt);
      }
    } catch (e) {
      if (my === seq.current) setLoadError(errorMessage(e, "预览失败"));
    } finally {
      if (my === seq.current) setLoading(false);
    }
  };

  useEffect(() => {
    void load();
    // 只在打开时取一次
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [base]);

  const generate = () =>
    job.start(
      () => api.post<AiJobStarted>(base, { wordIds: words.map((w) => w.id), prompt, ...(level ? { level } : {}) }),
      () => void queryClient.invalidateQueries({ queryKey: ["books"] }),
    );

  const changeLevel = (lv: AiLevel) => {
    setLevelOverride(lv);
    void load(target.kind === "unit" ? words.map((w) => w.id) : undefined, true, lv);
  };

  const nextBatch = () => {
    job.reset();
    void load();
  };

  const state = job.state;
  const done = state.phase === "done" ? state.result : null;
  const title = target.kind === "unit" ? "AI 补例句（本单元）" : `AI 例句：${target.word.spelling}`;

  const footer = done ? (
    <Space wrap style={{ justifyContent: "flex-end" }}>
      {target.kind === "unit" && (done.remaining ?? 0) > 0 && <Button onClick={nextBatch}>继续下一批</Button>}
      <Button type="primary" onClick={onClose}>
        完成
      </Button>
    </Space>
  ) : (
    <Space wrap style={{ justifyContent: "flex-end" }}>
      <Button onClick={onClose}>{job.running ? "关闭（后台继续）" : "取消"}</Button>
      <Button type="primary" icon={<ThunderboltOutlined />} loading={job.running} disabled={loading || words.length === 0 || !prompt.trim() || prompt.trim().length > AI_PROMPT_MAX_LENGTH} onClick={generate}>
        {job.running ? "生成中…" : "生成"}
      </Button>
    </Space>
  );

  return (
    <Modal open title={title} onCancel={onClose} footer={footer} destroyOnHidden maskClosable={false}>
      {done ? (
        <div data-testid="ai-examples-result">
          <Alert
            type={done.failed.length ? "warning" : "success"}
            showIcon
            message={`已生成 ${done.items.length} 条例句，已写回词书`}
            description={
              <>
                {done.failed.length > 0 && <div>没有生成合格例句：{done.failed.join("、")}</div>}
                {countFlagged(done.items) > 0 && <div>{countFlagged(done.items)} 条带标记（超纲或太长），可以在词条里修改</div>}
                {target.kind === "unit" && <div>本单元还有 {done.remaining ?? 0} 个词缺例句</div>}
              </>
            }
          />
          <div style={{ marginTop: 12 }}>
            {done.items.map((it) => (
              <div key={it.wordId} style={{ padding: "8px 0", borderTop: "1px solid var(--line)" }}>
                <span className="vx-word" style={{ fontWeight: 600 }}>
                  {it.spelling}
                </span>
                <div className="vx-example-en is-compact">{it.example}</div>
                <div className="vx-example-cn is-compact" style={{ color: "var(--muted)" }}>
                  {it.exampleCn}
                </div>
                <AiChecks checks={it.checks} />
              </div>
            ))}
          </div>
        </div>
      ) : loadError ? (
        <Alert type="error" showIcon message={loadError} action={<Button size="small" onClick={() => void load()}>重试</Button>} />
      ) : (
        <>
          {target.kind === "unit" && remaining !== null && (
            <div style={{ color: "var(--muted)", fontSize: 12, marginBottom: 10 }}>
              本单元有 {remaining} 个词缺例句，每次最多补 20 个。
            </div>
          )}
          <LevelField value={level} onChange={changeLevel} disabled={job.running || loading} />
          <AiPromptPreview
            words={words}
            onRemoveWord={target.kind === "unit" && !job.running ? (id) => void load(words.filter((w) => w.id !== id).map((w) => w.id), true) : undefined}
            prompt={prompt}
            onPromptChange={setPrompt}
            onRestore={() => setPrompt(basePrompt)}
            dirty={prompt !== basePrompt}
            loading={loading}
            disabled={job.running}
            emptyText={target.kind === "unit" ? "本单元的词都有例句了" : "没有可用的单词"}
          />
          <AiJobProgress state={state} />
        </>
      )}
    </Modal>
  );
}

/** 编辑词书：名称、描述、学段（spec 0005：决定 AI 生成例句、句型、仿写的默认难度） */
export function BookInfoModal({ book, onClose }: { book: BookDetail; onClose: () => void }) {
  const [form] = Form.useForm<NameForm>();
  const { message } = useApp();
  const queryClient = useQueryClient();
  const [level, setLevel] = useState<string>(isAiLevel(book.level) ? book.level : "");
  const save = useMutation({
    mutationFn: (v: NameForm) => api.patch(`/books/${book.id}`, { name: v.name, description: v.description || null, level: level || null }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["books"] });
      message.success("已保存");
      onClose();
    },
    onError: (e) => message.error(errorMessage(e)),
  });
  return (
    <Modal open title="编辑词书" okText="保存" cancelText="取消" confirmLoading={save.isPending} onCancel={onClose} onOk={() => form.submit()}>
      <Form form={form} layout="vertical" initialValues={{ name: book.name, description: book.description ?? "" }} onFinish={(v) => save.mutate(v)}>
        <Form.Item name="name" label="名称" rules={[{ required: true, whitespace: true, message: "请填写名称" }]}>
          <Input maxLength={60} />
        </Form.Item>
        <Form.Item name="description" label="描述">
          <Input.TextArea rows={3} maxLength={300} showCount />
        </Form.Item>
      </Form>
      <div data-testid="book-level">
        <div style={{ marginBottom: 8 }}>学段</div>
        <Radio.Group
          optionType="button"
          value={level}
          onChange={(e) => setLevel(String(e.target.value))}
          options={[{ value: "", label: "不设置" }, ...AI_LEVELS.map((l) => ({ value: l, label: AI_LEVEL_LABEL[l] }))]}
        />
        <div style={{ color: "var(--muted)", fontSize: 12, marginTop: 6, lineHeight: 1.7 }}>
          {isAiLevel(level) ? AI_LEVEL_HINT[level] : "不设置时按初中"}。AI 给这本书生成例句、句型和仿写时默认用这个学段，生成前还可以临时改。
        </div>
      </div>
    </Modal>
  );
}

function UnitNameModal({
  title,
  initial,
  submit,
  onDone,
  onClose,
}: {
  title: string;
  initial: string;
  submit: (name: string) => Promise<{ id: string }>;
  onDone: (unit: { id: string } | undefined) => void;
  onClose: () => void;
}) {
  const [form] = Form.useForm<{ name: string }>();
  const { message } = useApp();
  const queryClient = useQueryClient();
  const save = useMutation({
    mutationFn: (v: { name: string }) => submit(v.name.trim()),
    onSuccess: (unit) => {
      queryClient.invalidateQueries({ queryKey: ["books"] });
      message.success("已保存");
      onDone(unit);
    },
    onError: (e) => message.error(errorMessage(e)),
  });
  return (
    <Modal open title={title} okText="保存" cancelText="取消" confirmLoading={save.isPending} onCancel={onClose} onOk={() => form.submit()}>
      <Form form={form} layout="vertical" initialValues={{ name: initial }} onFinish={(v) => save.mutate(v)}>
        <Form.Item name="name" label="单元名称" rules={[{ required: true, whitespace: true, message: "请填写单元名称" }]}>
          <Input maxLength={60} placeholder="例如：Unit 1" autoFocus />
        </Form.Item>
      </Form>
    </Modal>
  );
}

/** 添加（unitId）或编辑（word）词条 */
function WordModal({ unitId, word, onClose }: { unitId?: string; word?: UnitWord; onClose: () => void }) {
  const [form] = Form.useForm<WordForm>();
  const { message } = useApp();
  const queryClient = useQueryClient();
  const isEdit = !!word;

  const save = useMutation({
    mutationFn: async (v: WordForm) => {
      const body = {
        spelling: v.spelling.trim(),
        phonetic: v.phonetic?.trim() || null,
        partOfSpeech: v.partOfSpeech?.trim() || null,
        definition: v.definition.trim(),
        example: v.example?.trim() || null,
        exampleCn: v.exampleCn?.trim() || null,
      };
      if (isEdit) {
        await api.patch(`/words/${word.id}`, body);
        return null;
      }
      return api.post<{ reused: boolean; linked: boolean }>(`/units/${unitId}/words`, body);
    },
    onSuccess: (r) => {
      queryClient.invalidateQueries({ queryKey: ["books"] });
      if (!r) message.success("词条已更新");
      else if (!r.linked) message.info("本单元已有该词");
      else if (r.reused) message.success("词库中已有该词，已直接加入本单元");
      else message.success("单词已添加");
      onClose();
    },
    onError: (e) => message.error(errorMessage(e)),
  });

  return (
    <Modal open title={isEdit ? "编辑词条" : "添加单词"} okText="保存" cancelText="取消" confirmLoading={save.isPending} onCancel={onClose} onOk={() => form.submit()}>
      {isEdit && word.usedByUnits > 1 && (
        <Alert type="warning" showIcon style={{ marginBottom: 12 }} message={`该词被 ${word.usedByUnits} 个单元引用，修改会同步到所有词书`} />
      )}
      {!isEdit && <Alert type="info" showIcon style={{ marginBottom: 12 }} message="词库按拼写去重：如果词库中已有该拼写，会直接复用已有词条。" />}
      <Form
        form={form}
        layout="vertical"
        onFinish={(v) => save.mutate(v)}
        initialValues={
          word
            ? {
                spelling: word.spelling,
                phonetic: word.phonetic ?? "",
                partOfSpeech: word.partOfSpeech ?? "",
                definition: word.definition,
                example: word.example ?? "",
                exampleCn: word.exampleCn ?? "",
              }
            : undefined
        }
      >
        <Row gutter={12}>
          <Col xs={24} sm={12}>
            <Form.Item name="spelling" label="拼写" rules={[{ required: true, whitespace: true, message: "请填写拼写" }]}>
              <Input className="vx-word" maxLength={100} autoFocus />
            </Form.Item>
          </Col>
          <Col xs={14} sm={7}>
            <Form.Item name="phonetic" label="音标">
              <Input maxLength={100} placeholder="/'wɜːd/" />
            </Form.Item>
          </Col>
          <Col xs={10} sm={5}>
            <Form.Item name="partOfSpeech" label="词性">
              <Input maxLength={30} placeholder="n." />
            </Form.Item>
          </Col>
        </Row>
        <Form.Item name="definition" label="释义" rules={[{ required: true, whitespace: true, message: "请填写释义" }]}>
          <Input maxLength={300} placeholder="中文释义" />
        </Form.Item>
        <Form.Item name="example" label="例句">
          <Input.TextArea rows={2} maxLength={500} />
        </Form.Item>
        <Form.Item name="exampleCn" label="例句翻译">
          <Input.TextArea rows={2} maxLength={500} />
        </Form.Item>
      </Form>
    </Modal>
  );
}
