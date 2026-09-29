import { describe, expect, it } from "vitest";
import { deriveFlow, firstRoundQueue, MAX_CONSOLIDATE_ATTEMPTS, modesOf } from "@/pages/study/engine";
import type { Mode, SessionAnswer, SessionItem } from "@/types";

const item = (wordId: string): SessionItem => ({
  wordId, spelling: wordId, answer: wordId, phonetic: null, partOfSpeech: "n.", definition: `${wordId}的意思`, example: null, exampleCn: null, type: "word", isNew: true, options: [],
});
const items = ["a", "b", "c"].map(item);
const modes: Mode[] = ["recognition", "spelling"];
const ans = (wordId: string, mode: Mode, phase: SessionAnswer["phase"], correct: boolean, attempt = 1): SessionAnswer => ({ wordId, mode, phase, attempt, correct, userAnswer: "", hintUsed: false });
const base = { sessionId: "s1", status: "active" as const, items, modes, cardsDone: false };

describe("按词题型", () => {
  const withCloze: SessionItem = { ...item("k"), cloze: "The ____ runs.", modes: ["recognition", "cloze"] };
  const noCloze: SessionItem = { ...item("m"), cloze: null, modes: ["recognition"] };

  it("缺例句的词不出挖空题", () => {
    const q = firstRoundQueue("s1", [withCloze, noCloze], ["recognition", "cloze"], "practice");
    expect(q.filter((x) => x.mode === "cloze").map((x) => x.wordId)).toEqual(["k"]);
    expect(q.filter((x) => x.mode === "recognition")).toHaveLength(2);
  });

  it("modesOf 没有 item.modes 时回退到组的题型", () => {
    expect(modesOf(item("a"), ["recognition", "spelling"])).toEqual(["recognition", "spelling"]);
    expect(modesOf(withCloze, ["recognition", "spelling"])).toEqual(["recognition", "cloze"]);
  });

  it("按词题型影响完成判定：只答完该词自己的题就算练完", () => {
    const answers = [ans("k", "recognition", "practice", true), ans("k", "cloze", "practice", true), ans("m", "recognition", "practice", true)];
    const f = deriveFlow({ ...base, kind: "review", items: [withCloze, noCloze], modes: ["recognition", "cloze"], answers });
    expect(f.stage).toBe("finish");
  });
});

describe("学习流程推导", () => {
  it("题序确定：同一组刷新后一致；认义在前拼写在后", () => {
    const q1 = firstRoundQueue("s1", items, modes, "practice");
    const q2 = firstRoundQueue("s1", items, modes, "practice");
    expect(q1).toEqual(q2);
    expect(q1.slice(0, 3).every((q) => q.mode === "recognition")).toBe(true);
    expect(q1).toHaveLength(6);
  });

  it("新学：未看完卡片且无作答 → cards；已有作答则跳过卡片", () => {
    expect(deriveFlow({ ...base, kind: "learn", answers: [] }).stage).toBe("cards");
    const f = deriveFlow({ ...base, kind: "learn", answers: [ans("a", "recognition", "practice", true)] });
    expect(f.stage).toBe("practice");
    expect(f.done).toBe(1);
  });

  it("复习直接练习；全部答对 → finish", () => {
    expect(deriveFlow({ ...base, kind: "review", answers: [] }).stage).toBe("practice");
    const all = items.flatMap((i) => modes.map((m) => ans(i.wordId, m, "practice", true)));
    expect(deriveFlow({ ...base, kind: "review", answers: all }).stage).toBe("finish");
  });

  it("有错 → 巩固错题；答对或达到上限后完成", () => {
    const practice = items.flatMap((i) => modes.map((m) => ans(i.wordId, m, "practice", !(i.wordId === "b" && m === "spelling"))));
    const f = deriveFlow({ ...base, kind: "review", answers: practice });
    expect(f.stage).toBe("consolidate");
    expect(f.current).toEqual({ wordId: "b", mode: "spelling", phase: "consolidate", attempt: 1 });
    expect(f.wrongWordIds).toEqual(["b"]);

    const retry = deriveFlow({ ...base, kind: "review", answers: [...practice, ans("b", "spelling", "consolidate", false, 1)] });
    expect(retry.current?.attempt).toBe(2);

    const fixed = deriveFlow({ ...base, kind: "review", answers: [...practice, ans("b", "spelling", "consolidate", true, 1)] });
    expect(fixed.stage).toBe("finish");

    const giveUp = Array.from({ length: MAX_CONSOLIDATE_ATTEMPTS }, (_, k) => ans("b", "spelling", "consolidate", false, k + 1));
    expect(deriveFlow({ ...base, kind: "review", answers: [...practice, ...giveUp] }).stage).toBe("finish");
  });

  it("检测：只按 test 阶段作答推进，答完 → finish", () => {
    const f = deriveFlow({ ...base, kind: "test", modes: ["recognition"], answers: [ans("a", "recognition", "test", false)] });
    expect(f).toMatchObject({ stage: "test", done: 1, total: 3 });
    const all = items.map((i) => ans(i.wordId, "recognition", "test", true));
    expect(deriveFlow({ ...base, kind: "test", modes: ["recognition"], answers: all }).stage).toBe("finish");
  });

  it("已完成的组 → finish", () => {
    expect(deriveFlow({ ...base, status: "completed", kind: "learn", answers: [] }).stage).toBe("finish");
  });
});

describe("单词单测试（sheet）", () => {
  it("与检测相同：只有 test 阶段、无认识卡片与巩固", () => {
    const flow = deriveFlow({ ...base, kind: "sheet", answers: [] });
    expect(flow.stage).toBe("test");
    expect(flow.total).toBe(items.length * modes.length);
    expect(flow.current?.phase).toBe("test");
  });
});
