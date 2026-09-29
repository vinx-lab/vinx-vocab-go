import { defineConfig } from "vitest/config";
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
  test: { environment: "jsdom", include: ["test/**/*.test.{ts,tsx}"] },
});
