/**
 * 契约测试目标解析：一个函数、一份真相，被 `vitest.config.ts`（主进程，配置加载阶段）、
 * `global-setup.ts`（探测阶段）、`client.ts`（测试进程）三处共用，避免各处各算一遍、互相不一致。
 *
 * 背景（早期踩过的坑）：Vite 会把它自己的 `import.meta.env.BASE_URL`（固定是 `"/"`）写进测试进程的
 * `process.env.BASE_URL`，所以 `BASE_URL` 只能在主进程（`vitest.config.ts` 求值的时候）读，再显式透传给
 * 测试进程；用例本身读的是内部变量名 `CONTRACT_BASE_URL`。如果有人直接在 shell 里设
 * `CONTRACT_BASE_URL=...`（以为这就是"给测试用的" URL），而 `vitest.config.ts` 又无条件用算出来的默认值
 * 覆盖 `test.env.CONTRACT_BASE_URL`，那个直接设置的值会被默默吃掉、退回 oracle 默认值——表面上"跑通"了，
 * 其实测的是另一个实例。第一版 A3 实现者就是这样把"vs Go"跑成了"vs oracle"，全绿到看不出问题，
 * 只是碰巧一条 Go-only 的 Origin 用例断言了 403 而实际拿到 200 才暴露出来。
 *
 * 这里的规则：
 * - `BASE_URL` 是唯一"官方"入口；`CONTRACT_BASE_URL` 可以直接设（等价别名），但两个都设时必须一致，否则报错。
 * - `TARGET`（可选）必须与 `BASE_URL` 是否以 `/api` 结尾推断出的目标一致，否则报错——不允许"目标"和"URL"矛盾。
 * - 光是这两条只能防住"配置本身自相矛盾"，防不住"配置一致但服务器不是它自称的那个"（比如端口指错、
 *   Go 实例没起来、误连到 oracle）——那部分由 `global-setup.ts` 的实际探测负责。
 */

export type Target = "oracle" | "go";

export interface ResolvedTarget {
  baseUrl: string;
  target: Target;
}

/** 默认 oracle 地址（历史行为：不给任何环境变量时的兜底）。 */
export const DEFAULT_BASE_URL = "http://localhost:4100";

function normalize(url: string): string {
  return url.replace(/\/+$/, "");
}

/** 由 BASE_URL 判断目标：Go 单文件版的 API 挂在 /api 下，oracle 没有这个前缀。 */
export function inferTarget(baseUrl: string): Target {
  return baseUrl.endsWith("/api") ? "go" : "oracle";
}

/**
 * 解析契约测试目标。传入的 env 默认是 `process.env`；显式传参主要是为了单测可以不依赖真实环境变量。
 * 冲突（BASE_URL 与 CONTRACT_BASE_URL 不一致、TARGET 与 BASE_URL 推断的目标不一致、TARGET 取值非法）
 * 一律抛错，让调用方（vitest.config.ts）在加载配置阶段就整体失败，不要开始跑任何用例。
 */
export function resolveTarget(env: NodeJS.ProcessEnv = process.env): ResolvedTarget {
  const rawBase = env.BASE_URL && env.BASE_URL !== "/" ? normalize(env.BASE_URL) : undefined;
  const rawContract = env.CONTRACT_BASE_URL ? normalize(env.CONTRACT_BASE_URL) : undefined;

  if (rawBase && rawContract && rawBase !== rawContract) {
    throw new Error(
      `契约测试目标冲突：BASE_URL="${rawBase}" 与 CONTRACT_BASE_URL="${rawContract}" 不一致。只设其中一个` +
        `（推荐用 BASE_URL；CONTRACT_BASE_URL 是内部透传变量名，两者都设时必须一样）。`,
    );
  }
  const baseUrl = rawBase ?? rawContract ?? DEFAULT_BASE_URL;
  const inferred = inferTarget(baseUrl);

  const rawTarget = env.TARGET;
  if (rawTarget !== undefined && rawTarget !== "go" && rawTarget !== "oracle") {
    throw new Error(`契约测试目标非法：TARGET="${rawTarget}"，只能是 "go" 或 "oracle"。`);
  }
  if (rawTarget !== undefined && rawTarget !== inferred) {
    throw new Error(
      `契约测试目标冲突：TARGET="${rawTarget}"，但 BASE_URL="${baseUrl}" 按是否以 /api 结尾推断出的目标是` +
        `"${inferred}"。要么去掉 TARGET 让它按 BASE_URL 推断，要么把 BASE_URL 改成与 TARGET 一致的形态` +
        `（Go 单文件版的 API 挂在 /api 下，oracle 没有这个前缀）。`,
    );
  }
  return { baseUrl, target: (rawTarget as Target | undefined) ?? inferred };
}
