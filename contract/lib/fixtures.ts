/**
 * Go 专属用例（spec 0002–0006）共用的建数据助手：老师、班级与凭邀请码入班的学生、
 * 用 /books/import 一次建好的词书（单元、词条、可选的句型 / 课文）。
 * 拼写全局唯一（K16）：调用方用 `uniq()` 生成的后缀造词，避免与 seed 或其他用例的词撞名。
 */
import { expect } from "vitest";
import { anon, login, STAMP, type Client } from "./client";

let seq = 0;

/** 每次调用都不同的小写字母后缀（拼写只用字母，避免分词把数字切开） */
export function uniq(): string {
  const n = `${STAMP}${seq++}`;
  return [...n].map((d) => "abcdefghij"[Number(d)]).join("");
}

export interface Account {
  client: Client;
  id: string;
  email: string;
}

/** 管理员建一个老师账号并登录 */
export async function newTeacher(admin: Client, tag: string): Promise<Account> {
  const email = `gt-${STAMP}-${seq++}@vinx.test`;
  const r = await admin.post("/users", { email, name: `老师${tag}`, password: "dev123456", role: "teacher" });
  expect(r.status).toBe(200);
  return { client: await login(email), id: r.body.data.id as string, email };
}

/** 自行注册的学生；给了邀请码就入班 */
export async function newStudent(tag: string, inviteCode?: string): Promise<Account> {
  const c = anon();
  const email = `gs-${STAMP}-${seq++}@vinx.test`;
  const r = await c.post("/auth/signup", { email, password: "dev123456", name: `学生${tag}`, ...(inviteCode ? { inviteCode } : {}) });
  expect(r.status, r.text).toBe(201);
  return { client: c, id: r.body.data.user.id as string, email };
}

/** 老师建一个班，并让一个新学生凭邀请码入班 */
export async function newClassWithStudent(teacher: Client, tag: string): Promise<{ classId: string; inviteCode: string; student: Account }> {
  const cls = await teacher.post("/classes", { name: `${tag} 班 ${STAMP}-${seq++}` });
  expect(cls.status).toBe(200);
  const student = await newStudent(tag, cls.body.data.inviteCode);
  return { classId: cls.body.data.id as string, inviteCode: cls.body.data.inviteCode as string, student };
}

export interface EntryIn {
  spelling: string;
  definition: string;
  partOfSpeech?: string;
  type?: "word" | "phrase";
}

export interface SentenceIn {
  en: string;
  cn: string;
  frame?: string;
  paragraph?: number;
}

export interface TextIn {
  title: string;
  titleCn?: string;
  kind: "text" | "list";
  sentences: SentenceIn[];
}

export interface UnitIn {
  name: string;
  entries: EntryIn[];
  texts?: TextIn[];
}

export interface BuiltBook {
  bookId: string;
  units: { id: string; name: string; words: { id: string; spelling: string; definition: string }[] }[];
  /** 拼写 → 词 id */
  wordId: (spelling: string) => string;
}

/** 用 /books/import 新建一本词书（单元、词条、可选的篇），返回各单元的词 id */
export async function importBook(owner: Client, name: string, units: UnitIn[]): Promise<BuiltBook> {
  const r = await owner.post("/books/import", { newBook: { name: `${name} ${STAMP}-${seq++}` }, units });
  expect(r.status, r.text).toBe(200);
  const bookId = r.body.data.bookId as string;
  const detail = await owner.get(`/books/${bookId}`);
  expect(detail.status).toBe(200);
  const out: BuiltBook["units"] = [];
  const ids = new Map<string, string>();
  for (const u of detail.body.data.units as { id: string; name: string }[]) {
    const list = await owner.get(`/units/${u.id}/words?limit=100`);
    expect(list.status).toBe(200);
    const words = (list.body.data.items as { id: string; spelling: string; definition: string }[]).map((w) => ({ id: w.id, spelling: w.spelling, definition: w.definition }));
    for (const w of words) ids.set(w.spelling, w.id);
    out.push({ id: u.id, name: u.name, words });
  }
  return {
    bookId,
    units: out,
    wordId: (s) => {
      const id = ids.get(s);
      if (!id) throw new Error(`词书里没有 ${s}`);
      return id;
    },
  };
}

/** 轮询后台任务直到结束（AI 生成），返回任务 */
export async function waitJob(c: Client, jobId: string, timeoutMs = 20_000) {
  const t0 = Date.now();
  for (;;) {
    const r = await c.get(`/ai/jobs/${jobId}`);
    expect(r.status).toBe(200);
    if (r.body.data.status !== "running") return r.body.data;
    if (Date.now() - t0 > timeoutMs) throw new Error(`任务 ${jobId} 超时`);
    await new Promise((res) => setTimeout(res, 50));
  }
}
