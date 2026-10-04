import { test, expect } from "@playwright/test";
import { login } from "../support/helpers";

/**
 * 长页面能用滚轮滚动：曾经有一条从 antd 抽取来的 `html body{overflow-y:hidden}`（弹窗滚动锁）被写死在 antd.css 里，
 * 整个应用的长页面都滚不动。Playwright 的程序滚动和整页截图不受它影响，所以这里用真实的滚轮事件。
 */
for (const [name, viewport] of [
  ["电脑", { width: 1280, height: 720 }],
  ["手机", { width: 390, height: 600 }],
] as const) {
  test(`${name}：词书详情页可以用滚轮往下滚`, async ({ browser }) => {
    const ctx = await browser.newContext({ viewport });
    const page = await ctx.newPage();
    await login(page, "student@vinx.test");
    await expect(page).toHaveURL(/\/today/);

    const books = await (await page.request.get("/api/books")).json();
    const book = (books.data.items as { id: string; name: string }[]).find((b) => b.name === "七年级上册")!;
    await page.goto(`/books/${book.id}`);
    await expect(page.getByText("Unit 1").first()).toBeVisible();
    const scrollHeight = await page.evaluate(() => document.documentElement.scrollHeight);
    expect(scrollHeight).toBeGreaterThan(viewport.height + 200);

    await page.mouse.move(viewport.width / 2, viewport.height / 2);
    await page.mouse.wheel(0, 800);
    await expect.poll(() => page.evaluate(() => window.scrollY)).toBeGreaterThan(0);
    await ctx.close();
  });
}
