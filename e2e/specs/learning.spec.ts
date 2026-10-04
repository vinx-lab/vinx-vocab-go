import { test, expect, type Page } from "@playwright/test";
import { login, PASSWORD, uid } from "../support/helpers";

interface Item {
  wordId: string;
  spelling: string;
  answer: string;
  definition: string;
  options: string[];
}

/** 按快照在页面上完成一组：卡片翻完，选择题点正确释义，拼写题输入正确拼写 */
async function finishGroup(page: Page) {
  await page.waitForURL(/\/study\//);
  const sessionId = page.url().split("/").pop();
  const res = await page.request.get(`/api/study/sessions/${sessionId}`);
  const items = (await res.json()).data.items as Item[];

  const nextCard = page.getByRole("button", { name: /下一个|开始练习/ });
  if (await nextCard.isVisible().catch(() => false)) {
    for (let i = 0; i < items.length; i++) await nextCard.click();
  }

  const main = page.locator("main");
  for (let guard = 0; guard < items.length * 3; guard++) {
    await expect(main).toContainText(/选出正确的中文意思|拼写英文|完成一组|太棒了|坚持就是进步/);
    const text = await main.innerText();
    if (/完成一组|太棒了|坚持就是进步/.test(text)) return;
    if (text.includes("拼写英文")) {
      const item = items.find((i) => text.includes(i.definition))!;
      await page.getByLabel("拼写答案").fill(item.answer);
      await page.keyboard.press("Enter");
      await expect(page.getByLabel("拼写答案")).toBeDisabled();
    } else {
      const item = items.find((i) => text.includes(i.spelling) && i.options.every((o) => text.includes(o)))!;
      await page.getByRole("button", { name: item.definition, exact: false }).first().click();
    }
    await page.waitForTimeout(1100);
  }
  await expect(main).toContainText(/完成一组|太棒了|坚持就是进步/);
}

test.describe("学习主线：老师建班布置 → 学生入班学习 → 老师看到进度", () => {
  test.setTimeout(120_000);

  test("完整链路", async ({ browser }) => {
    const id = uid();
    const className = `E2E 班 ${id}`;
    const planName = `E2E 计划 ${id}`;

    // 老师：建班
    const tCtx = await browser.newContext();
    const teacher = await tCtx.newPage();
    await login(teacher, "teacher@vinx.test");
    await expect(teacher).toHaveURL(/\/today/);
    await teacher.goto("/classes");
    await teacher.getByRole("button", { name: /新建班级/ }).first().click();
    await teacher.getByLabel("班级名称").fill(className);
    // spec 0008：建班时必须选一次是否允许学生自主安排
    await teacher.getByRole("radio", { name: /^允许/ }).check();
    await teacher.getByRole("dialog").getByRole("button", { name: /确\s*定|创\s*建/ }).click();
    await teacher.waitForURL(/\/classes\/[^/]+$/);
    const classId = teacher.url().split("/").pop()!.split("?")[0];
    const cls = (await (await teacher.request.get(`/api/classes/${classId}`)).json()).data;
    expect(cls.inviteCode).toMatch(/^[A-Z0-9]{6}$/);

    // 学生：注册时填邀请码
    const sCtx = await browser.newContext({ viewport: { width: 390, height: 844 } });
    const student = await sCtx.newPage();
    await student.goto("/login");
    await student.getByText("注册", { exact: true }).click();
    await student.getByLabel("姓名").fill(`学生${id.slice(-4)}`);
    await student.getByLabel("邮箱").fill(`e2e-${id}@vinx.test`);
    await student.getByLabel("密码").fill(PASSWORD);
    await student.getByLabel("班级邀请码（选填）").fill(cls.inviteCode.toLowerCase());
    await student.getByRole("button", { name: /注册并开始学习/ }).click();
    await expect(student).toHaveURL(/\/today/);
    await expect(student.getByText("今天没有学习任务")).toBeVisible();

    // 老师：为班级建计划（从班级页进入，对象已预选）
    await teacher.goto(`/plans/new?classId=${classId}`);
    await teacher.getByText("Unit 1", { exact: true }).first().click();
    await expect(teacher.getByText(/共\s*\d+\s*个去重单词/)).toBeVisible();
    await teacher.getByPlaceholder("选择单元后自动生成").fill(planName);
    await teacher.getByRole("button", { name: /创建计划/ }).click();
    await teacher.waitForURL(/\/plans\/[^/]+$/);
    await expect(teacher.getByText(planName).first()).toBeVisible();

    // 学生：今日出现计划 → 学一组新词
    await student.reload();
    await expect(student.getByText(planName)).toBeVisible();
    await student.getByRole("button", { name: /学新词/ }).first().click();
    await finishGroup(student);
    await expect(student.getByText("新学入库")).toBeVisible();
    await student.getByRole("button", { name: /返回今日/ }).click();
    await expect(student.getByText(/连续学习/)).toBeVisible();

    // 学生：记录页能看到这一组
    await student.goto("/records");
    await expect(student.getByText(planName).first()).toBeVisible();

    // 老师：班级概览显示该生今日已完成
    await teacher.goto(`/classes/${classId}`);
    const row = teacher.getByRole("row", { name: new RegExp(`学生${id.slice(-4)}`) });
    await expect(row).toContainText("已完成");

    // 清理演示老师名下的测试数据（学习记录随计划删除保留，班级删除后成员关系一并清除）
    const planId = (await (await teacher.request.get(`/api/plans?scope=created`)).json()).data.items.find((p: { name: string }) => p.name === planName)?.id;
    if (planId) await teacher.request.delete(`/api/plans/${planId}`);
    await teacher.request.delete(`/api/classes/${classId}`);

    await tCtx.close();
    await sCtx.close();
  });
});
