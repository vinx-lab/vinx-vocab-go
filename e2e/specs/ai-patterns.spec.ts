import { test, expect, type APIRequestContext } from "@playwright/test";
import { createServer, type Server } from "node:http";
import type { AddressInfo } from "node:net";
import { login } from "../support/helpers";
import { apiOk, importBook, letters, loginApi, newClassWithStudent, newTeacher } from "../support/school";

/**
 * AI 生成句型（spec 0005）：老师在单元的「句型」标签点「AI 生成」→ 生成草稿（带检查标记）→ 编辑草稿、删掉一句 → 保存
 * → 班里的学生在单元页看到保存的句型。
 * 不依赖真实 AI：测试里在 127.0.0.1 随机端口起假的 OpenAI 兼容服务，通过管理员设置接口指向它，结束后清除。
 */
const KEY = "sk-e2e-ai-patterns-secret-7788";
const PATTERN_REPLY = JSON.stringify([
  { en: "How do you learn English?", cn: "你怎样学英语？" },
  { en: "I learn English by reading.", cn: "我通过阅读学英语。", frame: "I learn ... by doing ..." },
  { en: "Do you study with a group?", cn: "你和小组一起学习吗？" },
]);

let fake: Server;
let admin: APIRequestContext;
const systems: string[] = [];

test.beforeAll(async () => {
  fake = createServer((req, res) => {
    let raw = "";
    req.on("data", (c) => (raw += c));
    req.on("end", () => {
      const msgs = (JSON.parse(raw || "{}").messages ?? []) as { role: string; content: string }[];
      systems.push(msgs.find((m) => m.role === "system")?.content ?? "");
      res.setHeader("content-type", "application/json");
      res.end(JSON.stringify({ choices: [{ message: { content: PATTERN_REPLY } }] }));
    });
  });
  await new Promise<void>((r) => fake.listen(0, "127.0.0.1", r));
  const url = `http://127.0.0.1:${(fake.address() as AddressInfo).port}/v1`;
  admin = await loginApi("admin@vinx.test");
  await apiOk(await admin.put("/api/settings/ai", { data: { provider: "openai", baseUrl: url, apiKey: KEY, model: "e2e-fake", timeoutSec: 10 } }));
});

test.afterAll(async () => {
  await admin?.delete("/api/settings/ai");
  await admin?.dispose();
  fake?.closeAllConnections();
  await new Promise((r) => fake?.close(r));
});

test.describe("AI 生成句型", () => {
  test.setTimeout(90_000);

  test("老师生成句型 → 编辑草稿 → 保存 → 学生在单元页看到", async ({ browser }) => {
    const teacher = await newTeacher("句型老师");
    const { student } = await newClassWithStudent(teacher, "句型班", "句型同学");
    const tag = letters();
    const book = await importBook(teacher.api, "句型词书", [
      { name: "Unit 1", entries: [{ spelling: `e2epatalpha${tag}`, definition: "甲" }, { spelling: `e2epatbravo${tag}`, definition: "乙" }] },
    ]);
    const edited = `I learn English by reading every day ${tag}.`;

    const tctx = await browser.newContext();
    const tp = await tctx.newPage();
    await login(tp, teacher.email);
    await expect(tp).toHaveURL(/\/today/);
    await tp.goto(`/books/${book.bookId}`);
    await tp.locator(".vx-unit-tabs .ant-segmented-item", { hasText: "句型" }).click();
    await expect(tp.getByText("这个单元还没有句型")).toBeVisible();
    await tp.getByRole("button", { name: "AI 生成" }).click();

    const modal = tp.getByRole("dialog", { name: "AI 生成句型" });
    await expect(modal).toBeVisible();
    await modal.getByLabel("话题或语法点").fill("学习方法");
    await modal.getByTestId("ai-generate").click();

    // 草稿：逐句可编辑
    await expect(modal.getByLabel("第 1 句英文")).toHaveValue("How do you learn English?");
    await expect(modal.getByLabel("第 2 句英文")).toHaveValue("I learn English by reading.");
    await expect(modal.getByLabel("第 2 句骨架")).toHaveValue("I learn ... by doing ...");
    expect(systems.some((s) => s.includes("重点句型"))).toBe(true);
    await modal.getByLabel("第 2 句英文").fill(edited);
    await modal.getByRole("button", { name: "删除第 3 句" }).click();
    await expect(modal.getByLabel("第 3 句英文")).toHaveCount(0);
    await modal.getByTestId("ai-save").click();
    await expect(tp.getByText("已保存 2 句")).toBeVisible();
    await expect(modal).toBeHidden();
    const list = tp.locator('.vx-unit-texts[data-kind="list"]');
    await expect(list).toContainText("Unit 1 重点句型");
    await expect(list).toContainText(edited);

    // 学生：单元页的句型
    const sctx = await browser.newContext();
    const sp = await sctx.newPage();
    await login(sp, student.email);
    await expect(sp).toHaveURL(/\/today/);
    await sp.goto(`/books/${book.bookId}`);
    await sp.locator(".vx-unit-tabs .ant-segmented-item", { hasText: "句型" }).click();
    const seen = sp.locator('.vx-unit-texts[data-kind="list"]');
    await expect(seen).toContainText("Unit 1 重点句型");
    await expect(seen).toContainText("How do you learn English?");
    await expect(seen).toContainText(edited);
    await expect(seen).not.toContainText("Do you study with a group?");
    await expect(sp.getByRole("button", { name: "AI 生成" })).toHaveCount(0);

    await tctx.close();
    await sctx.close();
    await teacher.api.dispose();
    await student.api.dispose();
  });
});
