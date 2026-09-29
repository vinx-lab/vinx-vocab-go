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
