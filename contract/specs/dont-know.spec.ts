/**
 * 由旧 apps/api/tests/dont-know.flow.test.ts 改写为纯 HTTP 黑盒：「不会」按答错计入所有口径，只在记录上单独标记（K39）。
 *
 * 与旧测试的差异：旧测试由老师建班、安排计划并直接查库；这里新注册的学生给自己建计划（seed 系统词书），
 * 数据库断言改为经 /study/sessions/:id、/records/sessions/:id、/records/words/:wordId 观察；
 * 检测组进行中拿不到标准答案，用单词历史查到已学词的拼写与释义后作答。
 */
import { describe, it, expect, beforeAll } from "vitest";
import { STAMP, type Client } from "../lib/client";
import { ready } from "../lib/areas";
import { newStudent, systemUnitId } from "../lib/study";

interface Item {
  wordId: string;
  definition: string;
  answer: string;
  modes?: string[];
}

interface RecordAnswer {
  mode: string;
  phase: string;
  correct: boolean | null;
  userAnswer: string | null;
  dontKnow: boolean;
}

describe.runIf(ready("study", "plans"))("「不会」作答", () => {
  let student: Client;
  let unitId = "";
  let planId = "";
  const dontKnowWords: string[] = [];

  beforeAll(async () => {
    ({ client: student } = await newStudent("dk"));
    unitId = await systemUnitId(student);
    const plan = await student.post("/plans", { name: `不会 ${STAMP}`, newPerDay: 10, reviewPerDay: 30, modes: ["recognition", "spelling", "cloze"], unitIds: [unitId] });
    expect(plan.status).toBe(200);
    planId = plan.body.data.id;
  });

  it("练习中点「不会」：各题型都返回答错和正确答案，记录标记 dontKnow 且不存填写内容", async () => {
    const start = await student.post("/study/sessions", { kind: "learn", planId });
    expect(start.status).toBe(200);
    const sessionId = start.body.data.id;
    const detail = await student.get(`/study/sessions/${sessionId}`);
    const modes = detail.body.data.modes as string[];
    expect(modes).toEqual(["recognition", "spelling", "cloze"]);
    const items = detail.body.data.items as Item[];
    expect(items.length).toBeGreaterThanOrEqual(3);
    // 缺例句（或例句里找不到目标词）的词不出挖空题（K27）
    for (const i of items) expect(i.modes!.filter((m) => m !== "cloze")).toEqual(["recognition", "spelling"]);

    const [recWord, spellWord] = items;
    const clozeWord = items.find((i) => i !== recWord && i !== spellWord && (i.modes ?? modes).includes("cloze"));

    for (const item of items) {
      for (const mode of item.modes ?? modes) {
        const dontKnow = (item === recWord && mode === "recognition") || (item === spellWord && mode === "spelling") || (item === clozeWord && mode === "cloze");
        const answer = dontKnow ? "" : mode === "recognition" ? item.definition : item.answer;
        const r = await student.post(`/study/sessions/${sessionId}/answers`, { wordId: item.wordId, mode, phase: "practice", attempt: 1, answer, dontKnow, durationMs: 2000 });
        expect(r.status).toBe(200);
        if (dontKnow) expect(r.body.data).toEqual({ recorded: true, correct: false, expected: mode === "recognition" ? item.definition : item.answer });
        else expect(r.body.data.correct).toBe(true);
      }
    }
    dontKnowWords.push(recWord.wordId, spellWord.wordId);
    if (clozeWord) dontKnowWords.push(clozeWord.wordId);

    // 网络重试同一作答：按已存结果返回答错
    const dup = await student.post(`/study/sessions/${sessionId}/answers`, { wordId: spellWord.wordId, mode: "spelling", phase: "practice", attempt: 1, answer: "", dontKnow: true });
    expect(dup.body.data).toMatchObject({ recorded: true, correct: false, expected: spellWord.answer });

    // 「不会」时即使带了正确内容也不判定，记为答错
    const withText = await student.post(`/study/sessions/${sessionId}/answers`, { wordId: recWord.wordId, mode: "recognition", phase: "consolidate", attempt: 1, answer: recWord.definition, dontKnow: true });
    expect(withText.body.data).toMatchObject({ correct: false });

    const rows = ((await student.get(`/study/sessions/${sessionId}`)).body.data.answers as RecordAnswer[]).filter((a) => a.dontKnow);
    expect(rows).toHaveLength(dontKnowWords.length + 1);
    expect(rows.every((a) => a.correct === false && a.userAnswer === "")).toBe(true);

    // 巩固阶段答对，结算仍按首次作答算错
    await student.post(`/study/sessions/${sessionId}/answers`, { wordId: spellWord.wordId, mode: "spelling", phase: "consolidate", attempt: 1, answer: spellWord.answer });
    const done = await student.post(`/study/sessions/${sessionId}/complete`);
    expect(done.status).toBe(200);
    const result = done.body.data.result;
    expect([...result.wrongWordIds].sort()).toEqual([...dontKnowWords].sort());
    expect(result.ratings.again).toBe(dontKnowWords.length);
    expect(result.totalFirst - result.correctFirst).toBe(dontKnowWords.length);

    // 逐题明细与单词历史带出 dontKnow
    const record = await student.get(`/records/sessions/${sessionId}`);
    const recAnswers = record.body.data.words.find((w: { wordId: string }) => w.wordId === recWord.wordId).answers as RecordAnswer[];
    expect(recAnswers.find((a) => a.mode === "recognition" && a.phase === "practice")).toMatchObject({ correct: false, dontKnow: true, userAnswer: "" });
    const others = record.body.data.words.find((w: { wordId: string }) => !dontKnowWords.includes(w.wordId)).answers as RecordAnswer[];
    expect(others.every((a) => a.dontKnow === false)).toBe(true);
    const history = await student.get(`/records/words/${spellWord.wordId}`);
    expect((history.body.data.answers as RecordAnswer[]).some((a) => a.mode === "spelling" && a.dontKnow && !a.correct)).toBe(true);
  });

  it("错词强化能选到点过「不会」的词", async () => {
    const t = await student.get("/today");
    expect(t.body.data.drillAvailable).toBe(dontKnowWords.length);
    const s = await student.post("/study/sessions", { kind: "drill" });
    expect(s.status).toBe(200);
    const detail = await student.get(`/study/sessions/${s.body.data.id}`);
    expect((detail.body.data.items as Item[]).map((i) => i.wordId).sort()).toEqual([...dontKnowWords].sort());
    expect((await student.del(`/study/sessions/${s.body.data.id}`)).status).toBe(200);
  });

  it("检测中点「不会」：不返回对错，成绩算错题，明细标 dontKnow", async () => {
    const plan = await student.post("/plans", { name: `不会检测 ${STAMP}`, kind: "test", modes: ["recognition", "spelling"], testSize: 5, testScope: "learned", unitIds: [unitId] });
    expect(plan.status).toBe(200);
    const s = await student.post("/study/sessions", { kind: "test", planId: plan.body.data.id });
    expect(s.status).toBe(200);
    const sessionId = s.body.data.id;

    const snap = (await student.get(`/study/sessions/${sessionId}`)).body.data as { items: Item[]; modes: string[] };
    const [first, second] = snap.items;
    let total = 0;
    for (const item of snap.items) {
      const word = (await student.get(`/records/words/${item.wordId}`)).body.data.word as { spelling: string; definition: string };
      for (const mode of item.modes ?? snap.modes) {
        total++;
        const dontKnow = (item === first && mode === "recognition") || (item === second && mode === "spelling");
        const answer = dontKnow ? "" : mode === "recognition" ? word.definition : word.spelling;
        const r = await student.post(`/study/sessions/${sessionId}/answers`, { wordId: item.wordId, mode, phase: "test", attempt: 1, answer, dontKnow });
        expect(r.status).toBe(200);
        expect(r.body.data).toEqual({ recorded: true });
      }
    }

    // 进行中不下发对错
    const active = await student.get(`/study/sessions/${sessionId}`);
    expect((active.body.data.answers as RecordAnswer[]).every((a) => a.correct === null)).toBe(true);
    expect((active.body.data.answers as RecordAnswer[]).filter((a) => a.dontKnow)).toHaveLength(2);

    const done = await student.post(`/study/sessions/${sessionId}/complete`);
    expect(done.body.data.result).toMatchObject({ totalFirst: total, correctFirst: total - 2 });
    expect([...done.body.data.result.wrongWordIds].sort()).toEqual([first.wordId, second.wordId].sort());

    const record = await student.get(`/records/sessions/${sessionId}`);
    const firstAnswers = record.body.data.words.find((w: { wordId: string }) => w.wordId === first.wordId).answers as RecordAnswer[];
    expect(firstAnswers.find((a) => a.mode === "recognition")).toMatchObject({ correct: false, dontKnow: true, userAnswer: "" });
    expect(firstAnswers.find((a) => a.mode === "spelling")).toMatchObject({ correct: true, dontKnow: false });
    const secondAnswers = record.body.data.words.find((w: { wordId: string }) => w.wordId === second.wordId).answers as RecordAnswer[];
    expect(secondAnswers.find((a) => a.mode === "spelling")).toMatchObject({ correct: false, dontKnow: true });
  });
});
