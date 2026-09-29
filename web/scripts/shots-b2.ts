/**
 * B2 截图：今日（见 shots.ts 的 layout-student）、学习、记录、短文。
 * 学习页截图不往库里写作答：学习组快照用 page.route 改写、作答接口直接拦截，两版拿到完全相同的数据。
 */
import type { Page } from "@playwright/test";
import type { Shot } from "./shots";

async function apiData<T>(page: Page, url: string): Promise<T> {
  const r = await page.request.get(url);
  if (!r.ok()) throw new Error(`${url} ${r.status()}`);
  return (await r.json()).data as T;
}

/** 学生演示账号的种子计划开一组（已有进行中的组会续做，两版拿到同一组） */
async function seedSessionId(page: Page): Promise<string> {
  const today = await apiData<{ plans: { planId: string; name: string; kind: string }[] }>(page, "/api/today");
  const plan = today.plans.find((p) => p.name.includes("每日背词")) ?? today.plans.find((p) => p.kind === "daily");
  if (!plan) throw new Error("学生没有每日计划");
  const r = await page.request.post("/api/study/sessions", { data: { kind: "learn", planId: plan.planId } });
  if (!r.ok()) throw new Error(`开组失败 ${r.status()} ${await r.text()}`);
  return (await r.json()).data.id as string;
}

type Snap = Record<string, any>;

/** 打开学习页，快照按 patch 改写（只改浏览器拿到的数据，不影响库） */
async function openStudy(page: Page, patch: (d: Snap) => void): Promise<Snap> {
  const id = await seedSessionId(page);
  let snap: Snap = {};
  await page.route(`**/api/study/sessions/${id}`, async (route) => {
    if (route.request().method() !== "GET") return route.continue();
    const res = await route.fetch();
    const body = await res.json();
    patch(body.data);
    snap = body.data;
    await route.fulfill({ response: res, json: body });
  });
  await page.goto(`/study/${id}`);
  await page.waitForLoadState("networkidle");
  return snap;
}

/** 跳过卡片、清空作答、只出指定题型 */
const practiceOnly = (modes: string[]) => (d: Snap) => {
  d.answers = [];
  d.modes = modes;
  d.items = d.items.map((i: Snap) => ({ ...i, modes: null }));
  d.progress = { ...(d.progress ?? {}), cardsDone: true };
};

/** 作答接口拦截为「答错」（不入库） */
async function fakeWrongAnswer(page: Page, expected: () => string) {
  await page.route("**/api/study/sessions/*/answers", (route) =>
    route.fulfill({ json: { success: true, data: { recorded: true, correct: false, expected: expected() }, timestamp: new Date().toISOString() } }),
  );
}

/** 最近一组已完成的新学组（结果页、学习组详情用） */
async function completedSessionId(page: Page): Promise<string> {
  const list = await apiData<{ items: { id: string; status: string }[] }>(page, "/api/records/sessions?page=1&limit=50&kind=learn");
  const s = list.items.find((x) => x.status === "completed");
  if (!s) throw new Error("没有已完成的学习组");
  return s.id;
}

async function go(page: Page, path: string) {
  await page.goto(path);
  await page.waitForLoadState("networkidle");
}

/**
 * 今日页用专门的固定账号（演示学生的数据会被其他测试不停追加计划，两版截图之间就会变）。
 * 账号不存在时注册；外观保持「跟随系统」，随截图上下文的配色走。
 */
async function useFixedAccount(page: Page, email: string, name: string) {
  const login = await page.request.post("/api/auth/login", { data: { email, password: "dev123456" } });
  if (!login.ok()) {
    const r = await page.request.post("/api/auth/signup", { data: { name, email, password: "dev123456" } });
    if (!r.ok()) throw new Error(`注册 ${email} 失败 ${r.status()}`);
  }
}

/** 截图账号的自建计划（七年级上册 Unit 1），没有就建一个 */
async function ensurePlan(page: Page) {
  const today = await apiData<{ plans: unknown[] }>(page, "/api/today");
  if (today.plans.length) return;
  const books = await apiData<{ items: { id: string; unitCount: number }[] }>(page, "/api/books?limit=50");
  for (const b of books.items.filter((x) => x.unitCount > 0)) {
    const unit = (await apiData<{ units: { id: string; name: string }[] }>(page, `/api/books/${b.id}`)).units.find((u) => u.name === "Unit 1");
    if (!unit) continue;
    const r = await page.request.post("/api/plans", { data: { name: "B2 截图计划", unitIds: [unit.id] } });
    if (!r.ok()) throw new Error(`建计划失败 ${r.status()}`);
    return;
  }
  throw new Error("找不到 Unit 1");
}

export const B2_SHOTS: Shot[] = [
  {
    name: "today",
    title: "今日（固定截图账号，有一个自建计划）",
    path: "/login",
    before: async (page) => {
      await useFixedAccount(page, "b2-shots@vinx.test", "截图同学");
      await ensurePlan(page);
      await go(page, "/today");
      await page.getByRole("button", { name: /学新词/ }).waitFor();
    },
  },
  {
    name: "today-empty",
    title: "今日：没有计划时的空状态（固定截图账号）",
    path: "/login",
    before: async (page) => {
      await useFixedAccount(page, "b2-shots-empty@vinx.test", "空白同学");
      await go(page, "/today");
      await page.getByText("今天没有学习任务").waitFor();
    },
  },
  { name: "records", title: "学习记录", path: "/records", account: "student" },
  {
    name: "records-session",
    title: "学习组详情",
    path: "/today",
    account: "student",
    before: async (page) => go(page, `/records/sessions/${await completedSessionId(page)}`),
  },
  { name: "words", title: "我的单词", path: "/words", account: "student" },
  {
    name: "word-history",
    title: "单词轨迹",
    path: "/today",
    account: "student",
    before: async (page) => {
      // 记忆库可能是空的（演示库里学生多半没有结算入库的词），取最近一组已完成学习组里的第一个词
      const w = await apiData<{ items: { wordId: string }[] }>(page, "/api/records/words?filter=all&page=1&limit=30");
      const s = await apiData<{ words: { wordId: string }[] }>(page, `/api/records/sessions/${await completedSessionId(page)}`);
      const wordId = w.items[0]?.wordId ?? s.words[0]?.wordId;
      if (!wordId) throw new Error("找不到单词");
      await go(page, `/words/${wordId}`);
    },
  },
  { name: "passages", title: "短文巩固（AI 未配置时）", path: "/passages", account: "student" },
  {
    name: "passage-detail",
    title: "短文阅读",
    path: "/today",
    account: "student",
    before: async (page) => {
      const list = await apiData<{ items: { id: string }[] }>(page, "/api/passages?page=1&limit=20");
      if (!list.items[0]) throw new Error("没有短文");
      await go(page, `/passages/${list.items[0].id}`);
    },
  },
  {
    name: "study-cards",
    title: "学习：认识新词卡片",
    path: "/today",
    account: "student",
    before: async (page) => {
      await openStudy(page, (d) => {
        d.answers = [];
        d.progress = { ...(d.progress ?? {}), cardsDone: false };
      });
      await page.getByRole("button", { name: /下一个|开始练习/ }).waitFor();
    },
  },
  {
    name: "study-recognition",
    title: "学习：认义题",
    path: "/today",
    account: "student",
    before: async (page) => {
      await openStudy(page, practiceOnly(["recognition"]));
      await page.getByText("选出正确的中文意思").waitFor();
    },
  },
  {
    name: "study-recognition-wrong",
    title: "学习：认义题点「不会」（作答接口被拦截，不入库）",
    path: "/today",
    account: "student",
    before: async (page) => {
      let expected = "";
      await fakeWrongAnswer(page, () => expected);
      const snap = await openStudy(page, practiceOnly(["recognition"]));
      await page.getByText("选出正确的中文意思").waitFor();
      const text = await page.locator("main").innerText();
      const items = snap.items as { spelling: string; definition: string; options: string[] }[];
      expected = items.find((i) => text.includes(i.spelling) && i.options.every((o) => text.includes(o)))?.definition ?? "";
      await page.getByRole("button", { name: /^不\s*会$/ }).click();
      await page.getByText("记一下正确答案").waitFor();
    },
  },
  {
    name: "study-spelling",
    title: "学习：拼写题（用过提示）",
    path: "/today",
    account: "student",
    before: async (page) => {
      await openStudy(page, practiceOnly(["spelling"]));
      await page.getByText(/拼写英文/).waitFor();
      await page.getByRole("button", { name: /提\s*示/ }).click();
      await page.getByLabel("拼写答案").fill("abc");
    },
  },
  {
    name: "study-spelling-wrong",
    title: "学习：拼写答错（作答接口被拦截，不入库）",
    path: "/today",
    account: "student",
    before: async (page) => {
      await fakeWrongAnswer(page, () => "");
      await openStudy(page, practiceOnly(["spelling"]));
      await page.getByText(/拼写英文/).waitFor();
      await page.getByLabel("拼写答案").fill("wrongword");
      await page.keyboard.press("Enter");
      await page.getByText("记一下正确答案").waitFor();
    },
  },
  {
    name: "study-exit-confirm",
    title: "学习：退出确认框",
    path: "/today",
    account: "student",
    fullPage: false,
    before: async (page) => {
      await openStudy(page, practiceOnly(["recognition"]));
      await page.getByText("选出正确的中文意思").waitFor();
      await page.getByRole("button", { name: "结束本组" }).click();
      await page.getByRole("dialog").waitFor();
    },
  },
  {
    name: "study-result",
    title: "学习：结果页（已完成的组）",
    path: "/today",
    account: "student",
    before: async (page) => {
      await go(page, `/study/${await completedSessionId(page)}`);
      await page.getByRole("button", { name: /返回今日/ }).waitFor();
    },
  },
];
