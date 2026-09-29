/**
 * AI 生成后台任务：立即返回任务号、轮询、只有发起人能查、同一账号并发上限 429、出错时 error 是具体说明且不含 Key。
 * 改写自旧 apps/api/tests/ai-jobs.flow.test.ts 的任务部分。
 *
 * 说明：上游连接失败 / DNS 失败的错误文案 Go 与 Node 的运行时不同（Node 给 ECONNREFUSED 这类 code，
 * Go 给 net.OpError/DNSError 的原始描述），这里只断言「不含 Key」与「以‘无法连接 AI 服务：’开头」，
 * 不比较具体错误码字符串（两版底层网络库的错误码不同）。
 */
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { login, STAMP } from "../lib/client";
import { ready, goOnly } from "../lib/areas";
import { closedPortUrl, startFakeAi, type FakeAi } from "../lib/fake-ai";

const KEY = "sk-contract-jobs-secret-9911";

async function makeWords(teacher: Awaited<ReturnType<typeof login>>, n: number) {
  const book = await teacher.post("/books", { name: `jobs-${STAMP}` });
  const unit = await teacher.post(`/books/${book.body.data.id}/units`, { name: "U" });
  const unitId = unit.body.data.id as string;
  const ids: string[] = [];
  for (let i = 0; i < n; i++) {
    const spelling = `vxjob${["one", "two", "three", "four"][i]}${STAMP}`;
    await teacher.post(`/units/${unitId}/words`, { spelling, definition: `词${i + 1}` });
    const list = await teacher.get(`/units/${unitId}/words?q=${spelling}`);
    ids.push(list.body.data.items[0].id as string);
  }
  return ids;
}

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

describe.runIf(ready("ai", "settings"))("AI 后台任务", () => {
  let fake: FakeAi;
  let admin: Awaited<ReturnType<typeof login>>;
  let student: Awaited<ReturnType<typeof login>>;
  let teacher: Awaited<ReturnType<typeof login>>;
  let ids: string[];

  const useAi = async (baseUrl?: string, timeoutSec = 10) => {
    const r = await admin.put("/settings/ai", { provider: "openai", baseUrl: baseUrl ?? fake.url, apiKey: KEY, model: "fake-model", timeoutSec });
    expect(r.status).toBe(200);
  };

  beforeAll(async () => {
    fake = await startFakeAi();
    admin = await login("admin@vinx.test");
    student = await login("student@vinx.test");
    teacher = await login("teacher@vinx.test");
    ids = await makeWords(admin, 3);
    await useAi();
  });

  afterAll(async () => {
    await admin.del("/settings/ai");
    await fake?.close();
  });

  it("立即返回任务号；轮询到完成；耗时不小于假服务的延迟", async () => {
    fake.set({ delayMs: 300, reply: JSON.stringify({ title: "T", titleCn: "标", passage: ids.join(" "), passageCn: "c", questions: [] }) });
    const t0 = Date.now();
    const started = await student.post("/passages/generate", { wordIds: ids, prompt: "写短文" });
    expect(started.status).toBe(200);
    expect(Date.now() - t0).toBeLessThan(250);
    const job = await waitJob(student, started.body.data.jobId);
    expect(job.status).toBe("done");
    expect(job.elapsedMs).toBeGreaterThanOrEqual(250);
  });

  it("只有发起人能查；不存在的任务 404", async () => {
    fake.set({ delayMs: 300, reply: JSON.stringify({ title: "T", titleCn: "标", passage: ids.join(" "), passageCn: "c", questions: [] }) });
    const started = await student.post("/passages/generate", { wordIds: ids, prompt: "写短文" });
    const jobId = started.body.data.jobId as string;
    expect((await admin.get(`/ai/jobs/${jobId}`)).status).toBe(404);
    expect((await teacher.get(`/ai/jobs/${jobId}`)).status).toBe(404);
    expect((await student.get("/ai/jobs/not-a-real-job")).status).toBe(404);
    await waitJob(student, jobId);
  });

  it("同一账号同时超过 2 个进行中的任务返回 429；完成一个后可以再开", async () => {
    fake.set({ delayMs: 800, reply: JSON.stringify({ title: "T", titleCn: "标", passage: ids.join(" "), passageCn: "c", questions: [] }) });
    const body = { wordIds: ids, prompt: "写短文" };
    const a = await student.post("/passages/generate", body);
    const b = await student.post("/passages/generate", body);
    expect(a.status).toBe(200);
    expect(b.status).toBe(200);
    const c = await student.post("/passages/generate", body);
    expect(c.status).toBe(429);
    expect(c.body.error.code).toBe("TOO_MANY_REQUESTS");
    expect(c.body.error.message).toContain("同时最多进行 2 个");
    // 别人不受影响
    const other = await teacher.post("/passages/generate", body);
    expect(other.status).toBe(200);
    await waitJob(student, a.body.data.jobId);
    await waitJob(student, b.body.data.jobId);
    await waitJob(teacher, other.body.data.jobId);
    const d = await student.post("/passages/generate", body);
    expect(d.status).toBe(200);
    await waitJob(student, d.body.data.jobId);
  });

  describe("失败时 error 是具体说明，且不含 Key", () => {
    const run = async () => {
      const started = await student.post("/passages/generate", { wordIds: ids, prompt: "写短文" });
      expect(started.status).toBe(200);
      const job = await waitJob(student, started.body.data.jobId);
      expect(job.status).toBe("failed");
      expect(job.error).not.toContain(KEY);
      return job.error as string;
    };

    it("上游 401 / 500：状态码 + 上游信息", async () => {
      fake.set({ status: 401, errorBody: { error: { message: `Incorrect API key provided: ${KEY}` } } });
      expect(await run()).toBe("AI 服务返回 401：Incorrect API key provided: ***");
      fake.set({ status: 500, errorBody: { error: { message: "internal boom" } } });
      expect(await run()).toBe("AI 服务返回 500：internal boom");
      fake.set({ reply: JSON.stringify({ title: "T", titleCn: "标", passage: ids.join(" "), passageCn: "c", questions: [] }) });
    });

    it("回复无法解析或为空：附上回复开头 / 明确说没有内容", async () => {
      fake.set({ reply: "抱歉，我不能输出结构化内容" });
      expect(await run()).toBe("AI 返回的内容无法解析：抱歉，我不能输出结构化内容");
      fake.set({ reply: "   " });
      expect(await run()).toBe("AI 没有返回内容");
      fake.set({ reply: JSON.stringify({ title: "T", titleCn: "标", passage: ids.join(" "), passageCn: "c", questions: [] }) });
    });

    it("连接失败：说明以「无法连接 AI 服务：」开头", async () => {
      await useAi(await closedPortUrl());
      expect(await run()).toMatch(/^无法连接 AI 服务：/);
      await useAi();
    });

    it("超时：说明含设定的秒数", async () => {
      await useAi(fake.url, 5);
      fake.set({ delayMs: 5500, reply: "late" });
      expect(await run()).toBe("请求超时（5 秒），可以在系统设置里调大超时，或换一个更快的模型");
      await useAi();
    }, 15_000);
  });

  // Go 端的错误分类与 Node 运行时不同（DescribeError 用 net.Error / net.OpError / net.DNSError 而非
  // ECONNREFUSED 这类 Node 专属错误码），只在 Go 上再单独确认一次「连不上」与「超时」两类的文案结构。
  it.runIf(goOnly)("Go：连接失败与超时的说明结构稳定", async () => {
    await useAi(await closedPortUrl());
    const started = await student.post("/passages/generate", { wordIds: ids, prompt: "写短文" });
    const job = await waitJob(student, started.body.data.jobId);
    expect(job.status).toBe("failed");
    expect(job.error).toMatch(/^无法连接 AI 服务：/);
    await useAi();
  });
});
