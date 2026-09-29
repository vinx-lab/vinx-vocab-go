import { expect, type Page } from "@playwright/test";

export const PASSWORD = "dev123456";

/** 通过登录页登录（账号可为邮箱或学号） */
export async function login(page: Page, account: string, password = PASSWORD) {
  await page.goto("/login");
  await page.getByLabel("账号").fill(account);
  await page.getByLabel("密码").fill(password);
  // antd 双字按钮会在两字间插空格（"登 录"）
  await page.getByRole("button", { name: /登\s*录/ }).click();
}

export async function logout(page: Page) {
  await page.getByRole("banner").getByRole("button").last().click();
  await page.getByRole("menuitem", { name: "退出登录" }).click();
  await expect(page).toHaveURL(/\/login/);
}

export const uid = () => `${Date.now()}${Math.floor(Math.random() * 1000)}`;
