import { test, expect, request as playwrightRequest, type APIRequestContext, type Page } from "@playwright/test";
import { createServer, type Server } from "node:http";
import type { AddressInfo } from "node:net";
import { PASSWORD, uid } from "../support/helpers";

/**
 * 生成前预览 + AI 后台任务（spec 0006 / K41），手机视口。
 * 不依赖真实 AI：测试里在 127.0.0.1 随机端口起一个假的 OpenAI 兼容服务，通过管理员设置接口指向它，结束后恢复为环境变量。
 */
test.use({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true });

const BASE = process.env.E2E_BASE_URL ?? "http://localhost:3250";
const KEY = "sk-e2e-ai-jobs-secret-5566";

interface Received {
  system: string;
  user: string;
}
let fake: Server;
let fakeUrl = "";
const received: Received[] = [];
let behavior: { delayMs?: number; status?: number; errorBody?: unknown; reply?: (req: Received) => string } = {};

let admin: APIRequestContext;

async function apiOk<T>(res: Awaited<ReturnType<APIRequestContext["get"]>>): Promise<T> {
  const body = await res.json();
  expect(res.ok(), JSON.stringify(body)).toBeTruthy();
  return body.data as T;
}

test.beforeAll(async () => {
  fake = createServer((req, res) => {
    let raw = "";
    req.on("data", (c) => (raw += c));
    req.on("end", () => {
      const msgs = (JSON.parse(raw || "{}").messages ?? []) as { role: string; content: string }[];
      const r: Received = { system: msgs.find((m) => m.role === "system")?.content ?? "", user: msgs.find((m) => m.role === "user")?.content ?? "" };
      received.push(r);
      const b = behavior;
      setTimeout(() => {
        if (res.destroyed) return;
        res.setHeader("content-type", "application/json");
        if (b.status) {
          res.statusCode = b.status;
          res.end(JSON.stringify(b.errorBody ?? {}));
        } else {
          res.end(JSON.stringify({ choices: [{ message: { content: b.reply ? b.reply(r) : "OK" } }] }));
        }
      }, b.delayMs ?? 0);
    });
  });
  await new Promise<void>((r) => fake.listen(0, "127.0.0.1", r));
  fakeUrl = `http://127.0.0.1:${(fake.address() as AddressInfo).port}/v1`;

  admin = await playwrightRequest.newContext({ baseURL: BASE });
  await apiOk(await admin.post("/api/auth/login", { data: { email: "admin@vinx.test", password: PASSWORD } }));
  await apiOk(await admin.put("/api/settings/ai", { data: { provider: "openai", baseUrl: fakeUrl, apiKey: KEY, model: "e2e-fake", timeoutSec: 10 } }));
});

test.afterAll(async () => {
  await admin?.delete("/api/settings/ai");
  await admin?.dispose();
  fake?.closeAllConnections();
  await new Promise((r) => fake?.close(r));
});

/** 注册一个学生并用接口学完一组新词（5 个），让「最近学过」有词可用；页面随之处于登录状态 */
async function studentWithLearnedWords(page: Page) {
  const email = `ai-${uid()}@vinx.test`;
  const me = await apiOk<{ user: { id: string } }>(await page.request.post("/api/auth/signup", { data: { email, password: PASSWORD, name: "AI 同学" } }));
  const books = await apiOk<{ items: { id: string; name: string }[] }>(await page.request.get("/api/books"));
  const book = await apiOk<{ units: { id: string }[] }>(await page.request.get(`/api/books/${books.items.find((b) => b.name === "七年级下册")!.id}`));
  const plan = await apiOk<{ id: string }>(
    await page.request.post("/api/plans", {
      data: { name: "AI 短文用", newPerDay: 5, reviewPerDay: 30, modes: ["recognition"], unitIds: [book.units[0].id], targets: { classIds: [], userIds: [me.user.id] } },
    }),
  );
  const session = await apiOk<{ id: string }>(await page.request.post("/api/study/sessions", { data: { kind: "learn", planId: plan.id } }));
  const detail = await apiOk<{ items: { wordId: string; definition: string }[] }>(await page.request.get(`/api/study/sessions/${session.id}`));
  for (const item of detail.items) {
    await apiOk(
      await page.request.post(`/api/study/sessions/${session.id}/answers`, {
        data: { wordId: item.wordId, mode: "recognition", phase: "practice", attempt: 1, answer: item.definition, durationMs: 1500 },
      }),
    );
  }
  await apiOk(await page.request.post(`/api/study/sessions/${session.id}/complete`, { data: {} }));
  return detail.items.length;
}

const passageReply = (req: Received) => {
  // 把用户消息里「1. word —— 释义」的单词都用上
  const words = [...req.user.matchAll(/^\d+\. (.+?) —— /gm)].map((m) => m[1]);
  return JSON.stringify({
    title: "E2E Sports Day",
    titleCn: "运动会",
    passage: `${words.join(" ")}.\n\nThe end.`,
    passageCn: "测试短文。",
    questions: [{ q: "问题？", a: "答案" }],
  });
};

test.describe("AI 生成前预览与后台任务", () => {
  test.setTimeout(90_000);

  test("短文页：选来源和词数后显示单词和提示词；去掉一个词、改一句提示词后生成，看到「生成中」再看到结果", async ({ page }) => {
    const learned = await studentWithLearnedWords(page);
    expect(learned).toBeGreaterThanOrEqual(4);
    await page.goto("/passages");

    await page.locator(".ant-segmented-item", { hasText: "最近学过" }).click();
    await page.locator(".ant-segmented-item", { hasText: /^5$/ }).click();

    const chips = page.getByTestId("ai-preview-words").locator(".ant-tag");
    await expect(chips).toHaveCount(Math.min(5, learned));
    const box = page.getByLabel("提示词");
    await expect(box).toHaveValue(new RegExp(`目标单词（${Math.min(5, learned)} 个）`));
    await expect(box).toHaveValue(/你是初中英语阅读材料编辑/);
    // 输出格式不在页面上
    expect(await box.inputValue()).not.toMatch(/json/i);

    // 去掉一个词，提示词跟着重拼
    const firstWord = (await chips.first().locator(".vx-word").innerText()).trim();
    await page.getByLabel(`去掉 ${firstWord}`).click();
    await expect(chips).toHaveCount(Math.min(5, learned) - 1);
    await expect(box).toHaveValue(new RegExp(`目标单词（${Math.min(5, learned) - 1} 个）`));
    expect(await box.inputValue()).not.toContain(`${firstWord} —— `);

    // 改一句提示词；「恢复」可以回到拼好的内容
    const original = await box.inputValue();
    const edited = original.replace("总长 80–140 个英文单词", "总长 60–100 个英文单词，故事发生在运动会上");
    await box.fill(edited);
    await page.getByRole("button", { name: /恢\s*复/ }).click();
    await expect(box).toHaveValue(original);
    await box.fill(edited);

    received.length = 0;
    behavior = { delayMs: 3500, reply: passageReply };
    await page.getByRole("button", { name: /生成短文/ }).click();
    await expect(page.getByText(/生成中… 已用 \d+ 秒/)).toBeVisible();
    await page.waitForURL(/\/passages\/[^/]+$/, { timeout: 20_000 });
    await expect(page.getByText("E2E Sports Day")).toBeVisible();

    expect(received).toHaveLength(1);
    expect(received[0].user).toBe(edited.trim());
    expect(received[0].system).toContain("JSON");
    expect(received[0].user).not.toContain(`${firstWord} —— `);

    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect(overflow).toBeLessThanOrEqual(0);
  });

  test("生成失败时显示具体原因，不显示 Key", async ({ page }) => {
    await studentWithLearnedWords(page);
    await page.goto("/passages");
    await page.locator(".ant-segmented-item", { hasText: "最近学过" }).click();
    await expect(page.getByTestId("ai-preview-words").locator(".ant-tag").first()).toBeVisible();
    await expect(page.getByLabel("提示词")).toHaveValue(/目标单词/);

    behavior = { status: 401, errorBody: { error: { message: `Incorrect API key provided: ${KEY}` } } };
    await page.getByRole("button", { name: /生成短文/ }).click();
    await expect(page.getByText("AI 服务返回 401：Incorrect API key provided: ***")).toBeVisible({ timeout: 15_000 });
    await expect(page.getByText(KEY)).toHaveCount(0);
  });

  test("词书页「AI 补例句」先弹出预览再生成；单个词也一样", async ({ page }) => {
    // 管理员自建词书和两个固定拼写的词（重复运行时复用词条，先清掉例句）
    const book = await apiOk<{ id: string }>(await admin.post("/api/books", { data: { name: `E2E AI 例句 ${uid()}` } }));
    const unit = await apiOk<{ id: string }>(await admin.post(`/api/books/${book.id}/units`, { data: { name: "Unit AI" } }));
    const spellings = ["e2eaijobalpha", "e2eaijobbravo"];
    for (const [i, s] of spellings.entries()) {
      await apiOk(await admin.post(`/api/units/${unit.id}/words`, { data: { spelling: s, definition: `测试词${i + 1}` } }));
    }
    const words = await apiOk<{ items: { id: string; spelling: string }[] }>(await admin.get(`/api/units/${unit.id}/words`));
    for (const w of words.items) await apiOk(await admin.patch(`/api/words/${w.id}`, { data: { example: null, exampleCn: null } }));

    try {
      await page.goto("/login");
      await page.getByLabel("账号").fill("admin@vinx.test");
      await page.getByLabel("密码").fill(PASSWORD);
      await page.getByRole("button", { name: /登\s*录/ }).click();
      await expect(page).toHaveURL(/\/today/);
      await page.goto(`/books/${book.id}`);

      behavior = {
        delayMs: 2500,
        reply: (req) =>
          JSON.stringify(spellings.filter((s) => req.user.includes(s)).map((s) => ({ spelling: s, example: `We use ${s} on sports day.`, exampleCn: "我们在运动会上用它。" }))),
      };
      received.length = 0;

      await page.getByRole("button", { name: /AI 补例句/ }).click();
      const dialog = page.getByRole("dialog");
      await expect(dialog.getByTestId("ai-preview-words").locator(".ant-tag")).toHaveCount(2);
      const box = dialog.getByLabel("提示词");
      await expect(box).toHaveValue(/请为下面 2 个词各写一个例句/);
      const edited = (await box.inputValue()).replace("长度 6–14 个英文单词", "长度 6–10 个英文单词");
      await box.fill(edited);
      await dialog.getByRole("button", { name: /生\s*成/ }).click();
      await expect(dialog.getByText(/生成中… 已用 \d+ 秒/)).toBeVisible();
      await expect(dialog.getByText("已生成 2 条例句，已写回词书")).toBeVisible({ timeout: 20_000 });
      expect(received.at(-1)!.user).toBe(edited.trim());
      await dialog.getByRole("button", { name: /完\s*成/ }).click();
      await expect(page.getByText(`We use ${spellings[0]} on sports day.`)).toBeVisible();

      // 单个词：重写例句
      behavior = { reply: () => JSON.stringify([{ spelling: spellings[1], example: `I like ${spellings[1]} a lot.`, exampleCn: "我很喜欢它。" }]) };
      await page.getByRole("button", { name: `AI 例句 ${spellings[1]}` }).click();
      const one = page.getByRole("dialog");
      await expect(one.getByTestId("ai-preview-words")).toContainText(spellings[1]);
      await expect(one.getByLabel("提示词")).toHaveValue(/请为下面 1 个词各写一个例句/);
      await one.getByRole("button", { name: /生\s*成/ }).click();
      await expect(one.getByText("已生成 1 条例句，已写回词书")).toBeVisible({ timeout: 15_000 });
      await one.getByRole("button", { name: /完\s*成/ }).click();
      await expect(page.getByText(`I like ${spellings[1]} a lot.`)).toBeVisible();
    } finally {
      await admin.delete(`/api/books/${book.id}`);
    }
  });
});
