# Vinx Vocab 产品说明

本文描述产品现在是什么样。决定的理由见 [decisions.md](decisions.md)，模块落位见 [architecture.md](architecture.md)，运行和部署见 [README](../README.md) 与 [deployment.md](deployment.md)。

单文件版与服务器版功能一致；版本向导与双向切换（K43）、首个账号为管理员（K44）是单文件版新增的规则。

## 0. 一句话

**学生每天打开只看到“今天要做什么”，点一下就开始；老师只需要建班、选单元、定每天几个词；记忆排程交给 FSRS。**

用户可见的四个概念：

| 概念 | 用户理解 |
|---|---|
| **词书 / 单元** | 教材、词表 |
| **学习计划** | 学哪些单元、每天几个新词、复习上限、题型 |
| **今日** | 按计划实时算出来的队列 |
| **记录** | 每一组学习、每一道题、每个词的记忆状态 |

## 1. 产品方向与长期约定

- **两种形态**（K24）：`VINX_EDITION=personal|school`，默认 `school`。个人版单人使用；班级版多出班级管理。新功能先想「个人版是否也要」，再决定放不放 `school/` 目录。
- **权限只加能力**（K23）：角色能力表 `internal/core/access.go`（3 角色 × 9 能力）是唯一事实来源，不引入权限点表或权限管理界面。
- **AI 走供应商抽象**（K25）：一律通过 `internal/service/ai.go`，默认 OpenAI 兼容接口（Ollama 已实测），Claude 可选；未配置时前端隐藏入口。
- **移动优先**：学习与检测必须在手机上好用（底部标签导航、48px+ 点击区、输入框 16px、安全区留白），`e2e/specs/mobile-study.spec.ts` 用手机视口跑完整学习流。

## 2. 角色与页面

### 学生（手机优先）
- **今日** `/today`：连续天数、今日进度；每个生效计划一张卡：「学新词（还剩 N）」「复习（到期 M）」「检测」；单词单只显示下一份（进行中的优先，否则编号最小的未测单子），写成「单词单 #2 · 30 词（还有 3 份待测）」；另有「错词强化」入口（不影响记忆）。
- **学习** `/study/:id`：沉浸式全屏。新学：认识卡片 → 练习（认义选择 / 拼写 / 挖空填词）→ 巩固错词；复习：练习 → 巩固；检测：连续作答 → 成绩单。
- **计划** `/plans`：老师给我的 + 我自己建的；可新建自主计划。
- **词书** `/books`：浏览词书单元，单元里分「词表 / 句型 / 课文」三个标签，句子可以点读（K49）；从单元一键“用这个单元建计划”。单词详情里有「出现在这些句子里」。
- **我的单词** `/words`、`/words/:wordId`：记忆状态列表（到期 / 困难 / 已掌握）、搜索、单词复习轨迹。
- **记录** `/records`：统计（近 30 天学习量、正确率、记忆分布、连续天数）+ 学习历史 → 每组详情（逐题）。设了目标词书时多一块「目标词书」：每本书一条三段进度条（会了 / 要学 / 未测），点开按状态看词表（K47）。
- **目标进度**（今日页，计划卡上方）：已测 / 应测，会了 · 要学 · 未测，「测未测的词」「练要学的词」直接进单词单生成页（K47）。没有目标时不显示。
- **短文巩固** `/passages`（配置 AI 后出现）：用到期 / 易错 / 最近学过 / 手选的词生成短文（K28）。选好来源和词数后先预览这次用的单词（可去掉个别词）和提示词，可以按这次的需要改提示词再生成；生成在后台进行，页面显示「生成中… 已用 N 秒」，完成后打开短文，失败时显示具体原因（K41）。
- **单词单** `/sheets`（侧栏，手机从今日卡片进入）：生成页先选**来源**（K33）——「不熟的词」（默认：近 14 天答错 ×3、遗忘 ×2、学习中 +2 / 巩固中 +1、3 天内到期 +1；排除已掌握且干净的词和其他待测单上的词）/「某次测试的错词」（近 30 天有首次答错的检测、单词单测试、新学 / 复习组，选一次用那次答错的词）/「某个单元」（单元下拉第一项是「整本书」，按单元顺序展开去重；先放不熟的，再放没学过的；已掌握且干净的排除）；任何来源都可再勾掉或搜索加词。**份数 1～7（默认 1）× 每份词数 10～30（默认 30）**，按顺序从难到易分到各份（第 1 份最难），编号连续（K34）→ 进入合并打印页 `/sheets/print?ids=…`（单张仍可用 `/sheets/:id/print`）：每份一页 A4，折线在纸张正中 105mm，页边距为 0、浏览器不印页眉页脚，双面（长边翻转）4 份 = 2 张纸（K35）→ 回家在今日页每天测一份；列表可勾选几份合并打印；成绩单可「用错词再出一张」（K29）。
- **默写单**（单词单的另一种格式，K51）：看中文写英文，可混合单词、短语、句子、仿写、句式转换；打印为题目页 + 答案页（横线或四线三格）；手写后在批改页点出错题提交，结果计入正式测试。学生本人的账号也能批改，标记为「自批」。
- **我的** `/profile`：资料、外观、改密码、加入班级（邀请码）、我的班级、目标词书（有班级时只读，没有班级时自己设）。
- **外观**（所有角色，右上角用户菜单或个人中心）：跟随系统（默认）/ 浅色 / 深色，跟账号保存（`User.theme`），本机 `localStorage` 只做缓存；单词单打印页始终白底黑字。

### 老师（班级版）
- 学生的全部能力（老师也可以自己背单词）。
- **班级** `/classes`：建班、邀请码、成员管理（邀请码加入 / 按邮箱添加 / 批量创建学生账号）；「目标词书」标签给全班设目标（K47）。
- **班级详情** `/classes/:id`：今日概览表（每个学生：今日新学/复习完成、连续天数、最近学习、7 天正确率、已学词数、目标覆盖、要学，以及自批比例）、班级难词榜、本班计划。
- **学生详情** `/classes/:id/students/:userId`：与学生“记录”同一组件，只读；「单词单」标签可为该学生生成、打印单词单（测试只能由学生本人做）。
- **计划**：建计划时可选班级 / 学生；计划详情显示每个学生的进度。
- **词书**：创建自己的词书、导入 txt（解析预览 → 修正 → 确认；可带 `[句型]`、`[课文]` 段）、单元的句型和课文（粘贴导入、逐句编辑）、词书学段（小学 / 初中 / 中考）、AI 生成重点句型和照例句仿写变式（草稿审核后保存，K50）、编辑词条、AI 补例句（整单元每次最多 20 个缺例句的词，或单个词；先弹出预览，确认单词和提示词后在后台生成，K41）、预缓存发音、单元排序。

### 管理员
- 全部数据；系统词书维护；**用户管理** `/admin/users`（建号 / 改角色 / 重置密码）。没有角色、权限管理页（K23）。
- **系统设置** `/admin/settings`（个人版也有）：AI 接口（K38）；AI 提示词（K41）——例句、短文、句型、仿写四段默认要求模板可修改（支持 `{学段}` 等占位符），可一键恢复默认；输出格式由系统自动加上，不在这里编辑。

## 3. 数据模型

```
User ─┬─ ClassMember ─ Classroom(teacherId, inviteCode)
      ├─ Plan(creatorId) ─┬─ PlanUnit ─ Unit ─ Book
      │                   └─ PlanTarget(classId | userId)
      ├─ MemoryState(userId, wordId)  ← FSRS 卡片状态（唯一）
      ├─ StudySession(userId, planId?, kind, status, snapshot)
      │     └─ Answer(sessionId, wordId, mode, phase, attempt, correct)
      ├─ ReviewLog(userId, wordId, sessionId, rating, 前后状态)
      ├─ WordSheet(userId, creatorId, seq, wordIds[], modes[], format, items[])  ← 单词单 / 默写单
      │     └─ SentenceAnswer(sessionId, sentenceId, correct)  ← 默写单的句子题
      ├─ UserTargetBook(userId, bookId)  ← 没有班级时自己的目标词书
      └─ Passage（AI 巩固短文）─ PassageSentence ─ Sentence
Classroom ─ ClassTargetBook(classId, bookId)
Book(level) ─ Unit ─┬─ UnitWord ─ Word(spelling unique)
                    └─ UnitText(kind text|list) ─ UnitTextSentence ─ Sentence(en, cn, frame?, source) ─ SentenceWord ─ Word
```

- `User.theme` 为 `system | light | dark`，默认 `system`。
- `User.role` 为 `student | teacher | admin` 字符串，能力由代码里的角色能力表推导，**数据库没有权限表**（K23）。
- `Plan.kind`: `daily | test`；`status`: `active | paused | archived`；`newPerDay`、`reviewPerDay`、`modes[]`（`recognition` / `spelling` / `cloze`）、`order`（`sequential`/`random`）、`testSize`、`testScope`（`all`/`learned`）、`startDate`/`endDate`（可空）。
- `StudySession.kind`: `learn | review | test | drill | sheet`；`sheetId` 指向单词单（仅 `sheet` 组）；`snapshot` JSON 冻结词表、题目选项、题型、计划名；`progress` JSON 记录客户端阶段游标，用于续做；`dayKey` 为 `YYYY-MM-DD`（学习日）。
- `WordSheet`：`(userId, seq)` 唯一，`wordIds` 生成后冻结；**状态不落库**——没有已完成的 `sheet` 组即「待测」，有即「已测」，成绩取首次交卷（K29）。
- `Answer` 唯一键 `(sessionId, wordId, mode, phase, attempt)` 保证网络重试幂等。
- `MemoryState` 存 FSRS 字段 `due / stability / difficulty / elapsedDays / scheduledDays / reps / lapses / state / lastReview`，另存 `introducedAt`、`introducedPlanId`（计算“今日已学新词”）。
- 发音缓存在数据目录的 `audio/`，运行时生成，不入库。

## 4. 今日队列算法（纯函数 `internal/core/today.go`）

对学生 u、今天 d（学习日范围 `[start,end)`）：

1. 生效计划 = `status=active` 且 (`PlanTarget.userId=u` 或 u 是 `PlanTarget.classId` 的成员) 且日期在区间内。
2. `daily` 计划 p：
   - `newDoneToday` = u 的 MemoryState 中 `introducedPlanId=p` 且 `introducedAt∈今天` 的数量；`newLeft = max(0, newPerDay − newDoneToday)`；
   - `newAvailable` = p 范围内 u 尚无 MemoryState 的词数（按单元顺序 / 随机）；
   - `dueWords` = p 范围内 u 的 MemoryState 中 `due < end`；按今日所有计划合并去重，词归属第一个包含它的计划；
   - `reviewDoneToday` = 今天 u 在 p 下已完成复习的去重词数；`reviewLeft = min(|dueWords|, max(0, reviewPerDay − reviewDoneToday))`；
   - 进行中的组（可续做）。
3. `test` 计划：未完成过则显示，完成后显示成绩。
4. 计划完成度 = 范围内已学词 / 范围总词数。

## 5. 学习流与评分

- 题型：`recognition`（看英文选中文，4 选 1，干扰项优先同词性、同范围）、`spelling`（看中文 + 词性拼写，可用“提示首字母”，用提示记为 hint）、`cloze`（例句挖空填词；没有例句的词自动跳过该题型，K27）。
- 新学组：认识卡片（可发音、可前后翻）→ 练习（每词 × 每题型各一题，乱序）→ 巩固（错过的词重做直到答对，最多 3 轮）→ 完成。
- 复习组：练习 → 巩固 → 完成。
- 检测：每词 × 每题型一题，作答不反馈，最后出成绩单（对错明细）。
- 「不会」（K39）：所有题型、所有阶段都可以点「不会」，按答错计入所有口径（正确率、FSRS 评分、巩固、错词强化、单词单错词来源）；练习和巩固阶段和答错一样立即亮出正确答案，检测类组不反馈直接下一题；记录里标 `dontKnow`，逐题明细和成绩单显示「不会」而不是所选或填写的内容。
- 错词强化（`drill`）：取最近 14 天答错或 lapses 最多的词，练习 + 巩固，**不更新记忆**。
- 单词单测试（`sheet`）：与检测同口径（`isTestKind()`，`@vinx/shared`）——作答不反馈、进行中不下发答案、K10 评分、K19 每词每日最多更新一次、首次交卷为正式成绩（重测只算练习，按同一张单子判断）；同一学生同时只有一张在测（K30）；进行中的单词单不占用复习词。
- 进行中检测类组（`test`/`sheet`）的作答在交卷前不进入任何统计与计数（正确率、错词强化、单词单预览）。
- 完成时服务端按 K7–K10 计算每词评分，调用 FSRS 更新 MemoryState 并写 ReviewLog，在一个事务内完成，重复调用幂等（`status=completed` 直接返回结果）。
- “结束本组”：只结算已作答完所有题型的词；没练完的新词不入记忆库，回到可学池。

## 6. 统计口径

- **已学词** = MemoryState 数；**已掌握** = `stability ≥ 21 天`；**巩固中** = `7 ≤ stability < 21`；**学习中** = 其余；**到期** = `due < 今日结束`。
- **正确率（近 N 天）** = 练习/检测阶段**首次作答**正确数 / 首次作答数（不含巩固）。
- **连续天数** = 截至今天（今天未学则截至昨天）连续有完成组的学习日数。
- **学习时长** = 各组内题目作答时长之和（单题上限 60 秒，防挂机累加）。
- 词数 ≠ 题数 ≠ 组数，界面分别标注。
- **覆盖进度**（K47）：目标词 = 目标词书按词去重，全部算进分母（K48）。正式测试作答 = 检测、单词单测试、默写单批改中的首次作答。每个词：没有作答为「未测」，最近一次错为「要学」，最近一次对、或错后记忆达到「已掌握」为「会了」；会了的词再错退回要学。不落库，按请求现算。句子同口径（没有记忆状态那一条）。

## 7. API 概览（统一包络）

| 模块 | 端点 |
|---|---|
| 认证 | `/auth/signup`（可选 `inviteCode`）、`login`、`logout`、`me`、`change-password`、`profile` |
| 配置 | `GET /config`（版本形态、功能开关）、`GET /health` |
| 词书 | `GET/POST /books`、`GET/PATCH/DELETE /books/:id`、`POST /books/:id/units`、`PATCH /books/:id/units/order`、`PATCH/DELETE /units/:id`、`POST /books/import/preview`、`POST /books/import`、`GET/POST /units/:id/words`、`DELETE /units/:id/words/:wordId`、`GET /words?q=`、`GET/PATCH /words/:id` |
| 班级（班级版） | `GET/POST /classes`、`GET/PATCH/DELETE /classes/:id`、`POST /classes/:id/members`、`POST /classes/:id/members/batch`、`DELETE /classes/:id/members/:userId`、`POST /classes/:id/members/:userId/reset-password`、`POST /classes/:id/invite-code`、`POST /classes/join`、`GET /classes/:id/overview`、`GET /me/classes`、`DELETE /me/classes/:id` |
| 计划 | `GET/POST /plans`、`GET/PATCH/DELETE /plans/:id`、`GET /plans/:id/progress`、`POST /plans/preview` |
| 今日与学习 | `GET /today`、`POST /study/sessions`、`GET/DELETE /study/sessions/:id`、`POST /study/sessions/:id/answers`、`PATCH /study/sessions/:id/progress`、`POST /study/sessions/:id/complete` |
| 单词单 | `POST /sheets/preview`（`{ userId?, count 10–210, source?: {kind:"unfamiliar"} \| {kind:"session",sessionId} \| {kind:"unit",unitId}, include? }`）、`GET /sheets/sources?userId`（近 30 天有错词的学习组）、`POST /sheets`（`{ userId?, wordIds 1–210, copies 1–7 = 1, perSheet 1–30 = 30, modes }` → `{ id, seq, items: [{id, seq}] }`）、`GET /sheets?userId`、`GET /sheets/:id`、`DELETE /sheets/:id`（仅从未开测的）；开测 `POST /study/sessions { kind: "sheet", sheetId }`；`GET /today` 含 `sheet: { id, seq, wordCount, activeSessionId, remaining } \| null` |
| 记录 | `GET /records/summary`、`/records/sessions`、`/records/sessions/:id`、`/records/words`、`/records/words/:wordId`、`/records/daily`（老师可带 `userId`） |
| AI 与发音 | `GET /ai/status`、`POST /ai/passages/preview`、`POST /ai/units/:id/examples/preview`、`POST /ai/words/:id/example/preview`（返回 `{ words, prompt }`，不调用 AI）、`POST /ai/units/:id/examples`、`POST /ai/words/:id/example`、`POST /passages/generate`（请求体带 `wordIds`、`prompt`，返回 `{ jobId }`）、`GET /ai/jobs/:id`（`{ status: running \| done \| failed, elapsedMs, result?, error? }`，只有发起人能查）、`GET /audio/words/:id`、`POST /audio/units/:id/prefetch`、`GET /passages`、`GET/DELETE /passages/:id` |
| 系统设置（`system`） | `GET/PUT/DELETE /settings/ai`、`POST /settings/ai/test`、`GET/PUT /settings/ai/prompts`（默认要求模板，`{ example?, passage? }` 只更新传了的项）、`DELETE /settings/ai/prompts/:key`（`example` \| `passage`，恢复默认） |
| 用户（班级版） | `GET/POST /users`、`PATCH /users/:id`、`POST /users/:id/reset-password` |

数据作用域：学生仅本人；老师可看自己班级成员、自己创建的计划；管理员全部。集中判定在 `internal/service/access.go`。

## 8. 权限

角色能力表（`internal/core/access.go`，K23）：

| 能力 | 含义 | student | teacher | admin |
|---|---|:-:|:-:|:-:|
| `study` | 今日 / 学习 / 自己的记录 | ✓ | ✓ | ✓ |
| `plans` | 自己的学习计划 | ✓ | ✓ | ✓ |
| `books.read` | 浏览词书 | ✓ | ✓ | ✓ |
| `plans.assign` | 给班级或学生安排计划 | | ✓ | ✓ |
| `books.edit` | 建词书 / 导入 / 编辑词条（含 AI 例句、发音缓存） | | ✓ | ✓ |
| `classes` | 班级与成员管理 | | ✓ | ✓ |
| `students.view` | 查看学生的学习记录 | | ✓ | ✓ |
| `users` | 用户管理 | | ✓ | ✓ |
| `system` | 系统词书、系统设置 | | | ✓ |

后端守卫 `requireCap(...)`（`internal/auth` 的 `RequireAuth` / `RequireCap`）；老师对账号的操作另受 K21 约束。

## 9. 不做（当前边界）

- 排行榜、社交。
- 计划内多活动组合、课程解锁（学完 Unit 1 才开 Unit 2）。
- FSRS 参数个性化训练（需积累足够 ReviewLog 后再做）。
- 移动端原生 App（Web 响应式覆盖手机）。
- 跨班搜索学生。
- 单词单：手机端打印、导出 PDF/Word、老师一次给全班批量生成、纸面带例句；一份跨两页的长单子（每份最多 30 词）。

## 10. 验收基线

1. `make vet`、`make test`、前端 `tsc` 与 vitest、`make e2e`、API 契约测试全绿。
2. E2E 主线：老师建班 → 学生凭邀请码注册入班 → 老师为班级建计划 → 学生今日看到计划 → 学完一组新词 → 老师班级概览看到进度；学生自建计划同链路；手机视口完整学习流。
3. 种子数据：五本教材词书（七上～九上）+ 中考词汇；演示老师/学生/班级/计划。
