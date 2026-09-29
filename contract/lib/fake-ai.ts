/**
 * 契约测试用的假 OpenAI 兼容服务（/chat/completions）：记录收到的消息，可以设置延迟、HTTP 错误和回复内容。
 * 只监听 127.0.0.1 的随机端口，不依赖真实 AI（移植自旧 apps/api/tests/support/fake-ai.ts）。
 */
import { createServer, type Server } from "node:http";
import type { AddressInfo } from "node:net";

export interface FakeAiRequest {
  system: string;
  user: string;
  auth?: string;
  model?: string;
}

export interface FakeAiBehavior {
  /** 回复前等待的毫秒数 */
  delayMs?: number;
  /** 非 200 时按这个状态码返回 errorBody */
  status?: number;
  errorBody?: unknown;
  /** 200 时模型回复的文本；可以按收到的请求动态生成 */
  reply?: string | ((req: FakeAiRequest) => string);
}

export async function startFakeAi() {
  const requests: FakeAiRequest[] = [];
  let behavior: FakeAiBehavior = { reply: "OK" };
  const server: Server = createServer((req, res) => {
    let body = "";
    req.on("data", (c) => (body += c));
    req.on("end", () => {
      let json: { model?: string; messages?: { role: string; content: string }[] } = {};
      try {
        json = JSON.parse(body || "{}");
      } catch {
        /* 无效 JSON 也记一条空请求，方便断言 badjson 场景 */
      }
      const msgs = json.messages ?? [];
      const record: FakeAiRequest = {
        system: msgs.find((m) => m.role === "system")?.content ?? "",
        user: msgs.find((m) => m.role === "user")?.content ?? "",
        auth: req.headers.authorization,
        model: json.model,
      };
      requests.push(record);
      const b = behavior;
      const send = () => {
        if (res.destroyed) return;
        res.setHeader("content-type", "application/json");
        if (b.status && b.status !== 200) {
          res.statusCode = b.status;
          res.end(JSON.stringify(b.errorBody ?? { error: { message: "fake error" } }));
          return;
        }
        const reply = typeof b.reply === "function" ? b.reply(record) : (b.reply ?? "OK");
        res.end(JSON.stringify({ choices: [{ message: { content: reply } }] }));
      };
      if (b.delayMs) setTimeout(send, b.delayMs);
      else send();
    });
  });
  await new Promise<void>((r) => server.listen(0, "127.0.0.1", r));
  const port = (server.address() as AddressInfo).port;
  return {
    url: `http://127.0.0.1:${port}/v1`,
    requests,
    set(next: FakeAiBehavior) {
      behavior = next;
    },
    async close() {
      server.closeAllConnections();
      await new Promise((r) => server.close(r));
    },
  };
}

export type FakeAi = Awaited<ReturnType<typeof startFakeAi>>;

/** 拿一个当前没有服务在听的本机端口（开了再关），用于测试「连不上」。 */
export async function closedPortUrl(): Promise<string> {
  const s = createServer();
  await new Promise<void>((r) => s.listen(0, "127.0.0.1", r));
  const port = (s.address() as AddressInfo).port;
  await new Promise((r) => s.close(r));
  return `http://127.0.0.1:${port}/v1`;
}
