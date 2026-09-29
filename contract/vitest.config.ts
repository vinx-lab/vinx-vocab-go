import { defineConfig } from "vitest/config";
import { resolveTarget } from "./lib/target";

// Vite 会把 import.meta.env.BASE_URL（"/"）写进测试进程的 process.env.BASE_URL，所以目标解析必须在
// 配置加载时（本文件求值的主进程，process.env 还没被 Vite 自己的约定覆盖）做一次，再显式透传给测试进程。
// resolveTarget()（lib/target.ts）在这里做：
//   - BASE_URL 与 CONTRACT_BASE_URL 都设且不一致 → 抛错；
//   - TARGET 与 BASE_URL 推断出的目标矛盾 → 抛错；
// 冲突直接让 vitest 启动失败（不会开始跑任何用例），比"全部跑绿但其实测错了实例"安全得多——
// 这是早期踩过的坑：之前直接设 CONTRACT_BASE_URL 会被这里静默的默认值覆盖，看起来测的是 Go，
// 实际请求全部打到了 oracle。globalSetup（lib/global-setup.ts）再做一次活体探测兜底：配置本身自洽，
// 但连上的服务器答非所问（比如端口指错）时同样中止。
const { baseUrl, target } = resolveTarget();

export default defineConfig({
  test: {
    env: { CONTRACT_BASE_URL: baseUrl, TARGET: target },
    environment: "node",
    include: ["specs/**/*.spec.ts"],
    globalSetup: ["./lib/global-setup.ts"],
    // 所有用例打同一个实例、共享同一个库：串行执行，避免互相干扰
    fileParallelism: false,
    testTimeout: 30_000,
    hookTimeout: 60_000,
  },
});
