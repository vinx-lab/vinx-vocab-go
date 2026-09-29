// 统计构建产物 JS 总大小（未压缩），预算 200KB（不含字体）。先 vite build。
import { readdirSync, statSync } from "node:fs";
import { join } from "node:path";
const dir = new URL("../dist/assets/", import.meta.url).pathname;
const files = readdirSync(dir).filter((f) => f.endsWith(".js"));
const total = files.reduce((n, f) => n + statSync(join(dir, f)).size, 0);
const css = readdirSync(dir).filter((f) => f.endsWith(".css")).reduce((n, f) => n + statSync(join(dir, f)).size, 0);
const BUDGET = 200 * 1024;
console.log(`JS ${files.length} 个文件，共 ${(total / 1024).toFixed(1)} KB（预算 200 KB）；CSS ${(css / 1024).toFixed(1)} KB`);
if (total > BUDGET) {
  console.error("超出 JS 预算");
  process.exitCode = 1;
}
