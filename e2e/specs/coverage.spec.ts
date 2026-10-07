import { test, expect } from "@playwright/test";
import { login } from "../support/helpers";
import { apiOk, importBook, letters, newClassWithStudent, newTeacher, wordIds } from "../support/school";

/**
 * 目标词书与覆盖进度（spec 0003）：老师给班级设置目标 → 学生今日页出现进度卡 → 从「测未接触的词」生成单词单并在线测完
 * → 进度变化 → 老师在概览和学生详情里看到同样的数字。
 * 新老师、班级、学生、4 个词的词书由 API 准备；设置目标、进度卡、出单、开测走页面，作答走 API（与 word-sheet 用例相同）。
 */
test.describe("目标词书与覆盖进度", () => {
  test.setTimeout(120_000);

  test("老师设目标 → 学生今日进度卡 → 测未接触的词 → 进度变化 → 老师概览与学生详情一致", async ({ browser }) => {
    const teacher = await newTeacher("覆盖老师");
    const { classId, student } = await newClassWithStudent(teacher, "覆盖班", "覆盖同学");
    const tag = letters();
    const spellings = ["alpha", "bravo", "charlie", "delta"].map((s) => `e2ecov${s}${tag}`);
    const book = await importBook(teacher.api, "覆盖词书", [{ name: "Unit 1", entries: spellings.map((s, i) => ({ spelling: s, definition: `覆盖释义${i + 1}` })) }]);
    const ids = await wordIds(teacher.api, book.unitIds[0]);

    // 老师：班级详情 →「目标词书」→ 添加 → 保存
    const tctx = await browser.newContext();
    const tp = await tctx.newPage();
    await login(tp, teacher.email);
    await expect(tp).toHaveURL(/\/today/);
    await tp.goto(`/classes/${classId}`);
    await expect(tp.getByText("还没有设置目标词书").first()).toBeVisible();
    await tp.getByRole("tab", { name: "目标词书" }).click();
    await tp.getByRole("combobox", { name: "添加词书" }).click();
    await tp.getByRole("option", { name: new RegExp(book.name) }).click();
    await tp.getByRole("button", { name: /保\s*存/ }).click();
    await expect(tp.getByText("目标词书已保存")).toBeVisible();

    // 学生：今日页出现进度卡
    const sctx = await browser.newContext();
    const sp = await sctx.newPage();
    await sp.addInitScript(() => {
      window.print = () => {};
    });
    await login(sp, student.email);
    await expect(sp).toHaveURL(/\/today/);
    const card = sp.getByLabel("目标进度");
    await expect(card).toContainText("已接触 0 / 4");
    await expect(card).toContainText("已掌握 0 · 巩固中 0 · 刚记住 0 · 没记住 0 · 未接触 4");
    await expect(card.getByRole("button", { name: "练没记住的词" })).toBeDisabled();

    // 测未接触的词 → 生成页预选「目标：未接触的词」→ 生成并打印
    await card.getByRole("button", { name: "测未接触的词" }).click();
    await sp.waitForURL(/\/sheets\/new/);
    for (const s of spellings) await expect(sp.getByText(s, { exact: true }).first()).toBeVisible();
    await sp.getByRole("button", { name: /生成并打印/ }).click();
    await sp.waitForURL(/\/sheets\/[^/]+\/print/);

    // 今日开测，作答（第 4 个词认义答错），交卷
    await sp.goto("/today");
    await sp.getByRole("button", { name: "开始测试" }).click();
    await sp.waitForURL(/\/study\//);
    const sid = sp.url().split("/").pop()!;
    const s = await apiOk<{ items: { wordId: string }[]; modes: string[] }>(await sp.request.get(`/api/study/sessions/${sid}`));
    expect(s.items.map((i) => i.wordId).sort()).toEqual(Object.values(ids).sort());
    const wrongId = ids[spellings[3]];
    for (const item of s.items) {
      const w = (await apiOk<{ word: { spelling: string; definition: string } }>(await sp.request.get(`/api/records/words/${item.wordId}`))).word;
      for (const mode of s.modes) {
        const answer = mode === "recognition" ? (item.wordId === wrongId ? "__wrong__" : w.definition) : w.spelling;
        await apiOk(await sp.request.post(`/api/study/sessions/${sid}/answers`, { data: { wordId: item.wordId, mode, phase: "test", attempt: 1, answer } }));
      }
    }
    await apiOk(await sp.request.post(`/api/study/sessions/${sid}/complete`));

    // 进度变化
    await sp.goto("/today");
    await expect(card).toContainText("已接触 4 / 4");
    await expect(card).toContainText("已掌握 0 · 巩固中 0 · 刚记住 3 · 没记住 1 · 未接触 0");
    await expect(card.getByRole("button", { name: "测未接触的词" })).toBeDisabled();
    await expect(card.getByRole("button", { name: "练没记住的词" })).toBeEnabled();

    // 老师：概览的「目标覆盖」「没记住」，学生详情的分书进度
    await tp.goto(`/classes/${classId}`);
    const row = tp.getByRole("row", { name: /覆盖同学/ });
    await expect(row).toContainText("100%");
    await expect(row.getByTitle("已接触 4/4 词")).toBeVisible();
    await tp.goto(`/classes/${classId}/students/${student.id}`);
    await expect(tp.getByText("已接触 4 / 4 · 已掌握 0 · 巩固中 0 · 刚记住 3 · 没记住 1 · 未接触 0")).toBeVisible();
    await tp.getByRole("button", { name: new RegExp(book.name) }).click();
    await expect(tp.getByText(spellings[3]).first()).toBeVisible();

    await tctx.close();
    await sctx.close();
    await teacher.api.dispose();
    await student.api.dispose();
  });
});
