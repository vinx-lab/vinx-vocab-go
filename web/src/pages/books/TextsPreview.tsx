/**
 * 句型 / 课文的导入预览（spec 0004 §7）：逐句显示解析结果、超纲词、词库外的词，可以改中英文、取消勾选。
 * 词书导入页的「句型」「课文」标签和单元页的「粘贴导入」共用。
 */
import { Checkbox, Input, Tag, Tooltip } from "@/ui";
import { DEFAULT_TITLE, KIND_LABEL, type EditPreviewSentence, type EditPreviewText } from "@/lib/sentences";
import type { AnalyzedWord, PreviewText } from "@/types";

const STATUS_TAG: Record<EditPreviewSentence["status"], { color: string; label: string }> = {
  ok: { color: "green", label: "正常" },
  warning: { color: "gold", label: "需检查" },
  error: { color: "red", label: "错误" },
};

/** 预览结果 → 可编辑状态（错误的句子默认不导入） */
export function toEditTexts(texts: PreviewText[] | undefined): EditPreviewText[] {
  return (texts ?? []).map((t) => ({ ...t, sentences: t.sentences.map((s) => ({ ...s, include: s.status !== "error" })) }));
}

/** 超纲词与词库外的词（只提示，不阻止保存） */
export function ScopeHints({ outOfScope, unknown }: { outOfScope: AnalyzedWord[]; unknown: string[] }) {
  if (outOfScope.length === 0 && unknown.length === 0) return null;
  return (
    <div style={{ display: "flex", flexWrap: "wrap", gap: 4, alignItems: "center", fontSize: 12, marginTop: 4 }}>
      {outOfScope.length > 0 && <span style={{ color: "var(--muted)" }}>超纲：</span>}
      {outOfScope.map((w, i) => (
        <Tooltip key={`o${i}`} title={w.definition}>
          <Tag color="gold" bordered={false} style={{ marginInlineEnd: 0 }}>
            {w.form}
            {w.form.toLowerCase() !== w.spelling.toLowerCase() ? ` → ${w.spelling}` : ""}
          </Tag>
        </Tooltip>
      ))}
      {unknown.length > 0 && <span style={{ color: "var(--muted)", marginLeft: outOfScope.length ? 6 : 0 }}>词库外：</span>}
      {unknown.map((t, i) => (
        <Tag key={`u${i}`} bordered={false} style={{ marginInlineEnd: 0 }}>
          {t}
        </Tag>
      ))}
    </div>
  );
}

export function TextsPreviewList({
  texts,
  onSentence,
  onText,
}: {
  texts: EditPreviewText[];
  onSentence: (textIndex: number, sentenceIndex: number, patch: Partial<EditPreviewSentence>) => void;
  onText: (textIndex: number, patch: Partial<Pick<EditPreviewText, "title" | "titleCn">>) => void;
}) {
  if (texts.length === 0) return <div style={{ color: "var(--muted)", padding: 8 }}>没有解析出内容</div>;
  return (
    <div style={{ display: "grid", gap: 14 }}>
      {texts.map((t, ti) => {
        const inc = t.sentences.filter((s) => s.include).length;
        return (
          <div key={ti} className="vx-texts-preview" style={{ border: "1px solid var(--line)", borderRadius: 8, padding: 12 }}>
            <div style={{ display: "flex", gap: 8, flexWrap: "wrap", alignItems: "center", marginBottom: 8 }}>
              <Tag color={t.kind === "list" ? "cyan" : "geekblue"} bordered={false} style={{ marginInlineEnd: 0 }}>
                {KIND_LABEL[t.kind]}
              </Tag>
              <Input size="small" value={t.title} placeholder={DEFAULT_TITLE[t.kind]} maxLength={100} style={{ maxWidth: 260 }} onChange={(e) => onText(ti, { title: e.target.value })} />
              <Input size="small" value={t.titleCn} placeholder="中文标题（可选）" maxLength={100} style={{ maxWidth: 220 }} onChange={(e) => onText(ti, { titleCn: e.target.value })} />
              <span style={{ fontSize: 12, color: "var(--muted)" }}>
                {t.sentences.length} 句 · 已勾选 {inc} · 第 {t.line} 行起
              </span>
            </div>
            {t.sentences.map((s, si) => {
              const newPara = t.kind === "text" && si > 0 && s.paragraph !== t.sentences[si - 1].paragraph;
              const missing = s.include && (!s.en.trim() || !s.cn.trim());
              return (
                <div key={si} style={{ display: "flex", gap: 10, padding: "8px 0", borderTop: newPara ? "2px solid var(--line)" : "1px dashed var(--line)", opacity: s.include ? 1 : 0.55 }}>
                  <div style={{ paddingTop: 2 }}>
                    <Checkbox checked={s.include} onChange={(e) => onSentence(ti, si, { include: e.target.checked })} aria-label={`导入第 ${s.line} 行`} />
                  </div>
                  <div style={{ flex: 1, minWidth: 0 }}>
                    <div style={{ display: "flex", gap: 6, alignItems: "center", flexWrap: "wrap", fontSize: 12, color: "var(--muted)", marginBottom: 4 }}>
                      <Tag color={STATUS_TAG[s.status].color} bordered={false} style={{ marginInlineEnd: 0 }}>
                        {STATUS_TAG[s.status].label}
                      </Tag>
                      <span title={s.raw}>第 {s.line} 行</span>
                      {newPara && <span>· 新段落</span>}
                      {s.frame && <span>· 句型：{s.frame}</span>}
                      {s.issues.length > 0 && <span style={{ color: s.status === "error" ? "var(--bad)" : "var(--accent)" }}>· {s.issues.join("；")}</span>}
                    </div>
                    <Input size="small" className="vx-word" value={s.en} placeholder="英文" status={missing && !s.en.trim() ? "error" : undefined} onChange={(e) => onSentence(ti, si, { en: e.target.value })} />
                    <Input size="small" value={s.cn} placeholder="中文" style={{ marginTop: 4 }} status={missing && !s.cn.trim() ? "error" : undefined} onChange={(e) => onSentence(ti, si, { cn: e.target.value })} />
                    <ScopeHints outOfScope={s.outOfScope} unknown={s.unknown} />
                  </div>
                </div>
              );
            })}
          </div>
        );
      })}
    </div>
  );
}

/** 句型 / 课文的格式说明 */
export const TEXTS_SAMPLE = `[句型]
How do you learn English words? | 你是如何学习英文单词的？
I find ... useful. | 我发现……很有用。 | I find making word cards useful.
[课文]
Title: How I Learn English | 我是怎样学英语的
I used to find English hard. | 我过去觉得英语很难。
Then I started to read aloud every day. | 后来我开始每天大声朗读。

Now I can understand more. | 现在我能听懂更多了。`;
