/** 目标词书与覆盖进度（spec 0003）的共用组件：三段进度条、目标词书编辑、记录页分书进度与词表 */
import type { ComponentChildren } from "preact";
import { useEffect, useState } from "preact/hooks";
import { Button, DeleteOutlined, DownOutlined, HolderOutlined, Pagination, Segmented, Select, Tag, UpOutlined } from "@/ui";
import { keepPreviousData, useQuery } from "@/lib/query";
import { api } from "@/lib/api";
import type { Book, CoverageCounts, CoverageData, CoverageStatus, CoverageWord, Paged, TargetBook } from "@/types";
import { EmptyBlock, ErrorBlock, Loading } from "@/components/ui";
import { Legend } from "@/components/charts";
import { withUser } from "@/pages/records/shared";
import { COVERAGE_COLOR, COVERAGE_LABEL, COVERAGE_TAG, moveItem } from "./coverage";

const STATUS_ORDER: CoverageStatus[] = ["known", "learning", "untested"];

/** 三段进度条：会了 / 要学 / 未测 */
export function CoverageBar({ counts, height = 10 }: { counts: Pick<CoverageCounts, "known" | "learning" | "untested">; height?: number }) {
  const total = counts.known + counts.learning + counts.untested;
  return (
    <div
      style={{ display: "flex", gap: 2, height, borderRadius: height / 2, overflow: "hidden", background: "var(--track)" }}
      role="img"
      aria-label={STATUS_ORDER.map((s) => `${COVERAGE_LABEL[s]} ${counts[s]}`).join("，")}
    >
      {total > 0 &&
        STATUS_ORDER.map((s) => (counts[s] > 0 ? <div key={s} title={`${COVERAGE_LABEL[s]} ${counts[s]} 词`} style={{ flex: counts[s], background: COVERAGE_COLOR[s] }} /> : null))}
    </div>
  );
}

export function CoverageLegend() {
  return (
    <div style={{ display: "flex", gap: 16, fontSize: 12, color: "var(--ink-soft)", flexWrap: "wrap" }}>
      {STATUS_ORDER.map((s) => (
        <Legend key={s} color={COVERAGE_COLOR[s]} label={COVERAGE_LABEL[s]} />
      ))}
    </div>
  );
}

/** 「会了 x · 要学 y · 未测 z」 */
export function coverageLine(c: Pick<CoverageCounts, "known" | "learning" | "untested">): string {
  return STATUS_ORDER.map((s) => `${COVERAGE_LABEL[s]} ${c[s]}`).join(" · ");
}

/**
 * 目标词书编辑：从可见词书里添加，拖动（或上下按钮，手机上用）排序，保存时整体替换。
 * readOnly 时只列出词书。excludeIds 不出现在「添加词书」里（例如已由班级设置的书，spec 0008）；
 * emptyText 为空列表时的提示；rowExtra 在每本书后面附加内容（如「由班级 ×× 设置」）。
 */
export function TargetBooksEditor({
  value,
  readOnly,
  saving,
  onSave,
  excludeIds,
  emptyText,
  rowExtra,
}: {
  value: TargetBook[];
  readOnly?: boolean;
  saving?: boolean;
  onSave?: (bookIds: string[]) => void;
  excludeIds?: string[];
  emptyText?: string;
  rowExtra?: (b: TargetBook) => ComponentChildren;
}) {
  const [list, setList] = useState<TargetBook[]>(value);
  const [dragIndex, setDragIndex] = useState<number | null>(null);
  const valueKey = value.map((b) => b.id).join(",");
  useEffect(() => setList(value), [valueKey]); // eslint-disable-line react-hooks/exhaustive-deps

  const books = useQuery({ queryKey: ["books", "all"], queryFn: () => api.get<Paged<Book>>("/books"), enabled: !readOnly });
  const dirty = list.map((b) => b.id).join(",") !== valueKey;
  const options = (books.data?.items ?? []).filter((b) => !list.some((x) => x.id === b.id) && !excludeIds?.includes(b.id)).map((b) => ({ value: b.id, label: `${b.name}（${b.wordCount} 词）` }));

  const add = (id: string) => {
    const b = books.data?.items.find((x) => x.id === id);
    if (b && !list.some((x) => x.id === id)) setList((l) => [...l, { id: b.id, name: b.name }]);
  };
  const move = (from: number, to: number) => setList((l) => moveItem(l, from, to));

  return (
    <div style={{ display: "grid", gap: 12 }}>
      {list.length === 0 ? (
        <div style={{ color: "var(--muted)", padding: "8px 0" }}>{emptyText ?? (readOnly ? "还没有设置目标词书" : "还没有目标词书，从下面添加")}</div>
      ) : (
        <div style={{ border: "1px solid var(--line)", borderRadius: 10, overflow: "hidden" }}>
          {list.map((b, i) => (
            <div
              key={b.id}
              draggable={!readOnly}
              onDragStart={() => setDragIndex(i)}
              onDragOver={(e) => !readOnly && e.preventDefault()}
              onDrop={() => dragIndex !== null && move(dragIndex, i)}
              onDragEnd={() => setDragIndex(null)}
              style={{
                display: "flex",
                alignItems: "center",
                gap: 8,
                padding: "8px 10px",
                borderTop: i ? "1px solid var(--line)" : "none",
                background: dragIndex === i ? "var(--primary-soft)" : undefined,
              }}
            >
              {!readOnly && <HolderOutlined aria-label="拖动排序" style={{ color: "var(--muted)", cursor: "grab" }} />}
              <span className="vx-num" style={{ color: "var(--muted)", width: 18, fontSize: 12 }}>
                {i + 1}
              </span>
              <span style={{ flex: 1, minWidth: 0, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{b.name}</span>
              {rowExtra?.(b)}
              {!readOnly && (
                <>
                  <Button size="small" type="text" icon={<UpOutlined />} aria-label={`上移 ${b.name}`} disabled={i === 0} onClick={() => move(i, i - 1)} />
                  <Button size="small" type="text" icon={<DownOutlined />} aria-label={`下移 ${b.name}`} disabled={i === list.length - 1} onClick={() => move(i, i + 1)} />
                  <Button size="small" type="text" danger icon={<DeleteOutlined />} aria-label={`移除 ${b.name}`} onClick={() => setList((l) => l.filter((x) => x.id !== b.id))} />
                </>
              )}
            </div>
          ))}
        </div>
      )}
      {!readOnly && (
        <div style={{ display: "flex", gap: 8, flexWrap: "wrap", alignItems: "center" }}>
          <Select
            showSearch
            optionFilterProp="label"
            style={{ flex: 1, minWidth: 200 }}
            placeholder="添加词书"
            aria-label="添加词书"
            loading={books.isLoading}
            value={null}
            onSelect={(v) => v && add(v as string)}
            notFoundContent="没有可添加的词书"
            options={options}
          />
          <Button type="primary" loading={saving} disabled={!dirty} onClick={() => onSave?.(list.map((b) => b.id))}>
            保存
          </Button>
          {dirty && <Button onClick={() => setList(value)}>撤销修改</Button>}
        </div>
      )}
    </div>
  );
}

const WORDS_PAGE = 20;

/** 记录页「目标词书」区块：每本书一行三段进度条，点开按状态看词表。学生本人与老师查看学生共用 */
export function CoverageSection({ userId }: { userId?: string }) {
  const q = useQuery({
    queryKey: ["records", userId ?? "me", "coverage"],
    queryFn: () => api.get<CoverageData>(withUser("/records/coverage", userId)),
  });
  const [open, setOpen] = useState<string | null>(null);

  if (q.isLoading) return null;
  if (q.error || !q.data) return <ErrorBlock error={q.error} onRetry={() => q.refetch()} />;
  const c = q.data;
  if (c.books.length === 0) return null;

  return (
    <div className="vx-card" style={{ padding: 16 }}>
      <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 8, marginBottom: 12, flexWrap: "wrap" }}>
        <h3 className="vx-title" style={{ fontSize: 17 }}>
          目标词书
        </h3>
        <span style={{ fontSize: 13, color: "var(--ink-soft)" }}>
          已测 <b className="vx-num">{c.total.tested}</b> / {c.total.target} · {coverageLine(c.total)}
        </span>
      </div>
      <div style={{ display: "flex", flexDirection: "column" }}>
        {c.books.map((b) => (
          <div key={b.bookId} style={{ borderTop: "1px solid var(--line)" }}>
            <button
              type="button"
              aria-expanded={open === b.bookId}
              onClick={() => setOpen(open === b.bookId ? null : b.bookId)}
              style={{ all: "unset", boxSizing: "border-box", width: "100%", cursor: "pointer", padding: "10px 4px", display: "grid", gap: 6 }}
            >
              <div style={{ display: "flex", alignItems: "baseline", gap: 8, flexWrap: "wrap" }}>
                <span style={{ fontWeight: 600, flex: 1, minWidth: 0 }}>{b.name}</span>
                <span className="vx-num" style={{ fontSize: 13 }}>
                  {b.tested}/{b.target}
                </span>
                <span style={{ fontSize: 12, color: "var(--muted)" }}>{coverageLine(b)}</span>
                {open === b.bookId ? <UpOutlined style={{ fontSize: 11, color: "var(--muted)" }} /> : <DownOutlined style={{ fontSize: 11, color: "var(--muted)" }} />}
              </div>
              <CoverageBar counts={b} />
            </button>
            {open === b.bookId && <CoverageWords userId={userId} bookId={b.bookId} counts={b} />}
          </div>
        ))}
      </div>
      <div style={{ marginTop: 10 }}>
        <CoverageLegend />
      </div>
    </div>
  );
}

function CoverageWords({ userId, bookId, counts }: { userId?: string; bookId: string; counts: CoverageCounts }) {
  const [status, setStatus] = useState<CoverageStatus>(counts.learning > 0 ? "learning" : counts.untested > 0 ? "untested" : "known");
  const [page, setPage] = useState(1);
  const words = useQuery({
    queryKey: ["records", userId ?? "me", "coverage", "words", bookId, status, page],
    queryFn: () => api.get<Paged<CoverageWord>>(withUser(`/records/coverage/words?bookId=${encodeURIComponent(bookId)}&status=${status}&page=${page}&limit=${WORDS_PAGE}`, userId)),
    placeholderData: keepPreviousData,
  });

  return (
    <div style={{ padding: "4px 4px 14px" }}>
      <Segmented<CoverageStatus>
        size="small"
        value={status}
        onChange={(v) => {
          setStatus(v);
          setPage(1);
        }}
        options={(["learning", "untested", "known"] as CoverageStatus[]).map((s) => ({ label: `${COVERAGE_LABEL[s]} ${counts[s]}`, value: s }))}
      />
      <div style={{ marginTop: 8 }}>
        {words.isLoading ? (
          <Loading />
        ) : words.error || !words.data ? (
          <ErrorBlock error={words.error} onRetry={() => words.refetch()} />
        ) : words.data.items.length === 0 ? (
          <EmptyBlock title={`没有${COVERAGE_LABEL[status]}的词`} />
        ) : (
          <>
            {words.data.items.map((w) => (
              <div key={w.wordId} style={{ display: "flex", alignItems: "baseline", gap: 10, padding: "6px 0", borderTop: "1px dashed var(--line)" }}>
                <span className="vx-word" style={{ fontWeight: 600, minWidth: 90 }}>
                  {w.spelling}
                </span>
                <span className="vx-cn" style={{ flex: 1, minWidth: 0, fontSize: 13, color: "var(--ink-soft)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                  {w.partOfSpeech} {w.definition}
                </span>
                <Tag color={COVERAGE_TAG[w.status]} bordered={false}>
                  {COVERAGE_LABEL[w.status]}
                </Tag>
              </div>
            ))}
            {words.data.total > WORDS_PAGE && (
              <div style={{ display: "flex", justifyContent: "center", marginTop: 8 }}>
                <Pagination size="small" current={page} pageSize={WORDS_PAGE} total={words.data.total} onChange={setPage} showSizeChanger={false} />
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}
