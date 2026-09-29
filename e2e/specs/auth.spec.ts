import { test, expect } from "@playwright/test";
import { login, logout, PASSWORD } from "../support/helpers";

test.describe("认证链路", () => {
  test("未登录访问 /today 重定向到 /login", async ({ page }) => {
    await page.goto("/today");
    await expect(page).toHaveURL(/\/login/);
  });

  test("错误密码登录显示后端错误提示并停留登录页", async ({ page }) => {
    await login(page, "student@vinx.test", "wrong-password");
    await expect(page.getByText("账号或密码错误")).toBeVisible();
    await expect(page).toHaveURL(/\/login/);
  });

  test("学生登录进入今日，导航无教学/管理菜单", async ({ page }) => {
    await login(page, "student@vinx.test", PASSWORD);
    await expect(page).toHaveURL(/\/today/);
    await expect(page.getByRole("menuitem", { name: "今日" })).toBeVisible();
    await expect(page.getByRole("menuitem", { name: "班级" })).toHaveCount(0);
    await expect(page.getByRole("menuitem", { name: "角色" })).toHaveCount(0);
  });

  test("刷新页面仍保持登录态（HttpOnly cookie 生效）", async ({ page }) => {
    await login(page, "student@vinx.test");
    await expect(page).toHaveURL(/\/today/);
    await page.reload();
    await expect(page).toHaveURL(/\/today/);
  });

  test("登出后回 /login，再访问被拦", async ({ page }) => {
    await login(page, "student@vinx.test");
    await expect(page).toHaveURL(/\/today/);
    await logout(page);
    await page.goto("/today");
    await expect(page).toHaveURL(/\/login/);
  });
});
