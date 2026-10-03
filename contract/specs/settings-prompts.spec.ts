/**
 * 系统设置 → AI 提示词的默认要求模板（旧 apps/api/tests/settings-prompts.flow.test.ts 改写）：
 * 权限、默认值、只做非空和长度校验、保存后预览里的可见提示词跟着变、恢复默认。
 * 结束前一律 DELETE /settings/ai/prompts/example 与 /passage，不影响其他并发跑的 agent。
 */
import { afterAll, describe, expect, it } from "vitest";
import { anon, login, STAMP } from "../lib/client";
import { goOnly, ready } from "../lib/areas";

const CUSTOM = `契约测试模板：例句不超过 8 个词，场景都放在运动会上 ${STAMP}`;

/**
 * 预览里的可见提示词以默认模板开头。
 * Go（spec 0005）：默认模板带 {学段}、{句长} 等占位符，预览时按学段替换成具体文字，
 * 所以每个占位符匹配一段不含换行的文字，其余文字逐字比对，且预览里不能残留未替换的占位符。
 */
function startsWithTemplate(prompt: string, template: string): boolean {
  if (!goOnly) return prompt.startsWith(template);
  const escaped = template.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const pattern = escaped.replace(/\\\{[^{}\n]+?\\\}/g, "[^\\n]+?");
  return new RegExp(`^${pattern}`).test(prompt) && !/\{[^{}\n]+\}/.test(prompt);
}

describe.runIf(ready("settings"))("系统设置 · AI 提示词模板", () => {
  afterAll(async () => {
    const admin = await login("admin@vinx.test");
    await admin.del("/settings/ai/prompts/example");
    await admin.del("/settings/ai/prompts/passage");
  });

  it("非管理员 403，未登录 401", async () => {
    const teacher = await login("teacher@vinx.test");
    const student = await login("student@vinx.test");
    expect((await teacher.get("/settings/ai/prompts")).status).toBe(403);
    expect((await student.put("/settings/ai/prompts", { example: CUSTOM })).status).toBe(403);
    expect((await teacher.del("/settings/ai/prompts/example")).status).toBe(403);
    expect((await anon().get("/settings/ai/prompts")).status).toBe(401);
  });

  it("没有改过时返回默认模板（不含输出格式），isDefault 为 true", async () => {
    const admin = await login("admin@vinx.test");
    await admin.del("/settings/ai/prompts/example");
    await admin.del("/settings/ai/prompts/passage");
    const r = await admin.get("/settings/ai/prompts");
    expect(r.status).toBe(200);
    expect(r.body.data.example).toMatchObject({ key: "example", isDefault: true });
    expect(r.body.data.example.current).toBe(r.body.data.example.defaultText);
    expect(r.body.data.example.current).not.toMatch(/json/i);
    expect(r.body.data.passage).toMatchObject({ key: "passage", isDefault: true });
  });

  it("空白、超长、空提交、未知 key 返回 400 VALIDATION，不保存", async () => {
    const admin = await login("admin@vinx.test");
    const blank = await admin.put("/settings/ai/prompts", { example: "   " });
    expect(blank.status).toBe(400);
    expect(blank.body.error.code).toBe("VALIDATION");
    const long = await admin.put("/settings/ai/prompts", { passage: "x".repeat(6001) });
    expect(long.status).toBe(400);
    const empty = await admin.put("/settings/ai/prompts", {});
    expect(empty.status).toBe(400);
    const unknownKey = await admin.del("/settings/ai/prompts/other");
    expect(unknownKey.status).toBe(400);
    // 未成功保存：仍是默认
    expect((await admin.get("/settings/ai/prompts")).body.data.example.isDefault).toBe(true);
  });

  it("保存后显示已修改，只存改过的项；恢复默认后回到默认", async () => {
    const admin = await login("admin@vinx.test");
    const saved = await admin.put("/settings/ai/prompts", { example: `  ${CUSTOM}\n` });
    expect(saved.status).toBe(200);
    expect(saved.body.data.example).toMatchObject({ current: CUSTOM, isDefault: false });
    expect(saved.body.data.passage.isDefault).toBe(true); // 只动了 example

    const reset = await admin.del("/settings/ai/prompts/example");
    expect(reset.status).toBe(200);
    expect(reset.body.data.example).toMatchObject({ isDefault: true });
    expect(reset.body.data.example.current).toBe(reset.body.data.example.defaultText);
  });

  it("保存后预览里的可见提示词用新的模板；恢复默认后预览也回到默认", async () => {
    const admin = await login("admin@vinx.test");
    const books = await admin.get("/books");
    const sysBook = books.body.data.items.find((b: { isSystem: boolean }) => b.isSystem);
    expect(sysBook).toBeDefined();
    const detail = await admin.get(`/books/${sysBook.id}`);
    const unitId = detail.body.data.units[0].id as string;
    const words = await admin.get(`/units/${unitId}/words?limit=1`);
    const wordId = words.body.data.items[0].id as string;

    const defaultPreview = await admin.post(`/ai/words/${wordId}/example/preview`);
    expect(defaultPreview.status).toBe(200);
    const defaults = await admin.get("/settings/ai/prompts");
    expect(startsWithTemplate(defaultPreview.body.data.prompt, defaults.body.data.example.defaultText)).toBe(true);

    const saved = await admin.put("/settings/ai/prompts", { example: `  ${CUSTOM}\n` });
    expect(saved.status).toBe(200);
    const customPreview = await admin.post(`/ai/words/${wordId}/example/preview`);
    expect(customPreview.status).toBe(200);
    expect(customPreview.body.data.prompt.startsWith(`${CUSTOM}\n\n请为下面 1 个词各写一个例句：`)).toBe(true);

    const reset = await admin.del("/settings/ai/prompts/example");
    expect(reset.status).toBe(200);
    const backToDefault = await admin.post(`/ai/words/${wordId}/example/preview`);
    expect(startsWithTemplate(backToDefault.body.data.prompt, defaults.body.data.example.defaultText)).toBe(true);
  });
});
