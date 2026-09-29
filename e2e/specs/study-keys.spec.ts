import { test, expect } from "@playwright/test";
import { PASSWORD, uid } from "../support/helpers";
import { cookieHeader, createPlanViaApi } from "../support/api";

test("学习页键盘操作（卡片 ←/→/Enter、数字键作答、答错 Enter 继续）与刷新续做", async ({ page, request }) => {
  test.setTimeout(90_000);
  const id = uid();
  await page.goto("/login");
  await page.getByText("注册", { exact: true }).click();
  await page.getByLabel("姓名").fill("续做");
  await page.getByLabel("邮箱").fill(`r-${id}@vinx.test`);
  await page.getByLabel("密码").fill(PASSWORD);
  await page.getByRole("button", { name: /注册并开始学习/ }).click();
  await expect(page).toHaveURL(/\/today/);
  await createPlanViaApi(request, await cookieHeader(page), { name: `续做 ${id}` });
  await page.goto("/today");
  await page.getByRole("button", { name: /学新词/ }).first().click();
  await page.waitForURL(/\/study\//);
  const sid = page.url().split("/").pop();
  const items = (await (await request.get(`/api/study/sessions/${sid}`, { headers: { cookie: await cookieHeader(page) } })).json()).data.items;
  // 卡片：→ 翻页，← 回退，Enter 前进
  await expect(page.locator("main")).toContainText(`认识新词 · 1 / ${items.length}`);
  await page.keyboard.press("ArrowRight");
  await expect(page.locator("main")).toContainText(`认识新词 · 2 / ${items.length}`);
  await page.keyboard.press("ArrowLeft");
  await expect(page.locator("main")).toContainText(`认识新词 · 1 / ${items.length}`);
  for (let i = 0; i < items.length; i++) await page.keyboard.press("Enter");
  await expect(page.locator("main")).toContainText("选出正确的中文意思");
  // 刷新：卡片不再出现（progress.cardsDone 已保存）
  await page.reload();
  await expect(page.locator("main")).toContainText("选出正确的中文意思");
  // 数字键作答（选正确的那项）
  for (let n = 0; n < 2; n++) {
    await page.waitForTimeout(400);
    const text = await page.locator("main").innerText();
    const item = items.find((i: any) => text.includes(i.spelling) && i.options.every((o: string) => text.includes(o)));
    await page.keyboard.press(String(item.options.indexOf(item.definition) + 1));
    await expect(page.locator("header")).toContainText(`练习 ${n + 1}/`, { timeout: 5000 });
  }
  // 数字键答错 → Enter 继续
  await page.waitForTimeout(400);
  const text = await page.locator("main").innerText();
  const item = items.find((i: any) => text.includes(i.spelling) && i.options.every((o: string) => text.includes(o)));
  const wrong = item.options.findIndex((o: string) => o !== item.definition);
  await page.keyboard.press(String(wrong + 1));
  await expect(page.locator("main")).toContainText("答错了，记一下正确答案");
  await page.waitForTimeout(300);
  await page.keyboard.press("Enter");
  await expect(page.locator("header")).toContainText("练习 3/");
  // 刷新后从第 4 题接着做
  await page.reload();
  await expect(page.locator("header")).toContainText("练习 3/");
  // 退出：已作答 → 「结束本组？」
  await page.getByRole("button", { name: "结束本组" }).click();
  await expect(page.getByRole("dialog")).toContainText("结束本组？");
  await page.getByRole("button", { name: /结束并保存/ }).click();
  await expect(page.locator("main")).toContainText(/坚持就是进步|完成一组|太棒了/);
  await expect(page.locator("main")).toContainText("没练完，已放回待学列表");
});
