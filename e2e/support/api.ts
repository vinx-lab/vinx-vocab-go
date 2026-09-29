import type { APIRequestContext, Page } from "@playwright/test";

/**
 * 新增用例（study-keys、study-exit-confirm）用：通过 API 快速建计划，把篇幅留给要验证的学习页交互。
 */
export async function cookieHeader(page: Page) {
  return (await page.context().cookies()).map((c) => `${c.name}=${c.value}`).join("; ");
}

/** 与旧用例在计划编辑页点第一本词书的「Unit 1」相同：取七年级上册 Unit 1 建计划（默认题型：认义 + 拼写） */
export async function createPlanViaApi(
  request: APIRequestContext,
  cookie: string,
  body: { name: string; targets?: { classIds: string[]; userIds: string[] } },
): Promise<string> {
  const books = (await (await request.get("/api/books?limit=50", { headers: { cookie } })).json()).data.items as { id: string; unitCount: number }[];
  let unitId: string | undefined;
  for (const b of books.filter((x) => x.unitCount > 0)) {
    const units = (await (await request.get(`/api/books/${b.id}`, { headers: { cookie } })).json()).data.units as { id: string; name: string }[];
    unitId = units.find((u) => u.name === "Unit 1")?.id;
    if (unitId) break;
  }
  if (!unitId) throw new Error("找不到 Unit 1");
  const res = await request.post("/api/plans", { headers: { cookie }, data: { name: body.name, unitIds: [unitId], ...(body.targets ? { targets: body.targets } : {}) } });
  if (!res.ok()) throw new Error(`建计划失败 ${res.status()} ${await res.text()}`);
  return (await res.json()).data.id as string;
}
