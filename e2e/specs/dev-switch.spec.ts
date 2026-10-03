import { test, expect, type Page } from "@playwright/test";
import { login } from "../support/helpers";

/**
 * 开发模式免密切换（spec 0002）：实例以 --dev 启动（run.sh 的 E2E_DEV_PORT，Playwright project「dev」）。
 * 同一个浏览器上下文（共用 Cookie）开两个标签页：A 切成老师、B 切成学生，刷新后各自不变；
 * B「恢复为浏览器账号」回到 Cookie 上的账号；A 选「整个浏览器」切成管理员后，新开的标签页也是管理员。
 */

async function switchTo(page: Page, name: string, scope: "仅本标签页" | "整个浏览器") {
  await page.getByTestId("dev-switcher").click();
  const panel = page.getByRole("dialog", { name: "开发模式：切换账号" });
  await expect(panel).toBeVisible();
  await panel.locator(".ant-segmented-item", { hasText: scope }).click();
  await panel.locator(".vx-dev-row", { hasText: name }).first().click();
  await expect(panel).toBeHidden();
}

async function expectIdentity(page: Page, name: string) {
  await expect(page.getByRole("banner")).toContainText(name);
}

test.describe("开发模式：切换账号", () => {
  test("两个标签页各自登录不同账号；恢复为浏览器账号；整个浏览器切换影响新标签页", async ({ context, page }) => {
    // 浏览器（Cookie）上的账号：学生小红
    await login(page, "student2@vinx.test");
    await expect(page).toHaveURL(/\/today/);
    await expectIdentity(page, "小红");
    await expect(page.getByTestId("dev-switcher")).toHaveText("DEV");

    // 标签页 A：仅本标签页切成老师
    const a = page;
    await switchTo(a, "王老师", "仅本标签页");
    await expectIdentity(a, "王老师");
    await expect(a.getByTestId("dev-switcher")).toContainText("王老师");

    // 标签页 B：仍是 Cookie 上的小红；切成学生小明
    const b = await context.newPage();
    await b.goto("/today");
    await expectIdentity(b, "小红");
    await switchTo(b, "小明", "仅本标签页");
    await expectIdentity(b, "小明");

    // 两边刷新后身份不变
    await a.reload();
    await b.reload();
    await expectIdentity(a, "王老师");
    await expectIdentity(b, "小明");

    // B 恢复为浏览器账号：回到 Cookie 上的小红
    await b.getByTestId("dev-switcher").click();
    await b.getByRole("button", { name: "恢复为浏览器账号" }).click();
    await expectIdentity(b, "小红");
    await expect(b.getByTestId("dev-switcher")).toHaveText("DEV");
    // A 不受影响
    await a.reload();
    await expectIdentity(a, "王老师");

    // A 选「整个浏览器」切成管理员：写 Cookie，新开的标签页也是管理员
    await switchTo(a, "管理员", "整个浏览器");
    await expectIdentity(a, "管理员");
    await expect(a.getByTestId("dev-switcher")).toHaveText("DEV");
    const c = await context.newPage();
    await c.goto("/");
    await expectIdentity(c, "管理员");
    // B 已经回到浏览器账号，刷新后同样变成管理员
    await b.reload();
    await expectIdentity(b, "管理员");
  });
});
