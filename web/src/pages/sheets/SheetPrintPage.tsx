import type { JSX } from "preact";
import { useEffect, useState } from "preact/hooks";
import { useNavigate, useParams, useSearchParams } from "@/lib/router";
import { Button, Checkbox, Segmented, ArrowLeftOutlined, PrinterOutlined } from "@/ui";
import { api } from "@/lib/api";
import { ErrorBlock, Loading } from "@/components/ui";
import type { SheetDetail } from "@/types";
import { parsePrintIds } from "./links";
import { paginate } from "./paginate";
import { LINE_STYLE_LABEL, ROW_H, answerPages, layoutQuestionPages, numberItems, parseLineStyle, type LineStyle } from "./dictation";

/**
 * A4 对折自测表：左栏英文 + 音标，右栏词性 + 中文；折线在纸张正中（105mm），对折盖住一侧即可自测。
 * 单张 `/sheets/:id/print` 与合并打印 `/sheets/print?ids=a,b,c` 共用：按编号排序，每份一页，
 * 选「双面打印（长边翻转）」4 份正好 2 张纸。打印页边距为 0，浏览器没地方印页眉页脚。
 * 默写单（spec 0006）走 DictationPages：题目页 + 答案页，没有折线；书写线（横线 / 四线三格）取 ?lines=，可在工具栏切换。
 * 不跟深色主题：屏幕预览也按纸张效果显示白底黑字——外层 data-theme="light" 让 UI 组件库的颜色
 * （CSS 变量随 data-theme 取值）和 styles.css 的业务色板在这个子树里都固定成浅色。
 */
export function SheetPrintPage() {
  return (
    <div className="vx-print-scope" data-theme="light">
      <SheetPrint />
    </div>
  );
}

/** 按 id 列表并行取单词单详情；ids 变化或 reload 时重新取 */
function useSheetDetails(ids: string[]) {
  const [sheets, setSheets] = useState<SheetDetail[] | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [reloadKey, setReloadKey] = useState(0);
  const key = ids.join(",");

  useEffect(() => {
    if (ids.length === 0) {
      setSheets(null);
      setError(null);
      return;
    }
    let alive = true;
    setSheets(null);
    setError(null);
    Promise.all(ids.map((id) => api.get<SheetDetail>(`/sheets/${id}`))).then(
      (list) => {
        if (alive) setSheets(list);
      },
      (e) => {
        if (alive) setError(e);
      },
    );
    return () => {
      alive = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, reloadKey]);

  return { sheets, error, reload: () => setReloadKey((n) => n + 1) };
}

function SheetPrint() {
  const { id } = useParams<{ id: string }>();
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const ids = id ? [id] : parsePrintIds(params.get("ids"));
  const { sheets, error, reload } = useSheetDetails(ids);
  const ready = ids.length > 0 && !!sheets;
  // 默写单：书写线（生成页按词书学段带上 ?lines=，默认横线）与是否打印答案页
  const [lineStyle, setLineStyle] = useState<LineStyle>(() => parseLineStyle(params.get("lines")) ?? "line");
  const [withAnswers, setWithAnswers] = useState(true);

  useEffect(() => {
    if (!ready) return;
    const t = window.setTimeout(() => window.print(), 300);
    return () => window.clearTimeout(t);
  }, [ready]);

  if (ids.length === 0) return <div className="vx-page"><ErrorBlock error={new Error("没有选择要打印的单词单")} /></div>;
  if (error) return <div className="vx-page"><ErrorBlock error={error} onRetry={reload} /></div>;
  if (!ready) return <Loading />;
  const sorted = [...sheets!].sort((a, b) => a.seq - b.seq);
  const hasDictation = sorted.some((s) => s.format === "dictation");

  return (
    <div className={`vx-sheet-root${withAnswers ? "" : " vx-dict-noanswers"}`}>
      <div className="vx-noprint vx-sheet-toolbar">
        <Button icon={<ArrowLeftOutlined />} onClick={() => navigate("/sheets")}>返回单词单</Button>
        <Button type="primary" icon={<PrinterOutlined />} onClick={() => window.print()}>
          打印{sorted.length > 1 ? `（${sorted.length} 份）` : ""}
        </Button>
        {hasDictation && (
          <>
            <span className="vx-dict-opt">
              书写线
              <Segmented<LineStyle>
                value={lineStyle}
                onChange={setLineStyle}
                options={(["line", "fourline"] as LineStyle[]).map((v) => ({ value: v, label: LINE_STYLE_LABEL[v] }))}
              />
            </span>
            <Checkbox checked={withAnswers} onChange={(e) => setWithAnswers(e.target.checked)}>
              打印答案页
            </Checkbox>
          </>
        )}
        {sorted.length > 1 && !hasDictation && <span className="vx-sheet-tip">每份一页；在打印对话框选「双面打印 · 长边翻转」，{sorted.length} 份用 {Math.ceil(sorted.length / 2)} 张纸。</span>}
        {hasDictation && <span className="vx-sheet-tip">默写单每份 = 题目页 + 答案页（另起一页，可以不打印）；书写线可以切换，四线三格适合小学。</span>}
      </div>
      {sorted.flatMap((s) => (s.format === "dictation" ? <DictationPages key={s.id} sheet={s} style={lineStyle} /> : <SheetPages key={s.id} sheet={s} />))}
    </div>
  );
}

/**
 * 默写单：题目页（页眉「VinxVocab 默写单 #N」、姓名、日期、得分；题号连续，左侧中文提示、右侧书写区）
 * + 答案页（另起一页，两栏，只写题号和答案）。分页按 dictation.ts 的高度预算，保证不被 overflow 截掉。
 */
export function DictationPages({ sheet: s, style }: { sheet: SheetDetail; style: LineStyle }) {
  const date = new Date(s.createdAt).toLocaleDateString("zh-CN");
  const items = numberItems(s.items ?? []);
  const pages = layoutQuestionPages(items, style);
  const answers = answerPages(items);
  const vars = { "--dict-row": `${ROW_H[style]}mm` } as JSX.CSSProperties;
  return (
    <>
      {pages.map((p, pi) => (
        <section key={`q${pi}`} className={`vx-sheet-page vx-dict-page vx-dict-lines-${style}`} style={vars} data-seq={s.seq}>
          <header className="vx-dict-head">
            <strong>VinxVocab 默写单 #{s.seq}</strong>
            <span className="vx-dict-meta">
              <span>{s.student.name}</span>
              <span>{date}</span>
              <span>
                得分 <span className="vx-dict-score" /> / {items.length}
              </span>
            </span>
          </header>
          {p.blocks.map((b, bi) =>
            b.kind === "title" ? (
              <div key={bi} className="vx-dict-title">{b.title}</div>
            ) : b.kind === "row" ? (
              <div key={bi} className={`vx-dict-row${b.full ? " is-full" : ""}`}>
                {b.items.map((it) => (
                  <div key={it.index} className={`vx-dict-cell tier-${it.tier}`}>
                    <span className="vx-dict-no">{it.no}.</span>
                    <span className="vx-dict-prompt">{it.prompt}</span>
                    <span className="vx-dict-w" />
                  </div>
                ))}
              </div>
            ) : (
              <div key={bi} className="vx-dict-sentence">
                <span className="vx-dict-no">{b.item.no}.</span>
                <span className="vx-dict-prompt">{b.item.prompt}</span>
                <div className="vx-dict-writes">
                  <div className="vx-dict-w" />
                  {b.tier === "double" && <div className="vx-dict-w" />}
                </div>
              </div>
            ),
          )}
          <footer className="vx-dict-foot">
            <span>{pages.length > 1 ? `第 ${pi + 1} / ${pages.length} 页` : ""}</span>
            <span>写完后：登录 → 单词单 → 默写单 #{s.seq} 批改</span>
          </footer>
        </section>
      ))}
      {answers.map((list, ai) => (
        <section key={`a${ai}`} className="vx-sheet-page vx-dict-page vx-dict-answer-page" data-seq={s.seq}>
          <header className="vx-dict-head">
            <strong>答案（家长 / 老师批改用）</strong>
            <span className="vx-dict-meta">
              默写单 #{s.seq} · {s.student.name} · {date}
              {answers.length > 1 ? ` · ${ai + 1}/${answers.length}` : ""}
            </span>
          </header>
          <div className="vx-dict-answers">
            {list.map((it) => (
              <div key={it.index} className="vx-dict-answer">
                <b>{it.no}.</b>
                {it.answer}
              </div>
            ))}
          </div>
        </section>
      ))}
    </>
  );
}

function SheetPages({ sheet: s }: { sheet: SheetDetail }) {
  const date = new Date(s.createdAt).toLocaleDateString("zh-CN");
  return (
    <>
      {paginate(s.words).map((page, pi) => (
        <section key={pi} className="vx-sheet-page" data-seq={s.seq}>
          <header className="vx-sheet-head">
            <strong>VinxVocab 单词单 #{s.seq}</strong>
            <span>{s.student.name} · {date} · {s.words.length} 词</span>
          </header>
          <div className="vx-sheet-rows">
            {page.rows.map((w, i) => (
              <div key={w.wordId} className="vx-sheet-row">
                <span className="vx-sheet-no">{page.start + i + 1}</span>
                <span className="vx-sheet-en">{w.spelling}</span>
                <span className="vx-sheet-ph">{w.phonetic ?? ""}</span>
                <span className="vx-sheet-fold" aria-hidden />
                <span className="vx-sheet-cn">{w.partOfSpeech ? `${w.partOfSpeech} ` : ""}{w.definition}</span>
              </div>
            ))}
          </div>
          <footer className="vx-sheet-foot">
            <span>✂ 沿虚线对折，盖住一侧自测</span>
            <span>回家后：登录 → 今日 → 单词单 #{s.seq} 测试</span>
          </footer>
        </section>
      ))}
    </>
  );
}
