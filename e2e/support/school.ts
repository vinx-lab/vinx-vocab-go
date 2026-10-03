import { expect, request as playwrightRequest, type APIRequestContext } from "@playwright/test";
import { PASSWORD, uid } from "./helpers";

/**
 * spec 0003–0006 的新增用例用：通过 API 准备与被测流程无关的数据（新老师、班级、凭邀请码入班的学生、
 * 用 /books/import 建的词书），把页面操作留给要验证的流程。每个账号一个独立的 APIRequestContext（各自的 Cookie）。
 */
export const BASE = process.env.E2E_BASE_URL ?? "http://localhost:3250";

export async function apiOk<T = unknown>(res: Awaited<ReturnType<APIRequestContext["get"]>>): Promise<T> {
  const text = await res.text();
  expect(res.ok(), `${res.status()} ${text}`).toBeTruthy();
  return (text ? JSON.parse(text).data : undefined) as T;
}

export interface ApiAccount {
  api: APIRequestContext;
  id: string;
  email: string;
  name: string;
}

export async function loginApi(email: string): Promise<APIRequestContext> {
  const api = await playwrightRequest.newContext({ baseURL: BASE });
  await apiOk(await api.post("/api/auth/login", { data: { email, password: PASSWORD } }));
  return api;
}

/** 管理员建一个新老师 */
export async function newTeacher(name: string): Promise<ApiAccount> {
  const admin = await loginApi("admin@vinx.test");
  const email = `e2e-t-${uid()}@vinx.test`;
  const u = await apiOk<{ id: string }>(await admin.post("/api/users", { data: { email, name, password: PASSWORD, role: "teacher" } }));
  await admin.dispose();
  return { api: await loginApi(email), id: u.id, email, name };
}

/** 老师建班，一个新学生凭邀请码注册入班 */
export async function newClassWithStudent(teacher: ApiAccount, className: string, studentName: string): Promise<{ classId: string; student: ApiAccount }> {
  const cls = await apiOk<{ id: string; inviteCode: string }>(await teacher.api.post("/api/classes", { data: { name: `${className} ${uid()}` } }));
  const api = await playwrightRequest.newContext({ baseURL: BASE });
  const email = `e2e-s-${uid()}@vinx.test`;
  const r = await api.post("/api/auth/signup", { data: { email, password: PASSWORD, name: studentName, inviteCode: cls.inviteCode } });
  expect(r.status(), await r.text()).toBe(201);
  const id = (await r.json()).data.user.id as string;
  return { classId: cls.id, student: { api, id, email, name: studentName } };
}

export interface BookUnitIn {
  name: string;
  entries: { spelling: string; definition: string; partOfSpeech?: string; type?: "word" | "phrase" }[];
  texts?: { title: string; titleCn?: string; kind: "text" | "list"; sentences: { en: string; cn: string; frame?: string; paragraph?: number }[] }[];
}

/** 用 /books/import 建一本词书，返回 bookId 与各单元 id */
export async function importBook(owner: APIRequestContext, name: string, units: BookUnitIn[]): Promise<{ bookId: string; name: string; unitIds: string[] }> {
  const fullName = `${name} ${uid()}`;
  const res = await apiOk<{ bookId: string }>(await owner.post("/api/books/import", { data: { newBook: { name: fullName }, units } }));
  const detail = await apiOk<{ units: { id: string }[] }>(await owner.get(`/api/books/${res.bookId}`));
  return { bookId: res.bookId, name: fullName, unitIds: detail.units.map((u) => u.id) };
}

/** 词书里各词的 id（按拼写） */
export async function wordIds(api: APIRequestContext, unitId: string): Promise<Record<string, string>> {
  const list = await apiOk<{ items: { id: string; spelling: string }[] }>(await api.get(`/api/units/${unitId}/words?limit=100`));
  return Object.fromEntries(list.items.map((w) => [w.spelling, w.id]));
}

/** 只用字母的唯一后缀（拼写里不放数字） */
export function letters(): string {
  return [...uid()].map((d) => "abcdefghij"[Number(d)]).join("");
}
