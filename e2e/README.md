# e2e

端到端用例，跑在构建好的 Go 单文件上（前端已嵌入）。

- `specs/` 里与旧仓库同名的用例（`auth`、`dark-theme`、`dont-know`、`learning`、`mobile-study`、`rbac`、`settings`、`settings-prompts`、`word-sheet`、`ai-generate`）从旧版 `e2e/specs` 原样复制，只去掉了营销页的 `public.spec.ts`（单文件版没有营销页）；`ai-generate` 只把默认地址改成读 `E2E_BASE_URL`。它们证明迁移后行为一致，不要为了让新版通过而改用例内容。
- Go 版新增：`setup-wizard`（首次运行向导与版本切换，需要全新数据目录的实例）、`study-keys`（学习页键盘操作与刷新续做）、`study-exit-confirm`（退出确认框焦点在确认按钮上）。`support/api.ts` 是新增用例用的建计划辅助。
- 运行：`make e2e`（先构建，再由 `run.sh` 起两个实例：seed-demo 数据的 `E2E_PORT`，默认 3250；全新数据目录的 `E2E_FRESH_PORT`，默认 3251；跑完自动停掉并删除临时数据）。只跑一部分：`bash e2e/run.sh learning.spec`。
- 两个 Playwright project：`go`（除向导外的全部用例）、`fresh`（只跑向导）。配置里不自动起服务：本机探测没人监听的端口可能挂起，`run.sh` 先用 `ss` 确认端口空闲。
- AI 用例自带假的 OpenAI 兼容服务（监听 127.0.0.1 随机端口），通过系统设置接口指向它，跑完恢复，不需要真实 AI。
- 旧版学习用例靠固定等待推进，对时序敏感；Go 版响应很快，开始学习时会并行预取学习页代码块，避免「卡片还没出现」的竞态。
