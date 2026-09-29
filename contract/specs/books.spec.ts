/**
 * 由旧 apps/api/src/routes/books.ts 的用例改写为纯 HTTP 黑盒。
 * 依赖 seed 数据：teacher@vinx.test（班主任，班级 DEMO01）、student@vinx.test（DEMO01 成员）、admin@vinx.test。
 * 每个用例用 STAMP 生成自己的词书/单元/单词，不依赖其他用例创建的数据。
 */
import { describe, it, expect } from "vitest";
import { anon, login, STAMP } from "../lib/client";
import { ready } from "../lib/areas";
import { expectShape } from "../lib/shape";

describe.runIf(ready("books"))("词书 / 单元 / 单词 / 导入", () => {
  it("列表可见性：系统词书 / 自有 / 班级学生可见", async () => {
    const admin = await login("admin@vinx.test");
    const teacher = await login("teacher@vinx.test");
    const student = await login("student@vinx.test"); // DEMO01 成员，班主任是 teacher@vinx.test
    const outsider = anon();
    const outsiderEmail = `outsider-${STAMP}@vinx.test`;
    const su = await outsider.post("/auth/signup", { email: outsiderEmail, password: "123456" });
    expect(su.status).toBe(201);

    const sysName = `sys-book-${STAMP}`;
    const sys = await admin.post("/books", { name: sysName, isSystem: true });
    expect(sys.status).toBe(200);
    expect(sys.body.data).toMatchObject({ name: sysName, isSystem: true });
    expect(typeof sys.body.data.ownerId).toBe("string");
    expectShape("books-create-raw", sys.body);

    const ownName = `own-book-${STAMP}`;
    const own = await teacher.post("/books", { name: ownName });
    expect(own.status).toBe(200);
    expect(own.body.data.isSystem).toBe(false);
    const ownId = own.body.data.id as string;

    const listAs = async (c: typeof teacher) => {
      const r = await c.get("/books");
      expect(r.status).toBe(200);
      return r.body.data.items as Array<{ id: string; name: string; isSystem: boolean; canEdit: boolean; ownerName: string | null }>;
    };

    const teacherItems = await listAs(teacher);
    expect(teacherItems.some((b) => b.name === sysName)).toBe(true);
    expect(teacherItems.some((b) => b.name === ownName)).toBe(true);
    const ownItem = teacherItems.find((b) => b.id === ownId)!;
    expect(ownItem.canEdit).toBe(true);
    const sysItem = teacherItems.find((b) => b.name === sysName)!;
    expect(sysItem.canEdit).toBe(false);
    expectShape("books-list", { items: teacherItems, total: teacherItems.length });

    // 学生在老师班上：能看到系统词书和班主任的词书，且都不可编辑
    const studentItems = await listAs(student);
    expect(studentItems.some((b) => b.name === sysName)).toBe(true);
    expect(studentItems.some((b) => b.id === ownId)).toBe(true);
    expect(studentItems.find((b) => b.id === ownId)!.canEdit).toBe(false);

    // 不在任何班级的学生：只看到系统词书
    const outsiderItems = await listAs(outsider);
    expect(outsiderItems.some((b) => b.name === sysName)).toBe(true);
    expect(outsiderItems.some((b) => b.id === ownId)).toBe(false);

    // 管理员：全部可见且都可编辑
    const adminItems = await listAs(admin);
    expect(adminItems.find((b) => b.id === ownId)!.canEdit).toBe(true);
    expect(adminItems.find((b) => b.name === sysName)!.canEdit).toBe(true);

    // 详情：不可见的词书视为不存在
    const outsiderDetail = await outsider.get(`/books/${ownId}`);
    expect(outsiderDetail.status).toBe(404);
    expect(outsiderDetail.body.error).toMatchObject({ code: "NOT_FOUND", message: "词书不存在" });

    const teacherDetail = await teacher.get(`/books/${ownId}`);
    expect(teacherDetail.status).toBe(200);
    expect(teacherDetail.body.data).toMatchObject({ id: ownId, name: ownName, isSystem: false, canEdit: true, units: [] });
    expectShape("books-detail", teacherDetail.body);

    // 未登录 → 401
    const anonList = await anon().get("/books");
    expect(anonList.status).toBe(401);
    expect(anonList.body.error.code).toBe("UNAUTHORIZED");

    // 管理员能看到不在任何词书里的词（GET /words/:id）：先建一个词再把它从唯一单元里移除
    const orphanSpelling = `orphan${STAMP}`;
    const orphanUnit = await teacher.post(`/books/${ownId}/units`, { name: `orphan-u-${STAMP}` });
    const orphanAdd = await teacher.post(`/units/${orphanUnit.body.data.id}/words`, { spelling: orphanSpelling, definition: "孤儿词" });
    const orphanList = await teacher.get(`/units/${orphanUnit.body.data.id}/words`);
    const orphanId = orphanList.body.data.items[0].id as string;
    expect(orphanAdd.status).toBe(200);
    await teacher.del(`/units/${orphanUnit.body.data.id}/words/${orphanId}`);
    // 非管理员：不在任何可见词书里的词视为不存在
    const teacherOrphan = await teacher.get(`/words/${orphanId}`);
    expect(teacherOrphan.status).toBe(404);
    expect(teacherOrphan.body.error).toMatchObject({ code: "NOT_FOUND", message: "单词不存在" });
    // 管理员：仍能看到，usedBy 为空数组
    const adminOrphan = await admin.get(`/words/${orphanId}`);
    expect(adminOrphan.status).toBe(200);
    expect(adminOrphan.body.data.usedBy).toEqual([]);

    // GET /words/:id：不存在的 id → 404
    const noSuchWord = await teacher.get("/words/does-not-exist");
    expect(noSuchWord.status).toBe(404);
    expect(noSuchWord.body.error).toMatchObject({ code: "NOT_FOUND", message: "单词不存在" });
  });

  it("权限：学生不能建词书；只能编辑自己创建的非系统词书", async () => {
    const student = await login("student@vinx.test");
    const teacher = await login("teacher@vinx.test");
    const admin = await login("admin@vinx.test");

    const denied = await student.post("/books", { name: `nope-${STAMP}` });
    expect(denied.status).toBe(403);
    expect(denied.body.error.code).toBe("FORBIDDEN");

    const sysName = `sys2-${STAMP}`;
    const sys = await admin.post("/books", { name: sysName, isSystem: true });
    const sysId = sys.body.data.id as string;

    // 老师不能编辑系统词书
    const editSys = await teacher.patch(`/books/${sysId}`, { name: "改名" });
    expect(editSys.status).toBe(403);
    expect(editSys.body.error).toMatchObject({ code: "FORBIDDEN", message: "只能编辑自己创建的词书" });
    const delSys = await teacher.del(`/books/${sysId}`);
    expect(delSys.status).toBe(403);

    // 老师改自己的词书：名称、描述（含清空为 null）
    const own = await teacher.post("/books", { name: `edit-${STAMP}`, description: "初始描述" });
    const ownId = own.body.data.id as string;
    const patched = await teacher.patch(`/books/${ownId}`, { name: `edit2-${STAMP}`, description: null });
    expect(patched.status).toBe(200);
    expect(patched.body.data).toMatchObject({ name: `edit2-${STAMP}`, description: null });
    // 非管理员传 isSystem 被忽略
    const ignoreSys = await teacher.patch(`/books/${ownId}`, { isSystem: true });
    expect(ignoreSys.body.data.isSystem).toBe(false);

    // 校验：名称过长
    const badName = await teacher.patch(`/books/${ownId}`, { name: "x".repeat(61) });
    expect(badName.status).toBe(400);
    expect(badName.body.error.details).toEqual({ name: "名称过长" });

    // 建词书校验：空名称
    const emptyName = await teacher.post("/books", { name: "" });
    expect(emptyName.status).toBe(400);
    expect(emptyName.body.error.details).toEqual({ name: "名称不能为空" });

    // 删除：自己的空词书可删
    const del = await teacher.del(`/books/${ownId}`);
    expect(del.status).toBe(200);
    expect(del.body).not.toHaveProperty("data");
    const gone = await teacher.get(`/books/${ownId}`);
    expect(gone.status).toBe(404);
  });

  it("单元：创建、改名、排序、删除", async () => {
    const teacher = await login("teacher@vinx.test");
    const book = await teacher.post("/books", { name: `units-${STAMP}` });
    const bookId = book.body.data.id as string;

    const u1 = await teacher.post(`/books/${bookId}/units`, { name: "Unit 1" });
    expect(u1.status).toBe(200);
    expect(u1.body.data).toMatchObject({ bookId, name: "Unit 1", sortOrder: 0 });
    const u2 = await teacher.post(`/books/${bookId}/units`, { name: "Unit 2" });
    expect(u2.body.data.sortOrder).toBe(1);
    const u3 = await teacher.post(`/books/${bookId}/units`, { name: "Unit 3" });
    expect(u3.body.data.sortOrder).toBe(2);
    expectShape("units-create-raw", u1.body);

    const rename = await teacher.patch(`/units/${u1.body.data.id}`, { name: "Unit 1 改" });
    expect(rename.status).toBe(200);
    expect(rename.body.data.name).toBe("Unit 1 改");

    // 空名称校验
    const badUnit = await teacher.post(`/books/${bookId}/units`, { name: "" });
    expect(badUnit.body.error.details).toEqual({ name: "名称不能为空" });

    // 显式 sortOrder：类型错误 + 正常生效
    const floatOrder = await teacher.post(`/books/${bookId}/units`, { name: `float-${STAMP}`, sortOrder: 1.5 });
    expect(floatOrder.status).toBe(400);
    expect(floatOrder.body.error.details).toEqual({ sortOrder: "Expected integer, received float" });
    const strOrder = await teacher.post(`/books/${bookId}/units`, { name: `str-${STAMP}`, sortOrder: "1" });
    expect(strOrder.status).toBe(400);
    expect(strOrder.body.error.details).toEqual({ sortOrder: "Expected number, received string" });
    const explicitOrder = await teacher.post(`/books/${bookId}/units`, { name: `explicit-${STAMP}`, sortOrder: 99 });
    expect(explicitOrder.body.data.sortOrder).toBe(99);
    // 删掉这个临时单元，不干扰下面按 u1/u2/u3 三个单元的排序断言
    await teacher.del(`/units/${explicitOrder.body.data.id}`);

    // 同名单元 → 409 唯一约束冲突
    const dupUnit = await teacher.post(`/books/${bookId}/units`, { name: "Unit 2" });
    expect(dupUnit.status).toBe(409);
    expect(dupUnit.body.error).toMatchObject({ code: "DUPLICATE", message: "数据已存在，唯一性冲突" });

    // PATCH 直接改 sortOrder
    const reorderOne = await teacher.patch(`/units/${u2.body.data.id}`, { sortOrder: 42 });
    expect(reorderOne.status).toBe(200);
    expect(reorderOne.body.data.sortOrder).toBe(42);

    // 老师不能操作管理员词书里的单元
    const admin = await login("admin@vinx.test");
    const sysBookForUnits = await admin.post("/books", { name: `sys-units-${STAMP}`, isSystem: true });
    const sysUnitForUnits = await admin.post(`/books/${sysBookForUnits.body.data.id}/units`, { name: "SysU" });
    const patchForbidden = await teacher.patch(`/units/${sysUnitForUnits.body.data.id}`, { name: "改" });
    expect(patchForbidden.status).toBe(403);
    expect(patchForbidden.body.error).toMatchObject({ code: "FORBIDDEN", message: "只能编辑自己创建的词书" });
    const deleteForbidden = await teacher.del(`/units/${sysUnitForUnits.body.data.id}`);
    expect(deleteForbidden.status).toBe(403);
    const deleteNotFound = await teacher.del(`/units/does-not-exist`);
    expect(deleteNotFound.status).toBe(404);
    expect(deleteNotFound.body.error).toMatchObject({ code: "NOT_FOUND", message: "单元不存在" });

    // 排序：整册一次提交
    const ids = [u3.body.data.id, u1.body.data.id, u2.body.data.id];
    const order = await teacher.patch(`/books/${bookId}/units/order`, { unitIds: ids });
    expect(order.status).toBe(200);
    expect(order.body.data).toEqual({ ordered: 3 });
    const detail = await teacher.get(`/books/${bookId}`);
    expect(detail.body.data.units.map((u: { id: string }) => u.id)).toEqual(ids);

    // 顺序缺项 → 400
    const badOrder = await teacher.patch(`/books/${bookId}/units/order`, { unitIds: [u1.body.data.id] });
    expect(badOrder.status).toBe(400);
    expect(badOrder.body.error).toMatchObject({ code: "VALIDATION", message: "单元顺序必须包含本词书的全部单元" });

    // unitIds: [] → details
    const emptyOrder = await teacher.patch(`/books/${bookId}/units/order`, { unitIds: [] });
    expect(emptyOrder.status).toBe(400);
    expect(emptyOrder.body.error.details).toEqual({ unitIds: "请提供单元顺序" });

    // 重复 id 与他书 id 被过滤后仍需覆盖全部单元才算数
    const otherBook = await teacher.post("/books", { name: `other-${STAMP}` });
    const otherUnit = await teacher.post(`/books/${otherBook.body.data.id}/units`, { name: "OtherU" });
    const dedupOrder = await teacher.patch(`/books/${bookId}/units/order`, {
      unitIds: [u1.body.data.id, u2.body.data.id, u3.body.data.id, u1.body.data.id, otherUnit.body.data.id],
    });
    expect(dedupOrder.status).toBe(200);
    expect(dedupOrder.body.data).toEqual({ ordered: 3 });
    const detailAfterDedup = await teacher.get(`/books/${bookId}`);
    expect(detailAfterDedup.body.data.units.map((u: { id: string }) => u.id)).toEqual([u1.body.data.id, u2.body.data.id, u3.body.data.id]);

    // 他人（学生）不能操作
    const student = await login("student@vinx.test");
    const forbiddenUnit = await student.post(`/books/${bookId}/units`, { name: "x" });
    expect(forbiddenUnit.status).toBe(403);

    // 删除
    const delUnit = await teacher.del(`/units/${u3.body.data.id}`);
    expect(delUnit.status).toBe(200);
    const afterDel = await teacher.get(`/books/${bookId}`);
    expect(afterDel.body.data.units.map((u: { id: string }) => u.id)).not.toContain(u3.body.data.id);

    // 不存在的单元
    const notFound = await teacher.patch(`/units/does-not-exist`, { name: "x" });
    expect(notFound.status).toBe(404);
    expect(notFound.body.error).toMatchObject({ code: "NOT_FOUND", message: "单元不存在" });
  });

  it("单词：加入单元（拼写复用）、列表搜索、移除关联、编辑权限（被多词书引用）", async () => {
    const teacher = await login("teacher@vinx.test");
    const admin = await login("admin@vinx.test");
    const book = await teacher.post("/books", { name: `words-${STAMP}` });
    const bookId = book.body.data.id as string;
    const unit = await teacher.post(`/books/${bookId}/units`, { name: "U1" });
    const unitId = unit.body.data.id as string;

    const spelling = `apple${STAMP}`;
    const add1 = await teacher.post(`/units/${unitId}/words`, { spelling, definition: "苹果" });
    expect(add1.status).toBe(200);
    expect(add1.body.data).toEqual({ reused: false, linked: true });

    // 同一单元重复添加：createMany skipDuplicates，不产生新关联，但也不算“复用词条”（因为词本身就在这个单元里，重点是拼写是否命中已存在的 Word）
    const add1b = await teacher.post(`/units/${unitId}/words`, { spelling, definition: "苹果（重复提交）" });
    expect(add1b.status).toBe(200);
    expect(add1b.body.data).toEqual({ reused: true, linked: false });

    // 另一个单元加同名词：复用词条，产生新关联
    const unit2 = await teacher.post(`/books/${bookId}/units`, { name: "U2" });
    const unit2Id = unit2.body.data.id as string;
    const add2 = await teacher.post(`/units/${unit2Id}/words`, { spelling, definition: "苹果" });
    expect(add2.status).toBe(200);
    expect(add2.body.data).toEqual({ reused: true, linked: true });

    // 列表 + 搜索
    const list = await teacher.get(`/units/${unitId}/words`);
    expect(list.status).toBe(200);
    expect(list.body.data.unit).toMatchObject({ id: unitId, bookId, canEdit: true });
    const item = list.body.data.items.find((w: { spelling: string }) => w.spelling === spelling);
    expect(item).toBeDefined();
    expect(item.usedByUnits).toBe(2);
    expectShape("unit-words-list", list.body);

    const search = await teacher.get(`/units/${unitId}/words?q=${spelling}`);
    expect(search.body.data.items.length).toBe(1);
    const missSearch = await teacher.get(`/units/${unitId}/words?q=${spelling}-nope`);
    expect(missSearch.body.data.items.length).toBe(0);

    // 词条详情：usedBy 含两个单元
    const wordId = item.id as string;
    const wordDetail = await teacher.get(`/words/${wordId}`);
    expect(wordDetail.status).toBe(200);
    expect(wordDetail.body.data.usedBy.length).toBe(2);
    expect(wordDetail.body.data.usedBy.map((u: { unitId: string }) => u.unitId).sort()).toEqual([unitId, unit2Id].sort());
    expectShape("word-detail", wordDetail.body);

    // 移除一个关联
    const removeLink = await teacher.del(`/units/${unitId}/words/${wordId}`);
    expect(removeLink.status).toBe(200);
    const listAfter = await teacher.get(`/units/${unitId}/words`);
    expect(listAfter.body.data.items.find((w: { id: string }) => w.id === wordId)).toBeUndefined();
    const wordDetail2 = await teacher.get(`/words/${wordId}`);
    expect(wordDetail2.body.data.usedBy.length).toBe(1);

    // 老师可以编辑：只被自己的词书引用
    const editOk = await teacher.patch(`/words/${wordId}`, { definition: "苹果（改）" });
    expect(editOk.status).toBe(200);
    expect(editOk.body.data.definition).toBe("苹果（改）");

    // 拼写重复冲突（在放进系统词书、失去编辑权之前测）
    await teacher.post(`/units/${unitId}/words`, { spelling: `banana${STAMP}`, definition: "香蕉" });
    const clash = await teacher.patch(`/words/${wordId}`, { spelling: `banana${STAMP}` });
    expect(clash.status).toBe(409);
    expect(clash.body.error).toMatchObject({ code: "DUPLICATE", message: "已存在相同拼写的单词" });

    // 把同一个词也放进系统词书 → 老师不能再编辑
    const sysBook = await admin.post("/books", { name: `syswords-${STAMP}`, isSystem: true });
    const sysUnit = await admin.post(`/books/${sysBook.body.data.id}/units`, { name: "SysU1" });
    const addToSys = await admin.post(`/units/${sysUnit.body.data.id}/words`, { spelling, definition: "苹果（系统）" });
    expect(addToSys.body.data.reused).toBe(true);

    const editBlocked = await teacher.patch(`/words/${wordId}`, { definition: "改不了" });
    expect(editBlocked.status).toBe(403);
    expect(editBlocked.body.error).toMatchObject({ code: "FORBIDDEN", message: "该词被系统词书或他人词书引用，只有管理员可以修改" });

    // 管理员始终可以改
    const adminEdit = await admin.patch(`/words/${wordId}`, { definition: "管理员改" });
    expect(adminEdit.status).toBe(200);
    expect(adminEdit.body.data.definition).toBe("管理员改");

    // 全局搜索
    const globalSearch = await teacher.get(`/words?q=${spelling}`);
    expect(globalSearch.status).toBe(200);
    expect(globalSearch.body.data.items.some((w: { spelling: string }) => w.spelling === spelling)).toBe(true);
    expect(globalSearch.body.data).toMatchObject({ page: 1, limit: 20 });
  });

  it("导入预览：解析、警告、已存在检测；导入：新建词书与已有词书去重", async () => {
    const teacher = await login("teacher@vinx.test");
    const admin = await login("admin@vinx.test");

    // 先建一个词已存在的词书，用于“已存在”检测
    const existingSpelling = `pear${STAMP}`;
    const seedBook = await teacher.post("/books", { name: `preview-seed-${STAMP}` });
    const seedUnit = await teacher.post(`/books/${seedBook.body.data.id}/units`, { name: "S" });
    await teacher.post(`/units/${seedUnit.body.data.id}/words`, { spelling: existingSpelling, definition: "梨" });

    const text = [
      "Unit 1",
      `${existingSpelling} /peər/ n. 梨`,
      `grape${STAMP} n. 葡萄`, // 缺音标 → warning
      `badline${STAMP} no chinese here`, // 无中文释义 → error
    ].join("\n");

    const preview = await teacher.post("/books/import/preview", { text });
    expect(preview.status).toBe(200);
    const data = preview.body.data;
    expect(data.units.length).toBe(1);
    expect(data.units[0].name).toBe("Unit 1");
    const entries = data.units[0].entries as Array<{ spelling: string; status: string; existing: boolean; existingDefinition: string | null }>;
    const pearEntry = entries.find((e) => e.spelling === existingSpelling)!;
    expect(pearEntry.existing).toBe(true);
    expect(pearEntry.existingDefinition).toBe("梨");
    expect(pearEntry.status).toBe("ok");
    const grapeEntry = entries.find((e) => e.spelling === `grape${STAMP}`)!;
    expect(grapeEntry.status).toBe("warning");
    expect(grapeEntry.existing).toBe(false);
    const badEntry = entries.find((e) => e.status === "error");
    expect(badEntry).toBeDefined();
    expect(data.stats).toMatchObject({ units: 1, entries: 3, existing: 1 });
    expectShape("import-preview", preview.body);

    // 校验：空文本
    const emptyPreview = await teacher.post("/books/import/preview", { text: "" });
    expect(emptyPreview.status).toBe(400);
    expect(emptyPreview.body.error.details).toEqual({ text: "请粘贴词表内容" });

    // 正式导入：新建词书
    const newBookName = `imported-${STAMP}`;
    const importBody = {
      newBook: { name: newBookName },
      units: [
        {
          name: "Unit 1",
          entries: [
            { spelling: existingSpelling, definition: "梨" },
            { spelling: `grape${STAMP}`, definition: "葡萄" },
          ],
        },
      ],
    };
    const imported = await teacher.post("/books/import", importBody);
    expect(imported.status).toBe(200);
    expect(imported.body.data).toMatchObject({ units: 1, unitsCreated: 1, wordsCreated: 1, wordsReused: 1, wordsLinked: 2 });
    const newBookId = imported.body.data.bookId as string;
    expectShape("import-result", imported.body);

    const newBookDetail = await teacher.get(`/books/${newBookId}`);
    expect(newBookDetail.body.data.name).toBe(newBookName);
    expect(newBookDetail.body.data.units.length).toBe(1);
    expect(newBookDetail.body.data.units[0].wordCount).toBe(2);

    // 再次导入相同内容到同一词书：单元复用（不新建），词全部复用
    const reimport = await teacher.post("/books/import", { bookId: newBookId, units: importBody.units });
    expect(reimport.status).toBe(200);
    expect(reimport.body.data).toMatchObject({ unitsCreated: 0, wordsCreated: 0, wordsReused: 2 });

    // 校验：既没 bookId 也没 newBook
    const noTarget = await teacher.post("/books/import", { units: importBody.units });
    expect(noTarget.status).toBe(400);
    expect(noTarget.body.error).toMatchObject({ code: "VALIDATION", message: "请选择词书或新建词书" });

    // 校验：units 为空数组
    const emptyUnits = await teacher.post("/books/import", { newBook: { name: `x-${STAMP}` }, units: [] });
    expect(emptyUnits.status).toBe(400);
    expect(emptyUnits.body.error.details).toEqual({ units: "没有可导入的单元" });

    // 学生不能导入
    const student = await login("student@vinx.test");
    const denied = await student.post("/books/import/preview", { text: "x" });
    expect(denied.status).toBe(403);

    // 老师不能把 newBook.isSystem 设为 true；管理员可以
    const teacherSysAttempt = await teacher.post("/books/import", { newBook: { name: `t-sys-${STAMP}`, isSystem: true }, units: importBody.units });
    expect(teacherSysAttempt.status).toBe(200);
    const tBook = await teacher.get(`/books/${teacherSysAttempt.body.data.bookId}`);
    expect(tBook.body.data.isSystem).toBe(false);

    const adminSysImport = await admin.post("/books/import", { newBook: { name: `a-sys-${STAMP}`, isSystem: true }, units: importBody.units });
    const aBook = await admin.get(`/books/${adminSysImport.body.data.bookId}`);
    expect(aBook.body.data.isSystem).toBe(true);

    // I1：units.N.name 用 zod 默认消息，不是自定义中文
    const badUnitNames = await teacher.post("/books/import", {
      newBook: { name: `badnames-${STAMP}` },
      units: [
        { name: "", entries: [{ spelling: "x", definition: "释义" }] },
        { name: "y".repeat(61), entries: [{ spelling: "z", definition: "释义" }] },
      ],
    });
    expect(badUnitNames.status).toBe(400);
    expect(badUnitNames.body.error.details).toEqual({
      "units.0.name": "String must contain at least 1 character(s)",
      "units.1.name": "String must contain at most 60 character(s)",
    });

    // bookId:null → 类型错误；bookId 指向他人（对老师而言是管理员）的词书 → 403
    const nullBookId = await teacher.post("/books/import", { bookId: null, units: importBody.units });
    expect(nullBookId.status).toBe(400);
    expect(nullBookId.body.error.details).toEqual({ bookId: "Expected string, received null" });

    const someoneElseBook = await admin.post("/books", { name: `admin-owned-${STAMP}` });
    const forbiddenImport = await teacher.post("/books/import", { bookId: someoneElseBook.body.data.id, units: importBody.units });
    expect(forbiddenImport.status).toBe(403);
    expect(forbiddenImport.body.error).toMatchObject({ code: "FORBIDDEN", message: "只能编辑自己创建的词书" });

    // splitByInitial + defaultUnit 生效
    const splitText = [`zebra${STAMP} /zi:brə/ n. 斑马`, `apple${STAMP} /ˈæpl/ n. 苹果`].join("\n");
    const splitPreview = await teacher.post("/books/import/preview", { text: splitText, splitByInitial: true, defaultUnit: "自定义分组" });
    expect(splitPreview.status).toBe(200);
    const splitUnitNames = splitPreview.body.data.units.map((u: { name: string }) => u.name).sort();
    expect(splitUnitNames).toEqual(["A", "Z"]); // 按首字母分组，覆盖了 defaultUnit（因为整段都能分到字母）

    const noHeadingText = `single${STAMP} /wʌn/ n. 单个`;
    const defaultUnitPreview = await teacher.post("/books/import/preview", { text: noHeadingText, defaultUnit: "自定义分组" });
    expect(defaultUnitPreview.body.data.units[0].name).toBe("自定义分组");
  });

  it("单元词条 / 全局词条：可见性、权限、分页与校验的失败路径", async () => {
    const teacher = await login("teacher@vinx.test");
    const admin = await login("admin@vinx.test");
    const student = await login("student@vinx.test");

    const book = await teacher.post("/books", { name: `fail-paths-${STAMP}` });
    const bookId = book.body.data.id as string;
    const unit = await teacher.post(`/books/${bookId}/units`, { name: "U1" });
    const unitId = unit.body.data.id as string;

    // 分页：3 个词，limit=2
    for (let i = 0; i < 3; i++) {
      await teacher.post(`/units/${unitId}/words`, { spelling: `page${i}-${STAMP}`, definition: `释义${i}` });
    }
    const page1 = await teacher.get(`/units/${unitId}/words?limit=2&page=1`);
    expect(page1.body.data).toMatchObject({ total: 3, page: 1, limit: 2 });
    expect(page1.body.data.items.length).toBe(2);
    const page2 = await teacher.get(`/units/${unitId}/words?limit=2&page=2`);
    expect(page2.body.data.items.length).toBe(1);

    // limit / page 校验
    const badLimit = await teacher.get(`/units/${unitId}/words?limit=501`);
    expect(badLimit.status).toBe(400);
    expect(badLimit.body.error.details).toEqual({ limit: "Number must be less than or equal to 500" });
    const badPage = await teacher.get(`/units/${unitId}/words?page=0`);
    expect(badPage.status).toBe(400);
    expect(badPage.body.error.details).toEqual({ page: "Number must be greater than or equal to 1" });

    // 班外学生看不到这个单元 → 404
    const outsider = anon();
    await outsider.post("/auth/signup", { email: `outsider2-${STAMP}@vinx.test`, password: "123456" });
    const invisible = await outsider.get(`/units/${unitId}/words`);
    expect(invisible.status).toBe(404);
    expect(invisible.body.error).toMatchObject({ code: "NOT_FOUND", message: "单元不存在" });

    // 学生不能加词；空拼写校验；phonetic:null 被接受
    const studentAdd = await student.post(`/units/${unitId}/words`, { spelling: "x", definition: "y" });
    expect(studentAdd.status).toBe(403);
    const blankSpelling = await teacher.post(`/units/${unitId}/words`, { spelling: "  ", definition: "释义" });
    expect(blankSpelling.status).toBe(400);
    expect(blankSpelling.body.error.details).toEqual({ spelling: "拼写不能为空" });
    const nullPhonetic = await teacher.post(`/units/${unitId}/words`, { spelling: `nullphon-${STAMP}`, definition: "释义", phonetic: null });
    expect(nullPhonetic.status).toBe(200);
    const nullPhonWord = await teacher.get(`/units/${unitId}/words?q=nullphon-${STAMP}`);
    expect(nullPhonWord.body.data.items[0].phonetic).toBeNull();
    const wordId = nullPhonWord.body.data.items[0].id as string;

    // 他人（管理员自己的书）的单元/词：老师删关联 → 403
    const adminBook = await admin.post("/books", { name: `admin-fail-${STAMP}` });
    const adminUnit = await admin.post(`/books/${adminBook.body.data.id}/units`, { name: "AU" });
    const adminWord = await admin.post(`/units/${adminUnit.body.data.id}/words`, { spelling: `adminw-${STAMP}`, definition: "释义" });
    const adminWordId = (await admin.get(`/units/${adminUnit.body.data.id}/words`)).body.data.items[0].id;
    const deleteLinkForbidden = await teacher.del(`/units/${adminUnit.body.data.id}/words/${adminWordId}`);
    expect(deleteLinkForbidden.status).toBe(403);
    expect(adminWord.status).toBe(200);

    // PATCH /words/:id：不存在 → 404；校验失败；可空字段设为 null
    const patchNotFound = await teacher.patch("/words/does-not-exist", { definition: "x" });
    expect(patchNotFound.status).toBe(404);
    expect(patchNotFound.body.error).toMatchObject({ code: "NOT_FOUND", message: "单词不存在" });
    const patchInvalid = await teacher.patch(`/words/${wordId}`, { spelling: "x".repeat(101) });
    expect(patchInvalid.status).toBe(400);
    expect(patchInvalid.body.error.details).toEqual({ spelling: "String must contain at most 100 character(s)" });
    const patchClearPhonetic = await teacher.patch(`/words/${wordId}`, { phonetic: "/x/" });
    expect(patchClearPhonetic.body.data.phonetic).toBe("/x/");
    const patchNullPhonetic = await teacher.patch(`/words/${wordId}`, { phonetic: null });
    expect(patchNullPhonetic.body.data.phonetic).toBeNull();

    // GET /words：缺 q / 空白 q
    const missingQ = await teacher.get("/words");
    expect(missingQ.status).toBe(400);
    expect(missingQ.body.error.details).toEqual({ q: "Required" });
    const blankQ = await teacher.get("/words?q=%20%20");
    expect(blankQ.status).toBe(400);
    expect(blankQ.body.error.details).toEqual({ q: "请输入关键词" });

    // M4：definition 大小写敏感，spelling 大小写（含非 ASCII，如 É/é）不敏感
    const caseSpelling = `CAFÉ${STAMP}`;
    await teacher.post(`/units/${unitId}/words`, { spelling: caseSpelling, definition: `ABC释义${STAMP}` });
    const spellingCi = await teacher.get(`/words?q=${encodeURIComponent(`café${STAMP}`)}`);
    expect(spellingCi.body.data.items.some((w: { spelling: string }) => w.spelling === caseSpelling)).toBe(true);
    const definitionCs = await teacher.get(`/words?q=${encodeURIComponent(`abc释义${STAMP}`)}`);
    expect(definitionCs.body.data.items.length).toBe(0);
    const definitionCsExact = await teacher.get(`/words?q=${encodeURIComponent(`ABC释义${STAMP}`)}`);
    expect(definitionCsExact.body.data.items.length).toBeGreaterThan(0);
  });
});
