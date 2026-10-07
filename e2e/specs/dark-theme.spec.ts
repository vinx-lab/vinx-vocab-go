import { test, expect, type APIRequestContext, type Page } from "@playwright/test";
import { login, PASSWORD, uid } from "../support/helpers";

/** 深色主题：跟账号保存、刷新不闪回、清缓存重新登录仍按账号显示；单词单打印页始终白底黑字 */
test.use({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true, colorScheme: "light" });

const DARK_PAPER = "rgb(20, 26, 38)"; // --paper 深色值 #141a26
const LIGHT_PAPER = "rgb(247, 244, 236)"; // --paper 浅色值 #f7f4ec

const htmlTheme = (page: Page) => page.locator("html");
const bodyBg = (page: Page) => page.evaluate(() => getComputedStyle(document.body).backgroundColor);

/** 走 API 学完一组新词，让账号有已学词（单词单只从已学词里挑） */
async function learnOneGroup(request: APIRequestContext) {
  const books = (await (await request.get("/api/books")).json()).data;
  const book = books.items.find((b: { name: string }) => b.name === "八年级上册");
  const detail = (await (await request.get(`/api/books/${book.id}`)).json()).data;
  const plan = (await (await request.post("/api/plans", { data: { name: "E2E 外观", unitIds: [detail.units[0].id], newPerDay: 10 } })).json()).data;
  const sid = (await (await request.post("/api/study/sessions", { data: { kind: "learn", planId: plan.id } })).json()).data.id;
  const s = (await (await request.get(`/api/study/sessions/${sid}`)).json()).data;
  for (const item of s.items) {
    for (const mode of s.modes) {
      const answer = mode === "recognition" ? "__wrong__" : "zzz";
      await request.post(`/api/study/sessions/${sid}/answers`, { data: { wordId: item.wordId, mode, phase: "practice", attempt: 1, answer } });
    }
  }
  await request.post(`/api/study/sessions/${sid}/complete`);
}

test.describe("深色主题", () => {
  test.setTimeout(120_000);

  test("切到深色 → 刷新仍深色 → 清缓存重新登录仍深色 → 打印页白底黑字", async ({ page, context }) => {
    const email = `dark-${uid()}@vinx.test`;
    await page.goto("/login");
    await page.getByText("注册", { exact: true }).click();
    await page.getByLabel("姓名").fill("夜读同学");
    await page.getByLabel("邮箱").fill(email);
    await page.getByLabel("密码").fill(PASSWORD);
    await page.getByRole("button", { name: /注册并开始学习/ }).click();
    await expect(page).toHaveURL(/\/today/);

    // 默认跟随系统（这里系统为浅色）
    await expect(htmlTheme(page)).toHaveAttribute("data-theme", "light");

    // 右上角用户菜单切到深色
    await page.getByRole("banner").getByRole("button").last().click();
    await page.getByRole("menuitem", { name: /深色/ }).click();
    await expect(htmlTheme(page)).toHaveAttribute("data-theme", "dark");
    await expect.poll(() => bodyBg(page)).toBe(DARK_PAPER);
    await expect.poll(() => page.evaluate(() => localStorage.getItem("vinx_theme"))).toBe("dark");
    // 等账号保存完成
    await expect.poll(async () => (await (await page.request.get("/api/auth/me")).json()).data.theme).toBe("dark");

    // 刷新：内联脚本在渲染前就设好深色
    await page.reload();
    await expect(htmlTheme(page)).toHaveAttribute("data-theme", "dark");
    await expect.poll(() => bodyBg(page)).toBe(DARK_PAPER);

    // 清空缓存与登录态：登录页按系统显示浅色；重新登录后按账号显示深色
    await page.evaluate(() => localStorage.clear());
    await context.clearCookies();
    await page.goto("/login");
    await expect(htmlTheme(page)).toHaveAttribute("data-theme", "light");
    await login(page, email);
    await expect(page).toHaveURL(/\/today/);
    await expect(htmlTheme(page)).toHaveAttribute("data-theme", "dark");
    await expect.poll(() => bodyBg(page)).toBe(DARK_PAPER);

    // 个人中心的「外观」卡片与菜单是同一个设置
    await page.goto("/profile");
    await expect(page.getByRole("radio", { name: "深色" })).toBeChecked();

    // 打印页：深色下仍是白底黑字
    await learnOneGroup(page.request);
    await page.addInitScript(() => { window.print = () => {}; });
    await page.goto("/sheets/new");
    await expect(page.getByText("刚记住").first()).toBeVisible();
    await page.getByRole("button", { name: /生成并打印/ }).click();
    await page.waitForURL(/\/sheets\/[^/]+\/print/);
    const sheet = page.locator(".vx-sheet-page").first();
    await expect(sheet).toBeVisible();
    const colors = await sheet.evaluate((el) => {
      const s = getComputedStyle(el);
      const en = el.querySelector(".vx-sheet-en");
      return { bg: s.backgroundColor, color: s.color, en: en ? getComputedStyle(en).color : "" };
    });
    expect(colors).toEqual({ bg: "rgb(255, 255, 255)", color: "rgb(0, 0, 0)", en: "rgb(0, 0, 0)" });
    expect(await bodyBg(page)).not.toBe(DARK_PAPER);

    // 切回浅色，避免影响同设备后续用例
    await page.goto("/profile");
    // antd 按钮样式单选的 input 是隐藏的，点外层 label
    await page.locator(".ant-radio-button-wrapper", { hasText: "浅色" }).click();
    await expect(htmlTheme(page)).toHaveAttribute("data-theme", "light");
    await expect.poll(() => bodyBg(page)).toBe(LIGHT_PAPER);
  });
});
