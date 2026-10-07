import { useEffect, useState } from "preact/hooks";
import { Button, Input, Pagination, Segmented, Tag } from "@/ui";
import { SearchOutlined } from "@/ui";
import { keepPreviousData, useQuery } from "@/lib/query";
import { Link, useNavigate } from "@/lib/router";
import { api } from "@/lib/api";
import type { MemoryWord, Paged, WordFilter } from "@/types";
import { EmptyBlock, ErrorBlock, Loading, MasteryTag, SpeakButton } from "@/components/ui";
import { fmtDate, withUser } from "./shared";

const PAGE_SIZE = 30;

const FILTERS: { label: string; value: WordFilter }[] = [
  { label: "全部", value: "all" },
  { label: "今天到期", value: "due" },
  { label: "难词", value: "difficult" },
  { label: "没记住", value: "missed" },
  { label: "刚记住", value: "learning" },
  { label: "巩固中", value: "consolidating" },
  { label: "已掌握", value: "mastered" },
];

const EMPTY_TEXT: Record<WordFilter, string> = {
  all: "还没有学过的单词",
  due: "今天没有到期的单词",
  difficult: "暂时没有难词（遗忘 ≥ 2 次或难度 ≥ 7）",
  missed: "没有没记住的单词（最近一次答错的词）",
  learning: "没有刚记住的单词",
  consolidating: "没有巩固中的单词",
  mastered: "还没有已掌握的单词（稳定性 ≥ 21 天）",
};

/** 输入防抖 */
function useDebounced<T>(value: T, ms = 350): T {
  const [v, setV] = useState(value);
  useEffect(() => {
    const t = setTimeout(() => setV(value), ms);
    return () => clearTimeout(t);
  }, [value, ms]);
  return v;
}

/**
 * 单词本视图：学生本人（userId 省略）与老师查看学生（传 userId）共用。
 */
export function WordsView({ userId }: { userId?: string }) {
  const navigate = useNavigate();
  const [filter, setFilter] = useState<WordFilter>("all");
  const [input, setInput] = useState("");
  const [page, setPage] = useState(1);
  const q = useDebounced(input.trim());

  // 筛选或搜索变化时回到第一页
  useEffect(() => setPage(1), [filter, q]);

  const words = useQuery({
    queryKey: ["records", userId ?? "me", "words", filter, q, page],
    queryFn: () => {
      const params = new URLSearchParams({ filter, page: String(page), limit: String(PAGE_SIZE) });
      if (q) params.set("q", q);
      return api.get<Paged<MemoryWord>>(withUser(`/records/words?${params.toString()}`, userId));
    },
    placeholderData: keepPreviousData,
  });

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
      <div style={{ display: "flex", gap: 8, flexWrap: "wrap", alignItems: "center", justifyContent: "space-between" }}>
        <div style={{ overflowX: "auto", maxWidth: "100%" }}>
          <Segmented<WordFilter> value={filter} onChange={setFilter} options={FILTERS} />
        </div>
        <Input
          allowClear
          prefix={<SearchOutlined style={{ color: "var(--muted)" }} />}
          placeholder="搜索拼写或释义"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          style={{ width: 220, maxWidth: "100%" }}
        />
      </div>

      {words.isLoading ? (
        <Loading />
      ) : words.error || !words.data ? (
        <ErrorBlock error={words.error} onRetry={() => words.refetch()} />
      ) : words.data.items.length === 0 ? (
        <EmptyBlock
          title={q ? `没有找到“${q}”` : EMPTY_TEXT[filter]}
          description={!q && filter === "all" && !userId ? "学完第一组新词后，单词会出现在这里" : undefined}
          action={
            !q && filter === "all" && !userId ? (
              <Button type="primary" onClick={() => navigate("/today")}>
                去今日开始学习
              </Button>
            ) : undefined
          }
        />
      ) : (
        <>
          <div style={{ fontSize: 13, color: "var(--muted)" }}>共 {words.data.total} 词</div>
          <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(280px, 1fr))", gap: 12 }}>
            {words.data.items.map((w) => (
              <WordCard key={w.wordId} word={w} userId={userId} />
            ))}
          </div>
          {words.data.total > PAGE_SIZE && (
            <div style={{ display: "flex", justifyContent: "center", marginTop: 4 }}>
              <Pagination size="small" current={page} pageSize={PAGE_SIZE} total={words.data.total} onChange={setPage} showSizeChanger={false} />
            </div>
          )}
        </>
      )}
    </div>
  );
}

function WordCard({ word: w, userId }: { word: MemoryWord; userId?: string }) {
  return (
    <Link to={withUser(`/words/${w.wordId}`, userId)} className="vx-card" style={{ display: "block", padding: "14px 16px", color: "inherit" }}>
      <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
        <span className="vx-word" style={{ fontSize: 22, fontWeight: 600, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", minWidth: 0 }}>
          {w.spelling}
        </span>
        <SpeakButton text={w.spelling} wordId={w.wordId} size="small" />
        <span style={{ marginLeft: "auto" }}>
          <MasteryTag level={w.level} />
        </span>
      </div>
      <div style={{ fontSize: 12, color: "var(--muted)", marginTop: 2 }}>
        {[w.phonetic, w.partOfSpeech].filter(Boolean).join("  ")}
      </div>
      <div className="vx-cn" style={{ marginTop: 6, color: "var(--ink-soft)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
        {w.definition}
      </div>
      <div style={{ display: "flex", alignItems: "center", gap: 8, marginTop: 10, fontSize: 12, color: "var(--muted)", flexWrap: "wrap" }}>
        {w.isDue && w.forgetting ? (
          <Tag color="red" bordered={false} style={{ margin: 0 }}>
            可能忘了
          </Tag>
        ) : w.isDue ? (
          <Tag color="gold" bordered={false} style={{ margin: 0 }}>
            待复查
          </Tag>
        ) : (
          <span>下次复习 {fmtDate(w.due)}</span>
        )}
        <span>
          复习 {w.reps} 次 · 遗忘 {w.lapses} 次
        </span>
      </div>
    </Link>
  );
}
