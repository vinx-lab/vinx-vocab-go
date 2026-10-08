# 更新记录

格式参考 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，版本号遵循[语义化版本](https://semver.org/lang/zh-CN/)：只修 bug 升修订号，新增功能升次版本号，不兼容的改动升主版本号。版本号来自 git 标签 `vX.Y.Z`，构建时注入程序，页面右上角用户菜单和登录页显示。

## [Unreleased]

### 新增

- `make release` 发版：从 git 标签算出下一个版本号，改 CHANGELOG、检查、提交、打标签、构建；推送标签由 GitHub Actions 构建 Windows / Linux 包并发布 Release；`main` 上加 CI。

## [3.1.0] - 2026-10-07

单文件版的第一个正式版本：Go + SQLite + 嵌入前端，一个可执行文件跑完整应用，功能与服务器版（Node + PostgreSQL）一致，服务器版数据可完整导入。功能与使用流程见 [docs/features.md](https://github.com/vinx-lab/vinx-vocab-go/blob/v3.1.0/docs/features.md)。

### 新增

- 单文件版：Windows 双击即用，Linux / NAS 作班级服务器；首次运行选择个人版或班级版，之后可双向切换、降级不删数据；首个注册账号为管理员；从服务器版 PostgreSQL 一次性只读导入（[spec 0001](https://github.com/vinx-lab/vinx-vocab-go/blob/v3.1.0/docs/specs/0001-go-single-binary.md)）。
- 开发模式：免密切换用户，支持仅本标签页登录（[spec 0002](https://github.com/vinx-lab/vinx-vocab-go/blob/v3.1.0/docs/specs/0002-dev-user-switch.md)）。
- 目标词书与覆盖进度：老师给班级设目标，学生今日页、记录页、班级概览显示覆盖进度（[spec 0003](https://github.com/vinx-lab/vinx-vocab-go/blob/v3.1.0/docs/specs/0003-target-books-coverage.md)）。
- 内容模型「词 ← 句 ← 篇」：单元的句型和课文、句子点读、单词反查所在句子（[spec 0004](https://github.com/vinx-lab/vinx-vocab-go/blob/v3.1.0/docs/specs/0004-sentences-passages.md)）。
- AI 生成按词书学段调整难度，新增重点句型和仿写变式，生成后自动检查、草稿审核后保存（[spec 0005](https://github.com/vinx-lab/vinx-vocab-go/blob/v3.1.0/docs/specs/0005-ai-generation.md)）。
- 默写单：看中文写英文，打印题目页 + 答案页，网页批改录入，学生自批单独标记（[spec 0006](https://github.com/vinx-lab/vinx-vocab-go/blob/v3.1.0/docs/specs/0006-dictation-sheets.md)）。
- 中考核心词汇清洗，带注释的拼写并回同一个词（[spec 0007](https://github.com/vinx-lab/vinx-vocab-go/blob/v3.1.0/docs/specs/0007-exam-vocab-cleanup.md)）。
- 班级「允许学生自主安排」开关；计划只能从目标词书里选单元（[spec 0008](https://github.com/vinx-lab/vinx-vocab-go/blob/v3.1.0/docs/specs/0008-class-self-plan-and-target-first.md)）。
- 每次作答都算数：任何入口的首次作答都更新记忆，五级词状态（未接触 / 没记住 / 刚记住 / 巩固中 / 已掌握），`memory backfill` 补算历史记忆（[spec 0009](https://github.com/vinx-lab/vinx-vocab-go/blob/v3.1.0/docs/specs/0009-every-answer-counts.md)）。
- 页面显示程序版本（用户菜单、登录页），`/api/config` 返回 `version`。

### 修复

- 长页面滚轮和手指滑动滚不动。
- 手机上贴底的提交栏被底部导航挡住。

### 升级说明

- 从 spec 0009 之前的构建升级：替换程序后、启动服务前，先运行 `vinx-vocab memory backfill --data <数据目录>` 看演练结果，再加 `--apply` 执行（自动备份）。

[Unreleased]: https://github.com/vinx-lab/vinx-vocab-go/compare/v3.1.0...HEAD
[3.1.0]: https://github.com/vinx-lab/vinx-vocab-go/releases/tag/v3.1.0
