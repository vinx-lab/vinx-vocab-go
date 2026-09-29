import { useEffect, useState } from "preact/hooks";
import { useNavigate, useParams, useSearchParams } from "@/lib/router";
import { Button, ArrowLeftOutlined, PrinterOutlined } from "@/ui";
import { api } from "@/lib/api";
import { ErrorBlock, Loading } from "@/components/ui";
import type { SheetDetail } from "@/types";
import { parsePrintIds } from "./links";
import { paginate } from "./paginate";

/**
 * A4 对折自测表：左栏英文 + 音标，右栏词性 + 中文；折线在纸张正中（105mm），对折盖住一侧即可自测。
 * 单张 `/sheets/:id/print` 与合并打印 `/sheets/print?ids=a,b,c` 共用：按编号排序，每份一页，
 * 选「双面打印（长边翻转）」4 份正好 2 张纸。打印页边距为 0，浏览器没地方印页眉页脚。
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

  useEffect(() => {
    if (!ready) return;
    const t = window.setTimeout(() => window.print(), 300);
    return () => window.clearTimeout(t);
  }, [ready]);

  if (ids.length === 0) return <div className="vx-page"><ErrorBlock error={new Error("没有选择要打印的单词单")} /></div>;
  if (error) return <div className="vx-page"><ErrorBlock error={error} onRetry={reload} /></div>;
  if (!ready) return <Loading />;
  const sorted = [...sheets!].sort((a, b) => a.seq - b.seq);

  return (
    <div className="vx-sheet-root">
      <div className="vx-noprint vx-sheet-toolbar">
        <Button icon={<ArrowLeftOutlined />} onClick={() => navigate("/sheets")}>返回单词单</Button>
        <Button type="primary" icon={<PrinterOutlined />} onClick={() => window.print()}>
          打印{sorted.length > 1 ? `（${sorted.length} 份）` : ""}
        </Button>
        {sorted.length > 1 && <span className="vx-sheet-tip">每份一页；在打印对话框选「双面打印 · 长边翻转」，{sorted.length} 份用 {Math.ceil(sorted.length / 2)} 张纸。</span>}
      </div>
      {sorted.flatMap((s) => <SheetPages key={s.id} sheet={s} />)}
    </div>
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
