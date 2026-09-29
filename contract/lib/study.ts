/**
 * 学习流契约用例共用的助手（learning.spec / dont-know.spec）。
 */
import { expect } from "vitest";
import { anon, STAMP, type Client } from "./client";

export interface Item {
  wordId: string;
  spelling: string;
  definition: string;
  answer: string;
  options: string[];
  modes?: string[];
  cloze: string | null;
}

let seq = 0;
/** 注册一个新学生（学校版无邀请码也可注册，不入班） */
export async function newStudent(tag: string): Promise<{ client: Client; id: string }> {
  const c = anon();
  const res = await c.post("/auth/signup", { email: `${tag}-${STAMP}-${seq++}@vinx.test`, password: "dev123456", name: `学生${tag}` });
  if (res.status !== 201) throw new Error(`注册失败 ${res.status} ${res.text}`);
  return { client: c, id: res.body.data.user.id };
}

/** seed 系统词书「八年级上册」第一个单元（> 12 个词） */
export async function systemUnitId(c: Client): Promise<string> {
  const books = await c.get("/books");
  const book = (books.body.data.items ?? books.body.data).find((b: { name: string; isSystem: boolean }) => b.name === "八年级上册" && b.isSystem);
  if (!book) throw new Error("找不到 seed 系统词书「八年级上册」");
  const detail = await c.get(`/books/${book.id}`);
  return detail.body.data.units[0].id;
}

/** 按快照作答：wrong(wordId, mode) 为真时答错 */
export async function answerAll(c: Client, sessionId: string, phase: "practice" | "test", wrong: (w: string, m: string) => boolean = () => false) {
  const s = await c.get(`/study/sessions/${sessionId}`);
  expect(s.status).toBe(200);
  const items = s.body.data.items as Item[];
  for (const item of items) {
    for (const mode of item.modes ?? (s.body.data.modes as string[])) {
      const bad = wrong(item.wordId, mode);
      const answer = mode === "recognition" ? (bad ? "__wrong__" : item.definition) : bad ? "zzz" : item.answer;
      const r = await c.post(`/study/sessions/${sessionId}/answers`, { wordId: item.wordId, mode, phase, attempt: 1, answer, durationMs: 3000 });
      expect(r.status).toBe(200);
    }
  }
  return items;
}

