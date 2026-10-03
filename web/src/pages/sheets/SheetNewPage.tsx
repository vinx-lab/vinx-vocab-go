import { useEffect, useMemo, useState } from "preact/hooks";
import { useNavigate, useSearchParams } from "@/lib/router";
import { useQuery, useQueryClient } from "@/lib/query";
import { Button, Checkbox, InputNumber, Segmented, Select, Spin, Table, Tag, useApp } from "@/ui";
import { api, errorMessage } from "@/lib/api";
import { ErrorBlock, PageHeader } from "@/components/ui";
import type {
  Book,
  BookDetail,
  CoverageData,
  DictSentenceCandidate,
  Mode,
  Paged,
  PassageListItem,
  SheetCandidate,
  SheetFormat,
  SheetPreviewData,
  SheetSource,
  SheetSourcesData,
  StudySessionData,
  TargetSheetStatus,
} from "@/types";
import { KIND_LABEL, MODE_LABEL } from "@/types";
import { COVERAGE_LABEL, parseTargetStatus } from "@/pages/coverage/coverage";
import { withUser } from "@/pages/records/shared";
import { batchSizes, printLink } from "./links";
import { reasonLabel, REASON_COLOR } from "./reasons";
import {
  DICT_DEFAULT_LIMITS,
  DICT_MAX_LIMITS,
  DICT_TYPE_LABEL,
  LINE_STYLE_LABEL,
  defaultLineStyle,
  printLinkWithLines,
  sentenceSourcesOf,
  splitDictation,
  type DictItemRef,
  type DictLimits,
  type LineStyle,
} from "./dictation";

interface WordHit { id: string; spelling: string; definition: string; phonetic: string | null; partOfSpeech: string | null; type?: string }

/**
 * 界面上的来源切换（整本书是单元下拉里的一项，提交时转成 book 来源；
 * 目标的两种状态各占一项，提交时转成 target 来源）
 */
type SourceKind = Exclude<SheetSource["kind"], "book" | "target"> | "targetUntested" | "targetLearning";

const SOURCE_HINT: Record<SourceKind, string> = {
  unfamiliar: "系统按最近答错、遗忘次数、记忆稳定性和到期时间挑词。",
  session: "选最近 30 天里的一次测试或学习，用那次答错的词。",
  unit: "选一个单元或整本书，先放还不熟的词，再放没学过的词（没学过的只记成绩，不建记忆卡）。",
  targetUntested: "目标词书里还没有正式测过的词，按书序排列，可以只出某一本书。",
  targetLearning: "目标词书里最近一次正式测试答错、还要再学的词，可以只出某一本书。",
};

const TARGET_KIND: Record<TargetSheetStatus, SourceKind> = { untested: "targetUntested", learning: "targetLearning" };
const KIND_TARGET: Partial<Record<SourceKind, TargetSheetStatus>> = { targetUntested: "untested", targetLearning: "learning" };

/** 单元下拉里的「整本书」选项 */
const WHOLE_BOOK = "__book__";
/** 目标词书下拉里的「全部目标词书」选项 */
const ALL_TARGETS = "__all__";

const TEXT_KIND_LABEL: Record<string, string> = { list: "句型", text: "课文" };
const SENTENCE_STATUS: Record<DictSentenceCandidate["status"], { label: string; color?: string }> = {
  learning: { label: "要学", color: "red" },
  untested: { label: "未测", color: "blue" },
  known: { label: "会了", color: "green" },
};

const wordKey = (id: string) => `w:${id}`;
const sentKey = (id: string) => `s:${id}`;

/**
 * 生成单词单：选格式（自测表 / 默写单）→ 选来源 → 系统预选（从难到易）→ 勾掉/加词 → 份数与每份题数 → 生成并合并打印。
 * 默写单（spec 0006）另有句子来源（单元的篇、要学的句子、AI 短文），单词、短语、句子各自设每份上限。
 */
export function SheetNewPage() {
  const [params] = useSearchParams();
  const userId = params.get("userId") ?? undefined;
  const from = params.get("from") ?? undefined;
  const navigate = useNavigate();
  const qc = useQueryClient();
  const { message } = useApp();
  const [format, setFormat] = useState<SheetFormat>(params.get("format") === "dictation" ? "dictation" : "selftest");
  const dict = format === "dictation";
  const initialTarget = parseTargetStatus(params.get("target"));
  const [sourceKind, setSourceKind] = useState<SourceKind>(initialTarget ? TARGET_KIND[initialTarget] : "unfamiliar");
  const [targetBookId, setTargetBookId] = useState<string>((initialTarget && params.get("bookId")) || ALL_TARGETS);
  const targetStatus = KIND_TARGET[sourceKind];
  const [sessionId, setSessionId] = useState<string>();
  const [bookId, setBookId] = useState<string>();
  const [unitId, setUnitId] = useState<string>();
  const [unitOpen, setUnitOpen] = useState(false);
  const [copies, setCopies] = useState(1);
  const [perSheet, setPerSheet] = useState(30);
  const [rows, setRows] = useState<SheetCandidate[]>([]);
  const [selected, setSelected] = useState<string[]>([]);
  const [modes, setModes] = useState<Mode[]>(["recognition", "spelling"]);
  const [keyword, setKeyword] = useState("");
  const [saving, setSaving] = useState(false);
  const count = copies * perSheet;

  // ---- 默写单（spec 0006） ----
  const [includeWords, setIncludeWords] = useState(true);
  const [includePhrases, setIncludePhrases] = useState(true);
  const [limits, setLimits] = useState<DictLimits>(DICT_DEFAULT_LIMITS);
  const [sBookId, setSBookId] = useState<string>();
  const [sUnitId, setSUnitId] = useState<string>();
  const [textIds, setTextIds] = useState<string[]>([]);
  const [learningOn, setLearningOn] = useState(false);
  const [passageId, setPassageId] = useState<string>();
  const [sentRows, setSentRows] = useState<DictSentenceCandidate[]>([]);
  const [sentSelected, setSentSelected] = useState<string[]>([]);
  const [lines, setLines] = useState<LineStyle>();
  const isSelf = !userId;

  // 「用错词再出一张」：读取那次测试的错词，排在不熟的词前面（默写单的「用错题再出一份」直接用那次批改作来源）
  const retest = useQuery({
    queryKey: ["study", from],
    queryFn: () => api.get<StudySessionData>(`/study/sessions/${from}`),
    enabled: !!from && !dict,
  });
  const include = dict ? [] : (retest.data?.result?.wrongWordIds ?? []);

  const sources = useQuery({
    queryKey: ["sheets", "sources", userId ?? "me"],
    queryFn: () => api.get<SheetSourcesData>("/sheets/sources", userId ? { userId } : {}),
    enabled: sourceKind === "session" || (dict && !from),
  });
  const books = useQuery({ queryKey: ["books", "all"], queryFn: () => api.get<Paged<Book>>("/books"), enabled: sourceKind === "unit" || (dict && !from) });
  const book = useQuery({ queryKey: ["books", bookId], queryFn: () => api.get<BookDetail>(`/books/${bookId}`), enabled: sourceKind === "unit" && !!bookId });
  // 默写单句子来源：单元的篇
  const sBook = useQuery({ queryKey: ["books", sBookId], queryFn: () => api.get<BookDetail>(`/books/${sBookId}`), enabled: dict && !!sBookId });
  const unitTexts = useQuery({
    queryKey: ["sheets", "sources", userId ?? "me", "unit", sUnitId],
    queryFn: () => api.get<SheetSourcesData>("/sheets/sources", { unitId: sUnitId!, ...(userId ? { userId } : {}) }),
    enabled: dict && !!sUnitId,
  });
  // 学生自己的 AI 短文：只有给自己出单时能列出（/passages 只返回本人的）
  const passages = useQuery({
    queryKey: ["passages", "sheet-source"],
    queryFn: () => api.get<Paged<PassageListItem>>("/passages", { page: 1, limit: 50 }),
    enabled: dict && isSelf && !from,
  });
  // 目标词书（学生自己的，或老师给出单的那个学生的）：有目标时才出现两种目标来源
  const coverage = useQuery({
    queryKey: ["records", userId ?? "me", "coverage"],
    queryFn: () => api.get<CoverageData>(withUser("/records/coverage", userId)),
    enabled: !from,
  });
  const targetBooks = coverage.data?.books ?? [];
  const showTargets = targetBooks.length > 0 || !!targetStatus;

  const source: SheetSource | null =
    dict && from
      ? { kind: "session", sessionId: from }
      : targetStatus
        ? { kind: "target", status: targetStatus, bookId: targetBookId === ALL_TARGETS ? undefined : targetBookId }
        : sourceKind === "session"
          ? sessionId
            ? { kind: "session", sessionId }
            : null
          : sourceKind === "unit"
            ? unitId === WHOLE_BOOK && bookId
              ? { kind: "book", bookId }
              : unitId
                ? { kind: "unit", unitId }
                : null
            : { kind: "unfamiliar" };

  const sentenceSources = sentenceSourcesOf({ from, textIds, learning: learningOn, passageId });
  const wordsWanted = includeWords || includePhrases;
  /** 默写单：词来源选完整了才取词；只出句子时不取词 */
  const wordsActive = wordsWanted && !!source;

  const dictBody = {
    userId,
    format: "dictation" as const,
    copies,
    includeWords: wordsActive && includeWords,
    includePhrases: wordsActive && includePhrases,
    source: wordsActive ? source : undefined,
    sentenceSources,
    wordCount: limits.words,
    phraseCount: limits.phrases,
    sentenceCount: limits.sentences,
  };

  const preview = useQuery({
    queryKey: dict
      ? ["sheets", "preview", "dictation", JSON.stringify(dictBody)]
      : ["sheets", "preview", userId ?? "me", count, JSON.stringify(source), include.join(",")],
    queryFn: () =>
      dict
        ? api.post<SheetPreviewData>("/sheets/preview", dictBody)
        : api.post<SheetPreviewData>("/sheets/preview", { userId, count, source, include: source?.kind === "unfamiliar" ? include : undefined }),
    enabled: dict ? wordsActive || sentenceSources.length > 0 : !!source && (!from || retest.isSuccess),
  });

  useEffect(() => {
    if (!source) {
      setRows([]);
      setSelected([]);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sourceKind, sessionId, unitId]);

  useEffect(() => {
    if (dict && !wordsActive && sentenceSources.length === 0) {
      setRows([]);
      setSelected([]);
      setSentRows([]);
      setSentSelected([]);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [dict, wordsActive, sentenceSources.length]);

  useEffect(() => {
    if (!preview.data) return;
    setRows(preview.data.items);
    setSelected(preview.data.items.map((i) => i.wordId));
    const sents = preview.data.sentences ?? [];
    setSentRows(sents);
    setSentSelected(sents.map((s) => s.sentenceId));
  }, [preview.data]);

  const search = useQuery({
    queryKey: ["words", "search", keyword],
    queryFn: () => api.get<Paged<WordHit>>("/words", { q: keyword, page: 1, limit: 20 }),
    enabled: keyword.trim().length > 0,
  });

  const addWord = (id: string) => {
    const w = search.data?.items.find((x) => x.id === id);
    if (!w || rows.some((r) => r.wordId === id)) return;
    const type = dict ? (w.type === "phrase" ? ("phrase" as const) : ("word" as const)) : undefined;
    setRows((rs) => [...rs, { wordId: w.id, spelling: w.spelling, phonetic: w.phonetic, partOfSpeech: w.partOfSpeech, definition: w.definition, score: 0, reasons: [], ...(type ? { type } : {}) }]);
    setSelected((s) => [...s, w.id]);
  };

  const ordered = useMemo(() => rows.filter((r) => selected.includes(r.wordId)).map((r) => r.wordId), [rows, selected]);
  const sizes = batchSizes(ordered.length, copies, perSheet);
  /** 每个已选词分到第几份（从难到易依次分） */
  const sheetOf = useMemo(() => {
    const m = new Map<string, number>();
    let i = 0;
    (sizes ?? []).forEach((n, k) => ordered.slice(i, (i += n)).forEach((id) => m.set(id, k + 1)));
    return m;
  }, [ordered, sizes]);

  // 默写单：已选的题（词按表格顺序，句子在后），按与后端相同的口径切分预告
  const dictItems: DictItemRef[] = useMemo(
    () => [
      ...rows.filter((r) => selected.includes(r.wordId)).map((r) => ({ type: r.type ?? ("word" as const), wordId: r.wordId })),
      ...sentRows.filter((s) => sentSelected.includes(s.sentenceId)).map((s) => ({ type: s.type, sentenceId: s.sentenceId })),
    ],
    [rows, selected, sentRows, sentSelected],
  );
  const dictSplit = useMemo(() => splitDictation(dictItems, copies, limits), [dictItems, copies, limits]);
  const dictSheetOf = useMemo(() => {
    const m = new Map<string, number>();
    if ("groups" in dictSplit) dictSplit.groups.forEach((g, k) => g.forEach((it) => m.set(it.sentenceId ? sentKey(it.sentenceId) : wordKey(it.wordId!), k + 1)));
    return m;
  }, [dictSplit]);

  // 书写线：词书学段为小学时默认四线三格（可改）；打印页也可以再切换
  const level = (sourceKind === "unit" ? book.data?.level : undefined) ?? sBook.data?.level ?? null;
  const lineStyle = lines ?? defaultLineStyle(level);

  const afterCreate = async (res: { id: string; items: { id: string }[] }, withLines?: LineStyle) => {
    await qc.invalidateQueries({ queryKey: ["sheets"] });
    await qc.invalidateQueries({ queryKey: ["today"] });
    const link = res.items.length > 1 ? printLink(res.items.map((x) => x.id)) : `/sheets/${res.id}/print`;
    navigate(withLines ? printLinkWithLines(link, withLines) : link);
  };

  const submit = async () => {
    setSaving(true);
    try {
      if (dict) {
        const res = await api.post<{ id: string; seq: number; items: { id: string; seq: number }[] }>("/sheets", {
          userId,
          format: "dictation",
          items: dictItems,
          copies,
          wordCount: limits.words,
          phraseCount: limits.phrases,
          sentenceCount: limits.sentences,
        });
        await afterCreate(res, lineStyle);
      } else {
        const res = await api.post<{ id: string; seq: number; items: { id: string; seq: number }[] }>("/sheets", { userId, wordIds: ordered, modes, copies, perSheet });
        await afterCreate(res);
      }
    } catch (e) {
      message.error(errorMessage(e));
    } finally {
      setSaving(false);
    }
  };

  const wordEmptyText =
    dict && !wordsWanted
      ? "这次不出单词和短语"
      : targetStatus
        ? `目标词书里没有${COVERAGE_LABEL[targetStatus]}的词，可以搜索手动加词`
        : sourceKind === "session" && !sessionId
          ? "先选一次测试"
          : sourceKind === "unit" && !bookId
            ? "先选一本词书"
            : sourceKind === "unit" && !unitId
              ? "再选一个单元（或整本书），就会列出要背的词"
              : "暂时没有需要加强的词，可以搜索手动加词";

  const title = from ? (dict ? "用错题再出一份" : "用错词再出一张") : dict ? "出默写单" : "生成单词单";
  const lead = dict
    ? from
      ? "那次批改里写错的词和句子都在下面，可以取消勾选，也可以搜索加词。"
      : `${wordsWanted ? SOURCE_HINT[sourceKind] : ""}再选句子来源；要学的句子排在前面，其次是没测过的。打印后看中文写英文，写完由家长或老师批改。`
    : `${SOURCE_HINT[sourceKind]}不想要的取消勾选，也可以搜索加词；排在前面的词更难，分到第 1 份。`;

  const textList = unitTexts.data?.texts ?? [];
  const learningCount = sources.data?.learningSentences ?? 0;

  return (
    <div className="vx-page">
      <PageHeader eyebrow={dict ? "默写单" : "单词单"} title={title}>
        {lead}
      </PageHeader>

      <div className="vx-card" style={{ padding: 16, display: "grid", gridTemplateColumns: "minmax(0, 1fr)", gap: 14, marginBottom: 14 }}>
        {!from && (
          <div style={{ display: "flex", gap: 12, flexWrap: "wrap", alignItems: "center" }}>
            <span>格式</span>
            <Segmented<SheetFormat>
              aria-label="格式"
              value={format}
              onChange={(v) => setFormat(v)}
              options={[
                { label: "自测表", value: "selftest" },
                { label: "默写单", value: "dictation" },
              ]}
            />
            <span style={{ color: "var(--muted)", fontSize: 13 }}>
              {dict ? "看中文写英文，题目页 + 答案页，手写后批改录入；不能在线测试。" : "对折自测表：左英文右中文，看完在电脑上测一遍。"}
            </span>
          </div>
        )}
        {dict && (
          <div style={{ display: "flex", gap: 12, flexWrap: "wrap", alignItems: "center" }}>
            <span>包含</span>
            <Checkbox checked={includeWords} onChange={(e) => setIncludeWords(e.target.checked)}>
              单词
            </Checkbox>
            <Checkbox checked={includePhrases} onChange={(e) => setIncludePhrases(e.target.checked)}>
              短语
            </Checkbox>
          </div>
        )}
        {!from && (!dict || wordsWanted) && (
          <div style={{ display: "flex", gap: 12, flexWrap: "wrap", alignItems: "center" }}>
            <span>{dict ? "词的来源" : "选词来源"}</span>
            {/* 来源选项较多，窄屏上在这一行里横向滑动，不撑宽卡片 */}
            <div style={{ maxWidth: "100%", overflowX: "auto" }}>
              <Segmented<SourceKind>
                value={sourceKind}
                onChange={(v) => setSourceKind(v)}
                options={[
                  { label: "不熟的词", value: "unfamiliar" },
                  { label: "某次测试的错词", value: "session" },
                  { label: "某个单元", value: "unit" },
                  ...(showTargets
                    ? [
                        { label: "目标：未测的词", value: "targetUntested" as SourceKind },
                        { label: "目标：要学的词", value: "targetLearning" as SourceKind },
                      ]
                    : []),
                ]}
              />
            </div>
            {targetStatus && (
              <Select
                style={{ minWidth: 220 }}
                aria-label="目标词书"
                loading={coverage.isLoading}
                value={targetBookId}
                onChange={(v) => setTargetBookId(v)}
                options={[
                  { value: ALL_TARGETS, label: `全部目标词书${coverage.data ? `（${COVERAGE_LABEL[targetStatus]} ${coverage.data.total[targetStatus]}）` : ""}` },
                  ...targetBooks.map((b) => ({ value: b.bookId, label: `${b.name}（${COVERAGE_LABEL[targetStatus]} ${b[targetStatus]}）` })),
                ]}
              />
            )}
            {sourceKind === "session" && (
              <Select
                style={{ minWidth: 320, flex: 1 }}
                placeholder="选一次测试（最近 30 天有错词的）"
                loading={sources.isLoading}
                value={sessionId}
                onChange={setSessionId}
                notFoundContent="最近 30 天没有答错过的测试"
                options={(sources.data?.sessions ?? []).map((x) => ({
                  value: x.id,
                  label: `${x.dayKey} · ${KIND_LABEL[x.kind]} · ${x.planName} · 错 ${x.wrongCount} 词${x.status === "active" ? "（未完成）" : ""}`,
                }))}
              />
            )}
            {sourceKind === "unit" && (
              <>
                <Select
                  style={{ minWidth: 180 }}
                  placeholder="词书"
                  loading={books.isLoading}
                  value={bookId}
                  onChange={(v) => {
                    setBookId(v);
                    setUnitId(undefined);
                    setUnitOpen(true); // 选完词书直接展开单元列表，免得以为选了词书就行
                  }}
                  options={(books.data?.items ?? []).map((b) => ({ value: b.id, label: b.name }))}
                />
                <Select
                  style={{ minWidth: 200 }}
                  placeholder="选单元"
                  disabled={!bookId}
                  open={unitOpen}
                  onDropdownVisibleChange={setUnitOpen}
                  loading={book.isLoading}
                  value={unitId}
                  onChange={setUnitId}
                  options={[
                    ...(book.data?.units.length ? [{ value: WHOLE_BOOK, label: `整本书（${book.data.units.length} 个单元）` }] : []),
                    ...(book.data?.units ?? []).map((u) => ({ value: u.id, label: `${u.name}（${u.wordCount} 词）` })),
                  ]}
                />
              </>
            )}
          </div>
        )}
        {dict && !from && (
          <div style={{ display: "grid", gap: 10 }}>
            <div style={{ display: "flex", gap: 12, flexWrap: "wrap", alignItems: "center" }}>
              <span>句子来源</span>
              <Select
                style={{ minWidth: 180 }}
                placeholder="词书"
                aria-label="句子来源词书"
                loading={books.isLoading}
                value={sBookId}
                onChange={(v) => {
                  setSBookId(v);
                  setSUnitId(undefined);
                  setTextIds([]);
                }}
                options={(books.data?.items ?? []).map((b) => ({ value: b.id, label: b.name }))}
              />
              <Select
                style={{ minWidth: 200 }}
                placeholder="选单元"
                aria-label="句子来源单元"
                disabled={!sBookId}
                loading={sBook.isLoading}
                value={sUnitId}
                onChange={(v) => {
                  setSUnitId(v);
                  setTextIds([]);
                }}
                options={(sBook.data?.units ?? []).map((u) => ({ value: u.id, label: u.name }))}
              />
              <Checkbox checked={learningOn} disabled={learningCount === 0} onChange={(e) => setLearningOn(e.target.checked)}>
                要学的句子（{learningCount}）
              </Checkbox>
              {isSelf && (
                <Select
                  style={{ minWidth: 220 }}
                  placeholder="从我的 AI 短文里挑句子"
                  aria-label="AI 短文"
                  allowClear
                  loading={passages.isLoading}
                  value={passageId}
                  onChange={(v) => setPassageId(v ?? undefined)}
                  notFoundContent="还没有 AI 短文"
                  options={(passages.data?.items ?? []).map((p) => ({ value: p.id, label: p.titleCn ? `${p.title}（${p.titleCn}）` : p.title }))}
                />
              )}
            </div>
            {sUnitId &&
              (unitTexts.isLoading ? (
                <Spin size="small" />
              ) : textList.length === 0 ? (
                <span style={{ color: "var(--muted)", fontSize: 13 }}>这个单元还没有句型或课文</span>
              ) : (
                <Checkbox.Group
                  value={textIds}
                  onChange={(v) => setTextIds(v as string[])}
                  options={textList.map((t) => ({
                    value: t.id,
                    label: `${TEXT_KIND_LABEL[t.kind] ?? t.kind} · ${t.title}（${t.sentenceCount} 句${t.variantCount ? `，含仿写 ${t.variantCount}` : ""}）`,
                  }))}
                />
              ))}
          </div>
        )}
        <div style={{ display: "flex", gap: 16, flexWrap: "wrap", alignItems: "center" }}>
          <span>
            份数 <InputNumber aria-label="份数" min={1} max={7} value={copies} onChange={(v) => setCopies(v ?? 1)} style={{ width: 70 }} />
          </span>
          {dict ? (
            <>
              <span>
                每份单词{" "}
                <InputNumber aria-label="每份单词" min={0} max={DICT_MAX_LIMITS.words} value={limits.words} onChange={(v) => setLimits((l) => ({ ...l, words: v ?? 0 }))} style={{ width: 70 }} />
              </span>
              <span>
                短语{" "}
                <InputNumber aria-label="每份短语" min={0} max={DICT_MAX_LIMITS.phrases} value={limits.phrases} onChange={(v) => setLimits((l) => ({ ...l, phrases: v ?? 0 }))} style={{ width: 70 }} />
              </span>
              <span>
                句子{" "}
                <InputNumber aria-label="每份句子" min={0} max={DICT_MAX_LIMITS.sentences} value={limits.sentences} onChange={(v) => setLimits((l) => ({ ...l, sentences: v ?? 0 }))} style={{ width: 70 }} />
              </span>
              <span style={{ display: "inline-flex", gap: 8, alignItems: "center" }}>
                书写线
                <Segmented<LineStyle>
                  aria-label="书写线"
                  value={lineStyle}
                  onChange={setLines}
                  options={(["line", "fourline"] as LineStyle[]).map((v) => ({ value: v, label: LINE_STYLE_LABEL[v] }))}
                />
              </span>
            </>
          ) : (
            <>
              <span>
                每份词数 <InputNumber aria-label="每份词数" min={10} max={30} step={5} value={perSheet} onChange={(v) => setPerSheet(v ?? 30)} style={{ width: 80 }} />
              </span>
              <span>
                测试题型{" "}
                <Checkbox.Group
                  value={modes}
                  onChange={(v) => setModes(v as Mode[])}
                  options={(["recognition", "spelling", "cloze"] as Mode[]).map((m) => ({ label: MODE_LABEL[m], value: m }))}
                />
              </span>
            </>
          )}
          <Select
            showSearch
            style={{ minWidth: 260, flex: 1 }}
            placeholder="搜索加词（英文或中文）"
            filterOption={false}
            onSearch={setKeyword}
            onSelect={(v) => v && addWord(v)}
            value={null}
            notFoundContent={search.isFetching ? <Spin size="small" /> : keyword ? "没有匹配的单词" : null}
            options={(search.data?.items ?? []).map((w) => ({ value: w.id, label: `${w.spelling} — ${w.definition}` }))}
          />
        </div>
      </div>

      {preview.isError ? (
        <ErrorBlock error={preview.error} onRetry={() => preview.refetch()} />
      ) : (
        <>
          {(!dict || wordsWanted || rows.length > 0) && (
            <Table<SheetCandidate>
              rowKey="wordId"
              size="small"
              loading={preview.isFetching || retest.isLoading}
              dataSource={rows}
              pagination={false}
              rowSelection={{ selectedRowKeys: selected, onChange: (keys) => setSelected(keys as string[]) }}
              locale={{ emptyText: wordEmptyText }}
              columns={[
                ...(dict ? [{ title: "题型", key: "type", width: 70, render: (_: unknown, r: SheetCandidate) => <Tag bordered={false}>{DICT_TYPE_LABEL[r.type ?? "word"]}</Tag> }] : []),
                { title: dict ? "答案" : "单词", dataIndex: "spelling", render: (v: string, r: SheetCandidate) => <span className="vx-word" style={{ fontWeight: 600 }}>{v} <span style={{ color: "var(--muted)", fontWeight: 400 }}>{r.phonetic}</span></span> },
                { title: dict ? "卷面提示" : "释义", dataIndex: "definition", render: (v: string, r: SheetCandidate) => <span className="vx-cn">{r.partOfSpeech} {v}</span> },
                {
                  title: "为什么选它",
                  dataIndex: "reasons",
                  render: (_: unknown, r: SheetCandidate) =>
                    r.reasons.length ? r.reasons.map((x, i) => <Tag key={i} color={REASON_COLOR[x.kind]} bordered={false}>{reasonLabel(x)}</Tag>) : <Tag bordered={false}>手动添加</Tag>,
                },
                ...(copies > 1
                  ? [{ title: "分到", key: "sheet", width: 80, render: (_: unknown, r: SheetCandidate) => {
                      const n = dict ? dictSheetOf.get(wordKey(r.wordId)) : sheetOf.get(r.wordId);
                      return n ? `第 ${n} 份` : "";
                    } }]
                  : []),
              ]}
            />
          )}
          {dict && (
            <div style={{ marginTop: 14 }}>
              <Table<DictSentenceCandidate>
                rowKey="sentenceId"
                size="small"
                loading={preview.isFetching}
                dataSource={sentRows}
                pagination={false}
                rowSelection={{ selectedRowKeys: sentSelected, onChange: (keys) => setSentSelected(keys as string[]) }}
                locale={{ emptyText: sentenceSources.length ? "这些来源里没有句子" : "选一个单元的句型或课文、要学的句子或 AI 短文，就会列出句子" }}
                columns={[
                  { title: "题型", key: "type", width: 70, render: (_: unknown, r: DictSentenceCandidate) => <Tag bordered={false}>{DICT_TYPE_LABEL[r.type]}</Tag> },
                  { title: "卷面提示", dataIndex: "prompt", render: (v: string) => <span className="vx-cn">{v}</span> },
                  { title: "答案", dataIndex: "answer", render: (v: string) => <span className="vx-word">{v}</span> },
                  {
                    title: "状态",
                    key: "status",
                    width: 70,
                    render: (_: unknown, r: DictSentenceCandidate) => (
                      <Tag color={SENTENCE_STATUS[r.status]?.color} bordered={false}>
                        {SENTENCE_STATUS[r.status]?.label ?? r.status}
                      </Tag>
                    ),
                  },
                  ...(copies > 1
                    ? [{ title: "分到", key: "sheet", width: 80, render: (_: unknown, r: DictSentenceCandidate) => {
                        const n = dictSheetOf.get(sentKey(r.sentenceId));
                        return n ? `第 ${n} 份` : "";
                      } }]
                    : []),
                ]}
              />
            </div>
          )}
        </>
      )}

      <div style={{ position: "sticky", bottom: "var(--tabbar-h, 0px)", zIndex: 1, background: "var(--paper)", padding: "12px 0", marginTop: 12, display: "flex", gap: 12, alignItems: "center", flexWrap: "wrap" }}>
        {dict ? (
          <span style={{ flex: 1, color: "error" in dictSplit ? "var(--bad)" : "var(--ink-soft)" }}>
            已选 {dictItems.length} 题
            {"error" in dictSplit
              ? `，${dictSplit.error}，请取消勾选或增加份数`
              : dictSplit.groups.length > 0 && `，生成 ${dictSplit.groups.length} 份（${dictSplit.groups.map((g) => g.length).join(" / ")} 题）`}
          </span>
        ) : (
          <span style={{ flex: 1, color: sizes ? "var(--ink-soft)" : "var(--bad)" }}>
            已选 {ordered.length} 个词
            {sizes
              ? sizes.length > 0 && `，生成 ${sizes.length} 份（${sizes.join(" / ")} 词）`
              : `，超出 ${copies} 份 × ${perSheet} 词 ${ordered.length - copies * perSheet} 个，请取消勾选或增加份数`}
          </span>
        )}
        <Button
          type="primary"
          size="large"
          loading={saving}
          disabled={dict ? "error" in dictSplit || dictItems.length === 0 : !sizes || ordered.length === 0 || modes.length === 0}
          onClick={submit}
        >
          生成并打印
        </Button>
      </div>
    </div>
  );
}
