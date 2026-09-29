import { test, expect } from "@playwright/test";
import { uid } from "../support/helpers";

/**
 * 首次运行向导 + 版本切换（Go 版新增，spec 0001 §2「版本」；oracle 没有这些接口/页面）。
 * 只能跑在指向 Go 后端、且数据目录全新（未 seed 过任何账号）的实例上：
 *   `make e2e` 里的 fresh 实例（E2E_FRESH_PORT）。
 * 用例顺序有依赖（同一个全新实例）：先走向导选版本、注册第一个账号（自动成为管理员），
 * 再用这个账号验证系统设置里的「版本」卡片可以双向切换、导航与路由守卫随之更新。
 */
const PASSWORD = "dev123456";

test.describe("首次运行向导 + 版本切换（Go）", () => {
  test("全新安装走向导注册出管理员；系统设置「版本」卡片可双向切换，导航与守卫立即更新", async ({ page }) => {
    const cfg = await (await page.request.get("/api/config")).json();
    expect(cfg.data.needsSetup, "被测实例须是全新安装（未 seed 过账号）").toBe(true);

    // 未登录访问任意路径都先到向导，而不是登录页
    await page.goto("/today");
    await expect(page.getByRole("radiogroup", { name: "版本" })).toBeVisible();
    await expect(page.getByText("开始之前，先选择怎么用")).toBeVisible();

    // 确认按钮在没选之前不可点
    // antd 双字按钮会在两字间插空格（"确 认"）
    const confirm = page.getByRole("button", { name: /确\s*认/ });
    await expect(confirm).toBeDisabled();
    await expect(page.getByText("第一个注册的账号将自动成为管理员")).toBeVisible();

    await page.getByTestId("setup-edition-school").click();
    await expect(page.getByTestId("setup-edition-school")).toHaveAttribute("aria-checked", "true");
    await expect(confirm).toBeEnabled();
    await confirm.click();

    // 进入登录页且已经是「注册」标签（不需要再点一次切换）
    await expect(page).toHaveURL(/\/login/);
    await expect(page.getByLabel("姓名")).toBeVisible();
    await expect(page.getByLabel("邮箱")).toBeVisible();

    const email = `setup-admin-${uid()}@vinx.test`;
    await page.getByLabel("姓名").fill("首任管理员");
    await page.getByLabel("邮箱").fill(email);
    await page.getByLabel("密码", { exact: true }).fill(PASSWORD);
    await page.getByRole("button", { name: /注册并开始学习/ }).click();

    await expect(page).toHaveURL(/\/today/);

    // 向导已完成：再次访问 / 不会回到向导
    await page.goto("/today");
    await expect(page.getByRole("radiogroup", { name: "版本" })).toHaveCount(0);

    // 第一个账号自动成为管理员：能看到用户管理与系统设置入口
    await expect(page.getByRole("menuitem", { name: "用户" })).toBeVisible();
    await expect(page.getByRole("menuitem", { name: "系统设置" })).toBeVisible();

    const after = await (await page.request.get("/api/config")).json();
    expect(after.data).toMatchObject({ needsSetup: false, editionLocked: false, edition: "school" });

    // ---- 系统设置「版本」卡片：管理员可双向切换；未锁定时不是只读；切换后导航与守卫立即更新 ----
    await page.goto("/admin/settings");
    await expect(page.getByText("当前：班级版")).toBeVisible();
    await expect(page.getByText("由启动配置指定")).toHaveCount(0);

    const toPersonal = page.getByRole("button", { name: "切换为个人版" });
    await expect(toPersonal).toBeVisible();
    await toPersonal.click();

    const dialog = page.getByRole("dialog");
    await expect(dialog.getByText("切换为个人版？")).toBeVisible();
    await expect(dialog).toContainText("隐藏班级");
    await expect(dialog).toContainText("已有数据不会删除");
    await dialog.getByRole("button", { name: /确定切换/ }).click();

    await expect(page.getByText("已切换为个人版")).toBeVisible();
    await expect(page.getByText("当前：个人版")).toBeVisible();

    // 守卫与导航立即更新：不刷新页面，班级 / 用户菜单消失，直连 /classes 被拦回
    await expect(page.getByRole("menuitem", { name: "班级" })).toHaveCount(0);
    await expect(page.getByRole("menuitem", { name: "用户" })).toHaveCount(0);
    await page.goto("/classes");
    await expect(page).toHaveURL(/\/(today|profile)/);

    // 切回班级版：原样恢复
    await page.goto("/admin/settings");
    const toSchool = page.getByRole("button", { name: "切换为班级版" });
    await toSchool.click();
    await page.getByRole("dialog").getByRole("button", { name: /确定切换/ }).click();
    await expect(page.getByText("已切换为班级版")).toBeVisible();
    await expect(page.getByText("当前：班级版")).toBeVisible();
    await expect(page.getByRole("menuitem", { name: "用户" })).toBeVisible();
  });
});
