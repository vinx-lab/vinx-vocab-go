import { Tag, Tooltip } from "@/ui";
import type { SentenceChecks } from "@vinx/shared";
import { checkTone } from "@/lib/aiDraft";

/**
 * 生成后的检查标记（spec 0005 §4）：目标词没用上标红；超纲词、太长、结构偏离标黄。只做提示，不拦截。
 * 全部通过时不显示。例句、短文的生成结果和句型、仿写的草稿共用。
 */
export function AiChecks({ checks, style }: { checks?: SentenceChecks | null; style?: preact.JSX.CSSProperties }) {
  const tone = checkTone(checks);
  if (!checks || tone === "ok") return null;
  const tag = (color: "red" | "gold", text: string, tip?: string) => {
    const t = (
      <Tag bordered={false} color={color} style={{ marginInlineEnd: 0, whiteSpace: "normal" }}>
        {text}
      </Tag>
    );
    return tip ? <Tooltip title={tip}>{t}</Tooltip> : t;
  };
  return (
    <div data-testid="ai-checks" data-tone={tone} style={{ display: "flex", flexWrap: "wrap", gap: 4, marginTop: 4, ...style }}>
      {checks.missingTargets.length > 0 && tag("red", `没用上：${checks.missingTargets.join("、")}`, "句子里没有用上这个目标词（允许词形变化）")}
      {checks.outOfScope.length > 0 && tag("gold", `超纲：${checks.outOfScope.join("、")}`, "不在已学 / 本单元及之前的词里")}
      {checks.tooLong && tag("gold", `太长 ${checks.words}/${checks.maxWords} 词`, "超过这个学段的单句上限")}
      {checks.structureDeviates && tag("gold", `结构偏离 ${(checks.similarity ?? 0).toFixed(2)}`, "和原句的句型结构相似度偏低")}
    </div>
  );
}

/** 一组结果里有几句带标记 */
export function countFlagged(list: { checks?: SentenceChecks | null }[]): number {
  return list.filter((x) => checkTone(x.checks) !== "ok").length;
}
