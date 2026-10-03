import { defineConfig, devices } from "@playwright/test";

/**
 * 端到端用例跑在 Go 单文件上（不自动起服务：本机探测没人监听的端口会挂起，先用 run.sh 起好）：
 *   - go：seed-demo 过的实例（E2E_BASE_URL，默认 http://localhost:3250），旧版全部用例 + Go 版新增的学习页用例；
 *   - fresh：全新数据目录、没有任何账号的实例（E2E_FRESH_URL，默认 http://localhost:3251），只跑首次运行向导；
 *   - dev：另一份 seed-demo 数据、以 --dev 启动的实例（E2E_DEV_URL，默认 http://localhost:3252），只跑开发模式切换账号。
 * 一条命令：make e2e（构建后调用 e2e/run.sh）。
 */
export default defineConfig({
  testDir: "./specs",
  timeout: 30_000,
  fullyParallel: false,
  // 共享同一个实例与数据库，串行避免登录态互相干扰（与旧版配置一致）
  workers: 1,
  reporter: [["list"]],
  outputDir: "./test-results",
  use: {
    trace: "retain-on-failure",
  },
  projects: [
    {
      name: "go",
      testIgnore: /(setup-wizard|dev-switch)\.spec\.ts/,
      use: { ...devices["Desktop Chrome"], baseURL: process.env.E2E_BASE_URL ?? "http://localhost:3250" },
    },
    {
      name: "fresh",
      testMatch: /setup-wizard\.spec\.ts/,
      use: { ...devices["Desktop Chrome"], baseURL: process.env.E2E_FRESH_URL ?? "http://localhost:3251" },
    },
    {
      name: "dev",
      testMatch: /dev-switch\.spec\.ts/,
      use: { ...devices["Desktop Chrome"], baseURL: process.env.E2E_DEV_URL ?? "http://localhost:3252" },
    },
  ],
});
