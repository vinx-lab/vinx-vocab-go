import { lazy } from "preact-iso";

/**
 * 学习页按路由懒加载；开始学习时（今日页「学新词 / 复习」、单词单「开始测试」）与创建学习组的请求并行预取这个块，
 * 跳转后不必再等代码下载，只剩取学习组数据。
 */
export const StudyPage = lazy(() => import("./StudyPage").then((m) => m.StudyPage));
export const preloadStudyPage = (): Promise<unknown> => StudyPage.preload().catch(() => undefined);
