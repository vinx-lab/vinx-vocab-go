import { test, expect } from "@playwright/test";
import { login } from "../support/helpers";

/**
 * 系统设置 → AI 提示词（spec 0006 / K41）：管理员只编辑默认要求模板（不含输出格式，不做格式检查）。
 * 修改「例句」模板、保存、刷新后仍在；恢复默认。不调用 AI；结束后恢复默认。
 */
test.describe("系统设置 · AI 提示词模板", () => {
  test.afterEach(async ({ page }) => {
    await page.request.delete("/api/settings/ai/prompts/example");
  });

  test("管理员修改例句模板并保存，刷新后仍在；恢复默认后回到默认内容", async ({ page }) => {
    await login(page, "admin@vinx.test");
    await expect(page).toHaveURL(/\/today/);
    await page.goto("/admin/settings");

    await expect(page.getByText("输出格式由系统自动加上").first()).toBeVisible();
    const box = page.getByLabel("例句要求模板");
    const status = page.getByTestId("prompt-status-example");
    await expect(status).toHaveText("默认");
    const original = await box.inputValue();
    expect(original).toContain("例句编辑");
    expect(original).not.toMatch(/json/i);

    // 不含 JSON 也能保存
    const custom = `E2E：例句不超过 8 个词。\n${original}`;
    await box.fill(custom);
    await page.getByRole("tabpanel", { name: "例句" }).getByRole("button", { name: /保\s*存/ }).click();
    await expect(page.getByText("已保存，下一次生成立即使用")).toBeVisible();
    await expect(status).toHaveText("已修改");

    await page.reload();
    await expect(page.getByTestId("prompt-status-example")).toHaveText("已修改");
    await expect(page.getByLabel("例句要求模板")).toHaveValue(custom);

    // 展开可对照默认内容
    await page.getByText("查看默认内容").first().click();
    await expect(page.getByTestId("prompt-default-example")).toContainText("你是初中英语教材的例句编辑");

    await page.getByRole("button", { name: /恢复默认/ }).click();
    await page.getByRole("button", { name: /^恢\s*复$/ }).click();
    await expect(page.getByText("已恢复默认")).toBeVisible();
    await expect(page.getByTestId("prompt-status-example")).toHaveText("默认");
    await expect(page.getByLabel("例句要求模板")).toHaveValue(original);
  });

  test("清空的模板不能保存", async ({ page }) => {
    await login(page, "admin@vinx.test");
    await expect(page).toHaveURL(/\/today/);
    await page.goto("/admin/settings");

    const box = page.getByLabel("例句要求模板");
    const original = await box.inputValue();
    await box.fill("   ");
    await expect(page.getByRole("tabpanel", { name: "例句" }).getByRole("button", { name: /保\s*存/ })).toBeDisabled();
    await page.reload();
    await expect(page.getByLabel("例句要求模板")).toHaveValue(original);
    await expect(page.getByTestId("prompt-status-example")).toHaveText("默认");
  });

  test("手机宽度下模板卡片可用", async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await login(page, "admin@vinx.test");
    await expect(page).toHaveURL(/\/today/);
    await page.goto("/admin/settings");
    await page.getByRole("tab", { name: "短文" }).click();
    await expect(page.getByLabel("短文要求模板")).toBeVisible();
    await expect(page.getByTestId("prompt-status-passage")).toHaveText("默认");
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect(overflow).toBeLessThanOrEqual(0);
  });
});
