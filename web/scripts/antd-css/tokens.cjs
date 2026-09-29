const { createRequire } = require("module");
// 生成 tokens.json（antd 亮/暗两套 design token），供 extract.mjs 把颜色差异映射成 --ant-color-* 变量名。
// 用法：OLD_ADMIN=<旧仓库>/apps/admin node scripts/antd-css/tokens.cjs
if (!process.env.OLD_ADMIN) throw new Error("需要设置 OLD_ADMIN");
const r = createRequire(require("path").join(process.env.OLD_ADMIN, "package.json"));
const { theme } = r("antd");
const PALETTE = {
  light: { primary: "#0f6e66", success: "#2f7d4a", error: "#b42318", warning: "#c2410c", text: "#1d2740", textSecondary: "#4a5468", border: "#ddd5c3", borderSecondary: "#e9e2d2", bgLayout: "#f7f4ec", bgContainer: "#fffdf8", bgElevated: "#ffffff", onPrimary: "#ffffff" },
  dark: { primary: "#4dbfb0", success: "#5cc585", error: "#f27b70", warning: "#f08a5d", text: "#ece6d8", textSecondary: "#bdb7a9", border: "#3d4659", borderSecondary: "#2c3444", bgLayout: "#141a26", bgContainer: "#1b2230", bgElevated: "#222a3a", onPrimary: "#0d1b1e" },
};
const out = {};
for (const k of ["light", "dark"]) {
  const p = PALETTE[k];
  out[k] = theme.getDesignToken({ algorithm: k === "dark" ? theme.darkAlgorithm : theme.defaultAlgorithm, token: {
    colorPrimary: p.primary, colorInfo: p.primary, colorSuccess: p.success, colorError: p.error, colorWarning: p.warning, colorText: p.text, colorTextSecondary: p.textSecondary, colorTextLightSolid: p.onPrimary, colorBorder: p.border, colorBorderSecondary: p.borderSecondary, colorBgLayout: p.bgLayout, colorBgContainer: p.bgContainer, colorBgElevated: p.bgElevated, borderRadius: 10 } });
}
require("fs").writeFileSync(require("path").join(__dirname, "tokens.json"), JSON.stringify(out, null, 1));
const keys = Object.keys(out.light).filter(k => /^color/.test(k) && typeof out.light[k] === "string");
for (const key of keys) console.log(key, out.light[key], out.dark[key]);
