/**
 * 版本开关：同一份代码支持两种形态。
 *
 * - `personal`：个人版，单人使用、可开源。没有班级、没有他人安排、没有用户管理，
 *   注册第一个账号即为使用者本人（之后关闭注册）。
 * - `school`：班级版，包含班级、批量建号、给学生安排计划、学生记录等教学功能。
 *
 * 班级版专属代码集中在 `apps/api/src/school/` 与 `apps/admin/src/pages/school/`，
 * 开源个人版可以整目录删除：两端都做了「模块缺失即降级」处理，不会因此报错。
 */

export type Edition = "personal" | "school";

export interface Features {
  /** 班级与成员管理 */
  classes: boolean;
  /** 给班级 / 学生安排计划 */
  assignToOthers: boolean;
  /** 多用户：注册、用户管理、角色 */
  multiUser: boolean;
  /** 查看他人学习记录 */
  studentRecords: boolean;
}

const FEATURES: Record<Edition, Features> = {
  personal: { classes: false, assignToOthers: false, multiUser: false, studentRecords: false },
  school: { classes: true, assignToOthers: true, multiUser: true, studentRecords: true },
};

export const EDITION_LABEL: Record<Edition, string> = {
  personal: "个人版",
  school: "班级版",
};

export function isEdition(value: unknown): value is Edition {
  return value === "personal" || value === "school";
}

export function featuresOf(edition: Edition): Features {
  return { ...FEATURES[edition] };
}

/** 运行时配置（GET /config 返回给前端） */
export interface AppConfig {
  edition: Edition;
  features: Features;
  /** AI 内容生成是否可用 */
  ai: boolean;
  /** 真人发音是否可用 */
  audio: boolean;
  /** 是否允许注册新账号 */
  signupEnabled: boolean;
  /** 版本由 VINX_EDITION 指定，界面上不可切换（Go 版新增） */
  editionLocked: boolean;
  /** 首次运行：未锁定、未选择版本且还没有账号时为 true，前端先进入版本向导（Go 版新增） */
  needsSetup: boolean;
  /** 以 serve --dev 启动：显示免密切换账号入口（Go 版新增，spec 0002） */
  dev: boolean;
  /** 程序版本（构建时由 git 标签注入，如 v3.1.0；本地开发为 dev） */
  version: string;
}
