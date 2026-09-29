// 抓取样板页（vite.config.mjs 起在 5176）亮/暗两套 antd 生成的样式规则，写到 OUT 目录的 rules-{light,dark}.json。
import { chromium } from "@playwright/test";
import { writeFileSync } from "node:fs";
const OUT = process.env.OUT ?? ".";
const browser = await chromium.launch();
for (const theme of ["light", "dark"]) {
  const page = await browser.newPage({ viewport: { width: 1400, height: 900 } });
  const errs = [];
  page.on("pageerror", (e) => errs.push(String(e)));
  await page.goto(`http://127.0.0.1:5176/?${theme}`);
  await page.waitForTimeout(4000);
  const rules = await page.evaluate(() => {
    const ser = (r) => {
      if (r instanceof CSSStyleRule) {
        // cssText 会尽量合并成简写，比逐个长属性小得多
        const props = [];
        let depth = 0, cur = "";
        for (const ch of r.style.cssText) {
          if (ch === "(") depth++;
          if (ch === ")") depth--;
          if (ch === ";" && depth === 0) { if (cur.trim()) props.push(cur.trim()); cur = ""; } else cur += ch;
        }
        if (cur.trim()) props.push(cur.trim());
        for (let i = 0; i < props.length; i++) {
          const d = props[i], k = d.indexOf(":");
          let v = d.slice(k + 1).trim(), pr = "";
          if (v.endsWith("!important")) { v = v.slice(0, -10).trim(); pr = "important"; }
          props[i] = [d.slice(0, k).trim(), v, pr];
        }
        return { t: "style", sel: r.selectorText, props, nested: r.cssRules ? [...r.cssRules].map(ser) : [] };
      }
      if (r instanceof CSSMediaRule) return { t: "media", cond: r.conditionText, rules: [...r.cssRules].map(ser) };
      if (r instanceof CSSKeyframesRule) return { t: "raw", text: r.cssText, name: r.name };
      return { t: "raw", text: r.cssText };
    };
    const all = [];
    for (const s of document.styleSheets) {
      if (s.ownerNode?.tagName !== "STYLE") continue;
      for (const r of s.cssRules) all.push(ser(r));
    }
    return all;
  });
  console.log(theme, rules.length, "rules", errs.length ? errs : "");
  writeFileSync(`${OUT}/rules-${theme}.json`, JSON.stringify(rules));
  await page.close();
}
await browser.close();
