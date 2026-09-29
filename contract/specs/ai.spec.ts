/**
 * AI 内容生成：状态探测、发音（非网络部分）、例句（单词 / 单元）、巩固短文（预览 / 生成 / 列表 / 详情 / 删除）。
 * 改写自旧 apps/api/tests/ai-jobs.flow.test.ts（后台任务轮询部分见 ai-jobs.spec.ts）。
 *
 * 用测试里起的假 OpenAI 兼容服务，不依赖真实 AI；每个用例用 STAMP 建自己的词书/单元/单词。
 * 结束前一律 DELETE /settings/ai，不影响其他并发跑的 agent。
 * 发音：不打真实发音源，只验证未登录 / 单词不存在 / 权限等非网络行为。
 */
import { afterAll, describe, expect, it } from "vitest";
import { anon, login, STAMP } from "../lib/client";
import { ready } from "../lib/areas";
import { expectShape } from "../lib/shape";
import { startFakeAi, type FakeAi } from "../lib/fake-ai";

const KEY = "sk-contract-ai-secret-4242";

// Word.spelling 全局唯一：每次调用用递增序号加进拼写，避免不同用例之间撞名互相复用（进而带出例句等状态）。
let unitSeq = 0;

async function makeUnitWithWords(teacher: Awaited<ReturnType<typeof login>>, n: number) {
  const tag = `${STAMP}x${++unitSeq}`;
  const book = await teacher.post("/books", { name: `ai-${tag}` });
  const bookId = book.body.data.id as string;
  const unit = await teacher.post(`/books/${bookId}/units`, { name: "AI Unit" });
  const unitId = unit.body.data.id as string;
  const ids: string[] = [];
  for (let i = 0; i < n; i++) {
    const spelling = `vxai${["alpha", "bravo", "charlie", "delta", "echo"][i]}${tag}`;
    const r = await teacher.post(`/units/${unitId}/words`, { spelling, definition: `测试词${i + 1}` });
    expect(r.status).toBe(200);
    const list = await teacher.get(`/units/${unitId}/words?q=${spelling}`);
    ids.push(list.body.data.items[0].id as string);
  }
  return { bookId, unitId, ids };
}

describe.runIf(ready("ai", "settings"))("AI 内容生成", () => {
  let fake: FakeAi;

  afterAll(async () => {
    const admin = await login("admin@vinx.test");
    await admin.del("/settings/ai");
    await fake?.close();
  });

  it("/ai/status：未配置时 enabled 为假；配置后带 provider/model/baseUrl/audio", async () => {
    const admin = await login("admin@vinx.test");
    await admin.del("/settings/ai");
    const off = await admin.get("/ai/status");
    expect(off.status).toBe(200);
    expect(off.body.data).toMatchObject({ enabled: false, provider: "none", model: null, baseUrl: null });
    expect(typeof off.body.data.audio).toBe("boolean");

    fake = await startFakeAi();
    await admin.put("/settings/ai", { provider: "openai", baseUrl: fake.url, apiKey: KEY, model: "fake-model", timeoutSec: 10 });
    const on = await admin.get("/ai/status");
    expect(on.body.data).toMatchObject({ enabled: true, provider: "openai", model: "fake-model", baseUrl: fake.url });
    expectShape("ai-status", on.body);
  });

  it("发音（非网络部分）：未登录 401；单词不存在 404；预缓存需要 books.edit", async () => {
    const teacher = await login("teacher@vinx.test");
    const student = await login("student@vinx.test");
    expect((await anon().get("/audio/words/does-not-exist")).status).toBe(401);
    const notFound = await teacher.get("/audio/words/does-not-exist");
    expect(notFound.status).toBe(404);
    const { unitId } = await makeUnitWithWords(teacher, 1);
    expect((await student.post(`/audio/units/${unitId}/prefetch`)).status).toBe(403);
  });

  it("单词补例句：预览不调用 AI；生成写回 Word；回复没用上目标词时任务失败", async () => {
    const admin = await login("admin@vinx.test");
    await admin.put("/settings/ai", { provider: "openai", baseUrl: fake.url, apiKey: KEY, model: "fake-model", timeoutSec: 10 });
    const student = await login("student@vinx.test");
    const { ids } = await makeUnitWithWords(admin, 1);
    const wordId = ids[0];

    fake.requests.length = 0;
    const preview = await admin.post(`/ai/words/${wordId}/example/preview`);
    expect(preview.status).toBe(200);
    expect(preview.body.data.words).toHaveLength(1);
    expect(preview.body.data.words[0].id).toBe(wordId);
    expect(fake.requests).toHaveLength(0);
    expect((await student.post(`/ai/words/${wordId}/example/preview`)).status).toBe(403);
    expectShape("ai-example-preview", preview.body);

    const spelling = preview.body.data.words[0].spelling as string;
    fake.set({ reply: JSON.stringify([{ spelling, example: "This sentence has nothing to do with it.", exampleCn: "无关" }]) });
    const bad = await admin.post(`/ai/words/${wordId}/example`, { prompt: preview.body.data.prompt });
    expect(bad.status).toBe(200);
    expect(typeof bad.body.data.jobId).toBe("string");
    const badJob = await waitJob(admin, bad.body.data.jobId);
    expect(badJob.status).toBe("failed");
    expect(badJob.error).toContain("AI 没能生成合格的例句");

    fake.set({ reply: JSON.stringify([{ spelling, example: `I really like ${spelling} today.`, exampleCn: "我今天很喜欢它。" }]) });
    const started = await admin.post(`/ai/words/${wordId}/example`, { prompt: preview.body.data.prompt });
    const job = await waitJob(admin, started.body.data.jobId);
    expect(job).toMatchObject({ kind: "example", status: "done" });
    expect(job.result.items[0]).toMatchObject({ wordId, spelling, example: `I really like ${spelling} today.` });
    expectShape("ai-job-example-done", job);
  });

  it("单元批量补例句：预览列出缺例句的词与 remaining；生成写回；学生无权", async () => {
    const admin = await login("admin@vinx.test");
    const student = await login("student@vinx.test");
    const { unitId, ids } = await makeUnitWithWords(admin, 2);

    const preview = await admin.post(`/ai/units/${unitId}/examples/preview`, {});
    expect(preview.status).toBe(200);
    expect(preview.body.data.words.map((w: { id: string }) => w.id)).toEqual(ids);
    expect(preview.body.data.remaining).toBe(2);
    expect((await student.post(`/ai/units/${unitId}/examples/preview`, {})).status).toBe(403);
    expect((await student.post(`/ai/units/${unitId}/examples`, { prompt: "x" })).status).toBe(403);

    const spellings = preview.body.data.words.map((w: { spelling: string }) => w.spelling) as string[];
    fake.set({
      reply: JSON.stringify(spellings.map((s) => ({ spelling: s, example: `I like ${s} very much.`, exampleCn: "我很喜欢它。" }))),
    });
    const started = await admin.post(`/ai/units/${unitId}/examples`, { prompt: preview.body.data.prompt });
    expect(started.status).toBe(200);
    const job = await waitJob(admin, started.body.data.jobId);
    expect(job).toMatchObject({ kind: "examples", status: "done" });
    expect(job.result.items).toHaveLength(2);
    expect(job.result.failed).toEqual([]);
    expect(job.result.remaining).toBe(0);
    expectShape("ai-job-examples-done", job);

    const noMore = await admin.post(`/ai/units/${unitId}/examples`, {});
    expect(noMore.status).toBe(404);
    expect(noMore.body.error.code).toBe("NO_DATA");
  });

  it("巩固短文：预览按给定 wordIds；生成后台完成、存进列表、可查详情、可删除", async () => {
    const admin = await login("admin@vinx.test");
    const student = await login("student@vinx.test");
    const { ids } = await makeUnitWithWords(admin, 3);

    const preview = await student.post("/ai/passages/preview", { wordIds: ids, topic: "  运动会  " });
    expect(preview.status).toBe(200);
    expect(preview.body.data.words.map((w: { id: string }) => w.id)).toEqual(ids);
    expect(preview.body.data.prompt.endsWith("主题倾向：运动会")).toBe(true);
    expectShape("ai-passage-preview", preview.body);

    const words = preview.body.data.words as { spelling: string }[];
    fake.set({
      reply: JSON.stringify({
        title: "Sports Day",
        titleCn: "运动会",
        passage: `${words.map((w) => w.spelling).join(" ")}.`,
        passageCn: "今天我们在学校用到这些词。",
        questions: [{ q: "问题一？", a: "答案一" }],
      }),
    });
    const started = await student.post("/passages/generate", { wordIds: ids, prompt: "写一篇关于运动会的短文" });
    expect(started.status).toBe(200);
    const job = await waitJob(student, started.body.data.jobId);
    expect(job).toMatchObject({ kind: "passage", status: "done" });
    expect(job.result.missingWords).toEqual([]);
    expectShape("ai-job-passage-done", job);

    const passageId = job.result.id as string;
    const detail = await student.get(`/passages/${passageId}`);
    expect(detail.status).toBe(200);
    expect(detail.body.data).toMatchObject({ id: passageId, title: "Sports Day", titleCn: "运动会", model: "fake-model" });
    expect(detail.body.data.words.map((w: { id: string }) => w.id)).toEqual(ids);
    expectShape("ai-passage-detail", detail.body);

    const list = await student.get("/passages");
    expect(list.status).toBe(200);
    expect(list.body.data.items.some((p: { id: string }) => p.id === passageId)).toBe(true);
    expectShape("ai-passage-list", list.body);

    // 别人看不到（视为不存在）
    const teacher = await login("teacher@vinx.test");
    expect((await teacher.get(`/passages/${passageId}`)).status).toBe(404);
    expect((await teacher.del(`/passages/${passageId}`)).status).toBe(404);

    const del = await student.del(`/passages/${passageId}`);
    expect(del.status).toBe(200);
    expect(del.body).not.toHaveProperty("data");
    expect((await student.get(`/passages/${passageId}`)).status).toBe(404);
  });

  it("参数校验：单词不足 3 个 404 NO_DATA；提示词为空或超长 400", async () => {
    const admin = await login("admin@vinx.test");
    const { ids } = await makeUnitWithWords(admin, 2);
    const few = await admin.post("/passages/generate", { wordIds: ids, prompt: "写短文" });
    expect(few.status).toBe(404);
    expect(few.body.error.code).toBe("NO_DATA");

    const { ids: ids3 } = await makeUnitWithWords(admin, 3);
    expect((await admin.post("/passages/generate", { wordIds: ids3, prompt: "   " })).status).toBe(400);
    expect((await admin.post("/passages/generate", { wordIds: ids3, prompt: "x".repeat(6001) })).status).toBe(400);
  });

  it("AI 未配置时生成接口返回 400 INVALID_ACTION", async () => {
    const admin = await login("admin@vinx.test");
    await admin.del("/settings/ai");
    const { ids } = await makeUnitWithWords(admin, 3);
    const r = await admin.post("/passages/generate", { wordIds: ids, prompt: "写短文" });
    expect(r.status).toBe(400);
    expect(r.body.error).toMatchObject({ code: "INVALID_ACTION" });
    await admin.put("/settings/ai", { provider: "openai", baseUrl: fake.url, apiKey: KEY, model: "fake-model", timeoutSec: 10 });
  });
});

async function waitJob(who: Awaited<ReturnType<typeof login>>, jobId: string, timeoutMs = 20_000) {
  const started = Date.now();
  for (;;) {
    const r = await who.get(`/ai/jobs/${jobId}`);
    expect(r.status).toBe(200);
    if (r.body.data.status !== "running") return r.body.data;
    if (Date.now() - started > timeoutMs) throw new Error("任务超时未结束");
    await new Promise((res) => setTimeout(res, 100));
  }
}
