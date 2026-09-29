import { api } from "@/lib/api";
import { useApi } from "@/lib/query";
import type { AiStatus } from "@/types";

/** AI / 发音能力探测（未配置时前端隐藏相关入口） */
export function useAiStatus() {
  const q = useApi(["ai", "status"], () => api.get<AiStatus>("/ai/status"), { staleTime: 5 * 60_000, retry: false });
  return { ai: q.data?.enabled ?? false, audio: q.data?.audio ?? false, status: q.data, isLoading: q.isLoading };
}
