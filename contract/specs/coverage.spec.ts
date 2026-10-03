/**
 * 目标词书与覆盖进度（spec 0003，只在 Go 上跑）：班级目标词书的设置与权限、我的目标词书、
 * /records/coverage 与 /records/coverage/words、班级概览的 coverage、两种正式测试后数字的变化、
 * 单词单的「目标：未测 / 要学」来源。
 *
 * 正式测试作答（spec 0003 §1）用两条路径各做一次：
 * - 老师出的默写单批改（spec 0006，attempt = 1，不需要线上作答，结果确定）；
 * - 学生从「目标：未测的词」出单词单并在线测完（检测的首次作答）。
 * 每个用例组用 STAMP 建自己的老师、班级、学生与词书，不依赖其他用例。
 */
import { beforeAll, describe, expect, it } from "vitest";
import { login, type Client } from "../lib/client";
import { goOnly } from "../lib/areas";
import { importBook, newClassWithStudent, newStudent, newTeacher, uniq, type Account, type BuiltBook } from "../lib/fixtures";

interface Counts {
  target: number;
  tested: number;
  known: number;
  learning: number;
  untested: number;
}

describe.runIf(goOnly)("目标词书与覆盖进度", () => {
  let admin: Client;
  let teacher: Account, other: Account, student: Account, loner: Account;
  let classId = "";
  let book: BuiltBook, privateBook: BuiltBook;
  const sp: string[] = [];

  async function coverage(c: Client, userId?: string) {
    const r = await c.get(`/records/coverage${userId ? `?userId=${userId}` : ""}`);
    expect(r.status, r.text).toBe(200);
    return r.body.data as { source: string; total: Counts; books: (Counts & { bookId: string; name: string })[] };
  }

  async function overviewCoverage() {
    const r = await teacher.client.get(`/classes/${classId}/overview`);
    expect(r.status).toBe(200);
    const row = (r.body.data.students as { userId: string; coverage: unknown }[]).find((s) => s.userId === student.id);
    expect(row).toBeDefined();
    return row!.coverage as { target: number; tested: number; learning: number; selfGraded: number; selfGradedRatio: number | null } | null;
  }

  async function wordsOf(c: Client, query: string) {
    const r = await c.get(`/records/coverage/words${query}`);
    expect(r.status, r.text).toBe(200);
    return (r.body.data.items as { wordId: string; status: string }[]).map((w) => `${w.wordId}:${w.status}`);
  }

  beforeAll(async () => {
    admin = await login("admin@vinx.test");
    teacher = await newTeacher(admin, "cov");
    other = await newTeacher(admin, "cov2");
    const cls = await newClassWithStudent(teacher.client, "覆盖");
    classId = cls.classId;
    student = cls.student;
    loner = await newStudent("cov-loner");
    const tag = uniq();
    for (const s of ["alpha", "bravo", "charlie", "delta"]) sp.push(`vxcov${s}${tag}`);
    book = await importBook(teacher.client, "覆盖词书", [{ name: "Unit 1", entries: sp.map((s, i) => ({ spelling: s, definition: `覆盖义${i}` })) }]);
    privateBook = await importBook(other.client, "别人的词书", [{ name: "Unit 1", entries: [{ spelling: `vxcovecho${tag}`, definition: "私有" }] }]);
  });

  it("班级目标词书：本班老师与管理员可看可改；别班老师、学生 403；不可见的词书 400；没有目标时 coverage 为 null", async () => {
    const empty = await teacher.client.get(`/classes/${classId}/target-books`);
    expect(empty.status).toBe(200);
    expect(empty.body.data.items).toEqual([]);
    expect(await overviewCoverage()).toBeNull();

    const invisible = await teacher.client.put(`/classes/${classId}/target-books`, { bookIds: [privateBook.bookId] });
    expect(invisible.status).toBe(400);
    expect(invisible.body.error).toMatchObject({ code: "VALIDATION", message: "有词书不存在或不可见" });
    const missing = await teacher.client.put(`/classes/${classId}/target-books`, {});
    expect(missing.status).toBe(400);
    expect(missing.body.error.details).toEqual({ bookIds: "Required" });

    expect((await other.client.get(`/classes/${classId}/target-books`)).status).toBe(403);
    expect((await other.client.put(`/classes/${classId}/target-books`, { bookIds: [] })).status).toBe(403);
    expect((await student.client.put(`/classes/${classId}/target-books`, { bookIds: [book.bookId] })).status).toBe(403);
    expect((await teacher.client.get(`/classes/no-such-class/target-books`)).status).toBe(404);

    // 重复的 id 去重，顺序即 sortOrder
    const set = await teacher.client.put(`/classes/${classId}/target-books`, { bookIds: [book.bookId, book.bookId] });
    expect(set.status).toBe(200);
    expect(set.body.data.items.map((b: { id: string }) => b.id)).toEqual([book.bookId]);
    expect((await admin.get(`/classes/${classId}/target-books`)).body.data.items.map((b: { id: string }) => b.id)).toEqual([book.bookId]);
  });

  it("我的目标词书：班级学生只读班级目标（自己保存了也仍是 class）；没有班级的学生自己设", async () => {
    const mine = await student.client.get("/me/target-books");
    expect(mine.status).toBe(200);
    expect(mine.body.data.source).toBe("class");
    expect(mine.body.data.books.map((b: { id: string }) => b.id)).toEqual([book.bookId]);
    const saved = await student.client.put("/me/target-books", { bookIds: [] });
    expect(saved.status).toBe(200);
    expect(saved.body.data.source).toBe("class");

    expect((await loner.client.get("/me/target-books")).body.data).toMatchObject({ source: "none", books: [] });
    // 看不到别的老师的私有词书
    expect((await loner.client.put("/me/target-books", { bookIds: [privateBook.bookId] })).status).toBe(400);
    // 系统词书可以
    const books = await loner.client.get("/books");
    const sys = books.body.data.items.find((b: { isSystem: boolean }) => b.isSystem);
    const own = await loner.client.put("/me/target-books", { bookIds: [sys.id] });
    expect(own.status).toBe(200);
    expect(own.body.data.source).toBe("own");
    expect(own.body.data.books.map((b: { id: string }) => b.id)).toEqual([sys.id]);
    const cov = await coverage(loner.client);
    expect(cov.source).toBe("own");
    expect(cov.total.target).toBeGreaterThan(0);
    expect(cov.total.untested).toBe(cov.total.target);
  });

  it("覆盖进度：初始全部未测；本人与本班老师看到同样的数字；别班老师、其他学生 403", async () => {
    const mine = await coverage(student.client);
    expect(mine.source).toBe("class");
    expect(mine.total).toEqual({ target: 4, tested: 0, known: 0, learning: 0, untested: 4 });
    expect(mine.books).toHaveLength(1);
    expect(mine.books[0]).toMatchObject({ bookId: book.bookId, target: 4, tested: 0, known: 0, learning: 0, untested: 4 });
    expect(await coverage(teacher.client, student.id)).toEqual(mine);
    expect(await overviewCoverage()).toEqual({ target: 4, tested: 0, learning: 0, selfGraded: 0, selfGradedRatio: null });

    expect((await other.client.get(`/records/coverage?userId=${student.id}`)).status).toBe(403);
    expect((await other.client.get(`/records/coverage/words?userId=${student.id}`)).status).toBe(403);
    expect((await loner.client.get(`/records/coverage?userId=${student.id}`)).status).toBe(403);

    expect((await wordsOf(student.client, "?status=untested")).sort()).toEqual(sp.map((s) => `${book.wordId(s)}:untested`).sort());
    expect((await student.client.get("/records/coverage/words?status=bad")).status).toBe(400);
    expect((await student.client.get(`/records/coverage/words?bookId=${privateBook.bookId}`)).status).toBe(404);
  });

  it("老师批改默写单（正式测试）后：会了 / 要学 / 未测变化，概览一致，不是自批", async () => {
    const created = await teacher.client.post("/sheets", {
      userId: student.id,
      format: "dictation",
      items: [
        { type: "word", wordId: book.wordId(sp[0]) },
        { type: "word", wordId: book.wordId(sp[1]) },
      ],
    });
    expect(created.status, created.text).toBe(200);
    const sheetId = created.body.data.id as string;
    const detail = await teacher.client.get(`/sheets/${sheetId}`);
    const items = detail.body.data.items as { index: number; wordId: string }[];
    const graded = await teacher.client.post(`/sheets/${sheetId}/grade`, {
      results: items.map((it) => ({ index: it.index, correct: it.wordId === book.wordId(sp[0]) })),
    });
    expect(graded.status, graded.text).toBe(200);
    expect(graded.body.data.grading.selfGraded).toBe(false);

    const cov = await coverage(student.client);
    expect(cov.total).toEqual({ target: 4, tested: 2, known: 1, learning: 1, untested: 2 });
    expect(await coverage(teacher.client, student.id)).toEqual(cov);
    expect(await overviewCoverage()).toEqual({ target: 4, tested: 2, learning: 1, selfGraded: 0, selfGradedRatio: 0 });
    expect(await wordsOf(teacher.client, `?userId=${student.id}&status=learning`)).toEqual([`${book.wordId(sp[1])}:learning`]);
    expect(await wordsOf(student.client, `?bookId=${book.bookId}&status=known`)).toEqual([`${book.wordId(sp[0])}:known`]);

    // 「目标：要学的词」出单词单
    const learn = await student.client.post("/sheets/preview", { count: 10, source: { kind: "target", status: "learning" } });
    expect(learn.status, learn.text).toBe(200);
    expect(learn.body.data.items.map((i: { wordId: string }) => i.wordId)).toEqual([book.wordId(sp[1])]);
    const badStatus = await student.client.post("/sheets/preview", { count: 10, source: { kind: "target", status: "known" } });
    expect(badStatus.status).toBe(400);
  });

  it("从「目标：未测的词」出单词单并在线测完：未测归零，答错的词变成要学", async () => {
    const preview = await student.client.post("/sheets/preview", { count: 10, source: { kind: "target", status: "untested" } });
    expect(preview.status, preview.text).toBe(200);
    const ids = (preview.body.data.items as { wordId: string }[]).map((i) => i.wordId);
    expect(ids.sort()).toEqual([book.wordId(sp[2]), book.wordId(sp[3])].sort());
    // 只出某本书的词：不在目标里的书 404
    const onlyBook = await student.client.post("/sheets/preview", { count: 10, source: { kind: "target", status: "untested", bookId: book.bookId } });
    expect(onlyBook.status).toBe(200);
    expect(onlyBook.body.data.items).toHaveLength(2);

    const created = await student.client.post("/sheets", { wordIds: ids, modes: ["recognition"] });
    expect(created.status, created.text).toBe(200);
    const sheetId = created.body.data.id as string;
    const start = await student.client.post("/study/sessions", { kind: "sheet", sheetId });
    expect(start.status, start.text).toBe(200);
    const sessionId = start.body.data.id as string;
    const s = await student.client.get(`/study/sessions/${sessionId}`);
    const wrongId = book.wordId(sp[3]);
    for (const item of s.body.data.items as { wordId: string }[]) {
      const w = (await student.client.get(`/records/words/${item.wordId}`)).body.data.word as { definition: string };
      const r = await student.client.post(`/study/sessions/${sessionId}/answers`, {
        wordId: item.wordId,
        mode: "recognition",
        phase: "test",
        attempt: 1,
        answer: item.wordId === wrongId ? "__wrong__" : w.definition,
      });
      expect(r.status).toBe(200);
    }
    expect((await student.client.post(`/study/sessions/${sessionId}/complete`)).status).toBe(200);

    const cov = await coverage(student.client);
    expect(cov.total).toEqual({ target: 4, tested: 4, known: 2, learning: 2, untested: 0 });
    expect(await coverage(teacher.client, student.id)).toEqual(cov);
    // 概览：sp[0]、sp[1] 由老师批改，sp[2]、sp[3] 是学生自己在线测的（不算自批，自批只指默写单的学生批改）
    expect(await overviewCoverage()).toMatchObject({ target: 4, tested: 4, learning: 2 });
    expect((await wordsOf(student.client, "?status=learning")).sort()).toEqual([`${book.wordId(sp[1])}:learning`, `${wrongId}:learning`].sort());
  });
});
