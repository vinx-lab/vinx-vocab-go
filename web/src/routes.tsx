/**
 * 路由表（与旧版 App.tsx 一一对应）。新增或迁移页面只改这里和 pages/ 下对应文件。
 * - cap：需要的能力，不足时回落到首页
 * - feature：需要的版本功能（个人版没有时回落到首页）
 * - bare：沉浸式页面，不带侧栏/底栏
 * 顺序有意义：先匹配到的先用（/plans/new 要在 /plans/:id 前面）。
 */
import type { ComponentType } from "preact";
import { lazy } from "preact-iso";
import type { AppConfig, Capability } from "@vinx/shared";
import { TodayPage } from "@/pages/today/TodayPage";
import { StudyPage } from "@/pages/study/lazy";

export interface RouteDef {
  path: string;
  component: ComponentType<any>;
  cap?: Capability;
  feature?: keyof AppConfig["features"];
  bare?: boolean;
}

// 与旧版一致：个人中心也按路由懒加载（旧 App.tsx 里 ProfilePage 是 lazy，今日页不是）
const ProfilePage = lazy(() => import("@/pages/profile/ProfilePage").then((m) => m.ProfilePage));
const PlansPage = lazy(() => import("@/pages/plans/PlansPage").then((m) => m.PlansPage));
const PlanEditorPage = lazy(() => import("@/pages/plans/PlanEditorPage").then((m) => m.PlanEditorPage));
const PlanDetailPage = lazy(() => import("@/pages/plans/PlanDetailPage").then((m) => m.PlanDetailPage));
const BooksPage = lazy(() => import("@/pages/books/BooksPage").then((m) => m.BooksPage));
const BookDetailPage = lazy(() => import("@/pages/books/BookDetailPage").then((m) => m.BookDetailPage));
const ImportPage = lazy(() => import("@/pages/books/ImportPage").then((m) => m.ImportPage));
const RecordsPage = lazy(() => import("@/pages/records/RecordsPage").then((m) => m.RecordsPage));
const SessionRecordPage = lazy(() => import("@/pages/records/SessionRecordPage").then((m) => m.SessionRecordPage));
const PassagesPage = lazy(() => import("@/pages/passages/PassagesPage").then((m) => m.PassagesPage));
const PassageDetailPage = lazy(() => import("@/pages/passages/PassageDetailPage").then((m) => m.PassageDetailPage));
const SheetsPage = lazy(() => import("@/pages/sheets/SheetsPage").then((m) => m.SheetsPage));
const SheetPrintPage = lazy(() => import("@/pages/sheets/SheetPrintPage").then((m) => m.SheetPrintPage));
const SheetNewPage = lazy(() => import("@/pages/sheets/SheetNewPage").then((m) => m.SheetNewPage));
const WordsPage = lazy(() => import("@/pages/records/WordsPage").then((m) => m.WordsPage));
const WordHistoryPage = lazy(() => import("@/pages/records/WordHistoryPage").then((m) => m.WordHistoryPage));
const ClassesPage = lazy(() => import("@/pages/school/classes/ClassesPage").then((m) => m.ClassesPage));
const ClassDetailPage = lazy(() => import("@/pages/school/classes/ClassDetailPage").then((m) => m.ClassDetailPage));
const StudentDetailPage = lazy(() => import("@/pages/school/classes/StudentDetailPage").then((m) => m.StudentDetailPage));
const UsersPage = lazy(() => import("@/pages/admin/users/UsersPage").then((m) => m.UsersPage));
const SettingsPage = lazy(() => import("@/pages/admin/settings/SettingsPage").then((m) => m.SettingsPage));

export const ROUTES: RouteDef[] = [
  // 沉浸式学习页不带导航框架
  { path: "/study/:id", component: StudyPage, cap: "study", bare: true },
  { path: "/sheets/print", component: SheetPrintPage, cap: "study", bare: true },
  { path: "/sheets/:id/print", component: SheetPrintPage, cap: "study", bare: true },

  { path: "/today", component: TodayPage, cap: "study" },
  { path: "/plans", component: PlansPage, cap: "plans" },
  { path: "/plans/new", component: PlanEditorPage, cap: "plans" },
  { path: "/plans/:id", component: PlanDetailPage, cap: "plans" },
  { path: "/plans/:id/edit", component: PlanEditorPage, cap: "plans" },
  { path: "/books", component: BooksPage, cap: "books.read" },
  { path: "/books/import", component: ImportPage, cap: "books.edit" },
  { path: "/books/:id", component: BookDetailPage, cap: "books.read" },
  { path: "/records", component: RecordsPage, cap: "study" },
  { path: "/records/sessions/:id", component: SessionRecordPage, cap: "study" },
  { path: "/passages", component: PassagesPage, cap: "study" },
  { path: "/passages/:id", component: PassageDetailPage, cap: "study" },
  { path: "/sheets", component: SheetsPage, cap: "study" },
  { path: "/sheets/new", component: SheetNewPage, cap: "study" },
  { path: "/words", component: WordsPage, cap: "study" },
  { path: "/words/:wordId", component: WordHistoryPage, cap: "study" },
  { path: "/classes", component: ClassesPage, cap: "classes", feature: "classes" },
  { path: "/classes/:id", component: ClassDetailPage, cap: "classes", feature: "classes" },
  { path: "/classes/:id/students/:userId", component: StudentDetailPage, cap: "classes", feature: "classes" },
  { path: "/profile", component: ProfilePage },
  { path: "/admin/users", component: UsersPage, cap: "users", feature: "multiUser" },
  { path: "/admin/settings", component: SettingsPage, cap: "system" },
];

/** 当前路径是否是沉浸式页面 */
export function isBarePath(path: string): boolean {
  return ROUTES.some((r) => r.bare && matchPath(path, r.path));
}

export function matchPath(path: string, pattern: string): boolean {
  const a = path.split("/").filter(Boolean);
  const b = pattern.split("/").filter(Boolean);
  return a.length === b.length && b.every((seg, i) => seg.startsWith(":") || seg === a[i]);
}
