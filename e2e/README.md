# e2e

端到端用例，跑在构建好的 Go 单文件上（前端已嵌入）。

- `specs/` 里与旧仓库同名的用例（`auth`、`dark-theme`、`dont-know`、`learning`、`mobile-study`、`rbac`、`settings`、`settings-prompts`、`word-sheet`、`ai-generate`）从旧版 `e2e/specs` 原样复制，只去掉了营销页的 `public.spec.ts`（单文件版没有营销页）；`ai-generate` 只把默认地址改成读 `E2E_BASE_URL`。它们证明迁移后行为一致，不要为了让新版通过而改用例内容。例外（ADR 0004，以单文件版为准后的有意改动）：spec 0005 把默认提示词模板改成带学段占位符，`settings-prompts` 对照的默认模板开头改为「你是{学段}英语教材的例句编辑」，`ai-generate` 改提示词时替换的长度文字改为初中学段的「总长 80～140 词」。
- Go 版新增：`setup-wizard`（首次运行向导与版本切换，需要全新数据目录的实例）、`study-keys`（学习页键盘操作与刷新续做）、`study-exit-confirm`（退出确认框焦点在确认按钮上）。`support/api.ts` 是新增用例用的建计划辅助。
- 单文件版新功能（spec 0002–0006）：`dev-switch`（开发模式两个标签页各自切换账号，需要 `--dev` 实例）、`coverage`（老师设目标词书 → 学生今日进度卡 → 测未测的词 → 老师概览与学生详情一致）、`sentences`（导入带句型 / 课文的单元 → 学生单元页与单词详情）、`ai-patterns`（假 AI 生成句型 → 编辑草稿 → 保存 → 学生看到）、`dictation`（出默写单 → 题目页 + 答案页 → 批改两道错题 → 今日已批改 → 错词变成要学）。`support/school.ts` 用 API 准备新老师、班级学生和词书。
- 运行：`make e2e`（先构建，再由 `run.sh` 起三个实例：seed-demo 数据的 `E2E_PORT`，默认 3250；全新数据目录的 `E2E_FRESH_PORT`，默认 3251；另一份 seed-demo 数据、以 `--dev` 启动的 `E2E_DEV_PORT`，默认 3252；跑完自动停掉并删除临时数据）。只跑一部分：`bash e2e/run.sh learning.spec`。
- 三个 Playwright project：`go`（除向导和开发模式外的全部用例）、`fresh`（只跑向导）、`dev`（只跑 `dev-switch`）。配置里不自动起服务：本机探测没人监听的端口可能挂起，`run.sh` 先用 `ss` 确认端口空闲。
- AI 用例自带假的 OpenAI 兼容服务（监听 127.0.0.1 随机端口），通过系统设置接口指向它，跑完恢复，不需要真实 AI。
- 旧版学习用例靠固定等待推进，对时序敏感；Go 版响应很快，开始学习时会并行预取学习页代码块，避免「卡片还没出现」的竞态。
