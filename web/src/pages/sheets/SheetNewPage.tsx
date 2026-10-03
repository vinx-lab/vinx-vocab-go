import { useEffect, useMemo, useState } from "preact/hooks";
import { useNavigate, useSearchParams } from "@/lib/router";
import { useQuery, useQueryClient } from "@/lib/query";
import { Button, Checkbox, InputNumber, Segmented, Select, Spin, Table, Tag, useApp } from "@/ui";
import { api, errorMessage } from "@/lib/api";
import { ErrorBlock, PageHeader } from "@/components/ui";
import type { Book, BookDetail, CoverageData, Mode, Paged, SheetCandidate, SheetSource, SheetSourceSession, StudySessionData, TargetSheetStatus } from "@/types";
import { KIND_LABEL, MODE_LABEL } from "@/types";
import { COVERAGE_LABEL, parseTargetStatus } from "@/pages/coverage/coverage";
import { withUser } from "@/pages/records/shared";
import { batchSizes, printLink } from "./links";
import { reasonLabel, REASON_COLOR } from "./reasons";

interface WordHit { id: string; spelling: string; definition: string; phonetic: string | null; partOfSpeech: string | null }

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

/** 生成单词单：选来源 → 系统预选（从难到易）→ 勾掉/加词 → 份数与每份词数 → 生成并合并打印 */
export function SheetNewPage() {
  const [params] = useSearchParams();
  const userId = params.get("userId") ?? undefined;
  const from = params.get("from") ?? undefined;
  const navigate = useNavigate();
  const qc = useQueryClient();
  const { message } = useApp();
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

  // 「用错词再出一张」：读取那次测试的错词，排在不熟的词前面
  const retest = useQuery({
    queryKey: ["study", from],
    queryFn: () => api.get<StudySessionData>(`/study/sessions/${from}`),
    enabled: !!from,
  });
  const include = retest.data?.result?.wrongWordIds ?? [];

  const sources = useQuery({
    queryKey: ["sheets", "sources", userId ?? "me"],
    queryFn: () => api.get<{ sessions: SheetSourceSession[] }>("/sheets/sources", userId ? { userId } : {}),
    enabled: sourceKind === "session",
  });
  const books = useQuery({ queryKey: ["books", "all"], queryFn: () => api.get<Paged<Book>>("/books"), enabled: sourceKind === "unit" });
  const book = useQuery({ queryKey: ["books", bookId], queryFn: () => api.get<BookDetail>(`/books/${bookId}`), enabled: sourceKind === "unit" && !!bookId });
  // 目标词书（学生自己的，或老师给出单的那个学生的）：有目标时才出现两种目标来源
  const coverage = useQuery({
    queryKey: ["records", userId ?? "me", "coverage"],
    queryFn: () => api.get<CoverageData>(withUser("/records/coverage", userId)),
    enabled: !from,
  });
  const targetBooks = coverage.data?.books ?? [];
  const showTargets = targetBooks.length > 0 || !!targetStatus;

  const source: SheetSource | null = targetStatus
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

  const preview = useQuery({
    queryKey: ["sheets", "preview", userId ?? "me", count, JSON.stringify(source), include.join(",")],
    queryFn: () => api.post<{ items: SheetCandidate[] }>("/sheets/preview", { userId, count, source, include: source?.kind === "unfamiliar" ? include : undefined }),
    enabled: !!source && (!from || retest.isSuccess),
  });

  useEffect(() => {
    if (!source) {
      setRows([]);
      setSelected([]);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sourceKind, sessionId, unitId]);

  useEffect(() => {
    if (!preview.data) return;
    setRows(preview.data.items);
    setSelected(preview.data.items.map((i) => i.wordId));
  }, [preview.data]);

  const search = useQuery({
    queryKey: ["words", "search", keyword],
    queryFn: () => api.get<Paged<WordHit>>("/words", { q: keyword, page: 1, limit: 20 }),
    enabled: keyword.trim().length > 0,
  });

  const addWord = (id: string) => {
    const w = search.data?.items.find((x) => x.id === id);
    if (!w || rows.some((r) => r.wordId === id)) return;
    setRows((rs) => [...rs, { wordId: w.id, spelling: w.spelling, phonetic: w.phonetic, partOfSpeech: w.partOfSpeech, definition: w.definition, score: 0, reasons: [] }]);
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

  const submit = async () => {
    setSaving(true);
    try {
      const res = await api.post<{ id: string; seq: number; items: { id: string; seq: number }[] }>("/sheets", { userId, wordIds: ordered, modes, copies, perSheet });
      await qc.invalidateQueries({ queryKey: ["sheets"] });
      await qc.invalidateQueries({ queryKey: ["today"] });
      navigate(res.items.length > 1 ? printLink(res.items.map((x) => x.id)) : `/sheets/${res.id}/print`);
    } catch (e) {
      message.error(errorMessage(e));
    } finally {
      setSaving(false);
    }
  };

  const emptyText = targetStatus
    ? `目标词书里没有${COVERAGE_LABEL[targetStatus]}的词，可以搜索手动加词`
    : sourceKind === "session" && !sessionId ? "先选一次测试" : sourceKind === "unit" && !bookId ? "先选一本词书" : sourceKind === "unit" && !unitId ? "再选一个单元（或整本书），就会列出要背的词" : "暂时没有需要加强的词，可以搜索手动加词";

  return (
    <div className="vx-page">
      <PageHeader eyebrow="单词单" title={from ? "用错词再出一张" : "生成单词单"}>
        {SOURCE_HINT[sourceKind]}不想要的取消勾选，也可以搜索加词；排在前面的词更难，分到第 1 份。
      </PageHeader>

      <div className="vx-card" style={{ padding: 16, display: "grid", gap: 14, marginBottom: 14 }}>
        {!from && (
          <div style={{ display: "flex", gap: 12, flexWrap: "wrap", alignItems: "center" }}>
            <span>选词来源</span>
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
        <div style={{ display: "flex", gap: 16, flexWrap: "wrap", alignItems: "center" }}>
          <span>
            份数 <InputNumber aria-label="份数" min={1} max={7} value={copies} onChange={(v) => setCopies(v ?? 1)} style={{ width: 70 }} />
          </span>
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
        <Table<SheetCandidate>
          rowKey="wordId"
          size="small"
          loading={preview.isFetching || retest.isLoading}
          dataSource={rows}
          pagination={false}
          rowSelection={{ selectedRowKeys: selected, onChange: (keys) => setSelected(keys as string[]) }}
          locale={{ emptyText }}
          columns={[
            { title: "单词", dataIndex: "spelling", render: (v: string, r) => <span className="vx-word" style={{ fontWeight: 600 }}>{v} <span style={{ color: "var(--muted)", fontWeight: 400 }}>{r.phonetic}</span></span> },
            { title: "释义", dataIndex: "definition", render: (v: string, r) => <span className="vx-cn">{r.partOfSpeech} {v}</span> },
            {
              title: "为什么选它",
              dataIndex: "reasons",
              render: (_: unknown, r) =>
                r.reasons.length ? r.reasons.map((x, i) => <Tag key={i} color={REASON_COLOR[x.kind]} bordered={false}>{reasonLabel(x)}</Tag>) : <Tag bordered={false}>手动添加</Tag>,
            },
            ...(copies > 1 ? [{ title: "分到", key: "sheet", width: 80, render: (_: unknown, r: SheetCandidate) => (sheetOf.get(r.wordId) ? `第 ${sheetOf.get(r.wordId)} 份` : "") }] : []),
          ]}
        />
      )}

      <div style={{ position: "sticky", bottom: 0, background: "var(--paper)", padding: "12px 0", marginTop: 12, display: "flex", gap: 12, alignItems: "center", flexWrap: "wrap" }}>
        <span style={{ flex: 1, color: sizes ? "var(--ink-soft)" : "var(--bad)" }}>
          已选 {ordered.length} 个词
          {sizes
            ? sizes.length > 0 && `，生成 ${sizes.length} 份（${sizes.join(" / ")} 词）`
            : `，超出 ${copies} 份 × ${perSheet} 词 ${ordered.length - copies * perSheet} 个，请取消勾选或增加份数`}
        </span>
        <Button type="primary" size="large" loading={saving} disabled={!sizes || ordered.length === 0 || modes.length === 0} onClick={submit}>
          生成并打印
        </Button>
      </div>
    </div>
  );
}
