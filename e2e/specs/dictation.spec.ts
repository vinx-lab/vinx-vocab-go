import { test, expect } from "@playwright/test";
import { login } from "../support/helpers";
import { apiOk, importBook, letters, newClassWithStudent, newTeacher, wordIds } from "../support/school";

/**
 * 默写单（spec 0006）：出一份包含单词、短语、句子的默写单 → 打印页显示题目页和答案页 → 批改页标两道错题并提交
 * → 今日页状态变成已批改 → 覆盖进度里对应的词变成「要学」。
 * 新老师、班级、学生、带句型清单的词书与班级目标词书由 API 准备；出题、打印、批改、查看进度走页面（学生本人账号，自批）。
 */
test.describe("默写单", () => {
  test.setTimeout(120_000);

  test("出题 → 打印（题目页 + 答案页）→ 批改两道错题 → 今日已批改 → 错的词变成要学", async ({ browser }) => {
    const teacher = await newTeacher("默写老师");
    const { classId, student } = await newClassWithStudent(teacher, "默写班", "默写同学");
    const tag = letters();
    const w1 = `e2edictalpha${tag}`;
    const w2 = `e2edictbravo${tag}`;
    const phrase = `e2edictpick${tag} up`;
    const book = await importBook(teacher.api, "默写词书", [
      {
        name: "Unit 1",
        entries: [
          { spelling: w1, definition: "默写甲", partOfSpeech: "n." },
          { spelling: w2, definition: "默写乙", partOfSpeech: "n." },
          { spelling: phrase, definition: "默写捡起", type: "phrase" },
        ],
        texts: [
          {
            title: "重点句型",
            kind: "list",
            sentences: [
              { en: `I like the ${w1}.`, cn: "我喜欢这个甲。" },
              { en: `I can ${phrase} the ${w2}.`, cn: "我能捡起这个乙。" },
            ],
          },
        ],
      },
    ]);
    await apiOk(await teacher.api.put(`/api/classes/${classId}/target-books`, { data: { bookIds: [book.bookId] } }));

    const ctx = await browser.newContext();
    const page = await ctx.newPage();
    await page.addInitScript(() => {
      window.print = () => {};
    });
    await login(page, student.email);
    await expect(page).toHaveURL(/\/today/);

    // 出题：默写单 · 词来自「目标：未测的词」· 句子来自单元的句型清单
    await page.goto("/sheets/new");
    await page.locator('[aria-label="格式"] .ant-segmented-item', { hasText: "默写单" }).click();
    await expect(page.getByText("出默写单").first()).toBeVisible();
    await page.locator(".ant-segmented-item", { hasText: "目标：未测的词" }).click();
    for (const s of [w1, w2, phrase]) await expect(page.getByText(s, { exact: true }).first()).toBeVisible();
    await page.getByRole("combobox", { name: "句子来源词书" }).click();
    await page.getByRole("option", { name: book.name }).click();
    await page.getByRole("combobox", { name: "句子来源单元" }).click();
    await page.getByRole("option", { name: "Unit 1" }).click();
    await page.getByRole("checkbox", { name: /重点句型（2 句）/ }).check();
    await expect(page.getByText(/生成 1 份（5 题）/)).toBeVisible();
    await page.getByRole("button", { name: /生成并打印/ }).click();
    await page.waitForURL(/\/sheets\/[^/]+\/print/);
    const sheetId = new URL(page.url()).pathname.split("/")[2];

    // 打印页：题目页 + 答案页
    const question = page.locator(".vx-dict-page:not(.vx-dict-answer-page)");
    const answer = page.locator(".vx-dict-answer-page");
    await expect(question).toHaveCount(1);
    await expect(answer).toHaveCount(1);
    await expect(question.locator(".vx-dict-head")).toContainText("VinxVocab 默写单 #1");
    await expect(question.locator(".vx-dict-head")).toContainText("默写同学");
    await expect(question).toContainText("我喜欢这个甲。");
    await expect(question).toContainText("默写捡起");
    await expect(question).not.toContainText(`I like the ${w1}.`);
    await expect(answer.locator(".vx-dict-head")).toContainText("答案（家长 / 老师批改用）");
    await expect(answer.locator(".vx-dict-answer")).toHaveCount(5);
    await expect(answer).toContainText(`I like the ${w1}.`);
    await expect(answer).toContainText(phrase);

    // 今日页：待批改 → 批改页
    await page.goto("/today");
    await expect(page.getByText("默写单 #1 · 待批改 · 5 题")).toBeVisible();
    await page.getByRole("button", { name: /批\s*改/ }).click();
    await page.waitForURL(new RegExp(`/sheets/${sheetId}/grade`));
    await expect(page.locator(".vx-grade-item")).toHaveCount(5);
    // 第 1 题是单词（单词分区在前），再标一道句子题
    const first = page.getByRole("button", { name: "第 1 题" });
    await expect(first).toContainText("单词");
    const wrongWord = (await first.locator(".vx-grade-answer").innerText()).trim();
    expect([w1, w2]).toContain(wrongWord);
    await first.click();
    await expect(first).toHaveAttribute("aria-pressed", "true");
    const sentenceItem = page.locator(".vx-grade-toggle", { hasText: "我喜欢这个甲。" });
    await sentenceItem.click();
    await page.getByLabel("第 1 题学生写的").fill("wrong");
    await expect(page.getByText("对 3 / 错 2")).toBeVisible();
    await page.getByRole("button", { name: "提交批改" }).click();
    await page.getByRole("button", { name: "确认提交" }).click();
    await expect(page.getByText("批改已提交")).toBeVisible();

    // 今日页：已批改（自批）
    await page.goto("/today");
    await expect(page.getByText("默写单 #1 · 已批改")).toBeVisible();
    await expect(page.getByText("成绩 3/5，错的词和句子已经进了「要学」。")).toBeVisible();
    await expect(page.getByText("默写单 #1 · 待批改")).toHaveCount(0);
    await expect(page.getByLabel("目标进度")).toContainText("要学 1");

    // 覆盖进度：错的词在「要学」里
    await page.goto("/records");
    await page.getByRole("button", { name: new RegExp(book.name) }).click();
    const learningTab = page.locator(".ant-segmented-item", { hasText: "要学 1" });
    await expect(learningTab).toBeVisible();
    await expect(page.getByText(wrongWord, { exact: true }).first()).toBeVisible();

    await ctx.close();
    await teacher.api.dispose();
    await student.api.dispose();
  });
  test("手机上批改页的提交栏不被底部导航挡住", async ({ browser }) => {
    const teacher = await newTeacher("默写老师");
    const { student } = await newClassWithStudent(teacher, "默写班", "默写同学");
    const tag = letters();
    const words = Array.from({ length: 20 }, (_, i) => ({ spelling: `e2emobile${tag}${String.fromCharCode(97 + i)}`, definition: `手机${i}`, partOfSpeech: "n." }));
    const book = await importBook(teacher.api, "手机默写", [{ name: "Unit 1", entries: words }]);
    const ids = await wordIds(teacher.api, book.unitIds[0]);
    const created = await apiOk<{ id: string }>(
      await student.api.post("/api/sheets", { data: { format: "dictation", items: Object.values(ids).map((wordId) => ({ type: "word", wordId })) } }),
    );

    const ctx = await browser.newContext({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true });
    const page = await ctx.newPage();
    await login(page, student.email);
    await expect(page).toHaveURL(/\/today/);
    await page.goto(`/sheets/${created.id}/grade`);
    await expect(page.locator(".vx-grade-item")).toHaveCount(20);
    const submit = page.getByRole("button", { name: "提交批改" });
    await expect(submit).toBeInViewport();
    // 按钮中心点上最上层的元素必须是按钮自己（没被固定的底部导航盖住）
    const onTop = await submit.evaluate((el) => {
      const r = el.getBoundingClientRect();
      const hit = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
      return !!hit && el.contains(hit);
    });
    expect(onTop).toBe(true);
    await submit.click();
    await expect(page.getByRole("button", { name: "确认提交" })).toBeVisible();

    await ctx.close();
    await teacher.api.dispose();
    await student.api.dispose();
  });
});
