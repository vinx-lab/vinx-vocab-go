import type { ComponentChildren } from "preact";
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "preact/hooks";
import { useNavigate, useParams } from "@/lib/router";
import { useMutation, useQuery, useQueryClient } from "@/lib/query";
import { Button, Input, Modal, Progress, Switch, Tooltip, useApp } from "@/ui";
import { ArrowLeftOutlined, ArrowRightOutlined, BulbOutlined, CheckCircleFilled, CloseCircleFilled, CloseOutlined } from "@/ui";
import { isTestKind } from "@vinx/shared";
import { api, errorMessage } from "@/lib/api";
import { ErrorBlock, Loading, SpeakButton, percent, speak, stopSpeaking, formatDuration } from "@/components/ui";
import type { Mode, SessionAnswer, SessionItem, StudySessionData, TodayData } from "@/types";
import { KIND_LABEL, MODE_LABEL } from "@/types";
import { deriveFlow, type Question } from "./engine";
import { regenerateLink } from "@/pages/sheets/links";
import { WordSentencesBlock } from "@/components/SentenceView";

// 迁移说明：键盘监听与「快照 → 本地状态」同步用 useLayoutEffect。Preact 的 useEffect 要到下一帧才执行，
// 连续快速按键时监听器里的闭包（当前是否最后一张卡、是否已作答）会是旧的；React 对键盘等离散事件会同步执行 effect，
// 用 layout effect 才能保持与旧版一致的行为。
const AUTO_SPEAK_KEY = "vx-autospeak";
const RATING_NAME_LABEL: Record<string, string> = { again: "忘记", hard: "模糊", good: "记得", easy: "熟练" };

function readAutoSpeak(): boolean {
  try {
    return localStorage.getItem(AUTO_SPEAK_KEY) !== "0";
  } catch {
    return true;
  }
}

interface AnswerResponse {
  recorded: boolean;
  correct?: boolean;
  expected?: string;
}

/** 沉浸式学习页：认识 → 练习 → 巩固 / 检测 → 结果 */
export function StudyPage() {
  const { id = "" } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const { message } = useApp();
  const [autoSpeak, setAutoSpeak] = useState(readAutoSpeak);

  const query = useQuery({
    queryKey: ["study", id],
    queryFn: () => api.get<StudySessionData>(`/study/sessions/${id}`),
    staleTime: Infinity,
  });
  const session = query.data;

  // 本地作答列表（乐观追加），与服务端记录合并推导流程
  const [answers, setAnswers] = useState<SessionAnswer[]>([]);
  const [cardsDone, setCardsDone] = useState(false);
  useLayoutEffect(() => {
    if (session) {
      setAnswers(session.answers);
      setCardsDone(Boolean(session.progress?.cardsDone));
    }
  }, [session]);

  const flow = useMemo(
    () =>
      session
        ? deriveFlow({ sessionId: session.id, kind: session.kind, status: session.status, items: session.items, modes: session.modes, answers, cardsDone })
        : null,
    [session, answers, cardsDone],
  );

  const itemById = useMemo(() => new Map((session?.items ?? []).map((i) => [i.wordId, i])), [session]);

  const complete = useMutation({
    mutationFn: () => api.post<{ result: StudySessionData["result"] }>(`/study/sessions/${id}/complete`),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["today"] });
      await qc.invalidateQueries({ queryKey: ["records"] });
      await query.refetch();
    },
    onError: (e) => message.error(errorMessage(e, "结算失败，请重试")),
  });

  // 所有题做完自动结算
  const finishing = flow?.stage === "finish" && session?.status === "active";
  useEffect(() => {
    if (finishing && !complete.isPending && !complete.isError) complete.mutate();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [finishing]);

  const toggleAutoSpeak = (v: boolean) => {
    setAutoSpeak(v);
    try {
      localStorage.setItem(AUTO_SPEAK_KEY, v ? "1" : "0");
    } catch {
      /* 存储不可用时仅本次生效 */
    }
  };

  const exit = () => {
    if (!session || session.status === "completed") return navigate("/today");
    const answered = answers.length > 0;
    Modal.confirm({
      title: answered ? "结束本组？" : "退出学习？",
      content: answered
        ? "已经练完的词会保存并安排复习；还没练完的词会回到待学列表。"
        : "还没有开始作答，退出后这一组会被取消。",
      okText: answered ? "结束并保存" : "退出",
      cancelText: "继续学习",
      onOk: async () => {
        try {
          if (answered) {
            await complete.mutateAsync();
          } else {
            try {
              await api.del(`/study/sessions/${id}`);
            } catch {
              // 本地尚未记账但服务端已有作答（如答错后未点继续）：改为保存结算
              await complete.mutateAsync();
              return;
            }
            await qc.invalidateQueries({ queryKey: ["today"] });
            navigate("/today");
          }
        } catch (e) {
          message.error(errorMessage(e));
        }
      },
    });
  };

  if (query.isLoading) return <Shell><Loading tip="准备单词" /></Shell>;
  if (query.isError || !session || !flow) return <Shell><ErrorBlock error={query.error} onRetry={() => query.refetch()} /></Shell>;

  const stageLabel =
    flow.stage === "cards" ? "认识" : flow.stage === "practice" ? "练习" : flow.stage === "consolidate" ? "巩固错词" : flow.stage === "test" ? "检测" : "完成";
  const pct = flow.total ? Math.round((flow.done / flow.total) * 100) : session.status === "completed" ? 100 : 0;

  return (
    <Shell>
      <header style={{ display: "flex", alignItems: "center", gap: 12, padding: "12px 16px", maxWidth: 720, margin: "0 auto", width: "100%", boxSizing: "border-box" }}>
        <Tooltip title={session.status === "completed" ? "返回" : "结束本组"}>
          <Button type="text" shape="circle" icon={<CloseOutlined />} aria-label="结束本组" onClick={exit} />
        </Tooltip>
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ display: "flex", justifyContent: "space-between", fontSize: 12, color: "var(--muted)" }}>
            <span style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
              {KIND_LABEL[session.kind]} · {session.planName ?? ""}
            </span>
            <span>
              {stageLabel}
              {flow.total > 0 && flow.stage !== "finish" && ` ${flow.done}/${flow.total}`}
            </span>
          </div>
          <Progress percent={pct} showInfo={false} size="small" strokeColor="var(--primary)" trailColor="var(--track)" style={{ margin: 0 }} />
        </div>
        <Tooltip title="自动朗读">
          <Switch size="small" checked={autoSpeak} onChange={toggleAutoSpeak} checkedChildren="读" unCheckedChildren="静" />
        </Tooltip>
      </header>

      <main className="vx-study-main" style={{ flex: 1, display: "flex", justifyContent: "center", padding: "8px 16px 0" }}>
        <div style={{ width: "100%", maxWidth: 560 }}>
          {flow.stage === "cards" && (
            <CardsStage
              items={session.items}
              autoSpeak={autoSpeak}
              onDone={() => {
                setCardsDone(true);
                api.patch(`/study/sessions/${id}/progress`, { progress: { cardsDone: true } }).catch(() => undefined);
              }}
            />
          )}

          {(flow.stage === "practice" || flow.stage === "consolidate" || flow.stage === "test") && flow.current && (
            <QuestionStage
              key={`${flow.current.wordId}:${flow.current.mode}:${flow.current.phase}:${flow.current.attempt}`}
              sessionId={id}
              question={flow.current}
              item={itemById.get(flow.current.wordId)!}
              isTest={isTestKind(session.kind)}
              autoSpeak={autoSpeak}
              intro={flow.stage === "consolidate" && flow.done === 0 && flow.current.attempt === 1 ? `有 ${flow.wrongWordIds.length} 个词刚才出错了，再练一遍把它们记牢。` : undefined}
              onAnswered={(a) => setAnswers((prev) => [...prev, a])}
            />
          )}

          {flow.stage === "finish" && (
            session.status === "completed" && session.result ? (
              <ResultStage session={session} />
            ) : (
              <Loading tip={complete.isError ? "结算失败" : "正在结算记忆"} />
            )
          )}
          {flow.stage === "finish" && complete.isError && (
            <div style={{ textAlign: "center" }}>
              <Button type="primary" onClick={() => complete.mutate()}>
                重新结算
              </Button>
            </div>
          )}
        </div>
      </main>
    </Shell>
  );
}

function Shell({ children }: { children: ComponentChildren }) {
  return (
    <div className="vx-paper" style={{ minHeight: "100vh", display: "flex", flexDirection: "column" }}>
      {children}
    </div>
  );
}

// ------------------------------------------------------------------
// 认识
// ------------------------------------------------------------------

function WordFace({ item, big = true }: { item: SessionItem; big?: boolean }) {
  return (
    <div style={{ textAlign: "center" }}>
      <div style={{ display: "flex", alignItems: "center", justifyContent: "center", gap: 10 }}>
        <span className="vx-word" style={{ fontSize: big ? (item.spelling.length > 14 ? 34 : 46) : 30, fontWeight: 600, lineHeight: 1.15, wordBreak: "break-word" }}>
          {item.spelling}
        </span>
        <SpeakButton text={item.answer || item.spelling} wordId={item.wordId} />
      </div>
      {item.phonetic && (
        <div style={{ color: "var(--ink-soft)", marginTop: 6, fontFamily: "var(--serif-en)", fontSize: 16 }}>/{item.phonetic}/</div>
      )}
    </div>
  );
}

function CardsStage({ items, autoSpeak, onDone }: { items: SessionItem[]; autoSpeak: boolean; onDone: () => void }) {
  const [index, setIndex] = useState(0);
  const item = items[index];
  const last = index === items.length - 1;

  useEffect(() => {
    if (autoSpeak && item) speak(item.answer || item.spelling, item.wordId);
  }, [index, autoSpeak, item]);

  useLayoutEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "ArrowRight" || e.key === "Enter") {
        e.preventDefault();
        if (last) onDone();
        else setIndex((i) => i + 1);
      } else if (e.key === "ArrowLeft") {
        setIndex((i) => Math.max(0, i - 1));
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [last, onDone]);

  if (!item) return null;
  return (
    <div>
      <div className="vx-eyebrow" style={{ textAlign: "center", marginBottom: 12 }}>
        认识新词 · {index + 1} / {items.length}
      </div>
      <div key={item.wordId} className="vx-card vx-rise" style={{ padding: "36px 24px 28px", minHeight: 300, display: "flex", flexDirection: "column", justifyContent: "center", gap: 20 }}>
        <WordFace item={item} />
        <div style={{ borderTop: "1px dashed var(--line)", paddingTop: 18, textAlign: "center" }}>
          {item.partOfSpeech && <span style={{ color: "var(--accent)", fontFamily: "var(--serif-en)", marginRight: 8, fontStyle: "italic" }}>{item.partOfSpeech}</span>}
          <span className="vx-cn" style={{ fontSize: 20 }}>
            {item.definition}
          </span>
        </div>
        {item.example && (
          <div style={{ background: "var(--paper)", borderRadius: 10, padding: "10px 14px", color: "var(--ink-soft)" }}>
            <div className="vx-example-en">{item.example}</div>
            {item.exampleCn && <div className="vx-example-cn" style={{ marginTop: 4 }}>{item.exampleCn}</div>}
          </div>
        )}
        <CardSentences key={item.wordId} wordId={item.wordId} />
      </div>
      <div style={{ display: "flex", gap: 12, marginTop: 20 }}>
        <Button size="large" icon={<ArrowLeftOutlined />} disabled={index === 0} onClick={() => setIndex((i) => i - 1)} aria-label="上一个" />
        <Button size="large" type="primary" block onClick={() => (last ? onDone() : setIndex((i) => i + 1))}>
          {last ? "记住了，开始练习" : "下一个"} {!last && <ArrowRightOutlined />}
        </Button>
      </div>
      <div style={{ display: "flex", justifyContent: "center", gap: 6, marginTop: 16 }} aria-hidden>
        {items.map((w, i) => (
          <span key={w.wordId} style={{ width: i === index ? 18 : 6, height: 6, borderRadius: 3, background: i <= index ? "var(--primary)" : "var(--line)", transition: "all .2s" }} />
        ))}
      </div>
    </div>
  );
}

/** 单词卡上的「出现在这些句子里」（spec 0004）：默认收起，点开才请求，不打断翻卡 */
function CardSentences({ wordId }: { wordId: string }) {
  const [open, setOpen] = useState(false);
  return (
    <div style={{ textAlign: "left" }}>
      <Button type="link" size="small" style={{ padding: 0 }} onClick={(e) => { (e.currentTarget as HTMLElement).blur(); setOpen((v) => !v); }}>
        {open ? "收起句子" : "出现在这些句子里"}
      </Button>
      {open && (
        <div style={{ marginTop: 6 }}>
          <WordSentencesBlock wordId={wordId} title="" links={false} />
        </div>
      )}
    </div>
  );
}

// ------------------------------------------------------------------
// 答题
// ------------------------------------------------------------------

function QuestionStage({
  sessionId,
  question,
  item,
  isTest,
  autoSpeak,
  intro,
  onAnswered,
}: {
  sessionId: string;
  question: Question;
  item: SessionItem;
  isTest: boolean;
  autoSpeak: boolean;
  intro?: string;
  onAnswered: (a: SessionAnswer) => void;
}) {
  const { message } = useApp();
  const startedAt = useRef(Date.now());
  const [submitting, setSubmitting] = useState(false);
  const [feedback, setFeedback] = useState<{ correct: boolean; expected: string; chosen: string; dontKnow: boolean; answer: SessionAnswer } | null>(null);
  const [hintUsed, setHintUsed] = useState(false);

  useEffect(() => {
    if (autoSpeak && question.mode === "recognition") speak(item.answer || item.spelling, item.wordId);
    // 切到下一题时停掉上一题的声音
    return stopSpeaking;
  }, [autoSpeak, question.mode, item]);

  const submit = useCallback(
    /** dontKnow：点了「不会」，不带答案，服务端直接记为答错 */
    async (value: string, dontKnow = false) => {
      if (submitting || feedback) return;
      setSubmitting(true);
      try {
        const res = await api.post<AnswerResponse>(`/study/sessions/${sessionId}/answers`, {
          wordId: question.wordId,
          mode: question.mode,
          phase: question.phase,
          attempt: question.attempt,
          answer: dontKnow ? "" : value,
          hintUsed,
          ...(dontKnow ? { dontKnow: true } : {}),
          durationMs: Date.now() - startedAt.current,
        });
        const answer: SessionAnswer = { wordId: question.wordId, mode: question.mode, phase: question.phase, attempt: question.attempt, correct: res.correct ?? null, userAnswer: dontKnow ? "" : value, hintUsed, dontKnow };
        if (isTest) {
          onAnswered(answer);
          return;
        }
        const correct = !!res.correct;
        setFeedback({ correct, expected: res.expected ?? "", chosen: dontKnow ? "" : value, dontKnow, answer });
        if (correct && question.mode !== "recognition") speak(item.answer || item.spelling, item.wordId);
      } catch (e) {
        message.error(errorMessage(e, "提交失败，请检查网络后重试"));
      } finally {
        setSubmitting(false);
      }
    },
    [submitting, feedback, sessionId, question, hintUsed, isTest, onAnswered, item, message],
  );

  const dontKnow = useCallback(() => submit("", true), [submit]);

  const next = useCallback(() => {
    if (feedback) onAnswered(feedback.answer);
  }, [feedback, onAnswered]);

  // 答对自动前进；答错需要看清正确答案后手动继续
  useEffect(() => {
    if (feedback?.correct) {
      const t = setTimeout(next, question.mode === "spelling" ? 900 : 650);
      return () => clearTimeout(t);
    }
  }, [feedback, next, question.mode]);

  useLayoutEffect(() => {
    if (!feedback || feedback.correct) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Enter" || e.key === " ") {
        e.preventDefault();
        next();
      }
    };
    // 延迟挂载，避免提交用的回车立即触发“继续”
    const t = setTimeout(() => window.addEventListener("keydown", onKey), 250);
    return () => {
      clearTimeout(t);
      window.removeEventListener("keydown", onKey);
    };
  }, [feedback, next]);

  const phaseTag = question.phase === "consolidate" ? `巩固 · 第 ${question.attempt} 次` : `${MODE_LABEL[question.mode]}题`;

  return (
    <div className="vx-rise">
      {intro && (
        <div className="vx-card" style={{ padding: "10px 14px", marginBottom: 14, background: "var(--accent-soft)", borderColor: "var(--accent-line)", color: "var(--ink)" }}>
          {intro}
        </div>
      )}
      <div className="vx-eyebrow" style={{ textAlign: "center", marginBottom: 12 }}>
        {phaseTag}
      </div>

      {question.mode === "recognition" ? (
        <RecognitionQuestion item={item} feedback={feedback} disabled={submitting || !!feedback} onChoose={submit} onDontKnow={dontKnow} isTest={isTest} />
      ) : (
        <TypingQuestion
          item={item}
          mode={question.mode}
          feedback={feedback}
          disabled={submitting || !!feedback}
          hintUsed={hintUsed}
          onHint={() => setHintUsed(true)}
          onSubmit={submit}
          onDontKnow={dontKnow}
          isTest={isTest}
        />
      )}

      {feedback && !feedback.correct && (
        <div className="vx-card vx-rise" style={{ marginTop: 16, padding: 16, borderColor: "var(--bad-line)", background: "var(--bad-soft)" }}>
          <div style={{ display: "flex", alignItems: "center", gap: 8, color: "var(--bad)", fontWeight: 600 }}>
            <CloseCircleFilled /> {feedback.dontKnow ? "没关系，记一下正确答案" : "答错了，记一下正确答案"}
          </div>
          <div style={{ marginTop: 10, display: "flex", alignItems: "center", gap: 10, flexWrap: "wrap" }}>
            <span className="vx-word" style={{ fontSize: 24, fontWeight: 600 }}>
              {item.answer || item.spelling}
            </span>
            <SpeakButton text={item.answer || item.spelling} wordId={item.wordId} size="small" />
            {item.phonetic && <span style={{ color: "var(--ink-soft)" }}>/{item.phonetic}/</span>}
          </div>
          <div className="vx-cn" style={{ marginTop: 4 }}>
            {item.partOfSpeech && <i style={{ color: "var(--accent)", marginRight: 6 }}>{item.partOfSpeech}</i>}
            {item.definition}
          </div>
          <Button type="primary" block size="large" style={{ marginTop: 14 }} onClick={next}>
            继续（Enter）
          </Button>
        </div>
      )}
    </div>
  );
}

function RecognitionQuestion({
  item,
  feedback,
  disabled,
  onChoose,
  onDontKnow,
  isTest,
}: {
  item: SessionItem;
  feedback: { correct: boolean; expected: string; chosen: string } | null;
  disabled: boolean;
  onChoose: (v: string) => void;
  onDontKnow: () => void;
  isTest: boolean;
}) {
  const mountedAt = useRef(Date.now());
  useLayoutEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      // 忽略按住连发与切题后 300ms 内的按键，避免一次按键答掉多题
      if (e.repeat || Date.now() - mountedAt.current < 300) return;
      const n = Number(e.key);
      if (!disabled && n >= 1 && n <= item.options.length) onChoose(item.options[n - 1]);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [disabled, item.options, onChoose]);

  return (
    <div>
      <div className="vx-card" style={{ padding: "32px 20px", marginBottom: 18 }}>
        <WordFace item={item} />
        {!isTest && <div style={{ textAlign: "center", color: "var(--muted)", marginTop: 12, fontSize: 13 }}>选出正确的中文意思</div>}
      </div>
      <div style={{ display: "grid", gap: 10 }}>
        {item.options.map((opt, i) => {
          const isExpected = feedback && opt === feedback.expected;
          const isChosenWrong = feedback && !feedback.correct && opt === feedback.chosen;
          const bg = isExpected ? "var(--good-soft)" : isChosenWrong ? "var(--bad-soft)" : "var(--surface)";
          const border = isExpected ? "var(--good)" : isChosenWrong ? "var(--bad)" : "var(--line)";
          return (
            <button
              key={opt}
              type="button"
              disabled={disabled}
              onClick={() => onChoose(opt)}
              className={isChosenWrong ? "vx-shake vx-tap" : "vx-tap"}
              style={{
                display: "flex",
                alignItems: "center",
                gap: 12,
                textAlign: "left",
                width: "100%",
                padding: "14px 16px",
                borderRadius: 12,
                border: `1.5px solid ${border}`,
                background: bg,
                cursor: disabled ? "default" : "pointer",
                fontSize: 16,
                color: "var(--ink)",
                fontFamily: "var(--serif-cn)",
                transition: "border-color .15s, background .15s, transform .1s",
              }}
            >
              <span style={{ width: 24, height: 24, flex: "none", borderRadius: 6, border: "1px solid var(--line)", display: "grid", placeItems: "center", fontSize: 12, color: "var(--muted)", fontFamily: "var(--sans)" }}>
                {i + 1}
              </span>
              <span style={{ flex: 1 }}>{opt}</span>
              {isExpected && <CheckCircleFilled style={{ color: "var(--good)" }} aria-label="正确答案" />}
              {isChosenWrong && <CloseCircleFilled style={{ color: "var(--bad)" }} aria-label="你的选择" />}
            </button>
          );
        })}
      </div>
      {/* 「不会」和选项分开、样式弱一些，避免误点 */}
      <div style={{ textAlign: "center", marginTop: 18 }}>
        <Button type="text" disabled={disabled} onClick={onDontKnow} style={{ color: "var(--muted)" }}>
          不会
        </Button>
      </div>
    </div>
  );
}

/** 拼写题与挖空题共用：给提示 → 输入英文 → 判定 */
function TypingQuestion({
  item,
  mode,
  feedback,
  disabled,
  hintUsed,
  onHint,
  onSubmit,
  onDontKnow,
  isTest,
}: {
  item: SessionItem;
  mode: Mode;
  feedback: { correct: boolean; expected: string; chosen: string } | null;
  disabled: boolean;
  hintUsed: boolean;
  onHint: () => void;
  onSubmit: (v: string) => void;
  onDontKnow: () => void;
  isTest: boolean;
}) {
  const [value, setValue] = useState("");
  const inputRef = useRef<HTMLInputElement>(null);
  const target = item.answer || item.spelling;
  const letters = item.letters ?? target.replace(/[^a-z]/gi, "").length;
  const words = item.wordsInAnswer ?? target.split(/\s+/).filter(Boolean).length;

  useEffect(() => {
    const t = setTimeout(() => inputRef.current?.focus(), 50);
    return () => clearTimeout(t);
  }, []);

  const state = feedback ? (feedback.correct ? "good" : "bad") : null;

  return (
    <div>
      <div className="vx-card" style={{ padding: "26px 20px", marginBottom: 18, textAlign: "center" }}>
        {mode === "cloze" ? (
          <>
            <div className="vx-word" style={{ fontSize: 20, lineHeight: 1.7, wordBreak: "break-word" }}>
              {(item.cloze ?? "").split("____").map((part, i, arr) => (
                <span key={i}>
                  {part}
                  {i < arr.length - 1 && (
                    <span style={{ display: "inline-block", minWidth: Math.max(60, letters * 11), borderBottom: "2px solid var(--accent)", verticalAlign: "bottom", height: "1.2em" }} />
                  )}
                </span>
              ))}
            </div>
            {item.clozeCn && <div className="vx-cn" style={{ color: "var(--ink-soft)", marginTop: 10, fontSize: 15 }}>{item.clozeCn}</div>}
            <div className="vx-cn" style={{ color: "var(--muted)", marginTop: 8, fontSize: 14 }}>
              {item.partOfSpeech && <i style={{ color: "var(--accent)", marginRight: 6 }}>{item.partOfSpeech}</i>}
              {item.definition}
            </div>
          </>
        ) : (
          <>
            {item.partOfSpeech && <div style={{ color: "var(--accent)", fontFamily: "var(--serif-en)", fontStyle: "italic" }}>{item.partOfSpeech}</div>}
            <div className="vx-cn" style={{ fontSize: 24, marginTop: 6, lineHeight: 1.5 }}>
              {item.definition}
            </div>
          </>
        )}
        <div style={{ color: "var(--muted)", fontSize: 13, marginTop: 10 }}>
          {mode === "cloze" ? "填入缺少的词" : "拼写英文"} · {words > 1 ? `${words} 个单词，` : ""}共 {letters} 个字母
          {hintUsed && !isTest && (
            <span style={{ marginLeft: 8, color: "var(--accent)" }}>
              首字母 <b className="vx-word">{target.charAt(0)}</b>
            </span>
          )}
        </div>
      </div>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          if (value.trim()) onSubmit(value.trim());
        }}
      >
        <Input
          inputRef={inputRef}
          size="large"
          value={value}
          disabled={disabled}
          onChange={(e) => setValue(e.target.value)}
          placeholder={mode === "cloze" ? "填入缺少的单词" : "输入拼写，回车提交"}
          inputMode="text"
          autoComplete="off"
          autoCorrect="off"
          autoCapitalize="off"
          spellCheck={false}
          aria-label="拼写答案"
          className={state === "bad" ? "vx-shake" : undefined}
          style={{
            fontFamily: "var(--serif-en)",
            fontSize: 24,
            textAlign: "center",
            letterSpacing: "0.04em",
            height: 60,
            borderColor: state === "good" ? "var(--good)" : state === "bad" ? "var(--bad)" : undefined,
            background: state === "good" ? "var(--good-soft)" : state === "bad" ? "var(--bad-soft)" : undefined,
          }}
          suffix={<CheckCircleFilled style={{ color: "var(--good)", visibility: state === "good" ? "visible" : "hidden" }} />}
        />
        <div style={{ display: "flex", gap: 10, marginTop: 14 }}>
          {!isTest && (
            <Button size="large" icon={<BulbOutlined />} disabled={disabled || hintUsed} onClick={onHint}>
              提示
            </Button>
          )}
          {/* 用过提示仍可点「不会」；样式弱一些，避免误点 */}
          <Button size="large" type="text" disabled={disabled} onClick={onDontKnow} style={{ color: "var(--muted)", flex: "none" }}>
            不会
          </Button>
          <Button size="large" type="primary" htmlType="submit" block disabled={disabled || !value.trim()}>
            {isTest ? "下一题" : "提交"}
          </Button>
        </div>
      </form>
    </div>
  );
}

// ------------------------------------------------------------------
// 结果
// ------------------------------------------------------------------

function ResultStage({ session }: { session: StudySessionData }) {
  const navigate = useNavigate();
  const { message } = useApp();
  const qc = useQueryClient();
  const r = session.result!;
  const today = useQuery({ queryKey: ["today"], queryFn: () => api.get<TodayData>("/today") });
  const card = today.data?.plans.find((p) => p.planId === session.planId);
  const itemById = new Map(session.items.map((i) => [i.wordId, i]));
  const wrong = r.wrongWordIds.map((w) => itemById.get(w)).filter((x): x is SessionItem => !!x);
  const accuracy = r.accuracy ?? 0;
  const headline = isTestKind(session.kind) ? (session.kind === "sheet" ? "单词单测试完成" : "检测完成") : accuracy >= 0.9 ? "太棒了！" : accuracy >= 0.6 ? "完成一组！" : "坚持就是进步";

  const start = async (kind: "learn" | "review") => {
    try {
      const res = await api.post<{ id: string }>("/study/sessions", { kind, planId: session.planId });
      await qc.invalidateQueries({ queryKey: ["today"] });
      navigate(`/study/${res.id}`, { replace: true });
    } catch (e) {
      message.info(errorMessage(e));
    }
  };

  // 检测：逐题对错
  const testRows = isTestKind(session.kind) ? session.items.map((i) => ({ item: i, answers: session.answers.filter((a) => a.wordId === i.wordId) })) : [];

  return (
    <div className="vx-rise" style={{ textAlign: "center" }}>
      <div className="vx-eyebrow" style={{ marginTop: 12 }}>{KIND_LABEL[session.kind]} · {session.planName}</div>
      <h1 className="vx-title" style={{ fontSize: 30, margin: "6px 0 18px" }}>
        {headline}
      </h1>
      <div className="vx-card" style={{ padding: 20, display: "grid", gridTemplateColumns: "repeat(3, 1fr)", gap: 8 }}>
        <ResultNum label={isTestKind(session.kind) ? "得分" : "首答正确"} value={isTestKind(session.kind) ? `${r.correctFirst}/${r.totalFirst}` : percent(r.accuracy)} />
        <ResultNum label={session.kind === "learn" ? "新学入库" : "单词"} value={session.kind === "learn" ? r.newLearned : r.words} />
        <ResultNum label="用时" value={formatDuration(r.durationMs)} small />
      </div>

      {session.kind !== "drill" && !isTestKind(session.kind) && Object.keys(r.ratings).length > 0 && (
        <div style={{ marginTop: 12, color: "var(--ink-soft)", fontSize: 13 }}>
          记忆评估：
          {Object.entries(r.ratings)
            .map(([k, v]) => `${RATING_NAME_LABEL[k] ?? k} ${v}`)
            .join(" · ")}
          {session.kind === "learn" && " —— 新词明天会安排第一次复习"}
        </div>
      )}
      {r.unsettled > 0 && session.kind !== "drill" && (
        <div style={{ marginTop: 8, color: "var(--muted)", fontSize: 13 }}>有 {r.unsettled} 个词没练完，已放回待学列表。</div>
      )}

      {wrong.length > 0 && !isTestKind(session.kind) && (
        <div className="vx-card" style={{ padding: 16, marginTop: 18, textAlign: "left" }}>
          <div style={{ fontWeight: 600, marginBottom: 8 }}>本组出错的词</div>
          {wrong.map((w) => (
            <div key={w.wordId} style={{ display: "flex", alignItems: "center", gap: 10, padding: "6px 0", borderTop: "1px dashed var(--line)" }}>
              <span className="vx-word" style={{ fontSize: 18, fontWeight: 600, minWidth: 110 }}>{w.spelling}</span>
              <span className="vx-cn" style={{ flex: 1, color: "var(--ink-soft)" }}>{w.definition}</span>
              <SpeakButton text={w.answer || w.spelling} wordId={w.wordId} size="small" />
            </div>
          ))}
        </div>
      )}

      {testRows.length > 0 && (
        <div className="vx-card" style={{ padding: 16, marginTop: 18, textAlign: "left" }}>
          <div style={{ fontWeight: 600, marginBottom: 8 }}>答题明细</div>
          {testRows.map(({ item, answers }) => (
            <div key={item.wordId} style={{ padding: "8px 0", borderTop: "1px dashed var(--line)" }}>
              <div style={{ display: "flex", gap: 10, alignItems: "baseline", flexWrap: "wrap" }}>
                <span className="vx-word" style={{ fontSize: 17, fontWeight: 600 }}>{item.spelling}</span>
                <span className="vx-cn" style={{ color: "var(--ink-soft)" }}>{item.definition}</span>
              </div>
              {answers.map((a) => (
                <div key={`${a.mode}`} style={{ fontSize: 13, color: a.correct ? "var(--good)" : "var(--bad)" }}>
                  {a.correct ? <CheckCircleFilled /> : <CloseCircleFilled />} {MODE_LABEL[a.mode as Mode]}
                  {a.dontKnow ? "：不会" : !a.correct && a.userAnswer ? `：你的答案「${a.userAnswer}」` : ""}
                </div>
              ))}
            </div>
          ))}
        </div>
      )}

      <div style={{ display: "grid", gap: 10, marginTop: 22 }}>
        {card && card.newLeft > 0 && session.kind === "learn" && (
          <Button type="primary" size="large" onClick={() => start("learn")}>
            再学一组（今天还剩 {card.newLeft} 个新词）
          </Button>
        )}
        {card && card.reviewLeft > 0 && !isTestKind(session.kind) && (
          <Button type={session.kind === "learn" && card.newLeft > 0 ? "default" : "primary"} size="large" onClick={() => start("review")}>
            去复习（{card.reviewLeft} 个到期）
          </Button>
        )}
        {session.kind === "sheet" && r.wrongWordIds.length > 0 && (
          <Button type="primary" size="large" onClick={() => navigate(regenerateLink(session))}>
            用错词再出一张（{r.wrongWordIds.length} 个错词）
          </Button>
        )}
        <Button size="large" onClick={() => navigate("/today")}>
          返回今日
        </Button>
      </div>
    </div>
  );
}

function ResultNum({ label, value, small }: { label: string; value: ComponentChildren; small?: boolean }) {
  return (
    <div>
      <div className="vx-num" style={{ fontSize: small ? 18 : 28, fontWeight: 600, color: "var(--ink)", minHeight: 36, display: "flex", alignItems: "center", justifyContent: "center" }}>
        {value}
      </div>
      <div style={{ fontSize: 12, color: "var(--muted)" }}>{label}</div>
    </div>
  );
}
