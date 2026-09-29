import { test, expect } from "@playwright/test";
import { PASSWORD, uid } from "../support/helpers";
import { cookieHeader, createPlanViaApi } from "../support/api";

/**
 * 退出确认框与 antd 5 Modal.confirm 一致：打开后焦点在「确定」类按钮（这里是「退出」）上。
 * 注意：旧版在卡片阶段按回车并不会确认——卡片页的全局 Enter 快捷键先处理（翻到下一张并 preventDefault），
 * 对话框保持打开；新版照旧，这里只断言焦点，再点按钮退出。旧前端上同样通过。
 */
test("学习页：点 ✕ 后焦点在「退出」按钮上，点它回到今日", async ({ page, request }) => {
  const id = uid();
  await page.goto("/login");
  await page.getByText("注册", { exact: true }).click();
  await page.getByLabel("姓名").fill("退出");
  await page.getByLabel("邮箱").fill(`x-${id}@vinx.test`);
  await page.getByLabel("密码").fill(PASSWORD);
  await page.getByRole("button", { name: /注册并开始学习/ }).click();
  await expect(page).toHaveURL(/\/today/);
  await createPlanViaApi(request, await cookieHeader(page), { name: `退出 ${id}` });
  await page.goto("/today");
  await page.getByRole("button", { name: /学新词/ }).first().click();
  await page.waitForURL(/\/study\//);
  await expect(page.locator("main")).toContainText("认识新词 · 1 /");
  await page.getByRole("button", { name: "结束本组" }).click();
  await expect(page.getByRole("dialog")).toContainText("退出学习？");
  await expect(page.getByRole("button", { name: /退\s*出/ })).toBeFocused();
  await page.getByRole("button", { name: /退\s*出/ }).click();
  await expect(page).toHaveURL(/\/today/);
});
