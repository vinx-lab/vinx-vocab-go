import { test, expect } from "@playwright/test";
import { login } from "../support/helpers";

test.describe("角色与能力（前端可见性）", () => {
  test("管理员可见用户管理，可进入用户页", async ({ page }) => {
    await login(page, "admin@vinx.test");
    await expect(page).toHaveURL(/\/today/);
    await page.getByRole("menuitem", { name: "用户" }).click();
    await expect(page).toHaveURL(/\/admin\/users/);
    await expect(page.getByRole("columnheader", { name: "角色" })).toBeVisible();
  });

  test("教师可见班级、不可见用户管理以外的系统功能；直连用户页可用（可建学生号）", async ({ page }) => {
    await login(page, "teacher@vinx.test");
    await expect(page).toHaveURL(/\/today/);
    await expect(page.getByRole("menuitem", { name: "班级" })).toBeVisible();
    await page.goto("/admin/users");
    await expect(page.getByRole("button", { name: /新建账号/ })).toBeVisible();
  });

  test("学生看不到班级与用户菜单，直连被拦回今日", async ({ page }) => {
    await login(page, "student@vinx.test");
    await expect(page).toHaveURL(/\/today/);
    await expect(page.getByRole("menuitem", { name: "班级" })).toHaveCount(0);
    await expect(page.getByRole("menuitem", { name: "用户" })).toHaveCount(0);
    await page.goto("/classes");
    await expect(page).toHaveURL(/\/today/);
    await page.goto("/admin/users");
    await expect(page).toHaveURL(/\/today/);
  });
});
