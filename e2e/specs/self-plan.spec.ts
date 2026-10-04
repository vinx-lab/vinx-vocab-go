import { test, expect, request as playwrightRequest } from "@playwright/test";
import { login, PASSWORD, uid } from "../support/helpers";
import { apiOk, BASE, importBook, letters, newTeacher } from "../support/school";

/**
 * 班级自主开关与「先定目标词书，再建计划」（spec 0008）：
 * - 老师建班必须选自主选项（不选不能创建）；
 * - 不允许时，学生的「新建计划」「自己安排计划」不可用；已有的自建计划显示「班级未开放自主安排，暂停中」；
 *   老师关闭前弹窗说明影响；
 * - 允许时，学生在「我的」追加目标词书后，建计划页只列目标里的书（班级的 + 追加的），能用追加的书建计划。
 * 新老师、两本词书、学生账号由 API 准备；建班、班级设置、追加目标、建计划走页面。
 */
test.describe("班级自主开关与目标优先", () => {
  test.setTimeout(120_000);

  test("建班必选自主选项；不允许时学生不能自建、已有自建计划暂停；允许时追加目标后用追加的书建计划", async ({ browser }) => {
    const teacher = await newTeacher("自主老师");
    const tag = letters();
    const classBook = await importBook(teacher.api, "班级目标书", [{ name: "Unit 1", entries: ["alpha", "bravo"].map((s, i) => ({ spelling: `e2esp${s}${tag}`, definition: `班级义${i + 1}` })) }]);
    const ownBook = await importBook(teacher.api, "自选书", [{ name: "Unit 1", entries: ["charlie", "delta"].map((s, i) => ({ spelling: `e2esp${s}${tag}`, definition: `自选义${i + 1}` })) }]);
    const otherBook = await importBook(teacher.api, "目标外的书", [{ name: "Unit 1", entries: [{ spelling: `e2especho${tag}`, definition: "目标外" }] }]);

    // ---------- 老师：建班必须选一次 ----------
    const tctx = await browser.newContext();
    const tp = await tctx.newPage();
    await login(tp, teacher.email);
    await expect(tp).toHaveURL(/\/today/);
    await tp.goto("/classes");
    await tp.getByRole("button", { name: /新建班级/ }).first().click();
    const className = `自主班 ${uid()}`;
    await tp.getByLabel("班级名称").fill(className);
    await tp.getByRole("dialog").getByRole("button", { name: /创\s*建/ }).click();
    await expect(tp.getByText("请选择是否允许学生自主安排计划")).toBeVisible();
    await tp.getByRole("radio", { name: /^不允许/ }).check();
    await tp.getByRole("dialog").getByRole("button", { name: /创\s*建/ }).click();
    await tp.waitForURL(/\/classes\/[^/]+$/);
    const classId = tp.url().split("/").pop()!.split("?")[0];
    const cls = await apiOk<{ inviteCode: string; allowSelfPlan: boolean }>(await tp.request.get(`/api/classes/${classId}`));
    expect(cls.allowSelfPlan).toBe(false);
    await apiOk(await teacher.api.put(`/api/classes/${classId}/target-books`, { data: { bookIds: [classBook.bookId] } }));

    // 学生凭邀请码注册入班
    const studentEmail = `e2e-s-${uid()}@vinx.test`;
    const sapi = await playwrightRequest.newContext({ baseURL: BASE });
    const signup = await sapi.post("/api/auth/signup", { data: { email: studentEmail, password: PASSWORD, name: "自主同学", inviteCode: cls.inviteCode } });
    expect(signup.status(), await signup.text()).toBe(201);

    // ---------- 学生：不允许时不能自建 ----------
    const sctx = await browser.newContext();
    const sp = await sctx.newPage();
    await login(sp, studentEmail);
    await expect(sp).toHaveURL(/\/today/);
    await expect(sp.getByRole("button", { name: /自己安排计划/ })).toBeDisabled();
    await sp.goto("/plans");
    await expect(sp.getByRole("button", { name: /新建计划/ })).toBeDisabled();
    await expect(sp.getByText("班级未开放自主安排计划").first()).toBeVisible();
    // 「我的」：只读班级目标，不能追加
    await sp.goto("/profile");
    await expect(sp.getByText(new RegExp(`由班级 ${className}`)).first()).toBeVisible();
    await expect(sp.getByText("班级未开放自主安排，目标词书由老师设置。")).toBeVisible();

    // ---------- 老师：班级设置里打开（打开不需要确认） ----------
    await tp.goto(`/classes/${classId}?tab=settings`);
    const sw = tp.getByRole("switch", { name: "允许学生自主安排计划" });
    await expect(sw).toHaveAttribute("aria-checked", "false");
    await sw.click();
    await expect(tp.getByText("已允许学生自主安排计划")).toBeVisible();
    await expect(sw).toHaveAttribute("aria-checked", "true");

    // ---------- 学生：在「我的」追加目标词书 ----------
    await sp.goto("/profile");
    await expect(sp.getByText("我追加的")).toBeVisible();
    const targetsCard = sp.locator(".ant-card", { hasText: "我追加的" });
    await targetsCard.getByRole("combobox", { name: "添加词书" }).click();
    await expect(sp.getByRole("option", { name: new RegExp(classBook.name) })).toHaveCount(0); // 班级的书不重复添加
    await sp.getByRole("option", { name: new RegExp(ownBook.name) }).click();
    await targetsCard.getByRole("button", { name: /保\s*存/ }).click();
    await expect(sp.getByText("目标词书已保存")).toBeVisible();

    // 建计划页只列目标里的书（班级的 + 追加的），不列目标外的书
    await sp.goto("/plans/new");
    await expect(sp.getByText(/只列我的目标词书里的书/)).toBeVisible();
    // 已经预选了一本书，选中项会盖住搜索框，点外层选择框打开下拉
    await sp.locator(".ant-select-selector").first().click();
    await expect(sp.getByRole("option", { name: new RegExp(classBook.name) })).toBeVisible();
    await expect(sp.getByRole("option", { name: new RegExp(ownBook.name) })).toBeVisible();
    await expect(sp.getByRole("option", { name: new RegExp(otherBook.name) })).toHaveCount(0);
    await sp.getByRole("option", { name: new RegExp(ownBook.name) }).click();
    await sp.getByRole("checkbox", { name: /全选本书/ }).check();
    await sp.getByRole("button", { name: /创建计划/ }).click();
    await sp.waitForURL(/\/plans\/[^/]+$/);
    const planId = sp.url().split("/").pop()!;
    await expect(sp.getByText("自己安排")).toBeVisible();

    // 词书页：目标外的书「用所选单元建计划」置灰，提示先加入目标
    await sp.goto(`/books/${otherBook.bookId}`);
    await expect(sp.getByTestId("book-plan-hint")).toContainText("先把这本书加入目标词书");
    await expect(sp.getByRole("button", { name: "加入我的目标" })).toBeVisible();

    // ---------- 老师：关闭前弹窗说明影响 ----------
    // 这个流程里学生恰好有 1 份自建计划、1 本追加的目标词书；改流程时同步改下面的数字
    await tp.goto(`/classes/${classId}?tab=settings`);
    await tp.getByRole("switch", { name: "允许学生自主安排计划" }).click();
    const dialog = tp.getByRole("dialog");
    await expect(dialog).toContainText("关闭学生自主安排？");
    await expect(dialog).toContainText("涉及 1 名学生");
    await expect(dialog).toContainText("1 份自建计划会暂停");
    await expect(dialog).toContainText("1 本自己追加的目标词书");
    await dialog.getByRole("button", { name: "关闭自主安排" }).click();
    await expect(tp.getByText("已关闭学生自主安排计划")).toBeVisible();

    // 学生：自建计划显示暂停中，今日页不出现并提示
    await sp.goto("/plans");
    await expect(sp.getByText("班级未开放自主安排，暂停中").first()).toBeVisible();
    await sp.goto(`/plans/${planId}`);
    await expect(sp.getByText("班级未开放自主安排，暂停中")).toBeVisible();
    await sp.goto("/today");
    await expect(sp.getByText(/你自己安排的 1 份计划暂停中/)).toBeVisible();

    await sapi.dispose();
    await tctx.close();
    await sctx.close();
  });
});
