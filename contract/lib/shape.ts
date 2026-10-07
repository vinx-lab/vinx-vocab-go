/**
 * 结构比对：把 JSON 值转为「结构描述」，跨实现比对字段名、类型、null 与缺省的区别，而不比具体值。
 *
 * - 基本类型：`string` / `number` / `boolean` / `null`；UTC 毫秒时间串（2026-09-28T05:51:26.656Z）记为 `datetime`
 * - 数组：`{ array: [元素结构…] }`，元素结构去重后按 JSON 排序（空数组为 `{ array: [] }`）
 * - 对象：`{ object: { 键: 结构 } }`，键按字母序；缺省的键不会出现，值为 null 的键记为 `null`
 *
 * 快照：`expectShape(name, value)` 在 oracle 上运行时写入 `shapes/<name>.json`，
 * 在 Go 上运行时与之比对（缺快照直接失败）。设 UPDATE_SHAPES=1 可在任何目标上强制重写。
 */
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { expect } from "vitest";
import { TARGET } from "./client";

export type Shape =
  | "string"
  | "number"
  | "boolean"
  | "null"
  | "datetime"
  | "undefined"
  | { array: Shape[] }
  | { object: Record<string, Shape> };

const ISO_MS = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$/;

export function shapeOf(value: unknown): Shape {
  if (value === null) return "null";
  if (value === undefined) return "undefined";
  if (Array.isArray(value)) {
    const seen = new Map<string, Shape>();
    for (const v of value) {
      const s = shapeOf(v);
      seen.set(JSON.stringify(s), s);
    }
    return { array: [...seen.keys()].sort().map((k) => seen.get(k)!) };
  }
  switch (typeof value) {
    case "string":
      return ISO_MS.test(value) ? "datetime" : "string";
    case "number":
      return "number";
    case "boolean":
      return "boolean";
    case "object": {
      const out: Record<string, Shape> = {};
      for (const k of Object.keys(value as object).sort()) out[k] = shapeOf((value as Record<string, unknown>)[k]);
      return { object: out };
    }
    default:
      return "string";
  }
}

const SHAPES_DIR = join(dirname(fileURLToPath(import.meta.url)), "..", "shapes");

/**
 * 与 oracle 录下的结构快照比对。
 * oracle 上运行（或 UPDATE_SHAPES=1）时写快照；Go 上运行时比对。
 */
export function expectShape(name: string, value: unknown) {
  const file = join(SHAPES_DIR, `${name}.json`);
  const added = TARGET === "go" ? GO_ADDED[name] : undefined;
  if (added && process.env.UPDATE_SHAPES !== "1") value = stripAdded(name, value, added);
  const actual = shapeOf(value);
  if (TARGET === "oracle" || process.env.UPDATE_SHAPES === "1") {
    mkdirSync(dirname(file), { recursive: true });
    writeFileSync(file, JSON.stringify(actual, null, 2) + "\n");
    return;
  }
  if (!existsSync(file)) throw new Error(`缺少结构快照 ${name}：先对 oracle 跑一遍契约测试`);
  const expected = JSON.parse(readFileSync(file, "utf8"));
  expect(actual, `结构与 oracle 不一致：${name}`).toEqual(expected);
}

/**
 * Go 版在 oracle 快照之外新增的字段（ADR 0004：单文件版为准，响应只增字段时，与 oracle 快照比对忽略新增字段）。
 *
 * 路径以响应体为根，用 `.` 分隔，`[]` 表示数组的每个元素，例如 `data.members[].coverage`。
 * Go 上比对前：先断言路径上的每个对象都带着这个键（新字段必须出现，值的含义由 Go 专属用例断言），
 * 再把它从响应里去掉，其余字段照常与 oracle 快照逐项全等比对。快照文件本身不改。
 */
const GO_ADDED: Record<string, string[]> = {
  // spec 0002：开发模式开关
  config: ["data.dev"],
  // spec 0003：班级概览成员的目标覆盖；spec 0008：覆盖口径与「含自选」
  "class-overview": ["data.students[].coverage", "data.coverageMode", "data.students[].coverageIncludesOwn"],
  "class-overview-empty": ["data.students[].coverage", "data.coverageMode", "data.students[].coverageIncludesOwn"],
  // spec 0006：今日页今天已批改的默写单；单词单的格式与题目
  // spec 0008：今日页「不在目标词书内」的计划、暂停的自建计划数
  "today-empty": ["data.gradedSheets", "data.outsideTargetPlanIds", "data.pausedSelfPlans"],
  today: ["data.gradedSheets", "data.outsideTargetPlanIds", "data.pausedSelfPlans"],
  "today-with-sheet": ["data.gradedSheets", "data.sheet.format", "data.sheet.itemCount", "data.outsideTargetPlanIds", "data.pausedSelfPlans"],
  // spec 0008：班级「允许学生自主安排」；计划的「暂停中」「不在目标词书内」标记
  "class-create": ["data.allowSelfPlan"],
  "class-update": ["data.allowSelfPlan"],
  "class-invite-code": ["data.allowSelfPlan"],
  "class-detail": ["data.allowSelfPlan"],
  "class-list": ["data.items[].allowSelfPlan"],
  "plans-detail": ["data.selfPlanPaused", "data.outsideTarget"],
  "sheets-detail": ["data.format", "data.grading", "data.items"],
  "sheets-list": ["data.items[].format", "data.items[].itemCount"],
  "sheets-sources": ["data.learningSentences"],
  // spec 0005：词书学段、AI 预览的学段、逐句检查标记
  "books-create-raw": ["data.level"],
  "books-list": ["items[].level"],
  "books-detail": ["data.level"],
  "ai-example-preview": ["data.level"],
  "ai-passage-preview": ["data.level"],
  "ai-job-example-done": ["result.level", "result.items[].checks"],
  "ai-job-examples-done": ["result.level", "result.items[].checks"],
  // spec 0004 / 0005：短文逐句（句子、学段、检查标记）
  "ai-job-passage-done": ["result.level", "result.sentences"],
  "ai-passage-detail": ["data.sentences"],
  // spec 0009：记忆分布的「没记住」、单词列表的「可能忘了」、复习记录的来源（补算）；
  // 单词单测试里没学过、答对的词建卡，ratings 多出 good
  "records-summary": ["data.mastery.missed"],
  "records-words": ["data.items[].forgetting"],
  "records-word-history": ["data.reviewLogs[].source"],
  "sheets-complete": ["data.result.ratings.good"],
};

function stripAdded(name: string, value: unknown, paths: string[]): unknown {
  const copy = structuredClone(value);
  for (const path of paths) {
    const segs = path.split(".");
    const visit = (node: unknown, i: number, at: string) => {
      const seg = segs[i];
      const isArr = seg.endsWith("[]");
      const key = isArr ? seg.slice(0, -2) : seg;
      expect(node !== null && typeof node === "object" && !Array.isArray(node), `${name}：${at} 不是对象`).toBe(true);
      const obj = node as Record<string, unknown>;
      expect(Object.hasOwn(obj, key), `${name}：Go 应带新增字段 ${at}${key}`).toBe(true);
      if (i === segs.length - 1) {
        delete obj[key];
        return;
      }
      if (isArr) {
        const arr = obj[key];
        expect(Array.isArray(arr), `${name}：${at}${key} 不是数组`).toBe(true);
        (arr as unknown[]).forEach((el, j) => visit(el, i + 1, `${at}${key}[${j}].`));
      } else if (obj[key] !== null) {
        visit(obj[key], i + 1, `${at}${key}.`);
      }
    };
    visit(copy, 0, "");
  }
  return copy;
}
