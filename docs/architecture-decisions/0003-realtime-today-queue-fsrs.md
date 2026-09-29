# 0003 今日队列实时计算 + FSRS

状态：已采纳（沿用服务器版）

## 背景

「方案 → 模板 → 派发 → 任务」的预生成模型复杂度高，学生缺席几天会积下「欠着的任务」；固定间隔复习阶梯不区分单词难度。

## 决定

- 以「学习计划」为唯一编排概念。今日内容不预建，每次按计划和个人记忆状态实时计算（纯函数 `internal/core/today.go`）。
- 记忆排程用 FSRS：`internal/core/fsrs` 是 ts-fsrs 长期排程路径的逐行移植，业务封装在 `internal/core/scheduler.go`。
- 学生不自评，系统按首次作答自动评分。

具体规则见 [decisions.md](../decisions.md) K1～K14，算法见 [product.md §4](../product.md)。

## 后果

- 缺席几天自然处理：新词额度不累计，到期复习自然留存，受每日复习上限约束。
- 防并发重复开组靠部分唯一索引 `StudySession_active_uniq`（K12）。
- 排程算法可替换，只动 `scheduler.go` 与 `fsrs/`；改动后要重新生成对拍数据（`contract/scripts/fsrs-fixtures.mjs`）。
