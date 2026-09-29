/**
 * Vitest globalSetup：跑任何用例之前先打印目标、探测一次，目标和实际连到的服务器对不上就整体中止。
 *
 * `vitest.config.ts` 已经在配置加载阶段校验过 BASE_URL / CONTRACT_BASE_URL / TARGET 三个变量本身
 * 是否自洽（见 lib/target.ts），但那只能防住"配置自相矛盾"，防不住"配置一致但服务器答非所问"——比如
 * BASE_URL 端口写对了、TARGET=go，但那个端口上其实跑着别的东西（连不上、是 oracle、或者压根不是本项目）。
 * 这里用 Go 版 /api/health 独有的 `X-Vinx-Vocab` 响应头（见 internal/api/health.go）区分两边：
 * oracle（Fastify）不会有这个头。
 */
import { resolveTarget } from "./target";

export default async function setup(): Promise<void> {
  const { baseUrl, target } = resolveTarget();
  const healthUrl = `${baseUrl}/health`;
  // eslint-disable-next-line no-console
  console.log(`[contract] 目标 TARGET=${target}  BASE_URL=${baseUrl}`);

  let res: Response;
  try {
    res = await fetch(healthUrl);
  } catch (err) {
    throw new Error(
      `[contract] 连不上 ${healthUrl}（TARGET=${target}）：${(err as Error).message}\n` +
        `先确认对应实例已经起来：oracle 通常跑在 tmux session vinx-oracle（:4100）；` +
        `Go 单文件版要先 vinx-vocab seed-demo --data <目录>，再 vinx-vocab --data <目录> --port <端口>。`,
    );
  }
  if (res.status !== 200) {
    throw new Error(`[contract] ${healthUrl} 返回 HTTP ${res.status}，不是预期的健康检查响应，中止。`);
  }
  const hasGoHeader = res.headers.has("x-vinx-vocab");
  if (target === "go" && !hasGoHeader) {
    throw new Error(
      `[contract] TARGET=go，但 ${healthUrl} 的响应没有 X-Vinx-Vocab 头，这不像是 Go 单文件版的实例` +
        `（很可能连到了 oracle，或者 BASE_URL 端口配错了）。BASE_URL=${baseUrl}，中止，不跑任何用例。`,
    );
  }
  if (target === "oracle" && hasGoHeader) {
    throw new Error(
      `[contract] TARGET=oracle，但 ${healthUrl} 的响应带 X-Vinx-Vocab 头——这其实是 Go 单文件版的实例，` +
        `不是 oracle。BASE_URL=${baseUrl}，中止，不跑任何用例。`,
    );
  }
  // eslint-disable-next-line no-console
  console.log(`[contract] 探测通过：${healthUrl} 的响应与 TARGET=${target} 相符`);
}
