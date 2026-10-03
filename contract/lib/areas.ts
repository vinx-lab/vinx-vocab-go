/**
 * 按模块分批移植：Go 端还没实现的模块，在 TARGET=go 时跳过对应用例（oracle 上始终全跑）。
 *
 * 后续任务实现一个模块后，把模块名加进 GO_AREAS，对应用例就会在 Go 上自动启用。
 * 全部模块移植完成后（A8），GO_AREAS 应覆盖 Area 的全部取值。
 */
import { TARGET } from "./client";

export type Area =
  | "health"
  | "auth"
  | "config"
  | "books" // A2
  | "plans" // A3
  | "study" // A4：今日 / 学习组 / 记录
  | "sheets" // A5
  | "classes" // A6
  | "users" // A6
  | "ai" // A7
  | "settings"; // A7

/** Go 端已实现的模块 */
export const GO_AREAS: Area[] = ["health", "auth", "config", "books", "plans", "ai", "settings", "study", "classes", "users", "sheets"];

/** 这些模块在当前目标上都已实现 */
export function ready(...areas: Area[]): boolean {
  if (TARGET !== "go") return true;
  return areas.every((a) => GO_AREAS.includes(a));
}

/**
 * 只在 Go 上验证的用例（Go 版新增或与 oracle 开发模式行为不同的约定，如 Origin 检查）。
 * ADR 0004 之后的新功能（spec 0002–0006：dev、coverage、sentences、aigen、dictation）只在单文件版实现，
 * 对应用例整组用 `describe.runIf(goOnly)`，不进 Area / GO_AREAS（ready() 在 oracle 上恒为真，挡不住）。
 */
export const goOnly = TARGET === "go";
