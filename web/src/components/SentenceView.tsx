/**
 * 句子的展示组件（spec 0004）：带词高亮的英文、点开后的译文与逐词释义、单词详情里的「出现在这些句子里」。
 */
import { useState } from "preact/hooks";
import { Button, Spin, Switch, Tag, SoundOutlined } from "@/ui";
import { useQuery } from "@/lib/query";
import { Link } from "@/lib/router";
import { api } from "@/lib/api";
import { groupParagraphs, highlightSegments, limitWordSentences } from "@/lib/sentences";
import type { Sentence, SentenceWordRef, WordDetail, WordSentenceItem, WordSentences } from "@/types";
import { speak } from "@/components/ui";

/**
 * 英文句子：关联到的词加下划虚线，目标词（targetId 或 targets 里的）高亮。
 * onWord 给出时，点关联词回调（比如朗读），不冒泡到句子。
 */
export function SentenceText({
  en,
  words,
  targetId,
  targets,
  onWord,
}: {
  en: string;
  words: SentenceWordRef[];
  targetId?: string;
  targets?: Set<string>;
  onWord?: (wordId: string, text: string) => void;
}) {
  const segs = highlightSegments(en, words, targetId);
  return (
    <>
      {segs.map((s, i) => {
        if (!s.wordId) return <span key={i}>{s.text}</span>;
        const hot = s.target || targets?.has(s.wordId);
        const style = hot
          ? { background: "var(--primary-soft)", color: "var(--primary)", fontWeight: 600, borderRadius: 4, padding: "0 2px" }
          : { textDecoration: "underline dotted", textDecorationColor: "var(--muted)", textUnderlineOffset: "3px" };
        return (
          <span
            key={i}
            data-word-id={s.wordId}
            className={hot ? "vx-sentence-target" : "vx-sentence-word"}
            style={{ ...style, cursor: onWord ? "pointer" : undefined }}
            onClick={
              onWord
                ? (e) => {
                    e.stopPropagation();
                    onWord(s.wordId!, s.text);
                  }
                : undefined
            }
          >
            {s.text}
          </span>
        );
      })}
    </>
  );
}

/** 句中一个词的释义（按需取 GET /words/:id） */
function WordGloss({ wordId, form }: { wordId: string; form: string }) {
  const q = useQuery({
    queryKey: ["words", "detail", wordId],
    queryFn: () => api.get<WordDetail>(`/words/${wordId}`),
    staleTime: 5 * 60_000,
    keepPrevious: false,
    retry: 1,
  });
  const w = q.data;
  return (
    <div style={{ display: "flex", alignItems: "baseline", gap: 8, flexWrap: "wrap", padding: "2px 0" }}>
      <button
        type="button"
        onClick={() => speak(w?.spelling ?? form, wordId)}
        className="vx-word"
        style={{ border: "none", background: "none", padding: 0, cursor: "pointer", fontWeight: 600, color: "var(--primary)", fontSize: 15 }}
      >
        {w?.spelling ?? form}
      </button>
      {w && w.spelling.toLowerCase() !== form.toLowerCase() && <span style={{ color: "var(--muted)", fontSize: 12 }}>（句中：{form}）</span>}
      {w?.partOfSpeech && <span style={{ color: "var(--accent)", fontStyle: "italic", fontFamily: "var(--serif-en)" }}>{w.partOfSpeech}</span>}
      <span className="vx-cn" style={{ color: "var(--ink-soft)" }}>
        {w ? w.definition : q.isError ? "释义不可用" : <Spin size="small" />}
      </span>
    </div>
  );
}

/** 点开一句后的内容：译文 + 朗读 + 逐词释义 */
export function SentenceGloss({ sentence }: { sentence: Pick<Sentence, "en" | "cn" | "words"> }) {
  // 同一个词在句中出现多次时只列一次
  const seen = new Set<string>();
  const words = sentence.words.filter((w) => (seen.has(w.wordId) ? false : (seen.add(w.wordId), true)));
  return (
    <div className="vx-sentence-gloss" style={{ background: "var(--paper)", border: "1px solid var(--line)", borderRadius: 8, padding: "10px 12px", margin: "6px 0 10px" }}>
      <div style={{ display: "flex", gap: 8, alignItems: "flex-start" }}>
        <div className="vx-cn" style={{ flex: 1, color: "var(--ink)", fontSize: 15, lineHeight: 1.7 }}>
          {sentence.cn}
        </div>
        <Button
          size="small"
          shape="circle"
          icon={<SoundOutlined />}
          aria-label="朗读这一句"
          onClick={(e) => {
            e.stopPropagation();
            speak(sentence.en);
          }}
        />
      </div>
      {words.length > 0 && (
        <div style={{ marginTop: 6, paddingTop: 6, borderTop: "1px dashed var(--line)" }}>
          {words.map((w) => (
            <WordGloss key={w.wordId} wordId={w.wordId} form={w.form} />
          ))}
        </div>
      )}
    </div>
  );
}

/**
 * 课文 / 短文：按段落连起来显示，点一句显示这一句的译文和逐词释义；整篇译文开关
 * （showCn 传入时由外面控制，不显示自带的开关）。targets 里的词高亮，点关联词默认朗读。
 */
export function TextReader({
  sentences,
  targets,
  onWord,
  showCn: showCnProp,
}: {
  sentences: Sentence[];
  targets?: Set<string>;
  onWord?: (wordId: string, text: string) => void;
  showCn?: boolean;
}) {
  const [open, setOpen] = useState<string | null>(null);
  const [showCnState, setShowCn] = useState(false);
  const showCn = showCnProp ?? showCnState;
  if (sentences.length === 0) return <div style={{ color: "var(--muted)" }}>还没有句子</div>;
  const paragraphs = groupParagraphs(sentences);
  const opened = sentences.find((s) => s.id === open);
  const toggle = (id: string) => setOpen((o) => (o === id ? null : id));
  return (
    <div className="vx-text-reader">
      {showCnProp === undefined && (
        <div style={{ display: "flex", justifyContent: "flex-end", alignItems: "center", gap: 6, fontSize: 13, color: "var(--ink-soft)", marginBottom: 6 }}>
          整篇译文 <Switch size="small" checked={showCn} onChange={setShowCn} />
        </div>
      )}
      {paragraphs.map((para, pi) => (
        <div key={pi} style={{ marginBottom: 14 }}>
          <p className="vx-word" style={{ fontSize: 18, lineHeight: 1.9, margin: 0 }}>
            {para.map((s) => (
              <span
                key={s.id}
                role="button"
                tabIndex={0}
                className="vx-text-sentence"
                aria-pressed={open === s.id}
                onClick={() => toggle(s.id)}
                onKeyDown={(e) => e.key === "Enter" && toggle(s.id)}
                style={{ cursor: "pointer", borderRadius: 4, background: open === s.id ? "var(--paper-deep)" : undefined, padding: "1px 0" }}
              >
                <SentenceText en={s.en} words={s.words} targets={targets} onWord={onWord ?? ((id, text) => speak(text, id))} />{" "}
              </span>
            ))}
          </p>
          {opened && para.includes(opened) && <SentenceGloss sentence={opened} />}
          {showCn && (
            <p className="vx-cn" style={{ color: "var(--ink-soft)", margin: "4px 0 0", lineHeight: 1.8 }}>
              {para.map((s) => s.cn).join("")}
            </p>
          )}
        </div>
      ))}
    </div>
  );
}

/** 句子出处；links 为假时只显示文字（学习页里不跳走） */
function fromLabel(it: WordSentenceItem, wordId: string, links: boolean) {
  const f = it.from;
  if (f.type === "example") return f.wordId && f.wordId !== wordId ? `「${f.spelling}」的例句` : "本词例句";
  const [text, to] = f.type === "passage" ? [`我的短文 · ${f.title}`, `/passages/${f.passageId}`] : [[f.bookName, f.unitName, f.title].filter(Boolean).join(" · "), `/books/${f.bookId}`];
  return links ? (
    <Link to={to} style={{ color: "inherit" }}>
      {text}
    </Link>
  ) : (
    text
  );
}

/** 单词详情的「出现在这些句子里」：最多 5 条，可以展开全部；目标词高亮，点句子看译文 */
export function WordSentencesBlock({ wordId, limit = 5, title = "出现在这些句子里", links = true }: { wordId: string; limit?: number; title?: string; links?: boolean }) {
  const [all, setAll] = useState(false);
  const [open, setOpen] = useState<string | null>(null);
  const q = useQuery({
    queryKey: ["words", "sentences", wordId],
    queryFn: () => api.get<WordSentences>(`/words/${wordId}/sentences`),
    keepPrevious: false,
    retry: 1,
  });
  if (q.isLoading) return <Spin size="small" />;
  if (q.isError || !q.data) return <div style={{ color: "var(--muted)", fontSize: 13 }}>句子加载失败</div>;
  const { total, groups } = limitWordSentences(q.data, all ? null : limit);
  return (
    <div className="vx-word-sentences">
      {title && (
        <div style={{ fontWeight: 600, marginBottom: 6 }}>
          {title}
          <span style={{ color: "var(--muted)", fontWeight: 400, fontSize: 12, marginLeft: 6 }}>{total} 句</span>
        </div>
      )}
      {total === 0 ? (
        <div style={{ color: "var(--muted)", fontSize: 13 }}>还没有包含这个词的句子</div>
      ) : (
        groups.map((g) => (
          <div key={g.key} style={{ marginBottom: 8 }}>
            <Tag bordered={false} style={{ marginBottom: 4 }}>
              {g.label}
            </Tag>
            {g.items.map((it, i) => {
              const k = `${g.key}-${it.id}-${i}`;
              return (
                <div key={k} style={{ padding: "4px 0" }}>
                  <div
                    role="button"
                    tabIndex={0}
                    className="vx-example-en is-compact"
                    style={{ cursor: "pointer" }}
                    onClick={() => setOpen((o) => (o === k ? null : k))}
                    onKeyDown={(e) => (e.key === "Enter" ? setOpen((o) => (o === k ? null : k)) : undefined)}
                  >
                    <SentenceText en={it.en} words={it.words} targetId={wordId} />
                  </div>
                  <div style={{ fontSize: 12, color: "var(--muted)" }}>{fromLabel(it, wordId, links)}</div>
                  {open === k && <SentenceGloss sentence={it} />}
                </div>
              );
            })}
          </div>
        ))
      )}
      {total > limit && (
        <Button type="link" size="small" style={{ padding: 0 }} onClick={() => setAll((v) => !v)}>
          {all ? "收起" : `展开全部 ${total} 句`}
        </Button>
      )}
    </div>
  );
}
