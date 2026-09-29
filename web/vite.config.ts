import { defineConfig } from "vite";
import { fileURLToPath, URL } from "node:url";

const src = fileURLToPath(new URL("./src", import.meta.url));

export default defineConfig({
  esbuild: { jsx: "automatic", jsxImportSource: "preact" },
  resolve: {
    alias: [
      { find: /^@vinx\/shared$/, replacement: `${src}/shared/index.ts` },
      { find: /^@\//, replacement: `${src}/` },
    ],
  },
  // 只扫描应用入口（scripts/antd-css 下的样板页依赖旧仓库的 react/antd，不参与）
  optimizeDeps: { entries: ["index.html"] },
  build: {
    target: "es2020",
    // 产物由 Go 嵌入（C1 复制到 internal/web/dist）
    outDir: "dist",
    assetsInlineLimit: 0,
    rollupOptions: {
      // 组件库（src/ui）的模块没有导入即生效的副作用（只有 List.Item = … 这类静态属性挂载，随组件一起保留）：
      // 标成无副作用后，只被懒加载页面用到的组件（Table、Tabs、Drawer…）跟着页面分块，不再全部塞进首屏包。
      // 样式文件（antd.css / ui.css）仍按有副作用处理。
      treeshake: { moduleSideEffects: (id) => !/\/src\/ui\/(?!index\.ts$)[^/]+\.tsx?$/.test(id) },
      // 图标、小组件按用途拆出的零碎共享块（几百字节）合并进引用它们的块，减少请求数
      output: { experimentalMinChunkSize: 1000 },
    },
  },
  server: {
    // 与旧版一致：监听全部网卡，放行任意主机名；/api 转发到后端并去掉前缀（oracle 路径不带 /api）。
    // 指向 Go 后端（它自己把 API 挂在 /api/ 下）时设 API_STRIP_PREFIX=false 保留前缀，不去掉。
    host: "0.0.0.0",
    port: Number(process.env.WEB_PORT ?? 5174),
    strictPort: true,
    allowedHosts: true,
    proxy: {
      "/api": {
        target: process.env.API_TARGET ?? "http://localhost:4100",
        rewrite: process.env.API_STRIP_PREFIX === "false" ? undefined : (path) => path.replace(/^\/api/, ""),
      },
    },
  },
});
