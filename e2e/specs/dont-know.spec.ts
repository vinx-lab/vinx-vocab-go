import { test, expect, type Page } from "@playwright/test";
import { PASSWORD, uid } from "../support/helpers";

/** 「不会」按钮（docs/specs/0004-dont-know-button.md）：手机视口下学新词和检测各点一次 */
test.use({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true, deviceScaleFactor: 3 });

interface Item {
  wordId: string;
  spelling: string;
  definition: string;
  answer: string;
  options: string[];
}

// antd 双字按钮会在两字间插空格（"不 会"）
const DONT_KNOW = /^不\s*会$/;

async function cookieHeader(page: Page) {
  return (await page.context().cookies()).map((c) => `${c.name}=${c.value}`).join("; ");
}

test.describe("作答「不会」", () => {
  test.setTimeout(180_000);

  test("学新词点「不会」亮出答案并进巩固；检测点「不会」明细显示「不会」", async ({ page, request }) => {
    const id = uid();
    await page.goto("/login");
    await page.getByText("注册", { exact: true }).click();
    await page.getByLabel("姓名").fill("不会同学");
    await page.getByLabel("邮箱").fill(`dk-${id}@vinx.test`);
    await page.getByLabel("密码").fill(PASSWORD);
    await page.getByRole("button", { name: /注册并开始学习/ }).click();
    await expect(page).toHaveURL(/\/today/);

    // 自建计划（默认题型：认义 + 拼写）
    await page.goto("/plans/new");
    await page.getByText("Unit 1", { exact: true }).first().click();
    await expect(page.getByText(/共\s*\d+\s*个去重单词/)).toBeVisible();
    await page.getByRole("button", { name: /创建计划/ }).click();
    // 创建前已在 /plans/new，要等跳到详情页，否则会取到 "new"
    await page.waitForURL(/\/plans\/(?!new$)[^/]+$/);
    const planId = page.url().split("/").pop()!;

    // 学新词
    await page.goto("/today");
    await page.getByRole("button", { name: /学新词/ }).first().click();
    await page.waitForURL(/\/study\//);
    const sessionId = page.url().split("/").pop();
    const items = (await (await request.get(`/api/study/sessions/${sessionId}`, { headers: { cookie: await cookieHeader(page) } })).json()).data.items as Item[];
    for (let i = 0; i < items.length; i++) await page.getByRole("button", { name: /下一个|开始练习/ }).click();

    const recTarget = items[0];
    const spellTarget = items[1];
    let recDone = false;
    let spellDone = false;
    let sawConsolidate = false;
    const main = page.locator("main");

    for (let n = 0; n < items.length * 8; n++) {
      const text = await main.innerText();
      if (/太棒了|完成一组|坚持就是进步/.test(text)) break;
      if (text.includes("巩固 ·")) sawConsolidate = true;
      const practice = !text.includes("巩固 ·");

      if (text.includes("拼写英文")) {
        const item = items.find((i) => text.includes(i.definition))!;
        if (practice && item === spellTarget && !spellDone) {
          spellDone = true;
          await page.getByRole("button", { name: DONT_KNOW }).click();
          await expect(main).toContainText("记一下正确答案");
          await expect(main).toContainText(item.answer || item.spelling);
          await page.getByRole("button", { name: /继续/ }).click();
        } else {
          await page.getByLabel("拼写答案").fill(item.answer);
          await page.keyboard.press("Enter");
        }
      } else if (text.includes("选出正确的中文意思")) {
        const item = items.find((i) => text.includes(i.spelling) && i.options.every((o) => text.includes(o)))!;
        if (practice && item === recTarget && !recDone) {
          recDone = true;
          await page.getByRole("button", { name: DONT_KNOW }).click();
          await expect(main).toContainText("记一下正确答案");
          await expect(page.getByLabel("正确答案")).toBeVisible();
          await page.getByRole("button", { name: /继续/ }).click();
        } else {
          await page.getByRole("button", { name: item.definition, exact: false }).first().click();
        }
      }
      await page.waitForTimeout(1000);
    }

    expect(recDone && spellDone).toBe(true);
    expect(sawConsolidate).toBe(true);
    await expect(main).toContainText(/太棒了|完成一组|坚持就是进步/);
    // 两个点过「不会」的词都算出错
    await expect(main).toContainText("本组出错的词");
    await expect(main).toContainText(recTarget.spelling);
    await expect(main).toContainText(spellTarget.spelling);

    // 检测：对刚学的词出 3 题认义
    const cookie = await cookieHeader(page);
    const plan = (await (await request.get(`/api/plans/${planId}`, { headers: { cookie } })).json()).data as { units: { id: string }[] };
    const testPlan = await request.post("/api/plans", {
      headers: { cookie },
      data: { name: `不会检测 ${id}`, kind: "test", modes: ["recognition"], testSize: 3, testScope: "learned", unitIds: plan.units.map((u) => u.id) },
    });
    expect(testPlan.ok()).toBe(true);
    const testSession = await request.post("/api/study/sessions", { headers: { cookie }, data: { kind: "test", planId: (await testPlan.json()).data.id } });
    const testId = (await testSession.json()).data.id as string;
    await page.goto(`/study/${testId}`);

    // 第一题点「不会」，其余随便选第一个选项
    await page.getByRole("button", { name: DONT_KNOW }).click();
    for (let n = 0; n < 6; n++) {
      const text = await main.innerText();
      if (text.includes("检测完成")) break;
      await main.locator("button.vx-tap").first().click();
      await page.waitForTimeout(800);
    }
    await expect(main).toContainText("检测完成");
    await expect(main).toContainText("答题明细");
    await expect(main).toContainText("：不会");
  });
});
