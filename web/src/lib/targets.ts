/**
 * 我的目标词书与自主安排（spec 0003 / 0008）的共用请求：
 * GET /me/target-books 的 canEditOwn 同时表示「能否自建计划」（不在班里，或所在班级都允许自主安排）。
 */
import { api } from "@/lib/api";
import { useApi } from "@/lib/query";
import type { MyTargetBooks } from "@/types";

export const myTargetsKey = ["me", "target-books"] as const;

export function useMyTargets(enabled = true) {
  return useApi(myTargetsKey, () => api.get<MyTargetBooks>("/me/target-books"), { enabled });
}

/** 班级未开放自主安排时，自建入口的说明 */
export const SELF_PLAN_CLOSED_TEXT = "班级未开放自主安排计划，学习计划由老师布置";
