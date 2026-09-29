import { test, expect } from "@playwright/test";
import { login, PASSWORD, uid } from "../support/helpers";

/** 手机上完成一组学习（学生最常见的使用方式）；用 chromium 模拟手机视口与触屏 */
test.use({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true, deviceScaleFactor: 3 });

test.describe("移动端学习流", () => {
  test.setTimeout(120_000);

  test("手机注册 → 自建计划 → 学一组 → 结果页可继续", async ({ page, request }) => {
    const id = uid();
    const email = `m-${id}@vinx.test`;

    // 注册（手机上从登录页切到注册）
    await page.goto("/login");
    await page.getByText("注册", { exact: true }).click();
    await page.getByLabel("姓名").fill("手机同学");
    await page.getByLabel("邮箱").fill(email);
    await page.getByLabel("密码").fill(PASSWORD);
    await page.getByRole("button", { name: /注册并开始学习/ }).click();
    await expect(page).toHaveURL(/\/today/);

    // 底部标签栏是移动端主导航
    const tabbar = page.getByRole("navigation", { name: "主导航" });
    await expect(tabbar).toBeVisible();
    await expect(tabbar.getByText("今日")).toBeVisible();

    // 自建计划（对象默认本人）
    await page.goto("/plans/new");
    await page.getByText("Unit 1", { exact: true }).first().click();
    await expect(page.getByText(/共\s*\d+\s*个去重单词/)).toBeVisible();
    await page.getByRole("button", { name: /创建计划/ }).click();
    await page.waitForURL(/\/plans\/[^/]+$/);

    // 今日 → 学新词
    await page.goto("/today");
    await page.getByRole("button", { name: /学新词/ }).first().click();
    await page.waitForURL(/\/study\//);
    const sessionId = page.url().split("/").pop();
    const items = (await (await request.get(`/api/study/sessions/${sessionId}`, { headers: { cookie: (await page.context().cookies()).map((c) => `${c.name}=${c.value}`).join("; ") } })).json()).data.items as {
      spelling: string;
      definition: string;
      answer: string;
      options: string[];
    }[];

    // 认识卡片
    for (let i = 0; i < items.length; i++) await page.getByRole("button", { name: /下一个|开始练习/ }).click();

    // 练习：认义 + 拼写
    const main = page.locator("main");
    for (let n = 0; n < items.length * 4; n++) {
      const text = await main.innerText();
      if (/太棒了|完成一组|坚持就是进步/.test(text)) break;
      if (text.includes("拼写英文")) {
        const item = items.find((i) => text.includes(i.definition))!;
        await page.getByLabel("拼写答案").fill(item.answer);
        await page.keyboard.press("Enter");
      } else if (text.includes("选出正确的中文意思")) {
        const item = items.find((i) => text.includes(i.spelling) && i.options.every((o) => text.includes(o)))!;
        await page.getByRole("button", { name: item.definition, exact: false }).first().click();
      }
      await page.waitForTimeout(1000);
    }

    await expect(main).toContainText(/太棒了|完成一组|坚持就是进步/);
    await expect(page.getByRole("button", { name: /返回今日/ })).toBeVisible();
  });
});
