// 仅开发期工具：用旧版 antd 渲染组件样板页，供 dump.mjs 抓取生成的样式。
// OLD_ADMIN 指向旧仓库 apps/admin 目录（需已安装依赖）。
import { defineConfig } from "vite";
import { join } from "node:path";
const OLD = process.env.OLD_ADMIN;
if (!OLD) throw new Error("需要设置 OLD_ADMIN=<旧仓库>/apps/admin");
const nm = (p) => join(OLD, "node_modules", p);
export default defineConfig({
  root: new URL(".", import.meta.url).pathname,
  esbuild: { jsx: "automatic", jsxImportSource: "react" },
  resolve: {
    alias: [
      { find: /^react-dom(\/.*)?$/, replacement: nm("react-dom") + "$1" },
      { find: /^react(\/.*)?$/, replacement: nm("react") + "$1" },
      { find: /^antd(\/.*)?$/, replacement: nm("antd") + "$1" },
      { find: /^@ant-design\/icons$/, replacement: nm("@ant-design/icons") },
      { find: /^dayjs(\/.*)?$/, replacement: nm("dayjs") + "$1" },
      { find: /^@old-theme$/, replacement: join(OLD, "src/theme.ts") },
    ],
  },
  server: { host: "127.0.0.1", port: 5176, strictPort: true, fs: { allow: ["/"] } },
});
