// 把 dump.mjs 抓到的亮/暗两套 antd 规则合并成一份 CSS：两套取值不同的属性改成 CSS 变量，
// 变量在 [data-theme="light"] / [data-theme="dark"] 下分别定义。输出 src/ui/antd.css。
// 用法：IN=<rules json 目录> node scripts/antd-css/extract.mjs
import { readFileSync, writeFileSync } from "node:fs";
const IN = process.env.IN ?? ".";
const L = JSON.parse(readFileSync(`${IN}/rules-light.json`, "utf8"));
const D = JSON.parse(readFileSync(`${IN}/rules-dark.json`, "utf8"));
const TOK = JSON.parse(readFileSync(new URL("./tokens.json", import.meta.url), "utf8"));

// 不实现的组件、RTL、紧凑模式等规则直接丢掉，控制体积
const DROP_LIST = [
  /ant-(rate|cascader|tree|descriptions|skeleton|image|badge|input-group-compact|space-compact|select-tree|motion-collapse-legacy)/,
  /-rtl\b|compact-(item|vertical|first|last)|ant-btn-group|ant-input-number-group/,
  /ant-(input|input-number|select|picker|input-affix-wrapper)-(filled|borderless|underlined)\b/,
  /ant-btn-color-(?!default|primary|dangerous)[a-z]+/,
  /ant-btn-variant-filled/,
  /ant-col-(xl|xxl)-|ant-col-([a-z]+-)?(push|pull|offset|order)-/,
  /ant-picker-(range|panels|time|week|month|year|quarter|decade|datetime|presets|footer-extra|ok)\b/,
  /ant-table-(expand|filter|tree|summary|sticky|selection-extra|bordered|column-has-filters|ping|fixed-column-gapped)/,
  /ant-tabs-(card|left|right|bottom|dropdown|nav-operations|nav-add|nav-more|editable)/,
  /ant-steps-(dot|navigation|inline|label-vertical|with-progress|rtl)|ant-steps-item-(custom|description)/,
  /ant-upload-(list|picture|wrapper-rtl)/,
  /ant-dropdown-menu-submenu|ant-menu-(submenu|horizontal|vertical|inline-collapsed|overflow|sub|dark)/,
  /ant-typography-(edit|copy|expand|ellipsis|collapse)|ant-typography[^,{]*\b(h1|h2|h3|h5|mark|kbd|del|ins|ul|ol|blockquote|pre)\b/,
  /ant-pagination-(options|jump|simple)|ant-pagination-mini[^,{]*options/,
  /ant-tag-(magenta|lime|gold|blue|purple|red|green|orange|cyan|volcano|geekblue)-inverse/,
  /ant-(zoom|move|fade)-(up|down|left|right|big-fast)?-?(appear|enter|leave)/,
  /ant-(collapse|timeline)[^,{]*(-borderless|-label|alternate|reverse|pending|-right)/,
  // 弹窗打开时 antd 动态加的滚动锁（rc-util ScrollLocker），抽样时弹窗开着就会被抽进来；静态写死会让整页无法滚动
  /^html body$/,
  // 组件库不做进出场动画，也不用 Tooltip/Popover 的彩色预设
  /ant-(zoom|move|slide|fade|motion)|-motion-|-appear|-enter|-leave/,
  /ant-(tooltip|popover)-(pink|magenta|red|volcano|orange|yellow|gold|cyan|lime|green|blue|geekblue|purple)\b/,
  /ant-select-tags|ant-input-number-affix|ant-input-number-prefix|ant-picker-(week|range|multiple)/,
];
const DROP = { test: (s) => DROP_LIST.some((r) => r.test(s)) };
const strip = (s) => s.replace(/:where\(\.css-dev-only-do-not-override-[a-z0-9]+\)/g, "").replace(/\.css-dev-only-do-not-override-[a-z0-9]+/g, "").replace(/css-dev-only-do-not-override-[a-z0-9]+-/g, "vx-");

// 颜色归一：hex → rgb()/rgba()，与浏览器序列化一致
function norm(v) {
  v = String(v).trim();
  const m = /^#([0-9a-f]{3,8})$/i.exec(v);
  if (!m) return v.replace(/\s+/g, "").toLowerCase();
  let h = m[1];
  if (h.length <= 4) h = [...h].map((c) => c + c).join("");
  const [r, g, b] = [0, 2, 4].map((i) => parseInt(h.slice(i, i + 2), 16));
  if (h.length === 8) return `rgba(${r},${g},${b},${+(parseInt(h.slice(6), 16) / 255).toFixed(3)})`;
  return `rgb(${r},${g},${b})`;
}
const tokenByPair = new Map();
for (const k of Object.keys(TOK.light)) {
  if (!/^color/.test(k) || typeof TOK.light[k] !== "string") continue;
  const key = norm(TOK.light[k]) + "|" + norm(TOK.dark[k]);
  if (!tokenByPair.has(key)) tokenByPair.set(key, "--ant-" + k.replace(/[A-Z]/g, (c) => "-" + c.toLowerCase()));
}
const vars = new Map(); // name -> [light, dark]
const byPair = new Map();
let n = 0;
function varFor(l, d) {
  const key = norm(l) + "|" + norm(d);
  if (byPair.has(key)) return byPair.get(key);
  const name = tokenByPair.get(key) ?? `--vx-a${++n}`;
  byPair.set(key, name);
  vars.set(name, [l, d]);
  return name;
}

let dropped = 0;
function emit(l, d, indent = "") {
  if (l.t !== d.t) throw new Error("结构不一致");
  if (l.t === "media") {
    const inner = l.rules.map((r, i) => emit(r, d.rules[i], indent + "  ")).filter(Boolean).join("\n");
    return inner ? `${indent}@media ${l.cond} {\n${inner}\n${indent}}` : "";
  }
  if (l.t === "raw") {
    if (strip(l.text) !== strip(d.text)) {
      if (l.name) return `${indent}${strip(l.text)}`; // 关键帧里的颜色差异忽略（只有波纹动效）
      throw new Error("raw 差异: " + l.text.slice(0, 80));
    }
    return indent + strip(l.text);
  }
  const sel = strip(l.sel);
  if (strip(d.sel) !== sel) throw new Error("选择器不一致: " + sel);
  // 选择器列表逐个过滤
  const parts = sel.split(/,(?![^(]*\))/).map((s) => s.trim()).filter((s) => !DROP.test(s));
  if (!parts.length) { dropped++; return ""; }
  const dmap = new Map(d.props.map(([p, v, pr]) => [p, strip(v) + (pr ? " !important" : "")]));
  const body = l.props.map(([p, v0, pr]) => {
    const v = strip(v0);
    const lv = v + (pr ? " !important" : "");
    const dv = dmap.get(p);
    if (dv === undefined || norm(dv) === norm(lv)) return `${p}:${lv}`;
    return `${p}:var(${varFor(v, dv.replace(/ !important$/, ""))})${pr ? " !important" : ""}`;
  });
  if (!body.length) return "";
  return `${indent}${parts.join(",")}{${body.join(";")}}`;
}
let out = L.map((r, i) => emit(r, D[i])).filter(Boolean);
// 只保留还被引用的关键帧
const used = new Set();
for (const r of out) if (!r.trim().startsWith("@keyframes")) for (const m of r.matchAll(/animation(?:-name)?:([^;}]*)/g)) for (const w of m[1].split(/[\s,]+/)) used.add(w);
out = out.filter((r) => { const m = /^\s*@keyframes\s+([\w-]+)/.exec(r); return !m || used.has(m[1]); });
const decl = (i) => [...vars].map(([k, v]) => `${k}:${v[i]}`).join(";");
const css = `/* 由 scripts/antd-css/extract.mjs 从 antd 5 生成的样式抽取，不要手改；定制写在 ui.css */\n` +
  `:root,[data-theme="light"]{${decl(0)}}\n[data-theme="dark"]{${decl(1)}}\n` + [...new Set(out)].join("\n") + "\n";
const FONT = /-apple-system, BlinkMacSystemFont, "PingFang SC", "Hiragino Sans GB", "Microsoft YaHei", "Segoe UI", sans-serif/g;
const min = css.replace(FONT, "var(--sans)").replace(/(rgba?\([^)]*\))/g, (m) => m.replace(/\s+/g, ""));
writeFileSync(new URL("../../src/ui/antd.css", import.meta.url), min);
console.log(`规则 ${out.length}，丢弃 ${dropped}，变量 ${vars.size}，${(min.length / 1024).toFixed(1)} KB`);
