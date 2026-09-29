import { useCallback, useEffect, useRef, useState } from "preact/hooks";
import { AI_JOB_POLL_MS, type AiJobStarted, type AiJobView } from "@vinx/shared";
import { api, errorMessage } from "@/lib/api";

/**
 * AI 后台任务（K41）：提交后拿到任务号，每 2 秒查一次进度，直到完成或失败。
 * 显示用的「已用 N 秒」每秒在本地走一次；api 重启后任务丢失（查询 404），提示重新生成。
 */
export type AiJobState<T> =
  | { phase: "idle" }
  | { phase: "running"; elapsedMs: number }
  | { phase: "done"; elapsedMs: number; result: T }
  | { phase: "failed"; error: string };

export const JOB_LOST_MESSAGE = "任务已中断，请重新生成";
/** 查询进度连续失败几次后放弃（例如网络断开） */
const MAX_POLL_ERRORS = 5;

/**
 * 查询进度出错时怎么办：404 说明任务已经没了（过期或 api 重启），直接判失败；
 * 其他错误（网关、网络抖动）先继续查，连续失败太多次才放弃。
 */
export function pollErrorOutcome(e: unknown, consecutiveErrors: number): { stop: boolean; message: string } {
  if ((e as { statusCode?: number })?.statusCode === 404) return { stop: true, message: JOB_LOST_MESSAGE };
  return { stop: consecutiveErrors >= MAX_POLL_ERRORS, message: errorMessage(e, "查询进度失败") };
}

/** 「生成中… 已用 N 秒」 */
export const runningLabel = (elapsedMs: number) => `生成中… 已用 ${Math.floor(elapsedMs / 1000)} 秒`;

export function useAiJob<T>() {
  const [state, setState] = useState<AiJobState<T>>({ phase: "idle" });
  // 每次开始新任务换一个令牌，旧任务的轮询结果直接丢弃
  const token = useRef(0);
  const timers = useRef<{ poll?: ReturnType<typeof setTimeout>; tick?: ReturnType<typeof setInterval> }>({});

  const clear = () => {
    if (timers.current.poll) clearTimeout(timers.current.poll);
    if (timers.current.tick) clearInterval(timers.current.tick);
    timers.current = {};
  };

  useEffect(
    () => () => {
      token.current += 1;
      clear();
    },
    [],
  );

  const reset = useCallback(() => {
    token.current += 1;
    clear();
    setState({ phase: "idle" });
  }, []);

  /** 提交任务并轮询；onDone 在完成时调用一次。提交本身失败（校验、429 等）时直接显示原因 */
  const start = useCallback(async (submit: () => Promise<AiJobStarted>, onDone?: (result: T) => void) => {
    const my = ++token.current;
    clear();
    const startedAt = Date.now();
    setState({ phase: "running", elapsedMs: 0 });
    let jobId: string;
    try {
      jobId = (await submit()).jobId;
    } catch (e) {
      if (my === token.current) setState({ phase: "failed", error: errorMessage(e, "生成失败") });
      return;
    }
    if (my !== token.current) return;

    let serverElapsed = 0;
    timers.current.tick = setInterval(() => {
      if (my !== token.current) return;
      setState((s) => (s.phase === "running" ? { phase: "running", elapsedMs: Math.max(serverElapsed, Date.now() - startedAt) } : s));
    }, 1000);

    let errors = 0;
    const poll = async () => {
      if (my !== token.current) return;
      try {
        const job = await api.get<AiJobView<T>>(`/ai/jobs/${jobId}`);
        if (my !== token.current) return;
        errors = 0;
        serverElapsed = job.elapsedMs;
        if (job.status === "done") {
          clear();
          setState({ phase: "done", elapsedMs: job.elapsedMs, result: job.result as T });
          onDone?.(job.result as T);
          return;
        }
        if (job.status === "failed") {
          clear();
          setState({ phase: "failed", error: job.error || "生成失败" });
          return;
        }
      } catch (e) {
        if (my !== token.current) return;
        errors += 1;
        const outcome = pollErrorOutcome(e, errors);
        if (outcome.stop) {
          clear();
          setState({ phase: "failed", error: outcome.message });
          return;
        }
      }
      timers.current.poll = setTimeout(poll, AI_JOB_POLL_MS);
    };
    timers.current.poll = setTimeout(poll, AI_JOB_POLL_MS);
  }, []);

  return { state, start, reset, running: state.phase === "running" };
}
