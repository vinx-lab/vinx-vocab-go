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

/** spec 0005：四种生成 + 重写一句。按系统消息（隐藏的输出格式）里的关键字区分。 */
export type GenerationKind = "rewrite" | "variant" | "pattern" | "example" | "passage" | "unknown";

/**
 * 判断一次请求是哪种生成。顺序有讲究：句型的输出格式里有「例句」二字，仿写的输出格式里有「变式」，
 * 所以先判断「重写」「仿写」「重点句型」，再判断「例句」「短文」。
 */
export function generationKind(req: FakeAiRequest): GenerationKind {
  const s = req.system;
  if (s.includes("重写")) return "rewrite";
  if (s.includes("仿写")) return "variant";
  if (s.includes("重点句型")) return "pattern";
  if (s.includes("例句")) return "example";
  if (s.includes("短文")) return "passage";
  return "unknown";
}

/** 例句：从用户消息里的「1. spelling (pos) —— 释义」行取出单词。 */
function promptSpellings(user: string): string[] {
  const out: string[] = [];
  for (const line of user.split("\n")) {
    const m = /^\d+\.\s+(.+?)(?:\s+\([^)]*\))?\s+——\s/.exec(line);
    if (m) out.push(m[1]);
  }
  return out;
}

/** 仿写：用户消息里「例句（N 句）」后面的原句数。 */
function originCount(user: string): number {
  const m = /例句（(\d+) 句）/.exec(user);
  return m ? Number(m[1]) : 1;
}

/**
 * spec 0005 逐句输出格式的默认回复：按请求种类生成一份合法的 JSON，契约测试可直接
 * `fake.set({ reply: generationReply })`。例句给用户消息里每个词写一句（句中用上该词）。
 */
export function generationReply(req: FakeAiRequest): string {
  switch (generationKind(req)) {
    case "rewrite":
      return JSON.stringify({ en: "I like reading books.", cn: "我喜欢读书。" });
    case "variant": {
      const n = originCount(req.user);
      const items: { origin: number; en: string; cn: string; change: string; note: string }[] = [];
      for (let i = 1; i <= n; i++) {
        items.push({ origin: i, en: `I find reading useful ${i}.`, cn: `我觉得阅读有用 ${i}。`, change: "替换", note: "换了动名词" });
      }
      return JSON.stringify(items);
    }
    case "pattern":
      return JSON.stringify([
        { en: "How do you learn English?", cn: "你怎样学英语？" },
        { en: "I learn English by reading.", cn: "我通过阅读学英语。", frame: "I learn ... by doing ..." },
      ]);
    case "example":
      return JSON.stringify(promptSpellings(req.user).map((sp) => ({ spelling: sp, en: `I can use ${sp} in a sentence.`, cn: `我会用 ${sp} 造句。` })));
    case "passage":
      return JSON.stringify({
        title: "My Day",
        titleCn: "我的一天",
        sentences: [
          { en: "I get up early.", cn: "我起得很早。", paragraph: 0 },
          { en: "Then I read a book.", cn: "然后我读书。", paragraph: 0 },
          { en: "At night I go to bed.", cn: "晚上我睡觉。", paragraph: 1 },
        ],
        questions: [{ q: "我几点起床？", a: "很早。" }],
      });
    default:
      return "OK";
  }
}

/** 拿一个当前没有服务在听的本机端口（开了再关），用于测试「连不上」。 */
export async function closedPortUrl(): Promise<string> {
  const s = createServer();
  await new Promise<void>((r) => s.listen(0, "127.0.0.1", r));
  const port = (s.address() as AddressInfo).port;
  await new Promise((r) => s.close(r));
  return `http://127.0.0.1:${port}/v1`;
}
