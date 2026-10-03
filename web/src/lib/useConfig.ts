import type { AppConfig } from "@vinx/shared";
import { api } from "@/lib/api";
import { useApi } from "@/lib/query";

/** 个人版默认配置：后端不可达时按最小可用功能渲染 */
const FALLBACK: AppConfig = {
  edition: "personal",
  features: { classes: false, assignToOthers: false, multiUser: false, studentRecords: false },
  ai: false,
  audio: false,
  signupEnabled: false,
  editionLocked: false,
  needsSetup: false,
  dev: false,
};

/** 运行时配置（版本 / 功能开关 / AI / 发音） */
export function useConfig(): AppConfig & { isLoading: boolean } {
  const q = useApi(["config"], () => api.get<AppConfig>("/config"), { staleTime: Infinity, retry: 1 });
  return { ...(q.data ?? FALLBACK), isLoading: q.isLoading };
}
