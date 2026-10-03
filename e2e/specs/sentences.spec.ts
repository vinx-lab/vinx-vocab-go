import { test, expect } from "@playwright/test";
import { login } from "../support/helpers";
import { apiOk, letters, newClassWithStudent, newTeacher, wordIds } from "../support/school";

/**
 * 句与篇（spec 0004）：老师在导入页导入一个带 [句型] 和 [课文] 的单元 → 班里的学生在单元页的「句型」「课文」标签看到
 * → 学生在单词详情的「出现在这些句子里」看到这些句子。新老师、班级、学生由 API 准备，导入与浏览走页面。
 */
test.describe("句型与课文", () => {
  test.setTimeout(90_000);

  test("老师导入带句型和课文的单元 → 学生在单元页看到 → 单词详情里出现这些句子", async ({ browser }) => {
    const teacher = await newTeacher("句子老师");
    const { student } = await newClassWithStudent(teacher, "句子班", "句子同学");
    const tag = letters();
    const w1 = `e2esentalpha${tag}`;
    const w2 = `e2esentbravo${tag}`;
    const bookName = `E2E 句子词书 ${tag}`;
    const text = [
      "Unit 1",
      `${w1} n. 甲物`,
      `${w2} v. 乙动`,
      "[句型]",
      `I ${w2} the ${w1}. | 我乙这个甲。`,
      `I find ... ${w1}. | 我发现……甲。 | I find the red ${w1}.`,
      "[课文]",
      `Title: My ${w1} | 我的甲`,
      `This is my ${w1}. | 这是我的甲。`,
      "",
      `We ${w2} it every day. | 我们每天乙它。`,
    ].join("\n");

    // 老师：导入页粘贴 → 解析预览（句型 / 课文标签）→ 新建词书 → 确认导入
    const tctx = await browser.newContext();
    const tp = await tctx.newPage();
    await login(tp, teacher.email);
    await expect(tp).toHaveURL(/\/today/);
    await tp.goto("/books/import");
    await tp.locator("textarea").first().fill(text);
    await tp.getByRole("button", { name: "解析预览" }).click();
    const tabs = tp.locator(".vx-import-tabs");
    await expect(tabs).toContainText("词表 2");
    await expect(tabs).toContainText("句型 2");
    await expect(tabs).toContainText("课文 2");
    await tabs.locator(".ant-segmented-item", { hasText: "句型" }).click();
    await expect(tp.getByPlaceholder("英文").first()).toHaveValue(`I ${w2} the ${w1}.`);
    await expect(tp.getByPlaceholder("中文", { exact: true }).first()).toHaveValue("我乙这个甲。");
    await tp.getByRole("button", { name: /下一步：选择目标/ }).click();
    await expect(tp.getByText("4 个句子（句型与课文）")).toBeVisible();
    await tp.getByRole("radio", { name: "新建词书" }).check();
    await tp.getByPlaceholder("例如：八年级上册").fill(bookName);
    await tp.getByRole("button", { name: "确认导入" }).click();
    await expect(tp.getByText("导入完成")).toBeVisible();
    await expect(tp.getByText("句型与课文 2 篇、4 句。")).toBeVisible();
    await tp.getByRole("button", { name: "打开词书" }).click();
    await tp.waitForURL(/\/books\/[^/]+$/);
    const bookUrl = new URL(tp.url()).pathname;

    // 学生：单元页的句型、课文
    const sctx = await browser.newContext();
    const sp = await sctx.newPage();
    await login(sp, student.email);
    await expect(sp).toHaveURL(/\/today/);
    await sp.goto(bookUrl);
    const unitTabs = sp.locator(".vx-unit-tabs");
    await expect(unitTabs).toContainText("句型 2");
    await unitTabs.locator(".ant-segmented-item", { hasText: "句型" }).click();
    const list = sp.locator('.vx-unit-texts[data-kind="list"]');
    await expect(list).toContainText(`I ${w2} the ${w1}.`);
    await expect(list).toContainText("我乙这个甲。");
    await expect(list).toContainText(`I find ... ${w1}.`);
    // 学生只读：没有编辑入口
    await expect(sp.getByRole("button", { name: /新建句型清单/ })).toHaveCount(0);
    await unitTabs.locator(".ant-segmented-item", { hasText: "课文" }).click();
    const reader = sp.locator('.vx-unit-texts[data-kind="text"]');
    await expect(reader).toContainText(`My ${w1}`);
    await expect(reader).toContainText(`This is my ${w1}.`);
    await expect(reader).toContainText(`We ${w2} it every day.`);

    // 学生：单词详情的「出现在这些句子里」
    const detail = await apiOk<{ units: { id: string }[] }>(await student.api.get(`/api${bookUrl}`));
    const w1Id = (await wordIds(student.api, detail.units[0].id))[w1];
    await sp.goto(`/words/${w1Id}`);
    const block = sp.locator(".vx-word-sentences");
    await expect(block).toContainText("出现在这些句子里");
    await expect(block).toContainText(`I ${w2} the ${w1}.`);
    await expect(block).toContainText(`This is my ${w1}.`);
    await expect(block).toContainText(`My ${w1}`);

    await tctx.close();
    await sctx.close();
    await teacher.api.dispose();
    await student.api.dispose();
  });
});
