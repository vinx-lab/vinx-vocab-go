/**
 * 新旧两版逐页截图对比。
 *
 *   npx tsx scripts/shots.ts                 # 全部页面、两版、手机+电脑、亮+暗
 *   npx tsx scripts/shots.ts --only login,profile --versions new
 *
 * 输出 docs/screens/<name>-{old,new}-{mobile,desktop}-{light,dark}.png 和 docs/screens/index.html（左右并排）。
 * 两版连同一个后端（同一份数据），外观通过 PUT /auth/profile 设成要截的主题，截完恢复「跟随系统」。
 * 以后的任务在 SHOTS 数组里追加页面即可。
 */
import { chromium, type Browser, type Page } from "@playwright/test";
import { existsSync, mkdirSync, readFileSync, readdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { B2_SHOTS } from "./shots-b2";
import { B3_SHOTS } from "./shots-b3";
import { B4_SHOTS } from "./shots-b4";

const OUT = join(import.meta.dirname, "../../docs/screens");
const VERSIONS = {
  old: process.env.OLD_URL ?? "http://localhost:5175",
  new: process.env.NEW_URL ?? "http://localhost:5174",
} as const;
const VIEWPORTS = {
  mobile: { width: 390, height: 844, isMobile: true, hasTouch: true },
  desktop: { width: 1280, height: 800, isMobile: false, hasTouch: false },
} as const;
const THEMES = ["light", "dark"] as const;
const PASSWORD = "dev123456";

type Version = keyof typeof VERSIONS;
type Viewport = keyof typeof VIEWPORTS;
type Theme = (typeof THEMES)[number];

export interface Shot {
  /** 文件名前缀，也是对比页里的标题 */
  name: string;
  /** 中文说明 */
  title: string;
  path: string;
  /** 登录账号（不填 = 不登录） */
  account?: "student" | "student2" | "teacher" | "admin";
  /** 只截某种视口 */
  only?: Viewport;
  /** 截图前的操作（打开菜单、点提交等） */
  before?: (page: Page, ctx: { viewport: Viewport; version: Version }) => Promise<void>;
  /** 整页截图（默认 true） */
  fullPage?: boolean;
}

export const SHOTS: Shot[] = [
  { name: "login", title: "登录页", path: "/login" },
  {
    name: "login-errors",
    title: "登录页：空提交的校验提示",
    path: "/login",
    before: async (page) => {
      await page.getByRole("button", { name: /登\s*录/ }).click();
      await page.getByText("请输入邮箱或学号").waitFor();
    },
  },
  {
    name: "signup",
    title: "注册表单",
    path: "/login",
    before: async (page) => {
      await page.getByText("注册", { exact: true }).click();
      await page.getByLabel("姓名").waitFor();
    },
  },
  { name: "layout-student", title: "布局（学生，今日）", path: "/today", account: "student" },
  { name: "layout-admin", title: "布局（管理员，完整菜单）", path: "/profile", account: "admin", only: "desktop" },
  {
    name: "user-menu",
    title: "右上角用户菜单",
    path: "/profile",
    account: "student",
    fullPage: false,
    before: async (page) => {
      await page.getByRole("banner").getByRole("button").last().click();
      await page.getByRole("menuitem", { name: /退出登录/ }).waitFor();
    },
  },
  {
    name: "mobile-drawer",
    title: "手机侧滑菜单",
    path: "/profile",
    account: "teacher",
    only: "mobile",
    fullPage: false,
    before: async (page) => {
      await page.getByRole("button", { name: "打开菜单" }).click();
      await page.getByRole("dialog").getByRole("menuitem").first().waitFor();
    },
  },
  { name: "profile", title: "个人中心（学生）", path: "/profile", account: "student" },
  { name: "profile-teacher", title: "个人中心（教师，无「我的班级」）", path: "/profile", account: "teacher" },
  {
    name: "profile-errors",
    title: "个人中心：修改密码校验",
    path: "/profile",
    account: "student",
    before: async (page) => {
      await page.getByLabel("新密码", { exact: true }).fill("123");
      await page.getByLabel("确认新密码").fill("456");
      await page.getByRole("button", { name: "修改密码" }).click();
      await page.getByText("两次输入不一致").waitFor();
    },
  },
  {
    name: "profile-leave-confirm",
    title: "个人中心：退出班级确认气泡",
    path: "/profile",
    account: "student",
    fullPage: false,
    only: "desktop",
    before: async (page) => {
      await page.getByRole("button", { name: /退\s*出/ }).first().click();
      await page.getByText("退出后将不再收到该班级的计划").waitFor();
    },
  },
  // B2：今日、学习、记录、短文（定义在 shots-b2.ts）
  ...B2_SHOTS,
  // B3：计划、词书、导入、单词单（含打印页，定义在 shots-b3.ts）
  ...B3_SHOTS,
  // B4：班级、用户管理、系统设置（定义在 shots-b4.ts）
  ...B4_SHOTS,
];

function parseArgs() {
  const a = process.argv.slice(2);
  const val = (k: string) => {
    const i = a.indexOf(k);
    return i >= 0 ? a[i + 1]?.split(",") : undefined;
  };
  return { only: val("--only"), versions: (val("--versions") ?? ["old", "new"]) as Version[], viewports: (val("--viewports") ?? ["mobile", "desktop"]) as Viewport[], themes: (val("--themes") ?? [...THEMES]) as Theme[] };
}

async function shoot(browser: Browser, shot: Shot, version: Version, viewport: Viewport, theme: Theme) {
  const base = VERSIONS[version];
  const ctx = await browser.newContext({ baseURL: base, viewport: VIEWPORTS[viewport], isMobile: VIEWPORTS[viewport].isMobile, hasTouch: VIEWPORTS[viewport].hasTouch, colorScheme: theme, reducedMotion: "reduce", locale: "zh-CN" });
  const page = await ctx.newPage();
  try {
    if (shot.account) {
      const r = await page.request.post(`${base}/api/auth/login`, { data: { email: `${shot.account}@vinx.test`, password: PASSWORD } });
      if (!r.ok()) throw new Error(`登录失败 ${shot.account}: ${r.status()}`);
      // 外观跟账号走：设成要截的主题
      await page.request.put(`${base}/api/auth/profile`, { data: { theme } });
    }
    await page.goto(shot.path);
    await page.waitForLoadState("networkidle");
    await page.waitForTimeout(300);
    if (shot.before) await shot.before(page, { viewport, version });
    await page.waitForTimeout(400);
    const file = join(OUT, `${shot.name}-${version}-${viewport}-${theme}.png`);
    await page.screenshot({ path: file, fullPage: shot.fullPage ?? true, animations: "disabled" });
    if (shot.account) await page.request.put(`${base}/api/auth/profile`, { data: { theme: "system" } });
    return null;
  } catch (e) {
    return `${shot.name} ${version} ${viewport} ${theme}: ${(e as Error).message.split("\n")[0]}`;
  } finally {
    await ctx.close();
  }
}

const DIFF_JS = `async function (a, b) {
  const load = (d) => new Promise((ok) => { const i = new Image(); i.onload = () => ok(i); i.src = "data:image/png;base64," + d; });
  const [x, y] = await Promise.all([load(a), load(b)]);
  const w = Math.min(x.width, y.width), h = Math.min(x.height, y.height);
  const px = (img) => { const c = document.createElement("canvas"); c.width = w; c.height = h; const g = c.getContext("2d"); g.drawImage(img, 0, 0); return g.getImageData(0, 0, w, h).data; };
  const p = px(x), q = px(y);
  let n = 0;
  for (let i = 0; i < p.length; i += 4) if (Math.abs(p[i] - q[i]) > 24 || Math.abs(p[i + 1] - q[i + 1]) > 24 || Math.abs(p[i + 2] - q[i + 2]) > 24) n++;
  const pct = ((n / (w * h)) * 100).toFixed(2) + "%";
  return x.width !== y.width || x.height !== y.height ? pct + "（尺寸 " + x.width + "×" + x.height + " / " + y.width + "×" + y.height + "）" : pct;
}`;

/** 用浏览器 canvas 比较两张图：返回差异像素占比（任一通道差 > 24 算不同；尺寸不同时按重叠区域比，并标注） */
async function diffAll(browser: Browser): Promise<Record<string, string>> {
  const page = await browser.newPage();
  const out: Record<string, string> = {};
  for (const f of readdirSync(OUT).filter((x) => x.includes("-old-") && x.endsWith(".png"))) {
    const g = f.replace("-old-", "-new-");
    if (!existsSync(join(OUT, g))) continue;
    const a = readFileSync(join(OUT, f)).toString("base64");
    const b = readFileSync(join(OUT, g)).toString("base64");
    // 以字符串传入浏览器执行（tsx 编译会给函数加 __name 辅助，浏览器里没有）
    out[f.replace("-old-", "-")] = await page.evaluate(`(${DIFF_JS})(${JSON.stringify(a)}, ${JSON.stringify(b)})`);
  }
  await page.close();
  return out;
}

function writeIndex(diffs: Record<string, string> = {}) {
  const files = new Set(readdirSync(OUT).filter((f) => f.endsWith(".png")));
  const cell = (f: string) => (files.has(f) ? `<a href="${f}"><img loading="lazy" src="${f}" alt="${f}"></a>` : `<div class="missing">无截图</div>`);
  const sections = SHOTS.map((s) => {
    const rows = (Object.keys(VIEWPORTS) as Viewport[])
      .filter((v) => !s.only || s.only === v)
      .flatMap((v) =>
        THEMES.map(
          (t) => `<div class="row ${v}"><div class="label">${v === "mobile" ? "手机 390×844" : "电脑 1280×800"} · ${t === "light" ? "浅色" : "深色"}${diffs[`${s.name}-${v}-${t}.png`] ? ` · 差异像素 ${diffs[`${s.name}-${v}-${t}.png`]}` : ""}</div>
<div class="pair"><figure><figcaption>旧版</figcaption>${cell(`${s.name}-old-${v}-${t}.png`)}</figure><figure><figcaption>新版</figcaption>${cell(`${s.name}-new-${v}-${t}.png`)}</figure></div></div>`,
        ),
      )
      .join("\n");
    return `<section id="${s.name}"><h2>${s.title} <code>${s.path}</code>${s.account ? ` <span class="acc">${s.account}</span>` : ""}</h2>${rows}</section>`;
  }).join("\n");
  const nav = SHOTS.map((s) => `<a href="#${s.name}">${s.title}</a>`).join("");
  const html = `<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>新旧版截图对比</title>
<style>
:root{--bg:#f7f4ec;--fg:#1d2740;--muted:#6b7280;--line:#ddd5c3;--card:#fffdf8}
@media (prefers-color-scheme:dark){:root{--bg:#141a26;--fg:#ece6d8;--muted:#9aa0ab;--line:#3d4659;--card:#1b2230}}
*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--fg);font:14px/1.6 -apple-system,"PingFang SC","Microsoft YaHei",sans-serif}
header{position:sticky;top:0;z-index:2;background:var(--bg);border-bottom:1px solid var(--line);padding:10px 16px}
header h1{margin:0 0 6px;font-size:18px}nav{display:flex;flex-wrap:wrap;gap:6px 14px}nav a{color:inherit;font-size:13px}
main{padding:16px;max-width:1800px;margin:0 auto}section{margin-bottom:40px}h2{font-size:16px;border-left:4px solid #0f6e66;padding-left:8px}
code{font-size:12px;color:var(--muted)}.acc{font-size:12px;color:var(--muted);border:1px solid var(--line);border-radius:4px;padding:0 4px}
.row{margin:12px 0}.label{color:var(--muted);font-size:13px;margin-bottom:4px}.pair{display:grid;grid-template-columns:1fr 1fr;gap:12px}
figure{margin:0;background:var(--card);border:1px solid var(--line);border-radius:8px;padding:8px;min-width:0}figcaption{font-size:12px;color:var(--muted);margin-bottom:4px}
img{display:block;max-width:100%;height:auto;margin:0 auto}.mobile img{max-width:390px;width:100%}.missing{color:var(--muted);padding:40px;text-align:center}
</style></head><body>
<header><h1>新旧版截图对比（左旧右新）</h1><nav>${nav}</nav></header>
<main>${sections}</main>
<footer style="padding:16px;color:var(--muted);font-size:12px;text-align:center">生成于 ${new Date().toLocaleString("zh-CN", { hour12: false })}，由 web/scripts/shots.ts 生成</footer>
</body></html>`;
  writeFileSync(join(OUT, "index.html"), html);
}

async function main() {
  const args = parseArgs();
  mkdirSync(OUT, { recursive: true });
  const browser = await chromium.launch();
  const errors: string[] = [];
  let n = 0;
  for (const shot of SHOTS) {
    if (args.only && !args.only.includes(shot.name)) continue;
    for (const version of args.versions)
      for (const viewport of args.viewports) {
        if (shot.only && shot.only !== viewport) continue;
        for (const theme of args.themes) {
          const err = await shoot(browser, shot, version, viewport, theme);
          if (err) errors.push(err);
          else n++;
        }
      }
  }
  const diffs = await diffAll(browser);
  await browser.close();
  writeIndex(diffs);
  writeFileSync(join(OUT, "diff.json"), JSON.stringify(diffs, null, 1));
  for (const [k, v] of Object.entries(diffs)) console.log(`  差异 ${k}: ${v}`);
  console.log(`截图 ${n} 张，失败 ${errors.length} 张 → ${OUT}`);
  for (const e of errors) console.log("  ✗", e);
  if (errors.length) process.exitCode = 1;
}

void main();
