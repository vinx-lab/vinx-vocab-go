/**
 * B4 截图：班级（列表 / 详情三个 tab / 学生详情）、用户管理、系统设置。
 * 「版本」卡片是 Go 版新增（oracle 没有对应页面/接口），旧版截图里不会出现，属预期差异。
 */
import type { Page } from "@playwright/test";
import type { Shot } from "./shots";

async function apiData<T>(page: Page, url: string): Promise<T> {
  const r = await page.request.get(url);
  if (!r.ok()) throw new Error(`${url} ${r.status()}`);
  return (await r.json()).data as T;
}

async function go(page: Page, path: string) {
  await page.goto(path);
  await page.waitForLoadState("networkidle");
}

async function demoClassId(page: Page): Promise<string> {
  const classes = await apiData<{ items: { id: string; name: string }[] }>(page, "/api/classes");
  const c = classes.items.find((x) => x.name.includes("DEMO")) ?? classes.items[0];
  if (!c) throw new Error("教师没有班级（先在两版共用的后端上跑一次 db:setup / seed-demo）");
  return c.id;
}

export const B4_SHOTS: Shot[] = [
  { name: "classes", title: "班级列表", path: "/classes", account: "teacher" },
  {
    name: "class-overview",
    title: "班级详情：今日概览",
    path: "/classes",
    account: "teacher",
    before: async (page) => {
      const id = await demoClassId(page);
      await go(page, `/classes/${id}`);
      await page.getByText("学生今日进度").waitFor();
    },
  },
  {
    name: "class-members",
    title: "班级详情：成员",
    path: "/classes",
    account: "teacher",
    before: async (page) => {
      const id = await demoClassId(page);
      await go(page, `/classes/${id}?tab=members`);
      await page.getByText("邀请码", { exact: true }).waitFor();
    },
  },
  {
    name: "class-plans",
    title: "班级详情：学习计划",
    path: "/classes",
    account: "teacher",
    before: async (page) => {
      const id = await demoClassId(page);
      await go(page, `/classes/${id}?tab=plans`);
    },
  },
  {
    name: "student-detail",
    title: "老师查看学生详情",
    path: "/classes",
    account: "teacher",
    before: async (page) => {
      const id = await demoClassId(page);
      const detail = await apiData<{ members: { id: string; name: string }[] }>(page, `/api/classes/${id}`);
      const student = detail.members[0];
      if (!student) throw new Error("班级没有学生");
      await go(page, `/classes/${id}/students/${student.id}`);
      await page.getByRole("tab", { name: "学习概况" }).waitFor();
    },
  },
  { name: "users", title: "用户管理", path: "/admin/users", account: "admin" },
  { name: "settings", title: "系统设置", path: "/admin/settings", account: "admin" },
];
