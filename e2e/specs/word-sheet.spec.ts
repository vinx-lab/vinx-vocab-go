import { test, expect, type APIRequestContext } from "@playwright/test";
import { PASSWORD, uid } from "../support/helpers";

/** 走 API 学完一组新词，让账号有已学词（单词单只从已学词里挑） */
async function learnOneGroup(request: APIRequestContext) {
  const books = (await (await request.get("/api/books")).json()).data;
  const book = books.items.find((b: { name: string }) => b.name === "八年级上册");
  const detail = (await (await request.get(`/api/books/${book.id}`)).json()).data;
  const unitId = detail.units[0].id;
  const plan = (await (await request.post("/api/plans", { data: { name: "E2E 计划", unitIds: [unitId], newPerDay: 10 } })).json()).data;
  const sid = (await (await request.post("/api/study/sessions", { data: { kind: "learn", planId: plan.id } })).json()).data.id;
  const s = (await (await request.get(`/api/study/sessions/${sid}`)).json()).data;
  for (const item of s.items) {
    for (const mode of s.modes) {
      const answer = mode === "recognition" ? "__wrong__" : "zzz"; // 全错：稳定性低，保证入选
      await request.post(`/api/study/sessions/${sid}/answers`, { data: { wordId: item.wordId, mode, phase: "practice", attempt: 1, answer } });
    }
  }
  await request.post(`/api/study/sessions/${sid}/complete`);
}

test.describe("单词单闭环", () => {
  test.setTimeout(120_000);

  test("生成 → 打印页 → 今日开测 → 交卷 → 用错词再出一张", async ({ page }) => {
    const email = `ws-${uid()}@vinx.test`;
    await page.goto("/login");
    await page.getByText("注册", { exact: true }).click();
    await page.getByLabel("姓名").fill("单词单同学");
    await page.getByLabel("邮箱").fill(email);
    await page.getByLabel("密码").fill(PASSWORD);
    await page.getByRole("button", { name: /注册并开始学习/ }).click();
    await expect(page).toHaveURL(/\/today/);

    await learnOneGroup(page.request);

    // 生成：不触发真实打印对话框
    await page.addInitScript(() => { window.print = () => {}; });
    await page.goto("/sheets/new");
    await expect(page.getByText("刚记住").first()).toBeVisible();
    await page.getByRole("button", { name: /生成并打印/ }).click();
    await page.waitForURL(/\/sheets\/[^/]+\/print/);
    await expect(page.getByText(/VinxVocab 单词单 #1/)).toBeVisible();
    await expect(page.getByText(/回家后：登录 → 今日 → 单词单 #1 测试/)).toBeVisible();

    // 今日开测
    await page.goto("/today");
    await page.getByRole("button", { name: "开始测试" }).click();
    await page.waitForURL(/\/study\//);
    const sid = page.url().split("/").pop()!;

    // 走 API 作答（全部答错）并交卷，再看结果页
    const s = (await (await page.request.get(`/api/study/sessions/${sid}`)).json()).data;
    for (const item of s.items) {
      for (const mode of s.modes) {
        await page.request.post(`/api/study/sessions/${sid}/answers`, { data: { wordId: item.wordId, mode, phase: "test", attempt: 1, answer: "zzz" } });
      }
    }
    await page.request.post(`/api/study/sessions/${sid}/complete`);
    await page.goto(`/study/${sid}`);
    await expect(page.getByText("单词单测试完成")).toBeVisible();

    await page.getByRole("button", { name: /用错词再出一张/ }).click();
    await page.waitForURL(/\/sheets\/new\?from=/);
    await expect(page.getByText("上次测错").first()).toBeVisible();
  });

  test("一次生成 4 份 → 合并打印页 4 页、编号连续；今日只显示下一份；列表多选合并打印", async ({ page }) => {
    const email = `wb-${uid()}@vinx.test`;
    await page.goto("/login");
    await page.getByText("注册", { exact: true }).click();
    await page.getByLabel("姓名").fill("批量单词单同学");
    await page.getByLabel("邮箱").fill(email);
    await page.getByLabel("密码").fill(PASSWORD);
    await page.getByRole("button", { name: /注册并开始学习/ }).click();
    await expect(page).toHaveURL(/\/today/);
    await learnOneGroup(page.request);

    await page.addInitScript(() => { window.print = () => {}; });
    await page.goto("/sheets/new");
    await expect(page.getByText("刚记住").first()).toBeVisible();
    await page.getByLabel("份数").fill("4");
    await page.getByLabel("份数").blur();
    await expect(page.getByText(/生成 4 份/)).toBeVisible();
    await page.getByRole("button", { name: /生成并打印/ }).click();
    await page.waitForURL(/\/sheets\/print\?ids=/);

    const pages = page.locator(".vx-sheet-page");
    await expect(pages).toHaveCount(4);
    for (let i = 0; i < 4; i++) {
      await expect(pages.nth(i).locator(".vx-sheet-head")).toContainText(`VinxVocab 单词单 #${i + 1}`);
      await expect(pages.nth(i).locator(".vx-sheet-head")).toContainText("批量单词单同学");
      await expect(pages.nth(i).locator(".vx-sheet-foot")).toContainText(`今日 → 单词单 #${i + 1} 测试`);
    }
    // 打印版式：每份一页 A4，没有多余空白页；折线在纸张正中 105mm
    await page.emulateMedia({ media: "print" });
    const foldX = await page.locator(".vx-sheet-fold").first().evaluate((el) => el.getBoundingClientRect().left - el.closest(".vx-sheet-page")!.getBoundingClientRect().left);
    expect(Math.abs(foldX - (105 * 96) / 25.4)).toBeLessThan(2);
    const pdf = await page.pdf({ preferCSSPageSize: true, printBackground: true });
    expect(pdf.toString("latin1").match(/\/Type\s*\/Page[^s]/g)?.length).toBe(4);
    await page.emulateMedia({ media: "screen" });

    await page.goto("/today");
    await expect(page.getByText(/单词单 #1 · \d+ 词（还有 3 份待测）/)).toBeVisible();
    await expect(page.getByText(/单词单 #2/)).toHaveCount(0);

    await page.goto("/sheets");
    await page.getByLabel("选择单词单 #2").check();
    await page.getByLabel("选择单词单 #4").check();
    const [popup] = await Promise.all([page.waitForEvent("popup"), page.getByRole("button", { name: /合并打印所选（2）/ }).click()]);
    await expect(popup.locator(".vx-sheet-page")).toHaveCount(2);
    await expect(popup.locator(".vx-sheet-head").first()).toContainText("#2");
    await expect(popup.locator(".vx-sheet-head").nth(1)).toContainText("#4");
  });
});
