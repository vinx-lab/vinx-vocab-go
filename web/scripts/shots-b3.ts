/**
 * B3 截图：计划、词书、导入、单词单（含打印页）。
 * 打印页截图用 page.route 拦截 GET /api/sheets/:id，两版拿到完全相同的编造数据，不往库里写单词单
 * （真实生成会让「合并打印」等场景的编号在两次截图之间漂移）。
 */
import type { Page } from "@playwright/test";
import type { Shot } from "./shots";
import type { SheetDetail, SheetWord } from "../src/types";

async function apiData<T>(page: Page, url: string): Promise<T> {
  const r = await page.request.get(url);
  if (!r.ok()) throw new Error(`${url} ${r.status()}`);
  return (await r.json()).data as T;
}

async function go(page: Page, path: string) {
  await page.goto(path);
  await page.waitForLoadState("networkidle");
}

const WORDS: SheetWord[] = [
  { wordId: "w1", spelling: "dinosaur", phonetic: "/ˈdaɪnəsɔː(r)/", partOfSpeech: "n.", definition: "恐龙" },
  { wordId: "w2", spelling: "museum", phonetic: "/mjuˈziːəm/", partOfSpeech: "n.", definition: "博物馆" },
  { wordId: "w3", spelling: "weather", phonetic: "/ˈweðə(r)/", partOfSpeech: "n.", definition: "天气" },
  { wordId: "w4", spelling: "be good at", phonetic: null, partOfSpeech: null, definition: "擅长……" },
  { wordId: "w5", spelling: "mountain", phonetic: "/ˈmaʊntɪn/", partOfSpeech: "n.", definition: "山" },
  { wordId: "w6", spelling: "beautiful", phonetic: "/ˈbjuːtɪfl/", partOfSpeech: "adj.", definition: "美丽的" },
  { wordId: "w7", spelling: "government", phonetic: "/ˈɡʌvənmənt/", partOfSpeech: "n.", definition: "政府" },
  { wordId: "w8", spelling: "communicate", phonetic: "/kəˈmjuːnɪkeɪt/", partOfSpeech: "v.", definition: "交流；沟通" },
];

/** 编造一份单词单详情（不落库） */
function fakeSheet(id: string, seq: number, name: string, words: SheetWord[]): SheetDetail {
  return { id, seq, createdAt: new Date("2026-09-20T02:00:00.000Z").toISOString(), student: { id: "fake-student", name }, modes: ["recognition", "spelling"], words };
}

async function mockSheet(page: Page, sheet: SheetDetail) {
  await page.route(`**/api/sheets/${sheet.id}`, (route) => route.fulfill({ json: { success: true, data: sheet, timestamp: new Date().toISOString() } }));
}

export const B3_SHOTS: Shot[] = [
  { name: "plans", title: "学习计划列表", path: "/plans", account: "teacher" },
  {
    name: "plan-detail",
    title: "计划详情",
    path: "/plans",
    account: "teacher",
    before: async (page) => {
      // 挑创建时间最早的（种子数据），比最新一条更不容易受同一后端上其他并行任务新增数据的影响
      const plans = await apiData<{ items: { id: string; createdAt: string }[] }>(page, "/api/plans?scope=created&page=1&limit=100");
      const p = [...plans.items].sort((a, b) => a.createdAt.localeCompare(b.createdAt))[0];
      if (!p) throw new Error("教师没有自建的计划");
      await go(page, `/plans/${p.id}`);
      await page.getByText("内容").waitFor();
    },
  },
  { name: "plan-new", title: "新建计划", path: "/plans/new", account: "teacher" },
  { name: "books", title: "词书列表", path: "/books", account: "teacher" },
  {
    name: "book-detail",
    title: "词书详情",
    path: "/books",
    account: "teacher",
    before: async (page) => {
      const books = await apiData<{ items: { id: string; name: string }[] }>(page, "/api/books?page=1&limit=20");
      const b = books.items.find((x) => x.name.includes("七年级上册")) ?? books.items[0];
      if (!b) throw new Error("没有词书");
      await go(page, `/books/${b.id}`);
      // 手机端没有「Units」这行（侧栏是下拉），等单词区的单元标题出现，两种布局都有
      await page.locator("h2.vx-word").first().waitFor();
    },
  },
  { name: "import", title: "导入词表（第一步）", path: "/books/import", account: "teacher" },
  {
    name: "sheets",
    title: "单词单列表",
    path: "/sheets",
    account: "student",
    before: async (page) => {
      await page.route("**/api/sheets?*", (route) => {
        if (route.request().method() !== "GET") return route.continue();
        const items = [
          { id: "fakesheet2", seq: 2, wordCount: 5, createdAt: "2026-09-21T02:00:00.000Z", creatorName: "本人", status: "pending", firstResult: null, activeSessionId: null },
          { id: "fakesheet1", seq: 1, wordCount: 5, createdAt: "2026-09-20T02:00:00.000Z", creatorName: "本人", status: "tested", firstResult: { sessionId: "fakesession1", correct: 4, total: 5 }, activeSessionId: null },
        ];
        return route.fulfill({ json: { success: true, data: { items, total: items.length, page: 1, limit: 50 }, timestamp: new Date().toISOString() } });
      });
      await go(page, "/sheets");
      await page.getByText("单词单 #2").waitFor();
    },
  },
  { name: "sheet-new", title: "生成单词单", path: "/sheets/new", account: "student" },
  {
    name: "sheet-print",
    title: "单词单打印页（A4 对折自测表）",
    path: "/today",
    account: "student",
    before: async (page) => {
      const sheet = fakeSheet("fakeprint1", 3, "截图同学", WORDS);
      await mockSheet(page, sheet);
      await page.addInitScript(() => {
        window.print = () => {};
      });
      await go(page, "/sheets/fakeprint1/print");
      await page.getByText(/VinxVocab 单词单 #3/).waitFor();
    },
  },
  {
    name: "sheet-print-merged",
    title: "单词单合并打印页（2 份）",
    path: "/today",
    account: "student",
    before: async (page) => {
      const s1 = fakeSheet("fakeprint2", 1, "截图同学", WORDS.slice(0, 4));
      const s2 = fakeSheet("fakeprint3", 2, "截图同学", WORDS.slice(4));
      await mockSheet(page, s1);
      await mockSheet(page, s2);
      await page.addInitScript(() => {
        window.print = () => {};
      });
      await go(page, "/sheets/print?ids=fakeprint2,fakeprint3");
      await page.getByText(/VinxVocab 单词单 #2/).waitFor();
    },
  },
];
