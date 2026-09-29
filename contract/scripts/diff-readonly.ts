/**
 * 迁移演练：只读接口差分比对（oracle 旧后端 vs 导入了同一份数据的 Go 实例）。
 *
 * 前提：Go 数据目录由 `vinx-vocab import --from-postgres <oracle 库>` 从 **同一时刻** 的 oracle 数据导入。
 * oracle 在被其他测试持续写入时，先把 oracle 库复制一份快照（pg_dump → 新库）、另起一个 oracle 进程指向快照，
 * 再从快照导入 Go，两边数据才完全相同（见任务报告）。
 *
 * 做法：用管理员账号从 oracle 取全部账号；对每个账号分别在两边签发令牌（HS256，载荷与旧版登录签发的相同），
 * 按角色 GET 全部只读接口（列表 → 逐个详情），同一请求同时发给两边，比较完整 JSON（忽略 timestamp、requestId；
 * /config 忽略 Go 新增的 editionLocked / needsSetup）。另抽样用原密码登录，验证导入后的密码哈希可用。
 *
 * 运行（Node ≥ 22.18，直接执行 TypeScript）：
 *   ORACLE_URL=http://localhost:3241 GO_URL=http://localhost:3242/api \
 *   ORACLE_JWT_SECRET=<oracle 的 JWT_SECRET> GO_JWT_SECRET=<Go 数据目录 secret.key 的 jwt_secret> \
 *   [OUT=diff-report.json] [CONCURRENCY=8] [USER_LIMIT=0] [USER_FILTER=<邮箱正则>] [ONLY=<端点模板正则>] \
 *   node contract/scripts/diff-readonly.ts
 *
 * 退出码：0 无差异；1 有差异；2 运行出错。密钥只从环境变量读，不写任何文件。
 */
import { createHmac } from "node:crypto";
import { writeFileSync } from "node:fs";

const ORACLE_URL = need("ORACLE_URL").replace(/\/$/, "");
const GO_URL = need("GO_URL").replace(/\/$/, "");
const ORACLE_SECRET = need("ORACLE_JWT_SECRET");
const GO_SECRET = need("GO_JWT_SECRET");
const OUT = process.env.OUT ?? "";
const CONCURRENCY = Number(process.env.CONCURRENCY ?? 8);
const USER_LIMIT = Number(process.env.USER_LIMIT ?? 0);
const LOGIN_SAMPLE = Number(process.env.LOGIN_SAMPLE ?? 30);
/** 只比较模板匹配该正则的端点（其余只向 oracle 取数用于发现 id）；复查某类差异时用 */
const ONLY = process.env.ONLY ? new RegExp(process.env.ONLY) : null;

function need(name: string): string {
  const v = process.env[name];
  if (!v) {
    console.error(`缺少环境变量 ${name}`);
    process.exit(2);
  }
  return v;
}

// ---------- JWT ----------
const b64url = (b: Buffer | string) => Buffer.from(b).toString("base64url");
function sign(secret: string, u: User): string {
  const iat = Math.floor(Date.now() / 1000);
  const head = b64url(JSON.stringify({ alg: "HS256", typ: "JWT" }));
  const body = b64url(JSON.stringify({ sub: u.id, email: u.email, name: u.name, role: u.role, iat, exp: iat + 3600 }));
  const sig = createHmac("sha256", secret).update(`${head}.${body}`).digest("base64url");
  return `${head}.${body}.${sig}`;
}

interface User {
  id: string;
  email: string;
  name: string;
  role: "student" | "teacher" | "admin";
}

// ---------- HTTP ----------
interface Resp {
  status: number;
  body: any;
}
async function get(base: string, path: string, token?: string): Promise<Resp> {
  const headers: Record<string, string> = {};
  if (token) headers.cookie = `vinx_token=${token}`;
  for (let i = 0; ; i++) {
    try {
      const res = await fetch(base + path, { headers, redirect: "manual", signal: AbortSignal.timeout(60_000) });
      const text = await res.text();
      let body: any = text;
      try {
        body = text ? JSON.parse(text) : undefined;
      } catch {
        /* 非 JSON 原样 */
      }
      return { status: res.status, body };
    } catch (e) {
      if (i >= 2) throw new Error(`${base}${path}: ${(e as Error).message}`);
    }
  }
}
async function post(base: string, path: string, body: unknown): Promise<Resp> {
  const res = await fetch(base + path, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify(body),
    signal: AbortSignal.timeout(60_000),
  });
  return { status: res.status, body: await res.json().catch(() => undefined) };
}

// ---------- 比较 ----------
const IGNORE_KEYS = new Set(["timestamp", "requestId"]);
const GO_ONLY_CONFIG_KEYS = new Set(["editionLocked", "needsSetup"]);

function diff(a: any, b: any, path: string, out: string[], template: string) {
  if (out.length >= 20) return;
  if (a === b) return;
  const ta = a === null ? "null" : Array.isArray(a) ? "array" : typeof a;
  const tb = b === null ? "null" : Array.isArray(b) ? "array" : typeof b;
  if (ta !== tb) {
    out.push(`${path}: oracle=${short(a)} go=${short(b)}`);
    return;
  }
  if (ta === "array") {
    if (a.length !== b.length) out.push(`${path}: 数组长度 oracle=${a.length} go=${b.length}`);
    for (let i = 0; i < Math.min(a.length, b.length); i++) diff(a[i], b[i], `${path}[${i}]`, out, template);
    return;
  }
  if (ta === "object") {
    const keys = new Set([...Object.keys(a), ...Object.keys(b)]);
    for (const k of keys) {
      if (IGNORE_KEYS.has(k)) continue;
      if (template === "/config" && GO_ONLY_CONFIG_KEYS.has(k)) continue;
      if (!(k in a)) out.push(`${path}.${k}: oracle 缺省, go=${short(b[k])}`);
      else if (!(k in b)) out.push(`${path}.${k}: go 缺省, oracle=${short(a[k])}`);
      else diff(a[k], b[k], `${path}.${k}`, out, template);
    }
    return;
  }
  out.push(`${path}: oracle=${short(a)} go=${short(b)}`);
}
function short(v: unknown) {
  const s = JSON.stringify(v);
  return s === undefined ? "undefined" : s.length > 160 ? s.slice(0, 160) + "…" : s;
}

/** 把具体路径归并成模板，用于分组统计 */
function templateOf(path: string): string {
  const [p, q] = path.split("?");
  const t = p
    .split("/")
    .map((seg) => (/^c[a-z0-9]{20,}$/.test(seg) || /^no-such/.test(seg) ? ":id" : seg))
    .join("/");
  const keys = q ? "?" + [...new URLSearchParams(q).keys()].sort().join("&") : "";
  return t + keys;
}

interface Diff {
  user: string;
  path: string;
  status: [number, number];
  details: string[];
}
const stats = new Map<string, { requests: number; diffs: number }>();
const diffs: Diff[] = [];
let requests = 0;

/** 同一请求同时发给两边并比较；返回 oracle 的响应（用来发现后续要访问的 id） */
async function both(u: User, tokens: { o: string; g: string }, path: string): Promise<Resp> {
  if (ONLY && !ONLY.test(templateOf(path))) return get(ORACLE_URL, path, tokens.o);
  const [o, g] = await Promise.all([get(ORACLE_URL, path, tokens.o), get(GO_URL, path, tokens.g)]);
  const tpl = templateOf(path);
  const st = stats.get(tpl) ?? { requests: 0, diffs: 0 };
  st.requests++;
  requests++;
  const details: string[] = [];
  if (o.status !== g.status) details.push(`status: oracle=${o.status} go=${g.status}`);
  diff(o.body, g.body, "$", details, tpl);
  if (details.length) {
    st.diffs++;
    diffs.push({ user: `${u.email} (${u.role})`, path, status: [o.status, g.status], details });
  }
  stats.set(tpl, st);
  return o;
}

const data = (r: Resp) => (r.status === 200 && r.body?.success ? r.body.data : undefined);

/** 分页列表：逐页取完（两边每页都比较） */
async function pages(u: User, t: { o: string; g: string }, base: string, limit: number): Promise<any[]> {
  const items: any[] = [];
  const sep = base.includes("?") ? "&" : "?";
  for (let page = 1; page <= 200; page++) {
    const d = data(await both(u, t, `${base}${sep}page=${page}&limit=${limit}`));
    if (!d?.items) break;
    items.push(...d.items);
    if (items.length >= d.total || d.items.length === 0) break;
  }
  return items;
}

async function pool<T>(xs: T[], n: number, fn: (x: T) => Promise<void>) {
  let i = 0;
  await Promise.all(
    Array.from({ length: Math.min(n, xs.length) }, async () => {
      while (i < xs.length) await fn(xs[i++]);
    }),
  );
}

// ---------- 每个账号的只读遍历 ----------
const seenUnitsForAll = new Set<string>();

async function crawl(u: User, all: User[]) {
  const t = { o: sign(ORACLE_SECRET, u), g: sign(GO_SECRET, u) };
  const staff = u.role !== "student";
  await both(u, t, "/auth/me");
  await both(u, t, "/config");
  await both(u, t, "/ai/status");
  await both(u, t, "/me/classes");

  // 今日与进行中的学习组
  const today = data(await both(u, t, "/today"));
  for (const p of today?.plans ?? []) for (const s of p.activeSessions ?? []) await both(u, t, `/study/sessions/${s.id}`);
  for (const s of today?.activeSessions ?? []) if (s?.id) await both(u, t, `/study/sessions/${s.id}`);

  // 学习记录（本人）
  await records(u, t, "");

  // 计划
  const planIds = new Set<string>();
  for (const q of ["", "?scope=mine", "?scope=created", "?status=active", "?status=paused", "?status=archived"]) {
    const d = data(await both(u, t, `/plans${q}`));
    for (const p of d?.items ?? []) planIds.add(p.id);
  }
  for (const id of planIds) {
    await both(u, t, `/plans/${id}`);
    await both(u, t, `/plans/${id}/progress`);
  }

  // 词书、单元、单词
  const books = data(await both(u, t, "/books"))?.items ?? [];
  for (const b of books) {
    const bd = data(await both(u, t, `/books/${b.id}`));
    // 单元词表：管理员 / 演示账号看全部；其他账号只看非系统词书（系统词书对所有人内容相同）
    const full = u.role === "admin" || u.email.endsWith("@vinx.test") && !/\d/.test(u.email);
    if (!full && b.isSystem) continue;
    for (const unit of bd?.units ?? []) {
      if (!full && seenUnitsForAll.has(`${u.id}:${unit.id}`)) continue;
      seenUnitsForAll.add(`${u.id}:${unit.id}`);
      await pages(u, t, `/units/${unit.id}/words`, 500);
      await both(u, t, `/units/${unit.id}/words?q=a`);
    }
  }
  for (const q of ["a", "the", "Unit", "学", "xyz-none"]) await both(u, t, `/words?q=${encodeURIComponent(q)}`);

  // 单词单与来源
  const sheetOwners = [""];
  // 巩固短文
  const passages = await pages(u, t, "/passages", 100);
  for (const p of passages) await both(u, t, `/passages/${p.id}`);

  // 老师 / 管理员：班级、学生的记录与单词单、用户列表
  const students = new Set<string>();
  if (staff) {
    const list: any[] = data(await both(u, t, "/classes"))?.items ?? [];
    for (const c of list) {
      const cd = data(await both(u, t, `/classes/${c.id}`));
      await both(u, t, `/classes/${c.id}/overview`);
      for (const m of cd?.members ?? []) if (m?.id) students.add(m.id);
    }
    await pages(u, t, "/users", 500);
    await both(u, t, `/users?q=${encodeURIComponent("student")}`);
    for (const sid of students) {
      await records(u, t, sid);
      sheetOwners.push(sid);
    }
  }
  for (const owner of sheetOwners) {
    const q = owner ? `?userId=${owner}` : "";
    await both(u, t, `/sheets/sources${q}`);
    const sheets = await pages(u, t, `/sheets${q}`, 100);
    for (const s of sheets) await both(u, t, `/sheets/${s.id}`);
  }
  if (u.role === "admin") {
    await both(u, t, "/settings/ai");
    await both(u, t, "/settings/ai/prompts");
    // 所有单词详情
    const words = new Set<string>();
    for (const b of books) {
      const bd = data(await get(ORACLE_URL, `/books/${b.id}`, t.o));
      for (const unit of bd?.units ?? []) {
        const w = data(await get(ORACLE_URL, `/units/${unit.id}/words?limit=500`, t.o));
        for (const it of w?.items ?? []) words.add(it.id ?? it.wordId);
      }
    }
    await pool([...words], 4, async (id) => void (await both(u, t, `/words/${id}`)));
    // 看别人的记录（管理员不限范围）
    for (const other of all.filter((x) => x.role === "student").slice(0, 40)) await records(u, t, other.id, true);
  }
  // 不存在的资源
  for (const p of ["/plans/no-such-id", "/books/no-such-id", "/records/sessions/no-such-id", "/sheets/no-such-id", "/words/no-such-id", "/study/sessions/no-such-id", "/passages/no-such-id"]) await both(u, t, p);
}

async function records(u: User, t: { o: string; g: string }, userId: string, light = false) {
  const q = userId ? `userId=${userId}` : "";
  const qs = (s: string) => (q ? (s ? `?${s}&${q}` : `?${q}`) : s ? `?${s}` : "");
  await both(u, t, `/records/summary${qs("")}`);
  await both(u, t, `/records/daily${qs("")}`);
  if (light) return;
  await both(u, t, `/records/daily${qs("days=180")}`);
  const sessions = await pages(u, t, `/records/sessions${qs("")}`, 100);
  for (const k of ["learn", "review", "test", "drill", "sheet"]) await both(u, t, `/records/sessions${qs(`kind=${k}`)}`);
  for (const s of sessions) await both(u, t, `/records/sessions/${s.id}${qs("")}`);
  const wordIds = new Set<string>();
  for (const f of ["all", "due", "difficult", "mastered", "consolidating", "learning"]) {
    const items = await pages(u, t, `/records/words${qs(`filter=${f}`)}`, 100);
    if (f === "all") for (const w of items) wordIds.add(w.wordId ?? w.id);
  }
  await both(u, t, `/records/words${qs("q=a")}`);
  for (const id of wordIds) await both(u, t, `/records/words/${id}${qs("")}`);
}

// ---------- 原密码登录抽样 ----------
async function loginCheck(users: User[]) {
  const candidates = ["dev123456", "123456", "abc123", "newpass1", "abc12345", "654321", "wrong-password"];
  const sample = [
    ...users.filter((u) => /^(admin|teacher|student|student2)@vinx\.test$/.test(u.email)),
    ...users.filter((u) => !/^(admin|teacher|student|student2)@vinx\.test$/.test(u.email)).filter((_, i, a) => i % Math.max(1, Math.floor(a.length / LOGIN_SAMPLE)) === 0).slice(0, LOGIN_SAMPLE),
  ];
  let ok = 0;
  const bad: string[] = [];
  for (const u of sample) {
    let matched = "";
    for (const pwd of candidates) {
      const [o, g] = await Promise.all([post(ORACLE_URL, "/auth/login", { email: u.email, password: pwd }), post(GO_URL, "/auth/login", { email: u.email, password: pwd })]);
      if (o.status !== g.status) bad.push(`${u.email} 密码 #${candidates.indexOf(pwd)}: oracle=${o.status} go=${g.status}`);
      if (o.status === 200) {
        matched = pwd;
        break;
      }
    }
    if (matched) ok++;
  }
  return { sampled: sample.length, loggedInWithKnownPassword: ok, mismatches: bad };
}

// ---------- 主流程 ----------
async function main() {
  const t0 = Date.now();
  const adminStub: User = { id: "", email: "admin@vinx.test", name: "", role: "admin" };
  // 用管理员登录 oracle 取账号列表（同时验证管理员原密码可用）
  const lo = await post(ORACLE_URL, "/auth/login", { email: "admin@vinx.test", password: "dev123456" });
  if (lo.status !== 200) throw new Error(`oracle 管理员登录失败 ${lo.status}`);
  const admin: User = lo.body.data.user;
  Object.assign(adminStub, admin);
  const tok = sign(ORACLE_SECRET, admin);
  const all: User[] = [];
  for (let page = 1; ; page++) {
    const r = await get(ORACLE_URL, `/users?page=${page}&limit=500`, tok);
    const d = r.body?.data;
    if (!d?.items) throw new Error(`取账号列表失败 ${r.status}`);
    all.push(...d.items.map((x: any) => ({ id: x.id, email: x.email, name: x.name, role: x.role })));
    if (all.length >= d.total || d.items.length === 0) break;
  }
  // 演示账号优先，其余按 id
  all.sort((a, b) => Number(!a.email.endsWith("@vinx.test") || /\d/.test(a.email)) - Number(!b.email.endsWith("@vinx.test") || /\d/.test(b.email)) || (a.id < b.id ? -1 : 1));
  const filtered = process.env.USER_FILTER ? all.filter((u) => new RegExp(process.env.USER_FILTER!).test(u.email)) : all;
  const users = USER_LIMIT > 0 ? filtered.slice(0, USER_LIMIT) : filtered;
  console.log(`账号 ${all.length} 个，遍历 ${users.length} 个（并发 ${CONCURRENCY}）`);

  let done = 0;
  await pool(users, CONCURRENCY, async (u) => {
    try {
      await crawl(u, all);
    } catch (e) {
      diffs.push({ user: u.email, path: "(crawl)", status: [0, 0], details: [String((e as Error).message)] });
    }
    if (++done % 50 === 0) console.log(`  ${done}/${users.length}，请求 ${requests}，差异 ${diffs.length}`);
  });

  const login = ONLY ? { sampled: 0, loggedInWithKnownPassword: 0, mismatches: [] as string[] } : await loginCheck(all);
  const byTemplate = [...stats.entries()].sort((a, b) => a[0].localeCompare(b[0])).map(([template, s]) => ({ template, ...s }));
  const report = { oracle: ORACLE_URL, go: GO_URL, users: users.length, requests, differences: diffs.length, seconds: Math.round((Date.now() - t0) / 1000), byTemplate, login, diffs };
  if (OUT) writeFileSync(OUT, JSON.stringify(report, null, 2));

  console.log(`\n端点模板 ${byTemplate.length} 个，请求 ${requests} 对，差异 ${diffs.length}，用时 ${report.seconds}s`);
  for (const t of byTemplate) console.log(`  ${t.diffs ? "✗" : "✓"} ${t.template.padEnd(44)} ${String(t.requests).padStart(6)}${t.diffs ? `  差异 ${t.diffs}` : ""}`);
  console.log(`\n原密码登录抽样：${login.sampled} 个账号，${login.loggedInWithKnownPassword} 个用已知密码登录成功；两边状态不一致 ${login.mismatches.length}`);
  for (const m of login.mismatches.slice(0, 20)) console.log("  " + m);
  for (const d of diffs.slice(0, 30)) {
    console.log(`\n✗ ${d.path}  [${d.user}]  status ${d.status.join(" / ")}`);
    for (const x of d.details.slice(0, 8)) console.log("    " + x);
  }
  process.exit(diffs.length || login.mismatches.length ? 1 : 0);
}

main().catch((e) => {
  console.error(e);
  process.exit(2);
});
