import { test, expect } from "@playwright/test";
import { login } from "../support/helpers";

/**
 * 系统设置 → AI（spec 0003）：管理员在页面保存配置后立即生效。
 * 不依赖真实 AI 后端：OpenAI 兼容接口填一个假地址，只看入口是否出现；结束后恢复为环境变量。
 */
test.describe("系统设置 · AI 接口", () => {
  test.afterEach(async ({ page }) => {
    await page.request.delete("/api/settings/ai");
  });

  test("管理员保存 AI 配置后，词书页出现「AI 补例句」入口；关闭后入口消失", async ({ page }) => {
    await login(page, "admin@vinx.test");
    await expect(page).toHaveURL(/\/today/);
    await page.getByRole("menuitem", { name: "系统设置" }).click();
    await expect(page).toHaveURL(/\/admin\/settings/);

    // 先关闭，确认入口隐藏（环境变量可能已配置 AI）
    await page.locator(".ant-radio-button-wrapper", { hasText: "关闭" }).click();
    await page.locator("form").getByRole("button", { name: /保\s*存/ }).click();
    await expect(page.getByText("当前生效：页面保存的配置")).toBeVisible();

    const books = (await (await page.request.get("/api/books")).json()).data;
    const bookId = books.items.find((b: { name: string }) => b.name === "七年级下册").id;
    await page.goto(`/books/${bookId}`);
    await expect(page.getByRole("button", { name: /添加单词/ })).toBeVisible();
    await expect(page.getByRole("button", { name: /AI 补例句/ })).toHaveCount(0);

    // 填写 OpenAI 兼容配置并保存
    await page.goto("/admin/settings");
    await page.locator(".ant-radio-button-wrapper", { hasText: "OpenAI 兼容" }).click();
    await page.getByLabel("接口地址").fill("http://127.0.0.1:9/v1");
    await page.getByLabel("API Key").fill("sk-e2e-dummy-key-0000");
    await page.getByLabel("模型").fill("e2e-model");
    await page.locator("form").getByRole("button", { name: /保\s*存/ }).click();
    await expect(page.getByText("已保存，立即生效")).toBeVisible();
    // Key 不回显，只显示末 4 位
    await expect(page.getByLabel("API Key")).toHaveValue("");
    await expect(page.getByLabel("API Key")).toHaveAttribute("placeholder", /末 4 位 0000/);

    await page.goto(`/books/${bookId}`);
    await expect(page.getByRole("button", { name: /AI 补例句/ })).toBeVisible();

    // 恢复为环境变量
    await page.goto("/admin/settings");
    await page.getByRole("button", { name: /恢复为环境变量/ }).click();
    await page.getByRole("button", { name: /^恢\s*复$/ }).click();
    await expect(page.getByText("当前生效：环境变量")).toBeVisible();
  });

  test("教师看不到系统设置，直连被拦回", async ({ page }) => {
    await login(page, "teacher@vinx.test");
    await expect(page).toHaveURL(/\/today/);
    await expect(page.getByRole("menuitem", { name: "系统设置" })).toHaveCount(0);
    await page.goto("/admin/settings");
    await expect(page).toHaveURL(/\/today/);
  });
});
